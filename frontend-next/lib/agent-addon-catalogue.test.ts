// Runs with: node --experimental-strip-types --test lib/agent-addon-catalogue.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  addonSaleDraft,
  addonSaleInput,
  addonsForPlan,
  addonLineDetail,
  keepOffered,
  toggleAddonId,
  shouldSaveAddonSale,
  togglePlanId,
} from "./agent-addon-catalogue.ts";
import type { UsageAgentAddon } from "./api/usage.ts";

const row = (over: Partial<UsageAgentAddon>): UsageAgentAddon => ({
  agent_id: "a",
  price_vnd: 49000,
  plan_ids: [],
  description: "",
  is_active: true,
  sort_order: 0,
  agent_available: true,
  ...over,
});

describe("add-on sale draft", () => {
  it("defaults to active, every plan, free", () => {
    assert.deepEqual(addonSaleDraft(null), { price_vnd: 0, plan_ids: [], description: "", is_active: true, sort_order: 0 });
  });
  it("builds the API input: rounded non-negative price, sorted unique plans, trimmed text", () => {
    assert.deepEqual(
      addonSaleInput("thue", { price_vnd: 49000.4, plan_ids: ["plus", "go", "go"], description: " Thuế ", is_active: false, sort_order: 2 }),
      { agent_id: "thue", price_vnd: 49000, plan_ids: ["go", "plus"], description: "Thuế", is_active: false, sort_order: 2 },
    );
    assert.equal(addonSaleInput("x", { ...addonSaleDraft(null), price_vnd: -5 }).price_vnd, 0);
  });
  it("toggles plans", () => {
    assert.deepEqual(togglePlanId(["plus"], "go"), ["go", "plus"]);
    assert.deepEqual(togglePlanId(["go", "plus"], "go"), ["plus"]);
  });
  it("saves only for a specialist agent with a row or edited fields", () => {
    assert.equal(shouldSaveAddonSale(false, false, "addon"), false);
    assert.equal(shouldSaveAddonSale(false, true, "addon"), true);
    assert.equal(shouldSaveAddonSale(true, false, "addon"), true);
    assert.equal(shouldSaveAddonSale(true, true, "basic"), false);
  });
});

describe("addonsForPlan", () => {
  it("keeps active, available rows sold with the plan, by sort order", () => {
    const rows = [
      row({ agent_id: "all", sort_order: 2, agent_name: "B" }),
      row({ agent_id: "go-only", plan_ids: ["go"], sort_order: 1 }),
      row({ agent_id: "plus-only", plan_ids: ["plus"] }),
      row({ agent_id: "off", is_active: false }),
      row({ agent_id: "gone", agent_available: false }),
    ];
    assert.deepEqual(addonsForPlan(rows, { id: "go" }).map((r) => r.agent_id), ["go-only", "all"]);
    assert.deepEqual(addonsForPlan(rows, { id: "plus" }).map((r) => r.agent_id), ["plus-only", "all"]);
  });
});

describe("buyer add-on choice", () => {
  it("toggles and keeps only offered add-ons", () => {
    assert.deepEqual(toggleAddonId(["a"], "b"), ["a", "b"]);
    assert.deepEqual(toggleAddonId(["a", "b"], "a"), ["b"]);
    assert.deepEqual(keepOffered(["a", "b"], [{ agent_id: "b" }]), ["b"]);
  });
  it("explains how a line was priced", () => {
    const fmt = (v: number) => `${v}đ`;
    const labels = {
      months: (n: number) => `${n} tháng`,
      half: (d: number) => `½ tháng (${d} ngày)`,
      full: (d: number) => `1 tháng (${d} ngày)`,
    };
    assert.equal(
      addonLineDetail({ agent_id: "a", name: "A", unit_price_vnd: 99000, units: 4, months: 2, leftover_days: 10, leftover_factor: "half", amount_vnd: 0 }, fmt, labels),
      "99000đ × 4 × (2 tháng + ½ tháng (10 ngày))",
    );
    assert.equal(
      addonLineDetail({ agent_id: "a", name: "A", unit_price_vnd: 99000, units: 1, months: 3, amount_vnd: 0 }, fmt, labels),
      "99000đ × (3 tháng)",
    );
  });
});

describe("renewal line detail", () => {
  it("adds the uncovered gap and the added seats", () => {
    const labels = {
      months: (n: number) => `${n} tháng`,
      half: (d: number) => `½ tháng (${d} ngày)`,
      full: (d: number) => `1 tháng (${d} ngày)`,
      gap: (span: string) => `phần còn thiếu ${span}`,
      extraSeats: (n: number, amount: string) => `${n} ghế thêm ${amount}`,
    };
    assert.equal(
      addonLineDetail(
        { agent_id: "a", name: "A", unit_price_vnd: 1, units: 3, months: 1, gap_months: 11, gap_vnd: 33, extra_units: 2, extra_units_vnd: 4, amount_vnd: 0 },
        (v) => `${v}đ`,
        labels,
      ),
      "1đ × 3 × (1 tháng) + phần còn thiếu 11 tháng + 2 ghế thêm 4đ",
    );
  });
});
