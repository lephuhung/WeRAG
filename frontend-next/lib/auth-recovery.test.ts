// RED: /auth/me failures must recover a stale tenant once, retain tokens
// on transient failure, and clear only on confirmed invalid auth.
// Runs with: node --experimental-strip-types --test lib/auth-recovery.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { ApiError } from "./api-client.ts";
import {
  isStaleTenantGuest,
  isTenantMembershipRequired,
  isTenantRequired,
  loadWithStaleTenantRecovery,
  tenantErrorCode,
} from "./auth-recovery.ts";

const unauthorized = () => new ApiError(401, "unauthorized");
const forbidden = () => new ApiError(403, "forbidden", { success: false });
const serverError = () => new ApiError(500, "Request failed");
const networkError = () => new TypeError("fetch failed");

describe("loadWithStaleTenantRecovery (issue 4)", () => {
  it("passes through success without a retry", async () => {
    let loads = 0;
    const outcome = await loadWithStaleTenantRecovery(
      async () => {
        loads++;
        return { user: "u1" };
      },
      true,
    );
    assert.equal(outcome.kind, "ok");
    assert.equal(loads, 1);
    assert.equal(outcome.kind === "ok" && outcome.recoveredFromStaleTenant, false);
  });

  it("retries once without the tenant header after a 401 and recovers", async () => {
    const seen: boolean[] = [];
    const outcome = await loadWithStaleTenantRecovery(async (skipTenant: boolean) => {
      seen.push(skipTenant);
      if (!skipTenant) throw unauthorized();
      return { user: "u1" };
    }, true);
    assert.equal(outcome.kind, "ok");
    assert.deepEqual(seen, [false, true]);
    assert.equal(outcome.kind === "ok" && outcome.recoveredFromStaleTenant, true);
  });

  it("retries once without the tenant header after a 403 and recovers", async () => {
    const seen: boolean[] = [];
    const outcome = await loadWithStaleTenantRecovery(async (skipTenant: boolean) => {
      seen.push(skipTenant);
      if (!skipTenant) throw forbidden();
      return { user: "u1" };
    }, true);
    assert.equal(outcome.kind, "ok");
    assert.deepEqual(seen, [false, true]);
  });

  it("reports invalid-auth when the tenantless retry also 401s", async () => {
    let loads = 0;
    const outcome = await loadWithStaleTenantRecovery(async () => {
      loads++;
      throw unauthorized();
    }, true);
    assert.equal(outcome.kind, "invalid-auth");
    assert.equal(loads, 2);
  });

  it("does not retry when no tenant was stored — straight to invalid-auth", async () => {
    let loads = 0;
    const outcome = await loadWithStaleTenantRecovery(async () => {
      loads++;
      throw unauthorized();
    }, false);
    assert.equal(outcome.kind, "invalid-auth");
    assert.equal(loads, 1);
  });

  it("retains on transient failures without retrying (network, 5xx)", async () => {
    for (const failure of [networkError(), serverError()]) {
      let loads = 0;
      const outcome = await loadWithStaleTenantRecovery(async () => {
        loads++;
        throw failure;
      }, true);
      assert.equal(outcome.kind, "transient");
      assert.equal(loads, 1);
    }
  });

  it("retains when the tenantless retry hits a transient failure", async () => {
    const outcome = await loadWithStaleTenantRecovery(async (skipTenant: boolean) => {
      if (!skipTenant) throw unauthorized();
      throw serverError();
    }, true);
    assert.equal(outcome.kind, "transient");
  });
});

describe("loadWithStaleTenantRecovery on tenant-header 400 (issue 1)", () => {
  // Real backend bodies from internal/middleware/auth.go:318-344 — a stale
  // stored X-Tenant-ID (deleted tenant / lost membership) is rejected with
  // HTTP 400, not 401/403. Shaped exactly as api-client builds them:
  // new ApiError(status, envelopeMessage(body), body).
  const deletedTenant400 = () =>
    new ApiError(400, "Invalid target workspace ID", {
      error: "Invalid target workspace ID",
    });
  const malformedHeader400 = () =>
    new ApiError(400, "Invalid X-Tenant-ID header", {
      error: "Invalid X-Tenant-ID header",
    });
  const arbitrary400 = () =>
    new ApiError(400, "Request failed", {
      success: false,
      error: "Bad input",
    });

  it("retries once tenantless after a deleted-tenant 400 and recovers", async () => {
    const seen: boolean[] = [];
    const outcome = await loadWithStaleTenantRecovery(async (skipTenant: boolean) => {
      seen.push(skipTenant);
      if (!skipTenant) throw deletedTenant400();
      return { user: "u1" };
    }, true);
    assert.equal(outcome.kind, "ok");
    assert.deepEqual(seen, [false, true]);
  });

  it("treats the organization wording of the deleted-tenant 400 the same way", async () => {
    const seen: boolean[] = [];
    const outcome = await loadWithStaleTenantRecovery(async (skipTenant: boolean) => {
      seen.push(skipTenant);
      if (!skipTenant)
        throw new ApiError(400, "Invalid target organization ID", { error: "Invalid target organization ID" });
      return { user: "u1" };
    }, true);
    assert.equal(outcome.kind, "ok");
    assert.deepEqual(seen, [false, true]);
    assert.equal(outcome.kind === "ok" && outcome.recoveredFromStaleTenant, true);
  });

  it("retries once tenantless after a malformed-header 400 and recovers", async () => {
    const seen: boolean[] = [];
    const outcome = await loadWithStaleTenantRecovery(async (skipTenant: boolean) => {
      seen.push(skipTenant);
      if (!skipTenant) throw malformedHeader400();
      return { user: "u1" };
    }, true);
    assert.equal(outcome.kind, "ok");
    assert.deepEqual(seen, [false, true]);
  });

  it("does NOT retry an arbitrary 400 — transient, tokens retained", async () => {
    let loads = 0;
    const outcome = await loadWithStaleTenantRecovery(async () => {
      loads++;
      throw arbitrary400();
    }, true);
    assert.equal(outcome.kind, "transient");
    assert.equal(loads, 1);
  });

  it("does not treat a tenant 400 as invalid-auth when no tenant was stored", async () => {
    let loads = 0;
    const outcome = await loadWithStaleTenantRecovery(async () => {
      loads++;
      throw deletedTenant400();
    }, false);
    // No stored tenant means the 400 did not come from our header: retain.
    assert.equal(outcome.kind, "transient");
    assert.equal(loads, 1);
  });
});

describe("TENANT_MEMBERSHIP_REQUIRED (stale home tenant guest)", () => {
  // internal/middleware/auth.go: valid session, tenant without membership.
  const membershipRequired = () =>
    new ApiError(403, "Forbidden: not a member of the target organization", {
      error: "Forbidden: not a member of the target organization",
      code: "TENANT_MEMBERSHIP_REQUIRED",
    });

  it("recognises only the coded 403", () => {
    assert.equal(isTenantMembershipRequired(membershipRequired()), true);
    assert.equal(isTenantMembershipRequired(forbidden()), false);
    assert.equal(
      isTenantMembershipRequired(new ApiError(409, "x", { code: "TENANT_MEMBERSHIP_REQUIRED" })),
      false,
    );
    assert.equal(isTenantMembershipRequired(networkError()), false);
  });

  it("retries tenantless and recovers, flagging the stale tenant", async () => {
    const seen: boolean[] = [];
    const outcome = await loadWithStaleTenantRecovery(async (skipTenant: boolean) => {
      seen.push(skipTenant);
      if (!skipTenant) throw membershipRequired();
      return { user: "u1" };
    }, true);
    assert.equal(outcome.kind, "ok");
    assert.deepEqual(seen, [false, true]);
    assert.equal(outcome.kind === "ok" && outcome.recoveredFromStaleTenant, true);
  });

  it("never signs out: a repeated membership 403 is transient", async () => {
    const outcome = await loadWithStaleTenantRecovery(async () => {
      throw membershipRequired();
    }, true);
    assert.equal(outcome.kind, "transient");
  });

  it("never signs out even with no stored tenant", async () => {
    let loads = 0;
    const outcome = await loadWithStaleTenantRecovery(async () => {
      loads++;
      throw membershipRequired();
    }, false);
    assert.equal(outcome.kind, "transient");
    assert.equal(loads, 1);
  });

  it("isStaleTenantGuest: tenantless non-SuperAdmin only", () => {
    assert.equal(isStaleTenantGuest({ tenant_required: true, user: {} }), true);
    assert.equal(
      isStaleTenantGuest({ tenant_required: true, user: { is_system_admin: true } }),
      false,
    );
    assert.equal(isStaleTenantGuest({ tenant_required: false, user: {} }), false);
    assert.equal(isStaleTenantGuest(undefined), false);
  });

  it("TENANT_REQUIRED (409) is a tenant rejection: retried tenantless, never a sign-out", async () => {
    const tenantRequired = () =>
      new ApiError(409, "Organization required", { error: "Organization required", code: "TENANT_REQUIRED" });
    assert.equal(isTenantRequired(tenantRequired()), true);
    assert.equal(isTenantRequired(new ApiError(409, "busy", { code: 1005 })), false);
    assert.equal(isTenantRequired(new ApiError(403, "x", { code: "TENANT_REQUIRED" })), false);

    const skips: boolean[] = [];
    const recovered = await loadWithStaleTenantRecovery(async (skip) => {
      skips.push(skip);
      if (!skip) throw tenantRequired();
      return "me";
    }, true);
    assert.deepEqual(recovered, { kind: "ok", value: "me", recoveredFromStaleTenant: true });
    assert.deepEqual(skips, [false, true]);

    const noTenant = await loadWithStaleTenantRecovery(async () => {
      throw tenantRequired();
    }, false);
    assert.equal(noTenant.kind, "transient");
  });

  it("tenantErrorCode reads flat and nested string codes only", () => {
    assert.equal(tenantErrorCode({ code: "TENANT_MEMBERSHIP_REQUIRED" }), "TENANT_MEMBERSHIP_REQUIRED");
    assert.equal(tenantErrorCode({ error: { code: "TENANT_REQUIRED" } }), "TENANT_REQUIRED");
    assert.equal(tenantErrorCode({ error: { code: 1005 } }), null);
    assert.equal(tenantErrorCode("TENANT_REQUIRED"), null);
    assert.equal(tenantErrorCode(null), null);
    assert.equal(
      isTenantMembershipRequired(new ApiError(403, "x", { error: { code: "TENANT_MEMBERSHIP_REQUIRED" } })),
      true,
    );
  });
});
