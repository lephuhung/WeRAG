# Streaming Chat API (SSE)

Frontend module: `frontend-next/lib/api/stream.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

All chat streaming endpoints use **Server-Sent Events** (`Accept: text/event-stream`). They bypass the `apiGet/apiPost` wrapper and use raw `fetch` so the response body can be read incrementally. Frames arrive as `data: <json>\n\n`; a literal `data: [DONE]` terminates the stream.

## Endpoints

| function | method + path | notes |
|---|---|---|
| `streamChat` | `POST /api/v1/knowledge-chat/:sessionId` | plain RAG pipeline |
| `streamChat` | `POST /api/v1/agent-chat/:sessionId` | agent pipeline — selected automatically when `agentId` is set and is not `builtin-quick-answer` |
| `streamNewAgentSession` | `POST /api/v1/agent-chat/new-session` | server mints the session; watch `session_id` / `data.session_id` on chunks |
| `continueStream` | `GET /api/v1/sessions/continue-stream/:sessionId?message_id=<assistantMessageId>` | attach to an in-flight reply after page refresh; **404** means the run already finished — fall back to polling messages |
| `streamEmbedChat` | `POST /api/v1/embed/:channelId/agent-chat/:sessionId` | embed visitor, agent pipeline |
| `streamEmbedChat` | `POST /api/v1/embed/:channelId/knowledge-chat/:sessionId` | embed visitor, RAG pipeline |

## Authentication

- **Web** streams send `Authorization: Bearer <token>` + `X-Tenant-ID` (from `weknora_selected_tenant_id`). On **401** the client refreshes once via `POST /api/v1/auth/refresh` (`{ refreshToken }` → `{ access_token }`) and re-opens the stream.
- **Embed** streams send `Authorization: Embed <embedToken>` plus optional `X-Embed-Session` (session signature) and `X-Embed-Visitor`. They **never** send `Bearer` or `X-Tenant-ID`, and there is no 401 refresh retry.
- Every request sends a random `X-Request-ID`.

## Request body (`buildChatBody`)

| field | type | notes |
|---|---|---|
| `query` | string | user message text |
| `agent_enabled` | boolean | defaults to `Boolean(agentId)` |
| `agent_id` | string | only when a non-builtin agent is selected |
| `agent_source_tenant_id` | number \| string | tenant that owns a shared agent |
| `knowledge_base_ids` | string[] | KB scope override |
| `knowledge_ids` | string[] | restrict to specific documents |
| `tag_ids` | string[] | tag scope |
| `mcp_service_ids` | string[] | **agent pipeline only** |
| `skill_names` | string[] | **agent pipeline only** |
| `mentioned_items` | MentionedItem[] | `@`-mentions; see type below |
| `web_search_enabled` | boolean | per-request web search toggle |
| `local_browser_enabled` | boolean | local browser tool toggle |
| `summary_model_id` | string | model used for title/summary generation |
| `attachment_ids` | string[] | previously uploaded attachments |
| `attachment_uploads` | StreamAttachmentUpload[] | inline uploads: `{ data, file_name, file_size }` |
| `images` | `{ data: string }[]` | base64 images |
| `suggestion_attribution` | object | `{ suggestion_set_id, question_id }` |
| `question_origin` | object | `{ knowledge_base_id, knowledge_id? }` |
| `channel` | string | `"web"` or `"embed"` — set automatically |

`MentionedItem`: `{ id, name, type, kb_type?, kb_id?, kb_name?, service_id?, skill_name? }`

## Stream chunks (`StreamChunk`)

Each `data:` frame is a JSON object:

| field | type | notes |
|---|---|---|
| `id` | string | chunk id |
| `response_type` / `type` | string | chunk kind (answer delta, tool event, ...) |
| `content` | string | text delta — append to render the answer |
| `done` | boolean | final chunk flag |
| `data` | object | metadata: `{ title?, session_id?, query?, request_id? }` |
| `session_id` | string | present on `new-session` flows |
| `assistant_message_id` | string | id of the assistant message being produced |
| `knowledge_references` | object[] | `{ knowledge_title?, knowledge_id?, chunk_id? }` citations |

## Error handling

- Non-2xx responses throw `Error` with `.status` attached (`401` → `"unauthorized"`).
- Malformed/partial `data:` lines are skipped (keep-alive comments, split frames).
- Abort via `params.signal` (`AbortSignal`).
