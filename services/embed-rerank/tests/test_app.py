import asyncio
import sys
import threading
import time
from contextlib import contextmanager
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import app


class FakeEmbeddingModel:
    def get_embedding_dimension(self):
        return 3

    def encode(self, texts, **kwargs):
        if isinstance(texts, str):
            texts = [texts]
        return Array([[float(i), 0.0, 1.0] for i, _ in enumerate(texts)])


class FakeRerankerModel:
    def predict(self, pairs, **kwargs):
        return Array([float(len(doc)) for _, doc in pairs])


class Array:
    def __init__(self, value):
        self.value = value

    def tolist(self):
        return self.value


@contextmanager
def client(monkeypatch):
    monkeypatch.setattr(app, "load_embedding_model", lambda: FakeEmbeddingModel())
    monkeypatch.setattr(app, "load_reranker_model", lambda: FakeRerankerModel())
    with TestClient(app.app) as test_client:
        yield test_client


def test_health_reports_loaded_models_and_dimension(monkeypatch):
    with client(monkeypatch) as http:
        response = http.get("/health")

    assert response.status_code == 200
    assert response.json() == {
        "status": "ok",
        "model": app.DEFAULT_EMBEDDING_MODEL,
        "dimension": 3,
        "reranker_model": app.DEFAULT_RERANKER_MODEL,
        # Reported so a stopped embedding worker cannot hide behind "ok".
        "embed_worker": "running",
    }


def test_embed_returns_one_vector_per_nonempty_text(monkeypatch):
    with client(monkeypatch) as http:
        response = http.post("/embed", json={"texts": ["first", "second"]})

    assert response.status_code == 200
    assert response.json()["dimension"] == 3
    assert len(response.json()["embeddings"]) == 2
    assert response.json()["embeddings"][0] == [0.0, 0.0, 1.0]


def test_embed_empty_batch_returns_empty_vectors_without_model_call(monkeypatch):
    with client(monkeypatch) as http:
        response = http.post("/embed", json={"texts": []})

    assert response.status_code == 200
    assert response.json()["embeddings"] == []


def test_rerank_sorts_filters_and_limits_results(monkeypatch):
    with client(monkeypatch) as http:
        response = http.post(
            "/rerank",
            json={
                "query": "question",
                "documents": ["a", "longest", "long"],
                "min_score": 4,
                "top_k": 1,
            },
        )

    assert response.status_code == 200
    assert response.json()["results"] == [{"index": 1, "score": 7.0, "text": "longest"}]


def test_rerank_empty_documents_returns_empty_results_without_model_call(monkeypatch):
    with client(monkeypatch) as http:
        response = http.post("/rerank", json={"query": "question", "documents": []})

    assert response.status_code == 200
    assert response.json()["results"] == []


@pytest.mark.parametrize(
    "payload",
    [
        {"texts": ["x"] * 513},
        {"texts": [""]},
    ],
)
def test_embed_rejects_invalid_or_oversized_batches(monkeypatch, payload):
    with client(monkeypatch) as http:
        response = http.post("/embed", json=payload)

    assert response.status_code == 422


@pytest.mark.parametrize(
    "payload",
    [
        {"query": "q", "documents": ["x"], "top_k": 0},
        {"query": "q", "documents": ["x"], "min_score": "not-a-number"},
        {"query": "q", "documents": ["x"] * 513},
        {"query": "q"},
    ],
)
def test_rerank_rejects_invalid_or_oversized_requests(monkeypatch, payload):
    with client(monkeypatch) as http:
        response = http.post("/rerank", json=payload)

    assert response.status_code == 422


def test_rerank_rejects_non_finite_min_score(monkeypatch):
    with client(monkeypatch) as http:
        response = http.post(
            "/rerank",
            content='{"query":"q","documents":["x"],"min_score":NaN}',
            headers={"content-type": "application/json"},
        )

    assert response.status_code == 422


def test_startup_failure_does_not_report_ready(monkeypatch):
    def fail_load():
        raise RuntimeError("model cache is empty")

    monkeypatch.setattr(app, "load_embedding_model", fail_load)
    monkeypatch.setattr(app, "load_reranker_model", lambda: FakeRerankerModel())

    with pytest.raises(RuntimeError, match="model cache is empty"):
        with TestClient(app.app):
            pass


def test_gpu_work_obeys_shared_concurrency_limit(monkeypatch):
    async def run():
        monkeypatch.setattr(app, "GPU_SEMAPHORE", asyncio.Semaphore(1))
        active = 0
        peak = 0
        lock = threading.Lock()

        def work():
            nonlocal active, peak
            with lock:
                active += 1
                peak = max(peak, active)
            time.sleep(0.02)
            with lock:
                active -= 1

        await asyncio.gather(*(app.run_gpu(work) for _ in range(8)))
        return peak

    assert asyncio.run(run()) == 1


class CountingEmbeddingModel(FakeEmbeddingModel):
    """Records the size of every encode call so coalescing is observable."""

    def __init__(self, gate=None):
        self.batch_sizes = []
        self.gate = gate

    def encode(self, texts, **kwargs):
        if isinstance(texts, str):
            texts = [texts]
        self.batch_sizes.append(len(texts))
        if self.gate is not None:
            self.gate.wait(timeout=5)
        # One distinct vector per text so a mis-sliced batch cannot pass.
        return Array([[float(hash(t) % 1000), 0.0, 1.0] for t in texts])


def test_embed_coalescer_slices_each_callers_vectors_back(monkeypatch):
    """The whole point of coalescing: one encode call, each caller its own rows."""
    model = CountingEmbeddingModel()
    coalescer = app.EmbedCoalescer(
        lambda texts: model.encode(texts).tolist(), app.EMBED_COALESCE_MAX
    )

    async def scenario():
        coalescer.start()
        try:
            # Block the worker inside the first encode so the rest queue up
            # behind it, which is exactly the shape real concurrency takes.
            first = asyncio.create_task(coalescer.embed(["a"]))
            await asyncio.sleep(0.05)
            rest = [
                asyncio.create_task(coalescer.embed(["b", "c"])),
                asyncio.create_task(coalescer.embed(["d"])),
            ]
            return await asyncio.gather(first, *rest)
        finally:
            await coalescer.stop()

    got = asyncio.run(scenario())

    assert [len(v) for v in got] == [1, 2, 1], "every caller gets exactly its own texts back"
    expected = {
        "a": [float(hash("a") % 1000), 0.0, 1.0],
        "b": [float(hash("b") % 1000), 0.0, 1.0],
        "c": [float(hash("c") % 1000), 0.0, 1.0],
        "d": [float(hash("d") % 1000), 0.0, 1.0],
    }
    assert got[0][0] == expected["a"]
    assert got[1] == [expected["b"], expected["c"]]
    assert got[2][0] == expected["d"]
    # The two that queued behind the first went through together.
    assert model.batch_sizes[0] == 1
    assert sum(model.batch_sizes) == 4
    assert len(model.batch_sizes) < 4, f"expected coalescing, got {model.batch_sizes}"


def test_embed_coalescer_runs_a_lone_request_without_waiting(monkeypatch):
    """A single caller must not pay a coalescing delay."""
    model = CountingEmbeddingModel()
    coalescer = app.EmbedCoalescer(
        lambda texts: model.encode(texts).tolist(), app.EMBED_COALESCE_MAX
    )

    async def scenario():
        coalescer.start()
        try:
            started = time.monotonic()
            vectors = await coalescer.embed(["only"])
            return vectors, time.monotonic() - started
        finally:
            await coalescer.stop()

    vectors, elapsed = asyncio.run(scenario())

    assert len(vectors) == 1
    assert elapsed < 0.2, f"a lone request waited {elapsed:.3f}s"
    assert model.batch_sizes == [1]


def test_embed_coalescer_reports_a_failed_batch_to_every_caller(monkeypatch):
    """A batch is shared, so its failure must reach all of its callers."""

    def explode(texts):
        raise RuntimeError("cuda out of memory")

    coalescer = app.EmbedCoalescer(explode, app.EMBED_COALESCE_MAX)

    async def scenario():
        coalescer.start()
        try:
            results = await asyncio.gather(
                coalescer.embed(["a"]),
                coalescer.embed(["b"]),
                return_exceptions=True,
            )
            return results
        finally:
            await coalescer.stop()

    results = asyncio.run(scenario())

    assert all(isinstance(r, RuntimeError) for r in results), results
    assert all("cuda out of memory" in str(r) for r in results)


def test_embed_coalescer_rejects_a_mismatched_vector_count(monkeypatch):
    """Silently mis-slicing would hand callers someone else's vectors."""
    coalescer = app.EmbedCoalescer(lambda texts: [[0.0, 0.0, 1.0]], app.EMBED_COALESCE_MAX)

    async def scenario():
        coalescer.start()
        try:
            return await asyncio.gather(
                coalescer.embed(["a", "b"]), return_exceptions=True
            )
        finally:
            await coalescer.stop()

    (result,) = asyncio.run(scenario())

    assert isinstance(result, RuntimeError)
    assert "1 vectors for 2 texts" in str(result)


def test_embed_coalescer_empty_texts_skip_the_model(monkeypatch):
    model = CountingEmbeddingModel()
    coalescer = app.EmbedCoalescer(
        lambda texts: model.encode(texts).tolist(), app.EMBED_COALESCE_MAX
    )

    async def scenario():
        coalescer.start()
        try:
            return await coalescer.embed([])
        finally:
            await coalescer.stop()

    assert asyncio.run(scenario()) == []
    assert model.batch_sizes == []


def test_embed_coalescer_honours_the_text_cap(monkeypatch):
    """The cap bounds one GPU call; the leftovers go in the next batch."""
    model = CountingEmbeddingModel()
    coalescer = app.EmbedCoalescer(lambda texts: model.encode(texts).tolist(), 3)

    async def scenario():
        coalescer.start()
        try:
            first = asyncio.create_task(coalescer.embed(["blocker"]))
            await asyncio.sleep(0.05)
            queued = [asyncio.create_task(coalescer.embed(["x", "y"])) for _ in range(3)]
            return await asyncio.gather(first, *queued)
        finally:
            await coalescer.stop()

    got = asyncio.run(scenario())

    assert [len(v) for v in got] == [1, 2, 2, 2]
    assert max(model.batch_sizes) <= 4, f"a batch exceeded the cap: {model.batch_sizes}"


def test_embed_coalescer_keeps_a_small_request_out_of_a_large_batch(monkeypatch):
    """A query must not wait for an ingest batch that arrived after it.

    Before this, _drain merged everything queued, so a single query landing
    just ahead of a 512-text batch paid for the whole batch.
    """
    model = CountingEmbeddingModel()
    coalescer = app.EmbedCoalescer(
        lambda texts: model.encode(texts).tolist(), max_texts=1024, small=8
    )

    async def scenario():
        coalescer.start()
        try:
            blocker = asyncio.create_task(coalescer.embed(["blocker"]))
            await asyncio.sleep(0.05)
            query = asyncio.create_task(coalescer.embed(["one query"]))
            await asyncio.sleep(0.01)
            ingest = asyncio.create_task(coalescer.embed([f"doc {i}" for i in range(64)]))
            return await asyncio.gather(blocker, query, ingest)
        finally:
            await coalescer.stop()

    got = asyncio.run(scenario())

    assert [len(v) for v in got] == [1, 1, 64]
    # The query went through on its own; the ingest batch followed.
    assert model.batch_sizes == [1, 1, 64], model.batch_sizes


def test_embed_coalescer_runs_a_large_request_alone(monkeypatch):
    """A large request is already an efficient batch; nothing is added to it."""
    model = CountingEmbeddingModel()
    coalescer = app.EmbedCoalescer(
        lambda texts: model.encode(texts).tolist(), max_texts=1024, small=4
    )

    async def scenario():
        coalescer.start()
        try:
            blocker = asyncio.create_task(coalescer.embed(["blocker"]))
            await asyncio.sleep(0.05)
            big = asyncio.create_task(coalescer.embed([f"d{i}" for i in range(10)]))
            await asyncio.sleep(0.01)
            small = asyncio.create_task(coalescer.embed(["after"]))
            return await asyncio.gather(blocker, big, small)
        finally:
            await coalescer.stop()

    got = asyncio.run(scenario())

    assert [len(v) for v in got] == [1, 10, 1]
    assert model.batch_sizes == [1, 10, 1], model.batch_sizes


def test_embed_refuses_to_queue_when_the_worker_is_not_running(monkeypatch):
    """Queueing onto a stopped worker would wait on a future nobody completes."""
    coalescer = app.EmbedCoalescer(lambda texts: [[0.0] for _ in texts], app.EMBED_COALESCE_MAX)

    async def scenario():
        with pytest.raises(RuntimeError, match="not running"):
            await coalescer.embed(["never started"])
        coalescer.start()
        assert coalescer.alive()
        await coalescer.stop()
        assert not coalescer.alive()
        with pytest.raises(RuntimeError, match="not running"):
            await coalescer.embed(["already stopped"])

    asyncio.run(scenario())


def test_embed_coalescer_reports_a_worker_crash_to_queued_callers(monkeypatch):
    """A BaseException ends the worker; nobody may be left waiting."""
    calls = {"n": 0}

    class WorkerKilled(BaseException):
        """Stands in for the BaseExceptions `except Exception` would miss."""

    def explode(texts):
        calls["n"] += 1
        raise WorkerKilled("worker killed")

    coalescer = app.EmbedCoalescer(explode, app.EMBED_COALESCE_MAX, small=1)
    _ = WorkerKilled

    async def scenario():
        coalescer.start()
        try:
            first = asyncio.create_task(coalescer.embed(["a"]))
            await asyncio.sleep(0.05)
            # Queued behind the crashing batch: the done-callback fails it.
            second = asyncio.create_task(coalescer.embed(["b"]))
            await asyncio.sleep(0.05)
            return await asyncio.gather(first, second, return_exceptions=True)
        finally:
            await coalescer.stop()

    results = asyncio.run(scenario())

    assert type(results[0]).__name__ == "WorkerKilled", results[0]
    assert isinstance(results[1], BaseException), results[1]
    assert calls["n"] == 1


def test_health_reports_degraded_once_the_embed_worker_stops(monkeypatch):
    with client(monkeypatch) as http:
        assert http.get("/health").status_code == 200
        assert http.get("/health").json()["embed_worker"] == "running"

        # Stand in a never-started coalescer for the running one: stopping the
        # real task from this thread would touch the app's event loop from
        # outside it, which is not what is under test here.
        app.app.state.embed_coalescer = app.EmbedCoalescer(
            lambda texts: [[0.0] for _ in texts], app.EMBED_COALESCE_MAX
        )

        response = http.get("/health")
        assert response.status_code == 503
        assert response.json()["status"] == "degraded"
        assert response.json()["embed_worker"] == "stopped"
