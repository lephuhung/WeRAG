// Pure helpers of the document-assistant workspace client.
// Runs with: node --experimental-strip-types --test lib/api/document-workspace.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  DocumentWorkspaceError,
  latestRevisionOf,
  revisionsNewestFirst,
  formatCheckInProgress,
  formatCheckResultKey,
  formatCheckProgress,
  formatCheckIsCurrent,
  FORMAT_CHECK_EXPECTED_MS,
  documentSelectionForDisplay,
  documentFileTooLarge,
  MAX_DOCUMENT_FILE_BYTES,
  selectionNeedsCollapse,
  openDocumentInNewSession,
  documentServerOrigin,
  isWordAttachment,
  isSourceDocument,
  splitDocumentsByRole,
  unopenedWordUploads,
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
  it("documentSelectionForDisplay keeps the document the passage is in", () => {
    assert.deepEqual(documentSelectionForDisplay({ text: " a ", document_id: "ws-2", document: "vb2 · b.docx" }), {
      text: "a",
      document_id: "ws-2",
      document: "vb2 · b.docx",
    });
  });

  it("documentFileTooLarge guards the 10 MB limit", () => {
    assert.equal(documentFileTooLarge({ size: MAX_DOCUMENT_FILE_BYTES }), false);
    assert.equal(documentFileTooLarge({ size: MAX_DOCUMENT_FILE_BYTES + 1 }), true);
  });

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

describe("revision timeline helpers", () => {
  const revs = [
    { seq: 1, source: "manual", label: "a", created_at: "" },
    { seq: 3, source: "ai", label: "c", created_at: "" },
    { seq: 2, source: "ai", label: "b", created_at: "" },
  ];
  it("sorts newest first without mutating", () => {
    assert.deepEqual(revisionsNewestFirst(revs).map((r) => r.seq), [3, 2, 1]);
    assert.equal(revs[0].seq, 1);
  });
  it("finds the latest snapshot of a source", () => {
    assert.equal(latestRevisionOf(revs, "ai")?.seq, 3);
    assert.equal(latestRevisionOf(revs, "close"), null);
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

describe("formatCheckResultKey", () => {
  const ready = { status: "ready" as const, revision: 0, started_at: "t0", finished_at: "t1" };
  it("changes when a new check finishes", () => {
    assert.notEqual(formatCheckResultKey("s", ready), formatCheckResultKey("s", { ...ready, finished_at: "t2" }));
    assert.notEqual(formatCheckResultKey("s", ready), formatCheckResultKey("s", { ...ready, revision: 1 }));
  });
  it("tells a running check from its finished result", () => {
    const running = { status: "running" as const, revision: 0, started_at: "t0" };
    assert.notEqual(formatCheckResultKey("s", running), formatCheckResultKey("s", ready));
    assert.equal(formatCheckResultKey("s", running), formatCheckResultKey("s", { ...running }));
  });
});

describe("formatCheckProgress", () => {
  const start = "2026-10-07T15:00:00.000Z";
  const at = (ms: number) => Date.parse(start) + ms;
  it("starts low, grows, and never reaches full while running", () => {
    const early = formatCheckProgress(start, at(1_000));
    const mid = formatCheckProgress(start, at(FORMAT_CHECK_EXPECTED_MS / 2));
    const late = formatCheckProgress(start, at(10 * FORMAT_CHECK_EXPECTED_MS));
    assert.ok(early >= 0.05 && early < mid && mid < late);
    assert.equal(late, 0.95);
  });
  it("tolerates a bad timestamp and a clock behind the server", () => {
    assert.equal(formatCheckProgress("not a date", at(0)), 0.05);
    assert.equal(formatCheckProgress(start, at(-5_000)), 0.05);
  });
});

describe("formatCheckIsCurrent", () => {
  const ready = { status: "ready" as const, revision: 2, started_at: "2026-10-07T15:00:00Z", finished_at: "2026-10-07T15:01:10Z" };
  it("keeps a check of the saved document", () => {
    assert.equal(formatCheckIsCurrent(ready, { revision: 2 }), true);
    assert.equal(formatCheckIsCurrent(ready, { revision: 2, last_saved_at: "2026-10-07T14:59:00Z" }), true);
  });
  it("drops it once the document was saved or rewritten after the check started", () => {
    assert.equal(formatCheckIsCurrent(ready, { revision: 2, last_saved_at: "2026-10-07T15:05:00Z" }), false);
    assert.equal(formatCheckIsCurrent(ready, { revision: 3 }), false);
  });
  it("follows the save the backend marked as covered", () => {
    const kept = { ...ready, checked_saved_at: "2026-10-07T15:06:00Z" };
    assert.equal(formatCheckIsCurrent(kept, { revision: 2, last_saved_at: "2026-10-07T15:05:00Z" }), true);
    assert.equal(formatCheckIsCurrent(kept, { revision: 2, last_saved_at: "2026-10-07T15:07:00Z" }), false);
  });
  it("always shows a running or queued check", () => {
    assert.equal(formatCheckIsCurrent({ ...ready, status: "running" }, { revision: 9, last_saved_at: "2026-10-07T16:00:00Z" }), true);
    assert.equal(formatCheckIsCurrent({ ...ready, status: "queued" }, { revision: 9, last_saved_at: "2026-10-07T16:00:00Z" }), true);
  });
});

describe("formatCheckInProgress", () => {
  it("counts a queued check as unfinished", () => {
    const at = { revision: 0, started_at: "t0" };
    assert.equal(formatCheckInProgress({ ...at, status: "queued" }), true);
    assert.equal(formatCheckInProgress({ ...at, status: "running" }), true);
    assert.equal(formatCheckInProgress({ ...at, status: "ready" }), false);
    assert.equal(formatCheckInProgress({ ...at, status: "failed" }), false);
  });
});

describe("document roles", () => {
  it("a row without a role is a target", () => {
    assert.equal(isSourceDocument({ role: "source" }), true);
    assert.equal(isSourceDocument({ role: "target" }), false);
    assert.equal(isSourceDocument({}), false);
    assert.equal(isSourceDocument(null), false);
  });
  it("splits targets (tabs) from sources, keeping the handle order", () => {
    const docs = [
      { id: "a", role: "target" },
      { id: "b", role: "source" },
      { id: "c" },
      { id: "d", role: "source" },
    ];
    const { targets, sources } = splitDocumentsByRole(docs);
    assert.deepEqual(targets.map((d) => d.id), ["a", "c"]);
    assert.deepEqual(sources.map((d) => d.id), ["b", "d"]);
  });
});

describe("unopenedWordUploads", () => {
  it("keeps Word uploads without a document of either role", () => {
    const uploads = [
      { id: "old", file_name: "cu.docx", status: "ready" },
      { id: "doc", file_name: "x", file_type: ".doc", status: "processing" },
      { id: "tab", file_name: "tab.docx", status: "ready" },
      { id: "src", file_name: "nguon.docx", status: "ready" },
      { id: "pdf", file_name: "a.pdf", status: "ready" },
      { id: "bad", file_name: "hong.docx", status: "failed" },
    ];
    const docs = [{ attachment_id: "tab" }, { attachment_id: "src" }, { attachment_id: "" }];
    assert.deepEqual(unopenedWordUploads(uploads, docs).map((a) => a.id), ["old", "doc"]);
  });
});
