import importlib.util
from pathlib import Path

from fastapi.testclient import TestClient


class FakeResponse:
    def __init__(self, body, status_code=200):
        self._body = body
        self.status_code = status_code
        self.text = "upstream failure"

    def json(self):
        return self._body


class FakeUpstream:
    def __init__(self):
        self.closed = False

    async def aclose(self):
        self.closed = True

    async def get(self, path):
        return FakeResponse(
            {"model": "vietlegal-harrier", "dimension": 3, "reranker_model": "bge-reranker"}
        )

    async def post(self, path, json):
        if path == "/embed":
            return FakeResponse({"model": "vietlegal-harrier", "embeddings": [[1.0, 0.0, 0.0]]})
        return FakeResponse(
            {
                "model": "bge-reranker",
                "results": [{"index": 0, "score": 0.9, "text": json["documents"][0]}],
            }
        )


def load_adapter(monkeypatch):
    monkeypatch.setenv("ADAPTER_API_KEY", "secret")
    path = Path(__file__).resolve().parents[3] / "docker/adapters/openai_embed_rerank.py"
    spec = importlib.util.spec_from_file_location("openai_adapter_test", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    module._client = FakeUpstream()
    return module


def test_adapter_protects_routes_and_converts_embeddings(monkeypatch):
    module = load_adapter(monkeypatch)
    http = TestClient(module.app)
    unauthorized = http.post("/v1/embeddings", json={"input": "hello"})
    response = http.post(
        "/v1/embeddings",
        json={"input": "hello"},
        headers={"Authorization": "Bearer secret"},
    )
    http.close()

    assert unauthorized.status_code == 401
    assert response.status_code == 200
    assert response.json()["data"] == [
        {"object": "embedding", "index": 0, "embedding": [1.0, 0.0, 0.0]}
    ]


def test_adapter_converts_rerank_results_to_vllm_shape(monkeypatch):
    module = load_adapter(monkeypatch)
    http = TestClient(module.app)
    response = http.post(
        "/v1/rerank",
        json={"query": "q", "documents": ["document"], "top_n": 1},
        headers={"Authorization": "Bearer secret"},
    )
    http.close()

    assert response.status_code == 200
    result = response.json()["results"][0]
    assert result["index"] == 0
    assert result["relevance_score"] == 0.9
    assert result["document"] == {"text": "document"}


def test_adapter_lifespan_opens_and_closes_upstream_client(monkeypatch):
    module = load_adapter(monkeypatch)
    upstream = FakeUpstream()
    monkeypatch.setattr(module, "_make_client", lambda: upstream)

    with TestClient(module.app):
        assert module._client is upstream
        assert upstream.closed is False

    assert upstream.closed is True
