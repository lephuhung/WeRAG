// B-phase2b(1) RED: switching attempts (or knowledge ID) while an old
// spans request is in flight must never let the late old payload overwrite
// the newest selection, and the newest selection must be fetched
// immediately (pending retry) instead of waiting for the next poll tick.
// Runs with: node --experimental-strip-types --test lib/timeline-request-coordinator.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { TimelineRequestCoordinator } from "./timeline-request-coordinator.ts";

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("TimelineRequestCoordinator race", () => {
  it("queues a newer selection while in flight and rejects the stale payload", () => {
    const coord = new TimelineRequestCoordinator({ knowledgeId: "doc-1", attempt: undefined });
    const first = coord.requestFetch();
    assert.ok(first, "first fetch executes immediately");

    // User switches to attempt 2 while the first request is in flight.
    coord.updateSelection({ knowledgeId: "doc-1", attempt: 2 });
    const queued = coord.requestFetch();
    assert.equal(queued, null, "second fetch is queued, not executed concurrently");

    const settled = coord.settle(first);
    assert.equal(settled.accept, false, "late old payload must not be accepted");
    assert.deepEqual(
      settled.retry,
      { knowledgeId: "doc-1", attempt: 2 },
      "newest selection must be retried immediately",
    );

    const second = coord.requestFetch();
    assert.ok(second, "retry executes");
    const settled2 = coord.settle(second);
    assert.equal(settled2.accept, true, "fresh payload for the newest selection is accepted");
    assert.equal(settled2.retry, null, "no further retry once caught up");
  });

  it("drives newest-selection-wins with deferred fetch promises", async () => {
    const coord = new TimelineRequestCoordinator({ knowledgeId: "doc-1", attempt: undefined });
    const applied: Array<{ attempt: number; marker: string }> = [];
    const seenFetches: Array<number | undefined> = [];

    // Driver mirroring ProcessingTimeline.fetchSpans: only the accepted
    // ticket may mutate data/summary/status/loading.
    const runFetch = async (gate: ReturnType<typeof deferred<string>>) => {
      const ticket = coord.requestFetch();
      if (!ticket) return "queued";
      const marker = await gate.promise;
      const { accept, retry } = coord.settle(ticket);
      if (accept) applied.push({ attempt: ticket.selection.attempt ?? -1, marker });
      if (retry) {
        coord.updateSelection(retry);
        const next = coord.requestFetch();
        assert.ok(next, "pending retry must execute immediately, not wait for a poll tick");
        const marker2 = await gate.promise;
        const done = coord.settle(next);
        if (done.accept) applied.push({ attempt: next.selection.attempt ?? -1, marker: marker2 });
      }
      return accept ? "applied" : "stale";
    };

    const gate = deferred<string>();
    const first = runFetch(gate);
    // Let the first fetch start, then switch attempts before it resolves.
    await Promise.resolve();
    seenFetches.push(coord.getCurrent().attempt);
    coord.updateSelection({ knowledgeId: "doc-1", attempt: 2 });
    const queued = coord.requestFetch();
    assert.equal(queued, null);
    assert.equal(coord.hasPending(), true);

    gate.resolve("old-payload");
    // Second gate resolution serves the retry.
    gate.promise.then(() => gate.resolve("new-payload"));
    const outcome = await first;
    assert.equal(outcome, "stale");
    assert.deepEqual(
      applied.map((a) => a.attempt),
      [2],
      "only the newest selection's payload is ever applied",
    );
    assert.equal(seenFetches.length, 1);
  });

  it("a late duplicate settle after success is rejected (no deadlock, no overwrite)", () => {
    const coord = new TimelineRequestCoordinator({ knowledgeId: "doc-1", attempt: 1 });
    const t1 = coord.requestFetch();
    assert.ok(t1);
    const done = coord.settle(t1);
    assert.equal(done.accept, true);
    const dup = coord.settle(t1);
    assert.equal(dup.accept, false, "double settle must not re-apply");
    assert.equal(dup.retry, null);
    assert.equal(coord.isInFlight(), false, "coordinator must not stay stuck in-flight");
  });

  it("knowledge ID switch discards the old document payload and retries the new one", () => {
    const coord = new TimelineRequestCoordinator({ knowledgeId: "doc-1", attempt: undefined });
    const t1 = coord.requestFetch();
    assert.ok(t1);
    coord.updateSelection({ knowledgeId: "doc-2", attempt: undefined });
    assert.equal(coord.requestFetch(), null);
    const settled = coord.settle(t1);
    assert.equal(settled.accept, false);
    assert.deepEqual(settled.retry, { knowledgeId: "doc-2", attempt: undefined });
  });

  it("reset() on document change drops pending work for the previous document", () => {
    const coord = new TimelineRequestCoordinator({ knowledgeId: "doc-1", attempt: undefined });
    const t1 = coord.requestFetch();
    assert.ok(t1);
    coord.updateSelection({ knowledgeId: "doc-1", attempt: 3 });
    assert.equal(coord.requestFetch(), null);
    coord.reset({ knowledgeId: "doc-9", attempt: undefined });
    const settled = coord.settle(t1);
    assert.equal(settled.accept, false, "pre-reset ticket must not be accepted");
    assert.equal(settled.retry, null, "pre-reset pending must not leak into the new document");
    assert.equal(coord.isInFlight(), false);
    const fresh = coord.requestFetch();
    assert.ok(fresh);
    assert.deepEqual(fresh.selection, { knowledgeId: "doc-9", attempt: undefined });
  });
});

describe("TimelineRequestCoordinator auto-select", () => {
  it("auto-selects only when the captured attempt was undefined and no newer choice exists", () => {
    const coord = new TimelineRequestCoordinator({ knowledgeId: "doc-1", attempt: undefined });
    const t1 = coord.requestFetch();
    assert.ok(t1);
    assert.equal(
      coord.shouldAutoSelectAttempt(t1.selection.attempt),
      true,
      "initial load with no selection may adopt the payload attempt",
    );

    // User picks attempt 2 before the payload lands.
    coord.updateSelection({ knowledgeId: "doc-1", attempt: 2 });
    assert.equal(
      coord.shouldAutoSelectAttempt(t1.selection.attempt),
      false,
      "a newer user choice must win over payload auto-selection",
    );
  });

  it("never auto-selects when the fetch explicitly targeted an attempt", () => {
    const coord = new TimelineRequestCoordinator({ knowledgeId: "doc-1", attempt: 2 });
    const t = coord.requestFetch();
    assert.ok(t);
    assert.equal(coord.shouldAutoSelectAttempt(t.selection.attempt), false);
  });
});
