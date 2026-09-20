"use client";

import { useEffect, useState } from "react";
import { ApiError } from "@/lib/api-client";
import {
  getParserEngines,
  getRuntimeQueues,
  getStorageEngineStatus,
  type QueueStat,
} from "@/lib/api/system";
import { IconRefresh } from "@/components/icons";

const STATUS = {
  healthy: { label: "Healthy", dot: "#16a34a", cls: "text-success" },
  degraded: { label: "Degraded", dot: "#f4c5a8", cls: "text-muted" },
  down: { label: "Down", dot: "#dc2626", cls: "text-error" },
} as const;

type ServiceRow = {
  id: string;
  name: string;
  description?: string;
  status: keyof typeof STATUS;
  detail?: string;
};

export default function SystemServices() {
  const [services, setServices] = useState<ServiceRow[] | null>(null);
  const [queues, setQueues] = useState<QueueStat[] | null>(null);
  const [queuesDenied, setQueuesDenied] = useState(false);
  const [queuesUnavailable, setQueuesUnavailable] = useState(false);
  const [error, setError] = useState("");

  const load = async () => {
    setError("");
    try {
      const [parsers, storage] = await Promise.all([
        getParserEngines(),
        getStorageEngineStatus(),
      ]);
      const rows: ServiceRow[] = [];
      rows.push({
        id: "docreader",
        name: "DocReader",
        description: `Document parsing service (${parsers.docreader_transport ?? "grpc"})`,
        status: parsers.connected === false ? "down" : "healthy",
        detail: parsers.docreader_addr,
      });
      for (const p of parsers.data ?? []) {
        rows.push({
          id: `parser-${p.Name}`,
          name: `Parser: ${p.Name}`,
          description: p.Description,
          status: p.Available === false ? "degraded" : "healthy",
          detail:
            p.Available === false
              ? p.UnavailableReason
              : p.FileTypes?.slice(0, 4).join(", "),
        });
      }
      for (const e of storage.data?.engines ?? []) {
        rows.push({
          id: `storage-${e.name}`,
          name: `Storage: ${e.name}`,
          description: e.description,
          status: !e.available ? "down" : e.allowed === false ? "degraded" : "healthy",
          detail: e.allowed === false ? "not allowed in this deployment" : undefined,
        });
      }
      setServices(rows);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load services");
      setServices([]);
    }
    try {
      const res = await getRuntimeQueues();
      setQueues(res.queues ?? []);
      setQueuesUnavailable(!res.available);
      setQueuesDenied(false);
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setQueuesDenied(true);
        setQueues([]);
      }
    }
  };

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const rows = services ?? [];
  const healthy = rows.filter((s) => s.status === "healthy").length;

  return (
    <div className="mx-auto w-full max-w-[1100px]">
      <div className="mb-5 flex items-center justify-between">
        <div className="caption text-muted">
          <span className="font-medium text-ink">
            {healthy}/{rows.length}
          </span>{" "}
          services healthy · {services === null ? "loading…" : "checked just now"}
        </div>
        <button className="btn btn-outline btn-sm" onClick={() => void load()}>
          <IconRefresh className="h-3.5 w-3.5" /> Refresh
        </button>
      </div>

      {error && <p className="caption mb-4 text-error">{error}</p>}

      <div className="card mb-6 overflow-hidden">
        {rows.map((s, i) => {
          const st = STATUS[s.status];
          return (
            <div
              key={s.id}
              className={`flex items-center gap-4 px-5 py-4 ${
                i > 0 ? "border-t border-hairline" : ""
              }`}
            >
              <span
                className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
                style={{ background: st.dot, boxShadow: `0 0 0 3px ${st.dot}22` }}
              />
              <div className="min-w-0 flex-1">
                <div className="text-[15px] font-medium text-ink">{s.name}</div>
                <div className="caption truncate text-muted">{s.description}</div>
              </div>
              <span className="caption w-56 truncate text-right text-muted">
                {s.detail ?? "—"}
              </span>
              <span className={`caption w-20 font-medium ${st.cls}`}>{st.label}</span>
            </div>
          );
        })}
        {services !== null && rows.length === 0 && !error && (
          <div className="px-5 py-12 text-center text-[14px] text-muted">
            No services reported by the backend.
          </div>
        )}
      </div>

      <h2 className="title-md mb-4">Runtime queues</h2>
      {queuesDenied && (
        <p className="caption mb-4 text-muted">
          Runtime queue metrics are only available to system administrators.
        </p>
      )}
      {queuesUnavailable && (
        <p className="caption mb-4 text-muted">
          Queue metrics are unavailable in this deployment.
        </p>
      )}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {(queues ?? []).map((q) => (
          <div key={q.name} className="card p-5">
            <div className="mb-4 flex items-center justify-between">
              <span className="text-[14px] font-medium text-ink">{q.name}</span>
              {q.archived > 0 && (
                <span className="caption font-medium text-error">{q.archived} failed</span>
              )}
              {q.paused && <span className="caption font-medium text-muted">paused</span>}
            </div>
            <div className="grid grid-cols-3 gap-2 text-center">
              {[
                { label: "Active", value: q.active },
                { label: "Pending", value: q.pending },
                { label: "Retry", value: q.retry },
              ].map((m) => (
                <div key={m.label}>
                  <div className="display-sm">{m.value}</div>
                  <div className="caption-uppercase mt-1 text-[10px] text-muted-soft">
                    {m.label}
                  </div>
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
      {queues !== null && queues.length === 0 && !queuesDenied && (
        <p className="caption mt-2 text-muted-soft">No queues reported.</p>
      )}
    </div>
  );
}
