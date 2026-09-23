"use client";

import { useEffect, useState } from "react";
import { ApiError } from "@/lib/api-client";
import { useT } from "@/lib/i18n";
import { RequireSystemAccess } from "@/components/require-system-access";
import {
  getRuntimeQueues,
  getSystemInfo,
  getSystemStats,
  type RuntimeQueuesResponse,
  type SystemInfo,
  type SystemStats,
} from "@/lib/api/system";
import { MessageHeatmap } from "@/components/system/message-heatmap";
import { SectionCardGrid, type SectionCard } from "@/components/system/section-cards";
import { IconClock, IconOrg } from "@/components/icons";

/* Management surfaces — full pages with tables/audit UI. */
const adminCards: SectionCard[] = [
  {
    key: "users",
    title: "Users",
    desc: "Platform accounts, roles and system admins.",
    icon: <IconOrg className="h-5 w-5" />,
    href: "/platform/system/admin/users",
  },
  {
    key: "logs",
    title: "Audit logs",
    desc: "Search the platform audit trail.",
    icon: <IconClock className="h-5 w-5" />,
    href: "/platform/system/admin/logs",
  },
];

function formatUptime(seconds?: number): string {
  if (seconds === undefined) return "—";
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

const DOC_STATUS_COLORS: Record<string, string> = {
  completed: "rgb(22, 163, 74)",
  processing: "rgb(59, 130, 246)",
  finalizing: "rgb(59, 130, 246)",
  pending: "var(--color-muted-soft)",
  failed: "rgb(220, 38, 38)",
  deleting: "var(--color-muted-soft)",
  cancelled: "var(--color-muted-soft)",
};

export default function SystemOverview() {
  return (
    <RequireSystemAccess>
      <SystemOverviewBody />
    </RequireSystemAccess>
  );
}

function SystemOverviewBody() {
  const { t } = useT();
  const [info, setInfo] = useState<SystemInfo | null>(null);
  const [queues, setQueues] = useState<RuntimeQueuesResponse | null>(null);
  const [queuesDenied, setQueuesDenied] = useState(false);
  const [stats, setStats] = useState<SystemStats | null>(null);
  const [statsDenied, setStatsDenied] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    getSystemInfo()
      .then((res) => alive && setInfo(res.data ?? null))
      .catch((e) => alive && setError(e instanceof Error ? e.message : "Failed to load system info"));
    getRuntimeQueues()
      .then((res) => alive && setQueues(res))
      .catch((e) => {
        if (alive && e instanceof ApiError && e.status === 403) setQueuesDenied(true);
      });
    getSystemStats(120)
      .then((res) => alive && setStats(res.data ?? null))
      .catch((e) => {
        if (alive && e instanceof ApiError && e.status === 403) setStatsDenied(true);
      });
    return () => {
      alive = false;
    };
  }, []);

  const totals = (queues?.queues ?? []).reduce(
    (acc, q) => ({
      active: acc.active + q.active,
      pending: acc.pending + q.pending,
      retry: acc.retry + q.retry,
      archived: acc.archived + q.archived,
    }),
    { active: 0, pending: 0, retry: 0, archived: 0 },
  );

  const maxDocCount = Math.max(1, ...(stats?.documents.by_status ?? []).map((s) => s.count));

  const infoRows: { label: string; value: string }[] = info
    ? [
        { label: "Version", value: info.version },
        { label: "Edition", value: info.edition ?? "—" },
        { label: "Commit", value: info.commit_id ?? "—" },
        { label: "Build time", value: info.build_time ?? "—" },
        { label: "Go version", value: info.go_version ?? "—" },
        { label: "Started at", value: info.started_at ?? "—" },
        { label: "Uptime", value: formatUptime(info.uptime_seconds) },
        { label: "Database", value: info.db_version ?? "—" },
        { label: "Keyword engine", value: info.keyword_index_engine ?? "—" },
        { label: "Vector store", value: info.vector_store_engine ?? "—" },
        { label: "Graph engine", value: info.graph_database_engine ?? "—" },
      ]
    : [];

  return (
    <div className="mx-auto w-full max-w-[1100px]">
      {error && <p className="caption mb-4 text-error">{error}</p>}
      {info?.db_migration_error && (
        <p className="caption mb-4 text-error">
          Database migration error: {info.db_migration_error}
        </p>
      )}

      <div className="mb-8">
        <SectionCardGrid cards={adminCards} />
      </div>

      {stats && (
        <>
          <div className="mb-6">
            <h2 className="title-md mb-3">{t("stats.accounts")}</h2>
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
              {[
                { label: t("stats.totalUsers"), value: stats.accounts.total_users },
                { label: t("stats.activeUsers"), value: stats.accounts.active_users },
                { label: t("stats.systemAdmins"), value: stats.accounts.system_admins },
                { label: t("stats.workspaces"), value: stats.accounts.total_tenants },
                { label: t("stats.newUsers30d"), value: stats.accounts.new_users_last_30d },
              ].map((s) => (
                <div key={s.label} className="card p-5">
                  <div className="caption-uppercase text-muted-soft">{s.label}</div>
                  <div className="display-md mt-2">{s.value.toLocaleString()}</div>
                </div>
              ))}
            </div>
          </div>

          <div className="mb-6 grid grid-cols-1 gap-4 lg:grid-cols-2">
            <div className="card p-6">
              <div className="mb-4 flex items-baseline justify-between">
                <h2 className="title-md">{t("stats.documentsByStatus")}</h2>
                <span className="caption text-muted">
                  {stats.documents.total.toLocaleString()} {t("stats.documents").toLowerCase()}
                </span>
              </div>
              {stats.documents.by_status.length === 0 ? (
                <p className="caption text-muted">{t("stats.noDocuments")}</p>
              ) : (
                <ul className="space-y-3">
                  {stats.documents.by_status.map((s) => (
                    <li key={s.status}>
                      <div className="mb-1 flex items-baseline justify-between">
                        <span className="caption capitalize text-muted">{s.status}</span>
                        <span className="text-[13px] font-medium text-ink">
                          {s.count.toLocaleString()}
                        </span>
                      </div>
                      <div className="h-1.5 w-full rounded-full bg-surface-strong">
                        <div
                          className="h-1.5 rounded-full"
                          style={{
                            width: `${Math.max(2, (s.count / maxDocCount) * 100)}%`,
                            backgroundColor:
                              DOC_STATUS_COLORS[s.status] ?? "var(--color-muted-soft)",
                          }}
                        />
                      </div>
                    </li>
                  ))}
                </ul>
              )}
            </div>

            <div className="card p-6">
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
          </div>
        </>
      )}
      {statsDenied && (
        <p className="caption mb-4 text-muted-soft">{t("stats.adminOnly")}</p>
      )}

      {queues && (
        <div className="mb-6 grid grid-cols-2 gap-4 lg:grid-cols-4">
          {[
            { label: "Active tasks", value: totals.active },
            { label: "Pending", value: totals.pending },
            { label: "Retrying", value: totals.retry },
            { label: "Failed (archived)", value: totals.archived },
          ].map((s) => (
            <div key={s.label} className="card p-5">
              <div className="caption-uppercase text-muted-soft">{s.label}</div>
              <div className="display-md mt-2">{s.value}</div>
            </div>
          ))}
        </div>
      )}
      {queuesDenied && (
        <p className="caption mb-4 text-muted-soft">
          Runtime queue metrics are only available to system administrators.
        </p>
      )}
      {queues && !queues.available && (
        <p className="caption mb-4 text-muted-soft">
          Queue metrics are unavailable in this deployment.
        </p>
      )}

      <div className="card p-6">
        <h2 className="title-md mb-5">System info</h2>
        {info === null && !error && <p className="caption text-muted">Loading…</p>}
        <dl className="grid grid-cols-1 gap-x-10 gap-y-4 sm:grid-cols-2">
          {infoRows.map((r) => (
            <div
              key={r.label}
              className="flex items-baseline justify-between border-b border-hairline pb-3"
            >
              <dt className="caption text-muted">{r.label}</dt>
              <dd className="text-[14px] font-medium text-ink">{r.value}</dd>
            </div>
          ))}
        </dl>
      </div>
    </div>
  );
}
