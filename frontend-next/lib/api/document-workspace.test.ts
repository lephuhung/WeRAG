// Pure helpers of the document-assistant workspace client.
// Runs with: node --experimental-strip-types --test lib/api/document-workspace.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  documentRevisionFromToolData,
  documentServerOrigin,
  isWordAttachment,
  parsePluginSelectionMessage,
} from "./document-workspace.ts";
import { clampLeftPct } from "../../components/doc-workspace/split-pane-math.ts";

describe("parsePluginSelectionMessage", () => {
  it("ignores foreign messages", () => {
    assert.equal(parsePluginSelectionMessage(null), undefined);
    assert.equal(parsePluginSelectionMessage("hi"), undefined);
    assert.equal(parsePluginSelectionMessage({ source: "other", type: "selection", text: "x" }), undefined);
  });
  it("returns null for an empty selection", () => {
    assert.equal(parsePluginSelectionMessage({ source: "werag-onlyoffice", type: "selection", text: "  " }), null);
  });
  it("trims text and maps paragraphHint", () => {
    assert.deepEqual(
      parsePluginSelectionMessage({ source: "werag-onlyoffice", type: "selection", text: " Điều 1 ", paragraphHint: "p3" }),
      { text: "Điều 1", paragraph_hint: "p3" },
    );
  });
});

describe("documentRevisionFromToolData", () => {
  it("reads numeric revisions of editing tools only", () => {
    assert.equal(documentRevisionFromToolData("apply_format_fixes", { document_revision: 4 }), 4);
    assert.equal(documentRevisionFromToolData(undefined, { tool_name: "rewrite_paragraphs", document_revision: "7" }), 7);
    assert.equal(documentRevisionFromToolData("knowledge_search", { document_revision: 4 }), null);
    assert.equal(documentRevisionFromToolData("apply_format_fixes", {}), null);
  });
});

describe("misc helpers", () => {
  it("documentServerOrigin", () => {
    assert.equal(documentServerOrigin("https://docs.example.vn:8443/ds/"), "https://docs.example.vn:8443");
    assert.equal(documentServerOrigin("not a url"), null);
  });
  it("isWordAttachment", () => {
    assert.equal(isWordAttachment("a.DOCX"), true);
    assert.equal(isWordAttachment("a.pdf"), false);
    assert.equal(isWordAttachment("x", "doc"), true);
  });
  it("clampLeftPct", () => {
    assert.equal(clampLeftPct(40), 55);
    assert.equal(clampLeftPct(90), 75);
    assert.equal(clampLeftPct(Number.NaN), 66);
    assert.equal(clampLeftPct(60.4), 60.4);
  });
});
