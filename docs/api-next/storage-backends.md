# Storage Backends API

Frontend module: `frontend-next/lib/api/storage-backends.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `listStorageBackends` — `GET /storage-backends`

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
### `listStorageBackendTypes` — `GET /storage-backends/types`

handler: `Types` · `storagebackend.go`

**Response**:
```json
{ success: true, data: storageallowlist.AllowedList() }
```
---
### `createStorageBackend` — `POST /storage-backends`

handler: `Create` · `storagebackend.go`

**Body** `storageBackendRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `provider` | string | yes |  |
| `config` | StorageBackendConfig |  |  |
| `status` | string |  |  |

**Response**:
```json
{ success: true, data: NewStorageBackendResponse }
```
---
### `updateStorageBackend` — `PUT /storage-backends/:id`

handler: `Update` · `storagebackend.go`

**Body** `storageBackendRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `provider` | string | yes |  |
| `config` | StorageBackendConfig |  |  |
| `status` | string |  |  |

**Response**:
```json
{ success: true, data: NewStorageBackendResponse }
```
---
### `deleteStorageBackend` — `DELETE /storage-backends/:id`

handler: `Delete` · `storagebackend.go`

**Response**:
```json
{ success: true }
```
---
### `setDefaultStorageBackend` — `PUT /storage-backends/:id/default`

handler: `SetDefault` · `storagebackend.go`

**Response**:
```json
{ success: true }
```
---
### `testStorageBackend` — `POST /storage-backends/test`

handler: `TestRaw` · `storagebackend.go`

**Body** `storageBackendRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `provider` | string | yes |  |
| `config` | StorageBackendConfig |  |  |
| `status` | string |  |  |

**Response**:
```json
{ success: true }
```
---
### `testStorageBackendByID` — `POST /storage-backends/:id/test`

handler: `TestByID` · `storagebackend.go`

**Response**:
```json
{ success: true }
```
---

## Referenced types

#### `StorageBackendConfig`
| field | type | req | notes |
|---|---|---|---|
| `mode` | string |  |  |
| `endpoint` | string |  |  |
| `region` | string |  |  |
| `access_key_id` | string |  |  |
| `secret_access_key` | string |  |  |
| `bucket_name` | string |  |  |
| `path_prefix` | string |  |  |
| `app_id` | string |  |  |
| `use_ssl` | boolean |  |  |
| `force_path_style` | boolean |  |  |
| `use_temp_bucket` | boolean |  |  |
| `temp_bucket_name` | string |  |  |
| `temp_region` | string |  |  |
