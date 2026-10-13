"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  LIVE_SPELLING_BATCH_PREFIX,
  normalizeAnchorText,
  opsBatchFromToolData,
  parsePluginParagraphsMessage,
  readParagraphsMessage,
  type OpsBatch,
  type OpsFailure,
} from "@/lib/api/document-ops";
import { reviewDocumentSpelling } from "@/lib/api/document-workspace";
import { planLiveSpelling, readReviewed, reviewedKeys, textKey, writeReviewed } from "@/lib/live-spelling";
import type { OnlyOfficeEditorHandle } from "./onlyoffice-editor";
import type { EditorOpsOutcome } from "./use-editor-ops";

// A review starts once the user has stopped typing for this long.
const LIVE_SPELL_DEBOUNCE_MS = 2_500;
// Safety net when no editor event arrives (e.g. edits by another user).
const LIVE_SPELL_POLL_MS = 15_000;
const READ_TIMEOUT_MS = 5_000;
// The server gives up on the model after 90 s; this is a little more.
const REVIEW_TIMEOUT_MS = 100_000;
// A paragraph whose underline failed to land is reviewed (from the server
// cache) and marked again at most this many times before it counts as done,
// so a mark the editor can never place does not repeat every poll.
const MAX_MARK_ATTEMPTS = 2;
// Older batches are already queued or applied (useEditorOps dedupes by id).
const KEEP_BATCHES = 50;

const randomId = () => Math.random().toString(36).slice(2) + Date.now().toString(36);

const storage = () => {
  try {
    return typeof window !== "undefined" ? window.localStorage : null;
  } catch {
    return null;
  }
};

/* Live spellcheck of one open document: after each pause in typing, reads
 * every paragraph's text from the editor plugin, sends the ones not
 * reviewed yet to the server, and returns the red-underline batches for
 * useEditorOps (an edited paragraph first loses its old underlines). Call
 * schedule() on editor events (document ready, state change). */
export function useLiveSpelling({
  sessionId,
  documentId,
  origin,
  editorRef,
  isReady,
  enabled,
}: {
  sessionId: string;
  documentId: string;
  origin: string | null;
  editorRef: React.RefObject<OnlyOfficeEditorHandle | null>;
  isReady: () => boolean;
  enabled: boolean;
}) {
  const [batches, setBatches] = useState<(OpsBatch & { rejected?: OpsFailure[] })[]>([]);
  const reviewedRef = useRef<Set<string>>(new Set());
  const firstPassRef = useRef(true);
  const busyRef = useRef(false);
  const againRef = useRef(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pendingReadRef = useRef<{ id: string; resolve: (texts: string[] | null) => void } | null>(null);
  // batches sent to the editor, by id: the hashes recorded once it applied them
  const awaitingRef = useRef(new Map<string, { docId: string; keys: string[]; opKeys: string[] }>());
  const attemptsRef = useRef(new Map<string, number>());
  // the server said the live spellcheck does not apply to this document
  const offRef = useRef(false);
  const live = useRef({ sessionId, documentId, origin, isReady, enabled });
  live.current = { sessionId, documentId, origin, isReady, enabled };

  // per document: the hashes already reviewed (survive a reload)
  useEffect(() => {
    const stored = readReviewed(storage(), documentId);
    reviewedRef.current = new Set(stored ?? []);
    firstPassRef.current = stored === null;
    awaitingRef.current.clear();
    attemptsRef.current.clear();
    offRef.current = false;
    setBatches([]);
  }, [documentId]);

  // the plugin's answers to read_paragraphs
  useEffect(() => {
    if (!origin) return;
    const onMessage = (event: MessageEvent) => {
      if (event.origin !== origin || !editorRef.current?.ownsMessageSource(event.source)) return;
      const msg = parsePluginParagraphsMessage(event.data);
      const pending = pendingReadRef.current;
      if (!msg || !pending || msg.requestId !== pending.id) return;
      pendingReadRef.current = null;
      pending.resolve(msg.texts);
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [origin, editorRef]);

  const readTexts = useCallback((): Promise<string[] | null> => {
    const target = live.current.origin;
    if (!target || !editorRef.current) return Promise.resolve(null);
    return new Promise((resolve) => {
      const id = randomId();
      const timer = setTimeout(() => {
        if (pendingReadRef.current?.id === id) pendingReadRef.current = null;
        resolve(null);
      }, READ_TIMEOUT_MS);
      pendingReadRef.current = {
        id,
        resolve: (texts) => {
          clearTimeout(timer);
          resolve(texts);
        },
      };
      if (!editorRef.current?.postToEditor(readParagraphsMessage(id), target)) {
        clearTimeout(timer);
        pendingReadRef.current = null;
        resolve(null);
      }
    });
  }, [editorRef]);

  const scheduleRef = useRef<(delay?: number) => void>(() => {});

  const markReviewed = useCallback((docId: string, keys: string[]) => {
    if (keys.length === 0 || live.current.documentId !== docId) return;
    for (const k of keys) {
      reviewedRef.current.add(k);
      attemptsRef.current.delete(k);
    }
    writeReviewed(storage(), docId, reviewedRef.current);
  }, []);

  /* The editor's result for a live batch: a paragraph counts as reviewed
   * once its underlines landed (or failed MAX_MARK_ATTEMPTS times); on a
   * timeout nothing is recorded and the next pass sends them again. */
  const onOutcome = useCallback(
    (o: EditorOpsOutcome) => {
      const sent = awaitingRef.current.get(o.batchId);
      if (!sent) return;
      awaitingRef.current.delete(o.batchId);
      if (o.kind === "timeout") return;
      const retry = new Set<string>();
      for (const f of o.failed) {
        const k = sent.opKeys[f.index];
        if (!k) continue;
        const n = (attemptsRef.current.get(k) ?? 0) + 1;
        attemptsRef.current.set(k, n);
        if (n < MAX_MARK_ATTEMPTS) retry.add(k);
      }
      markReviewed(
        sent.docId,
        sent.keys.filter((k) => !retry.has(k)),
      );
    },
    [markReviewed],
  );

  const run = useCallback(async () => {
    const { enabled: on, isReady: ready } = live.current;
    if (!on || !ready() || offRef.current) return;
    if (busyRef.current) {
      againRef.current = true;
      return;
    }
    busyRef.current = true;
    const docId = live.current.documentId;
    try {
      const texts = await readTexts();
      if (!texts || live.current.documentId !== docId) return;
      const plans = planLiveSpelling(texts, reviewedRef.current, firstPassRef.current);
      for (const plan of plans) {
        const res = await reviewDocumentSpelling(live.current.sessionId, docId, plan.paragraphs, {
          timeoutMs: REVIEW_TIMEOUT_MS,
        });
        if (live.current.documentId !== docId) return;
        if (res.off) {
          offRef.current = true; // not reviewed: nothing is recorded
          return;
        }
        if (res.failed) return; // the model is down: these stay unreviewed
        const keys = reviewedKeys(plan, res.missed);
        if (keys.length > 0) firstPassRef.current = false;
        const batch = opsBatchFromToolData("check_spelling", {
          document_ops: res.ops,
          ops_batch_id: LIVE_SPELLING_BATCH_PREFIX + randomId(),
          document_id: docId,
        });
        if (!batch || batch.ops.length === 0) {
          markReviewed(docId, keys);
          continue;
        }
        // recorded once the editor applied the underlines (onOutcome)
        const opKeys = batch.ops.map((op) => {
          const text = (op as { anchor?: { text?: unknown } }).anchor?.text;
          return typeof text === "string" ? textKey(normalizeAnchorText(text)) : "";
        });
        awaitingRef.current.set(batch.batchId, { docId, keys, opKeys });
        setBatches((prev) => [...prev.slice(-KEEP_BATCHES), batch]);
      }
    } catch {
      /* network / server error: the next pause tries again */
    } finally {
      busyRef.current = false;
      if (againRef.current) {
        againRef.current = false;
        scheduleRef.current();
      }
    }
  }, [readTexts]);

  const schedule = useCallback(
    (delay = LIVE_SPELL_DEBOUNCE_MS) => {
      if (timerRef.current) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => {
        timerRef.current = null;
        void run();
      }, delay);
    },
    [run],
  );
  scheduleRef.current = schedule;

  useEffect(() => {
    if (!enabled) return;
    const timer = setInterval(() => {
      if (typeof document !== "undefined" && document.visibilityState === "hidden") return;
      void run();
    }, LIVE_SPELL_POLL_MS);
    return () => clearInterval(timer);
  }, [enabled, run]);

  useEffect(
    () => () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    },
    [],
  );

  return { batches, schedule, onOutcome };
}
