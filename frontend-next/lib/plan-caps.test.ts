import { test } from "node:test";
import assert from "node:assert/strict";
import { capLines, planCapsFromFeatures } from "./plan-caps.ts";

const t = ((key: string, vars?: Record<string, string | number>) => (vars ? `${key}:${JSON.stringify(vars)}` : key)) as never;

test("planCapsFromFeatures keeps known positive numbers only", () => {
  assert.deepEqual(planCapsFromFeatures({ "session.max_turns": 15, "agent.turn_token_cap": "8000", other: 3, "session.max_tokens": 0, "context.history_tokens": -1 }), {
    "session.max_turns": 15,
    "agent.turn_token_cap": 8000,
  });
  assert.deepEqual(planCapsFromFeatures(null), {});
});

test("capLines leads a Tổ chức plan with how its quota is shared", () => {
  assert.deepEqual(capLines({ "session.max_turns": 15 }, "seat", "tenant", t), ["pricing.capSeat", 'pricing.capTurns:{"n":"15"}']);
  assert.deepEqual(capLines(undefined, "pool", "tenant", t), ["pricing.capShared"]);
  assert.deepEqual(capLines({}, "pool", "user", t), []);
});
