// Tenant role model (tenant/KB permission plan).
// Runs with: node --experimental-strip-types --test lib/tenant-roles.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { isTenantAdminRole, normalizeTenantRole } from "./tenant-roles.ts";

describe("tenant role model (tenant/KB permission plan)", () => {
  it("normalizes legacy owner to admin", () => {
    assert.equal(normalizeTenantRole("owner"), "admin");
    assert.equal(normalizeTenantRole("admin"), "admin");
  });

  it("collapses contributor/viewer/unknown to member", () => {
    assert.equal(normalizeTenantRole("member"), "member");
    assert.equal(normalizeTenantRole("contributor"), "member");
    assert.equal(normalizeTenantRole("viewer"), "member");
    assert.equal(normalizeTenantRole(""), "member");
    assert.equal(normalizeTenantRole(undefined), "member");
  });

  it("grants tenant-admin authority to admin and legacy owner only", () => {
    assert.equal(isTenantAdminRole("admin"), true);
    assert.equal(isTenantAdminRole("owner"), true);
    assert.equal(isTenantAdminRole("member"), false);
    assert.equal(isTenantAdminRole("viewer"), false);
    assert.equal(isTenantAdminRole(""), false);
  });
});
