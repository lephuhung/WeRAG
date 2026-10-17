// Resource-aware KB capability projection (frontend permission alignment).
// Runs with: node --experimental-strip-types --test lib/kb-capabilities.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import type { KBCapabilities, KBViewerRow } from "./kb-capabilities.ts";
import {
  getKBCapabilitiesFromAccess,
  getKBViewerCapabilities,
  kbAccessBadge,
} from "./kb-capabilities.ts";

type GetKBCapabilities = (
  kbTenantId: number | string | null | undefined,
  activeTenantId: number | string | null | undefined,
  isTenantAdmin: boolean,
) => KBCapabilities;

async function loadGetKBCapabilities(): Promise<GetKBCapabilities | undefined> {
  try {
    const mod = (await import("./kb-capabilities.ts")) as {
      getKBCapabilities?: GetKBCapabilities;
    };
    return mod.getKBCapabilities;
  } catch {
    return undefined;
  }
}

describe("getKBCapabilities (frontend permission alignment)", () => {
  it("grants own-tenant Member upload and original-download without manage", async () => {
    const getKBCapabilities = await loadGetKBCapabilities();
    if (typeof getKBCapabilities !== "function")
      throw new Error("expected getKBCapabilities to be exported");
    assert.deepEqual(getKBCapabilities(1, 1, false), {
      isForeign: false,
      canManage: false,
      canUpload: true,
      canDownloadOriginal: true,
    });
  });

  it("grants own-tenant Admin manage rights", async () => {
    const getKBCapabilities = await loadGetKBCapabilities();
    if (typeof getKBCapabilities !== "function")
      throw new Error("expected getKBCapabilities to be exported");
    assert.deepEqual(getKBCapabilities(1, 1, true), {
      isForeign: false,
      canManage: true,
      canUpload: true,
      canDownloadOriginal: true,
    });
  });

  it("denies all capabilities for foreign KBs regardless of admin flag", async () => {
    const getKBCapabilities = await loadGetKBCapabilities();
    if (typeof getKBCapabilities !== "function")
      throw new Error("expected getKBCapabilities to be exported");
    assert.deepEqual(getKBCapabilities(2, 1, false), {
      isForeign: true,
      canManage: false,
      canUpload: false,
      canDownloadOriginal: false,
    });
    assert.deepEqual(getKBCapabilities(2, 1, true), {
      isForeign: true,
      canManage: false,
      canUpload: false,
      canDownloadOriginal: false,
    });
  });

  it("fails closed as foreign with no capabilities for missing/zero IDs", async () => {
    const getKBCapabilities = await loadGetKBCapabilities();
    if (typeof getKBCapabilities !== "function")
      throw new Error("expected getKBCapabilities to be exported");
    const denied: KBCapabilities = {
      isForeign: true,
      canManage: false,
      canUpload: false,
      canDownloadOriginal: false,
    };
    assert.deepEqual(getKBCapabilities(null, 1, true), denied);
    assert.deepEqual(getKBCapabilities(1, undefined, true), denied);
    assert.deepEqual(getKBCapabilities(0, 1, true), denied);
    assert.deepEqual(getKBCapabilities(1, 0, false), denied);
  });
});

describe("getKBViewerCapabilities SuperAdmin (Phase 2)", () => {
  const rows: Array<[string, KBViewerRow]> = [
    ["own", { id: "own", owner_tenant_id: 1, tenant_id: 1, visibility: "tenant" }],
    ["public", { id: "pub", owner_tenant_id: 0, tenant_id: 0, visibility: "public" }],
    ["published", { id: "pubd", owner_tenant_id: 9, tenant_id: 9, visibility: "published" }],
    ["invited", { id: "for", owner_tenant_id: 9, tenant_id: 9, visibility: "tenant" }],
  ];
  const all = {
    canView: true,
    canDownloadOriginal: true,
    canManage: true,
    canUpload: true,
    canShare: true,
    canInvite: true,
  };

  for (const [kind, row] of rows) {
    it(`grants every capability on a ${kind} row, with or without an active tenant`, () => {
      const caps = getKBViewerCapabilities(row, {
        activeTenantId: 1, isTenantAdmin: false, isSystemAdmin: true,
      });
      assert.deepEqual(caps, { kind, ...all });
      // Tenantless SuperAdmin: the kind is relative to the active tenant,
      // the rights are not.
      const { kind: _ignored, ...tenantless } = getKBViewerCapabilities(row, {
        activeTenantId: null, isTenantAdmin: false, isSystemAdmin: true,
      });
      assert.deepEqual(tenantless, all);
    });
  }

  it("keeps member rules when not a SuperAdmin", () => {
    const caps = getKBViewerCapabilities(rows[3][1], {
      activeTenantId: 1, isTenantAdmin: true, isSystemAdmin: false,
    });
    assert.equal(caps.canManage, false);
    assert.equal(caps.canDownloadOriginal, false);
  });

  it("grants nothing on a missing row", () => {
    const caps = getKBViewerCapabilities(null, {
      activeTenantId: 1, isTenantAdmin: false, isSystemAdmin: true,
    });
    assert.equal(caps.canView, false);
    assert.equal(caps.canManage, false);
  });
});

describe("access_source projection (Phase 5)", () => {
  const member = { activeTenantId: 1, isTenantAdmin: false, isSystemAdmin: false };
  const base = { id: "kb", owner_tenant_id: 1, tenant_id: 1 };

  it("tenant_admin manages; tenant_scope follows member_default", () => {
    assert.equal(getKBViewerCapabilities({ ...base, visibility: "restricted", access_source: "tenant_admin" }, member).canManage, true);
    const contribute = getKBViewerCapabilities({ ...base, visibility: "tenant", access_source: "tenant_scope" }, member);
    assert.equal(contribute.canUpload, true);
    assert.equal(contribute.canManage, false);
    const viewOnly = getKBViewerCapabilities(
      { ...base, visibility: "tenant", member_default: "view", access_source: "tenant_scope" }, member);
    assert.equal(viewOnly.canView, true);
    assert.equal(viewOnly.canUpload, false);
    assert.equal(viewOnly.canDownloadOriginal, true);
  });

  it("an ACL entry on the caller's own unit's restricted KB is read-only and shared", () => {
    for (const source of ["acl_user", "acl_tenant"]) {
      const row = { ...base, visibility: "restricted", access_source: source };
      const caps = getKBViewerCapabilities(row, { ...member, isTenantAdmin: false });
      assert.deepEqual(caps, {
        kind: "invited", canView: true, canDownloadOriginal: false, canManage: false,
        canUpload: false, canShare: false, canInvite: false,
      });
      assert.equal(kbAccessBadge(row), "shared");
    }
  });

  it("published and public sources read and download, never write", () => {
    const pubd = getKBViewerCapabilities({ id: "p", owner_tenant_id: 9, visibility: "published", access_source: "published" }, member);
    assert.equal(pubd.kind, "published");
    assert.equal(pubd.canDownloadOriginal, true);
    assert.equal(pubd.canUpload, false);
    const pub = getKBViewerCapabilities({ id: "q", owner_tenant_id: 0, visibility: "public", access_source: "public" }, member);
    assert.equal(pub.kind, "public");
    assert.equal(pub.canManage, false);
  });

  it("a SuperAdmin keeps every right whatever the source", () => {
    const caps = getKBViewerCapabilities({ ...base, visibility: "restricted", access_source: "acl_user" },
      { activeTenantId: 1, isTenantAdmin: true, isSystemAdmin: true });
    assert.equal(caps.canManage, true);
    assert.equal(caps.canUpload, true);
  });

  it("restricted row without access_source: a member of the owner unit gets nothing", () => {
    const caps = getKBViewerCapabilities({ ...base, visibility: "restricted" }, member);
    assert.equal(caps.canView, false);
    assert.equal(caps.canUpload, false);
    const admin = getKBViewerCapabilities({ ...base, visibility: "restricted" }, { ...member, isTenantAdmin: true });
    assert.equal(admin.canManage, true);
    assert.equal(kbAccessBadge({ ...base, visibility: "restricted" }), "restricted");
  });

  it("member_default view without access_source drops upload for members only", () => {
    const row = { ...base, visibility: "tenant", member_default: "view" };
    assert.equal(getKBViewerCapabilities(row, member).canUpload, false);
    assert.equal(getKBViewerCapabilities(row, { ...member, isTenantAdmin: true }).canUpload, true);
  });

  it("badges by visibility when the source does not decide", () => {
    assert.equal(kbAccessBadge({ ...base, visibility: "tenant", access_source: "superadmin" }), null);
    assert.equal(kbAccessBadge({ ...base, visibility: "published", access_source: "tenant_scope" }), "published");
    assert.equal(kbAccessBadge({ id: "z", owner_tenant_id: 0, visibility: "public", access_source: "superadmin" }), "public");
    assert.equal(kbAccessBadge(null), null);
  });
});

describe("getKBCapabilitiesFromAccess (overview)", () => {
  const perms = (view: boolean, contribute: boolean, manage: boolean, dl: boolean) => ({
    view, contribute, manage, download_original: dl,
  });
  it("mirrors effective_permissions and labels by the deciding source", () => {
    const caps = getKBCapabilitiesFromAccess({
      owner_tenant_id: 1, visibility: "restricted", effective_permissions: perms(true, false, false, false),
      sources: ["acl_user"],
    });
    assert.deepEqual(caps, {
      kind: "invited", canView: true, canDownloadOriginal: false, canManage: false,
      canUpload: false, canShare: false, canInvite: false,
    });
    const admin = getKBCapabilitiesFromAccess({
      owner_tenant_id: 1, visibility: "published", effective_permissions: perms(true, true, true, true),
      sources: ["tenant_admin"],
    });
    assert.equal(admin.kind, "own");
    assert.equal(admin.canInvite, true);
    const pub = getKBCapabilitiesFromAccess({
      owner_tenant_id: 0, visibility: "public", effective_permissions: perms(true, true, true, true),
      sources: ["superadmin"],
    });
    assert.equal(pub.kind, "public");
  });
});
