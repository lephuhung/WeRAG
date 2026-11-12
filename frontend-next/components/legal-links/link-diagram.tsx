"use client";

/* The provision-link diagram of one amended document A (layout:
 * lib/legal-link-graph.ts). Left: A's provisions; right: each amending
 * document B as a header with its provisions under it; one curve per link,
 * coloured by the change it makes, drawn by what the reviewer can do with
 * it. Hovering, focusing or tapping a provision shows its wording; clicking
 * a link (or a provision with links to decide) opens the confirm panel;
 * clicking a document header opens its panel (bulk approval). Every node and
 * link is reachable with Tab and opened with Enter. */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useT, type LocaleKey } from "@/lib/i18n";
import { provisionLabel } from "@/lib/legal-provision-label";
import { opColor } from "@/components/graph/legal-style";
import { LAYOUT, edgesOf, type AmenderGroup, type GraphEdge, type LinkGraph } from "@/lib/legal-link-graph";
import type { LegalLinkCandidate } from "@/lib/api/legal-link-candidates";
import { ProvisionTextBox } from "./provision-text";

const NODE_H = LAYOUT.nodeHeight;
const HEADER_H = LAYOUT.headerHeight;
const INDENT = 14;
const CARD_W = 340;
const MIN_W = 560;

type Anchor = { nodeId: string; side: "left" | "right"; y: number };

function clip(s: string, max: number): string {
  return s.length > max ? `${s.slice(0, Math.max(1, max - 1))}…` : s;
}

function edgeStyle(e: GraphEdge): { stroke: string; width: number; dash?: string; opacity: number } {
  switch (e.state) {
    case "pending":
      return { stroke: opColor(e.op), width: 2, opacity: 1 };
    case "blocked":
      return { stroke: "var(--color-error)", width: 1.5, dash: "5 4", opacity: 0.85 };
    case "approved":
      return { stroke: "var(--color-success)", width: 1.5, opacity: 0.55 };
    case "rejected":
      return { stroke: "var(--color-muted-soft)", width: 1.2, dash: "2 4", opacity: 0.6 };
  }
  return { stroke: "var(--color-muted-soft)", width: 1.2, opacity: 0.6 };
}

export function LinkDiagram({
  graph,
  cands,
  targetDocId,
  targetLabel,
  selectedEdge,
  onOpenEdge,
  onOpenGroup,
  onToggleGroup,
}: {
  graph: LinkGraph;
  cands: Map<number, LegalLinkCandidate>;
  targetDocId: string;
  targetLabel: string;
  selectedEdge?: number | null;
  onOpenEdge: (candidateId: number) => void;
  onOpenGroup: (groupId: string) => void;
  onToggleGroup: (groupId: string) => void;
}) {
  const { t, locale } = useT();
  const boxRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(800);
  const [hover, setHover] = useState<string | null>(null);
  const [anchor, setAnchor] = useState<Anchor | null>(null);
  const [pinned, setPinned] = useState(false);
  const hideTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    const el = boxRef.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) => setWidth(Math.max(MIN_W, Math.floor(entry.contentRect.width))));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const W = width;
  const leftW = Math.round(Math.min(240, Math.max(150, W * 0.26)));
  const rightW = Math.round(Math.min(320, Math.max(200, W * 0.34)));
  const rightX = W - rightW;
  const srcX = rightX + INDENT;
  const srcW = rightW - INDENT;

  const groupOf = useMemo(() => {
    const m = new Map<string, AmenderGroup>();
    for (const g of graph.groups) {
      m.set(g.id, g);
      for (const s of g.sources) m.set(s.id, g);
    }
    return m;
  }, [graph]);
  const sourceY = useMemo(() => {
    const m = new Map<string, number>();
    for (const g of graph.groups) {
      m.set(g.id, g.y + HEADER_H / 2);
      for (const s of g.sources) m.set(s.id, s.y + NODE_H / 2);
    }
    return m;
  }, [graph]);
  const targetY = useMemo(() => new Map(graph.targets.map((n) => [n.id, n.y + NODE_H / 2])), [graph]);

  const active = useMemo(() => (hover ? edgesOf(graph, hover) : null), [graph, hover]);

  const cancelHide = () => {
    if (hideTimer.current) clearTimeout(hideTimer.current);
    hideTimer.current = null;
  };
  const show = useCallback((nodeId: string, a?: Anchor) => {
    cancelHide();
    setHover(nodeId);
    if (a) setAnchor(a);
  }, []);
  const hideSoon = useCallback(() => {
    if (pinned) return;
    cancelHide();
    hideTimer.current = setTimeout(() => {
      setHover(null);
      setAnchor(null);
    }, 160);
  }, [pinned]);
  const closeCard = useCallback(() => {
    cancelHide();
    setPinned(false);
    setHover(null);
    setAnchor(null);
  }, []);
  useEffect(() => () => cancelHide(), []);

  // the edges sharing a pair of nodes are spread a little apart
  const offsets = useMemo(() => {
    const byPair = new Map<string, string[]>();
    for (const e of graph.edges) {
      const k = `${e.from}|${e.to}`;
      byPair.set(k, [...(byPair.get(k) ?? []), e.id]);
    }
    const out = new Map<string, number>();
    for (const ids of byPair.values()) ids.forEach((id, i) => out.set(id, (i - (ids.length - 1) / 2) * 4));
    return out;
  }, [graph]);

  const edgeLabel = (e: GraphEdge) => {
    const c = cands.get(e.candidateId);
    const g = groupOf.get(e.to);
    const src = c?.source_path ? provisionLabel(c.source_path, locale) : t("llink.noPath");
    return t("llink.diagram.edgeAria", {
      target: provisionLabel(c?.target_path ?? "", locale) || "—",
      op: t(`llink.op.${e.op}` as LocaleKey),
      doc: g?.number ?? "",
      source: src,
      state: t(`llink.state.${e.state}` as LocaleKey),
    });
  };

  const nodeKey = (id: string, click: () => void) => (ev: React.KeyboardEvent) => {
    if (ev.key === "Enter" || ev.key === " ") {
      ev.preventDefault();
      click();
    } else if (ev.key === "Escape") {
      closeCard();
    }
  };

  const clickTarget = (nodeId: string, y: number) => {
    const n = graph.targets.find((x) => x.id === nodeId);
    const first = n?.edgeIds.map((id) => graph.edges.find((e) => e.id === id)).find((e) => e?.approvable);
    if (first) {
      closeCard();
      onOpenEdge(first.candidateId);
      return;
    }
    setPinned((p) => !(p && hover === nodeId));
    show(nodeId, { nodeId, side: "left", y });
  };
  const clickSource = (nodeId: string, y: number) => {
    const g = groupOf.get(nodeId);
    const s = g?.sources.find((x) => x.id === nodeId);
    const first = s?.edgeIds.map((id) => graph.edges.find((e) => e.id === id)).find((e) => e?.approvable);
    if (first) {
      closeCard();
      onOpenEdge(first.candidateId);
      return;
    }
    setPinned((p) => !(p && hover === nodeId));
    show(nodeId, { nodeId, side: "right", y });
  };

  // the hover card's content
  const card = (() => {
    if (!anchor || !hover || anchor.nodeId !== hover) return null;
    const isTarget = hover.startsWith("t:");
    let docId = targetDocId;
    let path = "";
    let docLabel = targetLabel;
    let missing = "";
    let edgeIds: string[] = [];
    if (isTarget) {
      const n = graph.targets.find((x) => x.id === hover);
      if (!n) return null;
      path = n.path;
      edgeIds = n.edgeIds;
    } else {
      const g = groupOf.get(hover);
      const s = g?.sources.find((x) => x.id === hover);
      if (!g || !s) return null;
      docId = g.amenderDocId;
      docLabel = g.number;
      missing = g.kind === "blocked" ? g.number : "";
      path = s.path;
      edgeIds = s.edgeIds;
    }
    const first = cands.get(Number(edgeIds[0]?.slice(2)));
    const pending = edgeIds.filter((id) => graph.edges.find((e) => e.id === id)?.approvable).length;
    const top = Math.max(0, Math.min(anchor.y, graph.height - 40));
    const left = anchor.side === "left" ? leftW + 10 : Math.max(0, rightX - CARD_W - 10);
    return (
      <div
        role="dialog"
        aria-label={`${docLabel} — ${path ? provisionLabel(path, locale) : t("llink.noPath")}`}
        className="card absolute z-10 flex flex-col gap-2 p-3 text-left shadow-lg"
        style={{ top, left, width: Math.min(CARD_W, W - 20) }}
        onMouseEnter={cancelHide}
        onMouseLeave={hideSoon}
      >
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <div className="caption truncate text-muted">{docLabel}</div>
            <div className="text-[13px] font-semibold text-ink">{path ? provisionLabel(path, locale) : t("llink.noPath")}</div>
          </div>
          {pinned && (
            <button type="button" className="btn btn-ghost btn-sm h-6 px-2 text-[11.5px]" onClick={closeCard}>
              {t("common.close")}
            </button>
          )}
        </div>
        <ProvisionTextBox
          docId={docId}
          path={path}
          docLabel={docLabel}
          missingNumber={missing}
          note={isTarget ? undefined : first?.note}
          feedUrl={first?.feed_url}
          maxHeight="max-h-48"
        />
        {pending > 0 && <div className="caption text-warning">{t("llink.diagram.pendingHere", { n: pending })}</div>}
      </div>
    );
  })();

  return (
    <div ref={boxRef} className="relative w-full" onKeyDown={(e) => e.key === "Escape" && closeCard()}>
      <svg
        width={W}
        height={graph.height}
        viewBox={`0 0 ${W} ${graph.height}`}
        role="group"
        aria-label={t("llink.diagram.aria", { doc: targetLabel })}
        className="block select-none"
      >
        {/* edges first: the nodes cover their ends */}
        {graph.edges.map((e) => {
          const y0 = (targetY.get(e.from) ?? 0) + (offsets.get(e.id) ?? 0);
          const isHeader = e.to.startsWith("g:");
          const x1 = isHeader ? rightX : srcX;
          const y1 = (sourceY.get(e.to) ?? 0) + (offsets.get(e.id) ?? 0);
          const x0 = leftW;
          const mx = (x0 + x1) / 2;
          const d = `M ${x0} ${y0} C ${mx} ${y0}, ${mx} ${y1}, ${x1} ${y1}`;
          const st = edgeStyle(e);
          const on = active?.has(e.id) || selectedEdge === e.candidateId;
          const dim = active && !active.has(e.id);
          return (
            <g key={e.id}>
              <path
                d={d}
                fill="none"
                stroke={st.stroke}
                strokeWidth={on ? st.width + 1.2 : st.width}
                strokeDasharray={st.dash}
                opacity={dim ? 0.12 : on ? 1 : st.opacity}
                pointerEvents="none"
              />
              <path
                d={d}
                fill="none"
                stroke="transparent"
                strokeWidth={10}
                tabIndex={0}
                role="button"
                aria-label={edgeLabel(e)}
                className="cursor-pointer outline-none"
                onMouseEnter={() => show(e.id)}
                onMouseLeave={hideSoon}
                onFocus={() => show(e.id)}
                onBlur={hideSoon}
                onClick={() => onOpenEdge(e.candidateId)}
                onKeyDown={nodeKey(e.id, () => onOpenEdge(e.candidateId))}
              >
                <title>{edgeLabel(e)}</title>
              </path>
              {on && (
                <text
                  x={mx}
                  y={(y0 + y1) / 2 - 4}
                  textAnchor="middle"
                  className="pointer-events-none fill-ink text-[11px] font-medium"
                  style={{ paintOrder: "stroke", stroke: "var(--color-surface-card)", strokeWidth: 3 }}
                >
                  {t(`llink.op.${e.op}` as LocaleKey)}
                </text>
              )}
            </g>
          );
        })}

        {/* A's provisions */}
        {graph.targets.map((n) => {
          const on = hover === n.id;
          const label = provisionLabel(n.path, locale) || t("llink.diagram.noTarget");
          const cy = n.y + NODE_H / 2;
          return (
            <g
              key={n.id}
              tabIndex={0}
              role="button"
              aria-label={t("llink.diagram.targetAria", { prov: label, n: n.approvable })}
              className="cursor-pointer outline-none"
              onMouseEnter={() => show(n.id, { nodeId: n.id, side: "left", y: n.y })}
              onMouseLeave={hideSoon}
              onFocus={() => show(n.id, { nodeId: n.id, side: "left", y: n.y })}
              onBlur={hideSoon}
              onClick={() => clickTarget(n.id, n.y)}
              onKeyDown={nodeKey(n.id, () => clickTarget(n.id, n.y))}
            >
              <rect
                x={1}
                y={n.y}
                width={leftW - 2}
                height={NODE_H}
                rx={7}
                fill="var(--color-surface-card)"
                stroke={n.approvable > 0 ? "var(--color-warning)" : "var(--color-hairline-strong)"}
                strokeWidth={on ? 2.2 : 1.2}
              />
              <text x={10} y={cy + 4} className="fill-ink text-[12px]">
                {clip(label, Math.floor((leftW - (n.approvable ? 44 : 18)) / 6.6))}
              </text>
              {n.approvable > 0 && (
                <>
                  <rect x={leftW - 32} y={n.y + 5} width={24} height={16} rx={8} fill="var(--color-warning-surface)" />
                  <text x={leftW - 20} y={cy + 4} textAnchor="middle" className="fill-warning text-[10.5px] font-semibold tabular-nums">
                    {n.approvable}
                  </text>
                </>
              )}
            </g>
          );
        })}

        {/* B: a header per document, its provisions under it */}
        {graph.groups.map((g) => {
          const blocked = g.kind === "blocked";
          const total = g.edgeIds.length;
          const line1 = blocked ? t("llink.diagram.waitingFor", { doc: g.number }) : g.number;
          const line2 = blocked
            ? t("llink.diagram.waitingCount", { n: g.counts.blocked }) +
              (g.requestStatus ? ` · ${t(`llink.req.${g.requestStatus}` as LocaleKey)}` : "")
            : g.counts.pending > 0
              ? t("llink.diagram.groupPending", { n: g.counts.pending, total })
              : t("llink.diagram.groupDone", { total });
          const chars = Math.floor((rightW - 46) / 6.6);
          return (
            <g key={g.id}>
              <g
                tabIndex={0}
                role="button"
                aria-label={`${line1}. ${line2}. ${g.title}`}
                className="cursor-pointer outline-none"
                onMouseEnter={() => show(g.id)}
                onMouseLeave={hideSoon}
                onFocus={() => show(g.id)}
                onBlur={hideSoon}
                onClick={() => onOpenGroup(g.id)}
                onKeyDown={nodeKey(g.id, () => onOpenGroup(g.id))}
              >
                <rect
                  x={rightX + 1}
                  y={g.y}
                  width={rightW - 2}
                  height={HEADER_H}
                  rx={8}
                  fill={blocked ? "var(--color-error-surface)" : "var(--color-surface-strong)"}
                  stroke={blocked ? "var(--color-error)" : g.counts.pending > 0 ? "var(--color-warning)" : "var(--color-hairline-strong)"}
                  strokeDasharray={blocked ? "5 4" : undefined}
                  strokeWidth={hover === g.id ? 2.2 : 1.2}
                />
                <text x={rightX + 10} y={g.y + 16} className="fill-ink text-[12px] font-semibold">
                  <title>{g.title}</title>
                  {clip(line1, chars)}
                </text>
                <text x={rightX + 10} y={g.y + 31} className={`text-[11px] ${blocked ? "fill-error" : "fill-muted"}`}>
                  {clip(line2, chars)}
                </text>
              </g>
              <g
                tabIndex={0}
                role="button"
                aria-expanded={!g.collapsed}
                aria-label={t(g.collapsed ? "llink.diagram.expand" : "llink.diagram.collapse", { doc: g.number })}
                className="cursor-pointer outline-none"
                onClick={() => onToggleGroup(g.id)}
                onKeyDown={nodeKey(g.id, () => onToggleGroup(g.id))}
              >
                <rect x={W - 30} y={g.y + 7} width={24} height={24} rx={6} fill="transparent" />
                <text x={W - 18} y={g.y + 24} textAnchor="middle" className="fill-muted text-[15px]">
                  {g.collapsed ? "+" : "−"}
                </text>
              </g>
              {!g.collapsed &&
                g.sources.map((s) => {
                  const label = s.path ? provisionLabel(s.path, locale) : t("llink.noPath");
                  const on = hover === s.id;
                  return (
                    <g
                      key={s.id}
                      tabIndex={0}
                      role="button"
                      aria-label={t("llink.diagram.sourceAria", { doc: g.number, prov: label, n: s.approvable })}
                      className="cursor-pointer outline-none"
                      onMouseEnter={() => show(s.id, { nodeId: s.id, side: "right", y: s.y })}
                      onMouseLeave={hideSoon}
                      onFocus={() => show(s.id, { nodeId: s.id, side: "right", y: s.y })}
                      onBlur={hideSoon}
                      onClick={() => clickSource(s.id, s.y)}
                      onKeyDown={nodeKey(s.id, () => clickSource(s.id, s.y))}
                    >
                      <rect
                        x={srcX}
                        y={s.y}
                        width={srcW - 1}
                        height={NODE_H}
                        rx={7}
                        fill="var(--color-surface-card)"
                        stroke={blocked ? "var(--color-error)" : s.approvable > 0 ? "var(--color-warning)" : "var(--color-hairline-strong)"}
                        strokeDasharray={blocked || !s.path ? "4 3" : undefined}
                        strokeWidth={on ? 2.2 : 1.2}
                      />
                      <text x={srcX + 10} y={s.y + NODE_H / 2 + 4} className={`text-[12px] ${s.path ? "fill-ink" : "fill-muted"}`}>
                        {clip(label, Math.floor((srcW - 18) / 6.6))}
                      </text>
                    </g>
                  );
                })}
            </g>
          );
        })}
      </svg>
      {card}
    </div>
  );
}

/** The colour key under the diagram. */
export function LinkDiagramLegend() {
  const { t } = useT();
  const ops = ["sua_doi_bo_sung", "thay_the", "bai_bo", "giai_thich"];
  const states: { key: string; stroke: string; dash?: string; opacity?: number }[] = [
    { key: "blocked", stroke: "var(--color-error)", dash: "5 4" },
    { key: "approved", stroke: "var(--color-success)", opacity: 0.55 },
    { key: "auto", stroke: "var(--color-muted-soft)" },
    { key: "rejected", stroke: "var(--color-muted-soft)", dash: "2 4" },
  ];
  const swatch = (stroke: string, dash?: string, opacity = 1) => (
    <svg width="26" height="8" aria-hidden className="shrink-0">
      <line x1="1" y1="4" x2="25" y2="4" stroke={stroke} strokeWidth="2" strokeDasharray={dash} opacity={opacity} />
    </svg>
  );
  return (
    <div className="caption flex flex-wrap items-center gap-x-4 gap-y-1.5 text-muted">
      <span className="font-medium text-body">{t("llink.legend.pending")}</span>
      {ops.map((op) => (
        <span key={op} className="inline-flex items-center gap-1.5">
          {swatch(opColor(op))}
          {t(`llink.op.${op}` as LocaleKey)}
        </span>
      ))}
      <span className="mx-1 h-3 w-px bg-hairline" aria-hidden />
      {states.map((s) => (
        <span key={s.key} className="inline-flex items-center gap-1.5">
          {swatch(s.stroke, s.dash, s.opacity)}
          {t(`llink.state.${s.key}` as LocaleKey)}
        </span>
      ))}
    </div>
  );
}
