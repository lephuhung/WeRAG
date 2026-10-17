/* Ported from frontend/src/api/vector-store.ts. */
import { apiDel, apiGet, apiPost } from "@/lib/api-client";

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

export function listVectorStores(): Promise<{ success: boolean; data: VectorStoreEntity[] }> {
  return apiGet("/api/v1/vector-stores");
}

export function deleteVectorStore(id: string) {
  return apiDel(`/api/v1/vector-stores/${id}`);
}

export function testVectorStoreById(id: string): Promise<unknown> {
  return apiPost(`/api/v1/vector-stores/${id}/test`, {});
}
