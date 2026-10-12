"use client";

import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { IconSearch } from "@/components/icons";
import { useT } from "@/lib/i18n";

/* Force-directed SVG graph with pan, zoom, node drag, type filter, search
 * and a details card. Shared by the KB page's wiki, entity and legal
 * relation graphs; each supplies nodes, edges, type styles and the card. */

export interface CanvasNode {
  id: string;
  title: string;
  type: string;
  /** Connectivity; sets the node radius. */
  weight: number;
}

export interface CanvasEdge {
  source: string;
  target: string;
  /** Shown on the edges of the hovered or selected node. */
  label?: string;
}

export interface TypeStyle {
  fill: string;
  stroke: string;
  label: string;
}

interface PosNode extends CanvasNode {
  x: number;
  y: number;
  vx: number;
  vy: number;
  radius: number;
}

const WIDTH = 1000;
const HEIGHT = 650;
const COLLISION_DISTANCE = 92; // Minimum distance between node centers to avoid overlap
const SPRING_LENGTH = 175; // Rest length of link springs
const ZOOM_MIN = 0.3;
const ZOOM_MAX = 3;
const DEFAULT_STYLE: TypeStyle = { fill: "#10b981", stroke: "#059669", label: "" };

/* layout runs the force simulation once per graph. */
function layout(rawNodes: CanvasNode[], rawEdges: CanvasEdge[]): { nodes: PosNode[]; edges: CanvasEdge[] } {
  const nodes: PosNode[] = rawNodes.map((n, i) => {
    const angle = i * 2.39996; // Golden angle: a wide spiral avoids initial stacking
    const dist = 60 + Math.sqrt(i + 1) * 45;
    const count = n.weight || 1;
    return {
      ...n,
      x: WIDTH / 2 + Math.cos(angle) * dist,
      y: HEIGHT / 2 + Math.sin(angle) * dist,
      vx: 0,
      vy: 0,
      radius: Math.min(18, Math.max(9, 8 + Math.sqrt(count) * 2)),
    };
  });
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const edges = rawEdges.filter((e) => byId.has(e.source) && byId.has(e.target) && e.source !== e.target);

  const iterations = 220;
  const dt = 0.85;
  for (let iter = 0; iter < iterations; iter++) {
    const alpha = Math.max(0.02, 1 - iter / iterations);
    // 1. Repulsion between all pairs + hard collision avoidance
    for (let i = 0; i < nodes.length; i++) {
      for (let j = i + 1; j < nodes.length; j++) {
        const a = nodes[i];
        const b = nodes[j];
        let dx = b.x - a.x;
        let dy = b.y - a.y;
        let dist = Math.sqrt(dx * dx + dy * dy);
        if (dist === 0) {
          dx = (Math.random() - 0.5) * 2;
          dy = (Math.random() - 0.5) * 2;
          dist = Math.sqrt(dx * dx + dy * dy);
        }
        // Softer falloff so far pairs still drift apart instead of forming a
        // tight lattice under the collision constraint.
        const repForce = (12000 / (dist * dist + 2500)) * alpha;
        a.vx -= (dx / dist) * repForce;
        a.vy -= (dy / dist) * repForce;
        b.vx += (dx / dist) * repForce;
        b.vy += (dy / dist) * repForce;
        // The jitter breaks the square lattice uniform packing produces.
        const minAllowed = (a.radius + b.radius + COLLISION_DISTANCE) * (0.85 + Math.random() * 0.3);
        if (dist < minAllowed) {
          const overlap = (minAllowed - dist) * 0.5 * alpha;
          a.vx -= (dx / dist) * overlap;
          a.vy -= (dy / dist) * overlap;
          b.vx += (dx / dist) * overlap;
          b.vy += (dy / dist) * overlap;
        }
      }
    }
    // 2. Spring force along edges
    for (const e of edges) {
      const a = byId.get(e.source)!;
      const b = byId.get(e.target)!;
      const dx = b.x - a.x;
      const dy = b.y - a.y;
      const dist = Math.sqrt(dx * dx + dy * dy) || 1;
      const springForce = (dist - SPRING_LENGTH) * 0.045 * alpha;
      a.vx += (dx / dist) * springForce;
      a.vy += (dy / dist) * springForce;
      b.vx -= (dx / dist) * springForce;
      b.vy -= (dy / dist) * springForce;
    }
    // 3. Weak centering; the view auto-fits anyway.
    for (const n of nodes) {
      n.vx += (WIDTH / 2 - n.x) * 0.004 * alpha;
      n.vy += (HEIGHT / 2 - n.y) * 0.004 * alpha;
      n.x += n.vx * dt;
      n.y += n.vy * dt;
      n.vx *= 0.6;
      n.vy *= 0.6;
    }
  }
  return { nodes, edges };
}

export function GraphCanvas({
  nodes: rawNodes,
  edges: rawEdges,
  types,
  loading,
  error,
  emptyText,
  directed = false,
  truncated = false,
  toolbarExtra,
  renderDetails,
  onNodeClick,
}: {
  nodes: CanvasNode[];
  edges: CanvasEdge[];
  types: Record<string, TypeStyle>;
  loading: boolean;
  error?: string;
  emptyText: ReactNode;
  /** Draw arrowheads (source → target). */
  directed?: boolean;
  truncated?: boolean;
  toolbarExtra?: ReactNode;
  renderDetails?: (node: CanvasNode, edges: CanvasEdge[], byId: Record<string, CanvasNode>) => ReactNode;
  onNodeClick?: (node: CanvasNode) => void;
}) {
  const { t } = useT();
  const [hovered, setHovered] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [filterType, setFilterType] = useState<string>("all");
  const [searchQuery, setSearchQuery] = useState("");
  const [nodes, setNodes] = useState<PosNode[]>([]);
  const [edges, setEdges] = useState<CanvasEdge[]>([]);
  const [zoom, setZoom] = useState(1);
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const dragRef = useRef<{ startX: number; startY: number; panX: number; panY: number } | null>(null);
  const nodeDragRef = useRef<{ id: string; startX: number; startY: number } | null>(null);

  useEffect(() => {
    const out = layout(rawNodes, rawEdges);
    setNodes(out.nodes);
    setEdges(out.edges);
    setSelectedId(null);
    setFilterType("all");
  }, [rawNodes, rawEdges]);

  const zoomBy = (factor: number) => setZoom((z) => Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, z * factor)));
  const resetView = () => {
    setZoom(1);
    setPan({ x: 0, y: 0 });
  };

  const fitView = useCallback(() => {
    if (nodes.length === 0) return;
    let minX = Infinity;
    let maxX = -Infinity;
    let minY = Infinity;
    let maxY = -Infinity;
    for (const n of nodes) {
      minX = Math.min(minX, n.x);
      maxX = Math.max(maxX, n.x);
      minY = Math.min(minY, n.y);
      maxY = Math.max(maxY, n.y);
    }
    const padding = 80;
    const scaleX = WIDTH / Math.max(maxX - minX + padding * 2, 200);
    const scaleY = HEIGHT / Math.max(maxY - minY + padding * 2, 200);
    const newZoom = Math.min(1.5, Math.max(0.4, Math.min(scaleX, scaleY)));
    setZoom(newZoom);
    setPan({
      x: (WIDTH / 2 - (minX + maxX) / 2) * newZoom,
      y: (HEIGHT / 2 - (minY + maxY) / 2) * newZoom,
    });
  }, [nodes]);

  const byId = useMemo(() => Object.fromEntries(nodes.map((n) => [n.id, n])), [nodes]);
  const selected = selectedId ? byId[selectedId] ?? null : null;

  const focusId = hovered ?? selectedId;
  const connected = useCallback(
    (id: string) =>
      focusId !== null &&
      (id === focusId || edges.some((e) => (e.source === focusId && e.target === id) || (e.target === focusId && e.source === id))),
    [focusId, edges]
  );

  const onSvgPointerDown = (e: React.PointerEvent) => {
    if (e.button !== 0 || nodeDragRef.current) return;
    dragRef.current = { startX: e.clientX, startY: e.clientY, panX: pan.x, panY: pan.y };
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
  };
  const onSvgPointerMove = (e: React.PointerEvent) => {
    if (nodeDragRef.current) {
      const { id, startX, startY } = nodeDragRef.current;
      const dx = (e.clientX - startX) / zoom;
      const dy = (e.clientY - startY) / zoom;
      setNodes((prev) => prev.map((n) => (n.id === id ? { ...n, x: n.x + dx, y: n.y + dy } : n)));
      nodeDragRef.current = { id, startX: e.clientX, startY: e.clientY };
      return;
    }
    if (!dragRef.current) return;
    setPan({
      x: dragRef.current.panX + e.clientX - dragRef.current.startX,
      y: dragRef.current.panY + e.clientY - dragRef.current.startY,
    });
  };
  const onSvgPointerUp = () => {
    dragRef.current = null;
    nodeDragRef.current = null;
  };
  const onWheel = (ev: React.WheelEvent) => {
    if (!ev.ctrlKey && !ev.metaKey && Math.abs(ev.deltaY) < 8) return;
    ev.preventDefault();
    zoomBy(ev.deltaY > 0 ? 0.9 : 1.1);
  };

  const presentTypes = useMemo(() => Array.from(new Set(nodes.map((n) => n.type))), [nodes]);
  const displayIds = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    return new Set(
      nodes
        .filter((n) => (filterType === "all" || n.type === filterType) && (!q || n.title.toLowerCase().includes(q)))
        .map((n) => n.id)
    );
  }, [nodes, filterType, searchQuery]);
  const styleOf = (type: string) => types[type] ?? { ...DEFAULT_STYLE, label: type };

  return (
    <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden bg-surface-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-hairline px-6 py-3">
        <div className="flex flex-wrap items-center gap-2">
          {presentTypes.length > 1 && (
            <div className="flex flex-wrap items-center gap-1 rounded-full bg-surface-strong p-1">
              {["all", ...presentTypes].map((type) => (
                <button
                  key={type}
                  onClick={() => setFilterType(type)}
                  className={`rounded-full px-3 py-1 text-[12px] font-medium transition-colors ${
                    filterType === type
                      ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.08)]"
                      : "text-muted hover:text-ink"
                  }`}
                >
                  {type === "all" ? t("graph.all") : styleOf(type).label}
                </button>
              ))}
            </div>
          )}
          <div className="relative w-48">
            <IconSearch className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-soft" />
            <input
              type="text"
              className="input h-8 pl-8 pr-2 text-[12px]"
              placeholder={t("graph.search")}
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
            />
          </div>
          {toolbarExtra}
        </div>
        <div className="flex items-center gap-2 text-muted">
          <div className="flex items-center rounded-lg border border-hairline bg-surface p-0.5">
            <button className="btn btn-ghost btn-sm h-7 w-7 p-0 text-[14px]" onClick={() => zoomBy(1.2)} title={t("graph.zoomIn")}>
              +
            </button>
            <button className="btn btn-ghost btn-sm h-7 w-7 p-0 text-[14px]" onClick={() => zoomBy(1 / 1.2)} title={t("graph.zoomOut")}>
              −
            </button>
          </div>
          <button className="btn btn-outline btn-sm h-7 text-[12px]" onClick={fitView} title={t("graph.fitTitle")}>
            {t("graph.fit")}
          </button>
          <button className="btn btn-tertiary btn-sm h-7 text-[12px]" onClick={resetView} title={t("graph.resetTitle")}>
            {t("graph.reset")}
          </button>
          <span className="text-[12px] text-muted-soft">{Math.round(zoom * 100)}%</span>
        </div>
      </div>

      {error && <p className="caption px-6 pt-3 text-error">{error}</p>}
      {loading && !error && (
        <div className="flex flex-1 items-center justify-center">
          <p className="caption animate-pulse text-muted">{t("graph.loading")}</p>
        </div>
      )}
      {!loading && !error && nodes.length === 0 && (
        <div className="flex flex-1 items-center justify-center px-6">
          <div className="caption max-w-md text-center text-muted">{emptyText}</div>
        </div>
      )}

      {!loading && nodes.length > 0 && (
        <svg
          viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
          preserveAspectRatio="xMidYMid meet"
          className="min-h-0 w-full flex-1 cursor-grab touch-none select-none active:cursor-grabbing"
          onPointerDown={onSvgPointerDown}
          onPointerMove={onSvgPointerMove}
          onPointerUp={onSvgPointerUp}
          onPointerLeave={onSvgPointerUp}
          onWheel={onWheel}
        >
          <defs>
            {["#cbd5e1", "#0284c7"].map((c) => (
              <marker
                key={c}
                id={`arrow-${c.slice(1)}`}
                viewBox="0 0 10 10"
                refX="10"
                refY="5"
                markerWidth="7"
                markerHeight="7"
                orient="auto-start-reverse"
              >
                <path d="M 0 0 L 10 5 L 0 10 z" fill={c} />
              </marker>
            ))}
          </defs>
          <g
            transform={`translate(${pan.x} ${pan.y}) scale(${zoom}) translate(${(WIDTH / 2) * (1 - zoom)} ${(HEIGHT / 2) * (1 - zoom)})`}
          >
            {edges.map((e, i) => {
              const a = byId[e.source];
              const b = byId[e.target];
              if (!a || !b || !displayIds.has(e.source) || !displayIds.has(e.target)) return null;
              const active = focusId !== null && (e.source === focusId || e.target === focusId);
              const dimmed = focusId !== null && !active;
              const color = active ? "#0284c7" : "#cbd5e1";
              // Stop the line at the target's rim so the arrowhead shows.
              const dx = b.x - a.x;
              const dy = b.y - a.y;
              const len = Math.sqrt(dx * dx + dy * dy) || 1;
              const x2 = directed ? b.x - (dx / len) * (b.radius + 3) : b.x;
              const y2 = directed ? b.y - (dy / len) * (b.radius + 3) : b.y;
              return (
                <g key={`${e.source}-${e.target}-${i}`}>
                  <line
                    x1={a.x}
                    y1={a.y}
                    x2={x2}
                    y2={y2}
                    stroke={color}
                    strokeWidth={active ? 2 : 1.2}
                    strokeOpacity={dimmed ? 0.15 : active ? 0.9 : 0.6}
                    markerEnd={directed ? `url(#arrow-${color.slice(1)})` : undefined}
                  />
                  {active && e.label && (
                    <text
                      x={(a.x + b.x) / 2}
                      y={(a.y + b.y) / 2 - 4}
                      textAnchor="middle"
                      fontSize={10.5}
                      fill="#0369a1"
                      stroke="var(--bg-surface-card, #ffffff)"
                      strokeWidth={3}
                      paintOrder="stroke fill"
                      style={{ pointerEvents: "none" }}
                    >
                      {e.label}
                    </text>
                  )}
                </g>
              );
            })}

            {nodes.map((n) => {
              if (!displayIds.has(n.id)) return null;
              const s = styleOf(n.type);
              const isSelected = selectedId === n.id;
              const isFocused = hovered === n.id || isSelected || connected(n.id);
              const isDimmed = focusId !== null && !isFocused;
              const r = isSelected ? n.radius + 3 : hovered === n.id ? n.radius + 2 : n.radius;
              return (
                <g
                  key={n.id}
                  transform={`translate(${n.x}, ${n.y})`}
                  opacity={isDimmed ? 0.25 : 1}
                  onMouseEnter={() => setHovered(n.id)}
                  onMouseLeave={() => setHovered(null)}
                  onPointerDown={(e) => {
                    e.stopPropagation();
                    if (e.button !== 0) return;
                    nodeDragRef.current = { id: n.id, startX: e.clientX, startY: e.clientY };
                    setSelectedId(n.id);
                  }}
                  onClick={() => {
                    setSelectedId(n.id);
                    onNodeClick?.(n);
                  }}
                  className="cursor-pointer"
                  style={{ transition: "opacity 0.2s ease" }}
                >
                  {isFocused && <circle r={r + 8} fill={s.fill} fillOpacity={0.2} className="animate-pulse" />}
                  <circle r={r + 10} fill="transparent" />
                  <circle
                    r={r}
                    fill={s.fill}
                    stroke={isSelected ? "#0c0a09" : s.stroke}
                    strokeWidth={isSelected ? 2.5 : 1.5}
                    filter={isFocused ? "drop-shadow(0px 2px 4px rgba(0,0,0,0.18))" : undefined}
                  />
                  <text
                    y={r + 14}
                    textAnchor="middle"
                    fontSize={isFocused ? 13 : 11.5}
                    fontWeight={isFocused ? 600 : 500}
                    stroke="var(--bg-surface-card, #ffffff)"
                    strokeWidth={4}
                    strokeLinejoin="round"
                    paintOrder="stroke fill"
                    fill={isFocused ? "#0f172a" : "#475569"}
                    style={{ fontFamily: "var(--font-sans)", pointerEvents: "none" }}
                  >
                    {n.title.length > 24 ? `${n.title.slice(0, 23)}…` : n.title}
                  </text>
                </g>
              );
            })}
          </g>
        </svg>
      )}

      {selected && (
        <div className="absolute bottom-16 right-6 max-h-[60%] w-80 overflow-y-auto rounded-xl border border-hairline bg-surface-card p-4 shadow-lg">
          <div className="flex items-start justify-between gap-2">
            <div className="flex items-center gap-2">
              <span className="inline-block h-3 w-3 rounded-full" style={{ background: styleOf(selected.type).fill }} />
              <span className="caption font-medium uppercase tracking-wider text-muted">{styleOf(selected.type).label}</span>
            </div>
            <button
              onClick={() => setSelectedId(null)}
              className="btn btn-ghost btn-sm h-6 w-6 p-0 text-muted hover:text-ink"
              title={t("graph.close")}
            >
              ✕
            </button>
          </div>
          <h4 className="body-md mt-1.5 break-words font-semibold text-ink">{selected.title}</h4>
          {renderDetails ? (
            renderDetails(
              selected,
              edges.filter((e) => e.source === selected.id || e.target === selected.id),
              byId
            )
          ) : (
            <p className="caption mt-2 text-muted">{t("graph.links", { n: selected.weight })}</p>
          )}
        </div>
      )}

      <div className="flex flex-wrap items-center justify-between gap-4 border-t border-hairline bg-surface/50 px-6 py-2.5">
        <div className="flex flex-wrap items-center gap-5">
          {presentTypes.map((type) => (
            <span key={type} className="caption flex items-center gap-2 text-muted">
              <span className="inline-block h-2.5 w-2.5 rounded-full" style={{ background: styleOf(type).fill }} />
              {styleOf(type).label}
            </span>
          ))}
        </div>
        <div className="caption text-muted-soft">
          <span>{t("graph.counts", { nodes: displayIds.size, edges: edges.length })}</span>
          {truncated && <span className="ml-3">· {t("graph.truncated")}</span>}
          <span className="ml-3 hidden sm:inline">{t("graph.hint")}</span>
        </div>
      </div>
    </div>
  );
}

/* RelationList lists a selected node's edges as "<label> <other node>". */
export function RelationList({
  node,
  edges,
  byId,
  label,
}: {
  node: CanvasNode;
  edges: CanvasEdge[];
  byId: Record<string, CanvasNode>;
  label: (edge: CanvasEdge, outgoing: boolean) => string;
}) {
  const { t } = useT();
  if (edges.length === 0) return null;
  return (
    <div className="mt-3">
      <p className="caption font-medium text-muted">{t("graph.relations")}</p>
      <ul className="mt-1 space-y-1">
        {edges.slice(0, 30).map((e, i) => {
          const outgoing = e.source === node.id;
          const other = byId[outgoing ? e.target : e.source];
          return (
            <li key={i} className="caption text-ink">
              {outgoing ? "→" : "←"} <span className="text-muted">{label(e, outgoing)}</span>{" "}
              {other?.title ?? "?"}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
