// Runs with: node --experimental-strip-types --test lib/plan-checkout.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { checkoutFromSearch, planCheckoutPath, withNext } from "./plan-checkout.ts";
import { resolveSafeNextPath } from "./safe-next.ts";

describe("plan checkout links", () => {
  it("builds the shop deep link and survives the next= round trip", () => {
    const path = planCheckoutPath("user", "d97ee9e4-f484-420b-a8e7-562a88810192");
    assert.equal(path, "/platform/system/workspace/billing?scope=user&plan=d97ee9e4-f484-420b-a8e7-562a88810192");
    const href = withNext("/register", path);
    const next = new URL(href, "http://x").searchParams.get("next");
    assert.equal(next, path);
    assert.equal(resolveSafeNextPath(next), path);
  });

  it("leaves the base alone without a next", () => {
    assert.equal(withNext("/login", ""), "/login");
    assert.equal(withNext("/login", null), "/login");
  });

  it("reads scope and plan, refusing bad pairs", () => {
    assert.deepEqual(checkoutFromSearch("?scope=tenant&plan=abc"), { scope: "tenant", planId: "abc" });
    assert.equal(checkoutFromSearch("?scope=admin&plan=abc"), null);
    assert.equal(checkoutFromSearch("?scope=user"), null);
    assert.equal(checkoutFromSearch(`?scope=user&plan=${"x".repeat(65)}`), null);
  });
});
