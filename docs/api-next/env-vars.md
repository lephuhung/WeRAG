# Environment Variables API

Frontend module: `frontend-next/lib/api/env-vars.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `listMyEnvVars` — `GET /me/env-vars`

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
### `setMySkillEnv` — `PUT /me/env-vars/skill`

handler: `SetSkill` · `me_env_var.go`

**Body** `meEnvVarRequest`:
| field | type | req | notes |
|---|---|---|---|
| `skill_id` | string |  |  |
| `sandbox_config_id` | string |  |  |
| `name` | string |  |  |
| `value` | string |  | Value is unused by the delete endpoints. Clearing a value is a delete rather than a write of "", so a member always has one unambiguous way to revoke. |

**Response**:
```json
{ success: true }
```
---
### `deleteMySkillEnv` — `DELETE /me/env-vars/skill`

handler: `DeleteSkill` · `me_env_var.go`

**Body** `meEnvVarRequest`:
| field | type | req | notes |
|---|---|---|---|
| `skill_id` | string |  |  |
| `sandbox_config_id` | string |  |  |
| `name` | string |  |  |
| `value` | string |  | Value is unused by the delete endpoints. Clearing a value is a delete rather than a write of "", so a member always has one unambiguous way to revoke. |

**Response**:
```json
{ success: true }
```
---
### `setMySandboxEnv` — `PUT /me/env-vars/sandbox`

handler: `SetSandbox` · `me_env_var.go`

**Body** `meEnvVarRequest`:
| field | type | req | notes |
|---|---|---|---|
| `skill_id` | string |  |  |
| `sandbox_config_id` | string |  |  |
| `name` | string |  |  |
| `value` | string |  | Value is unused by the delete endpoints. Clearing a value is a delete rather than a write of "", so a member always has one unambiguous way to revoke. |

**Response**:
```json
{ success: true }
```
---
### `deleteMySandboxEnv` — `DELETE /me/env-vars/sandbox`

handler: `DeleteSandbox` · `me_env_var.go`

**Body** `meEnvVarRequest`:
| field | type | req | notes |
|---|---|---|---|
| `skill_id` | string |  |  |
| `sandbox_config_id` | string |  |  |
| `name` | string |  |  |
| `value` | string |  | Value is unused by the delete endpoints. Clearing a value is a delete rather than a write of "", so a member always has one unambiguous way to revoke. |

**Response**:
```json
{ success: true }
```
---