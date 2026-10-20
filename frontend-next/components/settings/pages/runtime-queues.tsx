/* Runtime queue stats — extracted from the old services catch-all page. */
"use client";

import { useEffect, useState } from "react";
import { ApiError } from "@/lib/api-client";
import { getRuntimeQueues, type QueueStat, type WikiRunWindowStatus } from "@/lib/api/system";
import { IconRefresh } from "@/components/icons";
import { WikiRunWindowCard } from "@/components/settings/wiki-run-window-card";
import { InputLimitsCard } from "@/components/settings/input-limits-card";
import { ConsolePage } from "@/components/settings/console-page";
import { DeniedView } from "@/components/settings/system-section";
import { Notice } from "@/components/settings/notice";
import { useT } from "@/lib/i18n";

export default function RuntimeQueuesPage() {
  const { t } = useT();
  const [queues, setQueues] = useState<QueueStat[] | null>(null);
  const [wikiWindow, setWikiWindow] = useState<WikiRunWindowStatus | null>(null);
  const [denied, setDenied] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [loading, setLoading] = useState(true);
  /* Every failure that is not a 403 lands here — the page used to swallow
   * them and render an empty grid with no way to retry. */
  const [error, setError] = useState("");

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      const res = await getRuntimeQueues();
      setQueues(res.queues ?? []);
      setWikiWindow(res.wiki_run_window ?? null);
      setUnavailable(!res.available);
      setDenied(false);
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        setQueues([]);
        setWikiWindow(null);
      } else {
        setError(e instanceof Error ? e.message : t("sys.loadFailed"));
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (denied) {
    return <DeniedView />;
  }

  return (
    <ConsolePage>
      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div>
          <p className="caption text-muted">
            {loading ? t("q.checking") : t("q.count", { n: (queues ?? []).length })}
          </p>
        </div>
        <button className="btn btn-outline btn-sm" onClick={() => void load()} disabled={loading}>
          <IconRefresh className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} /> {t("common.refresh")}
        </button>
      </div>

      {error && (
        <Notice
          tone="error"
          className="mb-4"
          action={
            <button type="button" className="btn btn-outline btn-sm" onClick={() => void load()}>
              {t("common.retry")}
            </button>
          }
        >
          {error}
        </Notice>
      )}
      {unavailable && (
        <p className="caption mb-4 text-muted">
          {t("q.unavailable")}
        </p>
      )}
      <div
        className="mb-6 grid grid-cols-[repeat(auto-fit,minmax(min(100%,240px),1fr))] gap-4"
      >
        {(queues ?? []).map((q) => (
          <div key={q.name} className="card p-5">
            <div className="mb-4 flex items-center justify-between">
              <span className="text-[14px] font-medium text-ink">{q.name}</span>
              {q.archived > 0 && (
                <span className="caption font-medium text-error">{t("q.failed", { n: q.archived })}</span>
              )}
              {q.paused && <span className="caption font-medium text-muted">{t("q.paused")}</span>}
            </div>
            <div className="grid grid-cols-3 gap-2 text-center">
              {[
                { label: t("q.active"), value: q.active },
                { label: t("q.pending"), value: q.pending },
                { label: t("q.retry"), value: q.retry },
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
      {queues !== null && queues.length === 0 && !denied && !error && (
        <p className="caption mt-2 text-muted-soft">{t("q.empty")}</p>
      )}
      {wikiWindow && !unavailable && (
        <WikiRunWindowCard status={wikiWindow} onSaved={() => void load()} />
      )}
      {!denied && <InputLimitsCard />}
    </ConsolePage>
  );
}
