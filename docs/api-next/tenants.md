# Tenants & Workspace Config API

Frontend module: `frontend-next/lib/api/tenants.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `listAllTenants` — `GET /tenants/all`

handler: `ListAllTenants` · `tenant.go`

**Response**:
```json
{ success: true, data: {items} }
```
---
### `getAPIPrincipalConfig` — `GET /tenants/:id/api-principal-config`

handler: `GetAPIPrincipalConfig` · `tenant.go`

**Response**:
```json
{ success: true, data: apiPrincipalConfigForResponse(tenant.APIPrincipalConfig) }
```
---
### `updateAPIPrincipalConfig` — `PUT /tenants/:id/api-principal-config`

handler: `UpdateAPIPrincipalConfig` · `tenant.go`

**Body** `apiPrincipalConfigRequest`:
| field | type | req | notes |
|---|---|---|---|
| `mode` | APIPrincipalMode |  |  |
| `direct_header_name` | string |  |  |
| `signed_token_header_name` | string |  |  |
| `require_direct_header` | boolean |  |  |
| `hmac_secret` | string |  |  |

**Response**:
```json
{ success: true, data: apiPrincipalConfigForResponse(updatedTenant.APIPrincipalConfig) }
```
---
### `createAPIPrincipalTestToken` — `POST /tenants/:id/api-principal-test-token`

handler: `CreateAPIPrincipalTestToken` · `tenant.go`

**Body** `apiPrincipalTestTokenRequest`:
| field | type | req | notes |
|---|---|---|---|
| `external_user_id` | string |  |  |
| `expires_in_seconds` | number |  |  |

**Response**:
```json
{ success: true, data: apiPrincipalTestTokenResponse{ Token: token, HeaderName: defaultAPIPrincipalTokenHeader, ExpiresInSeconds: int(ttl.Seconds()), ExpiresAtUnix: expiresAt.Unix(), ExternalUserID: externalUserID, } }
```
---
### `listTenantAPIKeys` — `GET /tenants/:id/api-keys`

handler: `ListAPIKeys` · `tenant.go`

**Response**:
```json
{ success: true, data: resp }
```
---
### `createTenantAPIKey` — `POST /tenants/:id/api-keys`

handler: `CreateAPIKey` · `tenant.go`

**Body** `tenantAPIKeyCreateRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string |  |  |
| `full_access` | boolean |  |  |
| `knowledge_base_ids` | string[] |  |  |
| `capabilities` | string[] |  |  |
| `expires_at_unix` | number |  |  |

**Response**:
```json
{ success: true, data: tenantAPIKeyCreateResponse{ tenantAPIKeyResponse: tenantAPIKeyForResponse(result.APIKey), Token: result.Token, } }
```
---
### `updateTenantAPIKey` — `PUT /tenants/:id/api-keys/:key_id`

handler: `UpdateAPIKey` · `tenant.go`

**Body** `tenantAPIKeyUpdateRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string |  |  |
| `full_access` | boolean |  |  |
| `knowledge_base_ids` | string[] |  |  |
| `capabilities` | string[] |  |  |
| `expires_at_unix` | number |  |  |

**Response**:
```json
{ success: true, data: tenantAPIKeyForResponse(updated) }
```
---
### `deleteTenantAPIKey` — `DELETE /tenants/:id/api-keys/:key_id`

handler: `DeleteAPIKey` · `tenant.go`

**Response**:
```json
{ success: true }
```
---
### `updateTenant` — `PUT /tenants/:id`

handler: `UpdateTenant` · `tenant.go`

**Body** `Tenant`:
| field | type | req | notes |
|---|---|---|---|
| `id` | number |  | ID |
| `name` | string |  | Name |
| `description` | string |  | Description |
| `status` | string |  | Status |
| `retriever_engines` | RetrieverEngines |  | Retriever engines |
| `business` | string |  | Business |
| `storage_quota` | number |  | Storage quota (Bytes), default is 10GB, including vector, original file, text, index, etc. |
| `storage_used` | number |  | Storage used (Bytes) |
| `context_config` | ContextConfig |  | Global Context configuration for this workspace (default for all sessions) |
| `web_search_config` | WebSearchConfig |  | Global WebSearch configuration for this workspace |
| `parser_engine_config` | ParserEngineConfig |  | Parser engine config overrides (MinerU endpoint, API key, etc.). Used when parsing documents; overrides env. |
| `credentials` | CredentialsConfig |  | Credentials config: third-party provider credentials (e.g. WeKnoraCloud AppID/AppSecret) |
| `storage_engine_config` | StorageEngineConfig |  | Storage engine config: parameters for Local, MinIO, COS. Used for document/file storage and docreader. |
| `default_storage_backend_id` | string |  | DefaultStorageBackendID is the workspace default concrete storage instance. |
| `chat_history_config` | ChatHistoryConfig |  | Chat history config: knowledge base configuration for indexing and searching chat messages via vector search |
| `retrieval_config` | RetrievalConfig |  | Retrieval config: global search/retrieval parameters shared by knowledge search and message search |
| `memory_config` | MemoryConfig |  | Memory config: workspace switch for cross-session long-term memory |
| `created_at` | string(time) |  | Creation time |
| `updated_at` | string(time) |  | Last updated time |
| `deleted_at` | DeletedAt |  | Deletion time |

**Response**:
```json
{ success: true, data: NewTenantResponse }
```
---
### `deleteTenant` — `DELETE /tenants/:id`

handler: `DeleteTenant` · `tenant.go`

**Response**:
```json
{ success: true, message: string }
```
---
### `createTenant` — `POST /tenants`

handler: `CreateTenant` · `tenant.go`

**Body** `createTenantRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `description` | string |  |  |

**Response**:
```json
{ success: true, data: any }
```
`data` is the created `Tenant`; when legacy auto-create-key is enabled it is a Tenant map plus an extra `api_key` field holding the plaintext token.
---
### `searchTenants` — `GET /tenants/search`

handler: `SearchTenants` · `tenant.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `keyword` | string |  | search keyword |
| `tenant_id` | integer |  | space ID filter |
| `page` | integer |  | page number |
| `page_size` | integer |  | page size |

**Response**:
```json
{ success: true, data: {items, total, page, page_size} }
```
---
### `listMembers` — `GET /tenants/:id/members`

handler: `ListMembers` · `tenant_org.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `q` | string |  | fuzzy filter by email/username |
| `page` | integer |  | page number (1-based) |
| `page_size` | integer |  | page size (max 100) |

**Response**:
```json
{ success: true, data: members }
```
---
### `addMember` — `POST /tenants/:id/members`

handler: `AddMember` · `tenant_org.go`

**Body** `addMemberRequest`:
| field | type | req | notes |
|---|---|---|---|
| `email` | string | yes |  |
| `role` | TenantRole | yes |  |

**Response**:
```json
{ success: true }
```
---
### `updateMemberRole` — `PUT /tenants/:id/members/:user_id`

handler: `UpdateMemberRole` · `tenant_org.go`

**Body** `updateMemberRoleRequest`:
| field | type | req | notes |
|---|---|---|---|
| `role` | TenantRole | yes |  |

**Response**:
```json
{ success: true }
```
---
### `removeMember` — `DELETE /tenants/:id/members/:user_id`

handler: `RemoveMember` · `tenant_org.go`

**Response**:
```json
{ success: true }
```
---
### `leaveTenant` — `POST /tenants/:id/leave`

handler: `LeaveTenant` · `tenant_member.go`

**Response**:
```json
{ success: true }
```
---
### `listTenantInvitations` — `GET /tenants/:id/invitations`

handler: `ListTenantInvitations` · `tenant_invitation.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `include_terminal` | boolean |  | whether to include terminal-state rows; default false |
| `page` | integer |  | page number (1-based) |
| `page_size` | integer |  | page size |

**Response**:
```json
{ success: true, data: {invitations, total, page, page_size} }
```
---
### `createInvitation` — `POST /tenants/:id/invitations`

handler: `CreateInvitation` · `tenant_invitation.go`

**Body** `createInvitationRequest`:
| field | type | req | notes |
|---|---|---|---|
| `email` | string | yes |  |
| `role` | TenantRole | yes |  |
| `message` | string |  |  |

**Response**:
```json
{ success: true, data: resp }
```
---
### `revokeInvitation` — `DELETE /tenants/:id/invitations/:inv_id`

handler: `RevokeInvitation` · `tenant_invitation.go`

**Response**:
```json
{ success: true }
```
---
### `listMyInvitations` — `GET /me/invitations`

handler: `ListMyInvitations` · `tenant_invitation.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `include_terminal` | boolean |  | whether to include terminal rows (processed/expired, etc.); default false |

**Response**:
```json
{ success: true, data: {invitations, total} }
```
---
### `getMyPendingInvitationCount` — `GET /me/invitations/pending-count`

handler: `CountMyPendingInvitations` · `tenant_invitation.go`

**Response**:
```json
{ success: true, data: {pending_count} }
```
---
### `acceptInvitation` — `POST /me/invitations/:inv_id/accept`

handler: `AcceptMyInvitation` · `tenant_invitation.go`

**Response**:
```json
{ success: true, data: {membership} }
```
---
### `acceptInvitationByToken` — `POST /me/invitations/accept-by-token`

handler: `AcceptMyInvitationByToken` · `tenant_invitation.go`

**Body** `acceptInvitationByTokenRequest`:
| field | type | req | notes |
|---|---|---|---|
| `token` | string | yes |  |

**Response**:
```json
{ success: true, data: {membership, tenant_name} }
```
---
### `declineInvitation` — `POST /me/invitations/:inv_id/decline`

handler: `DeclineMyInvitation` · `tenant_invitation.go`

**Response**:
```json
{ success: true }
```
---
### `createInviteLink` — `POST /tenants/:id/invite-links`

handler: `CreateInviteLink` · `tenant_org.go`

**Body** `createInviteLinkRequest`:
| field | type | req | notes |
|---|---|---|---|
| `role` | TenantRole | yes |  |
| `message` | string |  |  |

**Response**:
```json
{ success: true, data: {invitation, url} }
```
---
### `listOrgs` — `GET /orgs`

handler: `ListOrgs` · `tenant_org.go`

**Response**:
```json
{ success: true, data: orgs }
```
---
### `createOrg` — `POST /orgs`

handler: `CreateOrg` · `tenant_org.go`

**Body** `CreateTenantOrgRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `description` | string |  |  |

**Response**:
```json
{ success: true, data: types.TenantOrg }
```
`data` is `TenantOrg`:
| field | type | req | notes |
|---|---|---|---|
| `id` | number | yes |  |
| `tenant_id` | number | yes |  |
| `name` | string | yes |  |
| `description` | string | yes |  |
| `created_by` | string | yes | CreatedBy is the user id that created the org (informational). |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |

---
### `updateOrg` — `PUT /orgs/:id`

handler: `UpdateOrg` · `tenant_org.go`

**Body** `UpdateTenantOrgRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string |  |  |
| `description` | string |  |  |

**Response**:
```json
{ success: true, data: types.TenantOrg }
```
`data` is `TenantOrg`:
| field | type | req | notes |
|---|---|---|---|
| `id` | number | yes |  |
| `tenant_id` | number | yes |  |
| `name` | string | yes |  |
| `description` | string | yes |  |
| `created_by` | string | yes | CreatedBy is the user id that created the org (informational). |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |

---
### `deleteOrg` — `DELETE /orgs/:id`

handler: `DeleteOrg` · `tenant_org.go`

**Response**:
```json
{ success: true }
```
---
### `listOrgMembers` — `GET /orgs/:id/members`

handler: `ListMembers` · `tenant_org.go`

**Response**:
```json
{ success: true, data: members }
```
---
### `addOrgMember` — `POST /orgs/:id/members`

handler: `AddMember` · `tenant_org.go`

**Body** `AddTenantOrgMemberRequest`:
| field | type | req | notes |
|---|---|---|---|
| `user_id` | string | yes |  |
| `role` | TenantOrgRole | yes |  |

**Response**:
```json
{ success: true }
```
---
### `updateOrgMemberRole` — `PUT /orgs/:id/members/:user_id`

handler: `UpdateMemberRole` · `tenant_org.go`

**Body** `UpdateTenantOrgMemberRequest`:
| field | type | req | notes |
|---|---|---|---|
| `role` | TenantOrgRole | yes |  |

**Response**:
```json
{ success: true }
```
---
### `removeOrgMember` — `DELETE /orgs/:id/members/:user_id`

handler: `RemoveMember` · `tenant_org.go`

**Response**:
```json
{ success: true }
```
---
### `createOrgInviteLink` — `POST /orgs/:id/invite-links`

handler: `CreateInviteLink` · `tenant_org.go`

**Body** `CreateTenantOrgInviteRequest`:
| field | type | req | notes |
|---|---|---|---|
| `role` | TenantRole | yes | Role is the tenant role the invitee receives (viewer/contributor recommended; admin/owner invites stay with tenant-level APIs). |
| `message` | string |  |  |

**Response**:
```json
{ success: true, data: {invitation, url} }
```
---

## Referenced types

#### `ChatHistoryConfig`
| field | type | req | notes |
|---|---|---|---|
| `enabled` | boolean | yes | Enabled controls whether chat history indexing is active |
| `embedding_model_id` | string | yes | EmbeddingModelID is the ID of the embedding model used for vectorizing chat messages. Once messages have been indexed, the model cannot be changed (requires re-indexing). |
| `knowledge_base_id` | string | yes | KnowledgeBaseID is the auto-managed hidden knowledge base for chat history. This is set internally when the feature is first enabled; users should not set this directly. |

#### `ContextConfig`
| field | type | req | notes |
|---|---|---|---|
| `max_tokens` | number | yes | Maximum tokens allowed in LLM context |
| `compression_strategy` | ContextCompressionStrategy | yes | Compression strategy: "sliding_window" or "smart" |
| `recent_message_count` | number | yes | For sliding_window: number of messages to keep For smart: number of recent messages to keep uncompressed |
| `summarize_threshold` | number | yes | Summarize threshold: number of messages before summarization |

#### `CredentialsConfig`
| field | type | req | notes |
|---|---|---|---|
| `weknoracloud` | WeKnoraCloudCredentials |  |  |

#### `MemoryConfig`
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

#### `ParserEngineConfig`
| field | type | req | notes |
|---|---|---|---|
| `chat_parser_engine_rules` | ParserEngineRule[] |  | ChatParserEngineRules selects parser engines for session-scoped chat documents. Knowledge bases keep their own rules in ChunkingConfig. |
| `mineru_endpoint` | string | yes |  |
| `mineru_api_key` | string | yes |  |
| `mineru_model` | string |  | MinerU self-hosted parse parameters |
| `mineru_vlm_server_url` | string |  |  |
| `mineru_enable_formula` | boolean |  |  |
| `mineru_enable_table` | boolean |  |  |
| `mineru_parse_method` | string |  |  |
| `mineru_enable_ocr` | boolean |  | MinerUEnableOCR is retained for compatibility with configurations saved before parse_method supported auto/ocr/txt. |
| `mineru_language` | string |  |  |
| `mineru_cloud_model` | string |  | MinerU cloud API parse parameters |
| `mineru_cloud_enable_formula` | boolean |  |  |
| `mineru_cloud_enable_table` | boolean |  |  |
| `mineru_cloud_enable_ocr` | boolean |  |  |
| `mineru_cloud_language` | string |  |  |
| `odl_hybrid` | string |  | OpenDataLoader PDF (docreader engine); hybrid requires opendataloader-pdf-hybrid service. |
| `odl_hybrid_url` | string |  |  |
| `odl_hybrid_mode` | string |  |  |
| `odl_hybrid_fallback` | boolean |  |  |
| `odl_markdown_with_html` | boolean |  |  |
| `paddleocr_vl_endpoint` | string |  | PaddleOCR-VL self-hosted pipeline service (full /layout-parsing API), or an OpenAI-compatible VLM OCR endpoint (vLLM /chat/completions) when PaddleOCRVLEndpoint ends in /v1 or Padd |
| `paddleocr_vl_api` | string |  |  |
| `paddleocr_vl_model` | string |  |  |
| `paddleocr_vl_api_key` | string |  |  |
| `paddleocr_vl_prompt` | string |  |  |
| `paddleocr_vl_vllm_xargs` | string |  | openai mode: "1" sends Unlimited-OCR's ngram vllm_xargs + skip_special_tokens=false; default off for SenOCR-Vi / PaddleOCR-VL. |
| `paddleocr_vl_use_seal_recognition` | boolean |  |  |
| `paddleocr_vl_use_chart_recognition` | boolean |  |  |
| `paddleocr_vl_cloud_token` | string |  | PaddleOCR-VL AI Studio cloud API. |
| `paddleocr_vl_cloud_model` | string |  |  |
| `paddleocr_vl_cloud_use_seal_recognition` | boolean |  |  |
| `paddleocr_vl_cloud_use_chart_recognition` | boolean |  |  |

#### `RetrievalConfig`
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

#### `RetrieverEngines`
| field | type | req | notes |
|---|---|---|---|
| `engines` | RetrieverEngineParams[] | yes |  |

#### `StorageEngineConfig`
| field | type | req | notes |
|---|---|---|---|
| `default_provider` | string | yes |  |
| `local` | LocalEngineConfig |  |  |
| `minio` | MinIOEngineConfig |  |  |
| `cos` | COSEngineConfig |  |  |
| `tos` | TOSEngineConfig |  |  |
| `s3` | S3EngineConfig |  |  |
| `oss` | OSSEngineConfig |  |  |
| `ks3` | KS3EngineConfig |  |  |
| `obs` | OBSEngineConfig |  |  |

#### `WebSearchConfig`
| field | type | req | notes |
|---|---|---|---|
| `provider` | string |  | Deprecated: Use WebSearchProviderEntity.Parameters.APIKey instead. |
| `api_key` | string |  | Deprecated: Use WebSearchProviderEntity.Parameters.APIKey instead. |
| `max_results` | number | yes |  |
| `include_date` | boolean | yes |  |
| `compression_method` | string | yes |  |
| `blacklist` | string[] | yes |  |
| `embedding_model_id` | string |  | RAG compression configuration |
| `embedding_dimension` | number |  |  |
| `rerank_model_id` | string |  |  |
| `document_fragments` | number |  |  |
| `proxy_url` | string |  |  |
