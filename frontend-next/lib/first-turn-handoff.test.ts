// B-2a(2) RED: the create-chat first turn must hand off selected files/images
// to the new session as uploaded attachment IDs (never raw bytes, never
// dropped), and a failed upload must surface instead of navigating silently.
// Runs with: node --experimental-strip-types --test lib/first-turn-handoff.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  buildFirstTurnSearch,
  buildSuggestionSend,
  cleanupSessionBestEffort,
  combinedCapError,
  consumeFirstTurnHandoff,
  FIRST_TURN_HANDOFF_FLAG_PARAM,
  firstTurnHandoffKey,
  initHandoffSessionState,
  MAX_TEMPORARY_ATTACHMENTS_PER_MESSAGE,
  mergeAttachmentIds,
  parseFirstTurnSearch,
  resolveFirstTurnHandoff,
  rotateHandoffSessionState,
  saveFirstTurnHandoff,
  takeHandoffTurn,
  uploadFirstTurnFiles,
  type FirstTurnHandoffPayload,
  type HandoffStore,
} from "./first-turn-handoff.ts";

const pdf = (name: string) => new File(["%PDF-content"], name, { type: "application/pdf" });
const png = (name: string) => new File(["img-bytes"], name, { type: "image/png" });

describe("first-turn handoff search params (URL carries only the marker)", () => {
  it("round-trips query and question origin without IDs", () => {
    const qs = buildFirstTurnSearch({
      query: "summarize these",
      questionOrigin: { knowledge_base_id: "kb-1", knowledge_id: "doc-9" },
      hasAttachments: true,
    });
    const parsed = parseFirstTurnSearch(new URLSearchParams(qs));
    assert.equal(parsed.query, "summarize these");
    assert.deepEqual(parsed.questionOrigin, { knowledge_base_id: "kb-1", knowledge_id: "doc-9" });
    assert.equal(parsed.expectsAttachments, true);
  });

  it("carries no IDs or names — only the innocuous flag", () => {
    const qs = buildFirstTurnSearch({ query: "q", hasAttachments: true });
    assert.ok(!qs.includes("&aids") && !qs.startsWith("aids"), "query string must not embed attachment IDs");
    assert.ok(!qs.includes("%PDF"), "query string must not embed file bytes");
    assert.ok(qs.includes(`${FIRST_TURN_HANDOFF_FLAG_PARAM}=`));
  });

  it("parses a question without the flag as attachment-free", () => {
    assert.equal(parseFirstTurnSearch(new URLSearchParams("q=hi")).expectsAttachments, false);
  });

  it("keeps origin-less questions origin-free", () => {
    const parsed = parseFirstTurnSearch(new URLSearchParams(buildFirstTurnSearch({ query: "hi" })));
    assert.equal(parsed.questionOrigin, undefined);
    assert.equal(parsed.query, "hi");
  });
});

describe("uploadFirstTurnFiles", () => {
  it("uploads files then images in order and returns their IDs", async () => {
    const seen: string[] = [];
    const ids = await uploadFirstTurnFiles("sess-1", [pdf("a.pdf"), pdf("b.pdf")], [png("c.png")], {
      upload: async (_sid, file) => {
        assert.equal(_sid, "sess-1");
        seen.push(file.name);
        return { id: `doc-${file.name}` };
      },
    });
    assert.deepEqual(seen, ["a.pdf", "b.pdf", "c.png"]);
    assert.deepEqual(ids, ["doc-a.pdf", "doc-b.pdf", "doc-c.png"]);
  });

  it("returns [] without uploading when nothing was selected", async () => {
    let calls = 0;
    const ids = await uploadFirstTurnFiles("sess-1", [], [], {
      upload: async () => {
        calls++;
        return { id: "x" };
      },
    });
    assert.deepEqual(ids, []);
    assert.equal(calls, 0);
  });

  it("on failure throws (caller stays for retry) and cleans up partial uploads", async () => {
    const removed: string[] = [];
    const err = await uploadFirstTurnFiles("sess-1", [pdf("ok.pdf"), pdf("boom.pdf")], [], {
      upload: async (_sid, file) => {
        if (file.name === "boom.pdf") throw new Error("network down");
        return { id: `doc-${file.name}` };
      },
      remove: async (_sid, id) => {
        removed.push(id);
      },
    }).then(
      () => null,
      (e: unknown) => e as Error,
    );
    assert.ok(err instanceof Error, "must reject so the page does not navigate");
    assert.ok(String(err?.message).includes("boom.pdf"), "error must name the failed file");
    assert.deepEqual(removed, ["doc-ok.pdf"]);
  });
});

describe("first-turn handoff display names (storage metadata, never bytes)", () => {
  it("round-trips attachment names including commas and unicode via storage", () => {
    const store = memoryStore();
    saveFirstTurnHandoff(store, "sess-1", {
      attachmentIds: ["a1", "a2"],
      attachmentNames: ["report, final.pdf", "ảnh chụp màn hình.png"],
    });
    const got = consumeFirstTurnHandoff(store, "sess-1");
    assert.deepEqual(got?.attachmentIds, ["a1", "a2"]);
    assert.deepEqual(got?.attachmentNames, ["report, final.pdf", "ảnh chụp màn hình.png"]);
  });

  it("the URL carries names never — not even as bytes", () => {
    const qs = buildFirstTurnSearch({ query: "q", hasAttachments: true });
    assert.ok(!qs.includes("data:"), "query string must not embed file bytes");
    assert.ok(!qs.includes("a.pdf"));
  });

  it("resolve without a stored handoff yields no names (no stale metadata)", () => {
    const r = resolveFirstTurnHandoff(new URLSearchParams("q=hi"), null);
    assert.deepEqual(r.attachmentNames, []);
  });
});

describe("combined attachment cap (mirrors Go MaxTemporaryAttachmentsPerMessage)", () => {
  it("matches the server cap of 5", () => {
    assert.equal(MAX_TEMPORARY_ATTACHMENTS_PER_MESSAGE, 5);
  });

  it("allows exactly 5 files+images, rejects 6 before upload", () => {
    assert.equal(combinedCapError(5), null);
    assert.equal(combinedCapError(0), null);
    const err = combinedCapError(6);
    assert.ok(typeof err === "string" && err.includes("5"), "error must name the cap");
  });
});

describe("mergeAttachmentIds", () => {
  it("dedupes across lists while preserving first-seen order", () => {
    assert.deepEqual(
      mergeAttachmentIds(["a", "b"], ["b", "c"], undefined, ["a", "d"]),
      ["a", "b", "c", "d"],
    );
  });

  it("drops blanks and trims", () => {
    assert.deepEqual(mergeAttachmentIds([" a ", "", "  "], ["b"]), ["a", "b"]);
  });
});

describe("session-keyed one-shot handoff state", () => {
  const parsedA = {
    questionOrigin: { knowledge_base_id: "kb-a" },
    attachmentIds: ["a1"],
    attachmentNames: ["a.pdf"],
  };

  it("first take yields IDs+names+origin, second take is empty (no leak to later turns)", () => {
    const s0 = initHandoffSessionState("sess-1", parsedA);
    const first = takeHandoffTurn(s0);
    assert.deepEqual(first.attachmentIds, ["a1"]);
    assert.deepEqual(first.attachmentNames, ["a.pdf"]);
    assert.deepEqual(first.questionOrigin, { knowledge_base_id: "kb-a" });
    const second = takeHandoffTurn(first.next);
    assert.deepEqual(second.attachmentIds, []);
    assert.deepEqual(second.attachmentNames, []);
    assert.equal(second.questionOrigin, undefined);
  });

  it("rotating to a new session ID resets IDs AND the consumed flag", () => {
    const s0 = initHandoffSessionState("sess-1", parsedA);
    const consumed = takeHandoffTurn(s0).next;
    assert.equal(consumed.consumed, true);
    const parsedB = { attachmentIds: ["b9"], attachmentNames: ["b.pdf"] };
    const s1 = rotateHandoffSessionState(consumed, "sess-2", parsedB);
    assert.equal(s1.consumed, false);
    const take = takeHandoffTurn(s1);
    assert.deepEqual(take.attachmentIds, ["b9"]);
    assert.deepEqual(take.attachmentNames, ["b.pdf"]);
    assert.equal(take.questionOrigin, undefined);
  });

  it("rotating with the same session ID keeps the consumed flag (no replay)", () => {
    const s0 = initHandoffSessionState("sess-1", parsedA);
    const consumed = takeHandoffTurn(s0).next;
    const same = rotateHandoffSessionState(consumed, "sess-1", {
      attachmentIds: ["evil"],
      attachmentNames: ["evil.pdf"],
    });
    assert.deepEqual(takeHandoffTurn(same).attachmentIds, []);
  });

  it("a null previous state initializes from the route", () => {
    const s = rotateHandoffSessionState(null, "sess-9", parsedA);
    assert.deepEqual(takeHandoffTurn(s).attachmentIds, ["a1"]);
  });
});

describe("buildSuggestionSend", () => {
  it("carries the currently selected attachments and image files", () => {
    const attachments = [{ localId: "l1", name: "a.pdf" }];
    const imageFiles = [png("shot.png")];
    const send = buildSuggestionSend({
      query: "picked suggestion",
      questionOrigin: { knowledge_base_id: "kb-1" },
      attachments,
      imageFiles,
    });
    assert.equal(send.query, "picked suggestion");
    assert.deepEqual(send.attachments, attachments);
    assert.deepEqual(send.imageFiles, imageFiles);
    assert.deepEqual(send.questionOrigin, { knowledge_base_id: "kb-1" });
    assert.deepEqual(send.mentionedItems, []);
  });

  it("sends empty selections as empty (nothing to preserve)", () => {
    const send = buildSuggestionSend({ query: "q", attachments: [], imageFiles: [] });
    assert.deepEqual(send.attachments, []);
    assert.deepEqual(send.imageFiles, []);
    assert.equal(send.questionOrigin, undefined);
  });
});

// REVIEW BLOCKER B-phase2a(1): attachment IDs and file names are private —
// they must NOT travel in the navigation URL (history, logs, shoulder
// surfing). They ride a one-shot same-tab sessionStorage record keyed by
// the new session id; the URL carries only the innocuous q/qokb/qok marker
// plus the fh flag. Runs with: node --experimental-strip-types --test lib/first-turn-handoff.test.ts
function memoryStore(impl?: Partial<Record<"getItem" | "setItem" | "removeItem", (...a: never[]) => never>>): HandoffStore {
  const data = new Map<string, string>();
  return {
    getItem: (k: string) => (data.has(k) ? data.get(k)! : null),
    setItem: (k: string, v: string) => {
      data.set(k, v);
    },
    removeItem: (k: string) => {
      data.delete(k);
    },
    ...(impl as object),
  } as HandoffStore;
}

describe("first-turn handoff privacy (no IDs/names in URL)", () => {
  it("buildFirstTurnSearch omits attachment IDs and names from the URL", () => {
    const qs = buildFirstTurnSearch({
      query: "summarize these",
      questionOrigin: { knowledge_base_id: "kb-1", knowledge_id: "doc-9" },
      hasAttachments: true,
    });
    assert.ok(!qs.includes("aids"), "URL must not contain the attachment-ID param");
    assert.ok(!qs.includes("an="), "URL must not contain the attachment-name param");
    assert.ok(!qs.includes("att-"), "URL must not contain raw attachment IDs");
    assert.ok(!qs.includes("secret-report.pdf"), "URL must not contain file names");
  });

  it("marks an expected file handoff with only the innocuous fh flag", () => {
    const withFiles = new URLSearchParams(
      buildFirstTurnSearch({ query: "q", hasAttachments: true }),
    );
    assert.equal(withFiles.get(FIRST_TURN_HANDOFF_FLAG_PARAM), "1");
    const plain = new URLSearchParams(buildFirstTurnSearch({ query: "q" }));
    assert.equal(plain.get(FIRST_TURN_HANDOFF_FLAG_PARAM), null);
    assert.equal(parseFirstTurnSearch(withFiles).expectsAttachments, true);
    assert.equal(parseFirstTurnSearch(plain).expectsAttachments, false);
  });

  it("keeps q/qokb/qok routing intact", () => {
    const parsed = parseFirstTurnSearch(
      new URLSearchParams(buildFirstTurnSearch({
        query: "hi",
        questionOrigin: { knowledge_base_id: "kb-1", knowledge_id: "doc-9" },
        hasAttachments: true,
      })),
    );
    assert.equal(parsed.query, "hi");
    assert.deepEqual(parsed.questionOrigin, { knowledge_base_id: "kb-1", knowledge_id: "doc-9" });
  });
});

describe("sessionStorage first-turn handoff (one-shot, session-keyed)", () => {
  const payload: FirstTurnHandoffPayload = {
    attachmentIds: ["att-1", "att-2"],
    attachmentNames: ["secret-report.pdf", "photo.png"],
  };

  it("round-trips IDs and names through the session-keyed record", () => {
    const store = memoryStore();
    saveFirstTurnHandoff(store, "sess-1", payload);
    assert.deepEqual(consumeFirstTurnHandoff(store, "sess-1"), payload);
  });

  it("consumes only once — the second take is empty (no replay)", () => {
    const store = memoryStore();
    saveFirstTurnHandoff(store, "sess-1", payload);
    assert.deepEqual(consumeFirstTurnHandoff(store, "sess-1"), payload);
    assert.equal(consumeFirstTurnHandoff(store, "sess-1"), null);
  });

  it("missing record consumes as null (expired handoff, other tab, reload)", () => {
    assert.equal(consumeFirstTurnHandoff(memoryStore(), "nope"), null);
  });

  it("isolates sessions — a mismatched id gets nothing and keeps the record", () => {
    const store = memoryStore();
    saveFirstTurnHandoff(store, "sess-A", payload);
    assert.equal(consumeFirstTurnHandoff(store, "sess-B"), null);
    assert.deepEqual(consumeFirstTurnHandoff(store, "sess-A"), payload);
  });

  it("keys records by session id (no cross-session leak)", () => {
    assert.ok(!firstTurnHandoffKey("sess-1").includes("att-1"));
    assert.notEqual(firstTurnHandoffKey("sess-1"), firstTurnHandoffKey("sess-2"));
    const store = memoryStore();
    saveFirstTurnHandoff(store, "sess-1", payload);
    saveFirstTurnHandoff(store, "sess-2", { attachmentIds: ["other"], attachmentNames: [] });
    assert.deepEqual(consumeFirstTurnHandoff(store, "sess-1")?.attachmentIds, ["att-1", "att-2"]);
    assert.deepEqual(consumeFirstTurnHandoff(store, "sess-2")?.attachmentIds, ["other"]);
  });

  it("treats a corrupt record as missing and clears it", () => {
    const store = memoryStore();
    store.setItem(firstTurnHandoffKey("sess-1"), "{not-json");
    assert.equal(consumeFirstTurnHandoff(store, "sess-1"), null);
    assert.equal(store.getItem(firstTurnHandoffKey("sess-1")), null);
  });

  it("save failure throws visibly so the caller stays instead of dropping files", () => {
    const failing = memoryStore({
      setItem: () => {
        throw new Error("storage unavailable");
      },
    });
    assert.throws(() => saveFirstTurnHandoff(failing, "sess-1", payload), /storage unavailable/);
  });
});

describe("resolveFirstTurnHandoff (chat route decision)", () => {
  const payload: FirstTurnHandoffPayload = {
    attachmentIds: ["att-1"],
    attachmentNames: ["a.pdf"],
  };

  it("resolves IDs+names+origin when the handoff is present", () => {
    const params = new URLSearchParams(
      buildFirstTurnSearch({
        query: "q",
        questionOrigin: { knowledge_base_id: "kb-1" },
        hasAttachments: true,
      }),
    );
    const r = resolveFirstTurnHandoff(params, payload);
    assert.equal(r.handoffError, null);
    assert.deepEqual(r.attachmentIds, ["att-1"]);
    assert.deepEqual(r.attachmentNames, ["a.pdf"]);
    assert.deepEqual(r.questionOrigin, { knowledge_base_id: "kb-1" });
  });

  it("flags an error (no auto-send) when files were expected but the handoff is gone", () => {
    const params = new URLSearchParams(buildFirstTurnSearch({ query: "q", hasAttachments: true }));
    const missing = resolveFirstTurnHandoff(params, null);
    assert.ok(typeof missing.handoffError === "string" && missing.handoffError.length > 0);
    assert.deepEqual(missing.attachmentIds, []);
    assert.equal(missing.query, "q");
  });

  it("plain questions without the flag resolve cleanly with no handoff", () => {
    const r = resolveFirstTurnHandoff(new URLSearchParams("q=hello"), null);
    assert.equal(r.handoffError, null);
    assert.equal(r.query, "hello");
    assert.deepEqual(r.attachmentIds, []);
  });

  it("no query means no first turn at all", () => {
    const r = resolveFirstTurnHandoff(new URLSearchParams(""), payload);
    assert.equal(r.query, null);
    assert.equal(r.handoffError, null);
  });
});

describe("cleanupSessionBestEffort (orphan session on failed first turn)", () => {
  it("deletes the orphan session", async () => {
    const deleted: string[] = [];
    await cleanupSessionBestEffort(async (id) => {
      deleted.push(id);
    }, "sess-orphan");
    assert.deepEqual(deleted, ["sess-orphan"]);
  });

  it("a cleanup failure never masks the upload error", async () => {
    await cleanupSessionBestEffort(async () => {
      throw new Error("delete failed");
    }, "sess-orphan");
  });
});

describe("phase2a: malformed-but-JSON-valid handoff fails closed when fh=1", () => {
  it("empty attachmentIds array with fh=1 reports handoffError (no auto-send)", () => {
    const store = memoryStore();
    store.setItem(firstTurnHandoffKey("sess-1"), JSON.stringify({ attachmentIds: [], attachmentNames: [] }));
    const handoff = consumeFirstTurnHandoff(store, "sess-1");
    const params = new URLSearchParams(buildFirstTurnSearch({ query: "q", hasAttachments: true }));
    const r = resolveFirstTurnHandoff(params, handoff);
    assert.ok(typeof r.handoffError === "string" && r.handoffError.length > 0, "must fail closed, not auto-send a file-less turn");
    assert.deepEqual(r.attachmentIds, []);
  });

  it("wrong-shape payload (attachmentIds not an array) with fh=1 reports handoffError", () => {
    const store = memoryStore();
    store.setItem(firstTurnHandoffKey("sess-1"), JSON.stringify({ attachmentIds: "att-1", attachmentNames: 42 }));
    const handoff = consumeFirstTurnHandoff(store, "sess-1");
    const params = new URLSearchParams(buildFirstTurnSearch({ query: "q", hasAttachments: true }));
    const r = resolveFirstTurnHandoff(params, handoff);
    assert.ok(typeof r.handoffError === "string" && r.handoffError.length > 0);
    assert.deepEqual(r.attachmentIds, []);
  });

  it("blank/whitespace IDs are dropped and an all-blank payload fails closed when fh=1", () => {
    const store = memoryStore();
    store.setItem(
      firstTurnHandoffKey("sess-1"),
      JSON.stringify({ attachmentIds: ["  ", ""], attachmentNames: ["a.pdf"] }),
    );
    const handoff = consumeFirstTurnHandoff(store, "sess-1");
    assert.deepEqual(handoff?.attachmentIds ?? [], [], "consume must validate nonempty ID strings");
    const params = new URLSearchParams(buildFirstTurnSearch({ query: "q", hasAttachments: true }));
    const r = resolveFirstTurnHandoff(params, handoff);
    assert.ok(typeof r.handoffError === "string" && r.handoffError.length > 0);
  });
});
