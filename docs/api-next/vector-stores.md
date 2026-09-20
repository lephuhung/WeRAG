# Vector Stores API

Frontend module: `frontend-next/lib/api/vector-stores.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `listVectorStoreTypes` — `GET /vector-stores/types`

handler: `ListStoreTypes` · `vectorstore.go`

**Response**:
```json
{ success: true, data: GetVectorStoreTypes }
```
---
### `listVectorStores` — `GET /vector-stores`

handler: `ListStores` · `vectorstore.go`

**Response**:
```json
{ success: true, data: allStores }
```
---
### `createVectorStore` — `POST /vector-stores`

handler: `CreateStore` · `vectorstore.go`

**Body** `CreateStoreRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `engine_type` | RetrieverEngineType | yes |  |
| `connection_config` | ConnectionConfig | yes |  |
| `index_config` | IndexConfig |  |  |

**Response**:
```json
{ success: true, data: NewVectorStoreResponse }
```
---
### `updateVectorStore` — `PUT /vector-stores/:id`

handler: `UpdateStore` · `vectorstore.go`

**Body** `UpdateStoreRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |

**Response**:
```json
{ success: true, data: nil }
```
---
### `deleteVectorStore` — `DELETE /vector-stores/:id`

handler: `DeleteStore` · `vectorstore.go`

**Response**:
```json
{ success: true }
```
---
### `testVectorStoreRaw` — `POST /vector-stores/test`

handler: `TestStoreRaw` · `vectorstore.go`

**Body** `TestStoreRequest`:
| field | type | req | notes |
|---|---|---|---|
| `engine_type` | RetrieverEngineType | yes |  |
| `connection_config` | ConnectionConfig | yes |  |

**Response**:
```json
{ success: true, version: string }
```
---
### `testVectorStoreById` — `POST /vector-stores/:id/test`

handler: `TestStoreByID` · `vectorstore.go`

**Response**:
```json
{ success: true, version: string }
```
---

## Referenced types

#### `ConnectionConfig`
| field | type | req | notes |
|---|---|---|---|
| `addr` | string |  | Common |
| `username` | string |  |  |
| `password` | string |  |  |
| `api_key` | string |  |  |
| `insecure_skip_verify` | boolean |  | InsecureSkipVerify disables TLS certificate verification when talking to the backing store over HTTPS. Defaults to false (secure). Set to true ONLY for self-signed development clus |
| `host` | string |  | Qdrant |
| `port` | number |  |  |
| `use_tls` | boolean |  |  |
| `grpc_address` | string |  | Weaviate |
| `scheme` | string |  |  |
| `database` | string |  | Database name used by engines that support database-level namespaces (currently Milvus, Tencent VectorDB, and Doris). |
| `use_default_connection` | boolean |  | Postgres |
| `http_port` | number |  | Doris: HTTP port for Stream Load API (FE default 8030). Addr is reused for the MySQL protocol "host:9030"; HTTPPort + the host of Addr together form the FE HTTP endpoint used by St |
| `version` | string |  | Version is the detected server version (e.g., "7.10.1", "16.2", "1.12.6"). Auto-populated by TestConnection on successful connectivity check. |

#### `IndexConfig`
| field | type | req | notes |
|---|---|---|---|
| `index_name` | string |  | --- Existing fields --- |
| `number_of_shards` | number |  |  |
| `number_of_replicas` | number |  |  |
| `collection_prefix` | string |  |  |
| `collection_name` | string |  |  |
| `shard_number` | number |  | --- Scalability fields --- |
| `replication_factor` | number |  |  |
| `shards_num` | number |  |  |
| `replica_number` | number |  |  |
| `desired_shard_count` | number |  |  |
| `buckets_num` | number |  |  |
| `replication_num` | number |  |  |
| `hnsw_m` | number |  | --- OpenSearch k-NN HNSW fields --- All omitempty so other engines' serialized IndexConfig is unchanged. Zero / empty values fall back to the driver defaults in buildInternalCfg. |
| `hnsw_ef_construction` | number |  |  |
| `hnsw_ef_search` | number |  |  |
| `knn_engine` | string |  |  |
