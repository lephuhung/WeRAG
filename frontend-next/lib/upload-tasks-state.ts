/**
 * Framework-free rules behind the knowledge upload queue (lib/upload-queue.ts
 * + lib/upload-tasks.tsx): how an item's transfer and parse states fold into
 * one display phase, how a set of items summarises into the panel header, and
 * which queued items may start next.
 *
 * Ported from frontend/src/stores/uploadTasksState.ts.
 */

/** Client-side life of one file's HTTP upload. */
export type TransferStatus = "queued" | "uploading" | "uploaded" | "failed" | "duplicate" | "cancelled";

export interface UploadItem {
  id: string;
  batchId: string;
  name: string;
  /** webkitRelativePath for folder uploads, '' otherwise. */
  relativePath: string;
  size: number;
  /** Bytes of the file body sent so far, scaled from the multipart progress. */
  loaded: number;
  transfer: TransferStatus;
  /** Transfer failure reason. */
  error?: string;
  /** Knowledge created by this upload, or the existing one for a duplicate. */
  knowledgeId?: string;
  /** Backend parse_status once uploaded; 'deleted' when the row disappeared. */
  parseStatus?: string;
  parseError?: string;
}

/**
 * One status per row, combining both stages. `saving` is the gap between the
 * last request byte leaving the browser and the response arriving, while the
 * server hashes and stores the file.
 */
export type ItemPhase =
  | "waiting"
  | "uploading"
  | "saving"
  | "parsing"
  | "ready"
  | "failed"
  | "duplicate"
  | "cancelled";

export type UploadStage = "uploading" | "parsing" | "done";

export function itemPhase(item: UploadItem): ItemPhase {
  switch (item.transfer) {
    case "queued":
      return "waiting";
    case "uploading":
      return item.size > 0 && item.loaded >= item.size ? "saving" : "uploading";
    case "failed":
      return "failed";
    case "duplicate":
      return "duplicate";
    case "cancelled":
      return "cancelled";
  }
  switch (item.parseStatus) {
    // finalizing means the document is already searchable; summary / question /
    // graph enrichment keeps running in the background.
    case "finalizing":
    case "completed":
      return "ready";
    case "failed":
      return "failed";
    case "cancelled":
    case "deleting":
    case "deleted":
      return "cancelled";
    default:
      return "parsing";
  }
}

/** Parse states the panel keeps polling. finalizing is polled so the row can settle on completed. */
export function needsParsePolling(item: UploadItem): boolean {
  if (item.transfer !== "uploaded" || !item.knowledgeId) return false;
  const status = item.parseStatus;
  return !status || status === "pending" || status === "processing" || status === "finalizing";
}

export interface UploadSummary {
  total: number;
  waiting: number;
  /** Includes `saving`. */
  uploading: number;
  parsing: number;
  ready: number;
  failed: number;
  duplicate: number;
  cancelled: number;
  /** Items whose transfer settled (uploaded, failed or duplicate). */
  transferSettled: number;
  /** Items still meant to be transferred (everything except transfer-cancelled). */
  transferTotal: number;
  /** Items that reached the server. */
  uploaded: number;
  totalBytes: number;
  /** Bytes no longer waiting to be sent: settled items in full plus in-flight progress. */
  loadedBytes: number;
  /**
   * Bytes that actually crossed the wire to a live or successful request. Unlike
   * loadedBytes it never jumps when a file fails, so it is what speed is measured on.
   */
  sentBytes: number;
  /** Failed or cancelled transfers that can be queued again. */
  retryable: number;
  stage: UploadStage;
  /** Stacked progress bar widths, each 0..1 of the non-cancelled items. */
  bar: { ready: number; active: number; failed: number; duplicate: number };
}

export function summarizeItems(items: readonly UploadItem[]): UploadSummary {
  const summary: UploadSummary = {
    total: items.length,
    waiting: 0,
    uploading: 0,
    parsing: 0,
    ready: 0,
    failed: 0,
    duplicate: 0,
    cancelled: 0,
    transferSettled: 0,
    transferTotal: 0,
    uploaded: 0,
    totalBytes: 0,
    loadedBytes: 0,
    sentBytes: 0,
    retryable: 0,
    stage: "done",
    bar: { ready: 0, active: 0, failed: 0, duplicate: 0 },
  };
  let inFlightUnits = 0;

  for (const item of items) {
    const phase = itemPhase(item);
    switch (phase) {
      case "waiting":
        summary.waiting++;
        break;
      case "uploading":
      case "saving":
        summary.uploading++;
        if (item.size > 0) inFlightUnits += Math.min(1, item.loaded / item.size);
        break;
      default:
        summary[phase]++;
    }

    if (item.transfer === "cancelled") {
      summary.retryable++;
      continue;
    }
    summary.transferTotal++;
    summary.totalBytes += item.size;
    if (item.transfer === "uploaded" || item.transfer === "failed" || item.transfer === "duplicate") {
      summary.transferSettled++;
      summary.loadedBytes += item.size;
      if (item.transfer !== "failed") summary.sentBytes += item.size;
    } else if (item.transfer === "uploading") {
      summary.loadedBytes += Math.min(item.loaded, item.size);
      summary.sentBytes += Math.min(item.loaded, item.size);
    }
    if (item.transfer === "uploaded") summary.uploaded++;
    if (item.transfer === "failed") summary.retryable++;
  }

  if (summary.waiting + summary.uploading > 0) summary.stage = "uploading";
  else if (summary.parsing > 0) summary.stage = "parsing";

  const denominator = summary.total - summary.cancelled;
  if (denominator > 0) {
    summary.bar = {
      ready: summary.ready / denominator,
      active: (summary.parsing + inFlightUnits) / denominator,
      failed: summary.failed / denominator,
      duplicate: summary.duplicate / denominator,
    };
  }
  return summary;
}

/**
 * Queued items to start now so that no more than `concurrency` transfers run
 * at once. Items sharing a `conflictKey` with a running or just-picked item
 * wait their turn, and the next free slot goes to a later item instead.
 */
export function pickNextToStart<T extends UploadItem>(
  items: readonly T[],
  concurrency: number,
  conflictKey?: (item: T) => string,
): T[] {
  let running = 0;
  const busyKeys = new Set<string>();
  for (const item of items) {
    if (item.transfer !== "uploading") continue;
    running++;
    if (conflictKey) busyKeys.add(conflictKey(item));
  }
  const picked: T[] = [];
  for (const item of items) {
    if (running >= concurrency) break;
    if (item.transfer !== "queued") continue;
    const key = conflictKey?.(item);
    if (key !== undefined) {
      if (busyKeys.has(key)) continue;
      busyKeys.add(key);
    }
    picked.push(item);
    running++;
  }
  return picked;
}

export type UploadOutcome =
  | { kind: "uploaded"; knowledgeId?: string; parseStatus?: string }
  | { kind: "duplicate"; knowledgeId?: string; message?: string }
  | { kind: "failed"; message?: string };

type AnyBody = {
  code?: string;
  message?: string;
  data?: { id?: string; parse_status?: string };
  error?: { code?: string; message?: string } | string;
  success?: boolean;
  payload?: AnyBody;
};

/**
 * Classify a resolved or rejected upload call. api-client rejects non-2xx
 * responses with ApiError carrying the JSON body on `.payload`, so a 409
 * duplicate arrives as a rejection whose payload has `code` and the existing
 * row in `data`.
 */
export function classifyUploadResult(result: unknown): UploadOutcome {
  const body = (result ?? undefined) as AnyBody | undefined;
  const inner = body?.payload ?? body;
  const code =
    inner?.code ?? (typeof inner?.error === "object" ? inner.error.code : undefined);
  if (code === "duplicate_file") {
    return { kind: "duplicate", knowledgeId: inner?.data?.id, message: inner?.message };
  }
  if (inner?.success === true) {
    return { kind: "uploaded", knowledgeId: inner?.data?.id, parseStatus: inner?.data?.parse_status };
  }
  const message =
    (typeof inner?.error === "object" ? inner.error.message : undefined) ??
    (typeof inner?.error === "string" ? inner.error : undefined) ??
    inner?.message ??
    (result instanceof Error ? result.message : undefined);
  return { kind: "failed", message };
}

export interface RateSample {
  at: number;
  bytes: number;
}

/**
 * Bytes per second over the samples inside `windowMs` of the newest one.
 * Returns 0 until the window spans at least one second, so a first burst of
 * buffered progress events doesn't produce a wildly optimistic ETA.
 */
export function estimateRate(samples: readonly RateSample[], windowMs = 8000): number {
  if (samples.length < 2) return 0;
  const newest = samples[samples.length - 1];
  const oldest = samples.find((sample) => newest.at - sample.at <= windowMs) ?? newest;
  const elapsed = newest.at - oldest.at;
  if (elapsed < 1000) return 0;
  return Math.max(0, (newest.bytes - oldest.bytes) / (elapsed / 1000));
}

/** Remaining seconds, or null when there is no usable rate yet. */
export function estimateRemainingSeconds(remainingBytes: number, bytesPerSecond: number): number | null {
  if (bytesPerSecond <= 0 || remainingBytes <= 0) return null;
  return Math.ceil(remainingBytes / bytesPerSecond);
}

/**
 * The coarse unit an ETA is read in. Seconds round up to 5 so the number
 * doesn't flicker every tick; hours keep one decimal.
 */
export function splitDuration(seconds: number): { unit: "seconds" | "minutes" | "hours"; value: number } {
  if (seconds <= 55) return { unit: "seconds", value: Math.max(5, Math.ceil(seconds / 5) * 5) };
  if (seconds < 3600) return { unit: "minutes", value: Math.ceil(seconds / 60) };
  return { unit: "hours", value: Math.round(seconds / 360) / 10 };
}
