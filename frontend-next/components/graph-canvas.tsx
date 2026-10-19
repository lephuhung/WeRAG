"use client";

import { memo, useCallback, useEffect, useMemo, useRef, useState, type MutableRefObject, type ReactNode } from "react";
import {
  forceCollide,
  forceLink,
  forceManyBody,
  forceSimulation,
  forceX,
  forceY,
  type Simulation,
  type SimulationLinkDatum,
  type SimulationNodeDatum,
} from "d3-force";
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

interface SimNode extends CanvasNode, SimulationNodeDatum {
  x: number;
  y: number;
  radius: number;
}

const ZOOM_MIN = 0.1;
const ZOOM_MAX = 5;
const EDGE_COLOR = "#94a3b8";
const DEFAULT_STYLE: TypeStyle = { fill: "#10b981", stroke: "#059669", label: "" };
/* Pointer travel (px) past which a press is a pan/drag, not a click. */
const CLICK_SLOP = 4;

/* nodeRadius grows with connectivity, kept small so hubs don't crowd. */
function nodeRadius(weight: number) {
  return Math.max(5, Math.min(15, 5 + Math.sqrt(weight || 0) * 1.6));
}

/* buildSimulation lays the graph out with d3-force: links pull with a
 * strength split by degree (dense graphs stay open), many-body repulsion
 * spreads clusters, collision keeps labels apart. It starts stopped. */
function buildSimulation(nodes: SimNode[], edges: CanvasEdge[]): Simulation<SimNode, undefined> {
  const links: SimulationLinkDatum<SimNode>[] = edges.map((e) => ({ source: e.source, target: e.target }));
  const density = edges.length / Math.max(nodes.length, 1);
  const degree = new Map<string, number>();
  for (const e of edges) {
    degree.set(e.source, (degree.get(e.source) ?? 0) + 1);
    degree.set(e.target, (degree.get(e.target) ?? 0) + 1);
  }
  // Isolated nodes get a stronger pull so they don't drift off and shrink the fit.
  const gravity = (n: SimNode) => (degree.get(n.id) ? 0.05 : 0.25);
  return forceSimulation(nodes)
    .force(
      "link",
      forceLink<SimNode, SimulationLinkDatum<SimNode>>(links)
        .id((n) => n.id)
        .distance(density > 3 ? 110 : 80)
    )
    .force("charge", forceManyBody<SimNode>().strength(density > 3 ? -420 : -260).distanceMax(900))
    .force("collide", forceCollide<SimNode>((n) => n.radius + 14).iterations(2))
    .force("x", forceX<SimNode>(0).strength(gravity))
    .force("y", forceY<SimNode>(0).strength(gravity))
    .stop();
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
  const [size, setSize] = useState({ width: 1000, height: 650 });
  const [zoom, setZoom] = useState(1);
  const [pan, setPan] = useState({ x: 0, y: 0 });
  // Mirrors zoom/pan so handlers compute the next view from current values.
  const viewRef = useRef({ zoom: 1, pan: { x: 0, y: 0 } });
  // d3-force mutates the nodes in place; GraphLayer re-renders on each tick,
  // `layoutVersion` re-renders the whole canvas once per new graph.
  const simRef = useRef<{ nodes: SimNode[]; edges: CanvasEdge[]; byId: Map<string, SimNode> }>({
    nodes: [],
    edges: [],
    byId: new Map(),
  });
  const forceRef = useRef<Simulation<SimNode, undefined> | null>(null);
  const [, setLayoutVersion] = useState(0);
  const sizeRef = useRef(size);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const svgRef = useRef<SVGSVGElement | null>(null);
  // Auto-fit once the layout settles, unless the user has moved the view.
  const autoFitRef = useRef(true);
  const panDragRef = useRef<{ startX: number; startY: number; panX: number; panY: number } | null>(null);
  const nodeDragRef = useRef<string | null>(null);
  // Where the current press started and whether it moved past CLICK_SLOP;
  // a moved press (pan or node drag) does not count as a click.
  const pressRef = useRef<{ x: number; y: number; moved: boolean }>({ x: 0, y: 0, moved: false });

  const setView = useCallback((z: number, p: { x: number; y: number }) => {
    viewRef.current = { zoom: z, pan: p };
    setZoom(z);
    setPan(p);
  }, []);

  const hasSvg = !loading && simRef.current.nodes.length > 0;

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const observer = new ResizeObserver((entries) => {
      const { width, height } = entries[0].contentRect;
      if (width > 50 && height > 50) setSize({ width, height });
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [hasSvg]);

  const fitView = useCallback(() => {
    const { nodes } = simRef.current;
    if (nodes.length === 0) return;
    const { width, height } = sizeRef.current;
    let minX = Infinity;
    let maxX = -Infinity;
    let minY = Infinity;
    let maxY = -Infinity;
    for (const n of nodes) {
      minX = Math.min(minX, n.x - n.radius);
      maxX = Math.max(maxX, n.x + n.radius);
      minY = Math.min(minY, n.y - n.radius);
      maxY = Math.max(maxY, n.y + n.radius + 16); // room for the label
    }
    const padding = 40;
    const graphW = Math.max(maxX - minX, 1);
    const graphH = Math.max(maxY - minY, 1);
    const z = Math.max(0.2, Math.min(2, Math.min((width - padding * 2) / graphW, (height - padding * 2) / graphH)));
    setView(z, { x: width / 2 - (minX + graphW / 2) * z, y: height / 2 - (minY + graphH / 2) * z });
  }, [setView]);

  useEffect(() => {
    sizeRef.current = size;
    if (autoFitRef.current) fitView();
  }, [size, fitView]);

  useEffect(() => {
    // x/y left unset so d3 seeds a phyllotaxis spiral; ticking fills them in.
    const nodes = rawNodes.map((n) => ({ ...n, radius: nodeRadius(n.weight) })) as SimNode[];
    const byId = new Map(nodes.map((n) => [n.id, n]));
    const edges = rawEdges.filter((e) => byId.has(e.source) && byId.has(e.target) && e.source !== e.target);
    simRef.current = { nodes, edges, byId };
    setSelectedId(null);
    setFilterType("all");
    autoFitRef.current = true;
    if (nodes.length === 0) {
      forceRef.current = null;
      setLayoutVersion((v) => v + 1);
      return;
    }
    const sim = buildSimulation(nodes, edges);
    forceRef.current = sim;
    // Settle most of the layout before the first paint, then animate the rest.
    sim.tick(120);
    fitView();
    setLayoutVersion((v) => v + 1);
    sim
      .on("end", () => {
        if (autoFitRef.current) {
          autoFitRef.current = false;
          fitView();
        }
      })
      .restart();
    return () => {
      sim.stop();
    };
  }, [rawNodes, rawEdges, fitView]);

  /* zoomAt scales the view around a point in container pixels. */
  const zoomAt = useCallback(
    (factor: number, px: number, py: number) => {
      autoFitRef.current = false;
      const { zoom: z, pan: p } = viewRef.current;
      const next = Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, z * factor));
      const ratio = next / z;
      setView(next, { x: px - (px - p.x) * ratio, y: py - (py - p.y) * ratio });
    },
    [setView]
  );
  const zoomBy = (factor: number) => zoomAt(factor, size.width / 2, size.height / 2);
  // The layout is centred on the origin, so 100% puts the origin mid-canvas.
  const resetView = () => {
    autoFitRef.current = false;
    const { width, height } = sizeRef.current;
    setView(1, { x: width / 2, y: height / 2 });
  };

  // Ctrl/Cmd+wheel (and trackpad pinch, which reports ctrlKey) zooms; a
  // plain wheel scrolls the page. Non-passive so preventDefault works.
  useEffect(() => {
    const svg = svgRef.current;
    if (!svg) return;
    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey && !e.metaKey) return;
      e.preventDefault();
      const rect = svg.getBoundingClientRect();
      // Normalise to pixels: Firefox reports lines (deltaMode 1) or pages (2).
      const unit = e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? rect.height : 1;
      zoomAt(Math.exp(-e.deltaY * unit * 0.0015), e.clientX - rect.left, e.clientY - rect.top);
    };
    svg.addEventListener("wheel", onWheel, { passive: false });
    return () => svg.removeEventListener("wheel", onWheel);
  }, [hasSvg, zoomAt]);

  const { nodes, edges, byId: simById } = simRef.current;
  const byId = useMemo(
    () => Object.fromEntries(nodes.map((n) => [n.id, n])) as Record<string, SimNode>,
    [nodes]
  );
  const selected = selectedId ? simById.get(selectedId) ?? null : null;

  const focusId = hovered ?? selectedId;
  const neighbours = useMemo(() => {
    const set = new Set<string>();
    if (focusId === null) return set;
    set.add(focusId);
    for (const e of edges) {
      if (e.source === focusId) set.add(e.target);
      if (e.target === focusId) set.add(e.source);
    }
    return set;
  }, [focusId, edges]);

  const toGraph = (e: React.PointerEvent) => {
    const rect = svgRef.current!.getBoundingClientRect();
    const { zoom: z, pan: p } = viewRef.current;
    return { x: (e.clientX - rect.left - p.x) / z, y: (e.clientY - rect.top - p.y) / z };
  };
  const onSvgPointerDown = (e: React.PointerEvent) => {
    if (e.button !== 0 || nodeDragRef.current) return;
    pressRef.current = { x: e.clientX, y: e.clientY, moved: false };
    const p = viewRef.current.pan;
    panDragRef.current = { startX: e.clientX, startY: e.clientY, panX: p.x, panY: p.y };
    (e.currentTarget as Element).setPointerCapture(e.pointerId);
  };
  const onSvgPointerMove = (e: React.PointerEvent) => {
    const press = pressRef.current;
    if (!press.moved && Math.hypot(e.clientX - press.x, e.clientY - press.y) > CLICK_SLOP) press.moved = true;
    const dragId = nodeDragRef.current;
    if (dragId) {
      if (!press.moved) return;
      const n = simById.get(dragId);
      if (!n) return;
      const p = toGraph(e);
      n.fx = p.x;
      n.fy = p.y;
      forceRef.current?.alphaTarget(0.3).restart();
      return;
    }
    const drag = panDragRef.current;
    if (!drag) return;
    autoFitRef.current = false;
    setView(viewRef.current.zoom, { x: drag.panX + e.clientX - drag.startX, y: drag.panY + e.clientY - drag.startY });
  };
  const onSvgPointerUp = () => {
    const dragId = nodeDragRef.current;
    if (dragId) {
      const n = simById.get(dragId);
      if (n) n.fx = n.fy = null;
      forceRef.current?.alphaTarget(0);
      nodeDragRef.current = null;
    }
    panDragRef.current = null;
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
  const styleOf = useCallback((type: string) => types[type] ?? { ...DEFAULT_STYLE, label: type }, [types]);
  const markerColors = useMemo(
    () => Array.from(new Set([EDGE_COLOR, ...presentTypes.map((type) => styleOf(type).fill)])),
    [presentTypes, styleOf]
  );
  // At low zoom only the biggest hubs and the focused neighbourhood keep
  // their labels, drawn at a constant screen size so they stay readable.
  const labelAll = zoom >= 0.8;
  const hubIds = useMemo(
    () => new Set([...nodes].sort((a, b) => b.weight - a.weight).slice(0, 12).map((n) => n.id)),
    [nodes]
  );
  const labelScale = 1 / Math.min(zoom, 1);

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
          <div className="flex items-center rounded-lg border border-hairline bg-surface-card p-0.5">
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

      {hasSvg && (
        <div ref={containerRef} className="relative min-h-0 flex-1">
          <svg
            ref={svgRef}
            width={size.width}
            height={size.height}
            viewBox={`0 0 ${size.width} ${size.height}`}
            className="absolute inset-0 h-full w-full cursor-grab touch-none select-none active:cursor-grabbing"
            onPointerDown={onSvgPointerDown}
            onPointerMove={onSvgPointerMove}
            onPointerUp={onSvgPointerUp}
            onPointerLeave={onSvgPointerUp}
            onClick={(e) => {
              if (e.target === e.currentTarget && !pressRef.current.moved) setSelectedId(null);
            }}
          >
            <defs>
              {markerColors.map((c) => (
                <marker
                  key={c}
                  id={`arrow-${c.slice(1)}`}
                  viewBox="0 0 10 10"
                  refX="10"
                  refY="5"
                  markerWidth="6"
                  markerHeight="6"
                  orient="auto-start-reverse"
                >
                  <path d="M 0 0 L 10 5 L 0 10 z" fill={c} />
                </marker>
              ))}
            </defs>
            <g transform={`translate(${pan.x},${pan.y}) scale(${zoom})`}>
              <GraphLayer
                sim={forceRef.current}
                nodes={nodes}
                edges={edges}
                simById={simById}
                displayIds={displayIds}
                neighbours={neighbours}
                focusId={focusId}
                hovered={hovered}
                selectedId={selectedId}
                labelAll={labelAll}
                hubIds={hubIds}
                labelScale={labelScale}
                directed={directed}
                styleOf={styleOf}
                setHovered={setHovered}
                setSelectedId={setSelectedId}
                nodeDragRef={nodeDragRef}
                pressRef={pressRef}
                onNodeClick={onNodeClick}
              />
            </g>
          </svg>
        </div>
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

      <div className="flex flex-wrap items-center justify-between gap-4 border-t border-hairline bg-surface-card/50 px-6 py-2.5">
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

/* GraphLayer draws the edges and nodes. It re-renders itself on every
 * simulation tick so the toolbar, legend and details card don't. */
const GraphLayer = memo(function GraphLayer({
  sim,
  nodes,
  edges,
  simById,
  displayIds,
  neighbours,
  focusId,
  hovered,
  selectedId,
  labelAll,
  hubIds,
  labelScale,
  directed,
  styleOf,
  setHovered,
  setSelectedId,
  nodeDragRef,
  pressRef,
  onNodeClick,
}: {
  sim: Simulation<SimNode, undefined> | null;
  nodes: SimNode[];
  edges: CanvasEdge[];
  simById: Map<string, SimNode>;
  displayIds: Set<string>;
  neighbours: Set<string>;
  focusId: string | null;
  hovered: string | null;
  selectedId: string | null;
  labelAll: boolean;
  hubIds: Set<string>;
  labelScale: number;
  directed: boolean;
  styleOf: (type: string) => TypeStyle;
  setHovered: (id: string | null) => void;
  setSelectedId: (id: string | null) => void;
  nodeDragRef: MutableRefObject<string | null>;
  pressRef: MutableRefObject<{ x: number; y: number; moved: boolean }>;
  onNodeClick?: (node: CanvasNode) => void;
}) {
  const [, setFrame] = useState(0);
  useEffect(() => {
    if (!sim) return;
    sim.on("tick.layer", () => setFrame((f) => f + 1));
    return () => {
      sim.on("tick.layer", null);
    };
  }, [sim]);

  return (
    <>
      {edges.map((e, i) => {
        const a = simById.get(e.source);
        const b = simById.get(e.target);
        if (!a || !b || !displayIds.has(e.source) || !displayIds.has(e.target)) return null;
        const active = focusId !== null && (e.source === focusId || e.target === focusId);
        const dimmed = focusId !== null && !active;
        const color = active ? styleOf(a.type).fill : EDGE_COLOR;
        // Stop the line at the target's rim so the arrowhead shows.
        const dx = b.x - a.x;
        const dy = b.y - a.y;
        const len = Math.sqrt(dx * dx + dy * dy) || 1;
        const x2 = directed ? b.x - (dx / len) * (b.radius + 2) : b.x;
        const y2 = directed ? b.y - (dy / len) * (b.radius + 2) : b.y;
        return (
          <g key={`${e.source}-${e.target}-${i}`}>
            <line
              x1={a.x}
              y1={a.y}
              x2={x2}
              y2={y2}
              stroke={color}
              strokeWidth={active ? 1.8 : 1}
              strokeOpacity={dimmed ? 0.08 : active ? 0.85 : 0.35}
              markerEnd={directed && !dimmed ? `url(#arrow-${color.slice(1)})` : undefined}
            />
            {active && e.label && (
              <text
                x={(a.x + b.x) / 2}
                y={(a.y + b.y) / 2 - 4}
                textAnchor="middle"
                fontSize={9.5}
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
        const isHovered = hovered === n.id;
        const isSelected = selectedId === n.id;
        const isFocused = focusId !== null && neighbours.has(n.id);
        const isDimmed = focusId !== null && !isFocused;
        const showLabel = labelAll || isFocused || hubIds.has(n.id);
        const fontSize = (isHovered || isSelected ? 11 : 9.5) * labelScale;
        return (
          <g
            key={n.id}
            transform={`translate(${n.x},${n.y})`}
            onMouseEnter={() => setHovered(n.id)}
            onMouseLeave={() => setHovered(null)}
            onPointerDown={(e) => {
              e.stopPropagation();
              if (e.button !== 0) return;
              pressRef.current = { x: e.clientX, y: e.clientY, moved: false };
              nodeDragRef.current = n.id;
            }}
            onClick={() => {
              // A drag keeps the node selected and does not open it.
              if (pressRef.current.moved) {
                setSelectedId(n.id);
                return;
              }
              setSelectedId(n.id === selectedId ? null : n.id);
              onNodeClick?.(n);
            }}
            className="cursor-pointer"
          >
            {(isHovered || isSelected) && (
              <circle r={n.radius + 4} fill="none" stroke={s.fill} strokeWidth={2} strokeOpacity={0.45} />
            )}
            <circle r={n.radius + 6} fill="transparent" />
            <circle
              r={n.radius}
              fill={s.fill}
              fillOpacity={isDimmed ? 0.15 : 0.9}
              stroke={isSelected ? "#0c0a09" : s.stroke}
              strokeWidth={isSelected ? 2 : 1}
              strokeOpacity={isDimmed ? 0.2 : 1}
            />
            {showLabel && (
              <text
                y={n.radius + 2 + fontSize}
                textAnchor="middle"
                fontSize={fontSize}
                fontWeight={isHovered || isSelected ? 600 : 500}
                stroke="var(--bg-surface-card, #ffffff)"
                strokeWidth={3 * labelScale}
                strokeLinejoin="round"
                paintOrder="stroke fill"
                fill={isFocused ? "#0f172a" : "#475569"}
                fillOpacity={isDimmed ? 0.2 : 1}
                style={{ fontFamily: "var(--font-sans)", pointerEvents: "none" }}
              >
                {isHovered || isSelected || n.title.length <= 20 ? n.title : `${n.title.slice(0, 18)}…`}
              </text>
            )}
          </g>
        );
      })}
    </>
  );
});

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
