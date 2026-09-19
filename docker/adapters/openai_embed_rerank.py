"""OpenAI-compatible adapter for AIRAG's embed-rerank service.

AIRAG's embed-rerank (:8090) exposes a custom API:
    POST /embed  {texts: [...]}            -> {embeddings: [[f32]]}
    POST /rerank {query, documents, top_k} -> {results: [{index, score, text}]}

WeRAG (WeKnora) model config expects OpenAI-style endpoints:
    POST /v1/embeddings  {model, input}         -> {data: [{embedding, index}]}
    POST /v1/rerank      {model, query, docs}   -> {results: [{index,
                       document: {text}, relevance_score}]}   (vLLM/Jina shape)

This single-file service bridges the two. Run it on the same docker network
as hrag-embed-rerank; WeRAG then points its embedding + rerank model base
URLs at this adapter.

Env:
    UPSTREAM_URL   — default http://hrag-embed-rerank:8090
    ADAPTER_API_KEY — optional; when set, require `Authorization: Bearer <key>`
"""

import os
import time
import uuid

import httpx
from fastapi import Depends, FastAPI, HTTPException, Request
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer

UPSTREAM = os.environ.get("UPSTREAM_URL", "http://hrag-embed-rerank:8090").rstrip("/")
ADAPTER_API_KEY = os.environ.get("ADAPTER_API_KEY", "").strip()
TIMEOUT = float(os.environ.get("ADAPTER_TIMEOUT", "120"))

app = FastAPI(title="embed-rerank OpenAI adapter")
_bearer = HTTPBearer(auto_error=False)
_client: httpx.AsyncClient | None = None


@app.on_event("startup")
async def _startup():
    global _client
    _client = httpx.AsyncClient(base_url=UPSTREAM, timeout=TIMEOUT)


@app.on_event("shutdown")
async def _shutdown():
    if _client:
        await _client.aclose()


def _auth(creds: HTTPAuthorizationCredentials = Depends(_bearer)):
    if ADAPTER_API_KEY and (creds is None or creds.credentials != ADAPTER_API_KEY):
        raise HTTPException(status_code=401, detail="invalid api key")


@app.get("/health")
async def health():
    try:
        r = await _client.get("/health")
        return {"status": "ok", "upstream": r.json()}
    except Exception as e:
        return {"status": "degraded", "upstream_error": str(e)}


@app.get("/v1/models")
async def models():
    r = await _client.get("/health")
    info = r.json()
    now = int(time.time())
    return {
        "object": "list",
        "data": [
            {"id": info["model"], "object": "model", "created": now,
             "owned_by": "embed-rerank"},
            {"id": info["reranker_model"], "object": "model", "created": now,
             "owned_by": "embed-rerank"},
        ],
    }


async def _embeddings(request: Request, _=Depends(_auth)):
    body = await request.json()
    inputs = body.get("input") or []
    if isinstance(inputs, str):
        inputs = [inputs]
    if not inputs:
        raise HTTPException(status_code=400, detail="empty input")

    r = await _client.post("/embed", json={"texts": inputs})
    if r.status_code != 200:
        raise HTTPException(status_code=r.status_code, detail=r.text[:500])
    up = r.json()

    return {
        "object": "list",
        "data": [
            {"object": "embedding", "index": i, "embedding": vec}
            for i, vec in enumerate(up["embeddings"])
        ],
        "model": body.get("model") or up.get("model"),
        "usage": {"prompt_tokens": 0, "total_tokens": 0},
    }


async def _rerank(request: Request, _=Depends(_auth)):
    body = await request.json()
    query = body.get("query", "")
    documents = body.get("documents") or []
    if not documents:
        raise HTTPException(status_code=400, detail="empty documents")
    top_n = body.get("top_n") or body.get("top_k")

    r = await _client.post(
        "/rerank",
        json={"query": query, "documents": documents, "top_k": top_n},
    )
    if r.status_code != 200:
        raise HTTPException(status_code=r.status_code, detail=r.text[:500])
    up = r.json()

    # AIRAG: {results: [{index, score, text}]} ->
    # vLLM/Jina: {results: [{index, relevance_score, document: {text}}]}
    return {
        "id": f"rerank-{uuid.uuid4().hex[:24]}",
        "model": body.get("model") or up.get("model"),
        "usage": {"total_tokens": 0},
        "results": [
            {
                "index": item["index"],
                "relevance_score": item["score"],
                "document": {"text": item.get("text", "")},
            }
            for item in up["results"]
        ],
    }


# Register under both /v1/* and bare paths so the adapter works whether the
# configured base URL ends in /v1 or not.
for prefix in ("/v1", ""):
    app.post(f"{prefix}/embeddings", dependencies=[Depends(_auth)])(_embeddings)
    app.post(f"{prefix}/rerank", dependencies=[Depends(_auth)])(_rerank)
