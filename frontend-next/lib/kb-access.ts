// KB access-management helpers ("Quyền truy cập" tab).
//
// Types mirror GET /knowledge-bases/:id/access
// (internal/handler/kb_access_overview.go). Pure UI helpers only: the
// backend (access.DecideKB) stays authoritative.
//
// This module is intentionally free of the `@/` import alias so it can be
// unit-tested with plain node --test like the other lib/*.test.ts files.

export type KBScope = "tenant" | "restricted" | "published" | "public";
export type KBMemberDefault = "view" | "contribute";
export type KBAccessSourceName =
  | "superadmin"
  | "tenant_admin"
  | "tenant_scope"
  | "acl_user"
  | "acl_tenant"
  | "public"
  | "published";

export interface KBEffectivePermissions {
  view: boolean;
  contribute: boolean;
  manage: boolean;
  download_original: boolean;
}

export interface KBGrantee {
  kind: "user" | "tenant";
  /** User ID or tenant ID of the grantee. */
  id: string;
  /** Invitation or grant row ID — what the revoke endpoints take. */
  share_id: string;
  name: string;
  email?: string;
  tenant_id?: number;
  tenant_name?: string;
  permission: string;
  status: string;
  expires_at?: string | null;
  revoked_reason?: string;
}

export interface KBAccessOverview {
  kb_id: string;
  owner_tenant_id: number;
  owner_tenant_name?: string;
  visibility: KBScope;
  member_default: KBMemberDefault;
  effective_permissions: KBEffectivePermissions;
  /** Every rule that currently gives the caller access, deciding one first. */
  sources: KBAccessSourceName[];
  /** Present only for the KB's managers. */
  grantees?: KBGrantee[];
}

/* i18n keys of the source labels (SuperAdmin / Admin đơn vị / toàn đơn vị /
 * được cấp riêng / được cấp cho đơn vị / công khai / đã công bố). */
export const SOURCE_LABEL_KEYS = {
  superadmin: "kbAccess.src.superadmin",
  tenant_admin: "kbAccess.src.tenantAdmin",
  tenant_scope: "kbAccess.src.tenantScope",
  acl_user: "kbAccess.src.aclUser",
  acl_tenant: "kbAccess.src.aclTenant",
  public: "kbAccess.src.public",
  published: "kbAccess.src.published",
} as const satisfies Record<KBAccessSourceName, string>;

export function sourceLabelKey(source: string): (typeof SOURCE_LABEL_KEYS)[KBAccessSourceName] | null {
  return (SOURCE_LABEL_KEYS as Record<string, (typeof SOURCE_LABEL_KEYS)[KBAccessSourceName]>)[source] ?? null;
}

export interface ScopeOption {
  value: KBScope;
  disabled: boolean;
}

/* Scope choices for the selector. A tenant-owned KB moves among tenant,
 * restricted and published; a SuperAdmin may also promote it to platform
 * public. A platform public KB shows only "public": leaving it is an owner
 * transfer, not a scope change. Everything is disabled for a caller who
 * cannot manage the KB. */
export function scopeOptions(input: {
  current: KBScope;
  ownerTenantId: number;
  isSystemAdmin: boolean;
  canManage: boolean;
}): ScopeOption[] {
  const locked = !input.canManage;
  if (input.current === "public" || input.ownerTenantId === 0) {
    return [{ value: "public", disabled: true }];
  }
  const out: ScopeOption[] = (["tenant", "restricted", "published"] as const).map((value) => ({
    value,
    disabled: locked,
  }));
  if (input.isSystemAdmin) out.push({ value: "public", disabled: locked });
  return out;
}

/* member_default only means something where owner-tenant members read by
 * scope: tenant and published. */
export function showsMemberDefault(scope: KBScope | string | null | undefined): boolean {
  return scope === "tenant" || scope === "published";
}

export function isLiveGrantee(g: Pick<KBGrantee, "status">): boolean {
  return g.status === "pending" || g.status === "accepted" || g.status === "approved";
}

/* Why a grantee keeps reading after its share is revoked: the KB is
 * published/public (everyone signed in reads it), or tenant-wide and the
 * person belongs to the owner unit. null when the revoke ends access. */
export function revokeKeepsAccess(
  overview: Pick<KBAccessOverview, "visibility" | "owner_tenant_id">,
  grantee: Pick<KBGrantee, "kind" | "tenant_id">,
): "published" | "tenant" | null {
  if (overview.visibility === "published" || overview.visibility === "public") return "published";
  if (
    overview.visibility === "tenant" &&
    grantee.kind === "user" &&
    grantee.tenant_id != null &&
    Number(grantee.tenant_id) === Number(overview.owner_tenant_id)
  ) {
    return "tenant";
  }
  return null;
}

export function granteeStatusKey(status: string): string {
  switch (status) {
    case "pending":
      return "kbAccess.status.pending";
    case "accepted":
    case "approved":
      return "kbAccess.status.accepted";
    case "revoked":
      return "kbAccess.status.revoked";
    case "expired":
      return "kbAccess.status.expired";
    default:
      return "kbAccess.status.other";
  }
}

export function revokedReasonKey(reason: string | null | undefined): string | null {
  return reason === "owner_changed" ? "kbAccess.revokedOwnerChanged" : null;
}

/* Default unit for a new personal invite: the owner unit on a restricted
 * KB (same-unit invites are the point of restricted), none otherwise. */
export function defaultInviteTenant(overview: Pick<KBAccessOverview, "visibility" | "owner_tenant_id">): string {
  return overview.visibility === "restricted" && overview.owner_tenant_id ? String(overview.owner_tenant_id) : "";
}
