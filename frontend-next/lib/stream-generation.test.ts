// REVIEW BLOCKER B-phase2a(2) RED: route changes while session A streams must
// not let A's late callbacks/finalizer mutate new session B state. A
// generation counter guards every late write; the previous AbortController is
// cancelled on id change. Runs with:
// node --experimental-strip-types --test lib/stream-generation.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { createStreamGeneration } from "./stream-generation.ts";

describe("createStreamGeneration", () => {
  it("starts current and reports the initial generation as live", () => {
    const g = createStreamGeneration();
    assert.equal(g.isCurrent(g.current()), true);
  });

  it("next() invalidates the previous generation (old A completion is stale while B streams)", () => {
    const g = createStreamGeneration();
    const genA = g.next(); // session A send starts streaming
    const genB = g.next(); // route changed to session B, new send starts
    assert.equal(g.isCurrent(genB), true);
    assert.equal(g.isCurrent(genA), false);
  });

  it("a late finally from generation A must not clear generation B busy state", () => {
    const g = createStreamGeneration();
    const genA = g.next();
    let busyB = true;
    // Simulate A's async finalizer arriving while B streams:
    const settleA = () => {
      if (!g.isCurrent(genA)) return; // guarded: must skip
      busyB = false;
    };
    g.next(); // B takes over
    settleA();
    assert.equal(busyB, true);
  });

  it("monotonic generations never collide", () => {
    const g = createStreamGeneration();
    const seen = new Set([g.next(), g.next(), g.next()]);
    assert.equal(seen.size, 3);
  });
});
