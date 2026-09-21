"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
} from "react";
import { batchQueryKnowledge, uploadKnowledgeFile } from "@/lib/api/knowledge";
import { useAuth } from "@/lib/auth";
import {
  createListRefreshThrottle,
  createUploadQueue,
  type KnowledgeStatusRow,
  type UploadBatch,
  type UploadPayload,
} from "@/lib/upload-queue";
import {
  summarizeItems,
  type UploadItem,
  type UploadSummary,
} from "@/lib/upload-tasks-state";

/**
 * Knowledge file uploads behind the floating progress panel
 * (components/upload-tasks/upload-tasks-panel.tsx). Lives in a provider rather
 * than the knowledge base page so uploads keep going, and stay visible, when
 * the user navigates elsewhere in the app.
 *
 * Ported from frontend/src/stores/uploadTasks.ts + uploadTasksCore.ts.
 */

/** Parallel transfers — same bound as the Vue store. */
const CONCURRENCY = 3;
const POLL_INTERVAL_MS = 3000;
/** Keeps the ids=… query string of one status request well under URL length limits. */
const POLL_CHUNK_SIZE = 40;
/** Minimum gap between mid-batch list refreshes pushed to an open page. */
const LIST_REFRESH_INTERVAL_MS = 2000;
/** Batch ends this close together produce one final refresh. */
const LIST_REFRESH_COALESCE_MS = 150;

export interface EnqueueUploadsInput {
  kbId: string;
  kbName: string;
  targetFolder?: string;
  tagIds?: string[];
  processConfig?: UploadBatch["processConfig"];
  uploads: UploadPayload[];
}

type UploadTasksContextValue = {
  items: UploadItem[];
  batches: UploadBatch[];
  summary: UploadSummary;
  visible: boolean;
  collapsed: boolean;
  batchById: Map<string, UploadBatch>;
  enqueue: (input: EnqueueUploadsInput) => void;
  cancelItem: (id: string) => void;
  cancelAll: () => void;
  retryItem: (id: string) => void;
  retryFailed: () => void;
  /** Hide the panel and forget every task, cancelling transfers still sending. */
  dismiss: () => void;
  toggleCollapsed: () => void;
};

const UploadTasksContext = createContext<UploadTasksContextValue | null>(null);

export function UploadTasksProvider({ children }: { children: React.ReactNode }) {
  const { user, selectedTenantId } = useAuth();

  // Mutated in place by the queue; `version` only marks that they changed.
  const itemsRef = useRef<UploadItem[]>([]);
  const batchesRef = useRef<UploadBatch[]>([]);
  const [version, bump] = useReducer((x: number) => x + 1, 0);
  const [visible, setVisible] = useState(false);
  const [collapsed, setCollapsed] = useState(false);

  const queueRef = useRef<ReturnType<typeof createUploadQueue> | null>(null);
  if (!queueRef.current) {
    queueRef.current = createUploadQueue(itemsRef.current, batchesRef.current, {
      concurrency: CONCURRENCY,
      pollIntervalMs: POLL_INTERVAL_MS,
      pollChunkSize: POLL_CHUNK_SIZE,
      upload: (batch, payload, onProgress, signal) =>
        uploadKnowledgeFile(
          batch.kbId,
          {
            file: payload.file,
            fileName: payload.fileName,
            tag_ids: batch.tagIds,
            process_config: batch.processConfig,
          },
          (percent) => onProgress(percent / 100),
          { signal },
        ),
      queryStatus: async (kbId, knowledgeIds) => {
        const query = knowledgeIds.map((id) => `ids=${encodeURIComponent(id)}`).join("&");
        try {
          const result = await batchQueryKnowledge(query, kbId);
          return result?.success && Array.isArray(result.data)
            ? (result.data as KnowledgeStatusRow[])
            : null;
        } catch {
          return null;
        }
      },
      onTransferEnd: createListRefreshThrottle((kbId, settled) => {
        window.dispatchEvent(new CustomEvent("knowledgeFileUploaded", { detail: { kbId, settled } }));
      }, LIST_REFRESH_INTERVAL_MS, LIST_REFRESH_COALESCE_MS),
      onChange: bump,
    });
  }
  const queue = queueRef.current;

  const enqueue = useCallback(
    (input: EnqueueUploadsInput) => {
      if (!input.kbId || input.uploads.length === 0) return;
      // Batches that finished cleanly have nothing left to show; ones with
      // failures stay so their files can still be retried.
      queue.pruneFinished();
      queue.add(
        {
          kbId: input.kbId,
          kbName: input.kbName,
          targetFolder: input.targetFolder || "",
          tagIds: input.tagIds && input.tagIds.length > 0 ? [...input.tagIds] : undefined,
          processConfig: input.processConfig,
        },
        input.uploads,
      );
      setVisible(true);
      setCollapsed(false);
    },
    [queue],
  );

  const dismiss = useCallback(() => {
    queue.clear();
    setVisible(false);
  }, [queue]);

  const toggleCollapsed = useCallback(() => setCollapsed((c) => !c), []);

  // Uploads belong to the account and workspace they were started in;
  // switching either drops the queue.
  const ownerKey = `${user?.id ?? ""}::${selectedTenantId ?? ""}`;
  const ownerRef = useRef(ownerKey);
  useEffect(() => {
    if (ownerRef.current === ownerKey) return;
    ownerRef.current = ownerKey;
    dismiss();
  }, [ownerKey, dismiss]);

  const value = useMemo<UploadTasksContextValue>(
    () => ({
      items: itemsRef.current,
      batches: batchesRef.current,
      summary: summarizeItems(itemsRef.current),
      visible,
      collapsed,
      batchById: new Map(batchesRef.current.map((batch) => [batch.id, batch])),
      enqueue,
      cancelItem: queue.cancel,
      cancelAll: queue.cancelAll,
      retryItem: queue.retry,
      retryFailed: queue.retryFailed,
      dismiss,
      toggleCollapsed,
    }),
    // version re-runs this memo after every queue mutation.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [version, visible, collapsed, enqueue, dismiss, toggleCollapsed, queue],
  );

  return <UploadTasksContext.Provider value={value}>{children}</UploadTasksContext.Provider>;
}

export function useUploadTasks(): UploadTasksContextValue {
  const ctx = useContext(UploadTasksContext);
  if (!ctx) throw new Error("useUploadTasks must be used inside <UploadTasksProvider>");
  return ctx;
}
