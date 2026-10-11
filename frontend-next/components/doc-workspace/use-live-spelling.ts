"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  LIVE_SPELLING_BATCH_PREFIX,
  opsBatchFromToolData,
  parsePluginParagraphsMessage,
  readParagraphsMessage,
  type OpsBatch,
  type OpsFailure,
} from "@/lib/api/document-ops";
import { reviewDocumentSpelling } from "@/lib/api/document-workspace";
import { planLiveSpelling, readReviewed, writeReviewed } from "@/lib/live-spelling";
import type { OnlyOfficeEditorHandle } from "./onlyoffice-editor";

// A review starts once the user has stopped typing for this long.
const LIVE_SPELL_DEBOUNCE_MS = 2_500;
// Safety net when no editor event arrives (e.g. edits by another user).
const LIVE_SPELL_POLL_MS = 15_000;
const READ_TIMEOUT_MS = 5_000;
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
  const live = useRef({ sessionId, documentId, origin, isReady, enabled });
  live.current = { sessionId, documentId, origin, isReady, enabled };

  // per document: the hashes already reviewed (survive a reload)
  useEffect(() => {
    const stored = readReviewed(storage(), documentId);
    reviewedRef.current = new Set(stored ?? []);
    firstPassRef.current = stored === null;
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

  const run = useCallback(async () => {
    const { enabled: on, isReady: ready } = live.current;
    if (!on || !ready()) return;
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
        const res = await reviewDocumentSpelling(live.current.sessionId, docId, plan.paragraphs);
        if (live.current.documentId !== docId) return;
        if (res.failed) return; // the model is down: these stay unreviewed
        for (const k of plan.keys) reviewedRef.current.add(k);
        firstPassRef.current = false;
        writeReviewed(storage(), docId, reviewedRef.current);
        const batch = opsBatchFromToolData("check_spelling", {
          document_ops: res.ops,
          ops_batch_id: LIVE_SPELLING_BATCH_PREFIX + randomId(),
          document_id: docId,
        });
        if (batch && batch.ops.length > 0) setBatches((prev) => [...prev.slice(-KEEP_BATCHES), batch]);
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

  return { batches, schedule };
}
