# frontend-next API Reference

Request/response documentation for every module in `frontend-next/lib/api/`, generated from the
backend Go router + handlers (`internal/router/`, `internal/handler/`) and `docs/swagger.json`.
Use these docs to map backend payloads onto frontend types correctly.

The older `docs/api/` tree documents the external API-key surface in Chinese; this tree documents
what `frontend-next` actually calls, in English.

## Conventions

### Base URL

All endpoints are under `/api/v1`. Paths inside each module doc are written as the backend route
(`:param` style); frontend template variables like `${id}` map onto them positionally.

### Authentication

| Mechanism | Header | Used by |
|---|---|---|
| JWT Bearer | `Authorization: Bearer <token>` | all authenticated web calls |
| Tenant scope | `X-Tenant-ID: <tenant-id>` | authenticated calls, when a tenant is selected |
| Embed token | `Authorization: Embed <token>` | `/api/v1/embed/*` visitor endpoints (anonymous) |
| Embed session | `X-Embed-Session`, `X-Embed-Visitor` | embed endpoints that carry a signed visitor session |

Tokens live in `localStorage`:

- `weknora_token` — access token
- `weknora_refresh_token` — refresh token
- `weknora_selected_tenant_id` — current tenant id

On `401`, `api-client.ts` performs a single shared refresh:
`POST /api/v1/auth/refresh` with `{ refreshToken }` → `{ access_token, refresh_token? }`, then
retries the original request once. Embed calls never trigger refresh.

Public (unauthenticated) paths: `/auth/login`, `/auth/register`, `/auth/register-by-invite`,
`/auth/invitations/lookup`, `/auth/refresh`, `/auth/auto-setup`, `/auth/config`,
`/auth/oidc/*`, and everything under `/embed/`.

### Response envelope

Most endpoints return the standard envelope:

```json
{ "success": true, "data": { /* payload */ } }
```

Errors:

```json
{
  "success": false,
  "error": { "code": "...", "message": "...", "details": "..." }
}
```

Notes:

- `api-client.ts` throws `ApiError` with `.status` and the raw `.payload`.
- Object responses get a non-enumerable `$httpStatus` property.
- Some handlers return `data` directly as the payload type; a few return extra top-level keys
  next to `success`/`data` — those are shown verbatim in each endpoint's **Response** block.
- `204 No Content` endpoints return no body — do not attempt `res.json()`.

### Special transports

| Kind | How it appears in docs | Frontend helper |
|---|---|---|
| SSE stream | `Response: streaming (SSE)` | raw `fetch` — see [stream.md](stream.md) |
| File download | `Response: binary download` | `apiDownload` → `Blob` |
| Multipart upload | **Multipart form** table | `apiUpload` (XHR, progress events) |

### Route notes

- `/tenants/kv/:key` is a **dispatcher**: the concrete key (`web-search-config`,
  `prompt-templates`, `parser-engine-config`, `storage-engine-config`, `chat-history-config`,
  `retrieval-config`, `memory-config`) selects a dedicated sub-handler. Each key is documented
  separately.

## Module docs

| Domain | Module | Endpoints |
|---|---|---|
| [Authentication](auth.md) | `auth.ts` | login, register, OIDC, refresh, preferences |
| [Tenants & Workspace Config](tenants.md) | `tenants.ts` | tenant CRUD, members, KV config dispatch |
| [Organizations & Sharing](organizations.md) | `organizations.ts` | orgs, spaces, agent/KB shares |
| [Initialization](initialization.md) | `initialization.ts` | bootstrap, remote-init, system status |
| [Knowledge](knowledge.md) | `knowledge.ts` | knowledge bases, documents, chunks, tags, FAQ, search |
| [Wiki](wiki.md) | `wiki.ts` | wiki pages, folders, links |
| [Chat & Sessions](chat.md) | `chat.ts` | sessions, messages, titles, forks |
| [Streaming (SSE)](stream.md) | `stream.ts` | chat SSE endpoints, chunk format |
| [Agents](agents.md) | `agents.ts` | custom agents, presets, IM channels |
| [Memory](memory.md) | `memory.ts` | memory settings, items, topics, export |
| [MCP Services](mcp.md) | `mcp.ts` | MCP servers, OAuth, tools |
| [Skills](skills.md) | `skills.ts` | installed skills |
| [Embed Channels](embed.md) | `embed.ts` | anonymous embed visitor API |
| [Models](models.md) | `models.ts` | model CRUD, debug |
| [System](system.md) | `system.ts` | settings, capabilities, engines, logs |
| [Audit](audit.md) | `audit.ts` | audit log query |
| [Environment Variables](env-vars.md) | `env-vars.ts` | env var management |
| [Storage Backends](storage-backends.md) | `storage-backends.ts` | storage engine configs |
| [Vector Stores](vector-stores.md) | `vector-stores.ts` | vector store configs |
| [Web Search](web-search.md) | `web-search.ts` | web search providers |
| [Remote Datasources](datasource.md) | `datasource.ts` | remote datasource sync |
| [Attachments](attachments.md) | `attachments.ts` | chat attachment upload/download |
| [Local Browser](browser.md) | `browser.ts` | browser extension pairing/status |
| [User Favorites](user-favorites.md) | `user-favorites.ts` | pinned agents/KBs |
| [Miscellaneous](extra.md) | `extra.ts` | uncategorized calls |
