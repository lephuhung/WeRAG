/* SystemAdmin dashboard — platform-wide counters on the /platform/admin
 * page: chat volume and answer ratings over the last 24h, documents per
 * platform-public knowledge base, the live ingest pipeline, task queues,
 * account totals, message heatmap and build info. The pipeline list polls
 * faster than the aggregate stats; both pause while the tab is hidden. */
"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { ApiError } from "@/lib/api-client";
import { useT, type LocaleKey } from "@/lib/i18n";
import { RequireSystemAccess } from "@/components/require-system-access";
import {
  getProcessingDocuments,
  getRuntimeQueues,
  getSystemInfo,
  getSystemStats,
  type ProcessingDocuments,
  type PublicKBDocumentCount,
  type RuntimeQueuesResponse,
  type SystemInfo,
  type SystemStats,
} from "@/lib/api/system";
import { formatTokens } from "@/lib/api/usage";
import { MessageHeatmap } from "@/components/system/message-heatmap";
import { IconRefresh } from "@/components/icons";

const STATS_REFRESH_MS = 60_000;
const PIPELINE_REFRESH_MS = 10_000;

/* Same status colors as the documents-by-status bars; every use is paired
 * with a text label or count so color never carries meaning alone. */
const COLOR_DONE = "rgb(22, 163, 74)";
const COLOR_ACTIVE = "rgb(59, 130, 246)";
const COLOR_FAILED = "rgb(220, 38, 38)";

const DOC_STATUS_COLORS: Record<string, string> = {
  completed: COLOR_DONE,
  processing: COLOR_ACTIVE,
  finalizing: COLOR_ACTIVE,
  pending: "var(--color-muted-soft)",
  failed: COLOR_FAILED,
  deleting: "var(--color-muted-soft)",
  cancelled: "var(--color-muted-soft)",
};

const PIPELINE_STATUS_KEYS: Record<string, LocaleKey> = {
  pending: "dash.statusPending",
  processing: "dash.statusProcessing",
  finalizing: "dash.statusFinalizing",
};

function formatDuration(seconds?: number): string {
  if (seconds === undefined || !Number.isFinite(seconds)) return "—";
  const s = Math.max(0, Math.floor(seconds));
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m`;
  return `${s}s`;
}

function secondsSince(iso: string, now: number): number | undefined {
  const t = Date.parse(iso);
  return Number.isNaN(t) ? undefined : (now - t) / 1000;
}

/* Runs fn now and every `ms` while the page is visible; refires on return. */
function usePolling(fn: () => void, ms: number) {
  useEffect(() => {
    fn();
    const id = window.setInterval(() => {
      if (document.visibilityState === "visible") fn();
    }, ms);
    const onVisible = () => document.visibilityState === "visible" && fn();
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      window.clearInterval(id);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [fn, ms]);
}

export default function AdminDashboard() {
  return (
    <RequireSystemAccess>
      <AdminDashboardBody />
    </RequireSystemAccess>
  );
}

function AdminDashboardBody() {
  const { t } = useT();
  const [info, setInfo] = useState<SystemInfo | null>(null);
  const [stats, setStats] = useState<SystemStats | null>(null);
  const [queues, setQueues] = useState<RuntimeQueuesResponse | null>(null);
  const [pipeline, setPipeline] = useState<ProcessingDocuments | null>(null);
  const [denied, setDenied] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  const onError = useCallback(
    (e: unknown) => {
      if (e instanceof ApiError && e.status === 403) setDenied(true);
      else setError(e instanceof Error ? e.message : t("adm.loadFailed"));
    },
    [t],
  );

  const loadStats = useCallback(() => {
    setLoading(true);
    Promise.all([
      getSystemStats(120).then((res) => setStats(res.data ?? null)),
      getRuntimeQueues().then(setQueues),
      getSystemInfo().then((res) => setInfo(res.data ?? null)),
    ])
      .then(() => setError(""))
      .catch(onError)
      .finally(() => setLoading(false));
  }, [onError]);

  const loadPipeline = useCallback(() => {
    getProcessingDocuments(50)
      .then((res) => {
        setPipeline(res.data ?? null);
        setNow(Date.now());
      })
      .catch(onError);
  }, [onError]);

  usePolling(loadStats, STATS_REFRESH_MS);
  usePolling(loadPipeline, PIPELINE_REFRESH_MS);

  const refreshAll = () => {
    loadStats();
    loadPipeline();
  };

  if (denied) {
    return <p className="caption text-muted-soft">{t("stats.adminOnly")}</p>;
  }

  const fb = stats?.feedback;
  const rated = fb ? fb.likes + fb.dislikes : 0;
  const queueTotals = (queues?.queues ?? []).reduce(
    (acc, q) => ({
      active: acc.active + q.active,
      pending: acc.pending + q.pending,
      retry: acc.retry + q.retry,
      archived: acc.archived + q.archived,
    }),
    { active: 0, pending: 0, retry: 0, archived: 0 },
  );

  const kpis: { label: string; value: string; sub?: string }[] = [
    {
      label: t("dash.chat24h"),
      value: stats ? stats.chat_24h.questions.toLocaleString() : "—",
      sub: stats
        ? t("dash.chat24hSub", {
            sessions: stats.chat_24h.active_sessions.toLocaleString(),
            users: stats.chat_24h.active_users.toLocaleString(),
          })
        : undefined,
    },
    {
      label: t("dash.likes"),
      value: fb ? fb.likes.toLocaleString() : "—",
      sub: fb ? t("dash.in24h", { n: fb.likes_24h.toLocaleString() }) : undefined,
    },
    {
      label: t("dash.dislikes"),
      value: fb ? fb.dislikes.toLocaleString() : "—",
      sub: fb ? t("dash.in24h", { n: fb.dislikes_24h.toLocaleString() }) : undefined,
    },
    {
      label: t("dash.satisfaction"),
      value: fb && rated > 0 ? `${Math.round((fb.likes / rated) * 100)}%` : "—",
      sub: fb
        ? rated > 0
          ? t("dash.satisfactionSub", { n: rated.toLocaleString() })
          : t("dash.noVotes")
        : undefined,
    },
    {
      label: t("dash.processingNow"),
      value: pipeline ? pipeline.total.toLocaleString() : "—",
      sub: pipeline ? t("dash.failed24h", { n: pipeline.failed_24h.toLocaleString() }) : undefined,
    },
    {
      label: t("dash.tokensMonth"),
      value: stats?.usage ? formatTokens(stats.usage.month.tokens) : "—",
      sub: stats?.usage
        ? t("dash.tokensMonthSub", {
            questions: stats.usage.month.questions.toLocaleString(),
            today: formatTokens(stats.usage.today.tokens),
          })
        : undefined,
    },
  ];

  return (
    <div className="mx-auto w-full max-w-[1400px] space-y-6">
      <div className="flex flex-wrap items-center gap-3">
        <span className="caption text-muted">
          {t("dash.autoRefresh", { s: STATS_REFRESH_MS / 1000 })}
        </span>
        <button
          className="btn btn-outline btn-sm ml-auto h-8 w-8 p-0"
          onClick={refreshAll}
          disabled={loading}
          title={t("common.refresh")}
          aria-label={t("common.refresh")}
        >
          <IconRefresh className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
        </button>
      </div>

      {error && <p className="caption text-error">{error}</p>}
      {info?.db_migration_error && (
        <p className="caption text-error">
          {t("adm.migrationError", { err: info.db_migration_error })}
        </p>
      )}

      <div className="grid grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-5">
        {kpis.map((k) => (
          <div key={k.label} className="card p-5">
            <div className="caption-uppercase text-muted-soft">{k.label}</div>
            <div className="display-md mt-2 tabular-nums">{k.value}</div>
            {k.sub && <div className="caption mt-1 text-muted">{k.sub}</div>}
          </div>
        ))}
      </div>

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-5">
        <div className="xl:col-span-3">
          <PublicKBTable rows={stats?.public_kbs ?? null} />
        </div>
        <div className="card p-6 xl:col-span-2">
          <div className="mb-4 flex items-baseline justify-between">
            <h2 className="title-md">{t("stats.documentsByStatus")}</h2>
            {stats && (
              <span className="caption text-muted">
                {stats.documents.total.toLocaleString()} {t("stats.documents").toLowerCase()}
              </span>
            )}
          </div>
          <DocStatusBars stats={stats} />
          {queues?.available && (
            <>
              <h3 className="caption-uppercase mb-3 mt-6 text-muted-soft">{t("dash.queues")}</h3>
              <dl className="grid grid-cols-2 gap-3">
                {[
                  { label: t("adm.qActive"), value: queueTotals.active },
                  { label: t("adm.qPending"), value: queueTotals.pending },
                  { label: t("adm.qRetry"), value: queueTotals.retry },
                  { label: t("adm.qFailed"), value: queueTotals.archived },
                ].map((s) => (
                  <div key={s.label} className="rounded-lg border border-hairline px-3 py-2.5">
                    <dt className="caption text-muted">{s.label}</dt>
                    <dd className="mt-0.5 text-[18px] font-semibold tabular-nums text-ink">
                      {s.value.toLocaleString()}
                    </dd>
                  </div>
                ))}
              </dl>
            </>
          )}
        </div>
      </div>

      <PipelineTable data={pipeline} now={now} />

      {stats && (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-5">
          <div className="card p-6 xl:col-span-3">
            <div className="mb-4 flex items-baseline justify-between">
              <h2 className="title-md">{t("stats.messageActivity")}</h2>
              <span className="caption text-muted">
                {stats.messages.total.toLocaleString()} {t("stats.messages").toLowerCase()} ·{" "}
                {stats.messages.total_sessions.toLocaleString()}{" "}
                {t("stats.sessions").toLowerCase()}
              </span>
            </div>
            <MessageHeatmap days={stats.messages.days} data={stats.messages.by_day} />
          </div>
          <div className="card p-6 xl:col-span-2">
            <h2 className="title-md mb-4">{t("stats.accounts")}</h2>
            <dl className="space-y-3">
              {[
                { label: t("stats.totalUsers"), value: stats.accounts.total_users },
                { label: t("stats.activeUsers"), value: stats.accounts.active_users },
                { label: t("stats.systemAdmins"), value: stats.accounts.system_admins },
                { label: t("stats.workspaces"), value: stats.accounts.total_tenants },
                { label: t("stats.newUsers30d"), value: stats.accounts.new_users_last_30d },
              ].map((s) => (
                <div
                  key={s.label}
                  className="flex items-baseline justify-between border-b border-hairline pb-2.5"
                >
                  <dt className="caption text-muted">{s.label}</dt>
                  <dd className="text-[14px] font-medium tabular-nums text-ink">
                    {s.value.toLocaleString()}
                  </dd>
                </div>
              ))}
            </dl>
          </div>
        </div>
      )}

      <SystemInfoCard info={info} />
    </div>
  );
}

function PublicKBTable({ rows }: { rows: PublicKBDocumentCount[] | null }) {
  const { t } = useT();
  const total = (rows ?? []).reduce((n, r) => n + r.total, 0);
  const max = Math.max(1, ...(rows ?? []).map((r) => r.total));
  return (
    <div className="card h-full overflow-hidden">
      <div className="flex flex-wrap items-baseline justify-between gap-2 px-6 pb-4 pt-6">
        <h2 className="title-md">{t("dash.publicKbs")}</h2>
        {rows && rows.length > 0 && (
          <span className="caption text-muted">
            {t("dash.publicKbsTotal", { n: total.toLocaleString(), k: rows.length })}
          </span>
        )}
      </div>
      {rows === null ? (
        <p className="caption px-6 pb-6 text-muted">{t("adm.loading")}</p>
      ) : rows.length === 0 ? (
        <p className="caption px-6 pb-6 text-muted">{t("dash.noPublicKbs")}</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[560px] border-collapse text-left">
            <thead>
              <tr className="caption-uppercase border-y border-hairline bg-surface-strong/30 text-muted">
                <th className="px-6 py-2.5 font-medium">{t("dash.colKb")}</th>
                <th className="w-20 px-3 py-2.5 text-right font-medium">{t("dash.colTotal")}</th>
                <th className="w-24 px-3 py-2.5 text-right font-medium">{t("dash.colCompleted")}</th>
                <th className="w-24 px-3 py-2.5 text-right font-medium">{t("dash.colProcessing")}</th>
                <th className="w-20 px-6 py-2.5 text-right font-medium">{t("dash.colFailed")}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-hairline">
              {rows.map((r) => (
                <tr key={r.id} className="transition-colors hover:bg-surface-strong/20">
                  <td className="px-6 py-3 align-middle">
                    <Link
                      href={`/platform/knowledge-bases/${r.id}`}
                      className="block truncate text-[13px] font-medium text-ink hover:underline"
                    >
                      {r.name}
                    </Link>
                    {/* Bar length = share of the largest KB; segments split it
                     * by status (counts are in the columns beside it). */}
                    <div
                      className="mt-1.5 flex h-1.5 gap-[2px] overflow-hidden rounded-full bg-surface-strong"
                      style={{ width: `${Math.max(2, (r.total / max) * 100)}%` }}
                      title={`${t("dash.colCompleted")}: ${r.completed} · ${t("dash.colProcessing")}: ${r.processing} · ${t("dash.colFailed")}: ${r.failed}`}
                    >
                      {[
                        { n: r.completed, c: COLOR_DONE },
                        { n: r.processing, c: COLOR_ACTIVE },
                        { n: r.failed, c: COLOR_FAILED },
                      ]
                        .filter((s) => s.n > 0)
                        .map((s, i) => (
                          <div key={i} style={{ flexGrow: s.n, backgroundColor: s.c }} />
                        ))}
                    </div>
                  </td>
                  <td className="px-3 py-3 text-right text-[13px] font-semibold tabular-nums text-ink">
                    {r.total.toLocaleString()}
                  </td>
                  <td className="px-3 py-3 text-right text-[13px] tabular-nums text-muted">
                    {r.completed.toLocaleString()}
                  </td>
                  <td className="px-3 py-3 text-right text-[13px] tabular-nums text-muted">
                    {r.processing.toLocaleString()}
                  </td>
                  <td
                    className={`px-6 py-3 text-right text-[13px] tabular-nums ${r.failed > 0 ? "font-medium text-error" : "text-muted"}`}
                  >
                    {r.failed.toLocaleString()}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function DocStatusBars({ stats }: { stats: SystemStats | null }) {
  const { t } = useT();
  if (!stats) return <p className="caption text-muted">{t("adm.loading")}</p>;
  if (stats.documents.by_status.length === 0)
    return <p className="caption text-muted">{t("stats.noDocuments")}</p>;
  const max = Math.max(1, ...stats.documents.by_status.map((s) => s.count));
  return (
    <ul className="space-y-3">
      {stats.documents.by_status.map((s) => (
        <li key={s.status}>
          <div className="mb-1 flex items-baseline justify-between">
            <span className="caption capitalize text-muted">{s.status}</span>
            <span className="text-[13px] font-medium tabular-nums text-ink">
              {s.count.toLocaleString()}
            </span>
          </div>
          <div className="h-1.5 w-full rounded-full bg-surface-strong">
            <div
              className="h-1.5 rounded-full"
              style={{
                width: `${Math.max(2, (s.count / max) * 100)}%`,
                backgroundColor: DOC_STATUS_COLORS[s.status] ?? "var(--color-muted-soft)",
              }}
            />
          </div>
        </li>
      ))}
    </ul>
  );
}

function PipelineTable({ data, now }: { data: ProcessingDocuments | null; now: number }) {
  const { t } = useT();
  return (
    <div className="card overflow-hidden">
      <div className="flex flex-wrap items-baseline justify-between gap-2 px-6 pb-4 pt-6">
        <h2 className="title-md">{t("dash.pipeline")}</h2>
        <span className="caption text-muted">
          {data && data.total > data.items.length
            ? t("dash.pipelineMore", { shown: data.items.length, total: data.total.toLocaleString() })
            : t("dash.autoRefresh", { s: PIPELINE_REFRESH_MS / 1000 })}
        </span>
      </div>
      {data === null ? (
        <p className="caption px-6 pb-6 text-muted">{t("adm.loading")}</p>
      ) : data.items.length === 0 ? (
        <p className="caption px-6 pb-6 text-muted">{t("dash.pipelineEmpty")}</p>
      ) : (
        <div className="max-h-[480px] overflow-auto">
          <table className="w-full min-w-[820px] border-collapse text-left">
            <thead className="sticky top-0 z-10 bg-canvas">
              <tr className="caption-uppercase border-y border-hairline bg-surface-strong/30 text-muted">
                <th className="px-6 py-2.5 font-medium">{t("dash.colDocument")}</th>
                <th className="w-[220px] px-3 py-2.5 font-medium">{t("dash.colKb")}</th>
                <th className="w-[160px] px-3 py-2.5 font-medium">{t("dash.colUnit")}</th>
                <th className="w-[170px] px-3 py-2.5 font-medium">{t("dash.colStatus")}</th>
                <th className="w-[110px] px-6 py-2.5 text-right font-medium">{t("dash.colAge")}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-hairline">
              {data.items.map((d) => {
                const statusKey = PIPELINE_STATUS_KEYS[d.parse_status];
                return (
                  <tr key={d.id} className="transition-colors hover:bg-surface-strong/20">
                    <td className="max-w-0 px-6 py-3 align-middle">
                      <div className="truncate text-[13px] font-medium text-ink" title={d.title || d.file_name}>
                        {d.title || d.file_name || d.id}
                      </div>
                      {d.file_type && (
                        <div className="caption uppercase text-muted-soft">{d.file_type}</div>
                      )}
                    </td>
                    <td className="max-w-0 px-3 py-3 align-middle">
                      <Link
                        href={`/platform/knowledge-bases/${d.knowledge_base_id}`}
                        className="block truncate text-[13px] text-ink hover:underline"
                      >
                        {d.knowledge_base_name || d.knowledge_base_id}
                      </Link>
                    </td>
                    <td className="max-w-0 px-3 py-3 align-middle">
                      <span className="block truncate text-[13px] text-muted">
                        {d.tenant_id === 0 ? t("dash.platformUnit") : d.tenant_name || `#${d.tenant_id}`}
                      </span>
                    </td>
                    <td className="px-3 py-3 align-middle">
                      <div className="flex items-center gap-2 text-[13px] text-ink">
                        <span
                          className={`h-2 w-2 shrink-0 rounded-full ${d.parse_status === "pending" ? "" : "animate-pulse"}`}
                          style={{
                            backgroundColor: DOC_STATUS_COLORS[d.parse_status] ?? "var(--color-muted-soft)",
                          }}
                        />
                        {statusKey ? t(statusKey) : d.parse_status}
                      </div>
                      {d.pending_subtasks_count > 0 && (
                        <div className="caption text-muted">
                          {t("dash.subtasks", { n: d.pending_subtasks_count })}
                        </div>
                      )}
                    </td>
                    <td className="px-6 py-3 text-right align-middle text-[13px] tabular-nums text-muted">
                      {formatDuration(secondsSince(d.created_at, now))}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function SystemInfoCard({ info }: { info: SystemInfo | null }) {
  const { t } = useT();
  const rows: { label: string; value: string }[] = info
    ? [
        { label: t("adm.infoVersion"), value: info.version },
        { label: t("adm.infoEdition"), value: info.edition ?? "—" },
        { label: t("adm.infoCommit"), value: info.commit_id ?? "—" },
        { label: t("adm.infoBuildTime"), value: info.build_time ?? "—" },
        { label: t("adm.infoGoVersion"), value: info.go_version ?? "—" },
        { label: t("adm.infoStartedAt"), value: info.started_at ?? "—" },
        { label: t("adm.infoUptime"), value: formatDuration(info.uptime_seconds) },
        { label: t("adm.infoDatabase"), value: info.db_version ?? "—" },
        { label: t("adm.infoKeywordEngine"), value: info.keyword_index_engine ?? "—" },
        { label: t("adm.infoVectorStore"), value: info.vector_store_engine ?? "—" },
        { label: t("adm.infoGraphEngine"), value: info.graph_database_engine ?? "—" },
      ]
    : [];
  return (
    <div className="card p-6">
      <h2 className="title-md mb-5">{t("adm.sysInfo")}</h2>
      {info === null && <p className="caption text-muted">{t("adm.loading")}</p>}
      <dl className="grid grid-cols-1 gap-x-10 gap-y-4 sm:grid-cols-2 xl:grid-cols-3">
        {rows.map((r) => (
          <div key={r.label} className="flex items-baseline justify-between gap-4 border-b border-hairline pb-3">
            <dt className="caption shrink-0 text-muted">{r.label}</dt>
            <dd className="truncate text-[14px] font-medium text-ink" title={r.value}>
              {r.value}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  );
}
