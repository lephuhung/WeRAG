# Remote Datasources API

Frontend module: `frontend-next/lib/api/datasource.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `getConnectorTypes` — `GET /datasource/types`

handler: `GetAvailableConnectors` · `datasource.go`

**Response**: `ConnectorMetadata[]`
| field | type | req | notes |
|---|---|---|---|
| `type` | string | yes |  |
| `name` | string | yes |  |
| `description` | string | yes |  |
| `icon` | string |  |  |
| `priority` | number | yes |  |
| `auth_type` | string | yes |  |
| `capabilities` | string[] | yes |  |

---
### `listDataSources` — `GET /datasource`

handler: `ListDataSources` · `datasource.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `kb_id` | string | yes | Knowledge base ID |

**Response**: `DataSource[]`
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier |
| `tenant_id` | number | yes | Workspace ID for multi-workspace isolation |
| `knowledge_base_id` | string | yes | Target knowledge base ID |
| `name` | string | yes | User-friendly name |
| `type` | string | yes | Connector type (feishu, notion, confluence, etc.) |
| `config` | JSON | yes | Encrypted configuration (API credentials, tokens, etc.) Stored as JSON with AES-256-GCM encryption |
| `sync_schedule` | string | yes | Cron expression for scheduled syncs (e.g., "0 */6 * * *" = every 6 hours) |
| `sync_mode` | string | yes | Sync mode: "incremental" (recommended) or "full" |
| `status` | string | yes | Current status: active, paused, error |
| `conflict_strategy` | string | yes | Conflict resolution strategy: overwrite or skip |
| `sync_deletions` | boolean | yes | Whether to sync deletions from source |
| `last_sync_at` | string(time) | yes | Last successful sync timestamp |
| `last_sync_cursor` | JSON | yes | Cursor or state for incremental sync (connector-specific) |
| `last_sync_result` | JSON | yes | Summary of last sync result |
| `error_message` | string | yes | Error message if status is "error" |
| `sync_log_retention_days` | number | yes | Number of days to keep sync logs (default: 30) |
| `created_at` | string(time) | yes | Creation timestamp |
| `updated_at` | string(time) | yes | Last update timestamp |
| `deleted_at` | DeletedAt | yes | Soft delete timestamp |
| `total_items_synced` | number | yes | Total items synced (not stored in DB, calculated on query) |
| `latest_sync_log` | SyncLog | yes | Latest sync log (not stored in DB, populated on query) |

---
### `getDataSource` — `GET /datasource/:id`

handler: `GetDataSource` · `datasource.go`

**Response** `DataSource`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier |
| `tenant_id` | number | yes | Workspace ID for multi-workspace isolation |
| `knowledge_base_id` | string | yes | Target knowledge base ID |
| `name` | string | yes | User-friendly name |
| `type` | string | yes | Connector type (feishu, notion, confluence, etc.) |
| `config` | JSON | yes | Encrypted configuration (API credentials, tokens, etc.) Stored as JSON with AES-256-GCM encryption |
| `sync_schedule` | string | yes | Cron expression for scheduled syncs (e.g., "0 */6 * * *" = every 6 hours) |
| `sync_mode` | string | yes | Sync mode: "incremental" (recommended) or "full" |
| `status` | string | yes | Current status: active, paused, error |
| `conflict_strategy` | string | yes | Conflict resolution strategy: overwrite or skip |
| `sync_deletions` | boolean | yes | Whether to sync deletions from source |
| `last_sync_at` | string(time) | yes | Last successful sync timestamp |
| `last_sync_cursor` | JSON | yes | Cursor or state for incremental sync (connector-specific) |
| `last_sync_result` | JSON | yes | Summary of last sync result |
| `error_message` | string | yes | Error message if status is "error" |
| `sync_log_retention_days` | number | yes | Number of days to keep sync logs (default: 30) |
| `created_at` | string(time) | yes | Creation timestamp |
| `updated_at` | string(time) | yes | Last update timestamp |
| `deleted_at` | DeletedAt | yes | Soft delete timestamp |
| `total_items_synced` | number | yes | Total items synced (not stored in DB, calculated on query) |
| `latest_sync_log` | SyncLog | yes | Latest sync log (not stored in DB, populated on query) |

---
### `createDataSource` — `POST /datasource`

handler: `CreateDataSource` · `datasource.go`

**Body** `DataSource`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string |  | Unique identifier |
| `tenant_id` | number |  | Workspace ID for multi-workspace isolation |
| `knowledge_base_id` | string |  | Target knowledge base ID |
| `name` | string |  | User-friendly name |
| `type` | string |  | Connector type (feishu, notion, confluence, etc.) |
| `config` | JSON |  | Encrypted configuration (API credentials, tokens, etc.) Stored as JSON with AES-256-GCM encryption |
| `sync_schedule` | string |  | Cron expression for scheduled syncs (e.g., "0 */6 * * *" = every 6 hours) |
| `sync_mode` | string |  | Sync mode: "incremental" (recommended) or "full" |
| `status` | string |  | Current status: active, paused, error |
| `conflict_strategy` | string |  | Conflict resolution strategy: overwrite or skip |
| `sync_deletions` | boolean |  | Whether to sync deletions from source |
| `last_sync_at` | string(time) |  | Last successful sync timestamp |
| `last_sync_cursor` | JSON |  | Cursor or state for incremental sync (connector-specific) |
| `last_sync_result` | JSON |  | Summary of last sync result |
| `error_message` | string |  | Error message if status is "error" |
| `sync_log_retention_days` | number |  | Number of days to keep sync logs (default: 30) |
| `created_at` | string(time) |  | Creation timestamp |
| `updated_at` | string(time) |  | Last update timestamp |
| `deleted_at` | DeletedAt |  | Soft delete timestamp |
| `total_items_synced` | number |  | Total items synced (not stored in DB, calculated on query) |
| `latest_sync_log` | SyncLog |  | Latest sync log (not stored in DB, populated on query) |

**Response** `DataSource`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier |
| `tenant_id` | number | yes | Workspace ID for multi-workspace isolation |
| `knowledge_base_id` | string | yes | Target knowledge base ID |
| `name` | string | yes | User-friendly name |
| `type` | string | yes | Connector type (feishu, notion, confluence, etc.) |
| `config` | JSON | yes | Encrypted configuration (API credentials, tokens, etc.) Stored as JSON with AES-256-GCM encryption |
| `sync_schedule` | string | yes | Cron expression for scheduled syncs (e.g., "0 */6 * * *" = every 6 hours) |
| `sync_mode` | string | yes | Sync mode: "incremental" (recommended) or "full" |
| `status` | string | yes | Current status: active, paused, error |
| `conflict_strategy` | string | yes | Conflict resolution strategy: overwrite or skip |
| `sync_deletions` | boolean | yes | Whether to sync deletions from source |
| `last_sync_at` | string(time) | yes | Last successful sync timestamp |
| `last_sync_cursor` | JSON | yes | Cursor or state for incremental sync (connector-specific) |
| `last_sync_result` | JSON | yes | Summary of last sync result |
| `error_message` | string | yes | Error message if status is "error" |
| `sync_log_retention_days` | number | yes | Number of days to keep sync logs (default: 30) |
| `created_at` | string(time) | yes | Creation timestamp |
| `updated_at` | string(time) | yes | Last update timestamp |
| `deleted_at` | DeletedAt | yes | Soft delete timestamp |
| `total_items_synced` | number | yes | Total items synced (not stored in DB, calculated on query) |
| `latest_sync_log` | SyncLog | yes | Latest sync log (not stored in DB, populated on query) |

---
### `updateDataSource` — `PUT /datasource/:id`

handler: `UpdateDataSource` · `datasource.go`

**Body** `DataSource`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string |  | Unique identifier |
| `tenant_id` | number |  | Workspace ID for multi-workspace isolation |
| `knowledge_base_id` | string |  | Target knowledge base ID |
| `name` | string |  | User-friendly name |
| `type` | string |  | Connector type (feishu, notion, confluence, etc.) |
| `config` | JSON |  | Encrypted configuration (API credentials, tokens, etc.) Stored as JSON with AES-256-GCM encryption |
| `sync_schedule` | string |  | Cron expression for scheduled syncs (e.g., "0 */6 * * *" = every 6 hours) |
| `sync_mode` | string |  | Sync mode: "incremental" (recommended) or "full" |
| `status` | string |  | Current status: active, paused, error |
| `conflict_strategy` | string |  | Conflict resolution strategy: overwrite or skip |
| `sync_deletions` | boolean |  | Whether to sync deletions from source |
| `last_sync_at` | string(time) |  | Last successful sync timestamp |
| `last_sync_cursor` | JSON |  | Cursor or state for incremental sync (connector-specific) |
| `last_sync_result` | JSON |  | Summary of last sync result |
| `error_message` | string |  | Error message if status is "error" |
| `sync_log_retention_days` | number |  | Number of days to keep sync logs (default: 30) |
| `created_at` | string(time) |  | Creation timestamp |
| `updated_at` | string(time) |  | Last update timestamp |
| `deleted_at` | DeletedAt |  | Soft delete timestamp |
| `total_items_synced` | number |  | Total items synced (not stored in DB, calculated on query) |
| `latest_sync_log` | SyncLog |  | Latest sync log (not stored in DB, populated on query) |

**Response** `DataSource`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier |
| `tenant_id` | number | yes | Workspace ID for multi-workspace isolation |
| `knowledge_base_id` | string | yes | Target knowledge base ID |
| `name` | string | yes | User-friendly name |
| `type` | string | yes | Connector type (feishu, notion, confluence, etc.) |
| `config` | JSON | yes | Encrypted configuration (API credentials, tokens, etc.) Stored as JSON with AES-256-GCM encryption |
| `sync_schedule` | string | yes | Cron expression for scheduled syncs (e.g., "0 */6 * * *" = every 6 hours) |
| `sync_mode` | string | yes | Sync mode: "incremental" (recommended) or "full" |
| `status` | string | yes | Current status: active, paused, error |
| `conflict_strategy` | string | yes | Conflict resolution strategy: overwrite or skip |
| `sync_deletions` | boolean | yes | Whether to sync deletions from source |
| `last_sync_at` | string(time) | yes | Last successful sync timestamp |
| `last_sync_cursor` | JSON | yes | Cursor or state for incremental sync (connector-specific) |
| `last_sync_result` | JSON | yes | Summary of last sync result |
| `error_message` | string | yes | Error message if status is "error" |
| `sync_log_retention_days` | number | yes | Number of days to keep sync logs (default: 30) |
| `created_at` | string(time) | yes | Creation timestamp |
| `updated_at` | string(time) | yes | Last update timestamp |
| `deleted_at` | DeletedAt | yes | Soft delete timestamp |
| `total_items_synced` | number | yes | Total items synced (not stored in DB, calculated on query) |
| `latest_sync_log` | SyncLog | yes | Latest sync log (not stored in DB, populated on query) |

---
### `deleteDataSource` — `DELETE /datasource/:id`

handler: `DeleteDataSource` · `datasource.go`

**Response**: `204 No Content`
---
### `validateConnection` — `POST /datasource/:id/validate`

handler: `ValidateConnection` · `datasource.go`

**Response**:
```json
{ status: string }
```
---
### `validateCredentials` — `POST /datasource/validate-credentials`

handler: `ValidateCredentials` · `datasource.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `type` | string | yes |  |
| `credentials` | map[string]interface{} | yes |  |

**Response**:
```json
{ status: string }
```
---
### `listResources` — `GET /datasource/:id/resources`

handler: `ListAvailableResources` · `datasource.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `parent_id` | string |  | Parent resource ExternalID; empty lists the top level |

**Response**: `Resource[]`
| field | type | req | notes |
|---|---|---|---|
| `external_id` | string | yes | Unique identifier in the external system |
| `name` | string | yes | Display name |
| `type` | string | yes | Resource type (document, folder, space, page, etc.) |
| `description` | string | yes | Optional description |
| `url` | string | yes | URL to access in external system |
| `modified_at` | string(time) | yes | Last modified time in external system |
| `parent_id` | string |  | For hierarchical resources (parent ID if applicable) |
| `has_children` | boolean |  | Whether this resource has children that can be expanded |
| `metadata` | map[string]interface{} |  | Additional metadata |

---
### `resolveResourceAncestors` — `POST /datasource/:id/resource-ancestors`

handler: `ResolveResourceAncestors` · `datasource.go`

**Body** `resolveAncestorsRequest`:
| field | type | req | notes |
|---|---|---|---|
| `resource_ids` | string[] |  |  |

**Response**:
```json
{ ancestors: ancestors }
```
---
### `triggerSync` — `POST /datasource/:id/sync`

handler: `ManualSync` · `datasource.go`

**Response** `SyncLog`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier |
| `data_source_id` | string | yes | Reference to the data source |
| `tenant_id` | number | yes | Workspace ID |
| `status` | string | yes | Sync status: running, success, partial, failed, canceled |
| `started_at` | string(time) | yes | Sync start time |
| `finished_at` | string(time) | yes | Sync completion time |
| `items_total` | number | yes | Total items fetched from source |
| `items_created` | number | yes | New items created in knowledge base |
| `items_updated` | number | yes | Existing items updated |
| `items_deleted` | number | yes | Items deleted from knowledge base |
| `items_skipped` | number | yes | Items skipped (no changes detected) |
| `items_failed` | number | yes | Items that failed to sync |
| `error_message` | string | yes | Error details if status is "failed" |
| `result` | JSON | yes | Detailed sync result (JSON-encoded) |
| `created_at` | string(time) | yes | Creation timestamp (usually same as StartedAt) |
| `updated_at` | string(time) | yes | Last update timestamp |

---
### `pauseDataSource` — `POST /datasource/:id/pause`

handler: `PauseDataSource` · `datasource.go`

**Response**:
```json
{ status: string }
```
---
### `resumeDataSource` — `POST /datasource/:id/resume`

handler: `ResumeDataSource` · `datasource.go`

**Response**:
```json
{ status: string }
```
---
### `getSyncLogs` — `GET /datasource/:id/logs`

handler: `GetSyncLogs` · `datasource.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `limit` | integer |  | Limit (default: 10) |
| `offset` | integer |  | Offset (default: 0) |

**Response**: `SyncLog[]`
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier |
| `data_source_id` | string | yes | Reference to the data source |
| `tenant_id` | number | yes | Workspace ID |
| `status` | string | yes | Sync status: running, success, partial, failed, canceled |
| `started_at` | string(time) | yes | Sync start time |
| `finished_at` | string(time) | yes | Sync completion time |
| `items_total` | number | yes | Total items fetched from source |
| `items_created` | number | yes | New items created in knowledge base |
| `items_updated` | number | yes | Existing items updated |
| `items_deleted` | number | yes | Items deleted from knowledge base |
| `items_skipped` | number | yes | Items skipped (no changes detected) |
| `items_failed` | number | yes | Items that failed to sync |
| `error_message` | string | yes | Error details if status is "failed" |
| `result` | JSON | yes | Detailed sync result (JSON-encoded) |
| `created_at` | string(time) | yes | Creation timestamp (usually same as StartedAt) |
| `updated_at` | string(time) | yes | Last update timestamp |

---
### `putDataSourceCredentials` — `PUT /datasource/:id/credentials`

handler: `Put` · `web_search_provider_credentials.go`

**Body** `webSearchCredentialsPutRequest`:
| field | type | req | notes |
|---|---|---|---|
| `api_key` | string |  |  |

**Response**:
```json
{ success: true, data: CredentialsResponse }
```
`data` is `CredentialsResponse`:
| field | type | req | notes |
|---|---|---|---|
| `fields` | map[string]CredentialFieldMetadata | yes |  |

---
### `deleteDataSourceCredentials` — `DELETE /datasource/:id/credentials/:field`

_Frontend always sends the literal field name `credentials` — i.e. `DELETE /api/v1/datasource/:id/credentials/credentials`._

handler: `DeleteField` · `web_search_provider_credentials.go`

**Response**: `204 No Content`
---

## Referenced types

#### `SyncLog`
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier |
| `data_source_id` | string | yes | Reference to the data source |
| `tenant_id` | number | yes | Workspace ID |
| `status` | string | yes | Sync status: running, success, partial, failed, canceled |
| `started_at` | string(time) | yes | Sync start time |
| `finished_at` | string(time) | yes | Sync completion time |
| `items_total` | number | yes | Total items fetched from source |
| `items_created` | number | yes | New items created in knowledge base |
| `items_updated` | number | yes | Existing items updated |
| `items_deleted` | number | yes | Items deleted from knowledge base |
| `items_skipped` | number | yes | Items skipped (no changes detected) |
| `items_failed` | number | yes | Items that failed to sync |
| `error_message` | string | yes | Error details if status is "failed" |
| `result` | JSON | yes | Detailed sync result (JSON-encoded) |
| `created_at` | string(time) | yes | Creation timestamp (usually same as StartedAt) |
| `updated_at` | string(time) | yes | Last update timestamp |
