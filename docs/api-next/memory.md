# Memory API

Frontend module: `frontend-next/lib/api/memory.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `getMemorySettings` — `GET /memory/settings`

handler: `GetSettings` · `memory.go`

**Response**:
```json
{ success: true, data: types.MemorySettings }
```
`data` is `MemorySettings`:
| field | type | req | notes |
|---|---|---|---|
| `workspace_enabled` | boolean | yes | WorkspaceEnabled is the admin switch on the workspace. |
| `user_enabled` | boolean | yes | UserEnabled is the caller's own opt out. Meaningless while the workspace switch is off, but still reported so the toggle keeps its position when an admin turns the workspace back o |
| `effective` | boolean | yes | Effective is what actually happens: WorkspaceEnabled && UserEnabled. |
| `write_mode` | string | yes | WriteMode is the workspace write mode. |
| `item_count` | number | yes | ItemCount is how many active memories the caller currently has. |
| `max_items` | number | yes | MaxItems is the capacity cap after which the lowest ranked are archived. |

---
### `updateMemoryEnabled` — `PUT /memory/settings`

handler: `UpdateSettings` · `memory.go`

**Body** `updateMemorySettingsRequest`:
| field | type | req | notes |
|---|---|---|---|
| `enabled` | boolean |  |  |

**Response**:
```json
{ success: true, data: types.MemorySettings }
```
`data` is `MemorySettings`:
| field | type | req | notes |
|---|---|---|---|
| `workspace_enabled` | boolean | yes | WorkspaceEnabled is the admin switch on the workspace. |
| `user_enabled` | boolean | yes | UserEnabled is the caller's own opt out. Meaningless while the workspace switch is off, but still reported so the toggle keeps its position when an admin turns the workspace back o |
| `effective` | boolean | yes | Effective is what actually happens: WorkspaceEnabled && UserEnabled. |
| `write_mode` | string | yes | WriteMode is the workspace write mode. |
| `item_count` | number | yes | ItemCount is how many active memories the caller currently has. |
| `max_items` | number | yes | MaxItems is the capacity cap after which the lowest ranked are archived. |

---
### `listMemoryItems` — `GET /memory/items`

handler: `ListItems` · `memory.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `status` | string |  | status filter |
| `limit` | integer |  | items per page |
| `offset` | integer |  | offset |

**Response**:
```json
{ success: true, data: items, total: total }
```
---
### `confirmMemoryItem` — `POST /memory/items/:id/confirm`

handler: `ConfirmItem` · `memory.go`

**Response**:
```json
{ success: true, data: types.MemoryItem }
```
`data` is `MemoryItem`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `tenant_id` | number | yes |  |
| `subject_id` | string | yes |  |
| `kind` | string | yes |  |
| `content` | string | yes |  |
| `topic` | string | yes | Topic is the readable subject of the statement as named by the extraction model (e.g. "database in use"). Kept verbatim next to the normalized key because it is the best retrieval hint. |
| `normalized_key` | string | yes | NormalizedKey identifies the topic this item is about. A new item with the same key as an active one supersedes it, which is how contradictions ("I use MySQL" then "I moved to Post |
| `importance` | number | yes |  |
| `origin` | string | yes |  |
| `status` | string | yes |  |
| `source_session_id` | string | yes |  |
| `source_message_id` | string | yes |  |
| `valid_from` | string(time) | yes |  |
| `invalid_at` | string(time) | yes |  |
| `expires_at` | string(time) | yes | ExpiresAt is when the statement stops being worth recalling, used for things that are true only for a while ("finish the migration this week"). Without it an in-flight task stays i |
| `replaces_id` | string |  |  |
| `superseded_by` | string | yes |  |
| `last_used_at` | string(time) | yes |  |
| `use_count` | number | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |

---
### `rejectMemoryItem` — `POST /memory/items/:id/reject`

handler: `RejectItem` · `memory.go`

**Response**:
```json
{ success: true }
```
---
### `createMemoryItem` — `POST /memory/items`

handler: `CreateItem` · `memory.go`

**Body** `createMemoryItemRequest`:
| field | type | req | notes |
|---|---|---|---|
| `kind` | string |  |  |
| `content` | string |  |  |
| `importance` | number |  |  |

**Response**:
```json
{ success: true, data: types.MemoryItem }
```
`data` is `MemoryItem`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `tenant_id` | number | yes |  |
| `subject_id` | string | yes |  |
| `kind` | string | yes |  |
| `content` | string | yes |  |
| `topic` | string | yes | Topic is the readable subject of the statement as named by the extraction model (e.g. "database in use"). Kept verbatim next to the normalized key because it is the best retrieval hint. |
| `normalized_key` | string | yes | NormalizedKey identifies the topic this item is about. A new item with the same key as an active one supersedes it, which is how contradictions ("I use MySQL" then "I moved to Post |
| `importance` | number | yes |  |
| `origin` | string | yes |  |
| `status` | string | yes |  |
| `source_session_id` | string | yes |  |
| `source_message_id` | string | yes |  |
| `valid_from` | string(time) | yes |  |
| `invalid_at` | string(time) | yes |  |
| `expires_at` | string(time) | yes | ExpiresAt is when the statement stops being worth recalling, used for things that are true only for a while ("finish the migration this week"). Without it an in-flight task stays i |
| `replaces_id` | string |  |  |
| `superseded_by` | string | yes |  |
| `last_used_at` | string(time) | yes |  |
| `use_count` | number | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |

---
### `updateMemoryItem` — `PUT /memory/items/:id`

handler: `UpdateItem` · `memory.go`

**Body** `updateMemoryItemRequest`:
| field | type | req | notes |
|---|---|---|---|
| `content` | string |  |  |
| `importance` | number |  |  |

**Response**:
```json
{ success: true, data: types.MemoryItem }
```
`data` is `MemoryItem`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `tenant_id` | number | yes |  |
| `subject_id` | string | yes |  |
| `kind` | string | yes |  |
| `content` | string | yes |  |
| `topic` | string | yes | Topic is the readable subject of the statement as named by the extraction model (e.g. "database in use"). Kept verbatim next to the normalized key because it is the best retrieval hint. |
| `normalized_key` | string | yes | NormalizedKey identifies the topic this item is about. A new item with the same key as an active one supersedes it, which is how contradictions ("I use MySQL" then "I moved to Post |
| `importance` | number | yes |  |
| `origin` | string | yes |  |
| `status` | string | yes |  |
| `source_session_id` | string | yes |  |
| `source_message_id` | string | yes |  |
| `valid_from` | string(time) | yes |  |
| `invalid_at` | string(time) | yes |  |
| `expires_at` | string(time) | yes | ExpiresAt is when the statement stops being worth recalling, used for things that are true only for a while ("finish the migration this week"). Without it an in-flight task stays i |
| `replaces_id` | string |  |  |
| `superseded_by` | string | yes |  |
| `last_used_at` | string(time) | yes |  |
| `use_count` | number | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |

---
### `deleteMemoryItem` — `DELETE /memory/items/:id`

handler: `DeleteItem` · `memory.go`

**Response**:
```json
{ success: true }
```
---
### `clearMemoryItems` — `DELETE /memory/items`

handler: `Clear` · `memory.go`

**Response**:
```json
{ success: true, removed: int64 }
```
---
### `exportMemoryItems` — `GET /memory/export`

handler: `Export` · `memory.go`

**Response**:
```json
{ success: true, total: int64, truncated: int64(len(items)) < total, data: []*types.MemoryItem }
```
`data` is `MemoryItem`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `tenant_id` | number | yes |  |
| `subject_id` | string | yes |  |
| `kind` | string | yes |  |
| `content` | string | yes |  |
| `topic` | string | yes | Topic is the readable subject of the statement as named by the extraction model (e.g. "database in use"). Kept verbatim next to the normalized key because it is the best retrieval hint. |
| `normalized_key` | string | yes | NormalizedKey identifies the topic this item is about. A new item with the same key as an active one supersedes it, which is how contradictions ("I use MySQL" then "I moved to Post |
| `importance` | number | yes |  |
| `origin` | string | yes |  |
| `status` | string | yes |  |
| `source_session_id` | string | yes |  |
| `source_message_id` | string | yes |  |
| `valid_from` | string(time) | yes |  |
| `invalid_at` | string(time) | yes |  |
| `expires_at` | string(time) | yes | ExpiresAt is when the statement stops being worth recalling, used for things that are true only for a while ("finish the migration this week"). Without it an in-flight task stays i |
| `replaces_id` | string |  |  |
| `superseded_by` | string | yes |  |
| `last_used_at` | string(time) | yes |  |
| `use_count` | number | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |

---
### `consolidateMemory` — `POST /memory/consolidate`

handler: `Consolidate` · `memory.go`

**Response**:
```json
{ success: true, data: types.MemoryConsolidationResult }
```
`data` is `MemoryConsolidationResult`:
| field | type | req | notes |
|---|---|---|---|
| `merged` | number | yes |  |
| `demoted` | number | yes |  |
| `expired` | number | yes |  |
| `reviewed` | number | yes | Reviewed is how many active memories the pass looked at. |
| `candidates` | number | yes | Candidates is how many groups were put in front of the model. |
| `skipped` | string |  | Skipped is why nothing was merged, empty when something was. |

---
### `listMemoryTopics` — `GET /memory/topics`

handler: `ListTopics` · `memory.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `limit` | integer |  | items per page |
| `offset` | integer |  | offset |

**Response**:
```json
{ success: true, data: topics, total: total }
```
---
### `promoteMemoryTopic` — `POST /memory/topics/:id/promote`

handler: `PromoteTopic` · `memory.go`

**Response**:
```json
{ success: true, data: types.MemoryItem }
```
`data` is `MemoryItem`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `tenant_id` | number | yes |  |
| `subject_id` | string | yes |  |
| `kind` | string | yes |  |
| `content` | string | yes |  |
| `topic` | string | yes | Topic is the readable subject of the statement as named by the extraction model (e.g. "database in use"). Kept verbatim next to the normalized key because it is the best retrieval hint. |
| `normalized_key` | string | yes | NormalizedKey identifies the topic this item is about. A new item with the same key as an active one supersedes it, which is how contradictions ("I use MySQL" then "I moved to Post |
| `importance` | number | yes |  |
| `origin` | string | yes |  |
| `status` | string | yes |  |
| `source_session_id` | string | yes |  |
| `source_message_id` | string | yes |  |
| `valid_from` | string(time) | yes |  |
| `invalid_at` | string(time) | yes |  |
| `expires_at` | string(time) | yes | ExpiresAt is when the statement stops being worth recalling, used for things that are true only for a while ("finish the migration this week"). Without it an in-flight task stays i |
| `replaces_id` | string |  |  |
| `superseded_by` | string | yes |  |
| `last_used_at` | string(time) | yes |  |
| `use_count` | number | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |

---
### `deleteMemoryTopic` — `DELETE /memory/topics/:id`

handler: `DeleteTopic` · `memory.go`

**Response**:
```json
{ success: true }
```
---
### `listMemoryDocuments` — `GET /memory/documents`

handler: `ListDocuments` · `memory.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `limit` | integer |  | items per page |
| `offset` | integer |  | offset |

**Response**:
```json
{ success: true, data: docs, total: total }
```
---
### `deleteMemoryDocument` — `DELETE /memory/documents/:id`

handler: `DeleteDocument` · `memory.go`

**Response**:
```json
{ success: true }
```
---
### `getTenantMemoryConfig` — `GET /tenants/kv/:key`

_dispatched via `/tenants/kv/:key` (key `memory-config`)_

handler: `GetTenantMemoryConfig` · `tenant.go`

**Response**:
```json
{ success: true, data: DefaultMemoryConfig }
```
---
### `updateTenantMemoryConfig` — `PUT /tenants/kv/:key`

_dispatched via `/tenants/kv/:key` (key `memory-config`)_

handler: `updateTenantMemoryConfigInternal` · `tenant.go`

**Body** `MemoryConfig`:
| field | type | req | notes |
|---|---|---|---|
| `enabled` | boolean |  | Enabled is on for workspaces that never saved a config (see DefaultMemoryConfig); once an admin saves, the stored value wins. |
| `write_mode` | string |  | WriteMode is MemoryWriteExplicitOnly or MemoryWriteAuto. |
| `extract_model_id` | string |  | ExtractModelID is the model used by the background extraction task. Empty means "use the model the conversation itself used", which is what the settings UI promises, so the extract |
| `max_items` | number |  | MaxItems caps active items per subject. 0 means DefaultMemoryMaxItems. |
| `extract_delay_seconds` | number |  | ExtractDelaySeconds is how long a finished turn waits before distillation runs. Waiting lets one model call cover the several messages a user usually sends in a row. 0 means the de |
| `extract_min_interval_seconds` | number |  | ExtractMinIntervalSeconds is the floor between two distillation runs for the same person, and exists purely to bound cost. It never drops a turn: a turn arriving inside the interva |
| `extract_instructions` | string |  | ExtractInstructions are workspace-specific rules appended to the distillation prompt, for policies the product cannot guess ("never record customer names", "always note the environ |
| `interest_threshold` | number |  | InterestThreshold is how many separate conversations must touch a topic before it becomes a stored interest. 0 means the default. Setting it to 1 records every topic on first sight |
| `embedding_model_id` | string |  | EmbeddingModelID is the single model used to score memory against a question. It is pinned per workspace: knowledge bases each have their own embedding model, and grabbing whicheve |
| `vector_recall` | boolean |  | VectorRecall adds semantic similarity to memory recall. Nil means on when an embedding model is reachable. Lexical matching alone cannot find a memory the user has re-worded, which |
| `retrieval_conditioning` | boolean |  | RetrievalConditioning lets memory shape retrieval — query rewriting and per-document ranking — rather than only being appended to the answer prompt. This is where memory earns its  |

**Response**:
```json
{ success: true, data: MemoryConfig, message: string }
```
`data` is `MemoryConfig`:
| field | type | req | notes |
|---|---|---|---|
| `enabled` | boolean | yes | Enabled is on for workspaces that never saved a config (see DefaultMemoryConfig); once an admin saves, the stored value wins. |
| `write_mode` | string | yes | WriteMode is MemoryWriteExplicitOnly or MemoryWriteAuto. |
| `extract_model_id` | string | yes | ExtractModelID is the model used by the background extraction task. Empty means "use the model the conversation itself used", which is what the settings UI promises, so the extract |
| `max_items` | number | yes | MaxItems caps active items per subject. 0 means DefaultMemoryMaxItems. |
| `extract_delay_seconds` | number | yes | ExtractDelaySeconds is how long a finished turn waits before distillation runs. Waiting lets one model call cover the several messages a user usually sends in a row. 0 means the de |
| `extract_min_interval_seconds` | number | yes | ExtractMinIntervalSeconds is the floor between two distillation runs for the same person, and exists purely to bound cost. It never drops a turn: a turn arriving inside the interva |
| `extract_instructions` | string | yes | ExtractInstructions are workspace-specific rules appended to the distillation prompt, for policies the product cannot guess ("never record customer names", "always note the environ |
| `interest_threshold` | number | yes | InterestThreshold is how many separate conversations must touch a topic before it becomes a stored interest. 0 means the default. Setting it to 1 records every topic on first sight |
| `embedding_model_id` | string | yes | EmbeddingModelID is the single model used to score memory against a question. It is pinned per workspace: knowledge bases each have their own embedding model, and grabbing whicheve |
| `vector_recall` | boolean | yes | VectorRecall adds semantic similarity to memory recall. Nil means on when an embedding model is reachable. Lexical matching alone cannot find a memory the user has re-worded, which |
| `retrieval_conditioning` | boolean | yes | RetrievalConditioning lets memory shape retrieval — query rewriting and per-document ranking — rather than only being appended to the answer prompt. This is where memory earns its  |

---