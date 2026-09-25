// Identity-gate contract for late async callbacks (timeline generation
// guard, KB header reload guard): a response captured for owner A must
// never mutate B's state, on success or on failure. Deferred promises
// simulate the A-resolves-after-switch race.
// Runs with: node --experimental-strip-types --test lib/request-identity.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { shouldApplyResponse, type OwnedRequest } from "./request-identity.ts";

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("shouldApplyResponse", () => {
  it("applies only for the matching id and generation", () => {
    const cur: OwnedRequest = { id: "doc-B", gen: 2 };
    assert.equal(shouldApplyResponse({ id: "doc-B", gen: 2 }, cur), true);
    assert.equal(
      shouldApplyResponse({ id: "doc-A", gen: 1 }, cur),
      false,
      "previous document must not write",
    );
    assert.equal(
      shouldApplyResponse({ id: "doc-B", gen: 1 }, cur),
      false,
      "stale generation for the same id must not write",
    );
  });

  it("timeline race: late doc-A per-attempt callbacks cannot write doc-B tab state", async () => {
    // Simulates ProcessingTimeline.ensureAttemptStatuses: each per-attempt
    // fetch captures its owner; only the current owner may set state.
    let current: OwnedRequest = { id: "doc-A", gen: 1 };
    const appliedStatuses = new Map<number, string>();
    const cleared: string[] = [];

    const perAttemptFetch = (owner: OwnedRequest, attempt: number, gate: ReturnType<typeof deferred<string>>) =>
      gate.promise.then(
        (status) => {
          if (!shouldApplyResponse(owner, current)) return "discarded";
          appliedStatuses.set(attempt, status);
          return "applied";
        },
        () => {
          // Catch branch is guarded too: a stale failure must not clear
          // the new document's header/tab state.
          if (!shouldApplyResponse(owner, current)) return "discarded";
          cleared.push(owner.id);
          return "cleared";
        },
      );

    const gateA = deferred<string>();
    const gateAFail = deferred<string>();
    const pendingA = perAttemptFetch({ id: "doc-A", gen: 1 }, 1, gateA);
    const pendingAFail = perAttemptFetch({ id: "doc-A", gen: 1 }, 2, gateAFail);

    // User switches documents before A resolves.
    current = { id: "doc-B", gen: 2 };

    gateA.resolve("done");
    gateAFail.reject(new Error("offline"));
    assert.deepEqual([await pendingA, await pendingAFail], ["discarded", "discarded"]);
    assert.equal(appliedStatuses.size, 0, "A must not write B header/tab state");
    assert.deepEqual(cleared, [], "stale catch must not clear B state");

    // The new document's own fetch still applies.
    const gateB = deferred<string>();
    const pendingB = perAttemptFetch({ id: "doc-B", gen: 2 }, 1, gateB);
    gateB.resolve("running");
    assert.equal(await pendingB, "applied");
    assert.equal(appliedStatuses.get(1), "running");
  });

  it("kb-detail race: pending A header reload cannot setKb on B", async () => {
    // Simulates KbDetail.refreshAfterDocChange: the header fetch captures
    // the KB id; only the current KB may apply it.
    let currentKb = "kb-A";
    let kb: string | null = null;

    const headerFetch = (ownerKb: string, gate: ReturnType<typeof deferred<string>>) =>
      gate.promise.then((row) => {
        if (!shouldApplyResponse({ id: ownerKb, gen: 0 }, { id: currentKb, gen: 0 })) {
          return "discarded";
        }
        kb = row;
        return "applied";
      });

    const gateA = deferred<string>();
    const pendingA = headerFetch("kb-A", gateA);
    currentKb = "kb-B"; // user switches KBs while A is in flight
    gateA.resolve("kb-A-row");
    assert.equal(await pendingA, "discarded");
    assert.equal(kb, null, "A must not setKb on B");

    const gateB = deferred<string>();
    const pendingB = headerFetch("kb-B", gateB);
    gateB.resolve("kb-B-row");
    assert.equal(await pendingB, "applied");
    assert.equal(kb, "kb-B-row");
  });
});
