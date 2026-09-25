// Phase2a blocker regression (RED first): a deferred upload started on
// session A must never commit into session B after an A→B route/generation
// switch — no A optimistic bubble, no A stream, and B's abort controller
// stays untouched. Pure helper test (no React) over the same
// generation+route-id gate chat-client's send path uses after every await.
// Runs with: node --experimental-strip-types --test lib/deferred-send-guard.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { createStreamGeneration } from "./stream-generation.ts";
import { shouldCommitDeferredSendTurn } from "./deferred-send-guard.ts";

describe("deferred upload with A→B switch must not commit A into B", () => {
  it("drops the stale A bubble/stream and leaves the B controller untouched", async () => {
    const generations = createStreamGeneration();
    // Session A send starts: captures its generation + origin route id.
    const genA = generations.next();
    const originA = "sess-A";
    let liveId = "sess-A";

    // Deferred upload pending on A.
    let resolveUpload!: (ids: string[]) => void;
    const upload = new Promise<string[]>((resolve) => {
      resolveUpload = resolve;
    });

    // While the upload is pending, the user navigates A→B: the route effect
    // bumps the generation and B installs its own abort controller.
    const controllerB = new AbortController();
    let liveAbort: AbortController | null = controllerB;
    liveId = "sess-B";
    generations.next(); // B generation (route-change effect)

    // The stale A upload now resolves.
    resolveUpload(["att-A-1"]);
    const ids = await upload;

    // Fixed behavior: the gate reports stale, so the commit phase must drop.
    assert.equal(shouldCommitDeferredSendTurn(generations, genA, liveId, originA), false);

    // Simulate the commit phase chat-client runs after the upload awaits:
    // optimistic bubble append, abort-controller install, streamChat launch.
    // All three must be skipped for stale A (pre-fix code ran them
    // unconditionally — that is what appended A's bubble to B and overrode
    // B's controller).
    let bubbles: string[] = [];
    let streamCalls = 0;
    const commit = () => {
      if (!shouldCommitDeferredSendTurn(generations, genA, liveId, originA)) return;
      bubbles = [...bubbles, `bubble:${ids[0]}`];
      liveAbort = new AbortController();
      streamCalls += 1;
    };
    commit();

    assert.deepEqual(bubbles, []);
    assert.equal(streamCalls, 0);
    assert.equal(liveAbort, controllerB, "B abort controller must be unaffected");
    assert.equal(controllerB.signal.aborted, false);
  });

  it("commits when the turn is still live (no route/generation switch)", async () => {
    const generations = createStreamGeneration();
    const genA = generations.next();
    const originA = "sess-A";
    const liveId = "sess-A";
    assert.equal(shouldCommitDeferredSendTurn(generations, genA, liveId, originA), true);
  });

  it("a newer send on the same route also invalidates the deferred turn", () => {
    const generations = createStreamGeneration();
    const genA = generations.next();
    generations.next(); // resend bumps the generation; route id unchanged
    assert.equal(shouldCommitDeferredSendTurn(generations, genA, "sess-A", "sess-A"), false);
  });
});
