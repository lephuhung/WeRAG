"use client";

import type { CSSProperties, MouseEvent as ReactMouseEvent, RefObject } from "react";
import { IconChevronDown, IconChevronRight } from "@/components/icons";
import { WRAP_CLS, barCls, dotCls } from "@/components/knowledge/trace-status";
import { useT } from "@/lib/i18n";
import {
  formatDuration,
  formatSpanDuration,
  isPlaceholder,
  localizedStatus as localizedStatusFor,
  rowKindLabel,
  rowLabel as rowLabelFor,
  type FlatRow,
  type SpanNode,
} from "./timeline-helpers";

/**
 * Everything the waterfall renders: the time ruler, the flattened span
 * rows and the geometry the bars are drawn with.
 */
export interface TimelineRowsView {
  loading: boolean;
  hasData: boolean;
  showRuler: boolean;
  rulerTicks: { left: string; label: string }[];
  scrollRef: RefObject<HTMLDivElement | null>;
  flatRows: FlatRow[];
  expandedRows: Set<string>;
  selectedSpanId: string | null;
  nowMarkerPct: number | null;
  barStyle: (node: SpanNode) => CSSProperties;
  wrapStyle: (node: SpanNode) => CSSProperties | null;
  barOffsetPct: (node: SpanNode) => number | null;
  barOffsetMs: (node: SpanNode) => number;
  wrapDurationMs: (node: SpanNode) => number;
  liveElapsedMs: (node: SpanNode) => number;
  selectRow: (row: FlatRow) => void;
  toggleTree: (row: FlatRow, ev?: ReactMouseEvent) => void;
}

/**
 * The waterfall body: the ruler stays outside the scroll region so the
 * time axis never scrolls away, the rows themselves scroll.
 */
export function TimelineRows({ view }: { view: TimelineRowsView }) {
  const { t } = useT();
  const {
    loading,
    hasData,
    showRuler,
    rulerTicks,
    scrollRef,
    flatRows,
    expandedRows,
    selectedSpanId,
    nowMarkerPct,
    barStyle,
    wrapStyle,
    barOffsetPct,
    barOffsetMs,
    wrapDurationMs,
    liveElapsedMs,
    selectRow,
    toggleTree,
  } = view;
  const rowLabel = (row: FlatRow) => rowLabelFor(row, t);
  const localizedStatus = (status: string) => localizedStatusFor(status, t);

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden bg-surface-card">
      {loading && !hasData ? (
        <div className="flex flex-1 items-center justify-center gap-2 px-5 py-14 text-muted-soft">
          <span className="h-4 w-4 animate-spin rounded-full border-2 border-hairline-strong border-t-ink" />
        </div>
      ) : !hasData ? (
        <div className="flex flex-1 items-center justify-center px-5 py-14 text-muted-soft">
          <span>{t("ks.noActivity")}</span>
        </div>
      ) : (
        <>
          {/* Ruler stays outside the scroll region so the time axis
           * never scrolls away. */}
          {showRuler && (
            <div className="grid h-6 shrink-0 grid-cols-[minmax(220px,42%)_64px_1fr] items-end border-b border-dashed border-hairline px-5 pb-1.5 pt-3">
              <div />
              <div />
              <div className="relative mr-4 h-full">
                {rulerTicks.map((tick, i) => (
                  <span
                    key={i}
                    className={`absolute bottom-0 flex flex-col items-center text-[11px] text-muted ${
                      i === 0
                        ? "translate-x-0 items-start"
                        : i === rulerTicks.length - 1
                          ? "-translate-x-full items-end"
                          : "-translate-x-1/2"
                    }`}
                    style={{ left: tick.left }}
                  >
                    <span className="h-[5px] w-px bg-hairline-strong" />
                    <span className="mt-0.5 font-mono tracking-[0.02em]">{tick.label}</span>
                  </span>
                ))}
              </div>
            </div>
          )}

          <div ref={scrollRef} className="min-h-0 flex-1 overflow-auto pb-4">
            <div className="flex flex-col">
              {flatRows.map((row) => {
                const wrap = wrapStyle(row.node);
                const offsetPct = barOffsetPct(row.node);
                const offsetMs = barOffsetMs(row.node);
                return (
                  <div
                    key={row.key}
                    data-span-key={row.key}
                    role="button"
                    tabIndex={0}
                    title={row.hasChildren && !row.isRoot ? t("ks.rowSelectHint") : undefined}
                    onClick={() => selectRow(row)}
                    onKeyDown={(e) => {
                      /* The expand button inside the row handles its own keys. */
                      if (e.target !== e.currentTarget) return;
                      if (e.key !== "Enter" && e.key !== " ") return;
                      e.preventDefault();
                      selectRow(row);
                    }}
                    className={`group/kp relative grid h-8 cursor-pointer grid-cols-[minmax(220px,42%)_64px_1fr] items-center px-5 transition-colors outline-none focus-visible:bg-surface-strong ${
                      selectedSpanId === row.key
                        ? "bg-surface-strong"
                        : row.isStage
                          ? "bg-surface-strong/55 hover:bg-surface-strong"
                          : !row.isRoot
                            ? "hover:bg-surface-strong"
                            : "hover:bg-surface-strong"
                    }`}
                  >
                    {selectedSpanId === row.key && (
                      <span className="absolute bottom-1 left-0 top-1 w-0.5 rounded-r bg-ink" />
                    )}

                    {/* name cell */}
                    <div className="min-w-0">
                      <div className="flex min-w-0 items-center gap-[7px]" style={{ paddingLeft: row.depth * 16 }}>
                        {row.hasChildren && !row.isRoot ? (
                          <button
                            type="button"
                            aria-expanded={expandedRows.has(row.key)}
                            aria-label={expandedRows.has(row.key) ? t("ks.collapseBranch") : t("ks.expandBranch")}
                            onClick={(e) => toggleTree(row, e)}
                            className="-my-[3px] inline-flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-md text-muted-soft transition-colors hover:bg-surface-card hover:text-ink"
                          >
                            {expandedRows.has(row.key) ? (
                              <IconChevronDown className="h-3.5 w-3.5" />
                            ) : (
                              <IconChevronRight className="h-3.5 w-3.5" />
                            )}
                          </button>
                        ) : (
                          <span className="-my-[3px] inline-block h-[22px] w-[22px] shrink-0" />
                        )}
                        <span className={`h-[7px] w-[7px] shrink-0 rounded-full ${dotCls(row.node.status, isPlaceholder(row.node))}`} />
                        <span
                          className={`truncate text-[13px] text-ink ${
                            row.isRoot
                              ? "text-[14px] font-semibold"
                              : !row.isStage
                                ? "font-mono text-[11px]"
                                : ""
                          }`}
                        >
                          {rowLabel(row)}
                        </span>
                        <span className="ml-auto shrink-0 pl-2 font-mono text-[10px] uppercase tracking-[0.5px] text-muted-soft">
                          {rowKindLabel(row)}
                        </span>
                      </div>
                    </div>

                    {/* duration cell */}
                    <div className="pr-3 text-right font-mono text-[11px] tracking-[0.02em] text-muted">
                      {row.node.status === "running" ? (
                        <span className="font-semibold text-running">
                          {formatDuration(liveElapsedMs(row.node))}
                        </span>
                      ) : (
                        formatSpanDuration(row.node)
                      )}
                    </div>

                    {/* bar cell */}
                    <div className="relative mr-4 h-8">
                      {nowMarkerPct !== null && row.isRoot && (
                        <span
                          className="pointer-events-none absolute bottom-1 top-1 z-[1] w-px bg-running/65 transition-[left] duration-1000 ease-linear"
                          style={{ left: `${nowMarkerPct}%` }}
                        >
                          <span className="kp-live-pulse absolute -left-[3px] -top-0.5 h-[7px] w-[7px] rounded-full bg-running" />
                        </span>
                      )}
                      {isPlaceholder(row.node) ? (
                        <div className="absolute right-1 top-[13px] h-1.5 w-3.5 rounded-sm border border-dashed border-hairline-strong" />
                      ) : (
                        <>
                          {wrap && (
                            <div
                              className={`group/wrap absolute top-[9px] z-[1] h-3.5 min-w-[4px] rounded-sm border border-dashed transition-[left,width] duration-700 ${WRAP_CLS[row.node.status] ?? "border-hairline-strong"}`}
                              style={wrap}
                            >
                              <span className="pointer-events-none absolute -top-[26px] left-1/2 z-10 flex -translate-x-1/2 items-center gap-1 whitespace-nowrap rounded-md bg-ink px-2 py-1 text-[11px] text-on-primary opacity-0 transition-opacity group-hover/wrap:opacity-100">
                                <span className="font-medium">{rowLabel(row)}</span>
                                <span className="opacity-60">·</span>
                                <span className="font-mono">{formatDuration(wrapDurationMs(row.node))}</span>
                                <span className="opacity-60">·</span>
                                <span>{t("ks.detail.includingChildren")}</span>
                              </span>
                            </div>
                          )}
                          <div
                            className={`group/bar absolute top-3 z-[2] h-2 min-w-[2px] rounded-sm transition-[left,width,filter] duration-700 ${barCls(row.node.status)}`}
                            style={barStyle(row.node)}
                          >
                            <span className={`pointer-events-none absolute left-1/2 z-10 flex -translate-x-1/2 items-center gap-1 whitespace-nowrap rounded-md bg-ink px-2 py-1 text-[11px] text-on-primary opacity-0 transition-opacity group-hover/bar:opacity-100 ${row.isRoot ? "top-[calc(100%+8px)]" : "-top-[26px]"}`}>
                              <span className="font-medium">{rowLabel(row)}</span>
                              <span className="opacity-60">·</span>
                              <span className="font-mono">
                                {row.node.status === "running"
                                  ? formatDuration(liveElapsedMs(row.node))
                                  : formatSpanDuration(row.node)}
                              </span>
                              <span className="opacity-60">·</span>
                              <span>{localizedStatus(row.node.status)}</span>
                            </span>
                          </div>
                          {offsetPct !== null && offsetMs > 0 && (
                            <span
                              className="absolute bottom-0 overflow-hidden text-ellipsis whitespace-nowrap text-center font-mono text-[9px] leading-[11px] text-muted-soft opacity-0 transition-opacity group-hover/kp:opacity-100"
                              style={{
                                left: `clamp(0px, calc(${offsetPct}% - 45px), max(0px, calc(100% - 90px)))`,
                                width: "min(90px, 100%)",
                              }}
                              title={`+${formatDuration(offsetMs)}`}
                            >
                              +{formatDuration(offsetMs)}
                            </span>
                          )}
                        </>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        </>
      )}
    </div>
  );
}
