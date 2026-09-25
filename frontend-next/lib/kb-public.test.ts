// Task 5: public catalog + viewer capability matrix (frontend permission alignment).
// Runs with: node --experimental-strip-types --test lib/kb-public.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  getKBViewerCapabilities,
  isPublicKBRow,
  type KBViewerCapabilities,
  type KBViewerRow,
} from "./kb-capabilities.ts";
import {
  buildPublicCreatePayload,
  buildScopeChangePayload,
  groupCatalogRows,
  normalizePublicCatalog,
  selectPublicSection,
} from "./kb-public.ts";

const ownRow: KBViewerRow = { id: "own", owner_tenant_id: 1, tenant_id: 1, visibility: "tenant" };
const publicRow: KBViewerRow = { id: "pub", owner_tenant_id: 0, tenant_id: 0, visibility: "public" };
const convertedPublicRow: KBViewerRow = { id: "pub2", owner_tenant_id: 0, tenant_id: 7, visibility: "public" };
const invitedRow: KBViewerRow = { id: "inv", owner_tenant_id: 9, tenant_id: 9, visibility: "tenant" };
const foreignRow: KBViewerRow = { id: "for", owner_tenant_id: 9, tenant_id: 9, visibility: "tenant" };

describe("isPublicKBRow (owner, never data scope)", () => {
  it("detects owner 0 + public visibility only", async () => {
    assert.equal(isPublicKBRow(publicRow), true);
    assert.equal(isPublicKBRow(convertedPublicRow), true);
    assert.equal(isPublicKBRow(ownRow), false);
    assert.equal(isPublicKBRow(invitedRow), false);
    // Malformed nonzero-owner public row is never platform-public.
    assert.equal(isPublicKBRow({ id: "x", owner_tenant_id: 9, tenant_id: 9, visibility: "public" }), false);
    // Legacy row without owner info is never public.
    assert.equal(isPublicKBRow({ id: "x", tenant_id: 1, visibility: "tenant" }), false);
  });
});

describe("getKBViewerCapabilities matrix", () => {
  it("own-tenant Member keeps upload/download without manage", async () => {
    const caps: KBViewerCapabilities = getKBViewerCapabilities(ownRow, {
      activeTenantId: 1, isTenantAdmin: false, isSystemAdmin: false,
    });
    assert.equal(caps.kind, "own");
    assert.equal(caps.canManage, false);
    assert.equal(caps.canUpload, true);
    assert.equal(caps.canDownloadOriginal, true);
    assert.equal(caps.canShare, false);
    assert.equal(caps.canInvite, false);
  });

  it("own-tenant Admin manages, shares and invites", async () => {
    const caps = getKBViewerCapabilities(ownRow, {
      activeTenantId: 1, isTenantAdmin: true, isSystemAdmin: false,
    });
    assert.equal(caps.kind, "own");
    assert.equal(caps.canManage, true);
    assert.equal(caps.canUpload, true);
    assert.equal(caps.canShare, true);
    assert.equal(caps.canInvite, true);
  });

  it("invited foreign row is read-only with no original download", async () => {
    const caps = getKBViewerCapabilities(invitedRow, {
      activeTenantId: 1, isTenantAdmin: true, isSystemAdmin: false,
    });
    assert.equal(caps.kind, "invited");
    assert.equal(caps.canView, true);
    assert.equal(caps.canManage, false);
    assert.equal(caps.canUpload, false);
    assert.equal(caps.canDownloadOriginal, false);
    assert.equal(caps.canShare, false);
    assert.equal(caps.canInvite, false);
  });

  it("public Member views/previews/downloads but never mutates", async () => {
    const caps = getKBViewerCapabilities(publicRow, {
      activeTenantId: 1, isTenantAdmin: false, isSystemAdmin: false,
    });
    assert.equal(caps.kind, "public");
    assert.equal(caps.canView, true);
    assert.equal(caps.canDownloadOriginal, true);
    assert.equal(caps.canManage, false);
    assert.equal(caps.canUpload, false);
    assert.equal(caps.canShare, false);
    assert.equal(caps.canInvite, false);
  });

  it("public explicit SuperAdmin alone manages/uploads", async () => {
    const caps = getKBViewerCapabilities(convertedPublicRow, {
      activeTenantId: 1, isTenantAdmin: false, isSystemAdmin: true,
    });
    assert.equal(caps.kind, "public");
    assert.equal(caps.canManage, true);
    assert.equal(caps.canUpload, true);
    assert.equal(caps.canDownloadOriginal, true);
    assert.equal(caps.canShare, false);
    assert.equal(caps.canInvite, false);
  });

  it("SuperAdmin/CanAccessAllTenants never manages foreign tenant-owned KBs", async () => {
    const caps = getKBViewerCapabilities(foreignRow, {
      activeTenantId: 1, isTenantAdmin: true, isSystemAdmin: true,
    });
    assert.equal(caps.kind, "invited");
    assert.equal(caps.canManage, false);
    assert.equal(caps.canUpload, false);
    assert.equal(caps.canDownloadOriginal, false);
  });

  it("converted tenant-owned row manages when owner matches active tenant", async () => {
    // Regression pin for the list share-modal wiring: ownership keys off
    // the owner, so a converted row (owner 1, platform data scope 0) is
    // manageable by the owning tenant's Admin. The legacy data-scope
    // helper misreads this row; call sites must use getKBViewerCapabilities.
    const converted: KBViewerRow = { id: "conv", owner_tenant_id: 1, tenant_id: 0, visibility: "tenant" };
    const convCaps = getKBViewerCapabilities(converted, {
      activeTenantId: 1, isTenantAdmin: true, isSystemAdmin: false,
    });
    assert.equal(convCaps.kind, "own");
    assert.equal(convCaps.canManage, true);
    assert.equal(convCaps.canUpload, true);
    // …while a converted public row stays SuperAdmin-only for admins.
    const pubCaps = getKBViewerCapabilities(convertedPublicRow, {
      activeTenantId: 1, isTenantAdmin: true, isSystemAdmin: false,
    });
    assert.equal(pubCaps.kind, "public");
    assert.equal(pubCaps.canManage, false);
  });
});

describe("groupCatalogRows", () => {
  it("groups workspace/public/invited and dedupes by id", async () => {
    const rows: KBViewerRow[] = [ownRow, publicRow, invitedRow, { ...publicRow }];
    const g = groupCatalogRows(rows, 1);
    assert.deepEqual(g.workspace.map((r) => r.id), ["own"]);
    assert.deepEqual(g.public.map((r) => r.id), ["pub"]);
    assert.deepEqual(g.invited.map((r) => r.id), ["inv"]);
  });
});

describe("selectPublicSection (endpoint page authoritative)", () => {
  const page2: KBViewerRow[] = [
    { id: "pub-10", owner_tenant_id: 0, tenant_id: 0, visibility: "public" },
  ];
  it("renders only the selected endpoint page once loaded", async () => {
    const rows = selectPublicSection([publicRow], { items: page2, ok: true });
    assert.deepEqual(rows.map((r) => r.id), ["pub-10"]);
  });
  it("falls back to main-list rows while loading or on error", async () => {
    const rows = selectPublicSection([publicRow], { items: [], ok: false });
    assert.deepEqual(rows.map((r) => r.id), ["pub"]);
  });
  it("dedupes by id within the chosen page", async () => {
    const rows = selectPublicSection([], {
      items: [publicRow, { ...publicRow }],
      ok: true,
    });
    assert.deepEqual(rows.map((r) => r.id), ["pub"]);
  });
  it("empty successful page renders empty, never main-list fallback", async () => {
    const rows = selectPublicSection([publicRow], { items: [], ok: true });
    assert.deepEqual(rows, []);
  });
});

describe("normalizePublicCatalog", () => {
  it("reads data.items/total/page/page_size with sane defaults", async () => {
    const norm = normalizePublicCatalog({ success: true, data: { items: [publicRow], total: 42, page: 2, page_size: 10 } });
    assert.equal(norm.total, 42);
    assert.equal(norm.page, 2);
    assert.equal(norm.pageSize, 10);
    assert.deepEqual(norm.items, [publicRow]);
  });

  it("defaults empty payloads to a valid empty page", async () => {
    const norm = normalizePublicCatalog({ success: false });
    assert.deepEqual(norm.items, []);
    assert.equal(norm.total, 0);
    assert.equal(norm.page, 1);
    assert.equal(norm.pageSize, 50);
  });
});

describe("public API payload builders", () => {
  it("buildPublicCreatePayload never carries tenant defaults/backends", async () => {
    const payload = buildPublicCreatePayload({
      name: "Laws", description: "d", type: "document",
      storage_backend_id: "tenant-backend", vector_store_id: "vs",
      storage_provider_config: { provider: "minio" },
    });
    assert.deepEqual(payload, { name: "Laws", description: "d", type: "document" });
  });

  it("buildScopeChangePayload requires an existing nonzero target for public-to-tenant", async () => {
    assert.deepEqual(buildScopeChangePayload("public", "kb-1", 7), {
      visibility: "tenant", target_tenant_id: 7,
    });
    assert.throws(() => buildScopeChangePayload("public", "kb-1", 0), /target tenant/);
    assert.deepEqual(buildScopeChangePayload("tenant", "kb-1"), { visibility: "public" });
  });
});
