// Runs with: node --experimental-strip-types --test lib/billing-order.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  addonIdsForPlan,
  billingErrorRetryable,
  billingServerMessage,
  defaultPlanSeats,
  heldNeedingRenewal,
  heldOutlivesPackage,
  orderAmountDue,
  orderPaymentHeld,
  orderPlanLineVND,
  orderSummaryParts,
  orderWasPaidFor,
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

describe("heldNeedingRenewal", () => {
  const subEnd = "2026-12-03T17:00:00Z"; // end of 3/12 VN
  it("pre-ticks the held agents that stop at the package end or run with it", () => {
    const held = [
      { agent_id: "with", ends_at: null },
      { agent_id: "same", ends_at: subEnd },
      { agent_id: "early", ends_at: "2026-11-20T17:00:00Z" },
    ];
    assert.deepEqual(heldNeedingRenewal(held, subEnd), ["with", "same", "early"]);
  });
  it("leaves a right held beyond the package end unticked (addon_held)", () => {
    const held = [{ agent_id: "kept", ends_at: "2027-01-31T17:00:00Z" }, { agent_id: "with" }];
    assert.deepEqual(heldNeedingRenewal(held, subEnd), ["with"]);
    // the renewal ticks only those
    assert.deepEqual(addonIdsForPlan([], ["kept", "with"], ["kept", "with"], true, heldNeedingRenewal(held, subEnd)), ["with"]);
  });
  it("uses the renewal's new end once the quote gave it (LOW-5)", () => {
    const newEnd = "2027-01-03T17:00:00Z";
    const held = [
      { agent_id: "until-dec", ends_at: "2026-12-20T17:00:00Z" }, // after the current end, before the new one
      { agent_id: "until-new", ends_at: newEnd }, // already to the new end
      { agent_id: "beyond", ends_at: "2027-02-01T17:00:00Z" },
      { agent_id: "with", ends_at: null },
    ];
    assert.deepEqual(heldNeedingRenewal(held, subEnd), ["with"]);
    assert.deepEqual(heldNeedingRenewal(held, subEnd, newEnd), ["until-dec", "with"]);
  });
  it("drops a held agent the renewal does not need from the ticks", () => {
    // "beyond" was ticked under a longer renewal; a shorter one covers it no more
    assert.deepEqual(addonIdsForPlan(["beyond", "b"], ["beyond", "b", "x"], ["beyond", "x"], true, ["x"]), ["b", "x"]);
  });
  it("ticks every held agent when the package has no end", () => {
    assert.deepEqual(heldNeedingRenewal([{ agent_id: "a", ends_at: "2027-01-31T17:00:00Z" }], null), ["a"]);
  });
});

describe("heldOutlivesPackage", () => {
  it("is true only for a right ending after the package", () => {
    const end = "2026-12-03T17:00:00Z";
    assert.equal(heldOutlivesPackage({ agent_id: "a", ends_at: "2027-01-31T17:00:00Z" }, end), true);
    assert.equal(heldOutlivesPackage({ agent_id: "a", ends_at: end }, end), false);
    assert.equal(heldOutlivesPackage({ agent_id: "a", ends_at: null }, end), false);
    assert.equal(heldOutlivesPackage({ agent_id: "a", ends_at: "2027-01-31T17:00:00Z" }, null), false);
  });
});

describe("orderSummaryParts", () => {
  const l = {
    length: () => "1 tháng",
    seats: (n: number) => `${n} chỗ`,
    addSeats: (n: number) => `thêm ${n} chỗ`,
    addons: (n: number) => `${n} trợ lý`,
  };
  it("says a seat order's seats once", () => {
    assert.equal(orderSummaryParts({ kind: "seats", mode: "seat", seats: 2, plan_name: "Go", addons: [] }, { ...l, length: () => "thêm chỗ" }).join(" · "), "Go · thêm 2 chỗ");
  });
  it("gives a plan order its length, seats and add-ons", () => {
    assert.deepEqual(orderSummaryParts({ kind: "plan", mode: "seat", seats: 5, plan_name: "Go", addons: [addon(1)] }, l), ["Go", "1 tháng", "5 chỗ", "1 trợ lý"]);
    assert.deepEqual(orderSummaryParts({ kind: "plan", mode: "pool", seats: 0, plan_name: "Gói cơ bản", addons: [] }, l), ["Gói cơ bản", "1 tháng"]);
  });
  it("gives an add-on order its kind only", () => {
    assert.deepEqual(orderSummaryParts({ kind: "addon", mode: "pool", seats: 0, plan_name: "Go", addons: [addon(1)] }, { ...l, length: () => "trợ lý chuyên ngành" }), ["Go", "trợ lý chuyên ngành"]);
  });
});

describe("orderWasPaidFor", () => {
  it("is false for a 0-đồng order (confirmed, not paid)", () => {
    assert.equal(orderWasPaidFor({ total_vnd: 0 }), false);
    assert.equal(orderWasPaidFor({ total_vnd: 49_000 }), true);
  });
});

describe("planNameAfterWord on the receipt's plan line", () => {
  it("does not repeat the word", () => {
    assert.equal(`Gói ${planNameAfterWord("Gói cơ bản", "gói")}`, "Gói cơ bản");
  });
});
