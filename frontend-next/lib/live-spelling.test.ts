// Runs with: node --experimental-strip-types --test lib/live-spelling.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  LIVE_SPELL_MAX_PARAGRAPHS,
  planLiveSpelling,
  readReviewed,
  textKey,
  writeReviewed,
} from "./live-spelling.ts";

describe("planLiveSpelling", () => {
  it("sends every paragraph with a letter on the first pass, unedited", () => {
    const plans = planLiveSpelling(["Kính gửi:  Sở Nội vụ", "", "  ", "1.", "Kính gửi: Sở Nội vụ"], new Set(), true);
    assert.equal(plans.length, 1);
    assert.deepEqual(plans[0].paragraphs, [
      { text: "Kính gửi:  Sở Nội vụ" },
      // same normalised text: the second instance for the anchor
      { text: "Kính gửi: Sở Nội vụ", occurrence: 2 },
    ]);
    assert.equal(plans[0].keys.length, 1);
  });

  it("sends only new or changed paragraphs afterwards, as edited", () => {
    const reviewed = new Set([textKey("Đoạn một."), textKey("Đoạn hai.")]);
    const plans = planLiveSpelling(["Đoạn một.", "Đoạn hai đã sửa.", "Đoạn hai."], reviewed, false);
    assert.deepEqual(plans, [{ paragraphs: [{ text: "Đoạn hai đã sửa.", edited: true }], keys: [textKey("Đoạn hai đã sửa.")] }]);
    assert.deepEqual(planLiveSpelling(["Đoạn một."], reviewed, false), []);
  });

  it("chunks to the server's request size", () => {
    const texts = Array.from({ length: LIVE_SPELL_MAX_PARAGRAPHS + 5 }, (_, i) => `Đoạn số ${i}`);
    const plans = planLiveSpelling(texts, new Set(), true);
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
