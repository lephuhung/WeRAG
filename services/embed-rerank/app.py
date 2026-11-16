"""GPU-backed embedding and reranking API used by the WeRAG model adapter."""

from __future__ import annotations

import asyncio
import logging
import math
import os
from contextlib import asynccontextmanager
from typing import Any, Callable

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from pydantic import BaseModel, ConfigDict, Field, field_validator

logger = logging.getLogger("werag.embed_rerank")
logging.basicConfig(level=os.getenv("LOG_LEVEL", "INFO"))

DEFAULT_EMBEDDING_MODEL = "mainguyen9/vietlegal-harrier-0.6b"
DEFAULT_RERANKER_MODEL = "BAAI/bge-reranker-v2-m3"
MAX_BATCH_SIZE = 512

EMBEDDING_MODEL = os.getenv("HRAG_EMBEDDING_MODEL", DEFAULT_EMBEDDING_MODEL)
RERANKER_MODEL = os.getenv("HRAG_RERANKER_MODEL", DEFAULT_RERANKER_MODEL)
EMBEDDING_DEVICE = os.getenv("HRAG_EMBEDDING_DEVICE", "auto")
RERANKER_DEVICE = os.getenv("HRAG_RERANKER_DEVICE", "auto")
EMBEDDING_BATCH_SIZE = max(1, int(os.getenv("HRAG_EMBEDDING_BATCH_SIZE", "32")))
RERANKER_BATCH_SIZE = max(1, int(os.getenv("HRAG_RERANKER_BATCH_SIZE", "16")))
GPU_CONCURRENCY = max(1, int(os.getenv("EMBED_RERANK_CONCURRENCY", "1")))
GPU_SEMAPHORE = asyncio.Semaphore(GPU_CONCURRENCY)

# Both models load in float32 by default, which is twice the memory traffic
# and, on this GPU, roughly half the throughput of float16 for no difference
# that survives ranking. Only applied on CUDA: half precision on CPU is
# either unsupported or slower.
USE_FP16 = os.getenv("HRAG_FP16", "true").strip().lower() in {"1", "true", "yes", "on"}

# Embedding is dominated by per-call overhead rather than compute: one text
# costs about as much as thirty-two. Requests that arrive while the GPU is
# busy are therefore coalesced into a single encode call. The queue is
# drained opportunistically — only what is already waiting is taken, never
# waiting for more to show up — so a lone request keeps its original latency
# and batches only form when there is real concurrency to exploit.
EMBED_COALESCE_MAX = max(1, int(os.getenv("HRAG_EMBED_COALESCE_MAX", "256")))

# A request of at most this many texts is a "small" one — in practice a
# single query being embedded for a retrieval. Small requests are only ever
# merged with other small ones: a question that arrives just before a
# 512-text ingest batch would otherwise wait for that whole batch, which is
# slower than it was before coalescing existed.
EMBED_COALESCE_SMALL = max(1, int(os.getenv("HRAG_EMBED_COALESCE_SMALL", "8")))


def _to_fp16(model: Any, inner: Any, label: str) -> Any:
    """Halve `inner`'s weights when asked for and the model sits on CUDA."""
    if not USE_FP16:
        return model
    device = str(getattr(model, "device", "") or getattr(inner, "device", ""))
    if "cuda" not in device:
        logger.info("%s stays float32 (device=%s)", label, device or "unknown")
        return model
    try:
        inner.half()
    except Exception:  # noqa: BLE001 - any failure here must not stop startup
        logger.warning("%s could not be converted to float16, keeping float32", label, exc_info=True)
        return model
    logger.info("%s converted to float16", label)
    return model


def load_embedding_model() -> Any:
    """Load the configured sentence-transformers model on its target device."""
    from sentence_transformers import SentenceTransformer

    device = None if EMBEDDING_DEVICE == "auto" else EMBEDDING_DEVICE
    model = SentenceTransformer(EMBEDDING_MODEL, device=device)
    return _to_fp16(model, model, f"embedding model {EMBEDDING_MODEL}")


def load_reranker_model() -> Any:
    """Load the configured cross-encoder model on its target device."""
    from sentence_transformers import CrossEncoder

    device = None if RERANKER_DEVICE == "auto" else RERANKER_DEVICE
    model = CrossEncoder(RERANKER_MODEL, device=device)
    # CrossEncoder wraps the transformer in .model; halving the wrapper itself
    # would miss the weights that matter.
    inner = getattr(model, "model", model)
    return _to_fp16(model, inner, f"reranker model {RERANKER_MODEL}")



class _EmbedRequest:
    """One caller's texts and the future carrying their vectors back."""

    __slots__ = ("texts", "future")

    def __init__(self, texts: list[str], future: "asyncio.Future[list[list[float]]]") -> None:
        self.texts = texts
        self.future = future


class EmbedCoalescer:
    """Runs queued /embed calls through one GPU encode at a time.

    The worker takes a request, then adds whatever else is *already* queued
    (never waiting for more), encodes the lot in one call and hands each
    caller its own slice back. Under concurrency the queue fills while the
    GPU is busy with the previous batch, so batches form on their own; a lone
    request is encoded immediately and pays no coalescing delay.
    """

    def __init__(
        self,
        encode: Callable[[list[str]], list[list[float]]],
        max_texts: int,
        small: int = EMBED_COALESCE_SMALL,
    ) -> None:
        self._encode = encode
        self._max_texts = max_texts
        self._small = small
        self._queue: "asyncio.Queue[_EmbedRequest]" = asyncio.Queue()
        self._task: asyncio.Task[None] | None = None

    def start(self) -> None:
        if self._task is None:
            self._task = asyncio.create_task(self._run(), name="embed-coalescer")
            # A dead worker would otherwise leave every later caller waiting
            # on a future nobody will ever complete.
            self._task.add_done_callback(self._worker_finished)

    def alive(self) -> bool:
        """Whether the worker is running (the health endpoint reports this)."""
        return self._task is not None and not self._task.done()

    def _worker_finished(self, task: "asyncio.Task[None]") -> None:
        """Fail everything still queued once the worker is gone."""
        if not task.cancelled() and task.exception() is not None:
            logger.error("Embedding worker stopped: %r", task.exception())
        self._fail_queued(RuntimeError("embedding worker is not running"))

    def _fail_queued(self, exc: BaseException) -> None:
        while True:
            try:
                req = self._queue.get_nowait()
            except asyncio.QueueEmpty:
                return
            if not req.future.done():
                req.future.set_exception(exc)

    async def stop(self) -> None:
        if self._task is None:
            return
        task, self._task = self._task, None
        task.cancel()
        try:
            await task
        except asyncio.CancelledError:
            pass
        # A worker that already died carries its cause; shutdown is not the
        # place to raise it again, and the callers were told at the time.
        except BaseException:  # noqa: BLE001
            logger.debug("Embedding worker had already stopped", exc_info=True)

    async def embed(self, texts: list[str]) -> list[list[float]]:
        if not texts:
            return []
        # Fail fast rather than queue onto a worker that has stopped, or one
        # that was never started.
        if not self.alive():
            raise RuntimeError("embedding worker is not running")
        future: "asyncio.Future[list[list[float]]]" = asyncio.get_running_loop().create_future()
        await self._queue.put(_EmbedRequest(texts, future))
        return await future

    def _drain(self, first: _EmbedRequest) -> list[_EmbedRequest]:
        """Add the queued requests that belong with `first`.

        A large request is already an efficient batch, so it goes alone.
        Small ones are merged with each other only: mixing a single query in
        with an ingest batch makes the query wait for the whole batch.
        """
        batch = [first]
        if len(first.texts) > self._small:
            return batch
        total = len(first.texts)
        deferred: list[_EmbedRequest] = []
        while total < self._max_texts:
            try:
                nxt = self._queue.get_nowait()
            except asyncio.QueueEmpty:
                break
            if len(nxt.texts) > self._small:
                deferred.append(nxt)
                break
            batch.append(nxt)
            total += len(nxt.texts)
        # Whatever was pulled out but does not belong in this batch keeps its
        # place at the head of the queue.
        for req in reversed(deferred):
            self._queue.put_nowait(req)
        return batch

    async def _run(self) -> None:
        while True:
            first = await self._queue.get()
            batch = [req for req in self._drain(first) if not req.future.done()]
            if not batch:
                continue
            flat: list[str] = []
            for req in batch:
                flat.extend(req.texts)
            try:
                vectors = await run_gpu(lambda texts=flat: self._encode(texts))
            except asyncio.CancelledError:
                for req in batch:
                    if not req.future.done():
                        req.future.cancel()
                raise
            # BaseException, not Exception: a batch is shared, so anything
            # that ends this pass — including the errors Exception does not
            # cover — has to reach its callers before it propagates, or they
            # wait on futures nobody will complete.
            except BaseException as exc:
                logger.exception("Embedding batch of %d text(s) failed", len(flat))
                for req in batch:
                    if not req.future.done():
                        req.future.set_exception(exc)
                if not isinstance(exc, Exception):
                    raise
                continue
            if len(vectors) != len(flat):
                err = RuntimeError(
                    f"embedding model returned {len(vectors)} vectors for {len(flat)} texts"
                )
                logger.error("%s", err)
                for req in batch:
                    if not req.future.done():
                        req.future.set_exception(err)
                continue
            offset = 0
            for req in batch:
                take = len(req.texts)
                if not req.future.done():
                    req.future.set_result(vectors[offset : offset + take])
                offset += take


def _dimension(model: Any) -> int:
    getter = getattr(model, "get_embedding_dimension", None)
    if getter is None:
        getter = model.get_sentence_embedding_dimension
    return int(getter())


@asynccontextmanager
async def lifespan(application: FastAPI):
    logger.info("Loading embedding model %s", EMBEDDING_MODEL)
    embedding_model = load_embedding_model()
    logger.info("Loading reranker model %s", RERANKER_MODEL)
    reranker_model = load_reranker_model()

    application.state.embedding_model = embedding_model
    application.state.reranker_model = reranker_model
    application.state.embedding_dimension = _dimension(embedding_model)

    def encode_texts(texts: list[str]) -> list[list[float]]:
        return embedding_model.encode(
            texts,
            convert_to_numpy=True,
            normalize_embeddings=True,
            batch_size=EMBEDDING_BATCH_SIZE,
        ).tolist()

    coalescer = EmbedCoalescer(encode_texts, EMBED_COALESCE_MAX)
    application.state.embed_coalescer = coalescer

    await run_gpu(
        lambda: embedding_model.encode(
            ["WeRAG embedding service warmup"],
            convert_to_numpy=True,
            normalize_embeddings=True,
            batch_size=1,
        )
    )
    await run_gpu(lambda: reranker_model.predict([("WeRAG warmup", "model ready")]))
    logger.info(
        "Embed/rerank service ready (embedding=%s, dimension=%d, reranker=%s, "
        "concurrency=%d, fp16=%s, embed_coalesce_max=%d)",
        EMBEDDING_MODEL,
        application.state.embedding_dimension,
        RERANKER_MODEL,
        GPU_CONCURRENCY,
        USE_FP16,
        EMBED_COALESCE_MAX,
    )
    coalescer.start()
    try:
        yield
    finally:
        await coalescer.stop()


app = FastAPI(title="WeRAG embedding and reranking", lifespan=lifespan)


@app.exception_handler(RequestValidationError)
async def validation_error_handler(
    request: Request, exc: RequestValidationError
) -> JSONResponse:
    # Pydantic can include NaN/Infinity in error inputs, which are not valid JSON.
    errors = [
        {"loc": error["loc"], "msg": error["msg"], "type": error["type"]}
        for error in exc.errors()
    ]
    return JSONResponse(status_code=422, content={"detail": errors})


class EmbedRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")
    texts: list[str] = Field(max_length=MAX_BATCH_SIZE)

    @field_validator("texts")
    @classmethod
    def require_nonempty_texts(cls, texts: list[str]) -> list[str]:
        if any(not text.strip() for text in texts):
            raise ValueError("texts must not contain empty strings")
        return texts


class RerankRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")
    query: str = Field(min_length=1, max_length=32_000)
    documents: list[str] = Field(max_length=MAX_BATCH_SIZE)
    top_k: int | None = Field(default=None, ge=1, le=MAX_BATCH_SIZE)
    min_score: float | None = None

    @field_validator("query")
    @classmethod
    def require_nonblank_query(cls, query: str) -> str:
        if not query.strip():
            raise ValueError("query must not be empty")
        return query

    @field_validator("documents")
    @classmethod
    def require_nonblank_documents(cls, documents: list[str]) -> list[str]:
        if any(not document.strip() for document in documents):
            raise ValueError("documents must not contain empty strings")
        return documents

    @field_validator("min_score")
    @classmethod
    def require_finite_score(cls, min_score: float | None) -> float | None:
        if min_score is not None and not math.isfinite(min_score):
            raise ValueError("min_score must be finite")
        return min_score


async def run_gpu(operation: Callable[[], Any]) -> Any:
    """Serialize GPU work and keep synchronous model inference off the event loop."""
    async with GPU_SEMAPHORE:
        return await asyncio.to_thread(operation)


@app.get("/health")
async def health() -> JSONResponse:
    # Reports the embedding worker too: without it /embed fails every call,
    # and a health check that still says "ok" would keep the container in
    # service and hide that.
    coalescer = getattr(app.state, "embed_coalescer", None)
    alive = coalescer is not None and coalescer.alive()
    body: dict[str, Any] = {
        "status": "ok" if alive else "degraded",
        "model": EMBEDDING_MODEL,
        "dimension": app.state.embedding_dimension,
        "reranker_model": RERANKER_MODEL,
        "embed_worker": "running" if alive else "stopped",
    }
    return JSONResponse(status_code=200 if alive else 503, content=body)


@app.post("/embed")
async def embed(request: EmbedRequest) -> dict[str, Any]:
    # Through the coalescer rather than straight to the GPU: a single query
    # embedding costs about as much as thirty-two, so concurrent callers are
    # merged into one encode call (see EmbedCoalescer).
    vectors = await app.state.embed_coalescer.embed(request.texts)
    return {
        "model": EMBEDDING_MODEL,
        "dimension": app.state.embedding_dimension,
        "embeddings": vectors,
    }


@app.post("/rerank")
async def rerank(request: RerankRequest) -> dict[str, Any]:
    model = app.state.reranker_model
    if not request.documents:
        results: list[dict[str, Any]] = []
    else:
        pairs = [(request.query, document) for document in request.documents]
        raw_scores = await run_gpu(
            lambda: model.predict(pairs, batch_size=RERANKER_BATCH_SIZE)
        )
        scores = raw_scores.tolist() if hasattr(raw_scores, "tolist") else list(raw_scores)
        ranked = sorted(
            (
                {"index": index, "score": float(score), "text": document}
                for index, (score, document) in enumerate(zip(scores, request.documents))
            ),
            key=lambda result: result["score"],
            reverse=True,
        )
        if request.min_score is not None:
            ranked = [item for item in ranked if item["score"] >= request.min_score]
        results = ranked[: request.top_k] if request.top_k is not None else ranked
    return {"model": RERANKER_MODEL, "results": results}
