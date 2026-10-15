// The chat's context indicator: parsing the endpoint, fill, tone, numbers.
// Runs with: node --experimental-strip-types --test lib/context-usage.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  contextFill,
  contextPercent,
  contextTone,
  formatCount,
  formatTokens,
  parseContextUsage,
} from "./context-usage.ts";

describe("parseContextUsage", () => {
  it("reads the endpoint's data", () => {
    const u = parseContextUsage({
      model_window: 250000,
      history_budget: 32768,
      history_tokens: 12345,
      summary_tokens: 900,
      compacted: true,
      compacted_turns: 6,
      seeded: false,
      mode: "agent",
      last_turn: { prompt_tokens: 40000, completion_tokens: 800, rounds: 3 },
      session: { turns: 5, max_turns: 15, tokens: 81234, max_tokens: 0, warning: false, reached: false, soft: false },
      turn_token_cap: 300000,
      plan_name: "Cơ bản",
      enforced: true,
    });
    assert.ok(u);
    assert.equal(u.history_tokens, 12345);
    assert.equal(u.compacted_turns, 6);
    assert.deepEqual(u.last_turn, { prompt_tokens: 40000, completion_tokens: 800, rounds: 3 });
    assert.equal(u.session.max_turns, 15);
    assert.equal(u.plan_name, "Cơ bản");
  });
  it("tolerates missing fields", () => {
    const u = parseContextUsage({ history_tokens: "x", last_turn: null });
    assert.ok(u);
    assert.equal(u.history_tokens, 0);
    assert.equal(u.last_turn, null);
    assert.equal(u.session.turns, 0);
    assert.equal(parseContextUsage(null), null);
    assert.equal(parseContextUsage("nope"), null);
  });
});

describe("fill and tone", () => {
  const base = parseContextUsage({ model_window: 250000, history_budget: 32768 })!;
  it("fills against the budget, else the window", () => {
    assert.equal(contextPercent({ ...base, history_tokens: 12345 }), 38);
    assert.equal(contextPercent({ ...base, history_budget: 0, history_tokens: 125000 }), 50);
    assert.equal(contextFill({ ...base, history_tokens: 99999 }), 1, "capped at full");
    assert.equal(contextFill({ ...base, history_budget: 0, model_window: 0 }), 0);
  });
  it("is neutral, amber, then red", () => {
    assert.equal(contextTone(0.59), "neutral");
    assert.equal(contextTone(0.6), "amber");
    assert.equal(contextTone(0.85), "amber");
    assert.equal(contextTone(0.86), "red");
  });
});

describe("numbers", () => {
  it("shortens token counts per locale", () => {
    assert.equal(formatTokens(950, "en"), "950");
    assert.equal(formatTokens(12345, "en"), "12.3k");
    assert.equal(formatTokens(12345, "vi"), "12,3k");
    assert.equal(formatTokens(32000, "vi"), "32k");
    assert.equal(formatTokens(250000, "vi"), "250k");
    assert.equal(formatTokens(1_234_567, "vi"), "1,2M");
  });
  it("groups plain counts", () => {
    assert.equal(formatCount(81234, "en"), "81,234");
    assert.equal(formatCount(81234, "vi"), "81.234");
  });
});
