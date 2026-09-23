/* Runtime queue stats — extracted from the old services catch-all page. */
"use client";

import { useEffect, useState } from "react";
import { ApiError } from "@/lib/api-client";
import { getRuntimeQueues, type QueueStat } from "@/lib/api/system";
import { IconRefresh } from "@/components/icons";

export default function RuntimeQueuesPage() {
  const [queues, setQueues] = useState<QueueStat[] | null>(null);
  const [denied, setDenied] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [loading, setLoading] = useState(true);

  const load = async () => {
    setLoading(true);
    try {
      const res = await getRuntimeQueues();
      setQueues(res.queues ?? []);
      setUnavailable(!res.available);
      setDenied(false);
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        setQueues([]);
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  return (
    <div>
      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 className="title-md font-semibold text-ink">Runtime Queues</h2>
          <p className="caption text-muted">
            {loading ? "checking…" : `${(queues ?? []).length} queues reported`}
          </p>
        </div>
        <button className="btn btn-outline btn-sm" onClick={() => void load()}>
          <IconRefresh className="h-3.5 w-3.5" /> Refresh
        </button>
      </div>

      {denied && (
        <p className="caption mb-4 text-muted">
          Runtime queue metrics are only available to system administrators.
        </p>
      )}
      {unavailable && (
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
      {queues !== null && queues.length === 0 && !denied && (
        <p className="caption mt-2 text-muted-soft">No queues reported.</p>
      )}
    </div>
  );
}
