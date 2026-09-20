# Audit Logs API

Frontend module: `frontend-next/lib/api/audit.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `listAuditLog` — `GET /tenants/:id/audit-log`

handler: `ListTenantAuditLog` · `audit_log.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `after_id` | integer |  | cursor: returns rows with id below this value (starts from newest by default) |
| `limit` | integer |  | page size, 1-100, default 50 |
| `action` | string |  | exact filter on action (e.g. rbac.member_added / rbac.access_denied) |
| `outcome` | string |  | exact filter on outcome (success / denied) |
| `actor` | string |  | exact filter on actor_user_id |

**Response** `auditLogListResponse`:
| field | type | req | notes |
|---|---|---|---|
| `success` | boolean | yes |  |
| `data` | AuditLog[] | yes |  |
| `next_cursor` | number | yes |  |

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
