# Web Search Providers API

Frontend module: `frontend-next/lib/api/web-search.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `createWebSearchProvider` — `POST /web-search-providers`

handler: `CreateProvider` · `web_search_provider.go`

**Body** `CreateProviderRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `provider` | WebSearchProviderType | yes |  |
| `description` | string |  |  |
| `parameters` | WebSearchProviderParameters |  |  |
| `is_default` | boolean |  |  |

**Response**:
```json
{ success: true, data: NewWebSearchProviderResponse }
```
---
### `listWebSearchProviders` — `GET /web-search-providers`

handler: `ListProviders` · `web_search_provider.go`

**Response**:
```json
{ success: true, data: NewWebSearchProviderResponses }
```
---
### `getWebSearchProvider` — `GET /web-search-providers/:id`

handler: `GetProvider` · `web_search_provider.go`

**Response** `WebSearchProviderEntity`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier (UUID, auto-generated) |
| `tenant_id` | number | yes | Workspace ID for scoping |
| `name` | string | yes | User-friendly name, e.g., "Production Bing Search" |
| `provider` | WebSearchProviderType | yes | Provider type: bing, google, duckduckgo, tavily |
| `description` | string | yes | Description |
| `parameters` | WebSearchProviderParameters | yes | Provider-specific parameters (API key, engine ID, etc.) stored as encrypted JSON |
| `is_default` | boolean | yes | Whether this is the default provider for the workspace |
| `created_at` | string(time) | yes | Timestamps |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |

---
### `updateWebSearchProvider` — `PUT /web-search-providers/:id`

handler: `UpdateProvider` · `web_search_provider.go`

**Body** `UpdateProviderRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string |  |  |
| `description` | string |  |  |
| `parameters` | WebSearchProviderParameters |  |  |
| `is_default` | boolean |  |  |

**Response** `WebSearchProviderEntity`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier (UUID, auto-generated) |
| `tenant_id` | number | yes | Workspace ID for scoping |
| `name` | string | yes | User-friendly name, e.g., "Production Bing Search" |
| `provider` | WebSearchProviderType | yes | Provider type: bing, google, duckduckgo, tavily |
| `description` | string | yes | Description |
| `parameters` | WebSearchProviderParameters | yes | Provider-specific parameters (API key, engine ID, etc.) stored as encrypted JSON |
| `is_default` | boolean | yes | Whether this is the default provider for the workspace |
| `created_at` | string(time) | yes | Timestamps |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |

---
### `deleteWebSearchProvider` — `DELETE /web-search-providers/:id`

handler: `DeleteProvider` · `web_search_provider.go`

**Response**:
```json
{ success: true }
```
---
### `listWebSearchProviderTypes` — `GET /web-search-providers/types`

handler: `ListProviderTypes` · `web_search_provider.go`

**Response**:
```json
{ success: true, data: GetWebSearchProviderTypes }
```
---
### `putWebSearchProviderCredentials` — `PUT /web-search-providers/:id/credentials`

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
### `deleteWebSearchProviderCredentialField` — `DELETE /web-search-providers/:id/credentials/:field`

handler: `DeleteField` · `web_search_provider_credentials.go`

**Response**: `204 No Content`
---
### `testWebSearchProvider` — `POST /web-search-providers/:id/test`

handler: `TestProviderByID` · `web_search_provider.go`

**Response**:
```json
{ success: true }
```
---
### `testWebSearchProvider` — `POST /web-search-providers/test`

handler: `TestProviderRaw` · `web_search_provider.go`

**Body** `TestProviderRequest`:
| field | type | req | notes |
|---|---|---|---|
| `provider` | string | yes |  |
| `parameters` | WebSearchProviderParameters |  |  |

**Response**:
```json
{ success: true }
```
---
### `getWebSearchProviders` — `GET /web-search/providers`

handler: `GetProviders` · `web_search.go`

**Response**:
```json
{ success: true, data: GetWebSearchProviderTypes }
```
---
### `getTenantWebSearchConfig` — `GET /tenants/kv/web-search-config`

_dispatched via `/tenants/kv/:key` (key `web-search-config`)_

handler: `GetTenantWebSearchConfig` · `tenant.go`

**Response**:
```json
{ success: true, data: WebSearchConfigForResponse }
```
---
### `updateTenantWebSearchConfig` — `PUT /tenants/kv/:key`

_dispatched via `/tenants/kv/:key` (key `web-search-config`)_

handler: `updateTenantWebSearchConfigInternal` · `tenant.go`

**Body** `WebSearchConfig`:
| field | type | req | notes |
|---|---|---|---|
| `provider` | string |  | Deprecated: Use WebSearchProviderEntity.Parameters.APIKey instead. |
| `api_key` | string |  | Deprecated: Use WebSearchProviderEntity.Parameters.APIKey instead. |
| `max_results` | number |  |  |
| `include_date` | boolean |  |  |
| `compression_method` | string |  |  |
| `blacklist` | string[] |  |  |
| `embedding_model_id` | string |  | RAG compression configuration |
| `embedding_dimension` | number |  |  |
| `rerank_model_id` | string |  |  |
| `document_fragments` | number |  |  |
| `proxy_url` | string |  |  |

**Response**:
```json
{ success: true, data: WebSearchConfigForResponse, message: string }
```
---

## Referenced types

#### `WebSearchProviderParameters`
| field | type | req | notes |
|---|---|---|---|
| `api_key` | string |  | API key for the search provider (encrypted in DB) |
| `engine_id` | string |  | Google Custom Search Engine ID (only for Google provider) |
| `base_url` | string |  | Base URL for self-hosted search engines (e.g. SearXNG instance URL). Validated with utils.ValidateURLForSSRF; private hosts must be added to SSRF_WHITELIST. |
| `proxy_url` | string |  | Optional HTTP/HTTPS proxy URL for outbound search requests (e.g. http://host:port); validated with utils.ValidateURLForSSRF. Does not replace the search API endpoint; only tunnels  |
| `extra_config` | map[string]string |  | Provider-specific extra configuration for future extensibility |
