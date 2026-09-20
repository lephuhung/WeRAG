/* Ported from frontend/src/api/vector-store.ts. */
import { apiDel, apiGet, apiPost, apiPut } from "@/lib/api-client";

export interface VectorStoreEntity {
  id?: string;
  name: string;
  engine_type: string;
  connection_config: Record<string, unknown>;
  index_config: Record<string, unknown>;
  source: "env" | "user";
  readonly: boolean;
  tenant_id?: number;
  created_at?: string;
  updated_at?: string;
}

export interface VectorStoreTypeInfo {
  type: string;
  display_name: string;
  connection_fields: FieldSchema[];
  index_fields: FieldSchema[];
}

export interface FieldSchema {
  name: string;
  type: "string" | "number" | "boolean";
  required: boolean;
  sensitive?: boolean;
  description?: string;
  default?: unknown;
  /** Inclusive bounds for number fields. When absent the UI falls back to
   * per-field heuristics (isReplicaField). */
  min?: number;
  max?: number;
  /** Closed value set for string fields (e.g. knn_engine ∈ lucene|faiss) —
   * render a select instead of free text. */
  enum?: string[];
  /** Cannot change after store creation (edit mode is read-only). */
  immutable?: boolean;
}

export function listVectorStoreTypes(): Promise<VectorStoreTypeInfo[]> {
  return apiGet<{ success: boolean; data?: VectorStoreTypeInfo[] }>(
    "/api/v1/vector-stores/types",
  ).then((res) => (res.success && res.data ? res.data : []));
}

export function listVectorStores(): Promise<{ success: boolean; data: VectorStoreEntity[] }> {
  return apiGet("/api/v1/vector-stores");
}

export function createVectorStore(data: Partial<VectorStoreEntity>) {
  return apiPost("/api/v1/vector-stores", data);
}

export function updateVectorStore(id: string, data: Partial<VectorStoreEntity>) {
  return apiPut(`/api/v1/vector-stores/${id}`, data);
}

export function deleteVectorStore(id: string) {
  return apiDel(`/api/v1/vector-stores/${id}`);
}

export function testVectorStoreRaw(data: {
  engine_type: string;
  connection_config: unknown;
}): Promise<unknown> {
  return apiPost("/api/v1/vector-stores/test", data);
}

export function testVectorStoreById(id: string): Promise<unknown> {
  return apiPost(`/api/v1/vector-stores/${id}/test`, {});
}
