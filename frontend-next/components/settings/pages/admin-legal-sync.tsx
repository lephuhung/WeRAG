"use client";

/* SuperAdmin monitor of the legal data workers, the "Data collection" tab of
 * the legal documents page (admin-legal-documents.tsx): the nightly vbpl.vn
 * sync, the auto-ingest and expired-retention workers (health, last run),
 * the queues they drain, the push activity of the legal feeds, and the run
 * history with each run's counts and error. Read only; refreshes itself.
 * Backend: /api/v1/system/legal-sync — SuperAdmin-only. */

import { useCallback, useEffect, useRef, useState } from "react";
import { IconRefresh } from "@/components/icons";
import { ConsolePage } from "@/components/settings/console-page";
import { Pill, PillGroup } from "@/components/settings/pill";
import { Notice } from "@/components/settings/notice";
import { useT, type LocaleKey } from "@/lib/i18n";
import { ApiError } from "@/lib/api-client";
import { formatDateTime } from "@/lib/datetime";
import {
  getLegalSyncStatus,
  listLegalSyncRuns,
  type LegalFeedActivity,
  type LegalSyncHealth,
  type LegalSyncRun,
  type LegalSyncRunStatus,
  type LegalSyncStatus,
  type LegalSyncStepTiming,
  type LegalSyncWorker,
  type LegalSyncWorkerStatus,
} from "@/lib/api/legal-sync";

const POLL_MS = 30_000;
const RUNS_LIMIT = 30;

const WORKER_LABEL: Record<LegalSyncWorker, LocaleKey> = {
  vbpl_sync: "lsync.worker.vbpl_sync",
  auto_ingest: "lsync.worker.auto_ingest",
  expired_retention: "lsync.worker.expired_retention",
};
const WORKER_DESC: Record<LegalSyncWorker, LocaleKey> = {
  vbpl_sync: "lsync.workerDesc.vbpl_sync",
  auto_ingest: "lsync.workerDesc.auto_ingest",
  expired_retention: "lsync.workerDesc.expired_retention",
};
const HEALTH_LABEL: Record<LegalSyncHealth, LocaleKey> = {
  ok: "lsync.health.ok",
  unknown: "lsync.health.unknown",
  warning: "lsync.health.warning",
  error: "lsync.health.error",
};
const STATUS_LABEL: Record<LegalSyncRunStatus, LocaleKey> = {
  running: "lsync.status.running",
  ok: "lsync.status.ok",
  failed: "lsync.status.failed",
};
const REASON_LABEL: Record<string, LocaleKey> = {
  no_runs: "lsync.reason.no_runs",
  last_failed: "lsync.reason.last_failed",
  stale: "lsync.reason.stale",
  running_long: "lsync.reason.running_long",
  no_heartbeat: "lsync.reason.no_heartbeat",
  starting: "lsync.reason.starting",
  heartbeat_stale: "lsync.reason.heartbeat_stale",
  tick_error: "lsync.reason.tick_error",
};
const STEP_LABEL: Record<string, LocaleKey> = {
  prepare: "lsync.step.prepare",
  crawl: "lsync.step.crawl",
  import: "lsync.step.import",
  push: "lsync.step.push",
};
/** Summary groups shown as sections of a run's details, in order. */
const GROUP_LABEL: Record<string, LocaleKey> = {
  crawl: "lsync.step.crawl",
  import: "lsync.step.import",
};
/** Labels of the known counts; an unknown key shows as is. */
const COUNT_LABEL: Record<string, LocaleKey> = {
  // crawl
  to_fetch: "lsync.count.to_fetch",
  fetched: "lsync.count.fetched",
  fetch_failed: "lsync.count.fetch_failed",
  requests: "lsync.count.requests",
  exported_documents: "lsync.count.exported_documents",
  exported_metadata_only: "lsync.count.exported_metadata_only",
  exported_relations: "lsync.count.exported_relations",
  kept: "lsync.count.kept",
  up_to_date: "lsync.count.up_to_date",
  last_fetch_failed: "lsync.count.last_fetch_failed",
  // import
  snapshot: "lsync.count.snapshot",
  documents: "lsync.count.documents",
  relations: "lsync.count.relations",
  relations_skipped: "lsync.count.relations_skipped",
  list_only: "lsync.count.list_only",
  twins_attached: "lsync.count.twins_attached",
  reviewed_updated_from_portal: "lsync.count.reviewed_updated_from_portal",
  pruned: "lsync.count.pruned",
  list_only_validity_updated: "lsync.count.list_only_validity_updated",
  knowledge_validity_updated: "lsync.count.knowledge_validity_updated",
  knowledge_linked: "lsync.count.knowledge_linked",
  queued_no_full_text: "lsync.count.queued_no_full_text",
  queued_auto_ingest: "lsync.count.queued_auto_ingest",
  requests_moved_to_auto_ingest: "lsync.count.requests_moved_to_auto_ingest",
  queued_text_changed: "lsync.count.queued_text_changed",
  warnings: "lsync.count.warnings",
  // auto-ingest
  tried: "lsync.count.tried",
  added: "lsync.count.added",
  rejected: "lsync.count.rejected",
  returned: "lsync.count.returned",
  retry: "lsync.count.retry",
  skipped: "lsync.count.skipped",
  // expired retention
  removed: "lsync.count.removed",
  clocks_started: "lsync.count.clocks_started",
  clocks_cleared: "lsync.count.clocks_cleared",
  failed: "lsync.count.failed",
  kept_live_copy: "lsync.count.kept_live_copy",
};
const REQUEST_SOURCE_LABEL: Record<string, LocaleKey> = {
  vbpl_no_text: "lsync.reqSource.vbpl_no_text",
  dangcongsan_no_text: "lsync.reqSource.dangcongsan_no_text",
  "feed:tvpl": "lsync.reqSource.feed_tvpl",
  catalogue: "lsync.reqSource.catalogue",
};
const FEED_LABEL: Record<string, LocaleKey> = {
  tvpl: "lsync.feed.tvpl",
  dangcongsan: "lsync.feed.dangcongsan",
};
const ACTION_LABEL: Record<string, LocaleKey> = {
  push_full: "lsync.action.push_full",
  push_links: "lsync.action.push_links",
  push_catalogue: "lsync.action.push_catalogue",
  push_draft: "lsync.action.push_draft",
};

const RUN_FILTERS: { key: LegalSyncWorker | ""; labelKey: LocaleKey }[] = [
  { key: "", labelKey: "lsync.filter.all" },
  { key: "vbpl_sync", labelKey: "lsync.worker.vbpl_sync" },
  { key: "auto_ingest", labelKey: "lsync.worker.auto_ingest" },
  { key: "expired_retention", labelKey: "lsync.worker.expired_retention" },
];

/** Text colour of a health's reasons. */
function healthText(h: LegalSyncHealth): string {
  switch (h) {
    case "error":
      return "text-error";
    case "warning":
      return "text-warning";
    default:
      return "text-muted";
  }
}

function healthTone(h: LegalSyncHealth): string {
  switch (h) {
    case "ok":
      return "badge-success";
    case "warning":
      return "badge-warning";
    case "error":
      return "badge-error";
    default:
      return "badge-neutral";
  }
}

function statusTone(s: LegalSyncRunStatus): string {
  switch (s) {
    case "ok":
      return "badge-success";
    case "failed":
      return "badge-error";
    default:
      return "badge-info";
  }
}

type T = ReturnType<typeof useT>["t"];

/** "42 giây", "5 phút", "1 giờ 5 phút". */
function formatDuration(t: T, ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return t("lsync.dur.s", { s });
  const m = Math.floor(s / 60);
  if (m < 60) return t("lsync.dur.m", { m });
  return t("lsync.dur.hm", { h: Math.floor(m / 60), m: m % 60 });
}

function runDuration(t: T, run: LegalSyncRun, now: number): string {
  const start = Date.parse(run.started_at);
  const end = run.finished_at ? Date.parse(run.finished_at) : now;
  return Number.isFinite(start) && Number.isFinite(end) ? formatDuration(t, end - start) : "";
}

/** A step still open in a finished run (an interrupted sync) ends with the run. */
function stepDuration(t: T, timing: LegalSyncStepTiming, run: LegalSyncRun, now: number): string {
  const start = timing.started_at ? Date.parse(timing.started_at) : NaN;
  const endAt = timing.finished_at ?? run.finished_at;
  const end = endAt ? Date.parse(endAt) : now;
  return Number.isFinite(start) && Number.isFinite(end) ? formatDuration(t, end - start) : "";
}

function label(t: T, map: Record<string, LocaleKey>, key: string): string {
  const k = map[key];
  return k ? t(k) : key;
}

/** Numbers in the page's language. */
function fmtNum(n: number, locale: string): string {
  return n.toLocaleString(locale === "vi" ? "vi-VN" : "en-US");
}

function formatValue(v: unknown, locale: string): string {
  if (typeof v === "number") return fmtNum(v, locale);
  if (Array.isArray(v)) return v.map(String).join("; ");
  if (v && typeof v === "object") return JSON.stringify(v);
  return String(v ?? "");
}

export default function LegalSyncSection({ onDenied }: { onDenied?: () => void }) {
  const { t, locale } = useT();
  const [status, setStatus] = useState<LegalSyncStatus | null>(null);
  const [runs, setRuns] = useState<LegalSyncRun[]>([]);
  // the nightly sync by default: the backend workers' passes come every few minutes
  const [filter, setFilter] = useState<LegalSyncWorker | "">("vbpl_sync");
  const seq = useRef(0);
  const inFlight = useRef(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [loadedAt, setLoadedAt] = useState<number>(0);

  const load = useCallback(
    async (quiet = false) => {
      if (quiet && inFlight.current > 0) return; // a load is on its way already
      const mine = ++seq.current; // an older answer arriving late is dropped
      inFlight.current++;
      if (!quiet) setLoading(true);
      try {
        const [st, rs] = await Promise.all([
          getLegalSyncStatus(),
          listLegalSyncRuns({ worker: filter, limit: RUNS_LIMIT }),
        ]);
        if (mine !== seq.current) return;
        setStatus(st.data ?? null);
        setRuns(rs.data?.items ?? []);
        setLoadedAt(Date.now());
        setError("");
      } catch (e) {
        if (mine !== seq.current) return;
        if (e instanceof ApiError && e.status === 403) onDenied?.();
        else setError(e instanceof Error && e.message ? e.message : t("lsync.loadFailed"));
      } finally {
        inFlight.current--;
        if (!quiet && mine === seq.current) setLoading(false);
      }
    },
    [filter, t, onDenied],
  );

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    const timer = setInterval(() => {
      if (document.visibilityState !== "hidden") void load(true);
    }, POLL_MS);
    return () => clearInterval(timer);
  }, [load]);

  const now = status ? Date.parse(status.now) || loadedAt : loadedAt;

  return (
    <ConsolePage size="wide">
      <div className="mb-5 flex flex-wrap items-center gap-3">
        <span className="caption text-muted">{t("lsync.desc")}</span>
        <div className="ml-auto flex items-center gap-2">
          {loadedAt > 0 && (
            <span className="caption text-muted">
              {t("lsync.updatedAt", { time: formatDateTime(loadedAt, locale) })}
            </span>
          )}
          <button
            className="btn btn-outline btn-sm h-8 w-8 p-0"
            onClick={() => void load()}
            disabled={loading}
            title={t("common.refresh")}
            aria-label={t("common.refresh")}
          >
            <IconRefresh className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
          </button>
        </div>
      </div>

      {error && (
        <Notice
          tone="error"
          className="mb-3"
          action={
            <button type="button" className="btn btn-outline btn-sm" onClick={() => void load()}>
              {t("common.retry")}
            </button>
          }
        >
          {error}
        </Notice>
      )}

      {status && (
        <>
          <div className="mb-6 grid gap-3 lg:grid-cols-3">
            {status.workers.map((w) => (
              <WorkerCard key={w.worker} w={w} now={now} t={t} locale={locale} />
            ))}
          </div>

          <div className="mb-6 grid gap-3 lg:grid-cols-2">
            <QueuesCard status={status} t={t} locale={locale} />
            <FeedsCard feeds={status.feeds} t={t} locale={locale} />
          </div>
        </>
      )}

      <h3 className="mb-3 text-sm font-semibold">{t("lsync.history")}</h3>
      <PillGroup ariaLabel={t("common.filters")} className="mb-4 flex-wrap">
        {RUN_FILTERS.map((f) => (
          <Pill key={f.key || "all"} active={filter === f.key} onClick={() => setFilter(f.key)}>
            {t(f.labelKey)}
          </Pill>
        ))}
      </PillGroup>
      {runs.length === 0 && !loading ? (
        <p className="caption text-muted">{t("lsync.noRuns")}</p>
      ) : (
        <div className="divide-y divide-hairline rounded-lg border border-hairline">
          {runs.map((r) => (
            <RunRow key={r.id} run={r} now={now} t={t} locale={locale} />
          ))}
        </div>
      )}
    </ConsolePage>
  );
}

function WorkerCard({ w, now, t, locale }: { w: LegalSyncWorkerStatus; now: number; t: T; locale: string }) {
  const last = w.last;
  const name = label(t, WORKER_LABEL, w.worker);
  return (
    <section className="rounded-lg border border-hairline px-4 py-3" aria-label={name}>
      <div className="mb-1 flex items-center gap-2">
        <h3 className="text-sm font-semibold">{name}</h3>
        <span className={`badge-pill ${healthTone(w.health)} ml-auto text-[10.5px]`}>
          {label(t, HEALTH_LABEL, w.health)}
        </span>
      </div>
      {WORKER_DESC[w.worker] && <p className="caption mb-2 text-muted">{t(WORKER_DESC[w.worker])}</p>}
      {w.reasons.length > 0 && (
        <ul className={`mb-2 space-y-0.5 text-[12.5px] ${healthText(w.health)}`}>
          {w.reasons.map((r) => (
            <li key={r}>{label(t, REASON_LABEL, r)}</li>
          ))}
        </ul>
      )}
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-[12.5px]">
        {last ? (
          <>
            <dt className="text-muted">{t("lsync.lastRun")}</dt>
            <dd className="flex flex-wrap items-center gap-1.5">
              <span className={`badge-pill ${statusTone(last.status)} text-[10.5px]`}>
                {label(t, STATUS_LABEL, last.status)}
              </span>
              <span>{formatDateTime(last.started_at, locale)}</span>
              <span className="text-muted">· {runDuration(t, last, now)}</span>
            </dd>
            {last.status !== "ok" && last.step && (
              <>
                <dt className="text-muted">{t("lsync.step")}</dt>
                <dd>{label(t, STEP_LABEL, last.step)}</dd>
              </>
            )}
          </>
        ) : (
          <>
            <dt className="text-muted">{t("lsync.lastRun")}</dt>
            <dd className="text-muted">{t("lsync.never")}</dd>
          </>
        )}
        {w.last_ok && w.last_ok.id !== last?.id && (
          <>
            <dt className="text-muted">{t("lsync.lastOk")}</dt>
            <dd>{formatDateTime(w.last_ok.finished_at ?? w.last_ok.started_at, locale)}</dd>
          </>
        )}
        {w.kind === "container" ? (
          <>
            <dt className="text-muted">{t("lsync.schedule")}</dt>
            <dd>
              <code className="text-[12px]">{w.schedule || "—"}</code>
            </dd>
          </>
        ) : (
          <>
            <dt className="text-muted">{t("lsync.every")}</dt>
            <dd>{formatDuration(t, (w.interval_seconds ?? 0) * 1000)}</dd>
            <dt className="text-muted">{t("lsync.lastTick")}</dt>
            <dd>{w.last_tick_at ? formatDateTime(w.last_tick_at, locale) : "—"}</dd>
          </>
        )}
      </dl>
      {(w.last_error || (last?.status === "failed" && last.error)) && (
        <details className="mt-2 text-[12px]">
          <summary className="cursor-pointer text-error">{t("lsync.lastError")}</summary>
          <pre className="mt-1 max-h-48 overflow-auto whitespace-pre-wrap break-words rounded bg-surface-card/70 p-2 text-[11.5px]">
            {w.last_error || last?.error}
          </pre>
        </details>
      )}
    </section>
  );
}

function QueuesCard({ status, t, locale }: { status: LegalSyncStatus; t: T; locale: string }) {
  const q = status.queues;
  const rows: { label: string; value: number; hint?: string }[] = [
    {
      label: t("lsync.queue.autoIngest"),
      value: q.auto_ingest,
      hint: q.auto_ingest_retrying > 0 ? t("lsync.queue.retrying", { n: q.auto_ingest_retrying }) : undefined,
    },
    {
      label: t("lsync.queue.requested"),
      value: q.requested,
      hint:
        Object.entries(q.requested_by_source ?? {})
          .filter(([, n]) => n > 0)
          .sort((a, b) => b[1] - a[1])
          .map(([src, n]) => `${label(t, REQUEST_SOURCE_LABEL, src)} ${fmtNum(n, locale)}`)
          .join(" · ") || undefined,
    },
    {
      label: t("lsync.queue.retention", { days: q.retention_days }),
      value: q.retention_waiting,
      hint:
        q.retention_due > 0
          ? t("lsync.queue.retentionDue", { n: q.retention_due })
          : q.retention_next_due
            ? t("lsync.queue.retentionNext", { time: formatDateTime(q.retention_next_due, locale) })
            : undefined,
    },
  ];
  return (
    <section className="rounded-lg border border-hairline px-4 py-3">
      <h3 className="mb-2 text-sm font-semibold">{t("lsync.queues")}</h3>
      <ul className="divide-y divide-hairline">
        {rows.map((r) => (
          <li key={r.label} className="flex items-baseline gap-3 py-1.5 text-[13px]">
            <span className="min-w-0 flex-1">
              {r.label}
              {r.hint && <span className="caption ml-1.5 text-muted">({r.hint})</span>}
            </span>
            <span className="font-semibold tabular-nums">{fmtNum(r.value, locale)}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}

function FeedsCard({ feeds, t, locale }: { feeds: LegalFeedActivity[]; t: T; locale: string }) {
  return (
    <section className="rounded-lg border border-hairline px-4 py-3">
      <h3 className="mb-2 text-sm font-semibold">{t("lsync.feeds")}</h3>
      {feeds.length === 0 ? (
        <p className="caption text-muted">{t("lsync.noFeeds")}</p>
      ) : (
        <table className="w-full text-[12.5px]">
          <thead>
            <tr className="text-left text-muted">
              <th className="py-1 font-normal">{t("lsync.feedCol.feed")}</th>
              <th className="py-1 font-normal">{t("lsync.feedCol.last")}</th>
              <th className="py-1 text-right font-normal">24h</th>
              <th className="py-1 text-right font-normal">{t("lsync.feedCol.7d")}</th>
              <th className="py-1 text-right font-normal">{t("lsync.feedCol.30d")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-hairline">
            {feeds.map((f) => (
              <tr key={f.feed} className="align-top">
                <td className="py-1.5">
                  <div className="font-medium">{label(t, FEED_LABEL, f.feed)}</div>
                  {Object.keys(f.by_action).length > 0 && (
                    <div className="caption text-muted">
                      {Object.entries(f.by_action)
                        .sort((a, b) => b[1] - a[1])
                        .map(([a, n]) => `${label(t, ACTION_LABEL, a)} ${fmtNum(n, locale)}`)
                        .join(" · ")}
                    </div>
                  )}
                </td>
                <td className="py-1.5">{f.last_at ? formatDateTime(f.last_at, locale) : "—"}</td>
                <td className="py-1.5 text-right tabular-nums">{fmtNum(f.last_24h, locale)}</td>
                <td className="py-1.5 text-right tabular-nums">{fmtNum(f.last_7d, locale)}</td>
                <td className="py-1.5 text-right tabular-nums">{fmtNum(f.last_30d, locale)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

function RunRow({ run, now, t, locale }: { run: LegalSyncRun; now: number; t: T; locale: string }) {
  const summary = run.summary ?? {};
  // Go writes the map sorted by name: show the steps in the order they ran
  const steps = Object.entries(summary.steps ?? {}).sort(([, a], [, b]) =>
    (a.started_at ?? "").localeCompare(b.started_at ?? ""),
  );
  const groups = Object.keys(GROUP_LABEL).filter((g) => summary[g] && typeof summary[g] === "object");
  const flat = Object.entries(summary).filter(
    ([k, v]) => k !== "steps" && k !== "schedule" && !(k in GROUP_LABEL && v && typeof v === "object"),
  );
  return (
    <details className="group px-3 py-2">
      <summary className="flex cursor-pointer flex-wrap items-center gap-2 text-[13px]">
        <span className={`badge-pill ${statusTone(run.status)} text-[10.5px]`}>{label(t, STATUS_LABEL, run.status)}</span>
        <span className="font-medium">{label(t, WORKER_LABEL, run.worker)}</span>
        <span>{formatDateTime(run.started_at, locale)}</span>
        <span className="text-muted">· {runDuration(t, run, now)}</span>
        {run.status !== "ok" && run.step && (
          <span className="caption text-muted">· {label(t, STEP_LABEL, run.step)}</span>
        )}
        <span className="caption ml-auto text-muted">{runHeadline(t, run)}</span>
      </summary>
      <div className="mt-2 space-y-3 pl-1 text-[12.5px]">
        {steps.length > 0 && (
          <div className="flex flex-wrap gap-x-4 gap-y-1">
            {steps.map(([s, timing]) => (
              <span key={s}>
                <span className="text-muted">{label(t, STEP_LABEL, s)}:</span> {stepDuration(t, timing, run, now)}
              </span>
            ))}
          </div>
        )}
        {groups.map((g) => (
          <CountGrid
            key={g}
            title={t(GROUP_LABEL[g])}
            counts={summary[g] as Record<string, unknown>}
            t={t}
            locale={locale}
          />
        ))}
        {flat.length > 0 && <CountGrid counts={Object.fromEntries(flat)} t={t} locale={locale} />}
        {run.error && (
          <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded bg-surface-card/70 p-2 text-[11.5px]">
            {run.error}
          </pre>
        )}
      </div>
    </details>
  );
}

/** The one-line gist of a run in the history. */
function runHeadline(t: T, run: LegalSyncRun): string {
  const s = run.summary ?? {};
  const num = (v: unknown) => (typeof v === "number" ? v : 0);
  if (run.worker === "vbpl_sync") {
    const crawl = (s.crawl ?? {}) as Record<string, unknown>;
    const imp = (s.import ?? {}) as Record<string, unknown>;
    if (!s.crawl && !s.import) return "";
    return t("lsync.headline.vbpl", {
      fetched: num(crawl.fetched),
      queued: num(imp.queued_auto_ingest) + num(imp.queued_text_changed),
      noText: num(imp.queued_no_full_text),
    });
  }
  if (run.worker === "auto_ingest") return t("lsync.headline.autoIngest", { added: num(s.added), tried: num(s.tried) });
  return t("lsync.headline.retention", { removed: num(s.removed), started: num(s.clocks_started) });
}

function CountGrid({
  title,
  counts,
  t,
  locale,
}: {
  title?: string;
  counts: Record<string, unknown>;
  t: T;
  locale: string;
}) {
  const entries = Object.entries(counts);
  if (entries.length === 0) return null;
  return (
    <div>
      {title && <div className="mb-1 font-medium">{title}</div>}
      <dl className="grid grid-cols-1 gap-x-6 gap-y-0.5 sm:grid-cols-2 xl:grid-cols-3">
        {entries.map(([k, v]) => (
          <div key={k} className="flex gap-2">
            <dt className="min-w-0 flex-1 text-muted">{label(t, COUNT_LABEL, k)}</dt>
            <dd className="max-w-[60%] break-words text-right tabular-nums">{formatValue(v, locale)}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}
