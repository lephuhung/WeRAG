# Knowledge Bases & Documents API

Frontend module: `frontend-next/lib/api/knowledge.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `listKnowledgeBaseActivity` — `GET /knowledge-bases/:id/activity`

handler: `ListKnowledgeBaseActivity` · `audit_log.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `after_id` | integer |  | cursor: returns rows with id below this value |
| `limit` | integer |  | page size, 1-100, default 50 |
| `action` | string |  | exact filter on action |
| `outcome` | string |  | exact filter on outcome |
| `actor` | string |  | exact filter on actor_user_id |

**Response** `auditLogListResponse`:
| field | type | req | notes |
|---|---|---|---|
| `success` | boolean | yes |  |
| `data` | AuditLog[] | yes |  |
| `next_cursor` | number | yes |  |

---
### `listKnowledgeBases` — `GET /knowledge-bases`

handler: `ListKnowledgeBases` · `knowledgebase.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `agent_id` | string |  | shared agent ID; when given, returns the knowledge bases usable by that agent |
| `creator` | string |  |  |

**Response**:
```json
{ success: true, data: h.buildKBListResponse(ctx }
```
---
### `createKnowledgeBase` — `POST /knowledge-bases`

handler: `CreateKnowledgeBase` · `knowledgebase.go`

**Body** `KnowledgeBase`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string |  | Unique identifier of the knowledge base |
| `name` | string |  | Name of the knowledge base |
| `type` | string |  | Type of the knowledge base (document, faq, etc.) |
| `is_temporary` | boolean |  | Whether this knowledge base is temporary (ephemeral) and should be hidden from UI |
| `description` | string |  | Description of the knowledge base |
| `tenant_id` | number |  | Workspace ID |
| `creator_id` | string |  | CreatorID records the user ID of whoever originally created the KB. Used by the workspace-level RBAC middleware to let Contributors edit their own KBs without granting them access  |
| `visibility` | KBVisibility |  | Visibility controls the read/search scope of this knowledge base. See KBVisibility constants. Rows predating migration 000107 read back as 'tenant' via the column default. |
| `org_id` | number |  | OrgID binds the knowledge base to a tenant org when Visibility is 'org'. Must reference a tenant_orgs row of the same tenant; nil for all other visibilities. |
| `chunking_config` | ChunkingConfig |  | Chunking configuration |
| `image_processing_config` | ImageProcessingConfig |  | Image processing configuration |
| `embedding_model_id` | string |  | ID of the embedding model |
| `summary_model_id` | string |  | Summary model ID |
| `vlm_config` | VLMConfig |  | VLM config |
| `asr_config` | ASRConfig |  | ASR config (Automatic Speech Recognition) |
| `storage_provider_config` | StorageProviderConfig |  | Storage provider config (new): only stores provider selection; credentials from workspace StorageEngineConfig |
| `storage_backend_id` | string |  | StorageBackendID binds this KB to one concrete storage instance. The legacy provider field remains readable during migration only. |
| `storage_config` | StorageConfig |  | Deprecated: legacy COS config column. Kept for backward compatibility with old data. |
| `vector_store_id` | string |  | VectorStoreID references the VectorStore this knowledge base is bound to. When nil, the KB falls back to the workspace's effective engines derived from the RETRIEVE_DRIVER environm |
| `extract_config` | ExtractConfig |  | Extract config |
| `faq_config` | FAQConfig |  | FAQConfig stores FAQ specific configuration such as indexing strategy |
| `question_generation_config` | QuestionGenerationConfig |  | QuestionGenerationConfig stores question generation configuration for document knowledge bases |
| `auto_tag_config` | AutoTagConfig |  | AutoTagConfig controls asynchronous association of existing tags after parsing. |
| `profile_config` | KnowledgeBaseProfileConfig |  | ProfileConfig controls automatic generation of the knowledge-base description from per-document profiles (document knowledge bases only). |
| `generated_profile` | KnowledgeBaseProfile |  | GeneratedProfile is the machine-generated description: a gist, merged topics, typical questions and the aggregate snapshot they came from. It never overwrites the user-authored Des |
| `wiki_config` | WikiConfig |  | WikiConfig stores wiki-specific configuration (only for wiki type knowledge bases) |
| `indexing_strategy` | IndexingStrategy |  | IndexingStrategy controls which indexing pipelines are active for this knowledge base. Pipelines: vector search, keyword search, wiki generation, knowledge graph extraction. |
| `is_pinned` | boolean |  | IsPinned and PinnedAt are computed per-caller from user_kb_pins (see migration 000050). They used to be stored on the row itself, which made pinning a workspace-wide ordering decis |
| `pinned_at` | string(time) |  | PinnedAt records when the current caller pinned this knowledge base; nil when they have not. |
| `created_at` | string(time) |  | Creation time of the knowledge base |
| `updated_at` | string(time) |  | Last updated time of the knowledge base |
| `deleted_at` | DeletedAt |  | Deletion time of the knowledge base |
| `knowledge_count` | number |  | Knowledge count (not stored in database, calculated on query) |
| `chunk_count` | number |  | Chunk count (not stored in database, calculated on query) |
| `is_processing` | boolean |  | IsProcessing indicates if there is a processing import task (for FAQ type knowledge bases) |
| `processing_count` | number |  | ProcessingCount indicates the number of knowledge items being processed (for document type knowledge bases) |
| `share_count` | number |  | ShareCount indicates the number of organizations this knowledge base is shared with (not stored in database) |
| `creator_name` | string |  | CreatorName is the display name (username/email) of the user referenced by CreatorID. It is batch-filled by list handlers only and never persisted; empty means the creator could not be resolved (deleted user, legacy rows with empty CreatorID, etc.). The frontend uses it for the mine-vs-workspace badge on cards. |

**Response**:
```json
{ success: true, data: buildKBResponse(kb }
```
---
### `getKnowledgeBase` — `GET /knowledge-bases/:id`

handler: `GetKnowledgeBase` · `knowledgebase.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `agent_id` | string |  | shared agent ID; used to check whether the agent can access this knowledge base |

**Response**:
```json
{ success: true, data: buildKBResponse(kb }
```
---
### `updateKnowledgeBase` — `PUT /knowledge-bases/:id`

handler: `UpdateKnowledgeBase` · `knowledgebase.go`

**Body** `UpdateKnowledgeBaseRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `description` | string |  |  |
| `config` | KnowledgeBaseConfig |  |  |

**Response**:
```json
{ success: true, data: buildKBResponse(kb }
```
---
### `updateKnowledgeBaseVisibility` — `PUT /knowledge-bases/:id/visibility`

handler: `UpdateKnowledgeBaseVisibility` · `knowledgebase.go`

**Body** `UpdateKnowledgeBaseVisibilityRequest`:
| field | type | req | notes |
|---|---|---|---|
| `visibility` | KBVisibility | yes |  |
| `org_id` | number |  |  |

**Response**:
```json
{ success: true, data: buildKBResponse(kb }
```
---
### `generateKnowledgeBaseProfile` — `POST /knowledge-bases/:id/profile/generate`

handler: `GenerateKnowledgeBaseProfile` · `knowledgebase.go`

**Response**:
```json
{ success: true, data: profile }
```
---
### `deleteKnowledgeBase` — `DELETE /knowledge-bases/:id`

handler: `DeleteKnowledgeBase` · `knowledgebase.go`

**Response**:
```json
{ success: true, message: string }
```
---
### `copyKnowledgeBase` — `POST /knowledge-bases/copy`

handler: `CopyKnowledgeBase` · `knowledgebase.go`

**Body** `CopyKnowledgeBaseRequest`:
| field | type | req | notes |
|---|---|---|---|
| `task_id` | string |  |  |
| `source_id` | string | yes |  |
| `target_id` | string |  |  |

**Response**:
```json
{ success: true, data: CopyKnowledgeBaseResponse{ TaskID: taskID, SourceID: req.SourceID, TargetID: req.TargetID, Message: "Knowledge base copy task started", } }
```
---
### `duplicateKnowledgeBase` — `POST /knowledge-bases/:id/duplicate`

handler: `DuplicateKnowledgeBase` · `knowledgebase.go`

**Response**:
```json
{ success: true, data: DuplicateKnowledgeBaseResponse{ SourceID: sourceID, TargetID: targetKB.ID, Message: "Knowledge base duplicate created", KnowledgeBase: buildKBResponse(targetKB, h.resolveKBStoreView(ctx, targetKB, callerTenantID), nil), } }
```
---
### `listMoveTargets` — `GET /knowledge-bases/:id/move-targets`

handler: `ListMoveTargets` · `knowledgebase.go`

**Response**:
```json
{ success: true, data: targets }
```
---
### `moveKnowledge` — `POST /knowledge/move`

handler: `MoveKnowledge` · `knowledge.go`

**Body** `MoveKnowledgeRequest`:
| field | type | req | notes |
|---|---|---|---|
| `knowledge_ids` | string[] | yes |  |
| `source_kb_id` | string | yes |  |
| `target_kb_id` | string | yes |  |
| `mode` | string | yes |  |

**Response** `MoveKnowledgeResponse`:
| field | type | req | notes |
|---|---|---|---|
| `task_id` | string | yes |  |
| `source_kb_id` | string | yes |  |
| `target_kb_id` | string | yes |  |
| `knowledge_count` | number | yes |  |
| `message` | string | yes |  |

---
### `getKnowledgeMoveProgress` — `GET /knowledge/move/progress/:task_id`

handler: `GetKnowledgeMoveProgress` · `knowledge.go`

**Response** `KnowledgeMoveProgress`:
| field | type | req | notes |
|---|---|---|---|
| `task_id` | string | yes |  |
| `source_kb_id` | string | yes |  |
| `target_kb_id` | string | yes |  |
| `status` | KBCloneTaskStatus | yes |  |
| `progress` | number | yes |  |
| `total` | number | yes |  |
| `processed` | number | yes |  |
| `failed` | number | yes |  |
| `message` | string | yes |  |
| `error` | string | yes |  |
| `created_at` | number | yes |  |
| `updated_at` | number | yes |  |

---
### `togglePinKnowledgeBase` — `PUT /knowledge-bases/:id/pin`

handler: `TogglePinKnowledgeBase` · `knowledgebase.go`

**Response**:
```json
{ success: true, data: buildKBResponse(kb }
```
---
### `listKnowledgeFiles` — `GET /knowledge-bases/:id/knowledge`

handler: `ListKnowledge` · `knowledge.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `page` | integer |  | page number |
| `page_size` | integer |  | page size |
| `tag_ids` | string |  | tag ID filter, comma-separated (OR semantics) |
| `keyword` | string |  | keyword search |
| `file_type` | string |  | file type filter |
| `parse_status` | string |  | parse status filter (pending/processing/completed/failed) |
| `source` | string |  | source/channel filter (web/api/feishu/notion/yuque/wechat/..., or manual/url matched by type) |
| `start_time` | string |  | updated-at start bound, RFC3339 format |
| `end_time` | string |  | updated-at end bound, RFC3339 format |
| `folder_path` | string |  | folder path filter; empty string = knowledge base root; omit to disable folder filtering |
| `folder_recursive` | boolean |  | when true, also return documents inside subfolders |

**Response**:
```json
{ success: true, data: result.Data, total: result.Total, page: result.Page, page_size: result.PageSize }
```
---
### `uploadKnowledgeFile` — `POST /knowledge-bases/:id/knowledge/file`

handler: `CreateKnowledgeFromFile` · `knowledge.go`

**Multipart form**:
| name | type | req | notes |
|---|---|---|---|
| `file` | file | yes |  |
| `fileName` | string |  |  |
| `metadata` | string |  |  |
| `enable_multimodel` | boolean |  |  |
| `tag_ids` | string |  |  |
| `process_config` | string |  |  |
| `channel` | string |  |  |

**Response**:
```json
{ success: true, data: knowledge }
```
---
### `createKnowledgeFromURL` — `POST /knowledge-bases/:id/knowledge/url`

handler: `CreateKnowledgeFromURL` · `knowledge.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `url` | string | yes |  |
| `file_name` | string |  |  |
| `file_type` | string |  |  |
| `enable_multimodel` | boolean |  |  |
| `title` | string |  |  |
| `tag_ids` | string[] |  |  |
| `channel` | string |  |  |
| `process_config` | KnowledgeProcessOverrides |  |  |

**Response**:
```json
{ success: true, data: knowledge }
```
---
### `createManualKnowledge` — `POST /knowledge-bases/:id/knowledge/manual`

handler: `CreateManualKnowledge` · `knowledge.go`

**Body** `ManualKnowledgePayload`:
| field | type | req | notes |
|---|---|---|---|
| `title` | string |  |  |
| `content` | string |  |  |
| `status` | string |  |  |
| `tag_ids` | string[] |  |  |
| `channel` | string |  |  |
| `process_config` | KnowledgeProcessOverrides |  |  |

**Response**:
```json
{ success: true, data: knowledge }
```
---
### `listKnowledgeFolders` — `GET /knowledge-bases/:id/knowledge/folders`

handler: `ListKnowledgeFolders` · `knowledge.go`

**Response**:
```json
{ success: true, data: types.KnowledgeFolderTree }
```
`data` is `KnowledgeFolderTree`:
| field | type | req | notes |
|---|---|---|---|
| `root_document_count` | number | yes | RootDocumentCount counts entries that live at the knowledge base root (folder_path = ""), i.e. documents that were not uploaded as part of a folder. |
| `total_document_count` | number | yes | TotalDocumentCount counts every entry in the knowledge base, matching the unfiltered document list total. |
| `folders` | *KnowledgeFolderNode[] | yes | Folders are the top-level folders, sorted by name. |

---
### `moveKnowledgeToFolder` — `POST /knowledge/folder`

handler: `MoveKnowledgeToFolder` · `knowledge.go`

**Body** `MoveKnowledgeToFolderRequest`:
| field | type | req | notes |
|---|---|---|---|
| `kb_id` | string | yes |  |
| `knowledge_ids` | string[] | yes |  |
| `folder_path` | string |  | FolderPath is the destination folder; the empty string is the knowledge base top level. It is deliberately not `binding:"required"` so documents can be moved back out of every fold |

**Response**:
```json
{ success: true, data: {moved_count, folder_path} }
```
---
### `renameKnowledgeFolder` — `PUT /knowledge-bases/:id/knowledge/folders`

handler: `RenameKnowledgeFolder` · `knowledge.go`

**Body** `RenameKnowledgeFolderRequest`:
| field | type | req | notes |
|---|---|---|---|
| `from` | string | yes |  |
| `to` | string | yes |  |

**Response**:
```json
{ success: true, data: {moved_count, folder_path} }
```
---
### `getKnowledgeDetails` — `GET /knowledge/:id`

handler: `GetKnowledge` · `knowledge.go`

**Response**:
```json
{ success: true, data: types.Knowledge }
```
`data` is `Knowledge`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier of the knowledge |
| `tenant_id` | number | yes | Workspace ID |
| `knowledge_base_id` | string | yes | ID of the knowledge base |
| `tags` | *KnowledgeTag[] | yes | Tags holds the tags associated with this knowledge (populated on query, not persisted directly). |
| `type` | string | yes | Type of the knowledge |
| `title` | string | yes | Title of the knowledge |
| `description` | string | yes | Description of the knowledge |
| `source` | string | yes | Source of the knowledge (e.g. URL address for url type, "manual" for manual type) |
| `channel` | string | yes | Channel indicates through which channel the knowledge was ingested (web, api, browser_extension, wechat, etc.) |
| `parse_status` | string | yes | Parse status of the knowledge |
| `pending_subtasks_count` | number | yes | PendingSubtasksCount is the outstanding enrichment subtask count (summary + question + graph chunks). Only meaningful while ParseStatus == "finalizing"; defaults to 0 in any termin |
| `summary_status` | string | yes | Summary status for async summary generation |
| `profile` | KnowledgeProfile |  | Profile is the structured companion of Description: a one-line gist, topic keywords, a document type and one typical question. It is produced by the same model call as the summary  |
| `enable_status` | string | yes | Enable status of the knowledge |
| `embedding_model_id` | string | yes | ID of the embedding model |
| `file_name` | string | yes | File name of the knowledge |
| `folder_path` | string | yes | FolderPath is the canonical relative directory this entry belongs to inside the knowledge base, e.g. "docs/spec" for a folder upload of "docs/spec/design.md". Empty means the knowl |
| `file_type` | string | yes | File type of the knowledge |
| `file_size` | number | yes | File size of the knowledge |
| `file_hash` | string | yes | File hash of the knowledge |
| `file_path` | string | yes | File path of the knowledge |
| `storage_size` | number | yes | Storage size of the knowledge |
| `metadata` | JSON | yes | Metadata of the knowledge |
| `custom_metadata` | JSON | yes | CustomMetadata is user-authored descriptive metadata. It is deliberately separate from Metadata, which contains internal ingestion state and IDs. |
| `last_faq_import_result` | JSON | yes | Last FAQ import result (for FAQ type knowledge only) |
| `created_at` | string(time) | yes | Creation time of the knowledge |
| `updated_at` | string(time) | yes | Last updated time of the knowledge |
| `processed_at` | string(time) | yes | Processed time of the knowledge |
| `error_message` | string | yes | Error message of the knowledge |
| `deleted_at` | DeletedAt | yes | Deletion time of the knowledge |
| `knowledge_base_name` | string | yes | Knowledge base name (not stored in database, populated on query) |

---
### `updateManualKnowledge` — `PUT /knowledge/manual/:id`

handler: `UpdateManualKnowledge` · `knowledge.go`

**Body** `ManualKnowledgePayload`:
| field | type | req | notes |
|---|---|---|---|
| `title` | string |  |  |
| `content` | string |  |  |
| `status` | string |  |  |
| `tag_ids` | string[] |  |  |
| `channel` | string |  |  |
| `process_config` | KnowledgeProcessOverrides |  |  |

**Response**:
```json
{ success: true, data: knowledge }
```
---
### `reparseKnowledge` — `POST /knowledge/:id/reparse`

handler: `ReparseKnowledge` · `knowledge.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `process_config` | KnowledgeProcessOverrides |  |  |

**Response**:
```json
{ success: true, message: string, data: knowledge }
```
---
### `cancelKnowledgeParse` — `POST /knowledge/:id/cancel-parse`

handler: `CancelKnowledgeParse` · `knowledge.go`

**Response**:
```json
{ success: true, message: string, data: types.Knowledge }
```
`data` is `Knowledge`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier of the knowledge |
| `tenant_id` | number | yes | Workspace ID |
| `knowledge_base_id` | string | yes | ID of the knowledge base |
| `tags` | *KnowledgeTag[] | yes | Tags holds the tags associated with this knowledge (populated on query, not persisted directly). |
| `type` | string | yes | Type of the knowledge |
| `title` | string | yes | Title of the knowledge |
| `description` | string | yes | Description of the knowledge |
| `source` | string | yes | Source of the knowledge (e.g. URL address for url type, "manual" for manual type) |
| `channel` | string | yes | Channel indicates through which channel the knowledge was ingested (web, api, browser_extension, wechat, etc.) |
| `parse_status` | string | yes | Parse status of the knowledge |
| `pending_subtasks_count` | number | yes | PendingSubtasksCount is the outstanding enrichment subtask count (summary + question + graph chunks). Only meaningful while ParseStatus == "finalizing"; defaults to 0 in any termin |
| `summary_status` | string | yes | Summary status for async summary generation |
| `profile` | KnowledgeProfile |  | Profile is the structured companion of Description: a one-line gist, topic keywords, a document type and one typical question. It is produced by the same model call as the summary  |
| `enable_status` | string | yes | Enable status of the knowledge |
| `embedding_model_id` | string | yes | ID of the embedding model |
| `file_name` | string | yes | File name of the knowledge |
| `folder_path` | string | yes | FolderPath is the canonical relative directory this entry belongs to inside the knowledge base, e.g. "docs/spec" for a folder upload of "docs/spec/design.md". Empty means the knowl |
| `file_type` | string | yes | File type of the knowledge |
| `file_size` | number | yes | File size of the knowledge |
| `file_hash` | string | yes | File hash of the knowledge |
| `file_path` | string | yes | File path of the knowledge |
| `storage_size` | number | yes | Storage size of the knowledge |
| `metadata` | JSON | yes | Metadata of the knowledge |
| `custom_metadata` | JSON | yes | CustomMetadata is user-authored descriptive metadata. It is deliberately separate from Metadata, which contains internal ingestion state and IDs. |
| `last_faq_import_result` | JSON | yes | Last FAQ import result (for FAQ type knowledge only) |
| `created_at` | string(time) | yes | Creation time of the knowledge |
| `updated_at` | string(time) | yes | Last updated time of the knowledge |
| `processed_at` | string(time) | yes | Processed time of the knowledge |
| `error_message` | string | yes | Error message of the knowledge |
| `deleted_at` | DeletedAt | yes | Deletion time of the knowledge |
| `knowledge_base_name` | string | yes | Knowledge base name (not stored in database, populated on query) |

---
### `getKnowledgeSpans` — `GET /knowledge/:id/spans`

handler: `GetKnowledgeSpans` · `knowledge.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `attempt` | integer |  | specific attempt number; omitted = latest |

**Response**:
```json
{ success: true, data: resp }
```
---
### `deleteKnowledge` — `DELETE /knowledge/:id`

handler: `DeleteKnowledge` · `knowledge.go`

**Response**:
```json
{ success: true, message: string, data: {task_id} }
```
---
### `batchDeleteKnowledge` — `POST /knowledge/batch-delete`

handler: `BatchDeleteKnowledge` · `knowledge.go`

**Body** `BatchDeleteKnowledgeRequest`:
| field | type | req | notes |
|---|---|---|---|
| `kb_id` | string | yes |  |
| `ids` | string[] | yes |  |

**Response**:
```json
{ success: true, message: string, data: {task_id, deleted_count} }
```
---
### `downloadKnowledge` — `GET /knowledge/:id/download`

handler: `DownloadKnowledgeFile` · `knowledge.go`

**Response**: binary download (file stream, not JSON)
---
### `batchDownloadKnowledge` — `POST /knowledge-bases/:id/knowledge/batch-download`

handler: `BatchDownloadKnowledge` · `knowledge_download.go`

**Body** `BatchDownloadKnowledgeRequest`:
| field | type | req | notes |
|---|---|---|---|
| `ids` | string[] | yes |  |

**Response**: binary download (file stream, not JSON)
---
### `previewKnowledgeFile` — `GET /knowledge/:id/preview`

handler: `PreviewKnowledgeFile` · `knowledge.go`

**Response**: binary download (file stream, not JSON)
---
### `batchQueryKnowledge` — `GET /knowledge/batch`

handler: `GetKnowledgeBatch` · `knowledge.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `ids` | array | yes | knowledge ID list |
| `kb_id` | string |  | optional knowledge base ID; scopes the request when the KB is shared |
| `agent_id` | string |  | optional shared agent ID; used to batch-fetch file details per agent space |
| `IDs` | string[] | yes |  |
| `KBID` | string | yes |  |
| `AgentID` | string | yes |  |
| `AgentSourceTenantID` | number | yes |  |

**Response**:
```json
{ success: true, data: []*types.Knowledge }
```
`data` is `Knowledge`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier of the knowledge |
| `tenant_id` | number | yes | Workspace ID |
| `knowledge_base_id` | string | yes | ID of the knowledge base |
| `tags` | *KnowledgeTag[] | yes | Tags holds the tags associated with this knowledge (populated on query, not persisted directly). |
| `type` | string | yes | Type of the knowledge |
| `title` | string | yes | Title of the knowledge |
| `description` | string | yes | Description of the knowledge |
| `source` | string | yes | Source of the knowledge (e.g. URL address for url type, "manual" for manual type) |
| `channel` | string | yes | Channel indicates through which channel the knowledge was ingested (web, api, browser_extension, wechat, etc.) |
| `parse_status` | string | yes | Parse status of the knowledge |
| `pending_subtasks_count` | number | yes | PendingSubtasksCount is the outstanding enrichment subtask count (summary + question + graph chunks). Only meaningful while ParseStatus == "finalizing"; defaults to 0 in any termin |
| `summary_status` | string | yes | Summary status for async summary generation |
| `profile` | KnowledgeProfile |  | Profile is the structured companion of Description: a one-line gist, topic keywords, a document type and one typical question. It is produced by the same model call as the summary  |
| `enable_status` | string | yes | Enable status of the knowledge |
| `embedding_model_id` | string | yes | ID of the embedding model |
| `file_name` | string | yes | File name of the knowledge |
| `folder_path` | string | yes | FolderPath is the canonical relative directory this entry belongs to inside the knowledge base, e.g. "docs/spec" for a folder upload of "docs/spec/design.md". Empty means the knowl |
| `file_type` | string | yes | File type of the knowledge |
| `file_size` | number | yes | File size of the knowledge |
| `file_hash` | string | yes | File hash of the knowledge |
| `file_path` | string | yes | File path of the knowledge |
| `storage_size` | number | yes | Storage size of the knowledge |
| `metadata` | JSON | yes | Metadata of the knowledge |
| `custom_metadata` | JSON | yes | CustomMetadata is user-authored descriptive metadata. It is deliberately separate from Metadata, which contains internal ingestion state and IDs. |
| `last_faq_import_result` | JSON | yes | Last FAQ import result (for FAQ type knowledge only) |
| `created_at` | string(time) | yes | Creation time of the knowledge |
| `updated_at` | string(time) | yes | Last updated time of the knowledge |
| `processed_at` | string(time) | yes | Processed time of the knowledge |
| `error_message` | string | yes | Error message of the knowledge |
| `deleted_at` | DeletedAt | yes | Deletion time of the knowledge |
| `knowledge_base_name` | string | yes | Knowledge base name (not stored in database, populated on query) |

---
### `listKnowledgeChunks` — `GET /chunks/:knowledge_id`

handler: `ListKnowledgeChunks` · `chunk.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `page` | integer |  | page number |
| `page_size` | integer |  | page size |
| `chunk_type` | string (repeatable) |  | chunk types to return; defaults to `text` only (e.g. `?chunk_type=text&chunk_type=image_ocr`) |
| `include_image_text` | boolean |  | when `true`, each returned chunk's image-derived text (OCR, falling back to caption) is spliced into `content` at the image placeholder — needed for scanned / image-only documents whose real text lives on `image_ocr` children |

**Response**:
```json
{ success: true, data: result.Data, total: result.Total, page: result.Page, page_size: result.PageSize }
```
---
### `updateDocumentChunk` — `PUT /chunks/:knowledge_id/:id`

handler: `UpdateChunk` · `chunk.go`

**Body** `UpdateChunkRequest`:
| field | type | req | notes |
|---|---|---|---|
| `content` | string |  |  |
| `is_enabled` | boolean |  |  |
| `expected_revision` | number |  |  |

**Response**: `response`
---
### `listChunkRevisions` — `GET /chunks/:knowledge_id/:id/revisions`

handler: `ListChunkRevisions` · `chunk.go`

**Response**:
```json
{ success: true, data: items }
```
---
### `revertDocumentChunk` — `POST /chunks/:knowledge_id/:id/revert`

handler: `RevertChunk` · `chunk.go`

**Body** `RevertChunkRequest`:
| field | type | req | notes |
|---|---|---|---|
| `revision` | number | yes |  |
| `expected_revision` | number |  |  |

**Response**: `response`
---
### `updateKnowledgeMetadata` — `PUT /knowledge/:id`

handler: `UpdateKnowledge` · `knowledge.go`

**Body** `UpdateKnowledgeRequest`:
| field | type | req | notes |
|---|---|---|---|
| `title` | string |  |  |
| `description` | string |  |  |
| `custom_metadata` | any |  |  |

**Response**:
```json
{ success: true, message: string, data: types.Knowledge }
```
`data` is `Knowledge`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier of the knowledge |
| `tenant_id` | number | yes | Workspace ID |
| `knowledge_base_id` | string | yes | ID of the knowledge base |
| `tags` | *KnowledgeTag[] | yes | Tags holds the tags associated with this knowledge (populated on query, not persisted directly). |
| `type` | string | yes | Type of the knowledge |
| `title` | string | yes | Title of the knowledge |
| `description` | string | yes | Description of the knowledge |
| `source` | string | yes | Source of the knowledge (e.g. URL address for url type, "manual" for manual type) |
| `channel` | string | yes | Channel indicates through which channel the knowledge was ingested (web, api, browser_extension, wechat, etc.) |
| `parse_status` | string | yes | Parse status of the knowledge |
| `pending_subtasks_count` | number | yes | PendingSubtasksCount is the outstanding enrichment subtask count (summary + question + graph chunks). Only meaningful while ParseStatus == "finalizing"; defaults to 0 in any termin |
| `summary_status` | string | yes | Summary status for async summary generation |
| `profile` | KnowledgeProfile |  | Profile is the structured companion of Description: a one-line gist, topic keywords, a document type and one typical question. It is produced by the same model call as the summary  |
| `enable_status` | string | yes | Enable status of the knowledge |
| `embedding_model_id` | string | yes | ID of the embedding model |
| `file_name` | string | yes | File name of the knowledge |
| `folder_path` | string | yes | FolderPath is the canonical relative directory this entry belongs to inside the knowledge base, e.g. "docs/spec" for a folder upload of "docs/spec/design.md". Empty means the knowl |
| `file_type` | string | yes | File type of the knowledge |
| `file_size` | number | yes | File size of the knowledge |
| `file_hash` | string | yes | File hash of the knowledge |
| `file_path` | string | yes | File path of the knowledge |
| `storage_size` | number | yes | Storage size of the knowledge |
| `metadata` | JSON | yes | Metadata of the knowledge |
| `custom_metadata` | JSON | yes | CustomMetadata is user-authored descriptive metadata. It is deliberately separate from Metadata, which contains internal ingestion state and IDs. |
| `last_faq_import_result` | JSON | yes | Last FAQ import result (for FAQ type knowledge only) |
| `created_at` | string(time) | yes | Creation time of the knowledge |
| `updated_at` | string(time) | yes | Last updated time of the knowledge |
| `processed_at` | string(time) | yes | Processed time of the knowledge |
| `error_message` | string | yes | Error message of the knowledge |
| `deleted_at` | DeletedAt | yes | Deletion time of the knowledge |
| `knowledge_base_name` | string | yes | Knowledge base name (not stored in database, populated on query) |

---
### `updateKnowledgeSummary` — `PUT /knowledge/:id`

handler: `UpdateKnowledge` · `knowledge.go`

**Body** `UpdateKnowledgeRequest`:
| field | type | req | notes |
|---|---|---|---|
| `title` | string |  |  |
| `description` | string |  |  |
| `custom_metadata` | any |  |  |

**Response**:
```json
{ success: true, message: string, data: types.Knowledge }
```
`data` is `Knowledge`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier of the knowledge |
| `tenant_id` | number | yes | Workspace ID |
| `knowledge_base_id` | string | yes | ID of the knowledge base |
| `tags` | *KnowledgeTag[] | yes | Tags holds the tags associated with this knowledge (populated on query, not persisted directly). |
| `type` | string | yes | Type of the knowledge |
| `title` | string | yes | Title of the knowledge |
| `description` | string | yes | Description of the knowledge |
| `source` | string | yes | Source of the knowledge (e.g. URL address for url type, "manual" for manual type) |
| `channel` | string | yes | Channel indicates through which channel the knowledge was ingested (web, api, browser_extension, wechat, etc.) |
| `parse_status` | string | yes | Parse status of the knowledge |
| `pending_subtasks_count` | number | yes | PendingSubtasksCount is the outstanding enrichment subtask count (summary + question + graph chunks). Only meaningful while ParseStatus == "finalizing"; defaults to 0 in any termin |
| `summary_status` | string | yes | Summary status for async summary generation |
| `profile` | KnowledgeProfile |  | Profile is the structured companion of Description: a one-line gist, topic keywords, a document type and one typical question. It is produced by the same model call as the summary  |
| `enable_status` | string | yes | Enable status of the knowledge |
| `embedding_model_id` | string | yes | ID of the embedding model |
| `file_name` | string | yes | File name of the knowledge |
| `folder_path` | string | yes | FolderPath is the canonical relative directory this entry belongs to inside the knowledge base, e.g. "docs/spec" for a folder upload of "docs/spec/design.md". Empty means the knowl |
| `file_type` | string | yes | File type of the knowledge |
| `file_size` | number | yes | File size of the knowledge |
| `file_hash` | string | yes | File hash of the knowledge |
| `file_path` | string | yes | File path of the knowledge |
| `storage_size` | number | yes | Storage size of the knowledge |
| `metadata` | JSON | yes | Metadata of the knowledge |
| `custom_metadata` | JSON | yes | CustomMetadata is user-authored descriptive metadata. It is deliberately separate from Metadata, which contains internal ingestion state and IDs. |
| `last_faq_import_result` | JSON | yes | Last FAQ import result (for FAQ type knowledge only) |
| `created_at` | string(time) | yes | Creation time of the knowledge |
| `updated_at` | string(time) | yes | Last updated time of the knowledge |
| `processed_at` | string(time) | yes | Processed time of the knowledge |
| `error_message` | string | yes | Error message of the knowledge |
| `deleted_at` | DeletedAt | yes | Deletion time of the knowledge |
| `knowledge_base_name` | string | yes | Knowledge base name (not stored in database, populated on query) |

---
### `regenerateKnowledgeSummary` — `POST /knowledge/:id/regenerate-summary`

handler: `RegenerateKnowledgeSummary` · `knowledge.go`

**Response**:
```json
{ success: true, data: types.Knowledge }
```
`data` is `Knowledge`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier of the knowledge |
| `tenant_id` | number | yes | Workspace ID |
| `knowledge_base_id` | string | yes | ID of the knowledge base |
| `tags` | *KnowledgeTag[] | yes | Tags holds the tags associated with this knowledge (populated on query, not persisted directly). |
| `type` | string | yes | Type of the knowledge |
| `title` | string | yes | Title of the knowledge |
| `description` | string | yes | Description of the knowledge |
| `source` | string | yes | Source of the knowledge (e.g. URL address for url type, "manual" for manual type) |
| `channel` | string | yes | Channel indicates through which channel the knowledge was ingested (web, api, browser_extension, wechat, etc.) |
| `parse_status` | string | yes | Parse status of the knowledge |
| `pending_subtasks_count` | number | yes | PendingSubtasksCount is the outstanding enrichment subtask count (summary + question + graph chunks). Only meaningful while ParseStatus == "finalizing"; defaults to 0 in any termin |
| `summary_status` | string | yes | Summary status for async summary generation |
| `profile` | KnowledgeProfile |  | Profile is the structured companion of Description: a one-line gist, topic keywords, a document type and one typical question. It is produced by the same model call as the summary  |
| `enable_status` | string | yes | Enable status of the knowledge |
| `embedding_model_id` | string | yes | ID of the embedding model |
| `file_name` | string | yes | File name of the knowledge |
| `folder_path` | string | yes | FolderPath is the canonical relative directory this entry belongs to inside the knowledge base, e.g. "docs/spec" for a folder upload of "docs/spec/design.md". Empty means the knowl |
| `file_type` | string | yes | File type of the knowledge |
| `file_size` | number | yes | File size of the knowledge |
| `file_hash` | string | yes | File hash of the knowledge |
| `file_path` | string | yes | File path of the knowledge |
| `storage_size` | number | yes | Storage size of the knowledge |
| `metadata` | JSON | yes | Metadata of the knowledge |
| `custom_metadata` | JSON | yes | CustomMetadata is user-authored descriptive metadata. It is deliberately separate from Metadata, which contains internal ingestion state and IDs. |
| `last_faq_import_result` | JSON | yes | Last FAQ import result (for FAQ type knowledge only) |
| `created_at` | string(time) | yes | Creation time of the knowledge |
| `updated_at` | string(time) | yes | Last updated time of the knowledge |
| `processed_at` | string(time) | yes | Processed time of the knowledge |
| `error_message` | string | yes | Error message of the knowledge |
| `deleted_at` | DeletedAt | yes | Deletion time of the knowledge |
| `knowledge_base_name` | string | yes | Knowledge base name (not stored in database, populated on query) |

---
### `getChunkByIdOnly` — `GET /chunks/by-id/:id`

handler: `GetChunkByIDOnly` · `chunk.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `include_image_text` | boolean |  | when `true`, the chunk's image-derived text (OCR, falling back to caption) is spliced into `content` at the image placeholder — needed for scanned / image-only documents whose real text lives on `image_ocr` children |

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
### `deleteGeneratedQuestion` — `DELETE /chunks/by-id/:id/questions`

handler: `DeleteGeneratedQuestion` · `chunk.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `question_id` | string | yes |  |

**Response**:
```json
{ success: true, message: string }
```
---
### `upsertGeneratedQuestion` — `PUT /chunks/by-id/:id/questions`

handler: `UpsertGeneratedQuestion` · `chunk.go`

**Body** `UpsertGeneratedQuestionRequest`:
| field | type | req | notes |
|---|---|---|---|
| `question_id` | string |  |  |
| `question` | string | yes |  |

**Response**:
```json
{ success: true, data: types.GeneratedQuestion }
```
`data` is `GeneratedQuestion`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `question` | string | yes |  |
| `content_revision` | number |  |  |

---
### `regenerateGeneratedQuestions` — `POST /chunks/by-id/:id/questions/regenerate`

handler: `RegenerateGeneratedQuestions` · `chunk.go`

**Response**:
```json
{ success: true, data: items }
```
---
### `listKnowledgeTags` — `GET /knowledge-bases/:id/tags`

handler: `ListTags` · `tag.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `page` | integer |  | page number |
| `page_size` | integer |  | page size |
| `keyword` | string |  | keyword search |

**Response**:
```json
{ success: true, data: types.PageResult }
```
`data` is `PageResult`:
| field | type | req | notes |
|---|---|---|---|
| `total` | number | yes |  |
| `page` | number | yes |  |
| `page_size` | number | yes |  |
| `data` | any | yes |  |

---
### `createKnowledgeBaseTag` — `POST /knowledge-bases/:id/tags`

handler: `CreateTag` · `tag.go`

**Body** `createTagRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `color` | string |  |  |
| `sort_order` | number |  |  |

**Response**:
```json
{ success: true, data: types.KnowledgeTag }
```
`data` is `KnowledgeTag`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier of the tag (UUID) |
| `seq_id` | number | yes | SeqID is an auto-increment integer ID for external API usage |
| `tenant_id` | number | yes | Workspace ID |
| `knowledge_base_id` | string | yes | Knowledge base ID that this tag belongs to |
| `name` | string | yes | Tag name, unique within the same knowledge base |
| `color` | string | yes | Optional display color |
| `sort_order` | number | yes | Sort order within the same knowledge base |
| `created_at` | string(time) | yes | Creation time |
| `updated_at` | string(time) | yes | Last updated time |

---
### `updateKnowledgeBaseTag` — `PUT /knowledge-bases/:id/tags/:tag_id`

handler: `UpdateTag` · `tag.go`

**Body** `updateTagRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string |  |  |
| `color` | string |  |  |
| `sort_order` | number |  |  |

**Response**:
```json
{ success: true, data: types.KnowledgeTag }
```
`data` is `KnowledgeTag`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier of the tag (UUID) |
| `seq_id` | number | yes | SeqID is an auto-increment integer ID for external API usage |
| `tenant_id` | number | yes | Workspace ID |
| `knowledge_base_id` | string | yes | Knowledge base ID that this tag belongs to |
| `name` | string | yes | Tag name, unique within the same knowledge base |
| `color` | string | yes | Optional display color |
| `sort_order` | number | yes | Sort order within the same knowledge base |
| `created_at` | string(time) | yes | Creation time |
| `updated_at` | string(time) | yes | Last updated time |

---
### `deleteKnowledgeBaseTag` — `DELETE /knowledge-bases/:id/tags/:tag_id`

handler: `DeleteTag` · `tag.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `force` | boolean |  | force delete |
| `content_only` | boolean |  | delete content only, keep tags |

**Body** `DeleteTagRequest`:
| field | type | req | notes |
|---|---|---|---|
| `exclude_ids` | number[] |  |  |

**Response**:
```json
{ success: true }
```
---
### `updateKnowledgeTagBatch` — `PUT /knowledge/tags`

handler: `UpdateKnowledgeTagBatch` · `knowledge.go`

**Body** `knowledgeTagBatchRequest`:
| field | type | req | notes |
|---|---|---|---|
| `updates` | map[string][]string | yes |  |
| `kb_id` | string |  |  |

**Response**:
```json
{ success: true }
```
---
### `updateFAQEntryTagBatch` — `PUT /knowledge-bases/:id/faq/entries/tags`

handler: `UpdateEntryTagBatch` · `faq.go`

**Body** `faqEntryTagBatchRequest`:
| field | type | req | notes |
|---|---|---|---|
| `updates` | map[int64]*int64 | yes |  |

**Response**:
```json
{ success: true }
```
---
### `listFAQEntries` — `GET /knowledge-bases/:id/faq/entries`

handler: `ListEntries` · `faq.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `page` | integer |  | page number |
| `page_size` | integer |  | page size |
| `tag_id` | integer |  | tag ID filter (seq_id); backward-compatible with the legacy single-tag form |
| `tag_ids` | string |  | tag UUID filter, comma-separated (OR semantics) |
| `keyword` | string |  | keyword search |
| `search_field` | string |  | search fields: standard_question, similar_questions, answers; searches all by default |
| `sort_order` | string |  | sort order: asc (by update time ascending); default is descending |
| `is_enabled` | boolean |  | filter by enabled status; all rows when omitted |

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
### `upsertFAQEntries` — `POST /knowledge-bases/:id/faq/entries`

handler: `UpsertEntries` · `faq.go`

**Body** `FAQBatchUpsertPayload`:
| field | type | req | notes |
|---|---|---|---|
| `entries` | FAQEntryPayload[] | yes |  |
| `mode` | string |  |  |
| `knowledge_id` | string |  |  |
| `task_id` | string |  |  |
| `dry_run` | boolean |  |  |

**Response**:
```json
{ success: true, data: {task_id} }
```
---
### `createFAQEntry` — `POST /knowledge-bases/:id/faq/entry`

handler: `CreateEntry` · `faq.go`

**Body** `FAQEntryPayload`:
| field | type | req | notes |
|---|---|---|---|
| `id` | number |  | ID is optional; used to pin seq_id during data migration (must be < the auto-increment start 100000000) |
| `standard_question` | string | yes |  |
| `similar_questions` | string[] |  |  |
| `negative_questions` | string[] |  |  |
| `answers` | string[] |  |  |
| `answer_strategy` | AnswerStrategy |  |  |
| `tag_id` | number |  |  |
| `tag_name` | string |  |  |
| `is_enabled` | boolean |  |  |
| `is_recommended` | boolean |  |  |

**Response**:
```json
{ success: true, data: types.FAQEntry }
```
`data` is `FAQEntry`:
| field | type | req | notes |
|---|---|---|---|
| `id` | number | yes |  |
| `chunk_id` | string | yes |  |
| `knowledge_id` | string | yes |  |
| `knowledge_base_id` | string | yes |  |
| `tag_id` | number | yes |  |
| `tag_name` | string | yes |  |
| `is_enabled` | boolean | yes |  |
| `is_recommended` | boolean | yes |  |
| `standard_question` | string | yes |  |
| `similar_questions` | string[] | yes |  |
| `negative_questions` | string[] | yes |  |
| `answers` | string[] | yes |  |
| `answer_strategy` | AnswerStrategy | yes |  |
| `index_mode` | FAQIndexMode | yes |  |
| `updated_at` | string(time) | yes |  |
| `created_at` | string(time) | yes |  |
| `score` | number |  |  |
| `match_type` | MatchType |  |  |
| `chunk_type` | ChunkType | yes |  |
| `matched_question` | string |  | MatchedQuestion is the actual question text that was matched in FAQ search Could be the standard question or one of the similar questions |

---
### `updateFAQEntry` — `PUT /knowledge-bases/:id/faq/entries/:entry_id`

handler: `UpdateEntry` · `faq.go`

**Body** `FAQEntryPayload`:
| field | type | req | notes |
|---|---|---|---|
| `id` | number |  | ID is optional; used to pin seq_id during data migration (must be < the auto-increment start 100000000) |
| `standard_question` | string | yes |  |
| `similar_questions` | string[] |  |  |
| `negative_questions` | string[] |  |  |
| `answers` | string[] |  |  |
| `answer_strategy` | AnswerStrategy |  |  |
| `tag_id` | number |  |  |
| `tag_name` | string |  |  |
| `is_enabled` | boolean |  |  |
| `is_recommended` | boolean |  |  |

**Response**:
```json
{ success: true, data: types.FAQEntry }
```
`data` is `FAQEntry`:
| field | type | req | notes |
|---|---|---|---|
| `id` | number | yes |  |
| `chunk_id` | string | yes |  |
| `knowledge_id` | string | yes |  |
| `knowledge_base_id` | string | yes |  |
| `tag_id` | number | yes |  |
| `tag_name` | string | yes |  |
| `is_enabled` | boolean | yes |  |
| `is_recommended` | boolean | yes |  |
| `standard_question` | string | yes |  |
| `similar_questions` | string[] | yes |  |
| `negative_questions` | string[] | yes |  |
| `answers` | string[] | yes |  |
| `answer_strategy` | AnswerStrategy | yes |  |
| `index_mode` | FAQIndexMode | yes |  |
| `updated_at` | string(time) | yes |  |
| `created_at` | string(time) | yes |  |
| `score` | number |  |  |
| `match_type` | MatchType |  |  |
| `chunk_type` | ChunkType | yes |  |
| `matched_question` | string |  | MatchedQuestion is the actual question text that was matched in FAQ search Could be the standard question or one of the similar questions |

---
### `updateFAQEntryFieldsBatch` — `PUT /knowledge-bases/:id/faq/entries/fields`

handler: `UpdateEntryFieldsBatch` · `faq.go`

**Body** `FAQEntryFieldsBatchUpdate`:
| field | type | req | notes |
|---|---|---|---|
| `by_id` | map[int64]FAQEntryFieldsUpdate |  | ByID updates by item ID; key is the item ID (seq_id) |
| `by_tag` | map[int64]FAQEntryFieldsUpdate |  | ByTag batch-updates by tag; key is the tag ID (seq_id) |
| `exclude_ids` | number[] |  | ExcludeIDs lists IDs to exclude in ByTag operations (seq_id) |

**Response**:
```json
{ success: true }
```
---
### `deleteFAQEntries` — `DELETE /knowledge-bases/:id/faq/entries`

handler: `DeleteEntries` · `faq.go`

**Body** `faqDeleteRequest`:
| field | type | req | notes |
|---|---|---|---|
| `ids` | number[] | yes |  |

**Response**:
```json
{ success: true }
```
---
### `searchFAQEntries` — `POST /knowledge-bases/:id/faq/search`

handler: `SearchFAQ` · `faq.go`

**Body** `FAQSearchRequest`:
| field | type | req | notes |
|---|---|---|---|
| `query_text` | string | yes |  |
| `vector_threshold` | number |  |  |
| `match_count` | number |  |  |
| `first_priority_tag_ids` | number[] |  |  |
| `second_priority_tag_ids` | number[] |  |  |
| `only_recommended` | boolean |  |  |

**Response**:
```json
{ success: true, data: entries }
```
---
### `exportFAQEntries` — `GET /knowledge-bases/:id/faq/entries/export`

handler: `ExportEntries` · `faq.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `format` | string |  | export format: csv (default) or json |

**Response**: binary download (file stream, not JSON)
---
### `getFAQImportProgress` — `GET /faq/import/progress/:task_id`

handler: `GetImportProgress` · `faq.go`

**Response**:
```json
{ success: true, data: types.FAQImportProgress }
```
`data` is `FAQImportProgress`:
| field | type | req | notes |
|---|---|---|---|
| `task_id` | string | yes |  |
| `kb_id` | string | yes |  |
| `knowledge_id` | string | yes |  |
| `status` | FAQImportTaskStatus | yes |  |
| `progress` | number | yes |  |
| `total` | number | yes |  |
| `processed` | number | yes |  |
| `success_count` | number | yes |  |
| `failed_count` | number | yes |  |
| `partial_failed_count` | number |  |  |
| `skipped_count` | number |  |  |
| `failed_entries` | FAQFailedEntry[] |  |  |
| `failed_entries_url` | string |  |  |
| `success_entries` | FAQSuccessEntry[] |  |  |
| `valid_entry_indices` | number[] |  |  |
| `merge_entry_indices` | number[] |  |  |
| `merged_count` | number |  |  |
| `added_count` | number |  |  |
| `merge_details` | FAQMergeDetail[] |  |  |
| `message` | string | yes |  |
| `error` | string | yes |  |
| `created_at` | number | yes |  |
| `updated_at` | number | yes |  |
| `dry_run` | boolean |  |  |
| `import_mode` | string |  | Result fields (populated when Status == "completed") |
| `imported_at` | string(time) |  |  |
| `display_status` | string |  |  |
| `processing_time` | number |  |  |

---
### `updateFAQImportResultDisplayStatus` — `PUT /knowledge-bases/:id/faq/import/last-result/display`

handler: `UpdateLastImportResultDisplayStatus` · `faq.go`

**Body** `updateLastFAQImportResultDisplayStatusRequest`:
| field | type | req | notes |
|---|---|---|---|
| `display_status` | string | yes |  |

**Response**:
```json
{ success: true }
```
---
### `searchKnowledge` — `GET /knowledge/search`

handler: `SearchKnowledge` · `qa.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `keyword` | string |  | Keyword to search |
| `offset` | integer |  | Offset for pagination (minimum 0) |
| `limit` | integer |  | Limit for pagination (default 20, maximum 100) |
| `file_types` | string |  | Comma-separated file extensions to filter (e.g., csv,xlsx) |
| `agent_id` | string |  | Shared agent ID (search within agent's KB scope) |
| `recent` | boolean |  | Return recent files when keyword is empty |

**Body** `SearchKnowledgeRequest`:
| field | type | req | notes |
|---|---|---|---|
| `query` | string | yes |  |
| `knowledge_base_id` | string |  |  |
| `knowledge_base_ids` | string[] |  |  |
| `knowledge_ids` | string[] |  |  |
| `tag_ids` | string[] |  |  |
| `mentioned_items` | MentionedItemRequest[] |  |  |

**Response**: streaming (SSE/WebSocket) — see stream.md
---
### `knowledgeSemanticSearch` — `POST /knowledge-search`

handler: `SearchKnowledge` · `qa.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `resource_urls` | string |  | file reference form; "public" returns a loadable direct URL |

**Body** `SearchKnowledgeRequest`:
| field | type | req | notes |
|---|---|---|---|
| `query` | string | yes |  |
| `knowledge_base_id` | string |  |  |
| `knowledge_base_ids` | string[] |  |  |
| `knowledge_ids` | string[] |  |  |
| `tag_ids` | string[] |  |  |
| `mentioned_items` | MentionedItemRequest[] |  |  |

**Response**: streaming (SSE/WebSocket) — see stream.md
---
### `batchReparseKnowledge` — `POST /knowledge/batch-reparse`

handler: `BatchReparseKnowledge` · `knowledge.go`

**Body** `batchReparseKnowledgeRequest`:
| field | type | req | notes |
|---|---|---|---|
| `kb_id` | string | yes |  |
| `ids` | string[] | yes |  |
| `process_config` | KnowledgeProcessOverrides |  |  |

**Response**:
```json
{ success: true, message: string, data: {task_id, reparse_count} }
```
---
### `getTenantRetrievalConfig` — `GET /tenants/kv/:key`

_dispatched via `/tenants/kv/:key` (key `retrieval-config`)_

handler: `GetTenantRetrievalConfig` · `tenant.go`

**Response**:
```json
{ success: true, data: RetrievalConfig }
```
`data` is `RetrievalConfig`:
| field | type | req | notes |
|---|---|---|---|
| `embedding_top_k` | number | yes | EmbeddingTopK is the maximum number of chunks returned by vector search (default: 50) |
| `vector_threshold` | number | yes | VectorThreshold is the minimum vector similarity score (0-1, default: 0.15) |
| `keyword_threshold` | number | yes | KeywordThreshold is the minimum keyword match score (0-1, default: 0.3) |
| `rerank_top_k` | number | yes | RerankTopK is the maximum number of results after reranking (default: 10) |
| `rerank_threshold` | number | yes | RerankThreshold is the minimum rerank score (-10 to 10, default: 0.2) |
| `rerank_model_id` | string | yes | RerankModelID is the ID of the rerank model to use (required for search) |
| `rrf_k` | number |  | RRFK is the smoothing constant of Reciprocal Rank Fusion. Larger values flatten the curve, reducing the bias towards top-1 results. Default: 60. Sensible range: 30..100 depending o |
| `rrf_vector_weight` | number |  | RRFVectorWeight is the weight applied to the vector retriever inside RRF. RRFVectorWeight + RRFKeywordWeight should usually sum to 1.0 but the math works for any positive weights.  |
| `rrf_keyword_weight` | number |  | RRFKeywordWeight is the keyword counterpart. Default: 0.3. |

---
### `updateTenantRetrievalConfig` — `PUT /tenants/kv/:key`

_dispatched via `/tenants/kv/:key` (key `retrieval-config`)_

handler: `updateTenantRetrievalConfigInternal` · `tenant.go`

**Body** `RetrievalConfig`:
| field | type | req | notes |
|---|---|---|---|
| `embedding_top_k` | number |  | EmbeddingTopK is the maximum number of chunks returned by vector search (default: 50) |
| `vector_threshold` | number |  | VectorThreshold is the minimum vector similarity score (0-1, default: 0.15) |
| `keyword_threshold` | number |  | KeywordThreshold is the minimum keyword match score (0-1, default: 0.3) |
| `rerank_top_k` | number |  | RerankTopK is the maximum number of results after reranking (default: 10) |
| `rerank_threshold` | number |  | RerankThreshold is the minimum rerank score (-10 to 10, default: 0.2) |
| `rerank_model_id` | string |  | RerankModelID is the ID of the rerank model to use (required for search) |
| `rrf_k` | number |  | RRFK is the smoothing constant of Reciprocal Rank Fusion. Larger values flatten the curve, reducing the bias towards top-1 results. Default: 60. Sensible range: 30..100 depending o |
| `rrf_vector_weight` | number |  | RRFVectorWeight is the weight applied to the vector retriever inside RRF. RRFVectorWeight + RRFKeywordWeight should usually sum to 1.0 but the math works for any positive weights.  |
| `rrf_keyword_weight` | number |  | RRFKeywordWeight is the keyword counterpart. Default: 0.3. |

**Response**:
```json
{ success: true, data: RetrievalConfig, message: string }
```
`data` is `RetrievalConfig`:
| field | type | req | notes |
|---|---|---|---|
| `embedding_top_k` | number | yes | EmbeddingTopK is the maximum number of chunks returned by vector search (default: 50) |
| `vector_threshold` | number | yes | VectorThreshold is the minimum vector similarity score (0-1, default: 0.15) |
| `keyword_threshold` | number | yes | KeywordThreshold is the minimum keyword match score (0-1, default: 0.3) |
| `rerank_top_k` | number | yes | RerankTopK is the maximum number of results after reranking (default: 10) |
| `rerank_threshold` | number | yes | RerankThreshold is the minimum rerank score (-10 to 10, default: 0.2) |
| `rerank_model_id` | string | yes | RerankModelID is the ID of the rerank model to use (required for search) |
| `rrf_k` | number |  | RRFK is the smoothing constant of Reciprocal Rank Fusion. Larger values flatten the curve, reducing the bias towards top-1 results. Default: 60. Sensible range: 30..100 depending o |
| `rrf_vector_weight` | number |  | RRFVectorWeight is the weight applied to the vector retriever inside RRF. RRFVectorWeight + RRFKeywordWeight should usually sum to 1.0 but the math works for any positive weights.  |
| `rrf_keyword_weight` | number |  | RRFKeywordWeight is the keyword counterpart. Default: 0.3. |

---
### `previewChunking` — `POST /chunker/preview`

handler: `PreviewChunking` · `chunker_debug.go`

**Body** `PreviewChunkingRequest`:
| field | type | req | notes |
|---|---|---|---|
| `text` | string |  |  |
| `chunking_config` | PreviewChunkingPayload |  |  |

**Response** `PreviewChunkingResponse`:
| field | type | req | notes |
|---|---|---|---|
| `selected_tier` | StrategyTier | yes |  |
| `tier_chain` | StrategyTier[] | yes |  |
| `rejected` | TierRejection[] | yes |  |
| `profile` | DocProfile | yes |  |
| `chunks` | PreviewChunkResult[] | yes |  |
| `stats` | PreviewChunkingStats | yes |  |

---

## Referenced types

#### `ASRConfig`
| field | type | req | notes |
|---|---|---|---|
| `enabled` | boolean | yes |  |
| `model_id` | string | yes |  |
| `language` | string | yes |  |

#### `AuditLog`
| field | type | req | notes |
|---|---|---|---|
| `id` | number | yes |  |
| `tenant_id` | number | yes |  |
| `actor_user_id` | string | yes |  |
| `actor_role` | string | yes |  |
| `action` | AuditAction | yes |  |
| `scope_type` | string | yes |  |
| `scope_id` | string | yes |  |
| `target_type` | string | yes |  |
| `target_id` | string | yes |  |
| `target_user_id` | string | yes |  |
| `request_path` | string | yes |  |
| `request_method` | string | yes |  |
| `outcome` | AuditOutcome | yes |  |
| `details` | JSON | yes |  |
| `created_at` | string(time) | yes |  |

#### `AutoTagConfig`
| field | type | req | notes |
|---|---|---|---|
| `enabled` | boolean | yes |  |
| `model_id` | string |  |  |
| `max_tags` | number |  |  |
| `skip_if_tagged` | boolean |  | SkipIfTagged leaves documents that already carry tags untouched, so a deliberate manual classification is not diluted by model guesses. It is a pointer because the default is true: |

#### `ChunkingConfig`
| field | type | req | notes |
|---|---|---|---|
| `chunk_size` | number | yes | Chunk size |
| `chunk_overlap` | number | yes | Chunk overlap |
| `separators` | string[] | yes | Separators |
| `parser_engine_rules` | ParserEngineRule[] |  | ParserEngineRules configures which parser engine to use for each file type. When empty, DefaultParserEngine is used (builtin/simple routing, except types that only a specific engin |
| `enable_parent_child` | boolean |  | EnableParentChild enables two-level parent-child chunking strategy. When enabled, large parent chunks provide context while small child chunks are used for vector matching. Retriev |
| `parent_chunk_size` | number |  | ParentChunkSize is the size of parent chunks (default: 4096). Only used when EnableParentChild is true. |
| `child_chunk_size` | number |  | ChildChunkSize is the size of child chunks used for embedding (default: 384). Only used when EnableParentChild is true. |
| `strategy` | string |  | Strategy selects the adaptive chunking tier. Empty / "legacy" preserves the historical recursive splitter; "auto" lets a profiler pick between heading-aware, heuristic and recursiv |
| `token_limit` | number |  | TokenLimit caps chunk size in approximate tokens. 0 = use ChunkSize as a character count. |
| `languages` | string[] |  | Languages hints the heuristic patterns. Empty = auto-detect from content. Examples: ["de"], ["en", "zh"]. |
| `table_metadata_instructions` | string |  | TableMetadataInstructions contains optional business guidance used when generating searchable summaries for CSV/Excel tables. The system-owned output contract remains fixed; these  |

#### `DocProfile`
| field | type | req | notes |
|---|---|---|---|
| `total_chars` | number | yes |  |
| `total_lines` | number | yes |  |
| `avg_line_len` | number | yes |  |
| `std_line_len` | number | yes |  |
| `md_heading_counts` | map[int]int | yes | Markdown structure |
| `md_heading_total` | number | yes |  |
| `numbered_section_count` | number | yes | Heuristic indicators |
| `all_caps_short_line_count` | number | yes |  |
| `blank_paragraph_breaks` | number | yes |  |
| `form_feed_count` | number | yes |  |
| `visual_sep_count` | number | yes |  |
| `german_chapter_count` | number | yes |  |
| `english_chapter_count` | number | yes |  |
| `chinese_chapter_count` | number | yes |  |
| `repeated_footer_count` | number | yes |  |
| `has_tables` | boolean | yes | Content characteristics |
| `has_code` | boolean | yes |  |
| `code_ratio` | number | yes |  |
| `detected_langs` | string[] | yes | Detected language hints (best-effort) |

#### `ExtractConfig`
| field | type | req | notes |
|---|---|---|---|
| `enabled` | boolean | yes |  |
| `text` | string |  |  |
| `tags` | string[] |  |  |
| `nodes` | *GraphNode[] |  |  |
| `relations` | *GraphRelation[] |  |  |
| `custom_instructions` | string |  | CustomInstructions adds domain-specific extraction guidance while the system keeps ownership of the structured graph output protocol. |

#### `FAQConfig`
| field | type | req | notes |
|---|---|---|---|
| `index_mode` | FAQIndexMode | yes |  |
| `question_index_mode` | FAQQuestionIndexMode | yes |  |

#### `FAQEntryPayload`
| field | type | req | notes |
|---|---|---|---|
| `id` | number |  | ID is optional; used to pin seq_id during data migration (must be < the auto-increment start 100000000) |
| `standard_question` | string | yes |  |
| `similar_questions` | string[] | yes |  |
| `negative_questions` | string[] | yes |  |
| `answers` | string[] | yes |  |
| `answer_strategy` | AnswerStrategy |  |  |
| `tag_id` | number | yes |  |
| `tag_name` | string | yes |  |
| `is_enabled` | boolean |  |  |
| `is_recommended` | boolean |  |  |

#### `FAQFailedEntry`
| field | type | req | notes |
|---|---|---|---|
| `index` | number | yes |  |
| `reason` | string | yes |  |
| `failure_type` | string |  |  |
| `is_partial_failure` | boolean |  |  |
| `tag_name` | string |  |  |
| `standard_question` | string | yes |  |
| `similar_questions` | string[] |  |  |
| `negative_questions` | string[] |  |  |
| `answers` | string[] |  |  |
| `answer_all` | boolean |  |  |
| `is_disabled` | boolean |  |  |
| `removed_similar_questions` | string[] |  | partial failure details (when IsPartialFailure is true) |
| `removed_negative_questions` | string[] |  |  |

#### `FAQMergeDetail`
| field | type | req | notes |
|---|---|---|---|
| `index` | number | yes |  |
| `standard_question` | string | yes |  |
| `answer_changed` | boolean | yes |  |
| `new_similar_count` | number | yes |  |
| `new_negative_count` | number | yes |  |

#### `FAQSuccessEntry`
| field | type | req | notes |
|---|---|---|---|
| `index` | number | yes |  |
| `seq_id` | number | yes |  |
| `tag_id` | number |  |  |
| `tag_name` | string |  |  |
| `standard_question` | string | yes |  |

#### `ImageProcessingConfig`
| field | type | req | notes |
|---|---|---|---|
| `model_id` | string | yes | Model ID |

#### `IndexingStrategy`
| field | type | req | notes |
|---|---|---|---|
| `vector_enabled` | boolean | yes | VectorEnabled enables semantic vector embedding and search |
| `keyword_enabled` | boolean | yes | KeywordEnabled enables keyword-based (BM25) search |
| `wiki_enabled` | boolean | yes | WikiEnabled enables automatic wiki page generation from documents |
| `graph_enabled` | boolean | yes | GraphEnabled enables knowledge graph entity/relation extraction |

#### `KnowledgeBaseConfig`
| field | type | req | notes |
|---|---|---|---|
| `chunking_config` | ChunkingConfig | yes | Chunking configuration |
| `image_processing_config` | ImageProcessingConfig | yes | Image processing configuration |
| `faq_config` | FAQConfig | yes | FAQ configuration (only for FAQ type knowledge bases) |
| `wiki_config` | WikiConfig | yes | Wiki configuration (only for wiki-enabled knowledge bases) |
| `auto_tag_config` | AutoTagConfig | yes | AutoTagConfig controls optional automatic association of existing KB tags. |
| `profile_config` | KnowledgeBaseProfileConfig | yes | ProfileConfig controls optional automatic knowledge-base description generation. nil means "no change" when updating. |
| `indexing_strategy` | IndexingStrategy | yes | IndexingStrategy controls which indexing pipelines are active. nil means "no change" when updating (preserves existing strategy). |

#### `KnowledgeBaseProfile`
| field | type | req | notes |
|---|---|---|---|
| `gist` | string |  | Gist is a short statement of what the knowledge base covers. |
| `topics` | string[] |  | Topics is the merged topic list (at most KnowledgeBaseProfileMaxTopics). |
| `typical_questions` | string[] |  | TypicalQuestions lists questions this knowledge base can answer. |
| `stats` | KnowledgeBaseProfileStats | yes | Stats is the aggregate snapshot the text was generated from. |
| `aggregate_hash` | string |  | AggregateHash identifies the aggregate the gist was generated from. A mismatch with the live aggregate means the text is stale. |
| `status` | string |  | Status is one of the KnowledgeBaseProfileStatus* values. |
| `error` | string |  | Error holds the last generation error when Status is failed. |
| `model_id` | string |  | ModelID records which chat model produced the text. |
| `generated_at` | string(time) |  | GeneratedAt records when the text was produced. |

#### `KnowledgeBaseProfileConfig`
| field | type | req | notes |
|---|---|---|---|
| `enabled` | boolean | yes |  |
| `model_id` | string |  | ModelID overrides the chat model; empty falls back to the KB summary model. |
| `custom_instructions` | string |  | CustomInstructions is appended to the stable system prompt, e.g. the audience the description should address or terms to keep. |

#### `KnowledgeProcessOverrides`
| field | type | req | notes |
|---|---|---|---|
| `summary_enabled` | boolean |  | SummaryEnabled defaults to true when omitted for backward compatibility. |
| `parser_engine_rules` | ParserEngineRule[] |  |  |
| `chunking_config` | ChunkingConfig |  |  |
| `enable_multimodel` | boolean |  |  |
| `vlm_config` | VLMConfig |  |  |
| `asr_config` | ASRConfig |  |  |
| `question_generation_config` | QuestionGenerationConfig |  |  |
| `graph_enabled` | boolean |  |  |
| `extract_config` | ExtractConfig |  |  |
| `parser_engine_overrides` | map[string]string |  | ParserEngineOverrides passes key-value configuration to docreader parsers (e.g. pdf_force_scanned=true). Merged with workspace-level overrides in the parse pipeline; per-upload val |

#### `KnowledgeProfile`
| field | type | req | notes |
|---|---|---|---|
| `gist` | string |  | Gist is a one-line statement of what the document is about. |
| `topics` | string[] |  | Topics lists 3-5 short topic keywords. |
| `doc_type` | string |  | DocType is a coarse label such as "user manual", "meeting notes" or "contract". |
| `typical_question` | string |  | TypicalQuestion is one natural-language question this document answers. |

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

#### `PreviewChunkResult`
| field | type | req | notes |
|---|---|---|---|
| `seq` | number | yes |  |
| `start` | number | yes |  |
| `end` | number | yes |  |
| `size_chars` | number | yes |  |
| `size_tokens_approx` | number | yes |  |
| `context_header` | string |  |  |
| `content` | string | yes |  |

#### `PreviewChunkingPayload`
| field | type | req | notes |
|---|---|---|---|
| `chunk_size` | number | yes |  |
| `chunk_overlap` | number | yes |  |
| `separators` | string[] | yes |  |
| `enable_parent_child` | boolean | yes |  |
| `parent_chunk_size` | number | yes |  |
| `child_chunk_size` | number | yes |  |
| `strategy` | string | yes |  |
| `token_limit` | number | yes |  |
| `languages` | string[] | yes |  |

#### `PreviewChunkingStats`
| field | type | req | notes |
|---|---|---|---|
| `count` | number | yes |  |
| `avg_chars` | number | yes |  |
| `min_chars` | number | yes |  |
| `max_chars` | number | yes |  |
| `stddev_chars` | number | yes |  |
| `truncated_to` | number |  | TruncatedTo, when set, is the original chunk count before the response was truncated to previewMaxChunks for transport. |

#### `QuestionGenerationConfig`
| field | type | req | notes |
|---|---|---|---|
| `enabled` | boolean | yes |  |
| `question_count` | number | yes | Number of questions to generate per chunk (default: 3, max: 10) |
| `custom_instructions` | string |  | CustomInstructions describes the intended audience or question style. It is appended to the stable system question-generation template. |

#### `StorageConfig`
| field | type | req | notes |
|---|---|---|---|
| `secret_id` | string | yes | Secret ID (COS) / Access Key ID (S3, MinIO) |
| `secret_key` | string | yes | Secret Key (COS) / Secret Access Key (S3, MinIO) |
| `region` | string | yes | Region |
| `bucket_name` | string | yes | Bucket Name |
| `app_id` | string | yes | App ID (COS specific) |
| `path_prefix` | string | yes | Path Prefix |
| `provider` | string | yes | Provider: "cos", "minio", "s3" |
| `endpoint` | string |  | Endpoint (S3 specific) - e.g., s3.amazonaws.com, oss-cn-hangzhou.aliyuncs.com |
| `use_ssl` | boolean |  | UseSSL (S3 specific) - whether to use HTTPS |
| `force_path_style` | boolean |  | ForcePathStyle (S3 specific) - whether to use path-style URLs |

#### `StorageProviderConfig`
| field | type | req | notes |
|---|---|---|---|
| `provider` | string | yes |  |

#### `TierRejection`
| field | type | req | notes |
|---|---|---|---|
| `tier` | StrategyTier | yes |  |
| `reason` | string | yes |  |

#### `VLMConfig`
| field | type | req | notes |
|---|---|---|---|
| `enabled` | boolean | yes |  |
| `model_id` | string | yes |  |
| `description_language` | string |  | DescriptionLanguage controls the language used for generated image captions. Empty means follow the document/request language. |
| `custom_instructions` | string |  | CustomInstructions adds KB-specific image interpretation guidance without replacing the system-owned OCR and Markdown output contract. |
| `model_name` | string | yes | legacy-compatible model name |
| `base_url` | string | yes | Base URL |
| `api_key` | string | yes | API Key |
| `interface_type` | string | yes | Interface Type: "ollama" or "openai" |

#### `WikiConfig`
| field | type | req | notes |
|---|---|---|---|
| `synthesis_model_id` | string | yes | SynthesisModelID is the LLM model ID used for wiki page generation and updates |
| `max_pages_per_ingest` | number | yes | MaxPagesPerIngest limits pages created/updated per ingest operation (0 = no limit) |
| `extraction_granularity` | WikiExtractionGranularity |  | ExtractionGranularity controls how many candidate slugs Pass 0 extracts per document. Empty / unknown value is treated as WikiExtractionStandard. |
| `content_instructions` | string |  | ContentInstructions controls tone, structure and emphasis for generated summary/entity/index prose. Citation and merge rules remain system-owned. |
| `extraction_instructions` | string |  | ExtractionInstructions tells candidate extraction which domain concepts to emphasize without replacing the stable JSON/citation protocol. |
| `ingest_batch_size` | number |  | Wiki ingest concurrency is two-level: 1. batch-level: multiple batches per KB run concurrently in the wiki worker pool, capped by IngestMaxInflight (below). 2. batch-internal: with |
| `ingest_map_parallel` | number |  | IngestMapParallel sets the errgroup limit for the Map phase (per-document extraction + summary + chunk citation) WITHIN one batch. 0 falls back to 10. Bound by the LLM provider's c |
| `ingest_reduce_parallel` | number |  | IngestReduceParallel sets the errgroup limit for the Reduce phase (per-slug page write) WITHIN one batch. 0 falls back to 10. Bound by the same LLM concurrency / HTTP pool consider |
| `ingest_max_inflight` | number |  | IngestMaxInflight caps how many ingest batches for THIS KB may run concurrently in the shared wiki worker pool (standard/Redis mode only). 0 falls back to the hard-coded default (4 |
