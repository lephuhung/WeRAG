// Resource-aware KB capability projection (frontend permission alignment).
// Runs with: node --experimental-strip-types --test lib/kb-capabilities.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import type { KBCapabilities, KBViewerRow } from "./kb-capabilities.ts";
import { getKBViewerCapabilities } from "./kb-capabilities.ts";

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
