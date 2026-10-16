// Runs with: node --experimental-strip-types --test lib/live-spelling.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  dismissOps,
  goToResult,
  sameMistake,
  LIVE_SPELL_MAX_PARAGRAPHS,
  mistakeContext,
  mistakesByParagraph,
  mistakesIn,
  readFound,
  writeFound,
  planLiveSpelling,
  readReviewed,
  reviewedKeys,
  textKey,
  writeReviewed,
} from "./live-spelling.ts";

describe("planLiveSpelling", () => {
  it("sends every paragraph with a letter on the first pass, old underlines cleared", () => {
    const plans = planLiveSpelling(["Kính gửi:  Sở Nội vụ", "", "  ", "1.", "Kính gửi: Sở Nội vụ"], new Set());
    assert.equal(plans.length, 1);
    assert.deepEqual(plans[0].paragraphs, [
      { text: "Kính gửi:  Sở Nội vụ", edited: true },
      // same normalised text: the second instance for the anchor
      { text: "Kính gửi: Sở Nội vụ", occurrence: 2, edited: true },
    ]);
    assert.equal(plans[0].keys.length, 1);
  });

  it("sends only new or changed paragraphs afterwards, as edited", () => {
    const reviewed = new Set([textKey("Đoạn một."), textKey("Đoạn hai.")]);
    const plans = planLiveSpelling(["Đoạn một.", "Đoạn hai đã sửa.", "Đoạn hai."], reviewed);
    assert.deepEqual(plans, [{ paragraphs: [{ text: "Đoạn hai đã sửa.", edited: true }], keys: [textKey("Đoạn hai đã sửa.")], paraKeys: [textKey("Đoạn hai đã sửa.")] }]);
    assert.deepEqual(planLiveSpelling(["Đoạn một."], reviewed), []);
  });

  it("chunks to the server's request size", () => {
    const texts = Array.from({ length: LIVE_SPELL_MAX_PARAGRAPHS + 5 }, (_, i) => `Đoạn số ${i}`);
    const plans = planLiveSpelling(texts, new Set());
    assert.deepEqual(plans.map((p) => p.paragraphs.length), [LIVE_SPELL_MAX_PARAGRAPHS, 5]);
  });
});

describe("reviewed store", () => {
  it("round-trips and tells a never-reviewed document apart", () => {
    const mem = new Map<string, string>();
    const store = { getItem: (k: string) => mem.get(k) ?? null, setItem: (k: string, v: string) => void mem.set(k, v) };
    assert.equal(readReviewed(store, "d1"), null);
    writeReviewed(store, "d1", ["a", "b"]);
    assert.deepEqual(readReviewed(store, "d1"), ["a", "b"]);
    writeReviewed(store, "d2", []);
    assert.deepEqual(readReviewed(store, "d2"), []);
  });
});

describe("reviewedKeys", () => {
  it("keeps the paragraphs the model missed unreviewed", () => {
    const [plan] = planLiveSpelling(["Đoạn một.", "Đoạn hai.", "Đoạn một.", "Đoạn ba."], new Set());
    assert.equal(plan.paraKeys.length, 4);
    // the second "Đoạn một." was missed: its key waits for both
    assert.deepEqual(reviewedKeys(plan, [2]), [textKey("Đoạn hai."), textKey("Đoạn ba.")]);
    assert.deepEqual(reviewedKeys(plan, []), plan.keys);
  });
});

describe("found mistakes", () => {
  it("keeps the marks per paragraph and lists them in document order", () => {
    const ops = [
      { op: "unmark", anchor: { text: "Đoạn  một." } },
      { op: "mark", anchor: { text: "Đoạn  một." }, text: "x", suggestion: " X " },
      { op: "mark", anchor: { text: "Đoạn một.", occurrence: 2 }, text: "y", textOccurrence: 2 },
      { op: "mark", anchor: { text: "Đoạn hai." }, text: "z" },
      { op: "mark", anchor: { text: "Đoạn hai." }, style: "underline" },
    ];
    const found = mistakesByParagraph(ops);
    assert.equal(found.get(textKey("Đoạn một."))?.length, 2);
    assert.deepEqual(found.get(textKey("Đoạn một."))?.[0], { paragraph: "Đoạn  một.", text: "x", suggestion: "X" });
    assert.deepEqual(found.get(textKey("Đoạn một."))?.[1], { paragraph: "Đoạn một.", occurrence: 2, text: "y", textOccurrence: 2 });
    const listed = mistakesIn(["Đoạn hai.", "Đoạn một.", "Đoạn ba.", "Đoạn một."], found);
    assert.deepEqual(listed.map((m) => m.text), ["z", "x", "y"]);
  });
  it("shows a few words around the mistake", () => {
    const paragraph = "c) Các cơ quan quản lý nhà nước có thẩm quyền qua rà soát, thanh kiểm tra phát hiện khách hàng không đủ điều kiện.";
    const ctx = mistakeContext({ paragraph, text: "qua", textOccurrence: 2 }, 12);
    assert.ok(ctx.includes("thẩm quyền qua rà soát"), ctx);
    assert.ok(ctx.startsWith("…") && ctx.endsWith("…"));
  });
  it("round-trips the mistakes", () => {
    const mem = new Map<string, string>();
    const store = { getItem: (k: string) => mem.get(k) ?? null, setItem: (k: string, v: string) => void mem.set(k, v) };
    writeFound(store, "d1", new Map([["a", [{ paragraph: "P", text: "x" }]], ["b", []]]));
    assert.deepEqual([...readFound(store, "d1")], [["a", [{ paragraph: "P", text: "x" }]]]);
    assert.equal(readFound(store, "d2").size, 0);
  });
});

describe("goToResult", () => {
  it("tells a selected mistake from a missing one, an old plugin and no answer", () => {
    assert.equal(goToResult({ kind: "result", applied: 1, failed: [] }), "ok");
    assert.equal(goToResult({ kind: "result", applied: 0, failed: [{ error: "text occurrence not found" }] }), "missing");
    assert.equal(goToResult({ kind: "result", applied: 0, failed: [{ error: "unknown op" }] }), "outdated");
    assert.equal(goToResult({ kind: "result", applied: 0, failed: [{ error: "select unsupported" }] }), "outdated");
    assert.equal(goToResult({ kind: "timeout" }), "noAnswer");
  });
});

describe("dismissOps", () => {
  it("clears the paragraph and puts the other underlines of it back", () => {
    const para = "Các cơ quan có thẩm quyền qua rà soát, quyết đinh.";
    const qua = { paragraph: para, text: "qua", textOccurrence: 2, suggestion: "quan" };
    const dinh = { paragraph: para, text: "đinh", suggestion: "định" };
    const elsewhere = { paragraph: para, occurrence: 2, text: "rà" };
    assert.deepEqual(dismissOps(qua, [qua, dinh, elsewhere]), [
      { op: "unmark", anchor: { text: para } },
      { op: "mark", anchor: { text: para }, text: "đinh", style: "underline", suggestion: "định" },
    ]);
    assert.ok(sameMistake(qua, { ...qua }));
    assert.ok(!sameMistake(qua, { ...qua, textOccurrence: 1 }));
  });
});
