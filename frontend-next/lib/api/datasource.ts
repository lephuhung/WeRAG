/* Ported from frontend/src/api/datasource/index.ts. */
import { apiDel, apiGet, apiPost, apiPut } from "@/lib/api-client";

export interface DataSource {
  id: string;
  tenant_id: number;
  knowledge_base_id: string;
  name: string;
  type: string;
  config: Record<string, unknown>;
  sync_schedule: string;
  sync_mode: "incremental" | "full";
  status: "active" | "paused" | "error";
  conflict_strategy: "overwrite" | "skip";
  sync_deletions: boolean;
  last_sync_at: string | null;
  last_sync_result: unknown;
  error_message: string;
  // Single-field "credentials" map — DataSource credentials are a
  // per-connector atomic set.
  credentials?: { credentials: { configured: boolean } };
  created_at: string;
  updated_at: string;
  latest_sync_log?: SyncLog;
}

/* One user-facing failure sample. The backend sends a stable i18n `code`
 * (+ interpolation `params`) so the UI localises to the viewer's language;
 * `message` is the fallback when no i18n key exists. */
export interface SyncItemError {
  title?: string;
  code?: string;
  params?: Record<string, string>;
  message?: string;
}

export interface SyncResultDetail {
  total?: number;
  created?: number;
  updated?: number;
  deleted?: number;
  skipped?: number;
  failed?: number;
  /** Per-item failure samples (capped); localised in the sync-log drawer. */
  errors?: SyncItemError[];
}

export interface SyncLog {
  id: string;
  data_source_id: string;
  status: "running" | "success" | "partial" | "failed" | "canceled";
  started_at: string;
  finished_at: string | null;
  items_total: number;
  items_created: number;
  items_updated: number;
  items_deleted: number;
  items_skipped: number;
  items_failed: number;
  error_message: string;
  result?: SyncResultDetail;
}

export interface ConnectorMeta {
  type: string;
  name: string;
  description: string;
  icon: string;
  priority: number;
  auth_type: string;
  capabilities: string[];
}

export interface Resource {
  external_id: string;
  name: string;
  type: string;
  description: string;
  url: string;
  parent_id?: string;
  has_children?: boolean;
}

export function getConnectorTypes() {
  return apiGet<{ success: boolean; data: ConnectorMeta[] }>("/api/v1/datasource/types");
}

export function listDataSources(kbId: string) {
  return apiGet<{ success: boolean; data: DataSource[] }>(
    `/api/v1/datasource?kb_id=${encodeURIComponent(kbId)}`,
  );
}

export function getDataSource(id: string) {
  return apiGet<{ success: boolean; data: DataSource }>(`/api/v1/datasource/${id}`);
}

export function createDataSource(data: Partial<DataSource>) {
  return apiPost<{ success: boolean; data: DataSource }>("/api/v1/datasource", data);
}

export function updateDataSource(id: string, data: Partial<DataSource>) {
  return apiPut<{ success: boolean; data: DataSource }>(`/api/v1/datasource/${id}`, data);
}

export function deleteDataSource(id: string) {
  return apiDel(`/api/v1/datasource/${id}`);
}

export function validateConnection(id: string) {
  return apiPost(`/api/v1/datasource/${id}/validate`, {});
}

/** Validate credentials without persisting (creation or credential replacement). */
export function validateCredentials(type: string, credentials: Record<string, unknown>) {
  return apiPost("/api/v1/datasource/validate-credentials", { type, credentials });
}

/* Lists selectable resources for a data source. Pass parentId to lazily load
 * the direct children of a resource (e.g. expanding a Feishu wiki
 * space/node), avoiding a full tree traversal up front. */
export function listResources(id: string, parentId?: string) {
  const query = parentId ? `?parent_id=${encodeURIComponent(parentId)}` : "";
  return apiGet<{ success: boolean; data: Resource[] }>(`/api/v1/datasource/${id}/resources${query}`, {
    timeoutMs: 120_000,
  });
}

/* Returns the ExternalIDs of every parent that must be expanded to reveal the
 * given (possibly deeply nested) selections in a lazily loaded picker. */
export function resolveResourceAncestors(id: string, resourceIds: string[]) {
  return apiPost<{ success: boolean; data: string[] }>(
    `/api/v1/datasource/${id}/resource-ancestors`,
    { resource_ids: resourceIds },
    { timeoutMs: 120_000 },
  );
}

export function triggerSync(id: string) {
  return apiPost(`/api/v1/datasource/${id}/sync`, {});
}

export function pauseDataSource(id: string) {
  return apiPost(`/api/v1/datasource/${id}/pause`, {});
}

export function resumeDataSource(id: string) {
  return apiPost(`/api/v1/datasource/${id}/resume`, {});
}

export function getSyncLogs(id: string, limit = 20, offset = 0) {
  return apiGet<{ success: boolean; data: SyncLog[]; total?: number }>(
    `/api/v1/datasource/${id}/logs?limit=${limit}&offset=${offset}`,
  );
}

/* Credential subresource: unlike the other resources, DataSource exposes a
 * single logical "credentials" field because connector auth is a
 * per-connector atomic map. */
export interface DataSourceCredentialsResponse {
  fields: {
    credentials: { configured: boolean };
  };
}

export async function putDataSourceCredentials(
  id: string,
  credentials: Record<string, unknown>,
): Promise<DataSourceCredentialsResponse> {
  const response = await apiPut<{ data?: DataSourceCredentialsResponse }>(
    `/api/v1/datasource/${id}/credentials`,
    { credentials },
  );
  return (response.data ?? response) as DataSourceCredentialsResponse;
}

export async function deleteDataSourceCredentials(id: string): Promise<void> {
  await apiDel(`/api/v1/datasource/${id}/credentials/credentials`);
}
