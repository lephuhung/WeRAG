// Runs with: node --experimental-strip-types --test lib/billing-order.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  addonIdsForPlan,
  billingErrorRetryable,
  billingServerMessage,
  defaultPlanSeats,
  orderAmountDue,
  orderPaymentHeld,
  orderPlanLineVND,
  pendingPlanOrderCode,
  planNameAfterWord,
  preferredPlanId,
} from "./billing-order.ts";
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
  it("is null when the refusal has no override (a seats order)", () => {
    assert.equal(pendingPlanOrderCode({ reason: "plan_order_pending", order_code: "WRX", no_override: "true" }), null);
    assert.equal(pendingPlanOrderCode({ reason: "plan_order_pending", order_code: "WRX", no_override: true }), null);
  });
});

describe("planNameAfterWord", () => {
  it("drops the word the sentence already has", () => {
    assert.equal(planNameAfterWord("Gói cơ bản", "gói"), "cơ bản");
    assert.equal(planNameAfterWord("GÓI Go", "gói"), "Go");
    assert.equal(planNameAfterWord("Package Pro", "package"), "Pro");
  });
  it("keeps other names, and a name that is only the word", () => {
    assert.equal(planNameAfterWord("Go", "gói"), "Go");
    assert.equal(planNameAfterWord("Góiplus", "gói"), "Góiplus");
    assert.equal(planNameAfterWord("Gói", "gói"), "Gói");
    assert.equal(planNameAfterWord("Gói cơ bản", "package"), "Gói cơ bản");
    assert.equal(planNameAfterWord(undefined, "gói"), "");
  });
});

describe("billingServerMessage", () => {
  it("shows a 4xx message written in the reader's language", () => {
    assert.equal(billingServerMessage("Gói này không bán trực tuyến.", 400, "vi"), "Gói này không bán trực tuyến.");
    assert.equal(billingServerMessage("invalid quote body", 400, "en"), "invalid quote body");
  });
  it("hides server errors and messages in the other language", () => {
    assert.equal(billingServerMessage("usage packages operation failed", 500, "en"), null);
    assert.equal(billingServerMessage("invalid quote body", 400, "vi"), null);
    assert.equal(billingServerMessage("Lỗi hệ thống", 400, "en"), null);
    assert.equal(billingServerMessage("", 400, "vi"), null);
    assert.equal(billingServerMessage("network", undefined, "en"), null);
  });
});

describe("billingErrorRetryable", () => {
  it("offers no retry on a conflict", () => {
    assert.equal(billingErrorRetryable(409), false);
    assert.equal(billingErrorRetryable(500), true);
    assert.equal(billingErrorRetryable(undefined), true);
  });
});

describe("preferredPlanId", () => {
  const ids = ["go", "basic", "pro"];
  it("opens on the plan in use when it is sold", () => {
    assert.equal(preferredPlanId(ids, "go", "basic", false), "basic");
    assert.equal(preferredPlanId(ids, "", "basic", false), "basic");
  });
  it("lets an explicit pick (banner intent, click) win", () => {
    assert.equal(preferredPlanId(ids, "pro", "basic", true), "pro");
  });
  it("falls back to the current choice, then the first plan", () => {
    assert.equal(preferredPlanId(ids, "pro", "gone", false), "pro");
    assert.equal(preferredPlanId(ids, "gone", "", false), "go");
    assert.equal(preferredPlanId(ids, "gone", "", true), "go");
    assert.equal(preferredPlanId([], "x", "y", false), "");
  });
});

describe("defaultPlanSeats", () => {
  it("starts a seat plan from the current seat subscription's seats", () => {
    assert.equal(defaultPlanSeats({ scope: "tenant", planMode: "seat", currentMode: "seat", currentSeats: 3 }), 3);
    assert.equal(defaultPlanSeats({ scope: "tenant", planMode: "seat", currentMode: "seat", currentSeats: 0 }), 1);
  });
  it("leaves the box alone otherwise", () => {
    assert.equal(defaultPlanSeats({ scope: "tenant", planMode: "seat", currentMode: "pool", currentSeats: 0 }), null);
    assert.equal(defaultPlanSeats({ scope: "tenant", planMode: "pool", currentMode: "seat", currentSeats: 3 }), null);
    assert.equal(defaultPlanSeats({ scope: "user", planMode: "seat", currentMode: "seat", currentSeats: 3 }), null);
    assert.equal(defaultPlanSeats({ scope: "tenant", planMode: "seat", currentMode: undefined, currentSeats: undefined }), null);
  });
});

describe("addonIdsForPlan", () => {
  it("ticks the held add-ons on a renewal of the plan in use", () => {
    assert.deepEqual(addonIdsForPlan([], ["a", "b"], ["a"], true), ["a"]);
    assert.deepEqual(addonIdsForPlan(["b"], ["a", "b"], ["a", "x"], true), ["b", "a"]);
  });
  it("leaves held add-ons to the keep choice on a plan change", () => {
    // the renewal had pre-ticked "a"; switching plans must not buy it again
    assert.deepEqual(addonIdsForPlan(["a", "b"], ["a", "b"], ["a"], false), ["b"]);
    assert.deepEqual(addonIdsForPlan(["a", "c"], ["a", "b"], [], false), ["a"]);
  });
});

describe("orderPaymentHeld", () => {
  it("is set by a refused transfer or money covering the total", () => {
    const at = "2026-11-03T10:00:00Z";
    assert.equal(orderPaymentHeld({ total_vnd: 100, received_vnd: 100, last_payment: { amount_vnd: 40, status: "confirm_failed", transacted_at: at } }), true);
    assert.equal(orderPaymentHeld({ total_vnd: 100, received_vnd: 100 }), true);
    assert.equal(orderPaymentHeld({ total_vnd: 100, received_vnd: 60, last_payment: { amount_vnd: 60, status: "underpaid", transacted_at: at } }), false);
    assert.equal(orderPaymentHeld({ total_vnd: 100 }), false);
  });
});
