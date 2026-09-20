# MCP Services API

Frontend module: `frontend-next/lib/api/mcp.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `listMCPServices` — `GET /mcp-services`

handler: `ListMCPServices` · `mcp_service.go`

**Response**:
```json
{ success: true, data: h.mcpServiceResponses(ctx }
```
---
### `getMCPService` — `GET /mcp-services/:id`

handler: `GetMCPService` · `mcp_service.go`

**Response**:
```json
{ success: true, data: h.mcpServiceResponses(ctx }
```
---
### `createMCPService` — `POST /mcp-services`

handler: `CreateMCPService` · `mcp_service.go`

**Body** `MCPService`:
| field | type | req | notes |
|---|---|---|---|
| `usage_instructions` | string |  | UsageInstructions is maintained locally and is not overwritten by directory refresh. |
| `id` | string |  |  |
| `tenant_id` | number |  |  |
| `name` | string |  |  |
| `description` | string |  |  |
| `enabled` | boolean |  |  |
| `transport_type` | MCPTransportType |  |  |
| `url` | string |  |  |
| `headers` | MCPHeaders |  |  |
| `auth_config` | MCPAuthConfig |  |  |
| `advanced_config` | MCPAdvancedConfig |  |  |
| `stdio_config` | MCPStdioConfig |  |  |
| `env_vars` | MCPEnvVars |  |  |
| `is_builtin` | boolean |  |  |
| `created_at` | string(time) |  |  |
| `updated_at` | string(time) |  |  |
| `deleted_at` | DeletedAt |  |  |

**Response**:
```json
{ success: true, data: NewMCPServiceResponse }
```
---
### `updateMCPService` — `PUT /mcp-services/:id`

handler: `UpdateMCPService` · `mcp_service.go`

**Body**: `map[string]interface`

**Response**:
```json
{ success: true, data: h.mcpServiceResponses(ctx }
```
---
### `deleteMCPService` — `DELETE /mcp-services/:id`

handler: `DeleteMCPService` · `mcp_service.go`

**Response**:
```json
{ success: true, message: string }
```
---
### `testMCPService` — `POST /mcp-services/:id/test`

handler: `TestMCPService` · `mcp_service.go`

**Response**:
```json
{ success: true, data: types.MCPTestResult }
```
`data` is `MCPTestResult`:
| field | type | req | notes |
|---|---|---|---|
| `success` | boolean | yes |  |
| `message` | string |  |  |
| `description` | string |  |  |
| `oauth_required` | boolean |  | OAuthRequired is true when the connection failed because the server requires OAuth authorization (RFC 9728), even though the service was not configured for OAuth. The UI uses it to |
| `tools` | *MCPTool[] |  |  |
| `resources` | *MCPResource[] |  |  |

---
### `getMCPServiceTools` — `GET /mcp-services/:id/tools`

handler: `GetMCPServiceTools` · `mcp_service.go`

**Response**:
```json
{ success: true, data: tools }
```
---
### `getMCPServiceResources` — `GET /mcp-services/:id/resources`

handler: `GetMCPServiceResources` · `mcp_service.go`

**Response**:
```json
{ success: true, data: resources }
```
---
### `getMCPToolApprovals` — `GET /mcp-services/:id/tool-approvals`

handler: `ListMCPToolApprovals` · `mcp_service.go`

**Response**:
```json
{ success: true, data: rows }
```
---
### `setMCPToolApproval` — `PUT /mcp-services/:id/tool-approvals/:tool_name`

handler: `SetMCPToolApproval` · `mcp_service.go`

**Body** `setMCPToolApprovalBody`:
| field | type | req | notes |
|---|---|---|---|
| `require_approval` | boolean |  |  |
| `enabled` | boolean |  |  |

**Response**:
```json
{ success: true }
```
---
### `setMCPToolEnabled` — `PUT /mcp-services/:id/tool-approvals/:tool_name`

handler: `SetMCPToolApproval` · `mcp_service.go`

**Body** `setMCPToolApprovalBody`:
| field | type | req | notes |
|---|---|---|---|
| `require_approval` | boolean |  |  |
| `enabled` | boolean |  |  |

**Response**:
```json
{ success: true }
```
---
### `putMCPCredentials` — `PUT /mcp-services/:id/credentials`

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
### `deleteMCPCredentialField` — `DELETE /mcp-services/:id/credentials/:field`

handler: `DeleteField` · `web_search_provider_credentials.go`

**Response**: `204 No Content`
---
### `getMCPOAuthAuthorizeURL` — `POST /mcp-services/:id/oauth/authorize-url`

handler: `AuthorizeURL` · `mcp_oauth.go`

**Body** `mcpOAuthAuthorizeRequest`:
| field | type | req | notes |
|---|---|---|---|
| `redirect_uri` | string |  | RedirectURI is the absolute backend callback URL registered with the authorization server (e.g. https://host/api/v1/mcp-services/oauth/callback). |
| `frontend_redirect` | string |  | FrontendRedirect is where the callback bounces the browser when done (e.g. the MCP settings page). Optional; defaults to "/". |

**Response**:
```json
{ success: true, data: {authorization_url, authorization_attempt} }
```
---
### `getMCPOAuthStatus` — `GET /mcp-services/:id/oauth/status`

handler: `Status` · `weknoracloud.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `authorization_attempt` | string |  | ID of this authorization attempt; when given, historical tokens are not accepted |

**Response** `WeKnoraCloudStatusResult`:
| field | type | req | notes |
|---|---|---|---|
| `has_models` | boolean | yes |  |
| `needs_reinit` | boolean | yes |  |
| `reason` | string |  |  |

---
### `getMCPOAuthAuthorizationStatus` — `GET /mcp-services/:id/oauth/status`

handler: `Status` · `weknoracloud.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `authorization_attempt` | string |  | ID of this authorization attempt; when given, historical tokens are not accepted |

**Response** `WeKnoraCloudStatusResult`:
| field | type | req | notes |
|---|---|---|---|
| `has_models` | boolean | yes |  |
| `needs_reinit` | boolean | yes |  |
| `reason` | string |  |  |

---
### `revokeMCPOAuthToken` — `DELETE /mcp-services/:id/oauth/token`

handler: `Revoke` · `mcp_oauth.go`

**Response**: `204 No Content`
---
### `resolveToolApproval` — `POST /agent/tool-approvals/:pending_id`

handler: `ResolveToolApproval` · `mcp_service.go`

**Body** `resolveToolApprovalBody`:
| field | type | req | notes |
|---|---|---|---|
| `decision` | string | yes |  |
| `modified_args` | any |  |  |
| `reason` | string |  |  |

**Response**:
```json
{ success: true }
```
---
### `resolveMCPOAuth` — `POST /agent/mcp-oauth-resolutions/:pending_id`

handler: `ResolveMCPOAuth` · `mcp_oauth.go`

**Body** `resolveMCPOAuthBody`:
| field | type | req | notes |
|---|---|---|---|
| `service_id` | string | yes | ServiceID is the MCP service the pending prompt belongs to; used to verify the user actually holds a token before resuming the agent. |
| `decision` | string |  | Decision is "authorize" (default) or "cancel" when the user skips OAuth. |

**Response**:
```json
{ success: true }
```
---
### `cancelMCPOAuth` — `POST /agent/mcp-oauth-resolutions/:pending_id/cancel`

handler: `CancelMCPOAuth` · `mcp_oauth.go`

**Response**:
```json
{ success: true }
```
---
### `getMCPMetadata` — `GET /mcp-services/:id/metadata`

handler: `GetMCPMetadata` · `mcp_metadata.go→mcpMetadata`

**Response**:
```json
{ success: true, data: *types.MCPMetadata }
```
`data` is `MCPMetadata`:
| field | type | req | notes |
|---|---|---|---|
| `service_id` | string | yes |  |
| `tools` | *MCPTool[] | yes |  |
| `instructions` | string | yes |  |
| `server_name` | string | yes |  |
| `server_version` | string | yes |  |
| `server_description` | string | yes |  |
| `synced_at` | string(time) | yes |  |
| `stale` | boolean | yes |  |

---
### `refreshMCPMetadata` — `POST /mcp-services/:id/metadata/refresh`

handler: `RefreshMCPMetadata` · `mcp_metadata.go→mcpMetadata`

**Response**:
```json
{ success: true, data: *types.MCPMetadata }
```
`data` is `MCPMetadata`:
| field | type | req | notes |
|---|---|---|---|
| `service_id` | string | yes |  |
| `tools` | *MCPTool[] | yes |  |
| `instructions` | string | yes |  |
| `server_name` | string | yes |  |
| `server_version` | string | yes |  |
| `server_description` | string | yes |  |
| `synced_at` | string(time) | yes |  |
| `stale` | boolean | yes |  |

---
### `generateMCPUsageInstructions` — `POST /mcp-services/:id/usage-instructions/generate`

handler: `GenerateMCPUsageInstructions` · `mcp_usage_instructions.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `language` | string |  |  |

**Response**:
```json
{ success: true, data: {usage_instructions} }
```
---
### `listMcpEndpoints` — `GET /mcp-endpoints`

handler: `ListMCPEndpoints` · `mcp_endpoint.go`

**Response**:
```json
{ success: true, data: out }
```
---
### `getMcpEndpointToolCatalog` — `GET /mcp-endpoints/tools`

handler: `ListToolCatalog` · `mcp_endpoint.go`

**Response**:
```json
{ success: true, data: {groups, tools, default_tools} }
```
---
### `createMcpEndpoint` — `POST /mcp-endpoints`

handler: `CreateMCPEndpoint` · `mcp_endpoint.go`

**Body** `mcpEndpointRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string |  |  |
| `description` | string |  |  |
| `enabled` | boolean |  |  |
| `knowledge_base_ids` | string[] |  |  |
| `tools` | string[] |  |  |
| `default_agent_id` | string |  |  |
| `rate_limit_per_minute` | number |  |  |

**Response**:
```json
{ success: true, data: mcpEndpointResponse(ep }
```
---
### `updateMcpEndpoint` — `PUT /mcp-endpoints/:endpoint_id`

handler: `UpdateMCPEndpoint` · `mcp_endpoint.go`

**Body** `mcpEndpointRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string |  |  |
| `description` | string |  |  |
| `enabled` | boolean |  |  |
| `knowledge_base_ids` | string[] |  |  |
| `tools` | string[] |  |  |
| `default_agent_id` | string |  |  |
| `rate_limit_per_minute` | number |  |  |

**Response**:
```json
{ success: true, data: mcpEndpointResponse(ep }
```
---
### `deleteMcpEndpoint` — `DELETE /mcp-endpoints/:endpoint_id`

handler: `DeleteMCPEndpoint` · `mcp_endpoint.go`

**Response**:
```json
{ success: true }
```
---
### `rotateMcpEndpointToken` — `POST /mcp-endpoints/:endpoint_id/rotate-token`

handler: `RotateMCPEndpointToken` · `mcp_endpoint.go`

**Response**:
```json
{ success: true, data: mcpEndpointResponse(ep }
```
---

## Referenced types

#### `MCPAdvancedConfig`
| field | type | req | notes |
|---|---|---|---|
| `timeout` | number | yes |  |
| `retry_count` | number | yes |  |
| `retry_delay` | number | yes |  |

#### `MCPAuthConfig`
| field | type | req | notes |
|---|---|---|---|
| `auth_type` | MCPAuthType |  | AuthType selects the authentication strategy. Empty ("") is treated as none for backward compatibility with rows that pre-date this field. |
| `api_key` | string |  |  |
| `api_key_header` | string |  | APIKeyHeader is the header name that carries APIKey when AuthType is api_key. Empty defaults to "X-API-Key". This is non-secret structural config (the secret is APIKey), so it is n |
| `token` | string |  |  |
| `custom_headers` | map[string]string |  |  |
| `scopes` | string[] |  | Scopes are the OAuth scopes requested during authorization. Optional. |
| `auth_server_metadata_url` | string |  | AuthServerMetadataURL optionally pins the OAuth authorization server metadata URL. When empty, the server is discovered automatically from the MCP URL (RFC 9728 / RFC 8414). |

#### `MCPStdioConfig`
| field | type | req | notes |
|---|---|---|---|
| `command` | string | yes |  |
| `args` | string[] | yes |  |
