"use client";

import { useEffect, useMemo, useRef, useState, useCallback } from "react";
import { getWikiGraph, type WikiGraphData } from "@/lib/api/wiki";
import { IconSearch } from "@/components/icons";

const TYPE_STYLE: Record<string, { fill: string; stroke: string; label: string; bg: string }> = {
  document: { fill: "#334155", stroke: "#1e293b", label: "Document", bg: "bg-slate-700" },
  entity: { fill: "#a855f7", stroke: "#7e22ce", label: "Entity", bg: "bg-purple-600" },
  concept: { fill: "#38bdf8", stroke: "#0284c7", label: "Concept", bg: "bg-sky-500" },
};
const FALLBACK_STYLE = { fill: "#10b981", stroke: "#059669", label: "Page", bg: "bg-emerald-500" };

export interface PosNode {
  slug: string;
  title: string;
  type: string;
  link_count: number;
  x: number;
  y: number;
  vx: number;
  vy: number;
  radius: number;
}

interface PosEdge {
  source: string;
  target: string;
  relation?: string;
}

const WIDTH = 1000;
const HEIGHT = 650;
const COLLISION_DISTANCE = 85; // Minimum distance between node centers to avoid overlap
const SPRING_LENGTH = 140; // Rest length of link springs

export function KnowledgeGraph({
  kbId,
  onSelectSlug,
}: {
  kbId: string;
  onSelectSlug?: (slug: string) => void;
}) {
  const [hovered, setHovered] = useState<string | null>(null);
  const [selectedNode, setSelectedNode] = useState<PosNode | null>(null);
  const [graph, setGraph] = useState<WikiGraphData | null>(null);
  const [error, setError] = useState("");
  const [filterType, setFilterType] = useState<string>("all");
  const [searchQuery, setSearchQuery] = useState("");

  const [nodes, setNodes] = useState<PosNode[]>([]);
  const [edges, setEdges] = useState<PosEdge[]>([]);

  // Pan & Zoom
  const [zoom, setZoom] = useState(1);
  const ZOOM_MIN = 0.3;
  const ZOOM_MAX = 3;
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const dragRef = useRef<{ startX: number; startY: number; panX: number; panY: number } | null>(null);
  const nodeDragRef = useRef<{ slug: string; startX: number; startY: number } | null>(null);

  const zoomBy = (factor: number) => {
    setZoom((z) => Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, z * factor)));
  };

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
      if (n.x < minX) minX = n.x;
      if (n.x > maxX) maxX = n.x;
      if (n.y < minY) minY = n.y;
      if (n.y > maxY) maxY = n.y;
    }

    const padding = 80;
    const graphWidth = Math.max(maxX - minX + padding * 2, 200);
    const graphHeight = Math.max(maxY - minY + padding * 2, 200);

    const scaleX = WIDTH / graphWidth;
    const scaleY = HEIGHT / graphHeight;
    const newZoom = Math.min(1.5, Math.max(0.4, Math.min(scaleX, scaleY)));

    const centerX = (minX + maxX) / 2;
    const centerY = (minY + maxY) / 2;

    setZoom(newZoom);
    setPan({
      x: (WIDTH / 2 - centerX) * newZoom,
      y: (HEIGHT / 2 - centerY) * newZoom,
    });
  }, [nodes]);

  useEffect(() => {
    let alive = true;
    getWikiGraph(kbId, { limit: 80 })
      .then((res: unknown) => {
        if (!alive) return;
        const r = res as { data?: WikiGraphData } & WikiGraphData;
        setGraph(r.data?.nodes ? r.data : r.nodes ? r : null);
      })
      .catch((e) => {
        if (alive) setError(e instanceof Error ? e.message : "Failed to load graph");
      });
    return () => {
      alive = false;
    };
  }, [kbId]);

  // Run Force-directed physics layout with collision avoidance
  useEffect(() => {
    if (!graph || !graph.nodes || graph.nodes.length === 0) {
      setNodes([]);
      setEdges([]);
      return;
    }

    const rawNodes = graph.nodes.slice(0, 70);
    const rawEdges = graph.edges || [];

    // Calculate node initial positions on a wide spiral to prevent initial stacking
    const initialNodes: PosNode[] = rawNodes.map((n, i) => {
      const angle = i * 2.39996; // Golden angle
      const dist = 60 + Math.sqrt(i + 1) * 45;
      const count = n.link_count || 1;
      const radius = Math.min(18, Math.max(9, 8 + Math.sqrt(count) * 2));
      return {
        slug: n.slug,
        title: n.title,
        type: n.page_type,
        link_count: count,
        x: WIDTH / 2 + Math.cos(angle) * dist,
        y: HEIGHT / 2 + Math.sin(angle) * dist,
        vx: 0,
        vy: 0,
        radius,
      };
    });

    const nodeMap = new Map<string, PosNode>();
    initialNodes.forEach((n) => nodeMap.set(n.slug, n));

    const validEdges: PosEdge[] = rawEdges.filter(
      (e) => nodeMap.has(e.source) && nodeMap.has(e.target)
    );

    // Iterative force simulation
    const iterations = 220;
    const dt = 0.85;

    for (let iter = 0; iter < iterations; iter++) {
      const alpha = Math.max(0.02, 1 - iter / iterations);

      // 1. Repulsion between all pairs + hard collision avoidance
      for (let i = 0; i < initialNodes.length; i++) {
        for (let j = i + 1; j < initialNodes.length; j++) {
          const a = initialNodes[i];
          const b = initialNodes[j];
          let dx = b.x - a.x;
          let dy = b.y - a.y;
          let dist = Math.sqrt(dx * dx + dy * dy);

          if (dist === 0) {
            dx = (Math.random() - 0.5) * 2;
            dy = (Math.random() - 0.5) * 2;
            dist = Math.sqrt(dx * dx + dy * dy);
          }

          // Coulomb repulsion
          const repForce = (7000 / (dist * dist + 400)) * alpha;
          const fx = (dx / dist) * repForce;
          const fy = (dy / dist) * repForce;

          a.vx -= fx;
          a.vy -= fy;
          b.vx += fx;
          b.vy += fy;

          // Hard collision constraint
          const minAllowedDist = a.radius + b.radius + COLLISION_DISTANCE;
          if (dist < minAllowedDist) {
            const overlap = (minAllowedDist - dist) * 0.5 * alpha;
            const pushX = (dx / dist) * overlap;
            const pushY = (dy / dist) * overlap;
            a.vx -= pushX;
            a.vy -= pushY;
            b.vx += pushX;
            b.vy += pushY;
          }
        }
      }

      // 2. Spring force along edges
      for (const e of validEdges) {
        const a = nodeMap.get(e.source)!;
        const b = nodeMap.get(e.target)!;
        let dx = b.x - a.x;
        let dy = b.y - a.y;
        let dist = Math.sqrt(dx * dx + dy * dy);
        if (dist === 0) dist = 1;

        const displacement = dist - SPRING_LENGTH;
        const springForce = displacement * 0.045 * alpha;
        const fx = (dx / dist) * springForce;
        const fy = (dy / dist) * springForce;

        a.vx += fx;
        a.vy += fy;
        b.vx -= fx;
        b.vy -= fy;
      }

      // 3. Centering force to prevent drift
      for (const n of initialNodes) {
        const cdx = WIDTH / 2 - n.x;
        const cdy = HEIGHT / 2 - n.y;
        n.vx += cdx * 0.012 * alpha;
        n.vy += cdy * 0.012 * alpha;

        // Apply velocity with damping
        n.x += n.vx * dt;
        n.y += n.vy * dt;
        n.vx *= 0.6;
        n.vy *= 0.6;

        // Soft canvas boundary constraints
        const pad = 60;
        if (n.x < pad) n.x = pad;
        if (n.x > WIDTH - pad) n.x = WIDTH - pad;
        if (n.y < pad) n.y = pad;
        if (n.y > HEIGHT - pad) n.y = HEIGHT - pad;
      }
    }

    setNodes(initialNodes);
    setEdges(validEdges);
  }, [graph]);

  const byId = useMemo(() => {
    return Object.fromEntries(nodes.map((n) => [n.slug, n]));
  }, [nodes]);

  const connected = useCallback(
    (slug: string) => {
      if (hovered === null) return false;
      if (slug === hovered) return true;
      return edges.some(
        (e) =>
          (e.source === hovered && e.target === slug) ||
          (e.target === hovered && e.source === slug)
      );
    },
    [hovered, edges]
  );

  // SVG pointer event handlers (Pan & Node Drag)
  const onSvgPointerDown = (e: React.PointerEvent) => {
    if (e.button !== 0) return;
    if (nodeDragRef.current) return;

    dragRef.current = {
      startX: e.clientX,
      startY: e.clientY,
      panX: pan.x,
      panY: pan.y,
    };
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
  };

  const onSvgPointerMove = (e: React.PointerEvent) => {
    // Handle Node Drag
    if (nodeDragRef.current) {
      const { slug, startX, startY } = nodeDragRef.current;
      const dx = (e.clientX - startX) / zoom;
      const dy = (e.clientY - startY) / zoom;

      setNodes((prev) =>
        prev.map((n) =>
          n.slug === slug
            ? { ...n, x: Math.max(40, Math.min(WIDTH - 40, n.x + dx)), y: Math.max(40, Math.min(HEIGHT - 40, n.y + dy)) }
            : n
        )
      );
      nodeDragRef.current = { slug, startX: e.clientX, startY: e.clientY };
      return;
    }

    // Handle Canvas Pan
    if (!dragRef.current) return;
    const dx = e.clientX - dragRef.current.startX;
    const dy = e.clientY - dragRef.current.startY;
    setPan({
      x: dragRef.current.panX + dx,
      y: dragRef.current.panY + dy,
    });
  };

  const onSvgPointerUp = (e: React.PointerEvent) => {
    dragRef.current = null;
    nodeDragRef.current = null;
  };

  const onWheel = (ev: React.WheelEvent) => {
    if (!ev.ctrlKey && !ev.metaKey && Math.abs(ev.deltaY) < 8) return;
    ev.preventDefault();
    zoomBy(ev.deltaY > 0 ? 0.9 : 1.1);
  };

  const handleNodePointerDown = (e: React.PointerEvent, n: PosNode) => {
    e.stopPropagation();
    if (e.button !== 0) return;
    nodeDragRef.current = { slug: n.slug, startX: e.clientX, startY: e.clientY };
    setSelectedNode(n);
  };

  // Filtered nodes
  const displayNodes = useMemo(() => {
    return nodes.filter((n) => {
      if (filterType !== "all" && n.type !== filterType) return false;
      if (searchQuery && !n.title.toLowerCase().includes(searchQuery.toLowerCase())) {
        return false;
      }
      return true;
    });
  }, [nodes, filterType, searchQuery]);

  const displaySlugs = useMemo(() => new Set(displayNodes.map((n) => n.slug)), [displayNodes]);

  return (
    <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden bg-surface-card">
      {/* Top Toolbar */}
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-hairline px-6 py-3">
        <div className="flex flex-wrap items-center gap-2">
          {/* Filter pills */}
          <div className="flex items-center gap-1 rounded-full bg-surface-strong p-1">
            {["all", "document", "entity", "concept"].map((type) => {
              const active = filterType === type;
              const label =
                type === "all" ? "Tất cả" : TYPE_STYLE[type]?.label ?? type;
              return (
                <button
                  key={type}
                  onClick={() => setFilterType(type)}
                  className={`rounded-full px-3 py-1 text-[12px] font-medium transition-colors ${
                    active
                      ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.08)]"
                      : "text-muted hover:text-ink"
                  }`}
                >
                  {label}
                </button>
              );
            })}
          </div>

          {/* Search inside graph */}
          <div className="relative w-48">
            <IconSearch className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-soft" />
            <input
              type="text"
              className="input h-8 pl-8 pr-2 text-[12px]"
              placeholder="Tìm kiếm node..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
            />
          </div>
        </div>

        {/* Zoom & Fit controls */}
        <div className="flex items-center gap-2 text-muted">
          <div className="flex items-center rounded-lg border border-hairline bg-surface p-0.5">
            <button
              className="btn btn-ghost btn-sm h-7 w-7 p-0 text-[14px]"
              onClick={() => zoomBy(1.2)}
              title="Phóng to"
            >
              +
            </button>
            <button
              className="btn btn-ghost btn-sm h-7 w-7 p-0 text-[14px]"
              onClick={() => zoomBy(1 / 1.2)}
              title="Thu nhỏ"
            >
              −
            </button>
          </div>
          <button
            className="btn btn-outline btn-sm h-7 text-[12px]"
            onClick={fitView}
            title="Căn vừa màn hình"
          >
            Vừa khung
          </button>
          <button
            className="btn btn-tertiary btn-sm h-7 text-[12px]"
            onClick={resetView}
            title="Đặt lại góc nhìn"
          >
            Reset
          </button>
          <span className="text-[12px] text-muted-soft">
            {Math.round(zoom * 100)}%
          </span>
        </div>
      </div>

      {error && <p className="caption px-6 pt-3 text-error">{error}</p>}
      {graph === null && !error && (
        <div className="flex flex-1 items-center justify-center">
          <p className="caption text-muted animate-pulse">Đang tải dữ liệu đồ thị…</p>
        </div>
      )}
      {graph !== null && nodes.length === 0 && (
        <div className="flex flex-1 items-center justify-center">
          <p className="caption text-muted">
            Chưa có trang wiki liên kết — sơ đồ sẽ hình thành khi tài liệu được lập chỉ mục.
          </p>
        </div>
      )}

      {/* SVG Canvas */}
      <svg
        viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
        preserveAspectRatio="xMidYMid meet"
        className="min-h-0 w-full flex-1 cursor-grab touch-none active:cursor-grabbing select-none"
        onPointerDown={onSvgPointerDown}
        onPointerMove={onSvgPointerMove}
        onPointerUp={onSvgPointerUp}
        onPointerLeave={onSvgPointerUp}
        onWheel={onWheel}
      >
        <defs>
          <radialGradient id="nodeGlow" cx="50%" cy="50%" r="50%">
            <stop offset="0%" stopColor="#38bdf8" stopOpacity="0.4" />
            <stop offset="100%" stopColor="#38bdf8" stopOpacity="0" />
          </radialGradient>
        </defs>

        <g
          transform={`translate(${pan.x} ${pan.y}) scale(${zoom}) translate(${(WIDTH / 2) * (1 - zoom)} ${(HEIGHT / 2) * (1 - zoom)})`}
        >
          {/* Edges */}
          {edges.map((e, i) => {
            const a = byId[e.source];
            const b = byId[e.target];
            if (!a || !b) return null;

            const isVisible = displaySlugs.has(e.source) && displaySlugs.has(e.target);
            if (!isVisible) return null;

            const isHoverActive =
              hovered !== null && (e.source === hovered || e.target === hovered);
            const isSelectedActive =
              selectedNode !== null &&
              (e.source === selectedNode.slug || e.target === selectedNode.slug);
            const active = isHoverActive || isSelectedActive;
            const dimmed = (hovered !== null || selectedNode !== null) && !active;

            return (
              <line
                key={`${e.source}-${e.target}-${i}`}
                x1={a.x}
                y1={a.y}
                x2={b.x}
                y2={b.y}
                stroke={active ? "#0284c7" : "#cbd5e1"}
                strokeWidth={active ? 2 : 1.2}
                strokeOpacity={dimmed ? 0.15 : active ? 0.9 : 0.6}
                style={{ transition: "stroke 0.2s, stroke-opacity 0.2s" }}
              />
            );
          })}

          {/* Nodes */}
          {nodes.map((n) => {
            const isVisible = displaySlugs.has(n.slug);
            if (!isVisible) return null;

            const s = TYPE_STYLE[n.type] ?? FALLBACK_STYLE;
            const isHovered = hovered === n.slug;
            const isSelected = selectedNode?.slug === n.slug;
            const isConn = connected(n.slug);
            const isFocused = isHovered || isSelected || isConn;
            const isDimmed = (hovered !== null || selectedNode !== null) && !isFocused;

            const nodeRadius = isSelected ? n.radius + 3 : isHovered ? n.radius + 2 : n.radius;

            return (
              <g
                key={n.slug}
                transform={`translate(${n.x}, ${n.y})`}
                opacity={isDimmed ? 0.25 : 1}
                onMouseEnter={() => setHovered(n.slug)}
                onMouseLeave={() => setHovered(null)}
                onPointerDown={(e) => handleNodePointerDown(e, n)}
                onClick={() => {
                  setSelectedNode(n);
                  if (onSelectSlug) onSelectSlug(n.slug);
                }}
                className="cursor-pointer"
                style={{ transition: "opacity 0.2s ease, transform 0.05s ease" }}
              >
                {/* Outer halo when active */}
                {isFocused && (
                  <circle
                    cx={0}
                    cy={0}
                    r={nodeRadius + 8}
                    fill={s.fill}
                    fillOpacity={0.2}
                    className="animate-pulse"
                  />
                )}

                {/* Hit area */}
                <circle cx={0} cy={0} r={nodeRadius + 10} fill="transparent" />

                {/* Main Node Circle */}
                <circle
                  cx={0}
                  cy={0}
                  r={nodeRadius}
                  fill={s.fill}
                  stroke={isSelected ? "#0c0a09" : s.stroke}
                  strokeWidth={isSelected ? 2.5 : 1.5}
                  className="transition-all duration-150"
                  filter={isFocused ? "drop-shadow(0px 2px 4px rgba(0,0,0,0.18))" : undefined}
                />

                {/* Node Label with Anti-collision Halo */}
                <g transform={`translate(0, ${nodeRadius + 14})`}>
                  <text
                    x={0}
                    y={0}
                    textAnchor="middle"
                    fontSize={isFocused ? 13 : 11.5}
                    fontWeight={isFocused ? 600 : 500}
                    stroke="var(--bg-surface-card, #ffffff)"
                    strokeWidth={4}
                    strokeLinejoin="round"
                    paintOrder="stroke fill"
                    fill={isFocused ? "#0f172a" : "#475569"}
                    style={{
                      fontFamily: "var(--font-sans)",
                      letterSpacing: "0.1px",
                      pointerEvents: "none",
                    }}
                  >
                    {n.title.length > 22 ? `${n.title.slice(0, 21)}…` : n.title}
                  </text>
                </g>
              </g>
            );
          })}
        </g>
      </svg>

      {/* Selected Node Details Floating Card */}
      {selectedNode && (
        <div className="absolute bottom-16 right-6 w-80 rounded-xl border border-hairline bg-surface-card p-4 shadow-lg backdrop-blur">
          <div className="flex items-start justify-between gap-2">
            <div className="flex items-center gap-2">
              <span
                className="inline-block h-3 w-3 rounded-full"
                style={{
                  background:
                    TYPE_STYLE[selectedNode.type]?.fill ?? FALLBACK_STYLE.fill,
                }}
              />
              <span className="caption uppercase tracking-wider text-muted font-medium">
                {TYPE_STYLE[selectedNode.type]?.label ?? selectedNode.type}
              </span>
            </div>
            <button
              onClick={() => setSelectedNode(null)}
              className="btn btn-ghost btn-sm h-6 w-6 p-0 text-muted hover:text-ink"
            >
              ✕
            </button>
          </div>
          <h4 className="body-md mt-1.5 font-semibold text-ink break-words">
            {selectedNode.title}
          </h4>
          <div className="caption mt-2 flex items-center gap-3 text-muted">
            <span>{selectedNode.link_count} liên kết</span>
            <span>ID: {selectedNode.slug}</span>
          </div>
          {onSelectSlug && (
            <button
              onClick={() => onSelectSlug(selectedNode.slug)}
              className="btn btn-primary btn-sm mt-3 w-full"
            >
              Mở trang Wiki
            </button>
          )}
        </div>
      )}

      {/* Footer Legend */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-t border-hairline px-6 py-2.5 bg-surface/50">
        <div className="flex items-center gap-5">
          {Object.entries(TYPE_STYLE).map(([typeKey, s]) => (
            <span key={typeKey} className="caption flex items-center gap-2 text-muted">
              <span
                className="inline-block h-2.5 w-2.5 rounded-full"
                style={{ background: s.fill }}
              />
              {s.label}
            </span>
          ))}
        </div>
        <div className="caption text-muted-soft">
          <span>
            {displayNodes.length} nodes · {edges.length} edges
          </span>
          <span className="ml-3 hidden sm:inline">
            Kéo thả node để điều chỉnh · Lăn chuột để zoom
          </span>
        </div>
      </div>
    </div>
  );
}
