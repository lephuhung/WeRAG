import { apiGet } from "@/lib/api-client";

/* Graph views drawn on a KB page (internal/handler/knowledge_graph_view.go):
 * the entity graph extracted from the KB's documents (Neo4j) and the
 * relation graph of its documents in the legal catalogue. */

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

export interface EntityGraphResponse {
  success: boolean;
  data: GraphView;
  /** The KB's own graph switch (indexing strategy + extraction). */
  graph_enabled: boolean;
  /** Whether the graph store is connected (always true on a running app). */
  graph_store: boolean;
}

export interface EntityGraphParams {
  q?: string;
  types?: string[];
  knowledgeId?: string;
  limit?: number;
}

export function getEntityGraph(kbId: string, params: EntityGraphParams = {}) {
  const query = new URLSearchParams();
  if (params.q) query.set("q", params.q);
  if (params.types?.length) query.set("types", params.types.join(","));
  if (params.knowledgeId) query.set("knowledge_id", params.knowledgeId);
  if (params.limit) query.set("limit", String(params.limit));
  const qs = query.toString();
  return apiGet<EntityGraphResponse>(
    `/api/v1/knowledge-bases/${kbId}/entity-graph${qs ? "?" + qs : ""}`
  );
}

export function getLegalGraph(kbId: string, limit?: number) {
  return apiGet<{ success: boolean; data: GraphView }>(
    `/api/v1/knowledge-bases/${kbId}/legal-graph${limit ? "?limit=" + limit : ""}`
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
