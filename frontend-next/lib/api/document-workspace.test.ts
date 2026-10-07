// Pure helpers of the document-assistant workspace client.
// Runs with: node --experimental-strip-types --test lib/api/document-workspace.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  DocumentWorkspaceError,
  documentRevisionFromToolData,
  documentSelectionForDisplay,
  selectionNeedsCollapse,
  openDocumentInNewSession,
  documentServerOrigin,
  isWordAttachment,
  parsePluginSelectionMessage,
  shouldRefreshEditor,
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

describe("shouldRefreshEditor", () => {
  const base = { currentKey: "k1", nextKey: "k2", editorReady: true, inFlight: false };
  it("refreshes on a new key once the editor is ready and idle", () => {
    assert.equal(shouldRefreshEditor(base), true);
  });
  it("never refreshes for the same or an empty key", () => {
    assert.equal(shouldRefreshEditor({ ...base, nextKey: "k1" }), false);
    assert.equal(shouldRefreshEditor({ ...base, nextKey: "" }), false);
    assert.equal(shouldRefreshEditor({ ...base, nextKey: undefined }), false);
  });
  it("waits while the editor is loading or a refresh is in flight", () => {
    assert.equal(shouldRefreshEditor({ ...base, editorReady: false }), false);
    assert.equal(shouldRefreshEditor({ ...base, inFlight: true }), false);
  });
  it("treats a missing current key as changed", () => {
    assert.equal(shouldRefreshEditor({ ...base, currentKey: null }), true);
  });
});

describe("openDocumentInNewSession", () => {
  const mkDeps = (over: Partial<Record<"upload" | "createWorkspace" | "deleteSession", () => Promise<unknown>>> = {}) => {
    const calls: string[] = [];
    const deps = {
      createSession: async () => {
        calls.push("create");
        return "s1";
      },
      upload: async (sid: string, file: string) => {
        calls.push(`upload:${sid}:${file}`);
        if (over.upload) await over.upload();
        return "a1";
      },
      createWorkspace: async (sid: string, aid: string) => {
        calls.push(`workspace:${sid}:${aid}`);
        if (over.createWorkspace) await over.createWorkspace();
      },
      deleteSession: async (sid: string) => {
        calls.push(`delete:${sid}`);
        if (over.deleteSession) await over.deleteSession();
      },
    };
    return { calls, deps };
  };

  it("creates the session, uploads, opens the workspace", async () => {
    const { calls, deps } = mkDeps();
    assert.equal(await openDocumentInNewSession("f.docx", deps), "s1");
    assert.deepEqual(calls, ["create", "upload:s1:f.docx", "workspace:s1:a1"]);
  });

  it("deletes the session when the upload fails", async () => {
    const { calls, deps } = mkDeps({ upload: async () => { throw new Error("upload boom"); } });
    await assert.rejects(openDocumentInNewSession("f.docx", deps), /upload boom/);
    assert.deepEqual(calls, ["create", "upload:s1:f.docx", "delete:s1"]);
  });

  it("deletes the session when the workspace create fails, keeping the original error", async () => {
    const { calls, deps } = mkDeps({
      createWorkspace: async () => { throw new Error("ws boom"); },
      deleteSession: async () => { throw new Error("delete boom"); },
    });
    await assert.rejects(openDocumentInNewSession("f.docx", deps), /ws boom/);
    assert.deepEqual(calls, ["create", "upload:s1:f.docx", "workspace:s1:a1", "delete:s1"]);
  });

  it("throws without uploading or deleting when createSession returns no id", async () => {
    const { calls, deps } = mkDeps();
    deps.createSession = async () => {
      calls.push("create");
      return "";
    };
    await assert.rejects(openDocumentInNewSession("f.docx", deps), /Failed to create session/);
    assert.deepEqual(calls, ["create"]);
  });

  it("treats an already-open workspace as success", async () => {
    const { calls, deps } = mkDeps({
      createWorkspace: async () => { throw new DocumentWorkspaceError("already_exists", 409, "exists"); },
    });
    assert.equal(await openDocumentInNewSession("f.docx", deps), "s1");
    assert.ok(!calls.includes("delete:s1"));
  });
});

describe("selection quote helpers", () => {
  it("documentSelectionForDisplay keeps only non-empty text", () => {
    assert.equal(documentSelectionForDisplay(undefined), undefined);
    assert.equal(documentSelectionForDisplay({ text: "   " }), undefined);
    assert.equal(documentSelectionForDisplay("x"), undefined);
    assert.deepEqual(documentSelectionForDisplay({ text: " Điều 1 ", paragraph_hint: " " }), { text: "Điều 1" });
    assert.deepEqual(documentSelectionForDisplay({ text: "a", paragraph_hint: "p2" }), { text: "a", paragraph_hint: "p2" });
  });
  it("selectionNeedsCollapse by length or line count", () => {
    assert.equal(selectionNeedsCollapse("short"), false);
    assert.equal(selectionNeedsCollapse(""), false);
    assert.equal(selectionNeedsCollapse("x".repeat(281)), true);
    assert.equal(selectionNeedsCollapse("x".repeat(280)), false);
    assert.equal(selectionNeedsCollapse("a\nb\nc\nd"), false);
    assert.equal(selectionNeedsCollapse("a\nb\nc\nd\ne"), true);
    assert.equal(selectionNeedsCollapse("abcdef", 5), true);
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
