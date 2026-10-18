# Native WeRAG Embed/Rerank Service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the local GPU embedding/reranking service and OpenAI adapter from this repository so the Compose deployment no longer needs `airag-backend`.

**Architecture:** Add an isolated FastAPI inference package and CUDA-capable Python image, loading SentenceTransformer and CrossEncoder directly. Package the existing adapter in the same image, then point the two existing Compose services at that image while retaining their service names, endpoints, model cache and GPU settings.

**Tech Stack:** Python 3.12 (the pinned local CUDA/PyTorch runtime), FastAPI/Pydantic, sentence-transformers, PyTorch CUDA, Docker Compose, pytest.

**Spec:** [docs/superpowers/specs/2026-10-18-native-embed-rerank-service-design.md](../specs/2026-10-18-native-embed-rerank-service-design.md)

## Global Constraints

- Preserve defaults `mainguyen9/vietlegal-harrier-0.6b` and `BAAI/bge-reranker-v2-m3`.
- Preserve `GET /health`, `POST /embed`, and `POST /rerank` response contracts and the adapter's OpenAI-compatible routes/auth behavior.
- Keep one shared GPU semaphore, controlled by `EMBED_RERANK_CONCURRENCY` (default `1`); execute inference off the event loop.
- Keep the existing `airag-models` opt-in profile, service/container names, GPU reservation, cache mount, DNS, port and restart behavior.
- Do not modify unrelated pre-existing working-tree changes, stop containers, or remove existing Docker images as part of this implementation.

## Review Focus

- Empty text batches and empty document lists should return valid empty results without invoking the model; pin in Task 1 service tests.
- `top_k` below 1, non-finite `min_score`, malformed payloads, and oversized batch handling should return controlled 4xx errors rather than tracebacks or GPU work; pin validation in Task 1.
- Offline cache misses and model load failures should fail startup clearly and never report ready; pin initialization behavior in Task 1 with mocked loaders.
- Multiple concurrent embed/rerank requests should never exceed the configured shared GPU concurrency; pin with a blocking fake model in Task 1.
- CPU-only test runs must not import or initialize CUDA/model weights; pin in Task 1 and build the image separately in Task 3.

---

### Task 1: Add a tested inference service

**Files:**
- Create: `services/embed-rerank/app.py`
- Create: `services/embed-rerank/tests/test_app.py`
- Create: `services/embed-rerank/requirements.txt`

**Interfaces:**
- `GET /health` -> `{status, model, dimension, reranker_model}`.
- `POST /embed` input `{texts: list[str]}` -> `{model, dimension, embeddings: list[list[float]]}`.
- `POST /rerank` input `{query: str, documents: list[str], top_k?: int, min_score?: float}` -> `{model, results: [{index, score, text}]}`.
- Internal model factory functions load `SentenceTransformer` and `CrossEncoder` lazily during FastAPI lifespan; request handlers call those shared instances.

- [ ] Write mocked-model tests for defaults, health metadata, embeddings shape, rerank order/threshold/top-k, empty batches, validation errors, startup load failures, and the shared concurrency ceiling.
- [ ] Run `python -m pytest services/embed-rerank/tests/test_app.py -q` and confirm the tests fail because the service does not exist yet.
- [ ] Implement the service with lazy model loading, warmup before readiness, configurable model/device/cache/offline settings, a shared async semaphore, and `asyncio.to_thread` inference calls.
- [ ] Run `python -m pytest services/embed-rerank/tests/test_app.py -q` and confirm all tests pass without downloading models or requiring a GPU.

### Task 2: Package the model service and OpenAI adapter

**Files:**
- Create: `services/embed-rerank/Dockerfile`
- Modify: `docker/adapters/openai_embed_rerank.py` only if packaging requires a compatibility adjustment.
- Test: `services/embed-rerank/tests/test_app.py`

**Interfaces:**
- Image default command starts `app:app` on `0.0.0.0:8090`.
- Adapter remains available as `/adapter/openai_adapter.py` and receives `UPSTREAM_URL` and `ADAPTER_API_KEY` from Compose.

- [ ] Extend tests to verify the adapter forwards embeddings/rerank requests and preserves authentication/error handling using a mocked upstream.
- [ ] Run the targeted pytest suite and confirm adapter tests fail before implementation changes.
- [ ] Build one CUDA-capable Python image with the inference package and existing adapter source; support overriding its Uvicorn app command for the adapter.
- [ ] Run `docker build -f services/embed-rerank/Dockerfile -t werag-embed-rerank:test .` and targeted tests; confirm the image builds and tests pass.

### Task 3: Switch Compose services to the repo-built image

**Files:**
- Modify: `docker-compose.yml` embed-rerank and airag-model-adapter services.
- Modify: `.env.example` only if new image/model configuration variables need documenting.

**Interfaces:**
- Compose service names remain `embed-rerank` and `airag-model-adapter` with existing container names `hrag-embed-rerank` and `hrag-openai-adapter`.
- The service keeps port `8090`, `airag-models` profile, WeKnora network, GPU selection, cache mount, and existing `HRAG_*`/adapter environment values.
- Compose builds the shared image once for `embed-rerank`; `airag-model-adapter` uses that same image with a command override. Neither service uses `AIRAG_BACKEND_IMAGE`.

- [ ] Update only the two service definitions and comments; build the shared image from `embed-rerank` only to avoid concurrent builds racing on one image tag. Preserve all unrelated changes in `docker-compose.yml`.
- [ ] Run `docker compose --profile airag-models config --quiet` and inspect rendered service image/build, command, environment, GPU, mount and dependency values.
- [ ] Verify with `rg -n 'airag-backend|AIRAG_BACKEND_IMAGE' docker-compose.yml .env.example` that no active embed/rerank Compose reference remains.

### Task 4: Verify deployment behavior and handoff

**Files:**
- Modify: `services/embed-rerank/README.md` (create) for build, profile startup, cache and model configuration instructions.

- [ ] Document local test/build commands, required NVIDIA container runtime, `airag-models` profile, cache/offline settings and both service URLs.
- [ ] Run `python -m pytest services/embed-rerank/tests -q` and the Compose config check.
- [ ] Build the shared image and smoke-test `/health`, `/embed`, `/rerank`, `/v1/embeddings` and `/v1/rerank` using cached models when GPU access is available; otherwise record the unrun GPU checks explicitly.
- [ ] Confirm the running Compose definitions point to the repo-built image before suggesting removal of the old `airag-backend` image; do not stop or replace currently running containers within this task.
