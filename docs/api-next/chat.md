# Chat & Sessions API

Frontend module: `frontend-next/lib/api/chat.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `listSessions` — `GET /sessions`

handler: `GetSessionsByTenant` · `handler.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `page` | integer |  | page number |
| `page_size` | integer |  | page size |
| `keyword` | string |  | fuzzy title search |
| `source` | string |  | source filter: web / embed / api / feishu / wechat / slack / ... (api, embed and IM channels require Admin+) |
| `agent_id` | string |  | filter by agent (only applies to IM sessions) |

**Response**:
```json
{ success: true, data: any, total: number, page: number, page_size: number }
```
`data` is `SessionListItem[]` — `PageResult` is flattened so `data`/`total`/`page`/`page_size` sit at the top level.
---
### `createSession` — `POST /sessions`

handler: `CreateSession` · `handler.go`

**Body** `CreateSessionRequest`:
| field | type | req | notes |
|---|---|---|---|
| `title` | string |  | Title for the session (optional) |
| `description` | string |  | Description for the session (optional) |

**Response**:
```json
{ success: true, data: Session }
```
`data` is `Session`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | ID |
| `title` | string | yes | Title |
| `description` | string | yes | Description |
| `tenant_id` | number | yes | Workspace ID |
| `user_id` | string |  | UserID is the owner scope for this session. WeKnora user UUIDs, API external-user principals, and embed visitor principals all use this column. |
| `is_pinned` | boolean | yes | IsPinned indicates whether the session is pinned in the list. |
| `pinned_at` | string(time) |  | PinnedAt records when the session was pinned; nil when not pinned. |
| `last_request_state` | SessionLastRequestState |  | LastRequestState records the input-bar state used the last time this session sent a question (agent, model, KB scope, web search, MCPs). Persisted on every successful POST to /know |
| `sandbox_config_id` | string |  | SandboxConfigID pins which sandbox config this session's CURRENT live sandbox was created on. Empty means no live sandbox; SandboxConfigIDGlobalDefault means the deployment-wide de |
| `parent_session_id` | string |  | ParentSessionID names the session this one was forked from. Empty for ordinary sessions. Deliberately not a foreign key: the parent may be deleted while the branch lives on, and a  |
| `forked_from_message_id` | string |  | ForkedFromMessageID is the user or assistant message, IN THE PARENT SESSION, that the fork branched at. For a user point, messages strictly before it were copied here. For an assis |
| `created_at` | string(time) | yes | // Strategy configuration KnowledgeBaseID   string              `json:"knowledge_base_id"`                    // associated knowledge base IDs                 `json:"max_rounds"`  |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |
| `im_platform` | string |  | IMPlatform is the originating IM platform (e.g. "feishu", "wecom") when this session is bound to an IM channel. It is not stored on the sessions table (it lives in im_channel_sessi |

---
### `getSession` — `GET /sessions/:id`

handler: `GetSession` · `handler.go`

**Response**:
```json
{ success: true, data: types.Session }
```
`data` is `Session`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | ID |
| `title` | string | yes | Title |
| `description` | string | yes | Description |
| `tenant_id` | number | yes | Workspace ID |
| `user_id` | string |  | UserID is the owner scope for this session. WeKnora user UUIDs, API external-user principals, and embed visitor principals all use this column. |
| `is_pinned` | boolean | yes | IsPinned indicates whether the session is pinned in the list. |
| `pinned_at` | string(time) |  | PinnedAt records when the session was pinned; nil when not pinned. |
| `last_request_state` | SessionLastRequestState |  | LastRequestState records the input-bar state used the last time this session sent a question (agent, model, KB scope, web search, MCPs). Persisted on every successful POST to /know |
| `sandbox_config_id` | string |  | SandboxConfigID pins which sandbox config this session's CURRENT live sandbox was created on. Empty means no live sandbox; SandboxConfigIDGlobalDefault means the deployment-wide de |
| `parent_session_id` | string |  | ParentSessionID names the session this one was forked from. Empty for ordinary sessions. Deliberately not a foreign key: the parent may be deleted while the branch lives on, and a  |
| `forked_from_message_id` | string |  | ForkedFromMessageID is the user or assistant message, IN THE PARENT SESSION, that the fork branched at. For a user point, messages strictly before it were copied here. For an assis |
| `created_at` | string(time) | yes | // Strategy configuration KnowledgeBaseID   string              `json:"knowledge_base_id"`                    // associated knowledge base IDs                 `json:"max_rounds"`  |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |
| `im_platform` | string |  | IMPlatform is the originating IM platform (e.g. "feishu", "wecom") when this session is bound to an IM channel. It is not stored on the sessions table (it lives in im_channel_sessi |

---
### `updateSession` — `PUT /sessions/:id`

handler: `UpdateSession` · `handler.go`

**Body** `Session`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string |  | ID |
| `title` | string |  | Title |
| `description` | string |  | Description |
| `tenant_id` | number |  | Workspace ID |
| `user_id` | string |  | UserID is the owner scope for this session. WeKnora user UUIDs, API external-user principals, and embed visitor principals all use this column. |
| `is_pinned` | boolean |  | IsPinned indicates whether the session is pinned in the list. |
| `pinned_at` | string(time) |  | PinnedAt records when the session was pinned; nil when not pinned. |
| `last_request_state` | SessionLastRequestState |  | LastRequestState records the input-bar state used the last time this session sent a question (agent, model, KB scope, web search, MCPs). Persisted on every successful POST to /know |
| `sandbox_config_id` | string |  | SandboxConfigID pins which sandbox config this session's CURRENT live sandbox was created on. Empty means no live sandbox; SandboxConfigIDGlobalDefault means the deployment-wide de |
| `parent_session_id` | string |  | ParentSessionID names the session this one was forked from. Empty for ordinary sessions. Deliberately not a foreign key: the parent may be deleted while the branch lives on, and a  |
| `forked_from_message_id` | string |  | ForkedFromMessageID is the user or assistant message, IN THE PARENT SESSION, that the fork branched at. For a user point, messages strictly before it were copied here. For an assis |
| `created_at` | string(time) |  | // Strategy configuration KnowledgeBaseID   string              `json:"knowledge_base_id"`                    // associated knowledge base IDs                 `json:"max_rounds"`  |
| `updated_at` | string(time) |  |  |
| `deleted_at` | DeletedAt |  |  |
| `im_platform` | string |  | IMPlatform is the originating IM platform (e.g. "feishu", "wecom") when this session is bound to an IM channel. It is not stored on the sessions table (it lives in im_channel_sessi |

**Response**:
```json
{ success: true, data: types.Session }
```
`data` is `Session`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | ID |
| `title` | string | yes | Title |
| `description` | string | yes | Description |
| `tenant_id` | number | yes | Workspace ID |
| `user_id` | string |  | UserID is the owner scope for this session. WeKnora user UUIDs, API external-user principals, and embed visitor principals all use this column. |
| `is_pinned` | boolean | yes | IsPinned indicates whether the session is pinned in the list. |
| `pinned_at` | string(time) |  | PinnedAt records when the session was pinned; nil when not pinned. |
| `last_request_state` | SessionLastRequestState |  | LastRequestState records the input-bar state used the last time this session sent a question (agent, model, KB scope, web search, MCPs). Persisted on every successful POST to /know |
| `sandbox_config_id` | string |  | SandboxConfigID pins which sandbox config this session's CURRENT live sandbox was created on. Empty means no live sandbox; SandboxConfigIDGlobalDefault means the deployment-wide de |
| `parent_session_id` | string |  | ParentSessionID names the session this one was forked from. Empty for ordinary sessions. Deliberately not a foreign key: the parent may be deleted while the branch lives on, and a  |
| `forked_from_message_id` | string |  | ForkedFromMessageID is the user or assistant message, IN THE PARENT SESSION, that the fork branched at. For a user point, messages strictly before it were copied here. For an assis |
| `created_at` | string(time) | yes | // Strategy configuration KnowledgeBaseID   string              `json:"knowledge_base_id"`                    // associated knowledge base IDs                 `json:"max_rounds"`  |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |
| `im_platform` | string |  | IMPlatform is the originating IM platform (e.g. "feishu", "wecom") when this session is bound to an IM channel. It is not stored on the sessions table (it lives in im_channel_sessi |

---
### `deleteSession` — `DELETE /sessions/:id`

handler: `DeleteSession` · `handler.go`

**Response**:
```json
{ success: true, message: string }
```
---
### `batchDeleteSessions` — `DELETE /sessions/batch`

handler: `BatchDeleteSessions` · `handler.go`

**Body** `batchDeleteRequest`:
| field | type | req | notes |
|---|---|---|---|
| `ids` | string[] |  |  |
| `delete_all` | boolean |  |  |

**Response**:
```json
{ success: true, message: string }
```
---
### `deleteAllSessions` — `DELETE /sessions/batch`

handler: `BatchDeleteSessions` · `handler.go`

**Body** `batchDeleteRequest`:
| field | type | req | notes |
|---|---|---|---|
| `ids` | string[] |  |  |
| `delete_all` | boolean |  |  |

**Response**:
```json
{ success: true, message: string }
```
---
### `pinSession` — `POST /sessions/:session_id/pin`

handler: `PinSession` · `handler.go→setSessionPinned`

**Response**:
```json
{ success: true, is_pinned: pinned }
```
---
### `unpinSession` — `DELETE /sessions/:id/pin`

handler: `UnpinSession` · `handler.go→setSessionPinned`

**Response**:
```json
{ success: true, is_pinned: pinned }
```
---
### `generateSessionTitle` — `POST /sessions/:session_id/generate_title`

handler: `GenerateTitle` · `title.go`

**Body** `GenerateTitleRequest`:
| field | type | req | notes |
|---|---|---|---|
| `messages` | Message[] | yes |  |

**Response**:
```json
{ success: true, data: string }
```
---
### `forkSession` — `POST /sessions/:session_id/fork`

handler: `ForkSession` · `fork.go`

**Body** `ForkSessionRequest`:
| field | type | req | notes |
|---|---|---|---|
| `message_id` | string | yes | MessageID is the message to branch at. A user message copies history strictly before it (the client prefills that question). An assistant message copies history through that answer |
| `title` | string |  | Title is optional. Empty falls back to the source title plus a suffix. |

**Response**:
```json
{ success: true, data: result }
```
`data` is `result`:
| field | type | req | notes |
|---|---|---|---|
| `KnowledgeBaseID` | string | yes |  |
| `Count` | number | yes |  |

---
### `stopSession` — `POST /sessions/:session_id/stop`

handler: `StopSession` · `stream.go`

**Body** `StopSessionRequest`:
| field | type | req | notes |
|---|---|---|---|
| `message_id` | string | yes |  |

**Response**:
```json
{ success: true, message: string }
```
---
### `clearSessionMessages` — `DELETE /sessions/:id/messages`

handler: `ClearSessionMessages` · `handler.go`

**Response**:
```json
{ success: true, message: string }
```
---
### `listMessages` — `GET /messages/:session_id/load`

handler: `LoadMessages` · `message.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `limit` | integer |  | number to return |
| `before_time` | string |  | messages before this time (RFC3339Nano format) |
| `resource_urls` | string |  | file reference form; "public" returns a loadable direct URL |

**Response**:
```json
{ success: true, data: storageurl.Rewriter }
```
---
### `steerSession` — `POST /sessions/:session_id/steer`

handler: `SteerMessage` · `steer.go`

**Body** `SteerMessageRequest`:
| field | type | req | notes |
|---|---|---|---|
| `expected_assistant_message_id` | string |  | Optional for older clients. New clients pin delivery to the run they see and supply a stable ID so a consume event may precede the HTTP response. |
| `steer_id` | string |  |  |
| `query` | string | yes |  |
| `mentioned_items` | MentionedItemRequest[] |  |  |
| `channel` | string |  |  |
| `delivery` | string |  | Delivery is "after" (default) or "inject". See the constants above. |

**Response**:
```json
{ success: true, status: string, steer_id: steerID, delivery: delivery, assistant_message_id: queuedOn }
```
---
### `promoteSteerSession` — `POST /sessions/:session_id/steer/:steer_id/inject`

handler: `PromoteSteerMessage` · `steer.go`

**Response**:
```json
{ success: true, status: string, steer_id: steerID, delivery: steerDeliveryInject, assistant_message_id: assistantID }
```
---
### `listSteerSession` — `GET /sessions/:id/steer`

handler: `ListSteerMessages` · `steer.go`

**Response**:
```json
{ success: true, assistant_message_id: assistantID, items: pendingSteerQueueItems(events }
```
---
### `removeSteerSession` — `DELETE /sessions/:id/steer/:steer_id`

handler: `DeleteSteerMessage` · `steer.go`

**Response**:
```json
{ success: true, status: string, removed: bool, steer_id: steerID }
```
---
### `listMessageArtifacts` — `GET /sessions/:id/messages/:message_id/artifacts`

handler: `ListMessageArtifacts` · `artifact_download.go`

**Response**:
```json
{ success: true, data: items }
```
---
### `listSessionArtifacts` — `GET /sessions/:id/artifacts`

handler: `ListSessionArtifacts` · `artifact_download.go`

**Response**:
```json
{ success: true, data: items }
```
---
### `downloadArtifact` — `GET /sessions/:id/messages/:message_id/artifacts/:index/download`

handler: `DownloadMessageArtifact` · `artifact_download.go`

**Response**: binary download (file stream, not JSON)
---
### `listArtifactLibrary` — `GET /artifacts`

handler: `ListArtifactLibrary` · `artifact_library.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `file_types` | string |  |  |
| `keyword` | string |  |  |
| `page` | number | yes | Page |
| `page_size` | number | yes | Page size |

**Response**:
```json
{ success: true, data: any, total: number, page: number, page_size: number }
```
`data` is `ArtifactLibraryItem[]` (flattened `PageResult`).
---
### `ensureMessageSuggestions` — `POST /sessions/:session_id/messages/:message_id/suggestions`

handler: `Ensure` · `message_suggestion.go`

**Body** `EnsureMessageSuggestionsRequest`:
| field | type | req | notes |
|---|---|---|---|
| `regenerate` | boolean |  |  |

**Response**:
```json
{ success: true, data: set }
```
---
### `getMessageSuggestions` — `GET /sessions/:id/messages/:message_id/suggestions`

handler: `Get` · `storagebackend.go`

**Response**:
```json
{ success: true, data: NewStorageBackendResponse }
```
---
### `recordMessageSuggestionEvent` — `POST /sessions/:session_id/suggestion-events`

handler: `RecordEvent` · `message_suggestion.go`

**Body** `SuggestionEventRequest`:
| field | type | req | notes |
|---|---|---|---|
| `suggestion_set_id` | string | yes |  |
| `question_id` | string |  |  |
| `event_type` | string | yes |  |

**Response**: `204 No Content`
---
### `getTenantChatHistoryConfig` — `GET /tenants/kv/:key`

_dispatched via `/tenants/kv/:key` (key `chat-history-config`)_

handler: `GetTenantChatHistoryConfig` · `tenant.go`

**Response**:
```json
{ success: true, data: ChatHistoryConfig }
```
`data` is `ChatHistoryConfig`:
| field | type | req | notes |
|---|---|---|---|
| `enabled` | boolean | yes | Enabled controls whether chat history indexing is active |
| `embedding_model_id` | string | yes | EmbeddingModelID is the ID of the embedding model used for vectorizing chat messages. Once messages have been indexed, the model cannot be changed (requires re-indexing). |
| `knowledge_base_id` | string | yes | KnowledgeBaseID is the auto-managed hidden knowledge base for chat history. This is set internally when the feature is first enabled; users should not set this directly. |

---
### `updateTenantChatHistoryConfig` — `PUT /tenants/kv/:key`

_dispatched via `/tenants/kv/:key` (key `chat-history-config`)_

handler: `updateTenantChatHistoryConfigInternal` · `tenant.go`

**Body** `ChatHistoryConfig`:
| field | type | req | notes |
|---|---|---|---|
| `enabled` | boolean |  | Enabled controls whether chat history indexing is active |
| `embedding_model_id` | string |  | EmbeddingModelID is the ID of the embedding model used for vectorizing chat messages. Once messages have been indexed, the model cannot be changed (requires re-indexing). |
| `knowledge_base_id` | string |  | KnowledgeBaseID is the auto-managed hidden knowledge base for chat history. This is set internally when the feature is first enabled; users should not set this directly. |

**Response**:
```json
{ success: true, data: ChatHistoryConfig, message: string }
```
`data` is `ChatHistoryConfig`:
| field | type | req | notes |
|---|---|---|---|
| `enabled` | boolean | yes | Enabled controls whether chat history indexing is active |
| `embedding_model_id` | string | yes | EmbeddingModelID is the ID of the embedding model used for vectorizing chat messages. Once messages have been indexed, the model cannot be changed (requires re-indexing). |
| `knowledge_base_id` | string | yes | KnowledgeBaseID is the auto-managed hidden knowledge base for chat history. This is set internally when the feature is first enabled; users should not set this directly. |

---
### `getChatHistoryKBStats` — `GET /messages/chat-history-stats`

handler: `GetChatHistoryKBStats` · `message.go`

**Response**:
```json
{ success: true, data: types.ChatHistoryKBStats }
```
`data` is `ChatHistoryKBStats`:
| field | type | req | notes |
|---|---|---|---|
| `enabled` | boolean | yes | Whether the chat history KB is configured and enabled |
| `embedding_model_id` | string |  | ID of the embedding model used |
| `knowledge_base_id` | string |  | ID of the knowledge base used for chat history |
| `knowledge_base_name` | string |  | Name of the knowledge base |
| `indexed_message_count` | number | yes | Number of indexed message entries (Knowledge count) |
| `has_indexed_messages` | boolean | yes | Whether there are any indexed messages (used by frontend to lock embedding model) |

---
### `searchMessages` — `POST /messages/search`

handler: `SearchMessages` · `message.go`

**Body** `SearchMessagesRequest`:
| field | type | req | notes |
|---|---|---|---|
| `query` | string | yes | Query text for search |
| `mode` | string |  | Search mode: "keyword", "vector", "hybrid" (default: "hybrid") |
| `limit` | number |  | Maximum number of results to return (default: 20) |
| `session_ids` | string[] |  | Filter by specific session IDs (optional) |

**Response**:
```json
{ success: true, data: types.MessageSearchResult }
```
`data` is `MessageSearchResult`:
| field | type | req | notes |
|---|---|---|---|
| `items` | *MessageSearchGroupItem[] | yes | List of merged Q&A pairs |
| `total` | number | yes | Total number of results |

---

## Referenced types

#### `MentionedItemRequest`
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `name` | string | yes |  |
| `type` | string | yes |  |
| `kb_type` | string | yes |  |
| `kb_id` | string | yes |  |
| `kb_name` | string | yes |  |
| `service_id` | string | yes |  |
| `skill_name` | string | yes |  |

#### `Message`
| field | type | req | notes |
|---|---|---|---|
| `role` | string | yes |  |
| `content` | string | yes |  |
| `multi_content` | MessageContentPart[] |  |  |
| `name` | string |  |  |
| `tool_call_id` | string |  |  |
| `tool_calls` | ToolCall[] |  |  |
| `images` | string[] |  |  |
| `reasoning_content` | string |  | ReasoningContent is the reasoning text a thinking-capable assistant model (DeepSeek thinking, MiMo, vLLM reasoning, ...) produced last turn. Some providers (MiMo, DeepSeek V3.2/V4 thinking modes) require echoing assistant reasoning_content verbatim in multi-turn conversations.  |

#### `SessionLastRequestState`
| field | type | req | notes |
|---|---|---|---|
| `agent_id` | string |  |  |
| `agent_enabled` | boolean | yes |  |
| `model_id` | string |  |  |
| `knowledge_base_ids` | string[] |  |  |
| `knowledge_ids` | string[] |  |  |
| `tag_ids` | string[] |  |  |
| `mcp_service_ids` | string[] |  |  |
| `skill_names` | string[] |  |  |
| `mentioned_items` | MentionedItems |  |  |
| `local_browser_enabled` | boolean | yes |  |
| `web_search_enabled` | boolean | yes |  |
