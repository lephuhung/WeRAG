import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { headingLead, locateParagraph, pickDraftKnowledgeBase } from "./legal-draft-nav.ts";

describe("pickDraftKnowledgeBase", () => {
  const kbs = [
    { id: "a", name: "VBPL – Luật" },
    { id: "b", name: "VBPL – Nghị định" },
    { id: "c", name: "VBPL – Thông tư liên tịch" },
  ];
  it("keeps a listed preferred KB", () => assert.equal(pickDraftKnowledgeBase(kbs, "Nghị định", "c"), "c"));
  it("ignores an unknown preferred KB", () => assert.equal(pickDraftKnowledgeBase(kbs, "Nghị định", "zz"), "b"));
  it("matches the type after the dash", () => assert.equal(pickDraftKnowledgeBase(kbs, "nghị định", null), "b"));
  it("falls back to the first", () => assert.equal(pickDraftKnowledgeBase(kbs, "Quyết định"), "a"));
  it("empty list", () => assert.equal(pickDraftKnowledgeBase([], "Luật"), ""));
});

describe("headingLead", () => {
  it("labels the kinds", () => {
    assert.equal(headingLead({ kind: "chuong", number: "II" }), "Chương II");
    assert.equal(headingLead({ kind: "dieu", number: "5" }), "Điều 5");
  });
});

describe("locateParagraph", () => {
  const texts = ["QUỐC HỘI", "Điều 5. Phạm vi", "", "Điều 50. Hiệu lực", "Điều 5. Phạm vi"];
  it("uses the server index when it fits", () => {
    assert.deepEqual(locateParagraph(texts, 4, "Điều 5"), { text: "Điều 5. Phạm vi", occurrence: 2 });
  });
  it("searches by lead when the index is off", () => {
    assert.deepEqual(locateParagraph(texts, 3, "Điều 5"), { text: "Điều 5. Phạm vi", occurrence: 1 });
    assert.deepEqual(locateParagraph(texts, 0, "Điều 50"), { text: "Điều 50. Hiệu lực", occurrence: 1 });
  });
  it("an index alone selects a non-empty paragraph", () => {
    assert.deepEqual(locateParagraph(texts, 0), { text: "QUỐC HỘI", occurrence: 1 });
    assert.equal(locateParagraph(texts, 2), null);
  });
  it("not found", () => assert.equal(locateParagraph(texts, -1, "Điều 9"), null));
});
