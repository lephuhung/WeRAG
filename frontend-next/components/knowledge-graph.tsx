"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { getWikiGraph, type WikiGraphData } from "@/lib/api/wiki";
import { getEntityGraph, getLegalGraph, type EntityGraphResponse, type GraphView } from "@/lib/api/graph";
import { GraphCanvas, RelationList, type CanvasEdge, type CanvasNode, type TypeStyle } from "@/components/graph-canvas";
import { GRAPH_FAMILY, type GraphFamily } from "@/components/graph/graph-palette";
import { useT, type LocaleKey } from "@/lib/i18n";
import { formatDate as vnFormatDate } from "@/lib/datetime";

type GraphMode = "wiki" | "entity" | "legal";

/* The KB page's graph tab: the wiki link graph, the entity graph extracted
 * from the KB's documents, and the relation graph of its legal documents. */
export function KnowledgeGraph({
  kbId,
  onSelectSlug,
  onOpenKnowledge,
}: {
  kbId: string;
  onSelectSlug?: (slug: string) => void;
  onOpenKnowledge?: (knowledgeId: string) => void;
}) {
  const { t } = useT();
  const [mode, setMode] = useState<GraphMode>("wiki");
  const modes: [GraphMode, LocaleKey][] = [
    ["wiki", "graph.modeWiki"],
    ["entity", "graph.modeEntity"],
    ["legal", "graph.modeLegal"],
  ];
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-1 border-b border-hairline px-6 pt-3">
        {modes.map(([m, key]) => (
          <button
            key={m}
            onClick={() => setMode(m)}
            className={`-mb-px border-b-2 px-3 pb-2 text-[13px] font-medium transition-colors ${
              mode === m ? "border-ink text-ink" : "border-transparent text-muted hover:text-ink"
            }`}
          >
            {t(key)}
          </button>
        ))}
      </div>
      {mode === "wiki" && <WikiGraph kbId={kbId} onSelectSlug={onSelectSlug} />}
      {mode === "entity" && <EntityGraph kbId={kbId} />}
      {mode === "legal" && <LegalGraph kbId={kbId} onOpenKnowledge={onOpenKnowledge} />}
    </div>
  );
}

/* useRelationLabel words an edge type through the graph.rel.* strings,
 * falling back to the raw type. */
function useRelationLabel() {
  const { t } = useT();
  return useCallback(
    (type: string) => {
      const key = `graph.rel.${type}` as LocaleKey;
      const label = t(key);
      return label === key ? type : label;
    },
    [t]
  );
}

function errorText(e: unknown) {
  return e instanceof Error ? e.message : String(e);
}

/* ---------- Wiki link graph ---------- */

/* The node types map onto the shared graph palette (CSS custom
 * properties) — the same values the canvas and the legend read, so the
 * dark mode and the Anthropic preset override them all at once. */
const WIKI_FAMILY: Record<string, GraphFamily> = {
  document: "slate",
  entity: "purple",
  concept: "sky",
  synthesis: "amber",
  comparison: "rose",
  index: "teal",
  field: "emerald",
  agency: "indigo",
  page: "slate",
};

/** The canvas type styles of a node-type map. */
function typeStyles(
  families: Record<string, GraphFamily>,
  label: (key: string) => string,
): Record<string, TypeStyle> {
  return Object.fromEntries(
    Object.entries(families).map(([key, family]) => [
      key,
      { fill: GRAPH_FAMILY[family].fill, stroke: GRAPH_FAMILY[family].stroke, label: label(key) },
    ]),
  );
}

/* wikiNodeType maps a page to its colour group. Catalogue-mode wikis only
 * have summary and index pages, so index pages split by slug: lĩnh vực,
 * cơ quan ban hành, or the main index. page_type may list several
 * comma-separated types (types.SplitWikiPageTypes); the first known wins. */
const CATALOGUE_HUBS: [string, string][] = [
  ["linh-vuc", "field"],
  ["co-quan", "agency"],
];

function wikiNodeType(pageType: string, slug: string) {
  for (const raw of (pageType ?? "").split(",")) {
    const type = raw.trim();
    if (type === "summary") return "document";
    if (type === "index") {
      for (const [hub, group] of CATALOGUE_HUBS) {
        if (slug === hub || slug.startsWith(`${hub}/`)) return group;
      }
      return "index";
    }
    if (WIKI_FAMILY[type] && type !== "page") return type;
  }
  return "page";
}

function WikiGraph({ kbId, onSelectSlug }: { kbId: string; onSelectSlug?: (slug: string) => void }) {
  const { t } = useT();
  const [graph, setGraph] = useState<WikiGraphData | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    setGraph(null);
    getWikiGraph(kbId, { limit: 80 })
      .then((res: unknown) => {
        if (!alive) return;
        const r = res as { data?: WikiGraphData } & WikiGraphData;
        setGraph(r.data?.nodes ? r.data : r.nodes ? r : { nodes: [], edges: [], meta: r.meta });
      })
      .catch((e) => alive && setError(errorText(e)));
    return () => {
      alive = false;
    };
  }, [kbId]);

  const types: Record<string, TypeStyle> = useMemo(
    () => typeStyles(WIKI_FAMILY, (k) => t(`graph.wiki.${k}` as LocaleKey)),
    [t]
  );
  const nodes: CanvasNode[] = useMemo(
    () =>
      (graph?.nodes ?? []).slice(0, 70).map((n) => ({
        id: n.slug,
        title: n.title,
        type: wikiNodeType(n.page_type, n.slug),
        weight: n.link_count,
      })),
    [graph]
  );
  const edges: CanvasEdge[] = useMemo(() => graph?.edges ?? [], [graph]);

  return (
    <GraphCanvas
      nodes={nodes}
      edges={edges}
      types={types}
      loading={graph === null && !error}
      error={error}
      emptyText={t("graph.wikiEmpty")}
      onNodeClick={(n) => onSelectSlug?.(n.id)}
      renderDetails={(n) => (
        <>
          <p className="caption mt-2 text-muted">{t("graph.links", { n: n.weight })}</p>
          {onSelectSlug && (
            <button onClick={() => onSelectSlug(n.id)} className="btn btn-primary btn-sm mt-3 w-full">
              {t("graph.openWiki")}
            </button>
          )}
        </>
      )}
    />
  );
}

/* ---------- Entity graph (Neo4j) ---------- */

const ENTITY_FAMILY: Record<string, GraphFamily> = {
  Document: "slateDeep",
  Article: "slate",
  Organization: "skyDeep",
  Person: "amber",
  Task: "purple",
  Location: "emerald",
  other: "slate",
};

function EntityGraph({ kbId }: { kbId: string }) {
  const { t } = useT();
  const relationLabel = useRelationLabel();
  const [resp, setResp] = useState<EntityGraphResponse | null>(null);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [applied, setApplied] = useState("");

  useEffect(() => {
    let alive = true;
    setResp(null);
    setError("");
    getEntityGraph(kbId, { q: applied || undefined, limit: 150 })
      .then((r) => alive && setResp(r))
      .catch((e) => alive && setError(errorText(e)));
    return () => {
      alive = false;
    };
  }, [kbId, applied]);

  const types: Record<string, TypeStyle> = useMemo(
    () => typeStyles(ENTITY_FAMILY, (k) => t(`graph.type.${k}` as LocaleKey)),
    [t]
  );
  const view = resp?.data;
  const nodes: CanvasNode[] = useMemo(
    () =>
      (view?.nodes ?? []).map((n) => ({
        id: n.id,
        title: n.name,
        type: ENTITY_FAMILY[n.type] ? n.type : "other",
        weight: n.degree,
      })),
    [view]
  );
  const edges: CanvasEdge[] = useMemo(
    () => (view?.edges ?? []).map((e) => ({ source: e.source, target: e.target, label: relationLabel(e.type) })),
    [view, relationLabel]
  );

  let emptyText = t("graph.entityEmpty");
  if (resp && !resp.graph_enabled) emptyText = t("graph.entityKbOff");

  return (
    <GraphCanvas
      nodes={nodes}
      edges={edges}
      types={types}
      directed
      truncated={view?.truncated}
      loading={resp === null && !error}
      error={error}
      emptyText={emptyText}
      toolbarExtra={
        <form
          className="flex items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            setApplied(query.trim());
          }}
        >
          <input
            type="text"
            className="input h-8 w-56 px-2 text-[12px]"
            placeholder={t("graph.entitySearch")}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          {applied && (
            <button
              type="button"
              className="btn btn-tertiary btn-sm h-7 text-[12px]"
              onClick={() => {
                setQuery("");
                setApplied("");
              }}
            >
              {t("graph.clearFocus")}
            </button>
          )}
        </form>
      }
      renderDetails={(n, nodeEdges, byId) => (
        <>
          <p className="caption mt-2 text-muted">{t("graph.links", { n: n.weight })}</p>
          <RelationList node={n} edges={nodeEdges} byId={byId} label={(e) => e.label ?? ""} />
          <button
            className="btn btn-outline btn-sm mt-3 w-full"
            onClick={() => {
              setQuery(n.title);
              setApplied(n.title);
            }}
          >
            {t("graph.focus")}
          </button>
        </>
      )}
    />
  );
}

/* ---------- Legal relation graph (legal catalogue) ---------- */

const LEGAL_GROUPS: Record<string, string[]> = {
  law: ["Hiến pháp", "Bộ luật", "Luật", "Pháp lệnh", "Sắc lệnh", "Sắc luật", "Lệnh"],
  decree: ["Nghị định"],
  circular: ["Thông tư", "Thông tư liên tịch", "Thông tư liên bộ"],
  decision: ["Quyết định", "Nghị quyết", "Nghị quyết liên tịch", "Chỉ thị"],
};
const LEGAL_FAMILY: Record<string, GraphFamily> = {
  law: "red",
  decree: "skyDeep",
  circular: "emerald",
  decision: "amber",
  other: "slate",
};

function legalGroup(docType: string) {
  for (const [group, docTypes] of Object.entries(LEGAL_GROUPS)) {
    if (docTypes.includes(docType)) return group;
  }
  return "other";
}

function LegalGraph({ kbId, onOpenKnowledge }: { kbId: string; onOpenKnowledge?: (knowledgeId: string) => void }) {
  const { t } = useT();
  const relationLabel = useRelationLabel();
  const [view, setView] = useState<GraphView | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    setView(null);
    setError("");
    getLegalGraph(kbId, 150)
      .then((r) => alive && setView(r.data))
      .catch((e) => alive && setError(errorText(e)));
    return () => {
      alive = false;
    };
  }, [kbId]);

  const types: Record<string, TypeStyle> = useMemo(
    () => typeStyles(LEGAL_FAMILY, (k) => t(`graph.legal.${k}` as LocaleKey)),
    [t]
  );
  const meta = useMemo(() => new Map((view?.nodes ?? []).map((n) => [n.id, n.meta ?? {}])), [view]);
  const nodes: CanvasNode[] = useMemo(
    () => (view?.nodes ?? []).map((n) => ({ id: n.id, title: n.name, type: legalGroup(n.type), weight: n.degree })),
    [view]
  );
  const edges: CanvasEdge[] = useMemo(
    () => (view?.edges ?? []).map((e) => ({ source: e.source, target: e.target, label: relationLabel(e.type) })),
    [view, relationLabel]
  );
  const formatDate = (iso?: string) =>
    (iso && vnFormatDate(iso)) || "—";

  return (
    <GraphCanvas
      nodes={nodes}
      edges={edges}
      types={types}
      directed
      truncated={view?.truncated}
      loading={view === null && !error}
      error={error}
      emptyText={t("graph.legalEmpty")}
      renderDetails={(n, nodeEdges, byId) => {
        const m = meta.get(n.id) ?? {};
        return (
          <>
            {m.title && m.title !== n.title && <p className="caption mt-1 text-muted">{m.title}</p>}
            <dl className="caption mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
              <dt className="text-muted">{t("graph.status")}</dt>
              <dd className="text-ink">{m.status || "—"}</dd>
              <dt className="text-muted">{t("graph.issued")}</dt>
              <dd className="text-ink">{formatDate(m.issued_on)}</dd>
            </dl>
            <p className={`caption mt-2 ${m.in_kb ? "text-success" : "text-muted"}`}>
              {m.in_kb ? t("graph.inKb") : t("graph.notInKb")}
            </p>
            <RelationList node={n} edges={nodeEdges} byId={byId} label={(e) => e.label ?? ""} />
            {m.in_kb && m.knowledge_id && onOpenKnowledge && (
              <button className="btn btn-primary btn-sm mt-3 w-full" onClick={() => onOpenKnowledge(m.knowledge_id)}>
                {t("graph.openDoc")}
              </button>
            )}
          </>
        );
      }}
    />
  );
}
