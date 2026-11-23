"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import {
  getLegalGraphExplore,
  type LegalGraphDoc,
  type LegalGraphExplore,
  type LegalGraphExploreNode,
} from "@/lib/api/graph";
import { ApiError } from "@/lib/api-client";
import { useAuth, useTenantRole } from "@/lib/auth";
import { useT, type LocaleKey } from "@/lib/i18n";
import { formatDate } from "@/lib/datetime";
import { knowledgeDocHref, legalDocShortLabel } from "@/lib/legal-status";
import { GraphCanvas, type CanvasEdge, type CanvasNode, type TypeStyle } from "@/components/graph-canvas";
import { GRAPH_FAMILY } from "@/components/graph/graph-palette";
import { LEGAL_FAMILY, StatusChip, legalGroup, relColor } from "@/components/graph/legal-style";
import { IconChevronLeft, IconFileSearch, IconGraph } from "@/components/icons";

/* The catalogue map, drawn under the validity lookup: one legal document
 * and the documents around it to a chosen number of tiers, over the whole
 * catalogue rather than one knowledge base. It is the same bubble network
 * as the KB's knowledge graph (components/graph-canvas.tsx: wheel zoom,
 * drag, double-click to zoom on a bubble), with the chosen document pinned
 * at the middle and drawn larger. A bubble's card redraws the map around
 * that document. (The tidy tree of cards was replaced on 2026-11-23.) */

// BASIS_AUTO_SHOW: a map with more "căn cứ" edges than this starts with them hidden
const BASIS_AUTO_SHOW = 250;
export const MAP_DEPTHS = [1, 2, 3] as const;

/** LegalMap draws the map of docId inside the validity lookup page: the
 * page owns the search box and the URL (doc, depth); the map reports a
 * refocus and a depth change back through callbacks. */
export function LegalMap({
  docId,
  depth,
  onFocus,
  onDepthChange,
}: {
  docId: string;
  depth: number;
  onFocus: (doc: LegalGraphDoc) => void;
  onDepthChange: (depth: number) => void;
}) {
  const { t } = useT();
  const { ready } = useAuth();
  const { isGuest } = useTenantRole();

  const [data, setData] = useState<LegalGraphExplore | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [notFound, setNotFound] = useState(false);
  /* "Căn cứ" starts shown unless the map has very many of them; the
   * user's own choice then holds. */
  const [basisChoice, setBasisChoice] = useState<boolean | null>(null);
  const [trail, setTrail] = useState<LegalGraphDoc[]>([]);
  const [full, setFull] = useState(false);

  useEffect(() => {
    if (!ready || !docId) {
      setData(null);
      return;
    }
    const ctrl = new AbortController();
    setLoading(true);
    setError("");
    setNotFound(false);
    getLegalGraphExplore(docId, { depth, isGuest, signal: ctrl.signal })
      .then((r) => {
        setData(r.data);
        setTrail((tr) => (tr.some((d) => d.id === r.data.focus.id) ? tr : [...tr, r.data.focus].slice(-12)));
      })
      .catch((e) => {
        if (ctrl.signal.aborted) return;
        if (e instanceof ApiError && e.status === 404) setNotFound(true);
        else setError(e instanceof Error ? e.message : String(e));
        setData(null);
      })
      .finally(() => !ctrl.signal.aborted && setLoading(false));
    return () => ctrl.abort();
  }, [docId, depth, isGuest, ready]);

  // Esc leaves full screen.
  useEffect(() => {
    if (!full) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setFull(false);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [full]);

  const basisCount = useMemo(() => (data?.edges ?? []).filter((e) => e.relation === "based_on").length, [data]);
  const showBasis = basisChoice ?? basisCount <= BASIS_AUTO_SHOW;

  const types: Record<string, TypeStyle> = useMemo(
    () =>
      Object.fromEntries(
        Object.entries(LEGAL_FAMILY).map(([key, family]) => [
          key,
          { fill: GRAPH_FAMILY[family].fill, stroke: GRAPH_FAMILY[family].stroke, label: t(`graph.legal.${key}` as LocaleKey) },
        ]),
      ),
    [t],
  );

  // The drawn network: hidden "căn cứ" edges drop out, and so does every
  // document joined to the focus only through them.
  const graph = useMemo(() => {
    if (!data) return null;
    const byId = new Map(data.nodes.map((n) => [n.doc.id, n]));
    const kept = data.edges.filter((e) => (showBasis || e.relation !== "based_on") && byId.has(e.from) && byId.has(e.to));
    const adj = new Map<string, string[]>();
    for (const e of kept) {
      adj.set(e.from, [...(adj.get(e.from) ?? []), e.to]);
      adj.set(e.to, [...(adj.get(e.to) ?? []), e.from]);
    }
    const reach = new Set<string>([data.focus.id]);
    const queue = [data.focus.id];
    while (queue.length) {
      const id = queue.shift()!;
      for (const o of adj.get(id) ?? []) if (!reach.has(o)) { reach.add(o); queue.push(o); }
    }
    const degree = new Map<string, number>();
    for (const e of kept) {
      degree.set(e.from, (degree.get(e.from) ?? 0) + 1);
      degree.set(e.to, (degree.get(e.to) ?? 0) + 1);
    }
    const nodes: CanvasNode[] = data.nodes
      .filter((n) => reach.has(n.doc.id))
      .map((n) => ({
        id: n.doc.id,
        title: n.doc.document_number || n.doc.title,
        type: legalGroup(n.doc.doc_type),
        weight: degree.get(n.doc.id) ?? 0,
      }));
    const edges: CanvasEdge[] = kept.map((e) => ({
      source: e.from,
      target: e.to,
      label: t(`graph.lf.edge.${e.relation}` as LocaleKey),
      dashed: e.relation === "based_on",
    }));
    return { byId, nodes, edges, hidden: data.nodes.length - reach.size };
  }, [data, showBasis, t]);

  const refocus = (d: LegalGraphDoc) => {
    if (d.id === data?.focus.id) return;
    onFocus(d);
  };
  const goBack = () => {
    const prev = trail[trail.length - 2];
    if (!prev) return;
    setTrail((tr) => tr.slice(0, -1));
    onFocus(prev);
  };

  const toolbar = (
    <>
      <span className="hidden text-[13px] font-semibold text-ink md:inline">{t("lmap.heading")}</span>
      {data && trail.length > 1 && (
        <button type="button" className="btn btn-ghost btn-sm h-8 max-w-[160px] gap-1 px-2 text-[12px]" onClick={goBack}>
          <IconChevronLeft className="h-3.5 w-3.5 shrink-0" />
          <span className="truncate">{legalDocShortLabel(trail[trail.length - 2])}</span>
        </button>
      )}
      <label className="flex items-center gap-1.5 whitespace-nowrap text-[12px] text-muted">
        {t("lmap.depth")}
        <select className="input h-8 w-auto min-w-[92px] py-0 pl-2.5 pr-8 text-[12.5px] bg-[position:right_8px_center]" value={depth} onChange={(e) => onDepthChange(Number(e.target.value))}>
          {MAP_DEPTHS.map((d) => (
            <option key={d} value={d}>
              {t("lmap.depthN", { n: d })}
            </option>
          ))}
        </select>
      </label>
      {basisCount > 0 && (
        <label className="flex cursor-pointer items-center gap-1.5 whitespace-nowrap text-[12px] text-muted" title={t("graph.lf.basisTitle")}>
          <input type="checkbox" checked={showBasis} onChange={(e) => setBasisChoice(e.target.checked)} />
          {t("graph.lf.basis", { n: basisCount })}
        </label>
      )}
      <button type="button" className="btn btn-outline btn-sm h-8 whitespace-nowrap text-[12px]" onClick={() => setFull((f) => !f)}>
        {full ? t("graph.lf.exitFullscreen") : t("graph.lf.fullscreen")}
      </button>
    </>
  );

  const notes = [
    data?.node_cap_hit ? { text: t("lmap.nodeCap", { n: data.nodes.length }), cls: "text-warning" } : null,
    data && data.nodes.length === 1 ? { text: t("lmap.alone"), cls: "text-muted" } : null,
    graph && graph.hidden > 0 && !showBasis ? { text: t("lmap.hidden", { n: graph.hidden }), cls: "text-muted-soft" } : null,
  ].filter(Boolean) as { text: string; cls: string }[];

  return (
    <div className={`flex min-h-0 flex-col ${full ? "fixed inset-0 z-50 bg-canvas" : "card h-[min(82dvh,820px)] min-h-[460px] overflow-hidden sm:min-h-[520px]"}`}>
      {notes.length > 0 && (
        <div className="border-b border-hairline px-3 py-1.5 sm:px-6">
          {notes.map((n) => (
            <p key={n.text} className={`caption ${n.cls}`}>
              {n.text}
            </p>
          ))}
        </div>
      )}
      <GraphCanvas
        nodes={graph?.nodes ?? []}
        edges={graph?.edges ?? []}
        types={types}
        directed
        centreId={data?.focus.id}
        loading={loading && !graph}
        error={error}
        emptyText={notFound ? t("lmap.notFound") : t("lmap.alone")}
        toolbarExtra={toolbar}
        renderDetails={(n) => {
          const node = graph?.byId.get(n.id);
          return node ? (
            <NodeDetails node={node} isFocus={node.doc.id === data?.focus.id} showBasis={showBasis} onRefocus={() => refocus(node.doc)} />
          ) : null;
        }}
      />
    </div>
  );
}

/* NodeDetails is a bubble's card: what the document is, where it can be
 * read, what the per-node cap left out, and a button to redraw the map
 * around it. */
function NodeDetails({
  node,
  isFocus,
  showBasis,
  onRefocus,
}: {
  node: LegalGraphExploreNode;
  isFocus: boolean;
  showBasis: boolean;
  onRefocus: () => void;
}) {
  const { t } = useT();
  const d = node.doc;
  const copies = node.copies ?? [];
  const dropped = Object.entries(node.dropped ?? {}).filter(([rel]) => showBasis || rel !== "based_on");
  return (
    <>
      {d.title && d.title !== d.document_number && <p className="caption mt-1 text-muted">{d.title}</p>}
      <p className="caption mt-2 flex flex-wrap items-center gap-2 text-muted">
        <StatusChip status={d.status} />
        {[d.issuer, formatDate(d.issued_on)].filter(Boolean).join(" · ")}
      </p>
      {isFocus && <p className="caption mt-2 font-medium text-ink">{t("graph.lf.focus")}</p>}
      {dropped.length > 0 && (
        <p className="mt-2 flex flex-wrap gap-1">
          {dropped.map(([rel, n]) => (
            <span key={rel} className="rounded-full bg-surface-strong px-1.5 text-[10.5px] font-medium leading-5" style={{ color: relColor(rel) }}>
              {t("lmap.more", { n, rel: t(`graph.lf.edge.${rel}` as LocaleKey) })}
            </span>
          ))}
        </p>
      )}
      {isFocus && (
        <div className="mt-3">
          <p className="caption text-muted">{copies.length ? t("lmap.copies") : t("lmap.noCopies")}</p>
          <div className="mt-1 flex flex-col gap-1">
            {copies.map((c) => (
              <Link
                key={c.knowledge_id}
                href={knowledgeDocHref(c.knowledge_base_id, c.knowledge_id)}
                className="caption inline-flex min-w-0 items-center gap-1 font-medium text-info hover:underline"
                title={c.knowledge_base_name || c.title}
              >
                <IconFileSearch className="h-3 w-3 shrink-0" />
                <span className="truncate">{c.knowledge_base_name || c.title}</span>
              </Link>
            ))}
          </div>
        </div>
      )}
      {!isFocus && (
        <div className="mt-3">
          {d.in_kb && <p className="caption mb-1.5 text-muted">{t("graph.lf.inKb")}</p>}
          {d.focusable ? (
            <button type="button" onClick={onRefocus} className="btn btn-outline btn-sm w-full gap-1.5">
              <IconGraph className="h-3.5 w-3.5" />
              {t("lmap.refocus")}
            </button>
          ) : (
            <p className="caption text-muted-soft">{t("graph.lf.notFocusable")}</p>
          )}
        </div>
      )}
    </>
  );
}
