// Session-activity markers: a session left mid-generation stays "detached"
// and keeps being polled until the backend finishes the turn. Runs with:
// node --experimental-strip-types --test lib/session-activity-state.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  createSessionActivityState,
  type SessionActivityRecord,
  type ActivityMessage,
} from "./session-activity-state.ts";

const assistant = (id: string, is_completed = true): ActivityMessage => ({
  id,
  role: "assistant",
  is_completed,
});

function setup(fetchMessages: (sessionId: string) => Promise<ActivityMessage[]>) {
  const entries = new Map<string, SessionActivityRecord>();
  const state = createSessionActivityState(entries, fetchMessages);
  return { entries, state };
}

describe("createSessionActivityState", () => {
  it("update(running) marks a session and update(false) clears it", () => {
    const { entries, state } = setup(async () => []);
    state.update("s1", true, "m1");
    assert.deepEqual(entries.get("s1"), { messageId: "m1", detached: false, failures: 0 });
    state.update("s1", false);
    assert.equal(entries.has("s1"), false);
  });

  it("ignores empty and 'new' session ids", () => {
    const { entries, state } = setup(async () => []);
    state.update("", true, "m1");
    state.update("new", true, "m1");
    assert.equal(entries.size, 0);
  });

  it("refresh polls only detached entries and clears a completed turn", async () => {
    const calls: string[] = [];
    const { entries, state } = setup(async (sid) => {
      calls.push(sid);
      return [assistant("m1", true)];
    });
    state.update("s1", true, "m1"); // still attached — must not be polled
    state.update("s2", true, "m1");
    state.detach("s2");
    await state.refresh();
    assert.deepEqual(calls, ["s2"]);
    assert.equal(entries.has("s1"), true);
    assert.equal(entries.has("s2"), false);
  });

  it("keeps the marker while the tracked message is still incomplete", async () => {
    const { entries, state } = setup(async () => [assistant("m1", false)]);
    state.update("s1", true, "m1");
    state.detach("s1");
    await state.refresh();
    assert.deepEqual(entries.get("s1"), { messageId: "m1", detached: true, failures: 0 });
  });

  it("clears after 3 empty reads (turn abandoned before persisting)", async () => {
    const { entries, state } = setup(async () => []);
    // No messageId: the marker was set before the assistant id arrived, so an
    // empty list is not proof of completion yet.
    state.update("s1", true);
    state.detach("s1");
    await state.refresh();
    await state.refresh();
    assert.equal(entries.has("s1"), true);
    await state.refresh();
    assert.equal(entries.has("s1"), false);
  });

  it("clears on 404 and survives transient errors (3 strikes)", async () => {
    let failures = 0;
    const { entries, state } = setup(async () => {
      failures++;
      const err = new Error("boom") as Error & { status?: number };
      err.status = 500;
      throw err;
    });
    state.update("s1", true, "m1");
    state.detach("s1");
    await state.refresh();
    await state.refresh();
    assert.equal(entries.has("s1"), true);
    await state.refresh();
    assert.equal(entries.has("s1"), false);
    assert.equal(failures, 3);

    const gone = setup(async () => {
      const err = new Error("gone") as Error & { status?: number };
      err.status = 404;
      throw err;
    });
    gone.state.update("s9", true, "m1");
    gone.state.detach("s9");
    await gone.state.refresh();
    assert.equal(gone.entries.has("s9"), false);
  });

  it("a stale poll must not clear a reattached stream (entry identity guard)", async () => {
    let resolveFetch: ((msgs: ActivityMessage[]) => void) | null = null;
    const { entries, state } = setup(
      () => new Promise<ActivityMessage[]>((res) => (resolveFetch = res)),
    );
    state.update("s1", true, "m1");
    state.detach("s1");
    const poll = state.refresh(); // fetch still in flight
    // User returns to the session and the turn resumes locally — the entry
    // is replaced (detached:false), so the late poll must drop out instead
    // of deleting the live marker when it sees m1 completed.
    state.update("s1", true, "m1");
    resolveFetch!([assistant("m1", true)]);
    await poll;
    assert.deepEqual(entries.get("s1"), { messageId: "m1", detached: false, failures: 0 });
  });
});
