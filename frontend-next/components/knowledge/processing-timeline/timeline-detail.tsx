"use client";

import { useMemo, type Dispatch, type ReactNode, type SetStateAction } from "react";
import { IconCheck, IconClose, IconCopy } from "@/components/icons";
import { CHIP_CLS, barCls, dotCls } from "@/components/knowledge/trace-status";
import { useT, type LocaleKey } from "@/lib/i18n";
import {
  buildKvEntries,
  formatDuration,
  formatSpanDuration,
  formatTime,
  ICON_BTN_CLS,
  isObjectWithKeys,
  isPlaceholder,
  localizedStatus as localizedStatusFor,
  prettyJSON,
  rowKindLabel,
  rowLabel as rowLabelFor,
  stageLabelFor,
  type FlatRow,
  type SpanNode,
} from "./timeline-helpers";

/* The tabs of the detail panel. */
export type TimelineDetailTab = "overview" | "input" | "output" | "metadata" | "raw";

/* One row of the identity / lineage table. */
export interface TimelineIdentityField {
  key: string;
  label: string;
  value: string;
  mono: boolean;
  copyable: boolean;
}

/* One stage of the stage-breakdown bars. */
export interface TimelineStageBreakdownEntry {
  name: string;
  label: string;
  status: string;
  duration_ms?: number;
  pct: number;
}

/**
 * Everything the detail panel renders: the selected span, its tabs and
 * the derived identity / stage-breakdown data.
 */
export interface TimelineDetailView {
  detailOpen: boolean;
  selectedRow: FlatRow | null;
  detailTab: TimelineDetailTab;
  setDetailTab: Dispatch<SetStateAction<TimelineDetailTab>>;
  tabHasContent: (tab: "input" | "output" | "metadata") => boolean;
  copiedKey: string | null;
  copyValue: (value: unknown, flashKey: string) => void;
  toggleJsonKey: (section: string, key: string) => void;
  isJsonExpanded: (section: string, key: string) => boolean;
  setSelectedSpanId: Dispatch<SetStateAction<string | null>>;
  stages: SpanNode[];
  totalMs: number;
  traceMetadata: unknown;
  traceKnowledgeId?: string;
  currentAttempt?: number;
  barOffsetMs: (node: SpanNode) => number;
  liveElapsedMs: (node: SpanNode) => number;
}

/**
 * The detail panel below the waterfall: timing, identity, trace metadata,
 * the stage breakdown and the raw payload of the selected span.
 */
export function TimelineDetail({ view }: { view: TimelineDetailView }) {
  const { t } = useT();
  const {
    detailOpen,
    selectedRow,
    detailTab,
    setDetailTab,
    tabHasContent,
    copiedKey,
    copyValue,
    toggleJsonKey,
    isJsonExpanded,
    setSelectedSpanId,
    stages,
    totalMs,
    traceMetadata,
    traceKnowledgeId,
    currentAttempt,
    barOffsetMs,
    liveElapsedMs,
  } = view;
  const iconBtn = ICON_BTN_CLS;
  const rowLabel = (row: FlatRow) => rowLabelFor(row, t);
  const localizedStatus = (status: string) => localizedStatusFor(status, t);

  /* Identity / lineage of the selected span. */
  const identityFields = (row: FlatRow): TimelineIdentityField[] => {
    const out: TimelineIdentityField[] = [];
    const node = row.node;
    out.push({ key: "name", label: t("ks.detail.name"), value: rowLabel(row), mono: false, copyable: false });
    out.push({ key: "kind", label: t("ks.detail.kind"), value: rowKindLabel(row), mono: true, copyable: false });
    out.push({ key: "status", label: t("ks.detail.status"), value: localizedStatus(row.node.status), mono: false, copyable: false });
    if (row.isStage) {
      const idx = stages.findIndex((s) => s.name === row.node.name);
      if (idx >= 0) {
        out.push({ key: "stageIndex", label: t("ks.detail.stageOrder"), value: `${idx + 1} / ${stages.length}`, mono: true, copyable: false });
      }
    }
    if (row.hasChildren) {
      out.push({ key: "children", label: t("ks.detail.childCount"), value: String((row.node.children || []).length), mono: true, copyable: false });
    }
    if (node.span_id) out.push({ key: "span_id", label: "span_id", value: node.span_id, mono: true, copyable: true });
    if (node.parent_span_id) out.push({ key: "parent_span_id", label: "parent_span_id", value: node.parent_span_id, mono: true, copyable: true });
    if (traceKnowledgeId) out.push({ key: "knowledge_id", label: "knowledge_id", value: traceKnowledgeId, mono: true, copyable: true });
    if (currentAttempt) out.push({ key: "attempt", label: t("ks.head.attempt"), value: `#${currentAttempt}`, mono: true, copyable: false });
    return out;
  };

  /* Duration share of every stage relative to the whole trace. */
  const stageBreakdown = useMemo<TimelineStageBreakdownEntry[]>(() => {
    const total = totalMs || 1;
    return stages.map((s) => ({
      name: s.name,
      label: stageLabelFor(s.name, t),
      status: s.status,
      duration_ms: s.duration_ms,
      pct:
        typeof s.duration_ms === "number" && s.duration_ms > 0
          ? Math.min(100, (s.duration_ms / total) * 100)
          : 0,
    }));
  }, [stages, totalMs, t]);

  return (
    <div
      className={`flex shrink-0 flex-col overflow-hidden border-t bg-surface-card transition-[height] duration-200 ${
        detailOpen ? "h-1/2 min-h-[320px] border-hairline" : "h-0 border-transparent"
      }`}
    >
      {selectedRow && (
        <>
          <div className="flex shrink-0 items-center justify-between gap-2 border-b border-hairline px-5 pb-2.5 pt-3">
            <div className="flex min-w-0 items-center gap-2">
              <span className={`h-2 w-2 shrink-0 rounded-full ${dotCls(selectedRow.node.status)}`} />
              <span className="truncate text-[14px] font-semibold text-ink">{rowLabel(selectedRow)}</span>
              <span className="shrink-0 font-mono text-[10px] uppercase tracking-[0.5px] text-muted-soft">
                {rowKindLabel(selectedRow)}
              </span>
              <span className={`inline-flex shrink-0 items-center rounded-md px-2 py-px text-[11px] font-medium ${CHIP_CLS[selectedRow.node.status] ?? "bg-surface-strong text-muted"}`}>
                {localizedStatus(selectedRow.node.status)}
              </span>
            </div>
            <div className="flex items-center gap-1">
              <button
                type="button"
                className={iconBtn}
                title={t("ks.copyDetails")}
                onClick={(e) => {
                  e.stopPropagation();
                  void copyValue(selectedRow.node, "detail");
                }}
              >
                {copiedKey === "detail" ? (
                  <IconCheck className="h-4 w-4 text-success" />
                ) : (
                  <IconCopy className="h-4 w-4" />
                )}
              </button>
              <button type="button" className={iconBtn} title={t("ks.close")} onClick={() => setSelectedSpanId(null)}>
                <IconClose className="h-4 w-4" />
              </button>
            </div>
          </div>

          <div className="flex shrink-0 border-b border-hairline bg-surface-card px-5">
            {(
              [
                ["overview", t("ks.tab.overview"), true],
                ["input", t("ks.detail.input"), tabHasContent("input")],
                ["output", t("ks.detail.output"), tabHasContent("output")],
                ["metadata", t("ks.detail.metadata"), tabHasContent("metadata")],
                ["raw", t("ks.tab.raw"), true],
              ] as const
            )
              .filter(([key, , has]) => key !== "metadata" || has)
              .map(([key, label, has]) => (
                <button
                  key={key}
                  type="button"
                  onClick={() => setDetailTab(key)}
                  className={`relative px-3.5 pb-2.5 pt-[9px] text-[13.5px] transition-colors ${
                    detailTab === key
                      ? "font-semibold text-ink after:absolute after:inset-x-3.5 after:-bottom-px after:h-0.5 after:rounded-t after:bg-ink"
                      : has
                        ? "text-muted hover:text-ink"
                        : "text-muted-soft hover:text-ink"
                  }`}
                >
                  {label}
                </button>
              ))}
          </div>

          <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
            {detailTab === "overview" && (
              <>
                {/* Timing */}
                <div className="flex flex-col gap-2">
                  <div className="text-[11px] font-medium uppercase tracking-[0.5px] text-muted">
                    {t("ks.detail.timing")}
                  </div>
                  <KvTable>
                    <KvRow k={t("ks.detail.started")} v={formatTime(selectedRow.node.started_at)} mono />
                    <KvRow
                      k={t("ks.detail.finished")}
                      mono
                      v={
                        selectedRow.node.status === "running" ? (
                          <span className="font-sans text-[11px] italic text-running">
                            {t("ks.detail.inProgress")}
                          </span>
                        ) : (
                          formatTime(selectedRow.node.finished_at)
                        )
                      }
                    />
                    <KvRow
                      k={t("ks.detail.duration")}
                      mono
                      v={
                        selectedRow.node.status === "running" ? (
                          <>
                            {formatDuration(liveElapsedMs(selectedRow.node))}
                            <span className="ml-1.5 inline-block rounded bg-running/15 px-1.5 text-[9px] font-semibold uppercase tracking-[0.5px] text-running">
                              {t("ks.detail.elapsed")}
                            </span>
                          </>
                        ) : (
                          formatSpanDuration(selectedRow.node)
                        )
                      }
                    />
                    {!selectedRow.isRoot && barOffsetMs(selectedRow.node) > 0 && (
                      <KvRow k={t("ks.detail.offset")} mono v={`+${formatDuration(barOffsetMs(selectedRow.node))}`} />
                    )}
                  </KvTable>
                </div>

                {/* Identity / lineage */}
                <div className="flex flex-col gap-2">
                  <div className="text-[11px] font-medium uppercase tracking-[0.5px] text-muted">
                    {t("ks.detail.identity")}
                  </div>
                  <KvTable>
                    {identityFields(selectedRow).map((entry) => (
                      <KvRow
                        key={entry.key}
                        k={entry.label}
                        mono={entry.mono}
                        truncate={entry.copyable}
                        v={
                          <>
                            <span className={entry.copyable ? "min-w-0 flex-1 truncate" : ""}>{entry.value}</span>
                            {entry.copyable && (
                              <button
                                type="button"
                                title={t("ks.copy")}
                                onClick={(e) => {
                                  e.stopPropagation();
                                  void copyValue(entry.value, `id:${entry.key}`);
                                }}
                                className="inline-flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded text-muted-soft hover:bg-surface-strong hover:text-ink"
                              >
                                {copiedKey === `id:${entry.key}` ? (
                                  <IconCheck className="h-3.5 w-3.5 text-success" />
                                ) : (
                                  <IconCopy className="h-3.5 w-3.5" />
                                )}
                              </button>
                            )}
                          </>
                        }
                      />
                    ))}
                  </KvTable>
                </div>

                {traceMetadata != null && (
                  <div className="flex flex-col gap-2">
                    <div className="text-[11px] font-medium uppercase tracking-[0.5px] text-muted">
                      {t("ks.detail.traceMetadata")}
                    </div>
                    <p className="caption m-0 text-muted-soft">{t("ks.detail.metadataHint")}</p>
                    <KvTable>
                      {buildKvEntries(traceMetadata).map((entry) => (
                        <KvRow key={entry.key} k={entry.key} monoKey v={entry.display} multiline />
                      ))}
                    </KvTable>
                  </div>
                )}

                {/* Stage breakdown (root only) */}
                {selectedRow.isRoot && (
                  <div className="flex flex-col gap-2">
                    <div className="text-[11px] font-medium uppercase tracking-[0.5px] text-muted">
                      {t("ks.detail.stageBreakdown")}
                    </div>
                    <div className="flex flex-col gap-1.5 rounded-lg border border-hairline bg-surface-card px-3 py-2.5">
                      {stageBreakdown.map((s) => (
                        <div key={s.name} className="grid grid-cols-[110px_1fr_64px] items-center gap-2.5 text-[13px]">
                          <span className="inline-flex items-center gap-1.5 text-ink">
                            <span className={`h-[7px] w-[7px] rounded-full ${dotCls(s.status)}`} />
                            {s.label}
                          </span>
                          <div className="relative h-1.5 overflow-hidden rounded-sm bg-surface-strong">
                            {s.status !== "pending" && (
                              <div
                                className={`absolute inset-y-0 left-0 rounded-sm transition-[width] duration-700 ${barCls(s.status)}`}
                                style={{ width: `${s.pct}%` }}
                              />
                            )}
                          </div>
                          <span className="text-right font-mono text-[11px] text-muted">
                            {s.status === "skipped" || s.status === "pending" ? "—" : formatDuration(s.duration_ms)}
                          </span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {/* Error */}
                {(selectedRow.node.status === "failed" || selectedRow.node.status === "cancelled") &&
                  (selectedRow.node.error_code || selectedRow.node.error_message) && (
                    <div className="flex flex-col gap-2 rounded-lg border border-error/30 bg-error/10 px-3 py-2.5">
                      <div className="flex items-center gap-2">
                        <span className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-error text-[11px] font-bold text-white">
                          !
                        </span>
                        <span className="caption font-semibold text-error">
                          {selectedRow.node.error_code || t("ks.detail.error")}
                        </span>
                        {selectedRow.node.error_code && (
                          <span className="ml-auto rounded bg-error px-1.5 py-px font-mono text-[10px] text-white">
                            {selectedRow.node.error_code}
                          </span>
                        )}
                      </div>
                      {selectedRow.node.error_message && (
                        <pre className="m-0 max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-md border border-hairline bg-surface-card px-2.5 py-2 font-mono text-[11px] text-muted">
                          {selectedRow.node.error_message}
                        </pre>
                      )}
                    </div>
                  )}

                {isPlaceholder(selectedRow.node) && (
                  <div className="caption rounded-lg border-l-2 border-hairline-strong bg-surface-strong px-3 py-2.5 text-muted">
                    {t("ks.detail.placeholderHint")}
                  </div>
                )}
              </>
            )}

            {(detailTab === "input" || detailTab === "output" || detailTab === "metadata") && (
              <>
                {!tabHasContent(detailTab) ? (
                  <div className="flex items-center justify-center py-12 text-muted-soft">
                    <span>{detailTab === "metadata" ? t("ks.detail.metadataEmpty") : t("ks.detail.empty")}</span>
                  </div>
                ) : (
                  <div className="flex flex-col gap-2">
                    <div className="flex items-center justify-between gap-2">
                      <span className="text-[11px] font-medium uppercase tracking-[0.5px] text-muted">
                        {t(`ks.detail.${detailTab}` as LocaleKey)}
                      </span>
                      <button
                        type="button"
                        className="inline-flex items-center gap-1 rounded-md border border-hairline-strong bg-surface-card px-2 py-[3px] text-[11px] text-muted transition-colors hover:bg-ink hover:text-on-primary"
                        onClick={() => void copyValue(selectedRow.node[detailTab], `sec:${detailTab}`)}
                      >
                        {copiedKey === `sec:${detailTab}` ? (
                          <IconCheck className="h-3.5 w-3.5 text-success" />
                        ) : (
                          <IconCopy className="h-3.5 w-3.5" />
                        )}
                        <span>{copiedKey === `sec:${detailTab}` ? t("ks.copied") : t("ks.copy")}</span>
                      </button>
                    </div>

                    {isObjectWithKeys(selectedRow.node[detailTab]) ? (
                      <KvTable>
                        {buildKvEntries(selectedRow.node[detailTab]).map((entry) => (
                          <KvRow key={entry.key} k={entry.key} monoKey multiline v={
                            entry.kind === "bool" ? (
                              <span className={`font-mono text-[11px] font-medium ${entry.raw ? "text-success" : "text-error"}`}>
                                {entry.display}
                              </span>
                            ) : entry.kind === "scalar" ? (
                              <span className="text-[13px]">{entry.display}</span>
                            ) : entry.defaultExpanded ? (
                              <div className="flex min-w-0 flex-col gap-1">
                                <span className="font-mono text-[11px] text-muted-soft">{entry.display}</span>
                                <pre className="m-0 max-h-[360px] overflow-auto whitespace-pre-wrap break-words rounded-lg border border-hairline bg-surface-strong px-3 py-2.5 font-mono text-[11px] leading-[1.6] text-ink">
                                  {prettyJSON(entry.raw)}
                                </pre>
                              </div>
                            ) : (
                              <div className="flex min-w-0 flex-col gap-1.5">
                                <button
                                  type="button"
                                  className="flex flex-wrap items-baseline gap-2 text-left"
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    toggleJsonKey(detailTab, entry.key);
                                  }}
                                >
                                  <span className="font-mono text-[11px] text-muted">{entry.display}</span>
                                  <span className="text-[11px] font-medium text-ink underline-offset-2 hover:underline">
                                    {isJsonExpanded(detailTab, entry.key)
                                      ? t("ks.detail.hideJson")
                                      : t("ks.detail.showJson")}
                                  </span>
                                </button>
                                {isJsonExpanded(detailTab, entry.key) && (
                                  <pre className="m-0 max-h-[360px] overflow-auto whitespace-pre-wrap break-words rounded-lg border border-hairline bg-surface-strong px-3 py-2.5 font-mono text-[11px] leading-[1.6] text-ink">
                                    {prettyJSON(entry.raw)}
                                  </pre>
                                )}
                              </div>
                            )
                          } />
                        ))}
                      </KvTable>
                    ) : (
                      <pre className="m-0 max-h-[360px] overflow-auto whitespace-pre-wrap break-words rounded-lg border border-hairline bg-surface-strong px-3 py-2.5 font-mono text-[11px] leading-[1.6] text-ink">
                        {prettyJSON(selectedRow.node[detailTab])}
                      </pre>
                    )}
                  </div>
                )}
              </>
            )}

            {detailTab === "raw" && (
              <div className="flex flex-col gap-2">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-[11px] font-medium uppercase tracking-[0.5px] text-muted">
                    {t("ks.tab.raw")}
                  </span>
                  <button
                    type="button"
                    className="inline-flex items-center gap-1 rounded-md border border-hairline-strong bg-surface-card px-2 py-[3px] text-[11px] text-muted transition-colors hover:bg-ink hover:text-on-primary"
                    onClick={() => void copyValue(selectedRow.node, "raw")}
                  >
                    {copiedKey === "raw" ? (
                      <IconCheck className="h-3.5 w-3.5 text-success" />
                    ) : (
                      <IconCopy className="h-3.5 w-3.5" />
                    )}
                    <span>{copiedKey === "raw" ? t("ks.copied") : t("ks.copy")}</span>
                  </button>
                </div>
                <pre className="m-0 max-h-[480px] overflow-auto whitespace-pre-wrap break-words rounded-lg border border-hairline bg-surface-strong px-3 py-2.5 font-mono text-[11px] leading-[1.6] text-ink">
                  {prettyJSON(selectedRow.node)}
                </pre>
              </div>
            )}
          </div>
        </>
      )}
    </div>
  );
}

function KvTable({ children }: { children: ReactNode }) {
  return (
    <div className="flex flex-col overflow-hidden rounded-lg border border-hairline bg-surface-card">
      {children}
    </div>
  );
}

function KvRow({
  k,
  v,
  mono = false,
  monoKey = false,
  multiline = false,
  truncate = false,
}: {
  k: string;
  v: ReactNode;
  mono?: boolean;
  monoKey?: boolean;
  multiline?: boolean;
  truncate?: boolean;
}) {
  return (
    <div
      className={`grid grid-cols-[130px_1fr] gap-3 px-3 py-2 text-[13px] [&+&]:border-t [&+&]:border-hairline-soft ${
        multiline ? "items-start" : "items-center"
      }`}
    >
      <span className={`truncate text-[11px] font-medium text-muted ${monoKey ? "font-mono" : ""}`}>{k}</span>
      <span
        className={`inline-flex min-w-0 items-center gap-1.5 break-words text-ink ${
          mono ? "font-mono text-[11px]" : ""
        } ${multiline ? "flex-col items-stretch gap-1" : ""} ${truncate ? "overflow-hidden" : ""}`}
      >
        {v}
      </span>
    </div>
  );
}
