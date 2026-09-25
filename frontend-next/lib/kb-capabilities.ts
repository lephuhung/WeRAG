// Resource-aware KB capability projection (frontend permission alignment).
//
// Pure UI helper only: it projects tenant-scoped KB capabilities for display
// gating. It does not authorize backend requests.
//
// This module is intentionally free of the `@/` import alias so it can be
// unit-tested with plain node --test like the other lib/*.test.ts files.

export interface KBCapabilities {
  isForeign: boolean;
  canManage: boolean;
  canUpload: boolean;
  canDownloadOriginal: boolean;
}

/* Minimal owner/visibility projection of a KB row. UI ownership is derived
 * from owner_tenant_id + visibility, never from the data-scope tenant_id
 * alone: a converted public row may retain a foreign/zero data scope while
 * belonging to the platform, and a converted tenant row may keep a
 * platform data scope while belonging to its owning tenant. */
export interface KBViewerRow {
  id: string;
  name?: string;
  owner_tenant_id?: number | string | null;
  tenant_id?: number | string | null;
  visibility?: string | null;
}

export type KBViewerKind = "own" | "public" | "invited" | "foreign";

export interface KBViewerCapabilities {
  kind: KBViewerKind;
  canView: boolean;
  canDownloadOriginal: boolean;
  canManage: boolean;
  canUpload: boolean;
  canShare: boolean;
  canInvite: boolean;
}

export interface KBViewerContext {
  activeTenantId: number | string | null | undefined;
  isTenantAdmin: boolean;
  /* Explicit platform SuperAdmin (user.is_system_admin). Never implied by
   * CanAccessAllTenants, and never carried by API-key sessions (which are
   * not modeled as human UI sessions). */
  isSystemAdmin: boolean;
}

function normalizeOwnerId(value: number | string | null | undefined): string {
  if (value === null || value === undefined) return "";
  return String(value).trim();
}

/* Platform-owned public identity: owner 0 AND public visibility. A nonzero
 * owner with public visibility is malformed and never platform-public. */
export function isPublicKBRow(row: KBViewerRow | null | undefined): boolean {
  if (!row) return false;
  return normalizeOwnerId(row.owner_tenant_id) === "0" && row.visibility === "public";
}

/* Owner-aware viewer capability projection (display gates only; the backend
 * remains authoritative). Public rows: any authenticated human views and
 * downloads originals; only an explicit SuperAdmin manages/uploads. Tenant
 * rows keep the existing role rules; foreign tenant-owned rows stay
 * read-only even for SuperAdmins (platform authority never overrides tenant
 * ownership without owning-tenant membership). Invited rows are read-only
 * with no original download, matching backend invite semantics. */
export function getKBViewerCapabilities(
  row: KBViewerRow | null | undefined,
  ctx: KBViewerContext,
): KBViewerCapabilities {
  const denied: KBViewerCapabilities = {
    kind: "foreign",
    canView: false,
    canDownloadOriginal: false,
    canManage: false,
    canUpload: false,
    canShare: false,
    canInvite: false,
  };
  if (!row || !row.id) return denied;
  if (isPublicKBRow(row)) {
    const privileged = ctx.isSystemAdmin === true;
    return {
      kind: "public",
      canView: true,
      canDownloadOriginal: true,
      canManage: privileged,
      canUpload: privileged,
      canShare: false,
      canInvite: false,
    };
  }
  const owner = normalizeOwnerId(row.owner_tenant_id);
  const active = normalizeTenantId(ctx.activeTenantId);
  const dataScope = normalizeTenantId(row.tenant_id);
  // Own rows: explicit owner match, or legacy rows without owner info that
  // live in the active data scope with tenant visibility.
  const owned =
    (owner !== "" && active !== "" && owner === active) ||
    (owner === "" && active !== "" && dataScope === active && row.visibility !== "public");
  if (owned) {
    const admin = ctx.isTenantAdmin === true;
    return {
      kind: "own",
      canView: true,
      canDownloadOriginal: true,
      canManage: admin,
      canUpload: true,
      canShare: admin,
      canInvite: admin,
    };
  }
  // Foreign tenant-owned rows (including accepted invites): read-only, no
  // original download, no mutation — regardless of admin or SuperAdmin
  // flags. UI affordances only; the backend enforces per-row access.
  // Labeled "invited": rows reachable outside the active tenant arrive
  // through recipient-bound invitations (the catalog never leaks foreign
  // tenant rows otherwise).
  return {
    kind: "invited",
    canView: true,
    canDownloadOriginal: false,
    canManage: false,
    canUpload: false,
    canShare: false,
    canInvite: false,
  };
}

function normalizeTenantId(value: number | string | null | undefined): string {
  if (value === null || value === undefined) return "";
  const normalized = String(value).trim();
  if (normalized === "" || normalized === "0") return "";
  return normalized;
}

export function getKBCapabilities(
  kbTenantId: number | string | null | undefined,
  activeTenantId: number | string | null | undefined,
  isTenantAdmin: boolean,
): KBCapabilities {
  const kb = normalizeTenantId(kbTenantId);
  const active = normalizeTenantId(activeTenantId);
  if (kb === "" || active === "" || kb !== active) {
    return {
      isForeign: true,
      canManage: false,
      canUpload: false,
      canDownloadOriginal: false,
    };
  }
  return {
    isForeign: false,
    canManage: isTenantAdmin,
    canUpload: true,
    canDownloadOriginal: true,
  };
}
