"use client";

import { useEffect, useState } from "react";
import { ApiError } from "@/lib/api-client";
import {
  getRuntimeQueues,
  getSystemInfo,
  type RuntimeQueuesResponse,
  type SystemInfo,
} from "@/lib/api/system";

function formatUptime(seconds?: number): string {
  if (seconds === undefined) return "—";
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

export default function SystemOverview() {
  const [info, setInfo] = useState<SystemInfo | null>(null);
  const [queues, setQueues] = useState<RuntimeQueuesResponse | null>(null);
  const [queuesDenied, setQueuesDenied] = useState(false);
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
