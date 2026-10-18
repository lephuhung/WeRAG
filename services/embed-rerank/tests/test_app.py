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
