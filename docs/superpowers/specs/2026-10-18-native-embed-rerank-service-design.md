# Native WeRAG Embed/Rerank Service

## Goal

Remove `airag-backend` as a runtime dependency for the GPU embedding and reranking services by building those services from this repository, without changing the model API currently consumed by WeRAG.

## Current state

- `docker-compose.yml` defines `embed-rerank` and `airag-model-adapter` under the `airag-models` profile. Both currently use `airag-backend:v2phase2`.
- The adapter source is already in `docker/adapters/openai_embed_rerank.py` and translates `/embed` and `/rerank` to OpenAI-compatible endpoints.
- The running Python service exposes `GET /health`, `POST /embed` and `POST /rerank`. It loads `SentenceTransformer` and `CrossEncoder` models through AIRAG's application services and limits combined GPU work with `EMBED_RERANK_CONCURRENCY` (default 1).
- The repo's Go backend contains embedding and rerank provider clients, but does not host these local GPU models. Therefore the GPU model host remains a separate service/image.

## Design

Add a dedicated Python service directory in this repository with a small FastAPI application and a dedicated Dockerfile. The service will own model loading and inference directly through `sentence-transformers`, rather than importing AIRAG application modules. This keeps model hosting isolated from the Go application while making its source and build reproducible from this repo.

Preserve the existing service contract:

- `GET /health` returns `status`, embedding model name, embedding dimension and reranker model name.
- `POST /embed` accepts `{ "texts": [...] }` and returns `{ "model", "dimension", "embeddings": [...] }`.
- `POST /rerank` accepts `{ "query", "documents", "top_k?", "min_score?" }` and returns `{ "model", "results": [{ "index", "score", "text" }] }`.
- Keep the existing defaults (`mainguyen9/vietlegal-harrier-0.6b` and `BAAI/bge-reranker-v2-m3`), `HRAG_EMBEDDING_MODEL`, `HRAG_RERANKER_MODEL`, GPU device configuration, Hugging Face cache mount/offline behavior, and a shared semaphore controlled by `EMBED_RERANK_CONCURRENCY`.
- Preserve the OpenAI adapter endpoints, auth behavior and upstream URL. Move its runtime into the repo-built image so neither Compose service requires the AIRAG image.

Compose will build a dedicated image from the new service directory (or a repository-root build context if needed for the adapter source), retain the existing service/container names, network, profile, GPU reservation, ports, restart policy and model-cache mount, and remove the `AIRAG_BACKEND_IMAGE` dependency. The service remains opt-in under `airag-models` unless deployment configuration explicitly changes that later.

## Error handling and compatibility

- Fail startup with a clear error if configured models cannot load; readiness must only succeed after both models load and warm up.
- Validate empty and malformed requests and reject invalid `top_k` values with normal 4xx responses.
- Keep response fields and ranking order compatible with the current clients and adapter.
- Never set `HRAG_EMBED_RERANK_URL` on the model host; add a guard against self-proxying if that setting is introduced.
- Run inference off the event loop and serialize GPU access through the shared semaphore.

## Testing and verification

- Unit-test request validation, response formatting, model-selection/config defaults, filtering/top-k behavior, and semaphore-protected calls with mocked models; tests must not download models or require a GPU.
- Add an image build check and Compose config validation for the `airag-models` profile.
- Where the host has a GPU and cached models, verify health, embedding dimension/vector count, rerank ordering, and that both containers start from the repo-built image. Otherwise report the GPU/model-load smoke test as unrun.
- Confirm no `airag-backend` image reference remains in the active Compose definitions for embed/rerank or adapter.

## Scope and deployment

This change ports the local embedding/rerank model host and its existing adapter. It does not alter WeRAG's Go model providers, model/tenant configuration, model choices, model weights, or unrelated AIRAG services. It does not automatically remove the old image or stop running containers; image cleanup and deployment replacement happen only after the repo-built image is validated.

## Open implementation detail

Choose a CUDA-capable Python base image and pinned or constrained dependency set compatible with the host NVIDIA runtime. Keep the image focused on inference rather than copying AIRAG's unrelated backend dependencies.
