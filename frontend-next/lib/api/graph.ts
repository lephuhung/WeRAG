import { apiGet } from "@/lib/api-client";

/* The graph drawn on a KB page (internal/handler/knowledge_graph_view.go):
 * the relation graph of its documents in the legal catalogue. */

/* The relation network of a knowledge base (GET …/legal-graph): its
 * catalogued documents, the documents they act on or are acted on by, and
 * the relations between them. Meta: document_number, title, status,
 * issuer, issued_on, in_kb, knowledge_id, knowledge_title. */
export interface GraphViewNode {
  id: string;
  name: string;
  type: string;
  degree: number;
  meta?: Record<string, string>;
}

export interface GraphViewEdge {
  source: string;
  target: string;
  type: string;
}

export interface GraphView {
  nodes: GraphViewNode[];
  edges: GraphViewEdge[];
  truncated: boolean;
}

export function getLegalGraph(kbId: string, limit?: number) {
  return apiGet<{ success: boolean; data: GraphView }>(
    `/api/v1/knowledge-bases/${kbId}/legal-graph${limit ? "?limit=" + limit : ""}`,
  );
}

/* The focused legal graph (GET …/legal-graph/focus): one catalogue
 * document, the documents it acts on ("out") and those acting on it
 * ("in"), and its provision-level links in both directions. */

export interface LegalGraphDoc {
  id: string;
  document_number: string;
  doc_type: string;
  title: string;
  status: string;
  issuer: string;
  /** YYYY-MM-DD or "". */
  issued_on: string;
  effective_on: string;
  in_kb: boolean;
  knowledge_id?: string;
  knowledge_title?: string;
  /** May be focused through this KB (held, or next to a held document). */
  focusable: boolean;
  degree?: number;
}

export interface LegalGraphNeighbour {
  doc: LegalGraphDoc;
  direction: "out" | "in";
  /** Catalogue relations, strongest first; relations[0] draws the edge. */
  relations: string[];
  /** Read from the provision links: the catalogue records no relation. */
  from_provisions?: boolean;
}

export interface LegalGraphProvisionRow {
  id: number;
  /** "dieu:5/khoan:2/diem:a" in the target document ("" unknown). */
  target_path: string;
  target_level: string;
  op: string;
  /** The acting document's provision ("" unknown). */
  source_path: string;
  /** The other document ("" unknown); see LegalGraphFocus.documents. */
  doc_id: string;
  eff_from: string;
  eff_to: string;
}

export interface LegalGraphFocus {
  focus: LegalGraphDoc | null;
  neighbours: LegalGraphNeighbour[];
  /** "<direction>:<relation>" → neighbours before the per-group cap. */
  neighbour_totals: Record<string, number>;
  neighbours_truncated: boolean;
  /** The focus is the target: the other document acts on it. */
  provisions_in: LegalGraphProvisionRow[];
  /** The focus is the source: it acts on the other document. */
  provisions_out: LegalGraphProvisionRow[];
  provisions_in_total: number;
  provisions_out_total: number;
  provisions_truncated: boolean;
  documents: Record<string, LegalGraphDoc>;
  candidates: LegalGraphDoc[];
  anchor_count: number;
}

export function getLegalGraphFocus(kbId: string, params: { doc?: string; q?: string } = {}) {
  const query = new URLSearchParams();
  if (params.doc) query.set("doc", params.doc);
  if (params.q) query.set("q", params.q);
  const qs = query.toString();
  return apiGet<{ success: boolean; data: LegalGraphFocus }>(
    `/api/v1/knowledge-bases/${kbId}/legal-graph/focus${qs ? "?" + qs : ""}`
  );
}

/* The catalogue map (GET /legal-graph/explore): one document and the
 * documents around it to a depth over the whole legal catalogue. depth
 * is signed: +k on the "in" side (acts on the focus, or on what acts on
 * it), -k on the "out" side (what the focus acts on, and so on). */
export interface LegalKnowledgeCopy {
  knowledge_id: string;
  knowledge_base_id: string;
  knowledge_base_name: string;
  title: string;
}

export interface LegalGraphExploreNode {
  doc: LegalGraphDoc;
  depth: number;
  copies?: LegalKnowledgeCopy[];
  /** Per relation, the next-step documents the per-node cap left out. */
  dropped?: Record<string, number>;
}

export interface LegalGraphExploreEdge {
  from: string;
  to: string;
  relation: string;
}

export interface LegalGraphExplore {
  focus: LegalGraphDoc;
  nodes: LegalGraphExploreNode[];
  edges: LegalGraphExploreEdge[];
  depth: number;
  node_cap_hit: boolean;
}

/** A guest sends no organization (a stored one would be refused). */
export function getLegalGraphExplore(doc: string, opts: { depth?: number; isGuest?: boolean; signal?: AbortSignal } = {}) {
  const q = new URLSearchParams({ doc });
  if (opts.depth) q.set("depth", String(opts.depth));
  return apiGet<{ success: boolean; data: LegalGraphExplore }>(`/api/v1/legal-graph/explore?${q.toString()}`, {
    skipTenant: opts.isGuest === true,
    signal: opts.signal,
  });
}
