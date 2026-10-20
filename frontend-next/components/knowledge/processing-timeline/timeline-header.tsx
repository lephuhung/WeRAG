"use client";

import { useMemo, type Dispatch, type SetStateAction } from "react";
import { IconClose, IconInfoCircle, IconMinusCircle, IconRefresh } from "@/components/icons";
import { CHIP_CLS } from "@/components/knowledge/trace-status";
import type { KnowledgeProcessOverrides } from "@/lib/api/knowledge";
import { useT } from "@/lib/i18n";
import { ICON_BTN_CLS, normalizeFileType } from "./timeline-helpers";

/* One attempt tab of the header. */
export interface TimelineAttemptTab {
  n: number;
  status: string;
  active: boolean;
}

/* The transient message below the header (cancel / retry feedback). */
export interface TimelineNotice {
  kind: "ok" | "err";
  text: string;
}

/**
 * Everything the header renders. The coordinator builds this object once
 * per render and hands it over as the component's only prop.
 */
export interface TimelineHeaderView {
  docTitle: string;
  knowledgeId: string;
  hasData: boolean;
  parseStatus?: string;
  lastError: { error_code: string; error_message: string } | null;
  headerStatus: string;
  headerStatusText: string;
  isLive: boolean;
  processOverrides: KnowledgeProcessOverrides | null;
  currentFileType: string;
  loading: boolean;
  refreshing: boolean;
  canCancelParse: boolean;
  cancelling: boolean;
  confirmCancel: boolean;
  notice: TimelineNotice | null;
  headMetaParts: string[];
  attemptTabs: TimelineAttemptTab[];
  showClose: boolean;
  onClose?: () => void;
  setConfirmCancel: Dispatch<SetStateAction<boolean>>;
  onManualRefresh: () => void;
  onCancelParseConfirm: () => void;
  onRetry: () => void;
  onAttemptChange: (n: number) => void;
}

function attemptGlyph(status: string): string {
  switch (status) {
    case "done":
      return "✓";
    case "failed":
      return "✗";
    case "running":
    case "pending":
    case "processing":
      return "●";
    default:
      return "–";
  }
}

function attemptGlyphCls(status: string): string {
  switch (status) {
    case "done":
      return "text-success";
    case "failed":
      return "text-error";
    case "running":
    case "pending":
    case "processing":
      return "text-running kp-live-pulse";
    default:
      return "text-muted-soft";
  }
}

function formatParserRulesForCurrentFile(
  rules: Array<{ file_types?: string[]; engine?: string }>,
  currentFileType: string,
): string {
  if (currentFileType) {
    const matched = rules.find((rule) =>
      (rule.file_types || []).some((ft) => normalizeFileType(ft) === currentFileType),
    );
    if (matched?.engine) return `${currentFileType}→${matched.engine}`;
  }
  return rules.map((r) => `${(r.file_types || []).join("/")}→${r.engine}`).join(", ");
}

/**
 * The timeline header: title, status chip, LIVE badge, the attempt tabs,
 * the parse-config popover and the refresh / cancel / retry actions.
 */
export function TimelineHeader({ view }: { view: TimelineHeaderView }) {
  const { t } = useT();
  const {
    docTitle,
    knowledgeId,
    hasData,
    parseStatus,
    lastError,
    headerStatus,
    headerStatusText,
    isLive,
    loading,
    refreshing,
    canCancelParse,
    cancelling,
    confirmCancel,
    notice,
    headMetaParts,
    attemptTabs,
    showClose,
    onClose,
    setConfirmCancel,
    onManualRefresh,
    onCancelParseConfirm,
    onRetry,
    onAttemptChange,
  } = view;
  const iconBtn = ICON_BTN_CLS;

  /* Human-readable summary of the per-upload parse overrides stored in
   * knowledge.metadata.process_overrides; empty → KB defaults. */
  const processConfigLines = useMemo<string[]>(() => {
    const o = view.processOverrides;
    const onOff = (v: boolean) => (v ? t("ks.proc.on") : t("ks.proc.off"));
    if (!o) return [t("ks.proc.kbDefault")];
    const lines: string[] = [];

    const cc = o.chunking_config;
    if (cc) {
      const parts: string[] = [];
      if (cc.chunk_size != null) parts.push(t("ks.proc.chunkSize", { n: cc.chunk_size }));
      if (cc.enable_parent_child != null) {
        parts.push(cc.enable_parent_child ? t("ks.proc.parentChildOn") : t("ks.proc.parentChildOff"));
      }
      if (parts.length) lines.push(`${t("ks.proc.chunking")}: ${parts.join(" · ")}`);
    }

    const rules = o.parser_engine_rules || cc?.parser_engine_rules;
    if (rules?.length) {
      lines.push(`${t("ks.proc.parser")}: ${formatParserRulesForCurrentFile(rules, view.currentFileType)}`);
    }

    const mm = o.vlm_config?.enabled ?? o.enable_multimodel;
    if (mm != null) lines.push(`${t("ks.proc.multimodal")}: ${onOff(mm)}`);

    if (o.asr_config?.enabled != null) lines.push(`${t("ks.proc.asr")}: ${onOff(o.asr_config.enabled)}`);

    const qg = o.question_generation_config;
    if (o.summary_enabled != null) lines.push(`${t("ks.proc.summary")}: ${onOff(o.summary_enabled)}`);
    if (qg?.enabled != null) {
      lines.push(`${t("ks.proc.question")}: ${qg.enabled ? t("ks.proc.questionOn", { n: qg.question_count ?? 3 }) : t("ks.proc.off")}`);
    }

    const graph = o.graph_enabled ?? o.extract_config?.enabled;
    if (graph != null) lines.push(`${t("ks.proc.graph")}: ${onOff(graph)}`);

    return lines.length ? lines : [t("ks.proc.kbDefault")];
  }, [view.processOverrides, view.currentFileType, t]);

  return (
    <div className="shrink-0 border-b border-hairline bg-surface-card px-5 pb-2.5 pt-3.5">
      <div className="flex min-w-0 items-center gap-2">
        <h2 className="min-w-0 flex-1 truncate text-[15px] font-semibold leading-[1.35] text-ink" title={docTitle || t("ks.title")}>
          {docTitle || t("ks.title")}
        </h2>
        {hasData && headerStatusText && (
          <span className={`shrink-0 rounded-md px-2 py-0.5 text-[11px] font-medium ${CHIP_CLS[headerStatus] ?? "bg-surface-strong text-muted"}`}>
            {headerStatusText}
          </span>
        )}
        {isLive && (
          <span
            className="inline-flex shrink-0 items-center gap-[5px] rounded-md bg-running/15 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.06em] text-running"
            title={t("ks.liveTooltip")}
          >
            <span className="kp-live-pulse h-1.5 w-1.5 rounded-full bg-running" />
            <span className="font-mono">{t("ks.live")}</span>
          </span>
        )}
        <div className="ml-auto flex shrink-0 items-center gap-1">
          {/* Parse config popover (hover) */}
          <div className="group relative">
            <button
              type="button"
              className={iconBtn}
              title={t("ks.proc.title")}
              aria-label={t("ks.proc.title")}
            >
              <IconInfoCircle className="h-3.5 w-3.5" />
            </button>
            <div className="pointer-events-none absolute right-0 top-full z-20 mt-1 hidden w-max max-w-[340px] rounded-lg border border-hairline bg-surface-card p-3 shadow-[0_8px_30px_rgba(0,0,0,0.12)] group-hover:block">
              <div className="mb-1 text-[12px] font-semibold text-ink">{t("ks.proc.title")}</div>
              {processConfigLines.map((line, i) => (
                <div key={i} className="caption text-muted">
                  {line}
                </div>
              ))}
            </div>
          </div>
          <button
            type="button"
            className={iconBtn}
            disabled={loading || refreshing}
            title={isLive ? t("ks.autoRefreshOn") : t("ks.refresh")}
            aria-label={isLive ? t("ks.autoRefreshOn") : t("ks.refresh")}
            onClick={() => void onManualRefresh()}
          >
            <IconRefresh
              className={`h-3.5 w-3.5 ${
                refreshing
                  ? "animate-spin"
                  : isLive
                    ? "animate-[spin_4s_linear_infinite] text-running"
                    : ""
              }`}
            />
          </button>
          {canCancelParse && (
            <button
              type="button"
              className={`${iconBtn} hover:bg-error/10 hover:text-error`}
              disabled={cancelling}
              title={t("doc.cancelParse")}
              aria-label={t("doc.cancelParse")}
              onClick={() => setConfirmCancel((v) => !v)}
            >
              {cancelling ? (
                <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-hairline-strong border-t-error" />
              ) : (
                <IconMinusCircle className="h-[15px] w-[15px]" />
              )}
            </button>
          )}
          {parseStatus === "failed" && (
            <button
              type="button"
              className="btn btn-outline btn-sm h-7"
              onClick={() => void onRetry()}
            >
              <IconRefresh className="h-3.5 w-3.5" />
              {t("ks.retry")}
            </button>
          )}
          {showClose && (
            <button
              type="button"
              className={iconBtn}
              aria-label={t("ks.close")}
              title={t("ks.close")}
              onClick={onClose}
            >
              <IconClose className="h-4 w-4" />
            </button>
          )}
        </div>
      </div>

      {confirmCancel && canCancelParse && (
        <div className="mt-2 flex items-center gap-2 rounded-lg border border-warning/40 bg-warning-surface px-3 py-2">
          <p className="caption min-w-0 flex-1 text-body">
            {t("doc.cancelParseConfirm", { fileName: docTitle || knowledgeId })}
          </p>
          <button type="button" className="btn btn-outline btn-sm h-7" onClick={() => setConfirmCancel(false)} disabled={cancelling}>
            {t("common.cancel")}
          </button>
          <button
            type="button"
            className="btn btn-primary btn-sm h-7 border-error bg-error hover:border-error hover:bg-error"
            onClick={() => void onCancelParseConfirm()}
            disabled={cancelling}
          >
            {t("doc.cancelParse")}
          </button>
        </div>
      )}
      {notice && (
        <p className={`caption mt-1.5 ${notice.kind === "err" ? "text-error" : "text-success"}`}>
          {notice.text}
        </p>
      )}

      {headMetaParts.length > 0 && (
        <p className="caption mb-0 mt-2 break-words text-muted">
          {headMetaParts.map((part, idx) => (
            <span key={idx}>
              {idx > 0 && <span className="mx-1.5 text-muted-soft">·</span>}
              {part}
            </span>
          ))}
        </p>
      )}

      {attemptTabs.length > 0 && (
        <div className="mt-2.5 flex gap-1.5 overflow-x-auto pb-0.5">
          {attemptTabs.map((tab) => (
            <button
              key={tab.n}
              type="button"
              onClick={() => onAttemptChange(tab.n)}
              className={`inline-flex items-center gap-[5px] whitespace-nowrap rounded-md border px-2.5 py-1 caption transition-colors ${
                tab.active
                  ? "border-ink bg-ink text-on-primary"
                  : "border-hairline-strong bg-surface-card text-muted hover:bg-surface-strong hover:text-ink"
              }`}
            >
              <span className="font-mono text-[11px] font-semibold">#{tab.n}</span>
              <span className={`text-[9px] leading-none ${tab.active ? "text-on-primary" : attemptGlyphCls(tab.status)}`}>
                {attemptGlyph(tab.status)}
              </span>
            </button>
          ))}
        </div>
      )}

      {lastError && parseStatus === "failed" && (
        <div className="mt-2.5 flex overflow-hidden rounded-lg border border-error/30 bg-error/10" role="alert">
          <div className="w-[3px] shrink-0 bg-error" />
          <div className="min-w-0 flex-1 px-3.5 py-2.5">
            <div className="mb-1 flex items-center gap-2">
              <span className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-error text-[11px] font-bold text-white">
                !
              </span>
              <span className="caption font-semibold text-error">{lastError.error_code || t("ks.detail.error")}</span>
              {lastError.error_code && (
                <span className="ml-auto rounded bg-error px-1.5 py-px font-mono text-[10px] text-white">
                  {lastError.error_code}
                </span>
              )}
            </div>
            <div className="caption mb-1 text-muted">{t("ks.errUnknownSuggestion")}</div>
            {lastError.error_message && (
              <div className="whitespace-pre-wrap break-words font-mono text-[11px] text-muted-soft">
                {lastError.error_message}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
