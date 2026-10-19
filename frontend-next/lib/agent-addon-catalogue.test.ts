// Runs with: node --experimental-strip-types --test lib/agent-addon-catalogue.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  addonSaleDraft,
  addonSaleInput,
  addonsForPlan,
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
