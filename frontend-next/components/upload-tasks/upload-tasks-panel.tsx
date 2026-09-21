"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import {
  IconArrowUp,
  IconCheck,
  IconChevronDown,
  IconChevronUp,
  IconClose,
  IconErrorCircleFilled,
  IconFileSearch,
  IconInfoCircle,
  IconCheckCircleFilled,
  IconRefresh,
} from "@/components/icons";
import { ProgressRing } from "@/components/upload-tasks/progress-ring";
import { UploadTaskRow } from "@/components/upload-tasks/upload-task-row";
import {
  estimateRate,
  estimateRemainingSeconds,
  itemPhase,
  splitDuration,
  type RateSample,
  type UploadItem,
} from "@/lib/upload-tasks-state";
import { useUploadTasks } from "@/lib/upload-tasks";
import { useT, type LocaleKey } from "@/lib/i18n";

/* Ported from frontend/src/components/upload-tasks/UploadTasksPanel.vue. */

/** A run that finished cleanly leaves on its own after this long without hover. */
const AUTO_DISMISS_MS = 6000;

function formatBytes(value: number): string {
  if (!value) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB"];
  const i = Math.min(Math.floor(Math.log(value) / Math.log(k)), sizes.length - 1);
  return `${parseFloat((value / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`;
}

type BadgeTone = "brand" | "success" | "warning" | "neutral";

const BADGE_CLS: Record<BadgeTone, string> = {
  brand: "bg-surface-strong text-ink",
  success: "bg-green-500/10 text-success",
  warning: "bg-amber-500/10 text-amber-600",
  neutral: "bg-surface-strong text-muted-soft",
};

const LEGEND_DOT: Record<string, string> = {
  ready: "bg-ink",
  active: "bg-ink/40",
  waiting: "bg-hairline-strong",
  failed: "bg-error",
  duplicate: "bg-amber-500",
};

export function UploadTasksPanel() {
  const { t } = useT();
  const pathname = usePathname();
  const router = useRouter();
  const store = useUploadTasks();
  const { items, batches, summary, visible, collapsed } = store;

  const [hovering, setHovering] = useState(false);
  const [filter, setFilter] = useState<"all" | "issues">("all");

  const issueCount = summary.failed + summary.duplicate + summary.cancelled;
  const isUploading = summary.stage === "uploading";
  const pendingTransfers = summary.waiting + summary.uploading;
  const parseSettled = summary.uploaded - summary.parsing;

  // A panel dismissed under the pointer never sees mouseleave.
  useEffect(() => {
    if (!visible) setHovering(false);
  }, [visible]);

  useEffect(() => {
    if (issueCount === 0) setFilter("all");
  }, [issueCount]);

  // ---- transfer rate --------------------------------------------------------

  const samplesRef = useRef<RateSample[]>([]);
  const [rate, setRate] = useState(0);
  const sentBytes = summary.sentBytes;

  useEffect(() => {
    if (!isUploading) {
      samplesRef.current = [];
      setRate(0);
      return;
    }
    const takeSample = () => {
      const samples = samplesRef.current;
      const last = samples[samples.length - 1];
      // A failed, cancelled or retried transfer moves the count backwards; start over.
      if (last && sentBytes < last.bytes) samples.length = 0;
      samples.push({ at: Date.now(), bytes: sentBytes });
      if (samples.length > 20) samples.shift();
      setRate(estimateRate(samples));
    };
    takeSample();
    const timer = setInterval(takeSample, 1000);
    return () => clearInterval(timer);
  }, [isUploading, sentBytes]);

  const remainingText = useMemo(() => {
    const seconds = estimateRemainingSeconds(summary.totalBytes - summary.loadedBytes, rate);
    if (seconds === null) return "";
    const { unit, value } = splitDuration(seconds);
    const etaKey = (`uploadTasks.eta.${unit}`) as LocaleKey;
    return t("uploadTasks.remaining", { time: t(etaKey, { n: value }) });
  }, [summary.totalBytes, summary.loadedBytes, rate, t]);

  // ---- header ---------------------------------------------------------------

  const batchLabel = (batch: { kbName: string; targetFolder: string }) =>
    batch.targetFolder ? `${batch.kbName} / ${batch.targetFolder}` : batch.kbName;

  const destination = useMemo(() => {
    const kbIds = new Set(batches.map((batch) => batch.kbId));
    if (kbIds.size > 1) return t("uploadTasks.destinationMany", { count: kbIds.size });
    const batch = batches[0];
    if (!batch) return "";
    return t("uploadTasks.destination", { name: batchLabel(batch) });
  }, [batches, t]);

  const headline = useMemo(() => {
    const s = summary;
    if (s.stage === "uploading")
      return t("uploadTasks.titleUploading", { done: s.transferSettled, total: s.transferTotal });
    if (s.stage === "parsing")
      return t("uploadTasks.titleParsing", { done: parseSettled, total: s.uploaded });
    if (s.ready === 0 && s.failed + s.duplicate === 0) return t("uploadTasks.titleCancelled");
    if (issueCount === 0) return t("uploadTasks.titleDone");
    return t("uploadTasks.titleDoneWithIssues", { ok: s.ready, bad: issueCount });
  }, [summary, parseSettled, issueCount, t]);

  const subline = useMemo(() => {
    if (summary.stage !== "uploading") return destination;
    const transferred = `${formatBytes(summary.loadedBytes)} / ${formatBytes(summary.totalBytes)}`;
    return remainingText ? `${transferred} · ${remainingText}` : transferred;
  }, [summary, destination, remainingText]);

  const badge = useMemo<{ tone: BadgeTone; icon: React.ReactNode; ring: number | null }>(() => {
    const s = summary;
    const cls = "h-[18px] w-[18px]";
    if (s.stage === "uploading") {
      return {
        tone: "brand",
        icon: <IconArrowUp className={cls} />,
        ring: s.totalBytes > 0 ? s.loadedBytes / s.totalBytes : 0,
      };
    }
    if (s.stage === "parsing") {
      return {
        tone: "brand",
        icon: <IconFileSearch className={cls} />,
        ring: s.uploaded > 0 ? parseSettled / s.uploaded : 0,
      };
    }
    if (s.ready === 0 && s.failed + s.duplicate === 0)
      return { tone: "neutral", icon: <IconClose className={cls} />, ring: null };
    if (issueCount > 0)
      return { tone: "warning", icon: <IconErrorCircleFilled className={cls} />, ring: null };
    return { tone: "success", icon: <IconCheck className={cls} />, ring: null };
  }, [summary, parseSettled, issueCount]);

  // ---- progress bar -----------------------------------------------------------

  const percent = (fraction: number) => `${(fraction * 100).toFixed(2)}%`;
  const overallPercent = Math.round(
    (summary.bar.ready + summary.bar.failed + summary.bar.duplicate) * 100,
  );

  const legend = [
    { key: "ready", label: t("uploadTasks.legend.ready"), count: summary.ready },
    { key: "active", label: t("uploadTasks.legend.active"), count: summary.uploading + summary.parsing },
    { key: "waiting", label: t("uploadTasks.legend.waiting"), count: summary.waiting },
    { key: "failed", label: t("uploadTasks.legend.failed"), count: summary.failed },
    { key: "duplicate", label: t("uploadTasks.legend.duplicate"), count: summary.duplicate },
  ].filter((entry) => entry.count > 0);

  const hint =
    summary.stage === "uploading"
      ? { icon: <IconInfoCircle className="h-4 w-4" />, text: t("uploadTasks.hintUploading") }
      : summary.stage === "parsing"
        ? { icon: <IconCheckCircleFilled className="h-4 w-4" />, text: t("uploadTasks.hintParsing") }
        : null;

  // ---- list -------------------------------------------------------------------

  const isIssue = (item: UploadItem) => {
    const phase = itemPhase(item);
    return phase === "failed" || phase === "duplicate" || phase === "cancelled";
  };

  const groups = useMemo(() => {
    const visibleItems = filter === "issues" ? items.filter(isIssue) : items;
    const byBatch = new Map<string, UploadItem[]>();
    for (const item of visibleItems) {
      if (!byBatch.has(item.batchId)) byBatch.set(item.batchId, []);
      byBatch.get(item.batchId)!.push(item);
    }
    return batches
      .filter((batch) => byBatch.has(batch.id))
      .map((batch) => ({ batch, label: batchLabel(batch), items: byBatch.get(batch.id)! }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items, batches, filter]);

  const openItem = (item: UploadItem) => {
    const kbId = store.batchById.get(item.batchId)?.kbId;
    if (!kbId || !item.knowledgeId) return;
    // Same knowledge base page: app-router dedupes an identical navigation, so
    // ask the page to open the document directly.
    if (pathname === `/platform/knowledge-bases/${kbId}`) {
      window.dispatchEvent(
        new CustomEvent("weknora:open-knowledge", {
          detail: { kbId, knowledgeId: item.knowledgeId },
        }),
      );
      return;
    }
    router.push(`/platform/knowledge-bases/${kbId}?knowledge_id=${item.knowledgeId}`);
  };

  // ---- auto dismiss -------------------------------------------------------------

  const finishedCleanly = summary.stage === "done" && summary.ready > 0 && issueCount === 0;
  useEffect(() => {
    if (!finishedCleanly || hovering) return;
    const timer = setTimeout(() => store.dismiss(), AUTO_DISMISS_MS);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [finishedCleanly, hovering]);

  // ---- leaving the page -----------------------------------------------------------

  // Closing the tab kills in-flight requests; parsing is server side and survives.
  useEffect(() => {
    if (!isUploading) return;
    const handler = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, [isUploading]);

  if (!visible || items.length === 0) return null;

  const handleClose = () => {
    // Popconfirm port: closing mid-upload asks once, via the native dialog.
    if (isUploading && !window.confirm(t("uploadTasks.closeConfirm", { count: pendingTransfers })))
      return;
    store.dismiss();
  };

  return (
    <section
      className="fixed bottom-6 right-6 z-40 flex max-h-[calc(100vh-80px)] w-[380px] max-w-[calc(100vw-32px)] flex-col overflow-hidden rounded-[16px] border border-hairline bg-surface-card shadow-[0_8px_30px_rgba(0,0,0,0.12)]"
      role="region"
      aria-label={t("uploadTasks.panelLabel")}
      onMouseEnter={() => setHovering(true)}
      onMouseLeave={() => setHovering(false)}
    >
      <header className="flex items-center gap-3 px-4 pt-4 pr-3">
        <span
          className={`relative grid h-9 w-9 shrink-0 place-items-center rounded-full ${BADGE_CLS[badge.tone]}`}
          aria-hidden="true"
        >
          {badge.ring !== null && (
            <ProgressRing className="absolute inset-0" value={badge.ring} size={36} stroke={2.5} />
          )}
          {badge.icon}
        </span>
        <div className="min-w-0 flex-1">
          <div className="text-[14px] font-semibold leading-[22px] tabular-nums text-ink" aria-live="polite">
            {headline}
          </div>
          <div className="truncate text-[12px] leading-[18px] tabular-nums text-muted" title={subline}>
            {subline}
          </div>
        </div>
        <div className="flex shrink-0 items-start gap-0.5 text-muted">
          <button
            type="button"
            className="grid h-7 w-7 place-items-center rounded-[6px] hover:bg-surface-strong hover:text-ink"
            aria-label={collapsed ? t("uploadTasks.expand") : t("uploadTasks.collapse")}
            aria-expanded={!collapsed}
            onClick={store.toggleCollapsed}
          >
            {collapsed ? <IconChevronUp className="h-4 w-4" /> : <IconChevronDown className="h-4 w-4" />}
          </button>
          <button
            type="button"
            className="grid h-7 w-7 place-items-center rounded-[6px] hover:bg-surface-strong hover:text-ink"
            aria-label={t("uploadTasks.close")}
            onClick={handleClose}
          >
            <IconClose className="h-4 w-4" />
          </button>
        </div>
      </header>

      <div className="px-4 py-3">
        <div
          className="flex h-1.5 overflow-hidden rounded-full bg-surface-strong"
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={overallPercent}
        >
          <span className="h-full bg-ink transition-all" style={{ width: percent(summary.bar.ready) }} />
          <span
            className="upload-bar-active h-full transition-all"
            style={{ width: percent(summary.bar.active) }}
          />
          <span className="h-full bg-error transition-all" style={{ width: percent(summary.bar.failed) }} />
          <span
            className="h-full bg-amber-500 transition-all"
            style={{ width: percent(summary.bar.duplicate) }}
          />
        </div>
        {!collapsed && legend.length > 0 && (
          <ul className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[11.5px] leading-[18px] text-muted">
            {legend.map((entry) => (
              <li key={entry.key} className="inline-flex items-center gap-1.5">
                <i className={`h-1.5 w-1.5 rounded-full ${LEGEND_DOT[entry.key]}`} aria-hidden="true" />
                {entry.label}
                <b className="font-semibold tabular-nums text-ink">{entry.count}</b>
              </li>
            ))}
          </ul>
        )}
      </div>

      {!collapsed && (
        <div className="flex min-h-0 flex-col border-t border-hairline">
          {hint && (
            <p
              className={`mx-4 mt-3 flex items-start gap-1.5 rounded-[8px] px-3 py-2 text-[12px] leading-[18px] ${
                summary.stage === "parsing" ? "bg-surface-strong text-ink" : "bg-surface-strong text-muted"
              }`}
            >
              <span className="mt-0.5 shrink-0 text-muted-soft">{hint.icon}</span>
              <span>{hint.text}</span>
            </p>
          )}

          {issueCount > 0 && items.length > 1 && (
            <div className="mx-4 mt-3 inline-flex self-start gap-0.5 rounded-[8px] bg-surface-strong p-0.5" role="tablist">
              <button
                type="button"
                role="tab"
                aria-selected={filter === "all"}
                className={`inline-flex items-center gap-1 rounded-[6px] px-3 py-0.5 text-[12px] leading-5 transition-colors ${
                  filter === "all" ? "bg-surface-card text-ink shadow-sm" : "text-muted hover:text-ink"
                }`}
                onClick={() => setFilter("all")}
              >
                {t("uploadTasks.filterAll")}
                <span className="tabular-nums">{items.length}</span>
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={filter === "issues"}
                className={`inline-flex items-center gap-1 rounded-[6px] px-3 py-0.5 text-[12px] leading-5 transition-colors ${
                  filter === "issues" ? "bg-surface-card text-ink shadow-sm" : "text-muted hover:text-ink"
                }`}
                onClick={() => setFilter("issues")}
              >
                {t("uploadTasks.filterIssues")}
                <span className="tabular-nums text-error">{issueCount}</span>
              </button>
            </div>
          )}

          <div className="mt-1 min-h-0 overflow-y-auto px-2 pb-2">
            {groups.map((group) => (
              <div key={group.batch.id}>
                {batches.length > 1 && (
                  <div
                    className="flex items-center justify-between px-2 pt-2 text-[11.5px] text-muted"
                    title={group.label}
                  >
                    <span className="truncate">{group.label}</span>
                    <span className="tabular-nums">{group.items.length}</span>
                  </div>
                )}
                {group.items.map((item) => (
                  <UploadTaskRow
                    key={item.id}
                    item={item}
                    onCancel={store.cancelItem}
                    onRetry={store.retryItem}
                    onOpen={openItem}
                  />
                ))}
              </div>
            ))}
          </div>

          {(isUploading || summary.retryable > 0) && (
            <footer className="flex items-center justify-between border-t border-hairline px-4 py-2">
              {isUploading ? (
                <button
                  type="button"
                  className="text-[12.5px] font-medium text-muted hover:text-ink"
                  onClick={store.cancelAll}
                >
                  {t("uploadTasks.cancelAll")}
                </button>
              ) : (
                <span />
              )}
              {summary.retryable > 0 && (
                <button
                  type="button"
                  className="inline-flex items-center gap-1 text-[12.5px] font-medium text-ink hover:underline"
                  onClick={store.retryFailed}
                >
                  <IconRefresh className="h-3.5 w-3.5" />
                  {t("uploadTasks.retryFailed", { count: summary.retryable })}
                </button>
              )}
            </footer>
          )}
        </div>
      )}
    </section>
  );
}
