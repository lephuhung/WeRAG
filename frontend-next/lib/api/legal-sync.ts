/* SuperAdmin monitor of the legal data workers: the nightly vbpl.vn sync
 * (container vbpl-sync: crawl → import → MinIO push), the backend's
 * auto-ingest and expired-retention workers, the queues they drain and the
 * push activity of the legal feeds (TVPL extension, Party documents).
 * Read only. Backend: /api/v1/system/legal-sync — SuperAdmin-only. */
import { apiGet, type ApiEnvelope } from "@/lib/api-client";

export type LegalSyncWorker = "vbpl_sync" | "auto_ingest" | "expired_retention";
export type LegalSyncRunStatus = "running" | "ok" | "failed";
export type LegalSyncHealth = "ok" | "unknown" | "warning" | "error";
export type LegalSyncReason =
  | "no_runs"
  | "last_failed"
  | "stale"
  | "running_long"
  | "no_heartbeat"
  | "starting"
  | "heartbeat_stale"
  | "tick_error"
  | "not_started"
  | "silent";

/** Timing of one step of a run (RFC 3339, UTC). */
export interface LegalSyncStepTiming {
  started_at?: string;
  finished_at?: string;
}

/** A run's summary: counts by step ("crawl", "import") or the worker's own
 * keys, the step timings under "steps", and for the sync its "schedule". */
export type LegalSyncSummary = Record<string, unknown> & {
  steps?: Record<string, LegalSyncStepTiming>;
  schedule?: string;
};

export interface LegalSyncRun {
  id: number;
  worker: LegalSyncWorker;
  status: LegalSyncRunStatus;
  step: string;
  started_at: string;
  finished_at: string | null;
  summary: LegalSyncSummary | null;
  error: string;
  /** last sign of life of the run */
  heartbeat_at?: string | null;
}

export interface LegalSyncWorkerStatus {
  worker: LegalSyncWorker;
  /** container: deploy/vbpl-sync, seen through its runs; backend: a
   * goroutine of the server, with a heartbeat. */
  kind: "container" | "backend";
  last: LegalSyncRun | null;
  last_ok: LegalSyncRun | null;
  /** while the sync runs: its previous finished run */
  last_finished?: LegalSyncRun | null;
  schedule?: string;
  interval_seconds?: number;
  last_tick_at?: string;
  last_error?: string;
  last_error_at?: string;
  /** a backend worker's passes have failed since then */
  failing_since?: string;
  health: LegalSyncHealth;
  reasons: LegalSyncReason[];
}

export interface LegalSyncQueues {
  auto_ingest: number;
  auto_ingest_retrying: number;
  requested: number;
  /** open requests by source: vbpl_no_text (the sync), dangcongsan_no_text,
   * feed:tvpl, catalogue (a user's question) … */
  requested_by_source: Record<string, number>;
  retention_days: number;
  retention_waiting: number;
  retention_due: number;
  retention_next_due?: string;
}

export interface LegalFeedActivity {
  feed: string;
  last_at: string | null;
  last_24h: number;
  last_7d: number;
  last_30d: number;
  /** push_full, push_links, push_catalogue, push_draft … over 7 days */
  by_action: Record<string, number>;
}

export interface LegalSyncStatus {
  now: string;
  /** the legal_sync_alert digest is kept (system setting legal.sync_alert.enabled) */
  alerts_enabled: boolean;
  workers: LegalSyncWorkerStatus[];
  queues: LegalSyncQueues;
  feeds: LegalFeedActivity[];
}

export function getLegalSyncStatus(): Promise<ApiEnvelope<LegalSyncStatus>> {
  return apiGet("/api/v1/system/legal-sync/status");
}

export function listLegalSyncRuns(params: {
  worker?: LegalSyncWorker | "";
  limit?: number;
}): Promise<ApiEnvelope<{ items: LegalSyncRun[] }>> {
  const q = new URLSearchParams();
  if (params.worker) q.set("worker", params.worker);
  if (params.limit) q.set("limit", String(params.limit));
  const qs = q.toString();
  return apiGet(`/api/v1/system/legal-sync/runs${qs ? `?${qs}` : ""}`);
}
