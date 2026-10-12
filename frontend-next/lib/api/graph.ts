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
  /** Whether the deployment runs a graph store at all (NEO4J_ENABLE). */
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
