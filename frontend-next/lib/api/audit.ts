/* Ported from frontend/src/api/tenant/audit-log.ts. */
import { apiGet } from "@/lib/api-client";

/* AuditAction mirrors internal/types/audit_log.go's namespaced action
 * enum. The dot prefix (`rbac.`) is deliberate — future namespaces
 * (`kb.*`, `agent.*`) arrive without a schema change, and the backend
 * treats this column as an opaque string. Keep in sync with
 * types/audit_log.go. */
export type AuditAction =
  | "rbac.member_added"
  | "rbac.member_removed"
  | "rbac.member_role_changed"
  | "rbac.member_left"
  | "rbac.access_denied"
  | string; // forward-compat: future namespaces shouldn't break the type

export type AuditOutcome = "accepted" | "success" | "failed" | "partial" | "canceled" | "denied";

/* AuditLog mirrors internal/types/audit_log.go. `details` is the JSONB
 * blob — for role changes it carries `{"old_role":..., "new_role":...}`,
 * for access_denied `{"required_role":...}`. Kept opaque so future detail
 * shapes don't need a frontend breaking change. */
export interface AuditLog {
  id: number;
  tenant_id: number;
  actor_user_id: string;
  actor_role: string;
  action: AuditAction;
  scope_type: string;
  scope_id: string;
  target_type: string;
  target_id: string;
  target_user_id: string;
  request_path: string;
  request_method: string;
  outcome: AuditOutcome;
  details: Record<string, unknown> | string | null;
  created_at: string;
}

export interface ListAuditLogResponse {
  success: boolean;
  data?: AuditLog[];
  next_cursor?: number;
  message?: string;
}

export interface ListAuditLogParams {
  // Cursor: rows with id < after_id, newest first. Pass the previous
  // response's `next_cursor`. Omit on first page.
  after_id?: number;
  // Page size, 1–100. Server defaults to 50 if omitted.
  limit?: number;
  // Optional filters; backend matches on equality.
  action?: AuditAction;
  outcome?: AuditOutcome;
  actor?: string;
}

export function auditLogQueryString(params: ListAuditLogParams = {}): string {
  const qs = new URLSearchParams();
  if (params.after_id) qs.append("after_id", String(params.after_id));
  if (params.limit) qs.append("limit", String(params.limit));
  if (params.action) qs.append("action", params.action);
  if (params.outcome) qs.append("outcome", params.outcome);
  if (params.actor) qs.append("actor", params.actor);
  return qs.toString();
}

/* Per-tenant audit log, cursor-paginated by descending id.
 * Backend: GET /api/v1/tenants/:id/audit-log (Admin+).
 * First call: no cursor. Subsequent pages pass
 * `after_id = previousResponse.next_cursor` until it returns 0. */
export function listAuditLog(
  tenantId: number,
  params: ListAuditLogParams = {},
): Promise<ListAuditLogResponse> {
  const tail = auditLogQueryString(params);
  return apiGet(`/api/v1/tenants/${tenantId}/audit-log${tail ? `?${tail}` : ""}`);
}

/* Platform-wide system audit log (SystemAdmin only).
 * Backend: GET /api/v1/system/admin/audit-log */
export function listSystemAuditLog(
  params: ListAuditLogParams = {},
): Promise<ListAuditLogResponse> {
  const tail = auditLogQueryString(params);
  return apiGet(`/api/v1/system/admin/audit-log${tail ? `?${tail}` : ""}`);
}
