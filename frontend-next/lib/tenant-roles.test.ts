// Tenant role model (tenant/KB permission plan).
// Runs with: node --experimental-strip-types --test lib/tenant-roles.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  canCreateKnowledgeBase,
  isMembershipState,
  isTenantAdminRole,
  normalizeTenantRole,
  resolveMembershipState,
} from "./tenant-roles.ts";

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

describe("membership state (Phase 2: explicit Guest)", () => {
  it("accepts only the three known states", () => {
    assert.equal(isMembershipState("superadmin"), true);
    assert.equal(isMembershipState("member"), true);
    assert.equal(isMembershipState("guest"), true);
    assert.equal(isMembershipState("admin"), false);
    assert.equal(isMembershipState(undefined), false);
  });

  it("prefers the server value", () => {
    assert.equal(
      resolveMembershipState({ membershipState: "guest", isSystemAdmin: false, memberships: [{ tenant_id: 3 }] }),
      "guest",
    );
  });

  it("derives SuperAdmin first, then active memberships, else guest", () => {
    assert.equal(resolveMembershipState({ isSystemAdmin: true, memberships: [] }), "superadmin");
    assert.equal(resolveMembershipState({ isSystemAdmin: false, memberships: [{ tenant_id: 3 }] }), "member");
    assert.equal(resolveMembershipState({ isSystemAdmin: false, memberships: [] }), "guest");
    assert.equal(resolveMembershipState({ memberships: [{ tenant_id: 0 }] }), "guest");
    assert.equal(resolveMembershipState({ membershipState: "bogus" }), "guest");
  });

  it("never lets a guest create a KB", () => {
    assert.equal(canCreateKnowledgeBase("guest", true, true), false);
    assert.equal(canCreateKnowledgeBase("member", false, true), false);
    assert.equal(canCreateKnowledgeBase("member", true, true), true);
    assert.equal(canCreateKnowledgeBase("member", true, false), false);
    assert.equal(canCreateKnowledgeBase("superadmin", false, true), true);
    assert.equal(canCreateKnowledgeBase("superadmin", false, false), false);
  });
});
