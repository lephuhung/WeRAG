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


def load_embedding_model() -> Any:
    """Load the configured sentence-transformers model on its target device."""
    from sentence_transformers import SentenceTransformer

    device = None if EMBEDDING_DEVICE == "auto" else EMBEDDING_DEVICE
    return SentenceTransformer(EMBEDDING_MODEL, device=device)


def load_reranker_model() -> Any:
    """Load the configured cross-encoder model on its target device."""
    from sentence_transformers import CrossEncoder

    device = None if RERANKER_DEVICE == "auto" else RERANKER_DEVICE
    return CrossEncoder(RERANKER_MODEL, device=device)


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
        "Embed/rerank service ready (embedding=%s, dimension=%d, reranker=%s, concurrency=%d)",
        EMBEDDING_MODEL,
        application.state.embedding_dimension,
        RERANKER_MODEL,
        GPU_CONCURRENCY,
    )
    yield


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
async def health() -> dict[str, Any]:
    return {
        "status": "ok",
        "model": EMBEDDING_MODEL,
        "dimension": app.state.embedding_dimension,
        "reranker_model": RERANKER_MODEL,
    }


@app.post("/embed")
async def embed(request: EmbedRequest) -> dict[str, Any]:
    model = app.state.embedding_model
    if not request.texts:
        vectors: list[list[float]] = []
    else:
        encoded = await run_gpu(
            lambda: model.encode(
                request.texts,
                convert_to_numpy=True,
                normalize_embeddings=True,
                batch_size=EMBEDDING_BATCH_SIZE,
            )
        )
        vectors = encoded.tolist()
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
