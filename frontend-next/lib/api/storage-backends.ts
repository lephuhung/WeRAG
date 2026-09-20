/* Ported from frontend/src/api/storage-backend.ts. */
import { apiDel, apiGet, apiPost, apiPut } from "@/lib/api-client";

export interface StorageBackendConfig {
  mode?: string;
  endpoint?: string;
  region?: string;
  access_key_id?: string;
  secret_access_key?: string;
  bucket_name?: string;
  path_prefix?: string;
  app_id?: string;
  use_ssl?: boolean;
  force_path_style?: boolean;
  use_temp_bucket?: boolean;
  temp_bucket_name?: string;
  temp_region?: string;
}

export interface StorageBackend {
  id: string;
  tenant_id?: number;
  name: string;
  provider: string;
  config: StorageBackendConfig;
  source: "user" | "env";
  status: "active" | "disabled";
  legacy_alias?: boolean;
  created_at?: string;
  updated_at?: string;
}

export interface StorageBackendListResponse {
  success: boolean;
  data: StorageBackend[];
  default_storage_backend_id?: string | null;
}

export function listStorageBackends(): Promise<StorageBackendListResponse> {
  return apiGet("/api/v1/storage-backends");
}

export function listStorageBackendTypes(): Promise<{ success: boolean; data: string[] }> {
  return apiGet("/api/v1/storage-backends/types");
}

export function createStorageBackend(data: Partial<StorageBackend>) {
  return apiPost("/api/v1/storage-backends", data);
}

export function updateStorageBackend(id: string, data: Partial<StorageBackend>) {
  return apiPut(`/api/v1/storage-backends/${id}`, data);
}

export function deleteStorageBackend(id: string) {
  return apiDel(`/api/v1/storage-backends/${id}`);
}

export function setDefaultStorageBackend(id: string) {
  return apiPut(`/api/v1/storage-backends/${id}/default`, {});
}

export function testStorageBackend(data: Partial<StorageBackend>) {
  return apiPost("/api/v1/storage-backends/test", data);
}

export function testStorageBackendByID(id: string) {
  return apiPost(`/api/v1/storage-backends/${id}/test`, {});
}
