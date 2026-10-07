"use client";

import { useCallback, useEffect, useRef } from "react";
import {
  applyOpsMessage,
  parsePluginOpsMessage,
  readAppliedBatches,
  rememberAppliedBatch,
  type OpsBatch,
  type OpsFailure,
} from "@/lib/api/document-ops";
import type { OnlyOfficeEditorHandle } from "./onlyoffice-editor";

const RESULT_TIMEOUT_MS = 20_000;
const ACK_RETRY_MS = 3_000;

export type EditorOpsOutcome =
  | { kind: "result"; batchId: string; total: number; applied: number; failed: OpsFailure[] }
  | { kind: "timeout"; batchId: string; total: number };

/* Feeds AI edit plans to the editor plugin, one batch at a time.
 *
 * - `batches` is append-only per session; each batchId is sent at most once
 *   per mount and never again once it is in the per-session ledger
 *   (localStorage), so a stream replay / reload cannot apply it twice.
 * - Waits for the editor (`isReady()`, flipped by onDocumentReady) — call
 *   `pump()` from onDocumentReady to drain the queue.
 * - Re-posts every 3s until the plugin acks (it may start after the
 *   document), gives up after 20s without an ops_result. */
export function useEditorOps({
  sessionId,
  origin,
  editorRef,
  isReady,
  batches,
  onOutcome,
}: {
  sessionId: string | undefined;
  /** Document Server origin (postMessage target + accepted sender). */
  origin: string | null;
  editorRef: React.RefObject<OnlyOfficeEditorHandle | null>;
  isReady: () => boolean;
  batches: (OpsBatch & { rejected?: OpsFailure[] })[];
  onOutcome: (o: EditorOpsOutcome) => void;
}) {
  const seenRef = useRef<Set<string>>(new Set());
  const queueRef = useRef<(OpsBatch & { rejected?: OpsFailure[] })[]>([]);
  const activeRef = useRef<{
    batchId: string;
    acked: boolean;
    finish: (o: EditorOpsOutcome) => void;
  } | null>(null);
  const originRef = useRef(origin);
  originRef.current = origin;
  const isReadyRef = useRef(isReady);
  isReadyRef.current = isReady;
  const onOutcomeRef = useRef(onOutcome);
  onOutcomeRef.current = onOutcome;

  const storage = () => {
    try {
      return typeof window !== "undefined" ? window.localStorage : null;
    } catch {
      return null;
    }
  };

  // New session → fresh bookkeeping seeded from the ledger. On unmount the
  // active batch is dropped (its timers then no-op).
  useEffect(() => {
    seenRef.current = new Set(sessionId ? readAppliedBatches(storage(), sessionId) : []);
    queueRef.current = [];
    activeRef.current = null;
    return () => {
      activeRef.current = null;
    };
  }, [sessionId]);

  const pump = useCallback(() => {
    if (activeRef.current || queueRef.current.length === 0) return;
    const target = originRef.current;
    if (!target || !isReadyRef.current() || !editorRef.current) return;
    const batch = queueRef.current.shift()!;
    const rejected = batch.rejected ?? [];
    const total = batch.ops.length + rejected.length;
    let retry: ReturnType<typeof setInterval> | null = null;
    let timeout: ReturnType<typeof setTimeout> | null = null;
    const post = () => editorRef.current?.postToEditor(applyOpsMessage(batch), target);
    const finish = (o: EditorOpsOutcome) => {
      if (activeRef.current?.batchId !== batch.batchId) return;
      activeRef.current = null;
      if (retry) clearInterval(retry);
      if (timeout) clearTimeout(timeout);
      if (o.kind === "result" && sessionId) rememberAppliedBatch(storage(), sessionId, batch.batchId);
      onOutcomeRef.current(o);
      pump();
    };
    activeRef.current = { batchId: batch.batchId, acked: false, finish };
    if (batch.ops.length === 0) {
      finish({ kind: "result", batchId: batch.batchId, total, applied: 0, failed: rejected });
      return;
    }
    post();
    retry = setInterval(() => {
      if (activeRef.current?.batchId === batch.batchId && !activeRef.current.acked) post();
    }, ACK_RETRY_MS);
    timeout = setTimeout(() => finish({ kind: "timeout", batchId: batch.batchId, total }), RESULT_TIMEOUT_MS);
    // Fold the client-side rejections into the plugin's result.
    activeRef.current.finish = (o) =>
      finish(
        o.kind === "result"
          ? {
              ...o,
              total,
              // Rejected indices refer to the raw tool ops, plugin ones to
              // the validated list — only the messages are shown.
              failed: [...rejected, ...o.failed],
            }
          : o,
      );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId, editorRef]);

  // Enqueue unseen batches.
  useEffect(() => {
    let added = false;
    for (const b of batches) {
      if (seenRef.current.has(b.batchId)) continue;
      seenRef.current.add(b.batchId);
      queueRef.current.push(b);
      added = true;
    }
    if (added) pump();
  }, [batches, pump]);

  // Plugin replies.
  useEffect(() => {
    if (!origin) return;
    const onMessage = (event: MessageEvent) => {
      if (event.origin !== origin) return;
      const msg = parsePluginOpsMessage(event.data);
      const active = activeRef.current;
      if (!msg || !active || msg.batchId !== active.batchId) return;
      if (msg.type === "ops_ack") {
        // Ledger on receipt: a lost ops_result must not lead to a second
        // apply after a reload (the plugin is already running the batch).
        if (!active.acked && sessionId) rememberAppliedBatch(storage(), sessionId, msg.batchId);
        active.acked = true;
        return;
      }
      active.finish({ kind: "result", batchId: msg.batchId, total: 0, applied: msg.applied, failed: msg.failed });
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [origin, sessionId]);

  /** The document under the editor was replaced (restore / key change):
   * drop queued and in-flight batches so stale ops never land on it. They
   * stay "seen", so they are not queued again. */
  const reset = useCallback(() => {
    queueRef.current = [];
    activeRef.current = null; // its timers / late replies then no-op
  }, []);

  return { pump, reset };
}
