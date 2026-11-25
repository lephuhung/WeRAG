// Runs with: node --experimental-strip-types --test lib/live-spelling.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  dismissOps,
  editedParagraphPairs,
  editVerdicts,
  resolveEditedMistakes,
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

describe("editedParagraphPairs", () => {
  it("pairs paragraphs edited in place, skipping unchanged and empty ones", () => {
    const pairs = editedParagraphPairs(["A", "quyết đinh", "", "C"], ["A", "quyết định", "", "C"]);
    assert.deepEqual(pairs, [
      { key: textKey("quyết định"), oldKey: textKey("quyết đinh"), oldText: "quyết đinh", newText: "quyết định" },
    ]);
  });
  it("pairs nothing on the first read or when paragraphs were added", () => {
    assert.deepEqual(editedParagraphPairs(null, ["A"]), []);
    assert.deepEqual(editedParagraphPairs(["A", "B"], ["A", "B2", "new"]), []);
  });
  it("ignores whitespace-only changes", () => {
    assert.deepEqual(editedParagraphPairs(["Kính gửi:  Sở"], ["Kính gửi: Sở"]), []);
  });
});

describe("resolveEditedMistakes", () => {
  const old = "Ủy ban ban hành quyết đinh số 5 về quyết đinh khác.";
  const m = (text: string, suggestion?: string, textOccurrence?: number) => ({
    paragraph: old, text, ...(suggestion ? { suggestion } : {}), ...(textOccurrence ? { textOccurrence } : {}),
  });

  it("accepted: the suggestion typed over the mistake", () => {
    const mk = m("đinh", "định");
    assert.deepEqual(resolveEditedMistakes(old, old.replace("đinh", "định"), [mk]), [
      { mistake: mk, verdict: "accepted", correction: "" },
    ]);
  });
  it("fixed: something else typed, kept as the correction", () => {
    const mk = m("đinh", "đỉnh");
    assert.deepEqual(resolveEditedMistakes(old, old.replace("đinh", "định"), [mk]), [
      { mistake: mk, verdict: "fixed", correction: "định" },
    ]);
  });
  it("nothing when the mistake is unchanged, deleted or extended by typed words", () => {
    const mk = m("đinh", "định");
    const elsewhere = old.replace("Ủy ban", "UBND");
    assert.deepEqual(resolveEditedMistakes(old, elsewhere, [mk]), []);
    assert.deepEqual(resolveEditedMistakes(old, old.replace("đinh ", ""), [mk]), []);
    assert.deepEqual(resolveEditedMistakes(old, old.replace("đinh", "đinh này"), [mk]), []);
  });
  it("nothing when the paragraph was rewritten", () => {
    assert.deepEqual(resolveEditedMistakes(old, "Một đoạn hoàn toàn khác.", [m("đinh", "định")]), []);
  });
  it("finds the second occurrence of the text", () => {
    const mk = m("đinh", "định", 2);
    const edited = "Ủy ban ban hành quyết đinh số 5 về quyết định khác.";
    assert.deepEqual(resolveEditedMistakes(old, edited, [mk]), [{ mistake: mk, verdict: "accepted", correction: "" }]);
    // the first instance changed: not this mistake
    assert.deepEqual(resolveEditedMistakes(old, old.replace("đinh", "định"), [mk]), []);
  });
  it("two mistakes, one changed: only that one", () => {
    const a = m("Ủy", "Uỷ");
    const b = m("đinh", "định");
    assert.deepEqual(resolveEditedMistakes(old, old.replace("Ủy", "Uỷ"), [a, b]), [
      { mistake: a, verdict: "accepted", correction: "" },
    ]);
    // both changed at once: neither (prefix/suffix no longer match)
    assert.deepEqual(resolveEditedMistakes(old, old.replace("Ủy", "Uỷ").replace("đinh", "định"), [a, b]), []);
  });
  it("Unicode: diacritics and astral characters before the span", () => {
    const p = "Tôi 😀 đi hoc về";
    const mk = { paragraph: p, text: "hoc", suggestion: "học" };
    assert.deepEqual(resolveEditedMistakes(p, "Tôi 😀 đi học về", [mk]), [{ mistake: mk, verdict: "accepted", correction: "" }]);
    // another word than the suggestion: fixed, kept as typed
    assert.deepEqual(resolveEditedMistakes(p, "Tôi 😀 đi hóc về", [mk]), [{ mistake: mk, verdict: "fixed", correction: "hóc" }]);
  });
  it("skips a correction over 255 code points or with a line break", () => {
    const mk = { paragraph: "a sai b", text: "sai", suggestion: "đúng" };
    assert.deepEqual(resolveEditedMistakes("a sai b", "a " + "ố".repeat(256) + " b", [mk]), []);
    assert.deepEqual(resolveEditedMistakes("a sai b", "a x\ny b", [mk]), []);
    assert.equal(resolveEditedMistakes("a sai b", "a " + "😀".repeat(255) + " b", [mk]).length, 1);
  });
});

describe("editVerdicts", () => {
  it("sends each verdict once, with the paragraph before the edit", () => {
    const before = "Ban hành quyết đinh số 5";
    const after = "Ban hành quyết định số 5";
    const found = new Map([[textKey(before), [{ paragraph: before, text: "đinh", suggestion: "định" }]]]);
    const sent = new Set<string>();
    const pairs = editedParagraphPairs([before], [after]);
    assert.deepEqual(editVerdicts(pairs, found, sent), [
      { paragraph: before, wrong: "đinh", suggestion: "định", verdict: "accepted" },
    ]);
    assert.deepEqual(editVerdicts(pairs, found, sent), []);
    // no underline in that paragraph: nothing
    assert.deepEqual(editVerdicts(editedParagraphPairs(["x sai"], ["x đúng"]), found, new Set()), []);
  });
});
