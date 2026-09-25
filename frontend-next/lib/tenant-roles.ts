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
