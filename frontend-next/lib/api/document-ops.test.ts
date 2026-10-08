// Edit-plan helpers of the document assistant.
// Runs with: node --experimental-strip-types --test lib/api/document-ops.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  applyOpsMessage,
  findAnchorParagraph,
  normalizeAnchorText,
  opValidationError,
  opsBatchFromToolData,
  parsePluginOpsMessage,
  readAppliedBatches,
  rememberAppliedBatch,
} from "./document-ops.ts";

describe("normalizeAnchorText", () => {
  it("collapses whitespace, NBSP, tabs, \\r and zero-width chars, trims", () => {
    assert.equal(normalizeAnchorText("  Điều\u00A0 1.\t\tPhạm\rvi\u200B "), "Điều 1. Phạm vi");
  });
  it("normalises to NFC", () => {
    const decomposed = "Hoà"; // "Hoà" as base + combining grave
    assert.equal(normalizeAnchorText(decomposed), "Hoà".normalize("NFC"));
  });
});

describe("findAnchorParagraph", () => {
  const paras = ["Căn cứ Luật X;", "", "Căn cứ  Luật X;", "Điều 1"];
  it("matches normalised text, 1-based occurrence", () => {
    assert.equal(findAnchorParagraph(paras, { text: "Căn cứ Luật X;" }), 0);
    assert.equal(findAnchorParagraph(paras, { text: "Căn cứ Luật X;", occurrence: 2 }), 2);
    assert.equal(findAnchorParagraph(paras, { text: "Căn cứ Luật X;", occurrence: 3 }), -1);
  });
  it("never matches an empty anchor", () => {
    assert.equal(findAnchorParagraph(paras, { text: "  " }), -1);
  });
});

describe("opValidationError", () => {
  const a = { text: "Điều 1" };
  it("accepts every op shape of the contract", () => {
    for (const op of [
      { op: "replaceText", anchor: a, old: "x", new: "y" },
      { op: "replaceParagraph", anchor: a, new: "" },
      { op: "insertAfter", anchor: { atStart: true }, text: "Mở đầu", alignment: "center", bold: true },
      { op: "insertAfter", anchor: { ...a, occurrence: 2 }, text: "t", like: { text: "Điều 1" } },
      { op: "mark", anchor: a, text: "1", style: "underline" },
      { op: "mark", anchor: a, style: "highlight" },
      { op: "mark", anchor: a, text: "1", textOccurrence: 2, style: "color" },
      { op: "formatParagraph", anchor: a, alignment: "both", font: "Times New Roman", sizePt: 14 },
      { op: "pageSetup", marginsMm: { top: 20, left: 30 }, a4: true },
      { op: "pageSetup", a4: true },
    ]) {
      assert.equal(opValidationError(op), null, JSON.stringify(op));
    }
  });
  it("rejects malformed ops", () => {
    assert.match(opValidationError({ op: "nope" })!, /unknown op/);
    assert.equal(opValidationError({ op: "replaceText", anchor: a, old: "", new: "y" }), "missing old");
    assert.equal(opValidationError({ op: "replaceText", anchor: { text: "" }, old: "x", new: "y" }), "bad anchor");
    assert.equal(opValidationError({ op: "replaceText", anchor: { text: "x", occurrence: 0 }, old: "x", new: "y" }), "bad anchor");
    assert.equal(opValidationError({ op: "replaceParagraph", anchor: { atStart: true }, new: "x" }), "bad anchor");
    assert.equal(opValidationError({ op: "insertAfter", anchor: a, text: "a\nb" }), "text must be one line");
    assert.equal(opValidationError({ op: "mark", anchor: a, style: "bold" }), "bad style");
    for (const bad of [0, -1, 1.5, "2"]) {
      assert.equal(opValidationError({ op: "mark", anchor: a, text: "x", textOccurrence: bad, style: "color" }), "bad textOccurrence");
    }
    assert.equal(opValidationError({ op: "formatParagraph", anchor: a, alignment: "justify" }), "bad alignment");
    assert.equal(opValidationError({ op: "pageSetup" }), "empty pageSetup");
    assert.equal(opValidationError({ op: "pageSetup", marginsMm: { top: -1 } }), "bad marginsMm");
  });
});

describe("opsBatchFromToolData", () => {
  it("extracts the batch of an editing tool, dropping invalid ops", () => {
    const b = opsBatchFromToolData("rewrite_paragraphs", {
      ops_batch_id: "b1",
      document_ops: [{ op: "replaceParagraph", anchor: { text: "x" }, new: "y" }, { op: "bogus" }],
    });
    assert.equal(b?.batchId, "b1");
    assert.equal(b?.ops.length, 1);
    assert.deepEqual(b?.rejected.map((r) => r.index), [1]);
  });
  it("fills the omitted text of a blank insertAfter line", () => {
    const b = opsBatchFromToolData("insert_paragraphs", {
      ops_batch_id: "b2",
      document_ops: [{ op: "insertAfter", anchor: { atStart: true } }],
    });
    assert.deepEqual(b?.ops, [{ op: "insertAfter", anchor: { atStart: true }, text: "" }]);
  });
  it("ignores other tools, empty ops and missing batch ids", () => {
    assert.equal(opsBatchFromToolData("knowledge_search", { ops_batch_id: "b", document_ops: [{}] }), null);
    assert.equal(opsBatchFromToolData("mark_passages", { ops_batch_id: "b", document_ops: [] }), null);
    assert.equal(opsBatchFromToolData(undefined, { tool_name: "mark_passages", document_ops: [{}] }), null);
  });

  it("carries the target document and accepts check_spelling marks", () => {
    const b = opsBatchFromToolData("check_spelling", {
      ops_batch_id: "b3",
      document_id: "ws-2",
      document_ops: [{ op: "mark", anchor: { text: "x" }, text: "x", style: "underline" }],
    });
    assert.equal(b?.documentId, "ws-2");
    assert.equal(b?.ops.length, 1);
    const legacy = opsBatchFromToolData("mark_passages", {
      ops_batch_id: "b4",
      document_ops: [{ op: "mark", anchor: { text: "x" }, style: "underline" }],
    });
    assert.equal(legacy?.documentId, undefined);
  });
});

describe("plugin messages", () => {
  it("parses ack and result, ignores foreign messages", () => {
    assert.deepEqual(parsePluginOpsMessage({ source: "werag-onlyoffice", type: "ops_ack", batchId: "b" }), { type: "ops_ack", batchId: "b" });
    assert.deepEqual(
      parsePluginOpsMessage({ source: "werag-onlyoffice", type: "ops_result", batchId: "b", applied: 2, failed: [{ index: 1, error: "anchor not found" }] }),
      { type: "ops_result", batchId: "b", applied: 2, failed: [{ index: 1, error: "anchor not found" }] },
    );
    assert.equal(parsePluginOpsMessage({ source: "werag-onlyoffice", type: "selection", text: "x" }), null);
    assert.equal(parsePluginOpsMessage({ source: "other", type: "ops_ack", batchId: "b" }), null);
  });
  it("builds the host message", () => {
    assert.deepEqual(applyOpsMessage({ batchId: "b", ops: [] }), { source: "werag-host", type: "apply_ops", batchId: "b", ops: [] });
  });
});

describe("applied-batch ledger", () => {
  it("remembers ids per session, deduped, tolerant of broken storage", () => {
    const m = new Map<string, string>();
    const store = { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v) };
    rememberAppliedBatch(store, "s1", "a");
    rememberAppliedBatch(store, "s1", "b");
    rememberAppliedBatch(store, "s1", "a");
    assert.deepEqual(readAppliedBatches(store, "s1"), ["b", "a"]);
    assert.deepEqual(readAppliedBatches(store, "s2"), []);
    const broken = { getItem: () => { throw new Error("blocked"); }, setItem: () => { throw new Error("blocked"); } };
    assert.deepEqual(readAppliedBatches(broken, "s1"), []);
    rememberAppliedBatch(broken, "s1", "x");
  });
});
