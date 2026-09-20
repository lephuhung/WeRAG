# Embed Channels (anonymous) API

Frontend module: `frontend-next/lib/api/embed.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `listEmbedChannels` — `GET /agents/:id/embed-channels`

handler: `ListEmbedChannels` · `embed_channel.go`

**Response**:
```json
{ success: true, data: embedChannelsResponse(rows) }
```
---
### `listAllEmbedChannels` — `GET /embed-channels`

handler: `ListAllEmbedChannels` · `embed_channel.go`

**Response**:
```json
{ success: true, data: embedChannelsResponse(rows) }
```
---
### `createEmbedChannel` — `POST /agents/:id/embed-channels`

handler: `CreateEmbedChannel` · `embed_channel.go`

**Body** `embedChannelRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string |  |  |
| `enabled` | boolean |  |  |
| `allowed_origins` | string[] |  |  |
| `welcome_message` | string |  |  |
| `rate_limit_per_minute` | number |  |  |
| `rate_limit_per_day` | number |  |  |
| `primary_color` | string |  |  |
| `page_title` | string |  |  |
| `header_title_mode` | string |  |  |
| `show_suggested_questions` | boolean |  |  |
| `widget_position` | string |  |  |
| `allow_web_search` | boolean |  |  |
| `allow_file_upload` | boolean |  |  |
| `default_locale` | string |  |  |
| `webhook_url` | string |  |  |
| `webhook_secret` | string |  |  |
| `agent_id` | string |  |  |

**Response**:
```json
{ success: true, data: embedChannelResponse(ch }
```
---
### `getEmbedChannel` — `GET /embed-channels/:channel_id`

handler: `GetEmbedChannel` · `embed_channel.go`

**Response**:
```json
{ success: true, data: embedChannelResponse(ch }
```
---
### `updateEmbedChannel` — `PUT /embed-channels/:channel_id`

handler: `UpdateEmbedChannel` · `embed_channel.go`

**Body** `embedChannelRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string |  |  |
| `enabled` | boolean |  |  |
| `allowed_origins` | string[] |  |  |
| `welcome_message` | string |  |  |
| `rate_limit_per_minute` | number |  |  |
| `rate_limit_per_day` | number |  |  |
| `primary_color` | string |  |  |
| `page_title` | string |  |  |
| `header_title_mode` | string |  |  |
| `show_suggested_questions` | boolean |  |  |
| `widget_position` | string |  |  |
| `allow_web_search` | boolean |  |  |
| `allow_file_upload` | boolean |  |  |
| `default_locale` | string |  |  |
| `webhook_url` | string |  |  |
| `webhook_secret` | string |  |  |
| `agent_id` | string |  |  |

**Response**:
```json
{ success: true, data: embedChannelResponse(ch }
```
---
### `deleteEmbedChannel` — `DELETE /embed-channels/:channel_id`

handler: `DeleteEmbedChannel` · `embed_channel.go`

**Response**:
```json
{ success: true }
```
---
### `rotateEmbedToken` — `POST /embed-channels/:channel_id/rotate-token`

handler: `RotateEmbedToken` · `embed_channel.go`

**Response**:
```json
{ success: true, data: embedChannelResponse(ch }
```
---
### `getEmbedChannelStats` — `GET /embed-channels/:channel_id/stats`

handler: `GetEmbedChannelStats` · `embed_channel.go`

**Response**:
```json
{ success: true, data: {session_count} }
```
---
### `issueEmbedPreviewSession` — `POST /embed-channels/:channel_id/preview-session`

handler: `IssuePreviewSession` · `embed_channel.go`

**Response**:
```json
{ success: true, data: {session_token, expires_in} }
```
---
### `getEmbedChunkById` — `GET /api/v1/embed/:channel_id/chunks/:chunk_id`

handler: `GetEmbedChunk` · `embed_channel.go`

**Response**:
```json
{ success: true, data: types.Chunk }
```
`data` is `Chunk`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier of the chunk, using UUID format |
| `seq_id` | number | yes | SeqID is an auto-increment integer ID for external API usage (FAQ entries) |
| `tenant_id` | number | yes | Tenant ID, used for multi-tenant isolation |
| `knowledge_id` | string | yes | ID of the parent knowledge, associated with the Knowledge model |
| `knowledge_base_id` | string | yes | ID of the knowledge base, for quick location |
| `tag_id` | string | yes | Optional tag ID for categorization within a knowledge base (used for FAQ) |
| `content` | string | yes | Actual text content of the chunk |
| `content_revision` | number | yes | ContentRevision is incremented for every user edit or rollback. |
| `index_status` | string | yes | IndexStatus reports whether the current content is reflected in the retrieval stores: ready | processing | failed. |
| `last_editor_id` | string | yes | LastEditorID records the actor that produced the current revision. |
| `chunk_index` | number | yes | Index position of the chunk in the original document |
| `is_enabled` | boolean | yes | Whether the chunk is enabled, can be used to temporarily disable certain chunks |
| `flags` | ChunkFlags | yes | Flags is a bitmask of boolean states (e.g. recommended). Default is ChunkFlagRecommended (1) = recommendable. |
| `status` | number | yes | Status of the chunk |
| `start_at` | number | yes | Starting character position in the original text |
| `end_at` | number | yes | Ending character position in the original text |
| `pre_chunk_id` | string | yes | Previous chunk ID |
| `next_chunk_id` | string | yes | Next chunk ID |
| `chunk_type` | ChunkType | yes | Chunk type, distinguishes different chunk kinds |
| `parent_chunk_id` | string | yes | parent chunk ID; links an image chunk to its source text chunk |
| `relation_chunks` | JSON | yes | relation chunk ID; links a relation chunk to its source text chunk |
| `indirect_relation_chunks` | JSON | yes | indirect-relation chunk ID; links an indirect relation chunk to its source text chunk |
| `metadata` | JSON | yes | Metadata stores chunk-level extra info, e.g. FAQ metadata |
| `content_hash` | string | yes | ContentHash stores the content hash for fast matching (mainly for FAQ) |
| `image_info` | string | yes | image info, stored as JSON |
| `created_at` | string(time) | yes | Chunk creation time |
| `updated_at` | string(time) | yes | Chunk last update time |
| `deleted_at` | DeletedAt | yes | Soft delete marker, supports data recovery |

---
### `getEmbedSuggestedQuestions` — `GET /api/v1/embed/:channel_id/suggested-questions`

handler: `GetEmbedSuggestedQuestions` · `embed_channel.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `limit` | string |  |  |

**Response**:
```json
{ success: true, data: {questions} }
```
---
### `ensureEmbedMessageSuggestions` — `POST /api/v1/embed/:channel_id/sessions/:session_id/messages/:message_id/suggestions`

handler: `EmbedEnsureMessageSuggestions` · `embed_channel.go`

**Response**:
```json
{ success: true, data: {status, suppression_reason, questions} }
```
---
### `getEmbedMessageSuggestions` — `GET /api/v1/embed/:channel_id/sessions/:session_id/messages/:message_id/suggestions`

handler: `EmbedGetMessageSuggestions` · `embed_channel.go`

**Response**:
```json
{ success: true, data: {status, suppression_reason, questions} }
```
---
### `recordEmbedMessageSuggestionEvent` — `POST /api/v1/embed/:channel_id/sessions/:session_id/suggestion-events`

handler: `EmbedRecordSuggestionEvent` · `embed_channel.go→RecordEvent`

**Body** `SuggestionEventRequest`:
| field | type | req | notes |
|---|---|---|---|
| `suggestion_set_id` | string | yes |  |
| `question_id` | string |  |  |
| `event_type` | string | yes |  |

**Response**: `204 No Content`
---
### `getEmbedConfig` — `GET /api/v1/embed/:channel_id/config`

handler: `GetEmbedConfig` · `embed_channel.go`

**Response**:
```json
{ success: true, data: h.embedSvc.PublicConfig(c.Request.Context() }
```
---
### `createEmbedSession` — `POST /api/v1/embed/:channel_id/sessions`

handler: `CreateEmbedSession` · `embed_channel.go`

**Response**:
```json
{ success: true, data: {id, sig} }
```
---
### `exchangeEmbedSession` — `POST /api/v1/embed/:channel_id/exchange`

handler: `ExchangeEmbedSession` · `embed_channel.go`

**Response**:
```json
{ success: true, data: {session_token, expires_in} }
```
---
### `stopEmbedSession` — `POST /api/v1/embed/:channel_id/sessions/:session_id/stop`

handler: `EmbedStopSession` · `embed_channel.go→StopSession`

**Body** `StopSessionRequest`:
| field | type | req | notes |
|---|---|---|---|
| `message_id` | string | yes |  |

**Response**:
```json
{ success: true, message: string }
```
---
### `resolveEmbedMCPOAuth` — `POST /api/v1/embed/:channel_id/sessions/:session_id/mcp-oauth-resolutions/:pending_id`

handler: `EmbedResolveMCPOAuth` · `embed_channel.go→ResolveMCPOAuth`

**Body** `resolveMCPOAuthBody`:
| field | type | req | notes |
|---|---|---|---|
| `service_id` | string | yes | ServiceID is the MCP service the pending prompt belongs to; used to verify the user actually holds a token before resuming the agent. |
| `decision` | string |  | Decision is "authorize" (default) or "cancel" when the user skips OAuth. |

**Response**:
```json
{ success: true }
```
---
### `cancelEmbedMCPOAuth` — `POST /api/v1/embed/:channel_id/sessions/:session_id/mcp-oauth-resolutions/:pending_id/cancel`

handler: `EmbedCancelMCPOAuth` · `embed_channel.go→CancelMCPOAuth`

**Response**:
```json
{ success: true }
```
---
### `getEmbedMCPOAuthAuthorizeURL` — `POST /api/v1/embed/:channel_id/sessions/:session_id/mcp-services/:id/oauth/authorize-url`

handler: `EmbedMCPOAuthAuthorizeURL` · `embed_channel.go→AuthorizeURL`

**Body** `mcpOAuthAuthorizeRequest`:
| field | type | req | notes |
|---|---|---|---|
| `redirect_uri` | string |  | RedirectURI is the absolute backend callback URL registered with the authorization server (e.g. https://host/api/v1/mcp-services/oauth/callback). |
| `frontend_redirect` | string |  | FrontendRedirect is where the callback bounces the browser when done (e.g. the MCP settings page). Optional; defaults to "/". |

**Response**:
```json
{ success: true, data: {authorization_url, authorization_attempt} }
```
---
### `getEmbedMCPOAuthStatus` — `GET /api/v1/embed/:channel_id/sessions/:session_id/mcp-services/:id/oauth/status`

handler: `EmbedMCPOAuthStatus` · `embed_channel.go→Status`

**Response** `WeKnoraCloudStatusResult`:
| field | type | req | notes |
|---|---|---|---|
| `has_models` | boolean | yes |  |
| `needs_reinit` | boolean | yes |  |
| `reason` | string |  |  |

---
### `resolveEmbedToolApproval` — `POST /api/v1/embed/:channel_id/sessions/:session_id/tool-approvals/:pending_id`

handler: `EmbedResolveToolApproval` · `embed_channel.go→ResolveToolApproval`

**Body** `resolveToolApprovalBody`:
| field | type | req | notes |
|---|---|---|---|
| `decision` | string | yes |  |
| `modified_args` | any |  |  |
| `reason` | string |  |  |

**Response**:
```json
{ success: true }
```
---
### `getEmbedMessageList` — `GET /api/v1/embed/:channel_id/messages/:session_id/load`

handler: `EmbedLoadMessages` · `embed_channel.go→LoadMessages`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `limit` | string |  |  |
| `before_time` | string |  |  |

**Response**:
```json
{ success: true, data: storageurl.Rewriter }
```
---
### `relayEmbedWebhookEvent` — `POST /api/v1/embed/:channel_id/sessions/:session_id/events`

handler: `EmbedRelayWebhookEvent` · `embed_channel.go`

**Body** `embedWebhookEventRequest`:
| field | type | req | notes |
|---|---|---|---|
| `type` | string |  |  |
| `session_id` | string |  |  |
| `query` | string |  |  |
| `content` | string |  |  |

**Response**:
```json
{ success: true }
```
---