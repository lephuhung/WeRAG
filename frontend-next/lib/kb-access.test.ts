// KB access tab helpers (Phase 5).
// Runs with: node --experimental-strip-types --test lib/kb-access.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  defaultInviteTenant,
  granteeStatusKey,
  isLiveGrantee,
  revokedReasonKey,
  revokeKeepsAccess,
  scopeOptions,
  showsMemberDefault,
  sourceLabelKey,
} from "./kb-access.ts";

describe("scopeOptions", () => {
  it("offers tenant/restricted/published to a manager, public only to a SuperAdmin", () => {
    const admin = scopeOptions({ current: "tenant", ownerTenantId: 1, isSystemAdmin: false, canManage: true });
    assert.deepEqual(admin.map((o) => o.value), ["tenant", "restricted", "published"]);
    assert.ok(admin.every((o) => !o.disabled));
    const sa = scopeOptions({ current: "restricted", ownerTenantId: 1, isSystemAdmin: true, canManage: true });
    assert.deepEqual(sa.map((o) => o.value), ["tenant", "restricted", "published", "public"]);
  });

  it("disables every option for a caller who cannot manage", () => {
    const opts = scopeOptions({ current: "tenant", ownerTenantId: 1, isSystemAdmin: false, canManage: false });
    assert.ok(opts.every((o) => o.disabled));
  });

  it("a platform public KB only shows public: leaving it is an owner transfer", () => {
    const opts = scopeOptions({ current: "public", ownerTenantId: 0, isSystemAdmin: true, canManage: true });
    assert.deepEqual(opts, [{ value: "public", disabled: true }]);
  });
});

describe("member_default and sources", () => {
  it("member_default only for tenant and published", () => {
    assert.equal(showsMemberDefault("tenant"), true);
    assert.equal(showsMemberDefault("published"), true);
    assert.equal(showsMemberDefault("restricted"), false);
    assert.equal(showsMemberDefault("public"), false);
  });

  it("labels every source and nothing else", () => {
    for (const s of ["superadmin", "tenant_admin", "tenant_scope", "acl_user", "acl_tenant", "public", "published"]) {
      assert.ok(sourceLabelKey(s)?.startsWith("kbAccess.src."), s);
    }
    assert.equal(sourceLabelKey("owner"), null);
  });
});

describe("grantees", () => {
  it("live statuses and labels", () => {
    assert.equal(isLiveGrantee({ status: "pending" }), true);
    assert.equal(isLiveGrantee({ status: "approved" }), true);
    assert.equal(isLiveGrantee({ status: "revoked" }), false);
    assert.equal(granteeStatusKey("approved"), "kbAccess.status.accepted");
    assert.equal(granteeStatusKey("expired"), "kbAccess.status.expired");
    assert.equal(revokedReasonKey("owner_changed"), "kbAccess.revokedOwnerChanged");
    assert.equal(revokedReasonKey(""), null);
  });

  it("warns when the person keeps reading after a revoke", () => {
    const user = { kind: "user" as const, tenant_id: 1 };
    const other = { kind: "user" as const, tenant_id: 2 };
    const unit = { kind: "tenant" as const, tenant_id: 2 };
    assert.equal(revokeKeepsAccess({ visibility: "published", owner_tenant_id: 1 }, unit), "published");
    assert.equal(revokeKeepsAccess({ visibility: "tenant", owner_tenant_id: 1 }, user), "tenant");
    assert.equal(revokeKeepsAccess({ visibility: "tenant", owner_tenant_id: 1 }, other), null);
    assert.equal(revokeKeepsAccess({ visibility: "restricted", owner_tenant_id: 1 }, user), null);
  });

  it("defaults the invite unit to the owner on a restricted KB", () => {
    assert.equal(defaultInviteTenant({ visibility: "restricted", owner_tenant_id: 4 }), "4");
    assert.equal(defaultInviteTenant({ visibility: "tenant", owner_tenant_id: 4 }), "");
  });
});
