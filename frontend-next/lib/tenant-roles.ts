// Human tenant role model (tenant/KB permission plan).
//
// Exactly Admin and Member at tenant scope, with SuperAdmin as the platform
// flag (user.is_system_admin) rather than a membership role. "owner" is a
// legacy backend alias for pre-migration rows — normalize it to "admin".
// Never assign "owner" (or contributor/viewer) to new memberships.
//
// This module is intentionally free of the `@/` import alias so it can be
// unit-tested with plain node --test like the other lib/*.test.ts files.

export type TenantRole = "owner" | "admin" | "member";

/** Assignable tenant roles for new memberships/invitations. */
export type AssignableTenantRole = "admin" | "member";

/** Collapse legacy/unknown role strings to the approved two-role model:
 * owner -> admin (legacy alias); contributor/viewer/unknown -> member. */
export function normalizeTenantRole(role: string | null | undefined): AssignableTenantRole {
  if (role === "admin" || role === "owner") return "admin";
  return "member";
}

/** True when the (possibly legacy) role carries Tenant Admin authority. */
export function isTenantAdminRole(role: string | null | undefined): boolean {
  return role === "admin" || role === "owner";
}

/** Account kind reported by GET /auth/me `membership_state`.
 * - superadmin: platform administrator (user.is_system_admin); every right
 *   on every KB, independent of tenant membership.
 * - member: at least one active tenant membership.
 * - guest: signed in with no active membership — reads only public and
 *   published KBs, cannot create or own KBs, gets no ACL. */
export type MembershipState = "superadmin" | "member" | "guest";

export function isMembershipState(value: unknown): value is MembershipState {
  return value === "superadmin" || value === "member" || value === "guest";
}

/** Resolve the account kind. The server's `membership_state` wins when it
 * is a known value; otherwise (older backend) derive it the same way the
 * server does: SuperAdmin first, then any active membership, else guest. */
export function resolveMembershipState(input: {
  membershipState?: unknown;
  isSystemAdmin?: boolean | null;
  memberships?: ReadonlyArray<{ tenant_id?: number | string | null }> | null;
}): MembershipState {
  if (isMembershipState(input.membershipState)) return input.membershipState;
  if (input.isSystemAdmin === true) return "superadmin";
  const hasMembership = (input.memberships ?? []).some(
    (m) => m != null && m.tenant_id != null && String(m.tenant_id).trim() !== "" && String(m.tenant_id) !== "0",
  );
  return hasMembership ? "member" : "guest";
}

/** Display gate for "create KB": never for a guest; a SuperAdmin or a
 * Tenant Admin needs a selected unit to create into. The backend remains
 * authoritative (TenantAdmin guard + TENANT_REQUIRED). */
export function canCreateKnowledgeBase(
  state: MembershipState,
  isTenantAdmin: boolean,
  hasTenant: boolean,
): boolean {
  if (state === "guest" || !hasTenant) return false;
  return state === "superadmin" || isTenantAdmin;
}
