// RED: /auth/me failures must recover a stale tenant once, retain tokens
// on transient failure, and clear only on confirmed invalid auth.
// Runs with: node --experimental-strip-types --test lib/auth-recovery.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { ApiError } from "./api-client.ts";
import { loadWithStaleTenantRecovery } from "./auth-recovery.ts";

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
