"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { getLegalGraph, type GraphView, type GraphViewNode } from "@/lib/api/graph";
import { GraphCanvas, type CanvasEdge, type CanvasNode, type TypeStyle } from "@/components/graph-canvas";
import { GRAPH_FAMILY } from "@/components/graph/graph-palette";
import { LEGAL_FAMILY, StatusChip, legalGroup } from "@/components/graph/legal-style";
import { IconGraph } from "@/components/icons";
import { useT, type LocaleKey } from "@/lib/i18n";
import { formatDate } from "@/lib/datetime";
import { userErrorMessage } from "@/lib/user-error";

/* The knowledge graph of a KB: every catalogued document of the KB and the
 * documents they act on or are acted on by, as a force-directed network of
 * bubbles (số hiệu inside, colour by document type, size by relations),
 * joined by the fixed legal relations (thay thế, bãi bỏ, sửa đổi, hướng
 * dẫn, căn cứ…). No model: the relations come from the legal catalogue and
 * the documents' own texts. A bubble's card opens the document in the KB
 * or its multi-tier map under the validity lookup. */

const NODE_BUDGET = 300;

export function LegalNetwork({ kbId, onOpenKnowledge }: { kbId: string; onOpenKnowledge?: (knowledgeId: string) => void }) {
  const { t } = useT();
  const [view, setView] = useState<GraphView | null>(null);
  const [error, setError] = useState("");
  // documents with no relation sit outside the network; off by default
  const [showIsolated, setShowIsolated] = useState(false);

  useEffect(() => {
    let alive = true;
    setView(null);
    setError("");
    getLegalGraph(kbId, NODE_BUDGET)
      .then((r) => alive && setView(r.data))
      .catch((e) => alive && setError(userErrorMessage(e, t, "graph.legalLoadFailed")));
    return () => {
      alive = false;
    };
    // t changes with the locale only
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kbId]);

  const byId = useMemo(() => new Map((view?.nodes ?? []).map((n) => [n.id, n])), [view]);
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
  const isolated = useMemo(() => (view?.nodes ?? []).filter((n) => n.degree === 0).length, [view]);
  const nodes: CanvasNode[] = useMemo(
    () =>
      (view?.nodes ?? [])
        .filter((n) => showIsolated || n.degree > 0)
        .map((n) => ({
          id: n.id,
          title: n.meta?.document_number || n.name,
          type: legalGroup(n.type),
          weight: n.degree,
        })),
    [view, showIsolated],
  );
  const edges: CanvasEdge[] = useMemo(
    () =>
      (view?.edges ?? []).map((e) => ({
        source: e.source,
        target: e.target,
        label: t(`graph.lf.edge.${e.type}` as LocaleKey),
        dashed: e.type === "based_on" || e.type === "references",
      })),
    [view, t],
  );

  return (
    <GraphCanvas
      nodes={nodes}
      edges={edges}
      types={types}
      directed
      loading={view === null && !error}
      error={error}
      truncated={view?.truncated}
      emptyText={t(isolated > 0 && !showIsolated ? "graph.legalOnlyIsolated" : "graph.legalEmpty")}
      toolbarExtra={
        isolated > 0 ? (
          <label className="flex cursor-pointer items-center gap-1.5 whitespace-nowrap text-[12px] text-muted">
            <input type="checkbox" checked={showIsolated} onChange={(e) => setShowIsolated(e.target.checked)} />
            {t("graph.showIsolated", { n: isolated })}
          </label>
        ) : undefined
      }
      renderDetails={(n) => <NodeDetails node={byId.get(n.id)} onOpenKnowledge={onOpenKnowledge} />}
    />
  );
}

function NodeDetails({ node, onOpenKnowledge }: { node?: GraphViewNode; onOpenKnowledge?: (id: string) => void }) {
  const { t } = useT();
  if (!node) return null;
  const m = node.meta ?? {};
  return (
    <>
      {m.title && m.title !== node.name && <p className="caption mt-1 text-muted">{m.title}</p>}
      <p className="caption mt-2 flex flex-wrap items-center gap-2 text-muted">
        <StatusChip status={m.status ?? ""} />
        {[m.issuer, formatDate(m.issued_on)].filter(Boolean).join(" · ")}
      </p>
      <div className="mt-3 flex flex-col gap-1.5">
        {m.in_kb === "true" && m.knowledge_id && onOpenKnowledge && (
          <button onClick={() => onOpenKnowledge(m.knowledge_id)} className="btn btn-primary btn-sm w-full">
            {t("graph.lf.openInKb")}
          </button>
        )}
        <Link href={`/platform/legal-lookup?doc=${encodeURIComponent(node.id)}`} className="btn btn-outline btn-sm w-full gap-1.5">
          <IconGraph className="h-3.5 w-3.5" />
          {t("lmap.openMap")}
        </Link>
      </div>
    </>
  );
}
