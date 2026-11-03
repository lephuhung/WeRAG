// Runs with: node --experimental-strip-types --test lib/billing-order.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { orderAmountDue, orderPlanLineVND, pendingPlanOrderCode } from "./billing-order.ts";
import type { UsageOrderAddonLine } from "./api/usage.ts";

const addon = (amount: number): UsageOrderAddonLine => ({ agent_id: "a", name: "A", unit_price_vnd: amount, units: 1, months: 1, amount_vnd: amount });

describe("orderPlanLineVND", () => {
  it("takes the prorated seats and the add-ons off the subtotal", () => {
    // 3 seats × 49.000 + 2 added seats prorated + one add-on
    assert.equal(orderPlanLineVND({ subtotal_vnd: 147_000 + 52_267 + 99_000, proration_vnd: 52_267, addons: [addon(99_000)] }), 147_000);
  });
  it("is the subtotal on an order without breakdown (older orders)", () => {
    assert.equal(orderPlanLineVND({ subtotal_vnd: 300_000 }), 300_000);
  });
  it("never goes below zero", () => {
    assert.equal(orderPlanLineVND({ subtotal_vnd: 10, addons: [addon(20)] }), 0);
  });
});

describe("orderAmountDue", () => {
  it("asks for the rest after a short transfer", () => {
    assert.equal(orderAmountDue({ total_vnd: 148_000, received_vnd: 100_000 }), 48_000);
    assert.equal(orderAmountDue({ total_vnd: 148_000 }), 148_000);
    assert.equal(orderAmountDue({ total_vnd: 148_000, received_vnd: 200_000 }), 0);
  });
});

describe("pendingPlanOrderCode", () => {
  it("reads the pending order's code from the refusal", () => {
    assert.equal(pendingPlanOrderCode({ reason: "plan_order_pending", order_code: "WRABCD2345" }), "WRABCD2345");
    assert.equal(pendingPlanOrderCode({ reason: "addon_pending" }), null);
    assert.equal(pendingPlanOrderCode(undefined), null);
  });
});
