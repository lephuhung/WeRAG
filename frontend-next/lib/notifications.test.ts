import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { badgeLabel, safeAppLink, toastable, wordFileTarget } from "./notifications.ts";

const n = (id: string, over: Partial<{ seen_at: string | null; read_at: string | null; payload: Record<string, unknown> | null }> = {}) => ({
  id,
  kind: "legal_request_added",
  link: "/platform/chat/s1",
  payload: null,
  seen_at: null,
  read_at: null,
  ...over,
});

describe("notifications", () => {
  it("toasts unseen unread ones once, oldest first", () => {
    const items = [n("c"), n("b", { seen_at: "x" }), n("a"), n("r", { read_at: "x" })]; // newest first
    assert.deepEqual(toastable(items, new Set()).map((x) => x.id), ["a", "c"]);
    assert.deepEqual(toastable(items, new Set(["a"])).map((x) => x.id), ["c"]);
  });

  it("keeps app links only", () => {
    assert.equal(safeAppLink("/platform/chat/s1"), "/platform/chat/s1");
    assert.equal(safeAppLink("/platform/legal-lookup?q=53%2F2022"), "/platform/legal-lookup?q=53%2F2022");
    assert.equal(safeAppLink("https://evil.example"), "");
    assert.equal(safeAppLink("//evil.example"), "");
    assert.equal(safeAppLink("/\\evil"), "");
    assert.equal(safeAppLink(""), "");
  });

  it("badges", () => {
    assert.equal(badgeLabel(0), "");
    assert.equal(badgeLabel(3), "3");
    assert.equal(badgeLabel(12), "9+");
  });

  it("opens a conversation in Word only from its own file", () => {
    const x = n("a", { payload: { session_id: "s1", word_file_id: "f1" } });
    assert.equal(wordFileTarget(x, "f1"), "s1");
    assert.equal(wordFileTarget(x, "f2"), "");
    assert.equal(wordFileTarget(x, null), "");
    assert.equal(wordFileTarget(n("b", { payload: { session_id: "s1" } }), "f1"), "");
  });
});
