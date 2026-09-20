"use client";

import { useEffect, useMemo, useState } from "react";
import { getWikiGraph, type WikiGraphData } from "@/lib/api/wiki";

const TYPE_STYLE: Record<string, { fill: string; ring: string; label: string }> = {
  document: { fill: "#292524", ring: "#292524", label: "Document" },
  entity: { fill: "#c8b8e0", ring: "#b8a4d8", label: "Entity" },
  concept: { fill: "#a8c8e8", ring: "#8fb4dc", label: "Concept" },
};
const FALLBACK_STYLE = { fill: "#a8c8e8", ring: "#8fb4dc", label: "Page" };

type PosNode = { slug: string; title: string; type: string; x: number; y: number };

export function KnowledgeGraph({ kbId }: { kbId: string }) {
  const [hovered, setHovered] = useState<string | null>(null);
  const [graph, setGraph] = useState<WikiGraphData | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    getWikiGraph(kbId, { limit: 60 })
      .then((res: unknown) => {
        if (!alive) return;
        const r = res as { data?: WikiGraphData } & WikiGraphData;
        setGraph(r.data?.nodes ? r.data : (r.nodes ? r : null));
      })
      .catch((e) => {
        if (alive) setError(e instanceof Error ? e.message : "Failed to load graph");
      });
    return () => {
      alive = false;
    };
  }, [kbId]);

  // Backend returns no coordinates — lay nodes out on concentric rings,
  // highest link_count nearest the center.
  const { nodes, edges, byId } = useMemo(() => {
    const raw = (graph?.nodes ?? []).slice(0, 60);
    const sorted = [...raw].sort((a, b) => b.link_count - a.link_count);
    const nodes: PosNode[] = sorted.map((n, i) => {
      const ring = Math.floor(i / 12);
      const idx = i % 12;
      const count = Math.min(12, sorted.length - ring * 12);
      const angle = (idx / Math.max(count, 1)) * Math.PI * 2 - Math.PI / 2;
      const radius = 60 + ring * 80;
      return {
        slug: n.slug,
        title: n.title,
        type: n.page_type,
        x: 360 + radius * Math.cos(angle),
        y: 260 + radius * 0.8 * Math.sin(angle),
      };
    });
    const byId = Object.fromEntries(nodes.map((n) => [n.slug, n]));
    const edges = (graph?.edges ?? []).filter((e) => byId[e.source] && byId[e.target]);
    return { nodes, edges, byId };
  }, [graph]);

  const connected = (slug: string) =>
    hovered !== null &&
    (slug === hovered ||
      edges.some(
        (e) =>
          (e.source === hovered && e.target === slug) ||
          (e.target === hovered && e.source === slug),
      ));

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {error && <p className="caption px-5 pt-4 text-error">{error}</p>}
      {graph === null && !error && (
        <p className="caption px-5 pt-4 text-muted">Loading graph…</p>
      )}
      {graph !== null && nodes.length === 0 && (
        <p className="caption px-5 pt-4 text-muted">
          No linked wiki pages yet — the graph builds as the wiki is generated.
        </p>
      )}

      <svg
        viewBox="0 0 720 520"
        preserveAspectRatio="xMidYMid meet"
        className="min-h-0 w-full flex-1"
      >
        {/* edges */}
        {edges.map((e, i) => {
          const a = byId[e.source];
          const b = byId[e.target];
          const active = hovered !== null && (e.source === hovered || e.target === hovered);
          return (
            <line
              key={i}
              x1={a.x}
              y1={a.y}
              x2={b.x}
              y2={b.y}
              stroke={active ? "#0c0a09" : "#d6d3d1"}
              strokeWidth={active ? 1.6 : 1}
            />
          );
        })}

        {/* nodes */}
        {nodes.map((n) => {
          const s = TYPE_STYLE[n.type] ?? FALLBACK_STYLE;
          const dim = hovered !== null && !connected(n.slug);
          return (
            <g
              key={n.slug}
              opacity={dim ? 0.3 : 1}
              onMouseEnter={() => setHovered(n.slug)}
              onMouseLeave={() => setHovered(null)}
              style={{ cursor: "pointer", transition: "opacity .15s ease" }}
            >
              <circle cx={n.x} cy={n.y} r={14} fill="transparent" />
              <circle cx={n.x} cy={n.y} r={8} fill={s.fill} stroke={s.ring} strokeWidth={1} />
              <text
                x={n.x}
                y={n.y + 24}
                textAnchor="middle"
                fontSize={12}
                fill={hovered === n.slug ? "#0c0a09" : "#777169"}
                fontWeight={hovered === n.slug ? 500 : 400}
                style={{ fontFamily: "var(--font-sans)", letterSpacing: "0.15px" }}
              >
                {n.title.length > 18 ? `${n.title.slice(0, 18)}…` : n.title}
              </text>
            </g>
          );
        })}
      </svg>

      {/* legend */}
      <div className="flex items-center gap-6 border-t border-hairline px-5 py-3">
        {Object.values(TYPE_STYLE).map((s) => (
          <span key={s.label} className="caption flex items-center gap-2 text-muted">
            <span
              className="inline-block h-2.5 w-2.5 rounded-full"
              style={{ background: s.fill }}
            />
            {s.label}
          </span>
        ))}
        <span className="caption ml-auto text-muted-soft">
          {nodes.length} nodes · {edges.length} edges
          {graph?.meta?.truncated ? " · truncated" : ""}
        </span>
      </div>
    </div>
  );
}
