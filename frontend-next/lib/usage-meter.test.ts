// The package pill's water level and colours.
// Runs with: node --experimental-strip-types --test lib/usage-meter.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { meterColors, usageMeter } from "./usage-meter.ts";

const lim = (metric: string, period: string, limit: number, used: number) => ({
  metric,
  period,
  limit,
  used,
  remaining: Math.max(0, limit - used),
  exhausted: limit > 0 && used >= limit,
});
const ent = (limits: ReturnType<typeof lim>[], extra: Partial<{ expired: boolean; mode: string; has_seat: boolean }> = {}) => ({
  expired: false,
  mode: "pool",
  has_seat: true,
  limits,
  ...extra,
});

describe("usageMeter", () => {
  it("levels on the tightest limit and counts the token limit", () => {
    const m = usageMeter(ent([lim("questions", "day", 20, 15), lim("tokens", "month", 1_000_000, 100_000)]));
    assert.equal(m.level, 75);
    assert.equal(m.tokens?.remaining, 900_000);
    assert.equal(m.blocked, false);
  });

  it("picks the tightest of several token limits", () => {
    const m = usageMeter(ent([lim("tokens", "month", 1_000_000, 100_000), lim("tokens", "day", 50_000, 40_000)]));
    assert.equal(m.tokens?.period, "day");
    assert.equal(m.level, 80);
  });

  it("is full when blocked", () => {
    assert.equal(usageMeter(ent([lim("questions", "day", 20, 20)])).level, 100);
    assert.equal(usageMeter(ent([], { expired: true })).blocked, true);
    assert.equal(usageMeter(ent([], { mode: "seat", has_seat: false })).level, 100);
  });

  it("is empty and has no token count without caps", () => {
    const m = usageMeter(ent([lim("tokens", "month", 0, 5_000)]));
    assert.equal(m.level, 0);
    assert.equal(m.tokens, null);
  });
});

describe("meterColors", () => {
  it("darkens as the level rises and switches to light text", () => {
    const light = (s: string) => Number(/(\d+)%\)$/.exec(s)?.[1]);
    assert.ok(light(meterColors(10, false).fill) > light(meterColors(90, false).fill));
    assert.notEqual(meterColors(10, false).ink, "#fff");
    assert.equal(meterColors(95, false).ink, "#fff");
    assert.match(meterColors(30, true).fill, /^hsl\(350/);
  });
});
