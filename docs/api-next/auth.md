# Authentication API

Frontend module: `frontend-next/lib/api/auth.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `login` — `POST /auth/login`

handler: `Login` · `auth.go`

**Body** `LoginRequest`:
| field | type | req | notes |
|---|---|---|---|
| `email` | string | yes |  |
| `password` | string | yes |  |

**Response** `LoginResponse`:
| field | type | req | notes |
|---|---|---|---|
| `success` | boolean | yes |  |
| `message` | string |  |  |
| `user` | User |  |  |
| `active_tenant` | Tenant |  | ActiveTenant is the workspace whose ID is encoded in the issued JWT; future requests are scoped to it until the client calls /auth/switch-tenant. Defaults to the user's home worksp |
| `memberships` | Membership[] | yes | Memberships lists every workspace the user can authenticate into, along with their role in each. Always populated (length 1 for users who only belong to their home workspace) so fr |
| `token` | string |  |  |
| `refresh_token` | string |  |  |

---
### `getOIDCAuthorizationURL` — `GET /auth/oidc/url`

handler: `GetOIDCAuthorizationURL` · `auth.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `redirect_uri` | string | yes | OIDC callback URL |

**Response** `OIDCAuthURLResponse`:
| field | type | req | notes |
|---|---|---|---|
| `success` | boolean | yes |  |
| `provider_display_name` | string |  |  |
| `authorization_url` | string |  |  |
| `state` | string |  |  |

---
### `getOIDCConfig` — `GET /auth/oidc/config`

handler: `GetOIDCConfig` · `auth.go`

**Response** `OIDCConfigResponse`:
| field | type | req | notes |
|---|---|---|---|
| `success` | boolean | yes |  |
| `enabled` | boolean | yes |  |
| `provider_display_name` | string |  |  |

---
### `getAuthConfig` — `GET /auth/config`

handler: `GetAuthConfig` · `auth.go`

**Response**:
```json
{ success: true, registration_mode: mode, complex_password_enabled: ResolveComplexPasswordEnabled }
```
---
### `register` — `POST /auth/register`

handler: `Register` · `auth.go`

**Body** `RegisterRequest`:
| field | type | req | notes |
|---|---|---|---|
| `username` | string | yes |  |
| `email` | string | yes |  |
| `password` | string | yes |  |

**Response** `RegisterResponse`:
| field | type | req | notes |
|---|---|---|---|
| `success` | boolean | yes |  |
| `message` | string |  |  |
| `user` | User |  |  |
| `tenant` | Tenant |  |  |

---
### `autoSetup` — `POST /auth/auto-setup`

handler: `AutoSetup` · `auth.go`

**Response** `LoginResponse`:
| field | type | req | notes |
|---|---|---|---|
| `success` | boolean | yes |  |
| `message` | string |  |  |
| `user` | User |  |  |
| `active_tenant` | Tenant |  | ActiveTenant is the workspace whose ID is encoded in the issued JWT; future requests are scoped to it until the client calls /auth/switch-tenant. Defaults to the user's home worksp |
| `memberships` | Membership[] | yes | Memberships lists every workspace the user can authenticate into, along with their role in each. Always populated (length 1 for users who only belong to their home workspace) so fr |
| `token` | string |  |  |
| `refresh_token` | string |  |  |

---
### `getCurrentUser` — `GET /auth/me`

handler: `GetCurrentUser` · `auth.go`

**Response**:
```json
{ success: true, data: {user, preference_defaults, tenant, memberships, tenant_required, capabilities} }
```
---
### `updateMyPreferences` — `PUT /auth/me/preferences`

handler: `UpdateMyPreferences` · `auth.go`

**Body** `updateMyPreferencesRequest`:
| field | type | req | notes |
|---|---|---|---|
| `browser_search_instructions` | string |  |  |
| `last_active_tenant_id` | number |  | LastActiveTenantID lets clients persist "after a fresh login, drop me back into this workspace" across devices. The SPA sends this after every tenant switch; POST /auth/switch-tena |
| `language` | string |  | Language stores the user's UI locale choice so it follows the account across devices. Empty string clears it back to the browser/default locale. |

**Response**:
```json
{ success: true, data: types.UserPreferences }
```
`data` is `UserPreferences`:
| field | type | req | notes |
|---|---|---|---|
| `browser_search_instructions` | string |  | BrowserSearchInstructions customizes browser search for this user. Nil/empty uses the platform default. |
| `last_active_tenant_id` | number |  | LastActiveTenantID remembers the last workspace the user actively switched into, so a fresh login (new device, cleared browser, new refresh token) lands them back in that workspace |
| `oidc_only_login` | boolean |  | OidcOnlyLogin is set server-side when an account is auto-provisioned via OIDC with a random password the user never received. The profile UI hides self-service password rotation un |
| `language` | string |  | Language is the UI locale this user last picked (e.g. "en", "vi"). Persisted so the choice follows the account across browsers and devices instead of living in one browser's localS |

---
### `refreshToken` — `POST /auth/refresh`

handler: `RefreshToken` · `auth.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `refreshToken` | string | yes |  |

**Response**:
```json
{ success: true, message: string, access_token: accessToken, refresh_token: newRefreshToken }
```
---
### `logout` — `POST /auth/logout`

handler: `Logout` · `auth.go`

**Response**:
```json
{ success: true, message: string }
```
---
### `changePassword` — `POST /auth/change-password`

handler: `ChangePassword` · `auth.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `old_password` | string | yes |  |
| `new_password` | string | yes |  |

**Response**:
```json
{ success: true, message: string }
```
---
### `validateToken` — `GET /auth/validate`

handler: `ValidateToken` · `auth.go`

**Response**:
```json
{ success: true, message: string, user: user.ToUserInfo() }
```
---
### `getInvitationByToken` — `POST /auth/invitations/lookup`

handler: `LookupInvitationByToken` · `auth_register_by_invite.go`

**Body** `invitationLookupRequest`:
| field | type | req | notes |
|---|---|---|---|
| `token` | string | yes |  |

**Response** `invitationLookupResponse`:
| field | type | req | notes |
|---|---|---|---|
| `tenant_id` | number | yes |  |
| `tenant_name` | string |  |  |
| `org_id` | number |  |  |
| `org_name` | string |  |  |
| `role` | TenantRole | yes |  |
| `expires_at` | string | yes |  |

---
### `registerByInvite` — `POST /auth/register-by-invite`

handler: `RegisterByInvite` · `auth_register_by_invite.go`

**Body** `registerByInviteRequest`:
| field | type | req | notes |
|---|---|---|---|
| `token` | string | yes |  |
| `email` | string | yes |  |
| `username` | string | yes |  |
| `password` | string | yes |  |

**Response** `LoginResponse`:
| field | type | req | notes |
|---|---|---|---|
| `success` | boolean | yes |  |
| `message` | string |  |  |
| `user` | User |  |  |
| `active_tenant` | Tenant |  | ActiveTenant is the workspace whose ID is encoded in the issued JWT; future requests are scoped to it until the client calls /auth/switch-tenant. Defaults to the user's home worksp |
| `memberships` | Membership[] | yes | Memberships lists every workspace the user can authenticate into, along with their role in each. Always populated (length 1 for users who only belong to their home workspace) so fr |
| `token` | string |  |  |
| `refresh_token` | string |  |  |

---

## Referenced types

#### `Membership`
| field | type | req | notes |
|---|---|---|---|
| `tenant_id` | number | yes |  |
| `tenant_name` | string | yes |  |
| `role` | TenantRole | yes |  |

#### `Tenant`
| field | type | req | notes |
|---|---|---|---|
| `id` | number | yes | ID |
| `name` | string | yes | Name |
| `description` | string | yes | Description |
| `status` | string | yes | Status |
| `retriever_engines` | RetrieverEngines | yes | Retriever engines |
| `business` | string | yes | Business |
| `storage_quota` | number | yes | Storage quota (Bytes), default is 10GB, including vector, original file, text, index, etc. |
| `storage_used` | number | yes | Storage used (Bytes) |
| `context_config` | ContextConfig | yes | Global Context configuration for this workspace (default for all sessions) |
| `web_search_config` | WebSearchConfig | yes | Global WebSearch configuration for this workspace |
| `parser_engine_config` | ParserEngineConfig | yes | Parser engine config overrides (MinerU endpoint, API key, etc.). Used when parsing documents; overrides env. |
| `credentials` | CredentialsConfig | yes | Credentials config: third-party provider credentials (e.g. WeKnoraCloud AppID/AppSecret) |
| `storage_engine_config` | StorageEngineConfig | yes | Storage engine config: parameters for Local, MinIO, COS. Used for document/file storage and docreader. |
| `default_storage_backend_id` | string |  | DefaultStorageBackendID is the workspace default concrete storage instance. |
| `chat_history_config` | ChatHistoryConfig | yes | Chat history config: knowledge base configuration for indexing and searching chat messages via vector search |
| `retrieval_config` | RetrievalConfig | yes | Retrieval config: global search/retrieval parameters shared by knowledge search and message search |
| `memory_config` | MemoryConfig | yes | Memory config: workspace switch for cross-session long-term memory |
| `created_at` | string(time) | yes | Creation time |
| `updated_at` | string(time) | yes | Last updated time |
| `deleted_at` | DeletedAt | yes | Deletion time |

#### `User`
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier of the user |
| `username` | string | yes | Username of the user |
| `email` | string | yes | Email address of the user |
| `avatar` | string | yes | Avatar URL of the user |
| `tenant_id` | number | yes | Workspace ID that the user belongs to |
| `is_active` | boolean | yes | Whether the user is active |
| `can_access_all_tenants` | boolean | yes | Whether the user can access all workspaces (cross-workspace access) |
| `is_system_admin` | boolean | yes | Whether the user is a system administrator (independent of workspace roles) |
| `preferences` | UserPreferences | yes | Per-user UI/feature preferences. Stored as JSON (jsonb on Postgres, TEXT on SQLite) via the driver.Valuer / sql.Scanner methods on UserPreferences. |
| `created_at` | string(time) | yes | Creation time of the user |
| `updated_at` | string(time) | yes | Last updated time of the user |
| `deleted_at` | DeletedAt | yes | Deletion time of the user |
| `tenant` | Tenant |  | Association relationship, not stored in the database |
