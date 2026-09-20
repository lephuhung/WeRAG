# System Administration API

Frontend module: `frontend-next/lib/api/system.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `listPlatformAPIKeys` — `GET /system/admin/api-keys`

handler: `ListPlatformAPIKeys` · `system.go`

**Response**:
```json
{ success: true, data: response }
```
---
### `createPlatformAPIKey` — `POST /system/admin/api-keys`

handler: `CreatePlatformAPIKey` · `system.go`

**Body** `platformAPIKeyCreateRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string |  |  |
| `capabilities` | string[] |  |  |
| `expires_at_unix` | number |  |  |

**Response**:
```json
{ success: true, data: tenantAPIKeyCreateResponse{tenantAPIKeyResponse: item, Token: result.Token} }
```
---
### `deletePlatformAPIKey` — `DELETE /system/admin/api-keys/:key_id`

handler: `DeletePlatformAPIKey` · `system.go`

**Response**:
```json
{ success: true }
```
---
### `getDeploymentCapabilities` — `GET /system/capabilities`

handler: `GetDeploymentCapabilities` · `deployment_capabilities.go`

**Response**:
```json
{ code: 0, msg: string, data: overlayLiveDockerSandboxCapability(h.deploymentCapabilities) }
```
---
### `getSystemInfo` — `GET /system/info`

handler: `GetSystemInfo` · `system.go`

**Response** `GetSystemInfoResponse`:
| field | type | req | notes |
|---|---|---|---|
| `version` | string | yes |  |
| `edition` | string | yes |  |
| `commit_id` | string |  |  |
| `build_time` | string |  |  |
| `go_version` | string |  |  |
| `keyword_index_engine` | string |  |  |
| `vector_store_engine` | string |  |  |
| `graph_database_engine` | string |  |  |
| `minio_enabled` | boolean |  |  |
| `db_version` | string |  |  |
| `db_migration_error` | string |  | DBMigrationError carries the human-readable error message recorded when the most recent startup migration attempt failed. Empty when migrations succeeded; non-empty values let the  |
| `started_at` | string |  | StartedAt is the server process boot time (RFC3339, UTC). |
| `uptime_seconds` | number |  | UptimeSeconds is seconds elapsed since process start. |

---
### `getPromptTemplates` — `GET /tenants/kv/prompt-templates`

_dispatched via `/tenants/kv/:key` (key `prompt-templates`)_

handler: `GetPromptTemplates` · `tenant.go`

**Response**:
```json
{ success: true, data: localized }
```
---
### `getParserEngines` — `GET /system/parser-engines`

handler: `ListParserEngines` · `system.go`

**Response**:
```json
{ code: 0, msg: string, data: engines, docreader_addr: docreaderAddr, docreader_transport: docreaderTransport, connected: connected }
```
---
### `checkParserEngines` — `POST /system/parser-engines/check`

handler: `CheckParserEngines` · `system.go`

**Body** `ParserEngineConfig`:
| field | type | req | notes |
|---|---|---|---|
| `chat_parser_engine_rules` | ParserEngineRule[] |  | ChatParserEngineRules selects parser engines for session-scoped chat documents. Knowledge bases keep their own rules in ChunkingConfig. |
| `mineru_endpoint` | string |  |  |
| `mineru_api_key` | string |  |  |
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

**Response**:
```json
{ code: 0, msg: string, data: engines, docreader_addr: docreaderAddr, docreader_transport: docreaderTransport, connected: connected }
```
---
### `getParserEngineConfig` — `GET /tenants/kv/:key`

_dispatched via `/tenants/kv/:key` (key `parser-engine-config`)_

handler: `GetTenantParserEngineConfig` · `tenant.go`

**Response**:
```json
{ success: true, data: ParserEngineConfig }
```
`data` is `ParserEngineConfig`:
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

---
### `updateParserEngineConfig` — `PUT /tenants/kv/:key`

_dispatched via `/tenants/kv/:key` (key `parser-engine-config`)_

handler: `updateTenantParserEngineConfigInternal` · `tenant.go`

**Body** `ParserEngineConfig`:
| field | type | req | notes |
|---|---|---|---|
| `chat_parser_engine_rules` | ParserEngineRule[] |  | ChatParserEngineRules selects parser engines for session-scoped chat documents. Knowledge bases keep their own rules in ChunkingConfig. |
| `mineru_endpoint` | string |  |  |
| `mineru_api_key` | string |  |  |
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

**Response**:
```json
{ success: true, data: ParserEngineConfigForResponse, message: string }
```
---
### `reconnectDocReader` — `POST /system/docreader/reconnect`

handler: `ReconnectDocReader` · `system.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `addr` | string | yes |  |

**Response**:
```json
{ code: 0, msg: string, data: engines, docreader_addr: addr, docreader_transport: docreaderTransport, connected: true }
```
---
### `getStorageEngineConfig` — `GET /tenants/kv/:key`

_dispatched via `/tenants/kv/:key` (key `storage-engine-config`)_

handler: `GetTenantStorageEngineConfig` · `tenant.go`

**Response**:
```json
{ success: true, data: StorageEngineConfig }
```
`data` is `StorageEngineConfig`:
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

---
### `updateStorageEngineConfig` — `PUT /tenants/kv/:key`

_dispatched via `/tenants/kv/:key` (key `storage-engine-config`)_

handler: `updateTenantStorageEngineConfigInternal` · `tenant.go`

**Body** `StorageEngineConfig`:
| field | type | req | notes |
|---|---|---|---|
| `default_provider` | string |  |  |
| `local` | LocalEngineConfig |  |  |
| `minio` | MinIOEngineConfig |  |  |
| `cos` | COSEngineConfig |  |  |
| `tos` | TOSEngineConfig |  |  |
| `s3` | S3EngineConfig |  |  |
| `oss` | OSSEngineConfig |  |  |
| `ks3` | KS3EngineConfig |  |  |
| `obs` | OBSEngineConfig |  |  |

**Response**:
```json
{ success: true, data: StorageEngineConfigForResponse, message: string }
```
---
### `getStorageEngineStatus` — `GET /system/storage-engine-status`

handler: `GetStorageEngineStatus` · `system.go`

**Response** `GetStorageEngineStatusResponse`:
| field | type | req | notes |
|---|---|---|---|
| `engines` | StorageEngineStatusItem[] | yes |  |
| `allowed_providers` | string[] | yes |  |
| `minio_env_available` | boolean | yes |  |

---
### `checkStorageEngine` — `POST /system/storage-engine-check`

handler: `CheckStorageEngine` · `system.go`

**Body** `StorageCheckRequest`:
| field | type | req | notes |
|---|---|---|---|
| `provider` | string |  |  |
| `minio` | MinIOEngineConfig |  |  |
| `cos` | COSEngineConfig |  |  |
| `tos` | TOSEngineConfig |  |  |
| `s3` | S3EngineConfig |  |  |
| `oss` | OSSEngineConfig |  |  |
| `ks3` | KS3EngineConfig |  |  |
| `obs` | OBSEngineConfig |  |  |

**Response** `StorageCheckResponse`:
| field | type | req | notes |
|---|---|---|---|
| `ok` | boolean | yes |  |
| `message` | string | yes |  |
| `bucket_created` | boolean |  |  |

---
### `promoteUserToSystemAdmin` — `POST /system/admin/promote`

handler: `PromoteUserToSystemAdmin` · `system.go`

**Body** `PromoteUserToSystemAdminRequest`:
| field | type | req | notes |
|---|---|---|---|
| `user_id` | string |  |  |
| `email` | string |  |  |

**Response** `UserInfo`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `username` | string | yes |  |
| `email` | string | yes |  |
| `avatar` | string | yes |  |
| `tenant_id` | number | yes |  |
| `is_active` | boolean | yes |  |
| `can_access_all_tenants` | boolean | yes |  |
| `is_system_admin` | boolean | yes |  |
| `preferences` | UserPreferences | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |

---
### `revokeSystemAdmin` — `POST /system/admin/revoke`

handler: `RevokeSystemAdmin` · `system.go`

**Body** `RevokeSystemAdminRequest`:
| field | type | req | notes |
|---|---|---|---|
| `user_id` | string | yes |  |

**Response** `UserInfo`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `username` | string | yes |  |
| `email` | string | yes |  |
| `avatar` | string | yes |  |
| `tenant_id` | number | yes |  |
| `is_active` | boolean | yes |  |
| `can_access_all_tenants` | boolean | yes |  |
| `is_system_admin` | boolean | yes |  |
| `preferences` | UserPreferences | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |

---
### `listSystemAdmins` — `GET /system/admin/list`

handler: `ListSystemAdmins` · `system.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `offset` | integer |  | Page offset |
| `limit` | integer |  | Page size (max 200) |

**Response** `ListSystemAdminsResponse`:
| field | type | req | notes |
|---|---|---|---|
| `total` | number | yes |  |
| `admins` | UserInfo[] | yes |  |

---
### `listSystemUsers` — `GET /system/admin/users`

handler: `ListSystemUsers` · `system.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `offset` | string |  |  |
| `limit` | string |  |  |
| `q` | string |  |  |

**Response**: `ListSystemUsersResponse{ Total: total, Users: infos, }`
---
### `updateSystemUserRole` — `PUT /system/admin/tenants/:tenant_id/members/:user_id`

handler: `UpdateSystemUserRole` · `system.go`

**Body** `updateMemberRoleRequest`:
| field | type | req | notes |
|---|---|---|---|
| `role` | TenantRole | yes |  |

**Response**:
```json
{ success: true }
```
---
### `updateSystemOrgTenantRole` — `PUT /system/admin/organizations/:org_id/members/:tenant_id`

handler: `UpdateSystemOrgTenantRole` · `system.go`

**Body** `updateOrgMemberRoleRequest`:
| field | type | req | notes |
|---|---|---|---|
| `role` | OrgMemberRole | yes |  |

**Response**:
```json
{ success: true }
```
---
### `resetUserPassword` — `POST /system/admin/users/reset-password`

handler: `ResetUserPassword` · `system.go`

**Body** `ResetUserPasswordRequest`:
| field | type | req | notes |
|---|---|---|---|
| `email` | string | yes |  |
| `new_password` | string | yes |  |

**Response**:
```json
{ message: string }
```
---
### `createSystemUser` — `POST /system/admin/users/create`

handler: `CreateSystemUser` · `system.go`

**Body** `AdminCreateUserRequest`:
| field | type | req | notes |
|---|---|---|---|
| `username` | string | yes |  |
| `email` | string | yes |  |
| `password` | string |  |  |

**Response** `CreateSystemUserResponse`:
| field | type | req | notes |
|---|---|---|---|
| `user` | UserInfo | yes |  |
| `generated_password` | string |  | GeneratedPassword is the plaintext password when the server auto-generated one. Absent when the caller supplied the password. |

---
### `listSystemSettings` — `GET /system/admin/settings`

handler: `ListSystemSettings` · `system.go`

**Response**: `rows`
---
### `getSystemSetting` — `GET /system/admin/settings/:key`

handler: `GetSystemSetting` · `system.go`

**Response** `SystemSetting`:
| field | type | req | notes |
|---|---|---|---|
| `id` | number | yes |  |
| `key` | string | yes |  |
| `value` | JSON | yes |  |
| `value_type` | string | yes | ValueType is one of "int", "string", "bool". Service layer rejects updates whose payload type does not match; UI uses it to pick InputNumber vs Input vs Switch. |
| `category` | string | yes | Category groups settings in the management UI ("limits", "agent", "auth", ...). Free-form string so adding a new category is a data-only change. |
| `description` | string | yes |  |
| `is_secret` | boolean | yes | IsSecret reserves UI affordances for P3 (mask + reveal-with-confirm). In P1 every row is is_secret=false; service Update accepts the column but does not yet enforce special handlin |
| `requires_restart` | boolean | yes | RequiresRestart reserves UI affordances for P3 (banner "this change won't take effect until the next restart"). In P1 the only seeded key is per-request, so always false. |
| `last_modified_by` | string | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |
| `enum` | string[] |  | Enum is populated by the service layer (NOT persisted) from the in-code registry. Empty/nil means "free-form input"; non-empty means the UI should render a select with these option |
| `last_modified_by_name` | string |  | LastModifiedByName is a display label resolved from LastModifiedBy (the user's UUID) at handler time — username when available, otherwise email. Empty for virtual rows that were ne |

---
### `updateSystemSetting` — `PUT /system/admin/settings/:key`

handler: `UpdateSystemSetting` · `system.go`

**Body** `UpdateSystemSettingRequest`:
| field | type | req | notes |
|---|---|---|---|
| `value` | any |  | Value is intentionally `any` (decoded as float64 / string / bool / etc. by the JSON unmarshaller). Service.encodeForType normalises these against the registry's declared type and r |

**Response** `SystemSetting`:
| field | type | req | notes |
|---|---|---|---|
| `id` | number | yes |  |
| `key` | string | yes |  |
| `value` | JSON | yes |  |
| `value_type` | string | yes | ValueType is one of "int", "string", "bool". Service layer rejects updates whose payload type does not match; UI uses it to pick InputNumber vs Input vs Switch. |
| `category` | string | yes | Category groups settings in the management UI ("limits", "agent", "auth", ...). Free-form string so adding a new category is a data-only change. |
| `description` | string | yes |  |
| `is_secret` | boolean | yes | IsSecret reserves UI affordances for P3 (mask + reveal-with-confirm). In P1 every row is is_secret=false; service Update accepts the column but does not yet enforce special handlin |
| `requires_restart` | boolean | yes | RequiresRestart reserves UI affordances for P3 (banner "this change won't take effect until the next restart"). In P1 the only seeded key is per-request, so always false. |
| `last_modified_by` | string | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |
| `enum` | string[] |  | Enum is populated by the service layer (NOT persisted) from the in-code registry. Empty/nil means "free-form input"; non-empty means the UI should render a select with these option |
| `last_modified_by_name` | string |  | LastModifiedByName is a display label resolved from LastModifiedBy (the user's UUID) at handler time — username when available, otherwise email. Empty for virtual rows that were ne |

---
### `resetSystemSetting` — `DELETE /system/admin/settings/:key`

handler: `ResetSystemSetting` · `system.go`

**Response**:
```json
{ success: true }
```
---
### `applyDefaultStorageQuotaToAllTenants` — `POST /system/admin/tenants/apply-default-storage-quota`

handler: `ApplyDefaultStorageQuotaToAllTenants` · `system.go`

**Response**:
```json
{ affected: int64, quota_bytes: quotaBytes, quota_gb: gb }
```
---
### `listSystemAuditLog` — `GET /system/admin/audit-log`

handler: `ListSystemAuditLog` · `audit_log.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `after_id` | integer |  | cursor: returns rows with id below this value (starts from newest by default) |
| `limit` | integer |  | page size, 1-100, default 50 |
| `action` | string |  | exact filter on action (e.g. system.setting_changed) |
| `outcome` | string |  | exact filter on outcome (success / denied) |
| `actor` | string |  | exact filter on actor_user_id |

**Response** `auditLogListResponse`:
| field | type | req | notes |
|---|---|---|---|
| `success` | boolean | yes |  |
| `data` | AuditLog[] | yes |  |
| `next_cursor` | number | yes |  |

---
### `getRuntimeQueues` — `GET /system/admin/runtime/queues`

handler: `GetRuntimeQueues` · `system.go`

**Response** `RuntimeQueuesResponse`:
| field | type | req | notes |
|---|---|---|---|
| `available` | boolean | yes |  |
| `upstream_concurrency` | number | yes |  |
| `parse_concurrency` | number | yes |  |
| `wiki_concurrency` | number | yes |  |
| `pools` | RuntimeWorkerPool[] | yes |  |
| `queues` | QueueStat[] | yes |  |
| `model_limiter_available` | boolean | yes |  |
| `models` | RuntimeStat[] | yes |  |
| `timestamp` | number | yes |  |

---
### `getRuntimeTasks` — `GET /system/admin/runtime/queues/:queue/tasks`

handler: `ListRuntimeTasks` · `system.go→listRuntimeTasks`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `state` | string | yes | Task state |
| `cursor` | string |  | Opaque continuation cursor |
| `page_size` | integer |  | Page size |

**Response** `RuntimeTasksResponse`:
| field | type | req | notes |
|---|---|---|---|
| `available` | boolean | yes |  |
| `tasks` | RuntimeTaskInfo[] | yes |  |
| `page_size` | number | yes |  |
| `has_more` | boolean | yes |  |
| `next_cursor` | string |  |  |

---
### `mutateRuntimeTask` — `POST /system/admin/runtime/queues/:queue/tasks/:task_id/actions/:action`

handler: `MutateRuntimeTask` · `system.go→mutateRuntimeTask`

**Response**:
```json
{ success: true }
```
---
### `purgeArchivedRuntimeTasks` — `DELETE /system/admin/runtime/queues/:queue/archived`

handler: `PurgeArchivedRuntimeTasks` · `system.go`

**Response**:
```json
{ success: true, deleted: deleted }
```
---
### `listSandboxConfigs` — `GET /sandbox-configs`

handler: `List` · `storagebackend.go`

**Response**:
```json
{ success: true, data: result, default_storage_backend_id: *string }
```
`data` is `result`:
| field | type | req | notes |
|---|---|---|---|
| `KnowledgeBaseID` | string | yes |  |
| `Count` | number | yes |  |

---
### `setSandboxWorkspacePolicy` — `PUT /sandbox-configs/workspace-policy`

handler: `SetWorkspacePolicy` · `sandbox_config.go`

**Body** `workspacePolicyRequest`:
| field | type | req | notes |
|---|---|---|---|
| `scripts_disabled` | boolean |  |  |

**Response**:
```json
{ success: true, workspace_scripts_disabled: boolean }
```
---
### `createSandboxConfig` — `POST /sandbox-configs`

handler: `Create` · `storagebackend.go`

**Body** `sandboxConfigRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `description` | string |  |  |
| `config` | TenantSandboxConfig |  |  |

**Response**:
```json
{ success: true, data: NewStorageBackendResponse }
```
---
### `getSandboxConfigById` — `GET /sandbox-configs/:id`

handler: `Get` · `storagebackend.go`

**Response**:
```json
{ success: true, data: NewStorageBackendResponse }
```
---
### `updateSandboxConfigById` — `PUT /sandbox-configs/:id`

handler: `Update` · `storagebackend.go`

**Body** `sandboxConfigRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `description` | string |  |  |
| `config` | TenantSandboxConfig |  |  |

**Response**:
```json
{ success: true, data: NewStorageBackendResponse }
```
---
### `deleteSandboxConfig` — `DELETE /sandbox-configs/:id`

handler: `Delete` · `storagebackend.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `force` | boolean |  | Force delete when inventory is unverifiable |

**Response**:
```json
{ success: true }
```
---
### `getSandboxConfigInventory` — `GET /sandbox-configs/:id/sandboxes`

handler: `Inventory` · `sandbox_config.go`

**Response**:
```json
{ success: true, data: inv }
```
---
### `querySandboxTemplates` — `POST /sandbox-configs/templates/query`

handler: `QueryTemplates` · `sandbox_config.go`

**Body** `sandboxTemplateQueryRequest`:
| field | type | req | notes |
|---|---|---|---|
| `config` | TenantSandboxConfig |  |  |
| `config_id` | string |  |  |
| `ensure_standard` | boolean |  |  |
| `replace_standard` | boolean |  |  |
| `ensure_desktop` | boolean |  |  |
| `replace_desktop` | boolean |  |  |

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
### `checkSandboxConfig` — `POST /system/sandbox-check`

handler: `CheckSandboxConfig` · `sandbox_check.go`

**Body** `SandboxCheckRequest`:
| field | type | req | notes |
|---|---|---|---|
| `config` | TenantSandboxConfig |  |  |
| `config_id` | string |  | ConfigID lets an edit form test stored credentials while overriding only the fields the admin changed in the drawer. |
| `deep` | boolean |  | Deep additionally runs a throwaway script. For remote backends this also creates and destroys one sandbox, which is the only way to validate the template ID, data plane, in-sandbox |

**Response** `SandboxCheckResponse`:
| field | type | req | notes |
|---|---|---|---|
| `ok` | boolean | yes |  |
| `provider` | string | yes |  |
| `checks` | SandboxCheckItem[] | yes |  |
| `capabilities` | map[string]bool |  |  |

---
### `listConfigSkills` — `GET /sandbox-configs/:id/skills`

handler: `List` · `storagebackend.go`

**Response**:
```json
{ success: true, data: result, default_storage_backend_id: *string }
```
`data` is `result`:
| field | type | req | notes |
|---|---|---|---|
| `KnowledgeBaseID` | string | yes |  |
| `Count` | number | yes |  |

---
### `uploadConfigSkill` — `POST /sandbox-configs/:id/skills`

handler: `Upload` · `sandbox_skill.go`

**Multipart form**:
| name | type | req | notes |
|---|---|---|---|
| `file` | file |  |  |

**Body** `skillSourceRequest`:
| field | type | req | notes |
|---|---|---|---|
| `source` | string |  | Source is exactly one of: "@owner/slug" or a slash-free slug (ClawHub), a github.com / gitlab.com / skills.sh / clawhub / skillhub page URL, a ClawHub skills-sh catalog page or "sk |

**Response**:
```json
{ success: true, data: {skill_id} }
```
---
### `installConfigSkillFromSource` — `POST /sandbox-configs/:id/skills`

handler: `Upload` · `sandbox_skill.go`

**Multipart form**:
| name | type | req | notes |
|---|---|---|---|
| `file` | file |  |  |

**Body** `skillSourceRequest`:
| field | type | req | notes |
|---|---|---|---|
| `source` | string |  | Source is exactly one of: "@owner/slug" or a slash-free slug (ClawHub), a github.com / gitlab.com / skills.sh / clawhub / skillhub page URL, a ClawHub skills-sh catalog page or "sk |

**Response**:
```json
{ success: true, data: {skill_id} }
```
---
### `reinstallConfigSkill` — `POST /sandbox-configs/:id/skills/:skillId/reinstall`

handler: `Reinstall` · `sandbox_skill.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `instructions` | string |  |  |

**Response**:
```json
{ success: true, data: {skill_id} }
```
---
### `stopConfigSkill` — `POST /sandbox-configs/:id/skills/:skillId/stop`

handler: `Stop` · `sandbox_skill.go`

**Response**:
```json
{ success: true, data: toSkillResponse(skill) }
```
---
### `patchConfigSkill` — `PATCH /sandbox-configs/:id/skills/:skillId`

handler: `Patch` · `sandbox_skill.go`

**Body** `skillPatchRequest`:
| field | type | req | notes |
|---|---|---|---|
| `enabled` | boolean |  | Enabled is a pointer because its absence is not a request to disable the skill; a body may carry envs instead. |
| `envs` | map[string]string |  | Envs is a pointer to a map because "sent an empty object" and "did not mention envs" are different requests: the first clears what it names, the second must leave every stored valu |

**Response**:
```json
{ success: true, data: toSkillResponse(updated) }
```
---
### `deleteConfigSkill` — `DELETE /sandbox-configs/:id/skills/:skillId`

handler: `Delete` · `storagebackend.go`

**Response**:
```json
{ success: true }
```
---
### `getConfigSkill` — `GET /sandbox-configs/:id/skills/:skillId`

handler: `Get` · `storagebackend.go`

**Response**:
```json
{ success: true, data: NewStorageBackendResponse }
```
---
### `listConfigSkillFiles` — `GET /sandbox-configs/:id/skills/:skillId/files`

handler: `ListFiles` · `sandbox_skill.go`

**Response**:
```json
{ success: true, data: files }
```
---
### `getConfigSkillFile` — `GET /sandbox-configs/:id/skills/:skillId/files/content`

handler: `GetFile` · `sandbox_skill.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `path` | string | yes | Skill-root-relative file path |

**Response**:
```json
{ success: true, data: string }
```
---
### `getConfigSkillGuidance` — `GET /sandbox-configs/:id/skills/:skillId/guidance`

handler: `InstallGuidance` · `sandbox_skill.go`

**Response**:
```json
{ success: true, data: state }
```
---

## Referenced types

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

#### `COSEngineConfig`
| field | type | req | notes |
|---|---|---|---|
| `secret_id` | string | yes |  |
| `secret_key` | string | yes |  |
| `region` | string | yes |  |
| `bucket_name` | string | yes |  |
| `app_id` | string | yes |  |
| `path_prefix` | string | yes |  |
| `temp_bucket_name` | string | yes |  |
| `temp_region` | string | yes |  |

#### `KS3EngineConfig`
| field | type | req | notes |
|---|---|---|---|
| `endpoint` | string | yes |  |
| `region` | string | yes |  |
| `access_key` | string | yes |  |
| `secret_key` | string | yes |  |
| `bucket_name` | string | yes |  |
| `path_prefix` | string | yes |  |

#### `LocalEngineConfig`
| field | type | req | notes |
|---|---|---|---|
| `path_prefix` | string | yes |  |

#### `MinIOEngineConfig`
| field | type | req | notes |
|---|---|---|---|
| `mode` | string | yes |  |
| `endpoint` | string | yes |  |
| `access_key_id` | string | yes |  |
| `secret_access_key` | string | yes |  |
| `bucket_name` | string | yes |  |
| `use_ssl` | boolean | yes |  |
| `path_prefix` | string | yes |  |

#### `OBSEngineConfig`
| field | type | req | notes |
|---|---|---|---|
| `endpoint` | string | yes |  |
| `region` | string | yes |  |
| `access_key` | string | yes |  |
| `secret_key` | string | yes |  |
| `bucket_name` | string | yes |  |
| `path_prefix` | string | yes |  |
| `use_ssl` | boolean | yes |  |

#### `OSSEngineConfig`
| field | type | req | notes |
|---|---|---|---|
| `endpoint` | string | yes |  |
| `region` | string | yes |  |
| `access_key` | string | yes |  |
| `secret_key` | string | yes |  |
| `bucket_name` | string | yes |  |
| `path_prefix` | string | yes |  |
| `use_temp_bucket` | boolean | yes |  |
| `temp_bucket_name` | string | yes |  |
| `temp_region` | string | yes |  |

#### `ParserEngineRule`
| field | type | req | notes |
|---|---|---|---|
| `file_types` | string[] | yes |  |
| `engine` | string | yes |  |
| `xlsx_first_row_as_header` | boolean |  | XLSXFirstRowAsHeader restores row-1 column context for flat XLSX tables. nil preserves the parser default; an explicit false disables the mode. |

#### `QueueStat`
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `pool` | string | yes | Pool is the independent worker pool that drains this queue. |
| `weight` | number | yes | Weight is the queue's scheduling weight inside its pool. |
| `size` | number | yes | Size is the total number of tasks in the queue (pending + active + scheduled + retry + aggregating + archived). |
| `pending` | number | yes |  |
| `active` | number | yes |  |
| `scheduled` | number | yes |  |
| `retry` | number | yes |  |
| `archived` | number | yes |  |
| `completed` | number | yes |  |
| `processed` | number | yes | Processed / Failed are today's counters (reset daily). |
| `failed` | number | yes |  |
| `paused` | boolean | yes | Paused reports whether the queue is paused (tasks not consumed). |
| `latency_ms` | number | yes | LatencyMs is the age of the oldest pending task, in milliseconds. |
| `memory_usage_bytes` | number | yes | MemoryUsageBytes is the approximate Redis memory the queue occupies. |

#### `RuntimeStat`
| field | type | req | notes |
|---|---|---|---|
| `model_id` | string | yes |  |
| `name` | string | yes |  |
| `active` | number | yes |  |
| `waiting` | number | yes |  |
| `limit` | number | yes |  |

#### `RuntimeTaskInfo`
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `queue` | string | yes |  |
| `type` | string | yes |  |
| `state` | RuntimeTaskState | yes |  |
| `allowed_actions` | RuntimeTaskAction[] | yes |  |
| `last_error` | string |  |  |
| `last_failed_at` | string(time) |  |  |
| `next_process_at` | string(time) |  |  |
| `started_at` | string(time) |  |  |
| `completed_at` | string(time) |  |  |
| `deadline` | string(time) |  |  |
| `enqueued_at` | string(time) |  |  |
| `retried` | number | yes |  |
| `max_retry` | number | yes |  |
| `is_orphaned` | boolean |  |  |
| `worker` | string |  |  |
| `tenant_id` | number |  |  |
| `knowledge_base_id` | string |  |  |
| `knowledge_id` | string |  |  |
| `task_id` | string |  |  |
| `source_id` | string |  |  |
| `target_id` | string |  |  |
| `source_kb_id` | string |  |  |
| `target_kb_id` | string |  |  |
| `data_source_id` | string |  |  |
| `sync_log_id` | string |  |  |
| `knowledge_count` | number |  |  |

#### `RuntimeWorkerPool`
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `concurrency` | number | yes |  |
| `queue_count` | number | yes |  |
| `instances` | number | yes |  |
| `cluster_capacity` | number | yes |  |
| `active` | number | yes |  |
| `utilization` | number | yes |  |

#### `S3EngineConfig`
| field | type | req | notes |
|---|---|---|---|
| `endpoint` | string | yes |  |
| `region` | string | yes |  |
| `access_key` | string | yes |  |
| `secret_key` | string | yes |  |
| `bucket_name` | string | yes |  |
| `path_prefix` | string | yes |  |
| `use_ssl` | boolean | yes |  |
| `force_path_style` | boolean | yes |  |

#### `SandboxCheckItem`
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `ok` | boolean | yes |  |
| `message` | string |  | Message carries free-form provider detail for an executed probe. |
| `reason` | string |  | Reason is a stable code explaining why a probe was skipped. It exists so the UI can phrase the skip in the operator's language instead of echoing a server-side sentence. |
| `latency_ms` | number |  |  |

#### `StorageEngineStatusItem`
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `allowed` | boolean | yes |  |
| `available` | boolean | yes |  |
| `description` | string | yes |  |

#### `TOSEngineConfig`
| field | type | req | notes |
|---|---|---|---|
| `endpoint` | string | yes |  |
| `region` | string | yes |  |
| `access_key` | string | yes |  |
| `secret_key` | string | yes |  |
| `bucket_name` | string | yes |  |
| `path_prefix` | string | yes |  |
| `temp_bucket_name` | string | yes |  |
| `temp_region` | string | yes |  |

#### `TenantSandboxConfig`
| field | type | req | notes |
|---|---|---|---|
| `sandbox_type` | string |  | SandboxType is cube, e2b, or docker; disabled is the hidden policy row. |
| `default_timeout_sec` | number |  | ── shared config (applies across backends) ─── DefaultTimeoutSec is the per-execution timeout in seconds. 0 uses the program's built-in default. |
| `terminal_idle_disconnect_sec` | number |  | TerminalIdleDisconnectSec is how long an interactive terminal or desktop may go without user activity before WeKnora closes the connection so the sandbox can pause on its provider  |
| `desktop_enabled` | boolean |  | DesktopEnabled declares that this config's base template is a desktop image (XFCE + x11vnc + websockify). It is NOT a second template: a config has exactly one boot target, and ski |
| `allow_private_endpoints` | boolean |  | AllowPrivateEndpoints permits this workspace config to reach RFC1918 or loopback cluster endpoints. Link-local/cloud-metadata addresses remain blocked. It is explicit in the UI ins |
| `env_vars` | map[string]string |  | EnvVars are additional environment variables injected into every sandbox created for this tenant. 🔒 Values are encrypted at rest. These become visible to all scripts running in the |
| `volume_mount` | VolumeMountConfig |  | VolumeMount configures an optional shared volume mounted into every sandbox created for this tenant. Currently used for tenant-installed skills, but the configuration itself is ski |
| `skill_image` | SkillImageConfig |  | SkillImage points at the snapshot that carries this config's installed skills. Empty means "use the base template". Written only by the skill install/remove path: MergeSandboxConfi |
| `skill_rollout` | string |  | SkillRollout decides whether sessions that already hold a sandbox of this config rebuild after a skill install or removal. Empty and SkillRolloutNextTurn rebuild on the next chat t |
| `network` | SandboxNetworkPolicy |  | Network is the outbound/inbound network policy applied to every sandbox created from this config — chat sessions, skill installs and deep connectivity probes alike. nil and the zer |
| `cube` | CubeSandboxConfig |  | ── backend-specific config (only one is active at a time, decided by SandboxType) ─── |
| `e2b` | E2BSandboxConfig |  |  |
| `docker` | DockerSandboxConfig |  |  |

#### `UserInfo`
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `username` | string | yes |  |
| `email` | string | yes |  |
| `avatar` | string | yes |  |
| `tenant_id` | number | yes |  |
| `is_active` | boolean | yes |  |
| `can_access_all_tenants` | boolean | yes |  |
| `is_system_admin` | boolean | yes |  |
| `preferences` | UserPreferences | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |

#### `UserPreferences`
| field | type | req | notes |
|---|---|---|---|
| `browser_search_instructions` | string |  | BrowserSearchInstructions customizes browser search for this user. Nil/empty uses the platform default. |
| `last_active_tenant_id` | number |  | LastActiveTenantID remembers the last workspace the user actively switched into, so a fresh login (new device, cleared browser, new refresh token) lands them back in that workspace |
| `oidc_only_login` | boolean |  | OidcOnlyLogin is set server-side when an account is auto-provisioned via OIDC with a random password the user never received. The profile UI hides self-service password rotation un |
| `language` | string |  | Language is the UI locale this user last picked (e.g. "en", "vi"). Persisted so the choice follows the account across browsers and devices instead of living in one browser's localS |
