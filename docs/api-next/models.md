# Models API

Frontend module: `frontend-next/lib/api/models.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `createModel` — `POST /models`

handler: `CreateModel` · `model.go`

**Body** `CreateModelRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `display_name` | string |  |  |
| `type` | ModelType | yes |  |
| `source` | ModelSource | yes |  |
| `description` | string |  |  |
| `parameters` | ModelParameters | yes |  |

**Response**:
```json
{ success: true, data: NewModelResponse }
```
---
### `listModels` — `GET /models`

handler: `ListModels` · `model.go`

**Response**:
```json
{ success: true, data: NewModelResponses }
```
---
### `getModel` — `GET /models/:id`

handler: `GetModel` · `model.go`

**Response**:
```json
{ success: true, data: NewModelResponse }
```
---
### `updateModel` — `PUT /models/:id`

handler: `UpdateModel` · `model.go`

**Body** `UpdateModelRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string |  |  |
| `display_name` | string |  |  |
| `description` | string |  |  |
| `parameters` | ModelParameters |  |  |
| `source` | ModelSource |  |  |
| `type` | ModelType |  |  |

**Response**:
```json
{ success: true, data: NewModelResponse }
```
---
### `deleteModel` — `DELETE /models/:id`

handler: `DeleteModel` · `model.go`

**Response**:
```json
{ success: true, message: string }
```
---
### `debugModel` — `POST /models/:id/debug`

handler: `DebugModel` · `model.go→writeModelDebugResult`

**Response**:
```json
{ success: true, data: data }
```
---
### `putModelCredentials` — `PUT /models/:id/credentials`

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
### `deleteModelCredentialField` — `DELETE /models/:id/credentials/:field`

handler: `DeleteField` · `web_search_provider_credentials.go`

**Response**: `204 No Content`
---
### `saveWeKnoraCloudCredentials` — `POST /weknoracloud/credentials`

handler: `SaveCredentials` · `weknoracloud.go`

**Body** `weKnoraCloudCredentialsRequest`:
| field | type | req | notes |
|---|---|---|---|
| `app_id` | string | yes |  |
| `app_secret` | string | yes |  |

**Response**:
```json
{ success: true, message: string }
```
---

## Referenced types

#### `ModelParameters`
| field | type | req | notes |
|---|---|---|---|
| `base_url` | string | yes |  |
| `api_key` | string | yes |  |
| `interface_type` | string | yes |  |
| `embedding_parameters` | EmbeddingParameters | yes |  |
| `parameter_size` | string | yes |  |
| `provider` | string | yes |  |
| `extra_config` | map[string]string | yes |  |
| `custom_headers` | map[string]string |  | CustomHeaders adds extra HTTP headers when calling the remote model API, similar to the Python OpenAI SDK extra_headers param (e.g. corporate gateway auth, trace IDs, routing tags). Reserved headers (Authorization, api-key, Content-Type, Accept, ...) are ignored at runtime. |
| `supports_vision` | boolean | yes |  |
| `context_window` | number |  | ContextWindow is the model's total context window in tokens and MaxOutputTokens the most it emits in one response. Both are provider facts the agent cannot discover but has to act  |
| `max_output_tokens` | number |  |  |
| `max_concurrency` | number |  | MaxConcurrency caps concurrent in-flight BACKGROUND (ingestion / enrichment) calls to THIS specific model, keyed by model ID and shared across all replicas. 0 (the default) means " |
| `app_id` | string |  | WeKnoraCloud vendor-specific credentials |
| `app_secret` | string |  |  |
