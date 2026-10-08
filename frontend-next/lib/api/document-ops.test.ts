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
  rewriteProposalFromToolData,
  proposalBatchId,
  proposalOpsBatch,
  proposalOutcomeApplied,
  readAppliedProposals,
  rememberAppliedProposal,
  newestProposalByDocument,
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

describe("rewrite proposals", () => {
  const anchor = { text: "Sở Nội vụ đề nghị năm 2026.", occurrence: 1 };
  const proposalData = (over: Record<string, unknown> = {}) => ({
    proposal: true,
    ops_batch_id: "b-1",
    document_id: "ws-2",
    document: "vb2 · To-trinh.docx",
    selection_text: "năm 2026",
    variants: [
      { id: "v1", label: "Gọn hơn", old: "năm 2026", new: "năm 2027", ops: [{ op: "replaceText", anchor, old: "năm 2026", new: "năm 2027" }] },
      { id: "v2", label: "Phương án 2", old: "năm 2026", new: "trong năm 2027", ops: [{ op: "replaceText", anchor, old: "năm 2026", new: "trong năm 2027" }] },
    ],
    ...over,
  });

  it("reads the proposal of a rewrite_paragraphs result", () => {
    const p = rewriteProposalFromToolData("rewrite_paragraphs", proposalData());
    assert.ok(p);
    assert.equal(p.batchId, "b-1");
    assert.equal(p.documentId, "ws-2");
    assert.equal(p.document, "vb2 · To-trinh.docx");
    assert.equal(p.selectionText, "năm 2026");
    assert.deepEqual(p.variants.map((v) => [v.id, v.label, v.new]), [
      ["v1", "Gọn hơn", "năm 2027"],
      ["v2", "Phương án 2", "trong năm 2027"],
    ]);
  });

  it("finds the tool name in data when the chunk has none", () => {
    assert.ok(rewriteProposalFromToolData(undefined, { ...proposalData(), tool_name: "rewrite_paragraphs" }));
  });

  it("is null for other tools, applied rewrites and empty proposals", () => {
    assert.equal(rewriteProposalFromToolData("insert_paragraphs", proposalData()), null);
    assert.equal(rewriteProposalFromToolData("rewrite_paragraphs", proposalData({ proposal: undefined })), null);
    assert.equal(rewriteProposalFromToolData("rewrite_paragraphs", proposalData({ variants: [] })), null);
    assert.equal(rewriteProposalFromToolData("rewrite_paragraphs", proposalData({ document_id: "" })), null);
    assert.equal(rewriteProposalFromToolData("rewrite_paragraphs", proposalData({ ops_batch_id: " " })), null);
  });

  it("drops a version with an invalid op or none, and duplicate ids", () => {
    const p = rewriteProposalFromToolData(
      "rewrite_paragraphs",
      proposalData({
        variants: [
          { id: "v1", label: "", old: "a", new: "b", ops: [{ op: "replaceText", anchor, old: "", new: "b" }] },
          { id: "v2", old: "a", new: "c", ops: [] },
          { id: "v3", old: "a", new: "d", ops: [{ op: "replaceParagraph", anchor, new: "d" }] },
          { id: "v3", old: "a", new: "e", ops: [{ op: "replaceParagraph", anchor, new: "e" }] },
        ],
      }),
    );
    assert.ok(p);
    assert.deepEqual(p.variants.map((v) => [v.id, v.label, v.new]), [["v3", "v3", "d"]]);
  });

  it("never auto-applies: opsBatchFromToolData ignores a proposal", () => {
    const data = { ...proposalData(), document_ops: [{ op: "replaceText", anchor, old: "năm 2026", new: "x" }] };
    assert.equal(opsBatchFromToolData("rewrite_paragraphs", data), null);
    // an applied rewrite still yields its batch
    assert.ok(opsBatchFromToolData("rewrite_paragraphs", { ...data, proposal: undefined, ops_batch_id: "b-2" }));
  });

  it("composes the version's batch id and batch", () => {
    const p = rewriteProposalFromToolData("rewrite_paragraphs", proposalData())!;
    assert.equal(proposalBatchId("b-1", "v2"), "b-1:v2");
    assert.deepEqual(proposalOpsBatch(p, p.variants[1]), {
      batchId: "b-1:v2",
      ops: p.variants[1].ops,
      documentId: "ws-2",
    });
  });

  it("tells an applied outcome from a failed one", () => {
    assert.equal(proposalOutcomeApplied({ kind: "result", applied: 1 }), true);
    assert.equal(proposalOutcomeApplied({ kind: "result", applied: 0 }), false);
    assert.equal(proposalOutcomeApplied({ kind: "timeout" }), false);
  });

  it("remembers the applied version per session", () => {
    const mem = new Map<string, string>();
    const store = { getItem: (k: string) => mem.get(k) ?? null, setItem: (k: string, v: string) => void mem.set(k, v) };
    assert.deepEqual(readAppliedProposals(store, "s1"), {});
    rememberAppliedProposal(store, "s1", "b-1", "v2");
    rememberAppliedProposal(store, "s1", "b-2", "v1");
    assert.deepEqual(readAppliedProposals(store, "s1"), { "b-1": "v2", "b-2": "v1" });
    assert.deepEqual(readAppliedProposals(store, "s2"), {});
    mem.set("werag.docops.proposals.s3", "not json");
    assert.deepEqual(readAppliedProposals(store, "s3"), {});
    const broken = { getItem: () => { throw new Error("blocked"); }, setItem: () => { throw new Error("blocked"); } };
    assert.deepEqual(readAppliedProposals(broken, "s1"), {});
    rememberAppliedProposal(broken, "s1", "b", "v1");
  });

  it("keeps only the newest proposal of each document active", () => {
    const p = (batchId: string, documentId: string) => ({ batchId, documentId, document: "", selectionText: "", variants: [] });
    const newest = newestProposalByDocument([p("a", "d1"), p("b", "d2"), p("c", "d1")]);
    assert.equal(newest.get("d1"), "c");
    assert.equal(newest.get("d2"), "b");
  });
});
