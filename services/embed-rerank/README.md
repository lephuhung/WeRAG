# WeRAG embedding and reranking service

This repository builds the GPU model host and its OpenAI-compatible adapter. It replaces the retired AIRAG application image for these two services; it does not change WeRAG's model-provider configuration or the model weights.

## Build and run

From the repository root, start both services with the optional profile:

```sh
docker compose --profile airag-models up -d --build embed-rerank airag-model-adapter
```

Compose builds `WERAG_EMBED_RERANK_IMAGE` (default `werag-embed-rerank:local`) from `services/embed-rerank/Dockerfile`. The CUDA/PyTorch runtime layers reuse `vllm/vllm-openai:v0.27.1`, already used by the WeRAG OCR service on this host. This service does not run the vLLM server or call its API. The model service gets the GPU; the adapter exposes host port 8090 by default.

The model host requires an NVIDIA Container Toolkit runtime. Models default to `mainguyen9/vietlegal-harrier-0.6b` and `BAAI/bge-reranker-v2-m3`. Set `EMBEDDING_MODEL`, `RERANKER_MODEL`, `EMBED_RERANK_GPU`, `EMBED_RERANK_CONCURRENCY`, and `HF_CACHE_DIR` in `.env` to override the defaults. `HF_HUB_OFFLINE=1` is the default; download both model snapshots into the shared Hugging Face cache before starting an offline deployment.

The internal model host is available as `http://hrag-embed-rerank:8090` on the Compose network. The adapter is available as `http://hrag-openai-adapter:8090` to other containers and `http://localhost:8090` on the host. Configure WeRAG embedding/rerank providers to use the adapter, with `/v1` optional. Set `AIRAG_ADAPTER_API_KEY` to require a Bearer token.

## API

- `GET /health` — model names and embedding dimension.
- `POST /embed` — `{ "texts": ["..."] }`.
- `POST /rerank` — `{ "query": "...", "documents": ["..."], "top_k": 10, "min_score": 0.3 }`.
- Adapter endpoints: `POST /v1/embeddings`, `POST /v1/rerank`, and their non-versioned equivalents.

## Tests

Run mocked API tests without model downloads or GPU access:

```sh
python3 -m pip install pytest fastapi httpx httpx2
python3 -m pytest services/embed-rerank/tests -q
```

Build-only validation:

```sh
docker build -f services/embed-rerank/Dockerfile -t werag-embed-rerank:test .
docker compose --profile airag-models config --quiet
```
