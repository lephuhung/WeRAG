// Per-session caps: reading the complete event and editing plan features.
// Runs with: node --experimental-strip-types --test lib/session-limits.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  planFeatureValue,
  sessionUsageFromComplete,
  tokenPercent,
  turnsUsed,
  withPlanFeature,
} from "./session-limits.ts";

describe("sessionUsageFromComplete", () => {
  it("reads a flagged session", () => {
    const u = sessionUsageFromComplete({
      total_steps: 2,
      session_usage: { session_id: "s1", turns: 11, max_turns: 15, tokens: 0, reached: false, warning: true },
    });
    assert.deepEqual(u, {
      sessionId: "s1", turns: 11, maxTurns: 15, tokens: 0, maxTokens: 0, reached: false, soft: false,
    });
    assert.equal(turnsUsed(u!), 12);
  });
  it("ignores completions without a warning", () => {
    assert.equal(sessionUsageFromComplete({ total_steps: 1 }), null);
    assert.equal(sessionUsageFromComplete({ session_usage: { warning: false, turns: 1 } }), null);
    assert.equal(sessionUsageFromComplete(undefined), null);
  });
  it("computes the token share", () => {
    const u = sessionUsageFromComplete({ session_usage: { warning: true, tokens: 850, max_tokens: 1000 } })!;
    assert.equal(tokenPercent(u), 85);
    assert.equal(tokenPercent({ ...u, maxTokens: 0 }), 0);
  });
});

describe("plan features", () => {
  it("sets and clears a key, keeping unknown keys", () => {
    const f = withPlanFeature({ "ui.theme": "dark" }, "session.max_turns", 15);
    assert.deepEqual(f, { "ui.theme": "dark", "session.max_turns": 15 });
    assert.equal(planFeatureValue(f, "session.max_turns"), 15);
    assert.deepEqual(withPlanFeature(f, "session.max_turns", 0), { "ui.theme": "dark" });
    assert.equal(planFeatureValue({}, "agent.turn_token_cap"), 0);
    assert.equal(planFeatureValue({ "agent.turn_token_cap": "x" }, "agent.turn_token_cap"), 0);
  });
});
