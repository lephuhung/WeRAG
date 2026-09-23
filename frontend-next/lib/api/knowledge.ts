/* Ported from frontend/src/api/knowledge-base/index.ts + retrieval.ts +
 * chunker/index.ts. Audit types come from ./audit (tenant/audit-log.ts).
 */
import {
  apiDel,
  apiDownload,
  apiGet,
  apiPost,
  apiPostDownload,
  apiPut,
  apiUpload,
} from "@/lib/api-client";
import type { AuditLog, AuditOutcome, ListAuditLogResponse } from "./audit";

/* Matches backend types.KnowledgeProcessOverrides (snake_case JSON). */
export interface ParserEngineRule {
  file_types: string[];
  engine: string;
  xlsx_first_row_as_header?: boolean;
}

export interface ChunkingConfigOverride {
  chunk_size?: number;
  chunk_overlap?: number;
  separators?: string[];
  parser_engine_rules?: ParserEngineRule[];
  enable_parent_child?: boolean;
  parent_chunk_size?: number;
  child_chunk_size?: number;
  strategy?: string;
  token_limit?: number;
  languages?: string[];
  table_metadata_instructions?: string;
}

export interface VLMConfigOverride {
  enabled?: boolean;
  model_id?: string;
  description_language?: string;
  custom_instructions?: string;
}

export interface ASRConfigOverride {
  enabled?: boolean;
  model_id?: string;
  language?: string;
}

export interface QuestionGenerationConfigOverride {
  enabled?: boolean;
  question_count?: number;
  custom_instructions?: string;
}

export interface GraphNodeOverride {
  name: string;
  attributes?: string[];
}

export interface GraphRelationOverride {
  node1: string;
  node2: string;
  type: string;
}

export interface ExtractConfigOverride {
  enabled?: boolean;
  text?: string;
  tags?: string[];
  nodes?: GraphNodeOverride[];
  relations?: GraphRelationOverride[];
  custom_instructions?: string;
}

export interface KnowledgeProcessOverrides {
  summary_enabled?: boolean;
  parser_engine_rules?: ParserEngineRule[];
  chunking_config?: ChunkingConfigOverride;
  enable_multimodel?: boolean;
  vlm_config?: VLMConfigOverride;
  asr_config?: ASRConfigOverride;
  question_generation_config?: QuestionGenerationConfigOverride;
  graph_enabled?: boolean;
  extract_config?: ExtractConfigOverride;
  parser_engine_overrides?: Record<string, string>;
}

// ---- knowledge base activity -------------------------------------------------

export type KnowledgeBaseActivity = AuditLog;

export interface ListKnowledgeBaseActivityParams {
  after_id?: number;
  limit?: number;
  action?: string;
  outcome?: AuditOutcome;
  actor?: string;
}

export function listKnowledgeBaseActivity(
  id: string,
  params: ListKnowledgeBaseActivityParams = {},
): Promise<ListAuditLogResponse> {
  const query = new URLSearchParams();
  if (params.after_id) query.set("after_id", String(params.after_id));
  if (params.limit) query.set("limit", String(params.limit));
  if (params.action) query.set("action", params.action);
  if (params.outcome) query.set("outcome", params.outcome);
  if (params.actor) query.set("actor", params.actor);
  const qs = query.toString();
  return apiGet(`/api/v1/knowledge-bases/${id}/activity${qs ? `?${qs}` : ""}`);
}

// ---- knowledge bases ----------------------------------------------------------

/* Mirrors types.KBVisibility (internal/types/knowledgebase.go): 'tenant'
 * scopes read/search to the owning workspace's members; 'public' makes the
 * KB readable by every authenticated user of every tenant. Writes always
 * stay with the owning tenant. Cross-tenant read access beyond 'public'
 * goes through kb_access_grants (share_count), not a third scope value. */
export type KBVisibility = "tenant" | "public";

export type KnowledgeBaseRow = {
  id: string;
  name: string;
  description?: string;
  /* Backend actually sends knowledge_count (types.KnowledgeBase json tag);
   * document_count is a legacy alias kept so the ported Next surfaces keep
   * compiling while migrating to the real name. */
  knowledge_count?: number;
  document_count?: number;
  doc_count?: number;
  chunk_count?: number;
  updated_at?: string;
  created_at?: string;
  is_pinned?: boolean;
  creator_id?: string;
  creator_name?: string;
  type?: string;
  visibility?: KBVisibility;
  /* Owning workspace id — differs from the caller's tenant on public or
   * grant-shared KBs that leak into the list from other tenants. */
  tenant_id?: number;
  /* Live cross-tenant access grants on this KB (kb_access_grants). */
  share_count?: number;
  /* Process-config defaults echoed back on the detail response
   * (GET /knowledge-bases/:id); they seed the upload/reparse parse-settings
   * dialog. Loosely typed because the KB detail payload is a superset of
   * the list row. */
  chunking_config?: unknown;
  vlm_config?: {
    enabled: boolean;
    model_id?: string;
    description_language?: string;
    custom_instructions?: string;
  };
  asr_config?: { enabled: boolean; model_id?: string; language?: string };
  question_generation_config?: {
    enabled?: boolean;
    question_count?: number;
    custom_instructions?: string;
  };
  extract_config?: unknown;
  indexing_strategy?: {
    vector_enabled: boolean;
    keyword_enabled: boolean;
    wiki_enabled: boolean;
    graph_enabled: boolean;
  };
};

type ListResponse = {
  success: boolean;
  data?: KnowledgeBaseRow[] | { items?: KnowledgeBaseRow[]; list?: KnowledgeBaseRow[] };
  total?: number;
  message?: string;
};

function normalizeList(res: ListResponse): KnowledgeBaseRow[] {
  if (Array.isArray(res.data)) return res.data;
  const d = res.data as { items?: KnowledgeBaseRow[]; list?: KnowledgeBaseRow[] } | undefined;
  return d?.items ?? d?.list ?? [];
}

/* Read-only vector-store binding metadata enriched onto every KB response.
 *
 *   - source 'env'    → KB uses the tenant's env-configured store
 *                       (RETRIEVE_DRIVER). vector_store_id is null and
 *                       vector_store_name is the localized "System default"
 *                       label; vector_store_engine_type still reports the
 *                       underlying engine (e.g. "postgres").
 *   - source 'user'   → KB is bound to a tenant-owned VectorStore.
 *   - source 'shared' → KB belongs to a different tenant, readable via
 *                       cross-organization sharing. The server strips
 *                       vector_store_id and engine_type to avoid leaking
 *                       the owner tenant's store inventory.
 *   - status 'unavailable' → the binding cannot be reached right now
 *                       (deleted row, registry miss, transient infra
 *                       failure). Recover via the Vector Stores settings. */
export type VectorStoreSource = "env" | "user" | "shared" | "unavailable";
export type VectorStoreStatus = "available" | "unavailable";

export interface KnowledgeBaseStoreView {
  vector_store_id?: string | null;
  vector_store_name?: string;
  vector_store_engine_type?: string;
  vector_store_source?: VectorStoreSource;
  vector_store_status?: VectorStoreStatus;
}

export function listKnowledgeBases(params?: {
  agent_id?: string;
  agent_source_tenant_id?: string;
  /* Creator filter. "mine" → creator_id matches caller; "others" →
   * created by someone else in this tenant; omitted/"all" → no filter.
   * KBs predating the RBAC backfill (creator_id="") match neither view. */
  creator?: "all" | "mine" | "others";
}) {
  const query = new URLSearchParams();
  if (params?.agent_id) query.set("agent_id", params.agent_id);
  if (params?.agent_source_tenant_id)
    query.set("agent_source_tenant_id", params.agent_source_tenant_id);
  if (params?.creator && params.creator !== "all") query.set("creator", params.creator);
  const qs = query.toString();
  return apiGet<ListResponse>(`/api/v1/knowledge-bases${qs ? `?${qs}` : ""}`).then(normalizeList);
}

export function createKnowledgeBase(data: {
  name: string;
  description?: string;
  type?: "document" | "faq";
  /* Read scope: 'tenant' (default) readable by every member of the owning
   * workspace; 'public' readable by every tenant — creating it is restricted
   * server-side to the workspace Owner or a system admin. */
  visibility?: KBVisibility;
  chunking_config?: unknown;
  embedding_model_id?: string;
  summary_model_id?: string;
  auto_tag_config?: {
    enabled: boolean;
    model_id?: string;
    max_tags?: number;
    skip_if_tagged?: boolean;
  };
  /* Opt-in binding to a tenant-owned VectorStore. Omit to fall back to
   * the env-configured store. Immutable after creation. */
  vector_store_id?: string;
  /* Concrete tenant-owned storage instance. When omitted, the tenant
   * default backend is bound by the server at creation time. */
  storage_backend_id?: string;
  vlm_config?: {
    enabled: boolean;
    model_id?: string;
    description_language?: string;
    custom_instructions?: string;
  };
  storage_provider_config?: { provider: string };
  storage_config?: unknown; // legacy, kept for backward compat (dual-write)
  asr_config?: { enabled: boolean; model_id?: string; language?: string };
  extract_config?: unknown;
  faq_config?: { index_mode: string; question_index_mode?: string };
  wiki_config?: {
    synthesis_model_id?: string;
    max_pages_per_ingest?: number;
    extraction_granularity?: "focused" | "standard" | "exhaustive";
    content_instructions?: string;
    extraction_instructions?: string;
  };
  indexing_strategy?: {
    vector_enabled: boolean;
    keyword_enabled: boolean;
    wiki_enabled: boolean;
    graph_enabled: boolean;
  };
}) {
  return apiPost<{ success: boolean; data?: KnowledgeBaseRow }>(`/api/v1/knowledge-bases`, data);
}

export function getKnowledgeBase(
  id: string,
  options?: { agent_id?: string; agent_source_tenant_id?: string },
) {
  const query = new URLSearchParams();
  if (options?.agent_id) query.set("agent_id", options.agent_id);
  if (options?.agent_source_tenant_id)
    query.set("agent_source_tenant_id", options.agent_source_tenant_id);
  const qs = query.toString();
  return apiGet<{ success: boolean; data?: KnowledgeBaseRow }>(
    `/api/v1/knowledge-bases/${id}${qs ? `?${qs}` : ""}`,
  ).then((r) => r.data);
}

export function updateKnowledgeBase(
  id: string,
  data: {
    name: string;
    description?: string;
    config?: {
      chunking_config?: unknown;
      image_processing_config?: unknown;
      faq_config?: unknown;
      wiki_config?: {
        synthesis_model_id?: string;
        max_pages_per_ingest?: number;
        extraction_granularity?: "focused" | "standard" | "exhaustive";
        content_instructions?: string;
        extraction_instructions?: string;
      };
      auto_tag_config?: {
        enabled: boolean;
        model_id?: string;
        max_tags?: number;
        skip_if_tagged?: boolean;
      };
      profile_config?: KnowledgeBaseProfileConfig;
      indexing_strategy?: {
        vector_enabled: boolean;
        keyword_enabled: boolean;
        wiki_enabled: boolean;
        graph_enabled: boolean;
      };
    };
  },
) {
  return apiPut(`/api/v1/knowledge-bases/${id}`, data);
}

/* Changes the KB scope (PUT /knowledge-bases/:id/visibility). The route is
 * workspace Owner only; the service additionally requires Owner/SystemAdmin
 * for 'public' and rejects callers outside the owning tenant. */
export function updateKnowledgeBaseVisibility(
  id: string,
  data: { visibility: KBVisibility },
) {
  return apiPut(`/api/v1/knowledge-bases/${id}/visibility`, data);
}

// ---- cross-tenant access grants -------------------------------------------

/* Tenant-to-tenant read grants (kb_access_grants — see internal/types/
 * kb_access_grant.go). The grantee tenant's owner files a request on a
 * foreign KB; the owning tenant's owner approves, rejects or revokes.
 * Every grant endpoint is workspace-Owner gated server-side. */
export type KBGrantStatus = "pending" | "approved" | "rejected" | "revoked" | "expired";

export interface KBAccessGrant {
  id: string;
  kb_id: string;
  kb_name?: string;
  owner_tenant_id: number;
  owner_tenant_name?: string;
  grantee_tenant_id: number;
  grantee_tenant_name?: string;
  permission: "viewer" | "editor" | "admin" | string;
  status: KBGrantStatus;
  requested_by?: string;
  approved_by?: string | null;
  message?: string;
  expires_at?: string | null;
  responded_at?: string | null;
  created_at?: string;
}

type KBGrantListResponse = { success: boolean; data?: KBAccessGrant[]; message?: string };
type KBGrantResponse = { success: boolean; data?: KBAccessGrant; message?: string };

function grantStatusQuery(statuses?: KBGrantStatus[]): string {
  return statuses?.length ? `?status=${statuses.join(",")}` : "";
}

/* Grantee side: ask the owning tenant for read access to kbId. 400 when the
 * KB is already ours, 404 when it doesn't exist, 409 when a live grant or
 * pending request already covers the pair. */
export function requestKBAccess(
  kbId: string,
  data: { message?: string; expires_at?: string } = {},
): Promise<KBGrantResponse> {
  return apiPost(`/api/v1/knowledge-bases/${kbId}/access-requests`, data);
}

/* Owner side: every grant touching KBs this tenant owns, optionally
 * filtered by ?status=pending,approved,… */
export function listIncomingKBGrants(
  tenantId: number | string,
  statuses?: KBGrantStatus[],
): Promise<KBAccessGrant[]> {
  return apiGet<KBGrantListResponse>(
    `/api/v1/tenants/${tenantId}/access-grants${grantStatusQuery(statuses)}`,
  ).then((r) => r.data ?? []);
}

/* Grantee side: requests this tenant has filed on foreign KBs. */
export function listOutgoingKBGrants(statuses?: KBGrantStatus[]): Promise<KBAccessGrant[]> {
  return apiGet<KBGrantListResponse>(
    `/api/v1/access-grants${grantStatusQuery(statuses)}`,
  ).then((r) => r.data ?? []);
}

/* Owner side: approve or reject a pending request. 409 when the row was
 * already finalised. */
export function reviewKBGrant(
  tenantId: number | string,
  grantId: string,
  data: { approved: boolean; message?: string },
): Promise<KBGrantResponse> {
  return apiPut(`/api/v1/tenants/${tenantId}/access-grants/${grantId}`, data);
}

/* Owner side: withdraw an approved grant. */
export function revokeKBGrant(
  tenantId: number | string,
  grantId: string,
): Promise<KBGrantResponse> {
  return apiDel(`/api/v1/tenants/${tenantId}/access-grants/${grantId}`);
}

/** Opt-in automatic generation of the knowledge-base description. */
export interface KnowledgeBaseProfileConfig {
  enabled: boolean;
  model_id?: string;
  custom_instructions?: string;
}

export interface KnowledgeBaseProfileNamedCount {
  name: string;
  count: number;
}

/* Machine-generated KB description. Derived from per-document profiles;
 * never overwrites the user-authored description. */
export interface KnowledgeBaseProfile {
  gist?: string;
  topics?: string[];
  typical_questions?: string[];
  stats?: {
    document_count: number;
    profiled_count: number;
    file_types?: KnowledgeBaseProfileNamedCount[];
    tags?: KnowledgeBaseProfileNamedCount[];
    raw_topics?: KnowledgeBaseProfileNamedCount[];
    doc_types?: KnowledgeBaseProfileNamedCount[];
    folders?: string[];
    earliest_at?: string;
    latest_at?: string;
  };
  aggregate_hash?: string;
  status?: "ready" | "empty" | "failed" | string;
  error?: string;
  model_id?: string;
  generated_at?: string;
}

/** Regenerates the AI description of a knowledge base synchronously. */
export function generateKnowledgeBaseProfile(id: string) {
  return apiPost(`/api/v1/knowledge-bases/${id}/profile/generate`, {});
}

export function deleteKnowledgeBase(id: string) {
  return apiDel(`/api/v1/knowledge-bases/${id}`);
}

export function copyKnowledgeBase(data: { source_id: string; target_id?: string }) {
  return apiPost(`/api/v1/knowledge-bases/copy`, data);
}

export function duplicateKnowledgeBase(id: string) {
  return apiPost(`/api/v1/knowledge-bases/${id}/duplicate`, {});
}

/** Move targets: same type, same embedding model. */
export function listMoveTargets(sourceKbId: string) {
  return apiGet(`/api/v1/knowledge-bases/${sourceKbId}/move-targets`);
}

export function moveKnowledge(data: {
  knowledge_ids: string[];
  source_kb_id: string;
  target_kb_id: string;
  mode: "reuse_vectors" | "reparse";
}) {
  return apiPost("/api/v1/knowledge/move", data);
}

export function getKnowledgeMoveProgress(taskId: string) {
  return apiGet(`/api/v1/knowledge/move/progress/${taskId}`);
}

export function togglePinKnowledgeBase(id: string) {
  return apiPut(`/api/v1/knowledge-bases/${id}/pin`, {});
}

// ---- knowledge documents -----------------------------------------------------

export type KnowledgeDoc = {
  id: string;
  title?: string;
  file_name?: string;
  /* Real backend field (types.Knowledge json:parse_status):
   * pending/processing/finalizing/completed/failed/cancelled. The phantom
   * `status` alias is kept for callers written against the old shape. */
  parse_status?: string;
  status?: string;
  knowledge_base_id?: string;
  type?: string;
  source?: string;
  channel?: string;
  /* Model-written summary/description block. Description is the full
   * summary; Profile is the structured companion card (gist, topics,
   * doc_type, typical_question). summary_status tracks async generation:
   * none/pending/processing/completed/failed. */
  description?: string;
  profile?: {
    gist?: string;
    topics?: string[];
    doc_type?: string;
    typical_question?: string;
  };
  summary_status?: string;
  enable_status?: string;
  /* File metadata from ingestion */
  file_type?: string;
  file_size?: number;
  folder_path?: string;
  custom_metadata?: Record<string, unknown>;
  updated_at?: string;
  created_at?: string;
  processed_at?: string | null;
  chunk_count?: number;
  tags?: Array<{ id?: string; tag_seq_id?: number; name: string }>;
};
export interface ListKnowledgeFilesParams {
  page: number;
  page_size: number;
  tag_ids?: string;
  keyword?: string;
  file_type?: string;
  parse_status?: string;
  source?: string;
  start_time?: string;
  end_time?: string;
  /* Folder to browse. Empty string = KB root; only sent when defined —
   * leaving it out lists every folder (the flat view). */
  folder_path?: string;
  /** Include documents stored in sub-folders of folder_path. */
  folder_recursive?: boolean;
}

export function listKnowledgeFiles(kbId: string, params: ListKnowledgeFilesParams) {
  const query = new URLSearchParams();
  query.append("page", String(params.page));
  query.append("page_size", String(params.page_size));
  if (params.tag_ids) query.append("tag_ids", params.tag_ids);
  if (params.keyword) query.append("keyword", params.keyword);
  if (params.file_type) query.append("file_type", params.file_type);
  if (params.parse_status) query.append("parse_status", params.parse_status);
  if (params.source) query.append("source", params.source);
  if (params.start_time) query.append("start_time", params.start_time);
  if (params.end_time) query.append("end_time", params.end_time);
  if (params.folder_path !== undefined) {
    query.append("folder_path", params.folder_path);
    if (params.folder_recursive) query.append("folder_recursive", "true");
  }
  return apiGet<{ success: boolean; data?: KnowledgeDoc[] | { items?: KnowledgeDoc[] }; total?: number }>(
    `/api/v1/knowledge-bases/${kbId}/knowledge?${query.toString()}`,
  );
}

export function uploadKnowledgeFile(
  kbId: string,
  data: {
    file: File;
    tag_ids?: string[];
    fileName?: string;
    process_config?: KnowledgeProcessOverrides | string;
    [key: string]: unknown;
  },
  onProgress?: (percent: number) => void,
  opts?: { signal?: AbortSignal },
) {
  const formData = new FormData();
  for (const [key, value] of Object.entries(data)) {
    if (value === undefined) continue;
    if (key === "tag_ids" && Array.isArray(value)) {
      formData.append(key, value.join(","));
    } else if (key === "process_config" && value && typeof value !== "string") {
      formData.append(key, JSON.stringify(value));
    } else {
      formData.append(key, value as string | Blob);
    }
  }
  return apiUpload(`/api/v1/knowledge-bases/${kbId}/knowledge/file`, formData, onProgress, opts);
}

export function createKnowledgeFromURL(
  kbId: string,
  data: {
    url: string;
    enable_multimodel?: boolean;
    tag_ids?: string[];
    process_config?: KnowledgeProcessOverrides;
  },
) {
  return apiPost(`/api/v1/knowledge-bases/${kbId}/knowledge/url`, data);
}

export function createManualKnowledge(
  kbId: string,
  data: {
    title: string;
    content: string;
    status: string;
    tag_ids?: string[];
    process_config?: KnowledgeProcessOverrides;
  },
) {
  return apiPost(`/api/v1/knowledge-bases/${kbId}/knowledge/manual`, data);
}

/** One node of the knowledge base folder tree. */
export interface KnowledgeFolderNode {
  /** Canonical folder path, e.g. "docs/spec". */
  path: string;
  /** Last segment of the path, used as the row label. */
  name: string;
  /** Documents stored directly in this folder. */
  document_count: number;
  /** Documents in this folder plus every descendant folder. */
  total_count: number;
  children?: KnowledgeFolderNode[];
}

export interface KnowledgeFolderTree {
  /** Documents that are not part of any uploaded folder. */
  root_document_count: number;
  /** Documents in the whole knowledge base. */
  total_document_count: number;
  folders: KnowledgeFolderNode[];
}

export function listKnowledgeFolders(kbId: string) {
  return apiGet<{ success: boolean; data?: KnowledgeFolderTree }>(
    `/api/v1/knowledge-bases/${kbId}/knowledge/folders`,
  );
}

/* Re-file documents under `folderPath` ('' = KB top level). Folders are
 * derived from stored paths, so a path that does not exist yet is created
 * by this call. Only grouping changes; documents are not re-parsed. */
export function moveKnowledgeToFolder(kbId: string, ids: string[], folderPath: string) {
  return apiPost("/api/v1/knowledge/folder", {
    kb_id: kbId,
    knowledge_ids: ids,
    folder_path: folderPath,
  });
}

/** Rename or move a folder together with everything below it. */
export function renameKnowledgeFolder(kbId: string, from: string, to: string) {
  return apiPut(`/api/v1/knowledge-bases/${kbId}/knowledge/folders`, { from, to });
}

export function getKnowledgeDetails(
  id: string,
  options?: { agent_id?: string; agent_source_tenant_id?: string },
) {
  const query = new URLSearchParams();
  if (options?.agent_id) query.set("agent_id", options.agent_id);
  if (options?.agent_source_tenant_id)
    query.set("agent_source_tenant_id", options.agent_source_tenant_id);
  const qs = query.toString();
  return apiGet(`/api/v1/knowledge/${id}${qs ? `?${qs}` : ""}`);
}

export function updateManualKnowledge(
  id: string,
  data: { title: string; content: string; status: string; process_config?: KnowledgeProcessOverrides },
) {
  return apiPut(`/api/v1/knowledge/manual/${id}`, data);
}

export function reparseKnowledge(id: string, data?: { process_config?: KnowledgeProcessOverrides }) {
  return apiPost(`/api/v1/knowledge/${id}/reparse`, data);
}

export function cancelKnowledgeParse(id: string) {
  return apiPost(`/api/v1/knowledge/${id}/cancel-parse`, {});
}

export function getKnowledgeSpans(id: string, attempt?: number) {
  const qs = attempt ? `?attempt=${attempt}` : "";
  return apiGet(`/api/v1/knowledge/${id}/spans${qs}`);
}

export function deleteKnowledge(id: string) {
  return apiDel(`/api/v1/knowledge/${id}`);
}

/* Batch delete within one KB. The backend validates every id belongs to
 * kb_id and that the caller has edit permission. */
export function batchDeleteKnowledge(kbId: string, ids: string[]) {
  return apiPost(`/api/v1/knowledge/batch-delete`, { kb_id: kbId, ids });
}

export function downloadKnowledge(id: string): Promise<Blob> {
  return apiDownload(`/api/v1/knowledge/${id}/download`);
}

export function previewKnowledgeFile(id: string): Promise<Blob> {
  return apiDownload(`/api/v1/knowledge/${id}/preview`);
}

export interface KnowledgeSemanticSearchResult {
  id?: string;
  chunk_id?: string;
  chunk_index: number;
  knowledge_id: string;
  knowledge_base_id: string;
  knowledge_title?: string;
  knowledge_filename?: string;
  kb_name?: string;
  content: string;
  matched_content?: string;
  match_type?: "vector" | "keyword" | string;
  score: number;
}

export function knowledgeSemanticSearch(data: {
  query: string;
  knowledge_base_ids?: string[];
  knowledge_ids?: string[];
}): Promise<{ success: boolean; data?: KnowledgeSemanticSearchResult[] }> {
  return apiPost("/api/v1/knowledge-search", data);
}

/* ZIP download reusing the login + tenant headers — never put credentials
 * in the download URL. POST + body per the backend route; 5-minute budget
 * for large batches. */
export function batchDownloadKnowledge(
  kbId: string,
  ids: string[],
  signal?: AbortSignal,
): Promise<Blob> {
  return apiPostDownload(
    `/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/knowledge/batch-download`,
    { ids },
    { timeoutMs: 300_000, signal },
  );
}

/** @param idsQueryString - query string with ids (e.g. ids=xxx&ids=yyy) */
export function batchQueryKnowledge(
  idsQueryString: string,
  kbId?: string,
  agentId?: string,
  agentSourceTenantId?: string,
) {
  let qs = idsQueryString;
  if (kbId) qs += `&kb_id=${encodeURIComponent(kbId)}`;
  if (agentId) qs += `&agent_id=${encodeURIComponent(agentId)}`;
  if (agentSourceTenantId) qs += `&agent_source_tenant_id=${encodeURIComponent(agentSourceTenantId)}`;
  return apiGet<{ success: boolean; data?: KnowledgeDoc[] }>(`/api/v1/knowledge/batch?${qs}`);
}

// ---- chunks -------------------------------------------------------------------

export const KNOWLEDGE_CHUNK_PAGE_SIZE = 25;

/* `includeImageText` asks the backend to splice each chunk's image-derived
 * text (OCR, falling back to caption) into `content` at the image
 * placeholder. Scanned / image-only documents keep their real text on
 * image_ocr children — the default text-only response carries nothing but
 * the page-image link, which renders blank in the extracted-text view. */
export function listKnowledgeChunks(
  id: string,
  page: number,
  opts?: { includeImageText?: boolean },
) {
  const extra = opts?.includeImageText ? "&include_image_text=true" : "";
  return apiGet(`/api/v1/chunks/${id}?page=${page}&page_size=${KNOWLEDGE_CHUNK_PAGE_SIZE}${extra}`);
}

export interface ChunkEditPayload {
  content?: string;
  is_enabled?: boolean;
  expected_revision?: number;
}

export function updateDocumentChunk(knowledgeId: string, chunkId: string, data: ChunkEditPayload) {
  return apiPut(`/api/v1/chunks/${knowledgeId}/${chunkId}`, data);
}

export function listChunkRevisions(knowledgeId: string, chunkId: string) {
  return apiGet(`/api/v1/chunks/${knowledgeId}/${chunkId}/revisions`);
}

export function revertDocumentChunk(
  knowledgeId: string,
  chunkId: string,
  revision: number,
  expectedRevision: number,
) {
  return apiPost(`/api/v1/chunks/${knowledgeId}/${chunkId}/revert`, {
    revision,
    expected_revision: expectedRevision,
  });
}

export function updateKnowledgeMetadata(knowledgeId: string, customMetadata: Record<string, unknown>) {
  return apiPut(`/api/v1/knowledge/${knowledgeId}`, { custom_metadata: customMetadata });
}

export function updateKnowledgeSummary(knowledgeId: string, description: string) {
  return apiPut(`/api/v1/knowledge/${knowledgeId}`, { description });
}

export function regenerateKnowledgeSummary(knowledgeId: string) {
  return apiPost(`/api/v1/knowledge/${knowledgeId}/regenerate-summary`, {});
}

export function getChunkByIdOnly(chunkId: string, opts?: { includeImageText?: boolean }) {
  const extra = opts?.includeImageText ? "?include_image_text=true" : "";
  return apiGet(`/api/v1/chunks/by-id/${chunkId}${extra}`);
}

export function deleteGeneratedQuestion(chunkId: string, questionId: string) {
  return apiDel(`/api/v1/chunks/by-id/${chunkId}/questions`, { question_id: questionId });
}

export function upsertGeneratedQuestion(chunkId: string, question: string, questionId?: string) {
  return apiPut(`/api/v1/chunks/by-id/${chunkId}/questions`, {
    question_id: questionId || "",
    question,
  });
}

export function regenerateGeneratedQuestions(chunkId: string) {
  return apiPost(`/api/v1/chunks/by-id/${chunkId}/questions/regenerate`, {});
}

// ---- tags ---------------------------------------------------------------------

const buildQuery = (params?: Record<string, unknown>) => {
  if (!params) return "";
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === "") continue;
    query.append(key, String(value));
  }
  const qs = query.toString();
  return qs ? `?${qs}` : "";
};

export function listKnowledgeTags(
  kbId: string,
  params?: { page?: number; page_size?: number; keyword?: string },
) {
  return apiGet(`/api/v1/knowledge-bases/${kbId}/tags${buildQuery(params)}`);
}

export function createKnowledgeBaseTag(
  kbId: string,
  data: { name: string; color?: string; sort_order?: number },
) {
  return apiPost(`/api/v1/knowledge-bases/${kbId}/tags`, data);
}

export function updateKnowledgeBaseTag(
  kbId: string,
  tagId: string,
  data: { name?: string; color?: string; sort_order?: number },
) {
  return apiPut(`/api/v1/knowledge-bases/${kbId}/tags/${tagId}`, data);
}

export function deleteKnowledgeBaseTag(kbId: string, tagSeqId: number, params?: { force?: boolean }) {
  const forceQuery = params?.force ? "?force=true" : "";
  return apiDel(`/api/v1/knowledge-bases/${kbId}/tags/${tagSeqId}${forceQuery}`);
}

export function updateKnowledgeTagBatch(data: { updates: Record<string, string[]> }) {
  return apiPut(`/api/v1/knowledge/tags`, data);
}

export function updateFAQEntryTagBatch(
  kbId: string,
  data: { updates: Record<number, number | null> },
) {
  return apiPut(`/api/v1/knowledge-bases/${kbId}/faq/entries/tags`, data);
}

// ---- FAQ ----------------------------------------------------------------------

export function listFAQEntries(
  kbId: string,
  params?: {
    page?: number;
    page_size?: number;
    tag_id?: number;
    tag_ids?: string;
    keyword?: string;
    is_enabled?: boolean;
  },
) {
  return apiGet(`/api/v1/knowledge-bases/${kbId}/faq/entries${buildQuery(params)}`);
}

export function upsertFAQEntries(
  kbId: string,
  data: { entries: unknown[]; mode: "append" | "replace" },
) {
  return apiPost(`/api/v1/knowledge-bases/${kbId}/faq/entries`, data);
}

export function createFAQEntry(kbId: string, data: unknown) {
  return apiPost(`/api/v1/knowledge-bases/${kbId}/faq/entry`, data);
}

export function updateFAQEntry(kbId: string, entryId: number, data: unknown) {
  return apiPut(`/api/v1/knowledge-bases/${kbId}/faq/entries/${entryId}`, data);
}

/* Unified batch update — is_enabled / is_recommended / tag_id.
 * by_id: per-entry update; by_tag: same update applied to every entry
 * under a tag (minus exclude_ids). */
export interface FAQEntryFieldsUpdate {
  is_enabled?: boolean;
  is_recommended?: boolean;
  tag_id?: number | null;
}

export interface FAQEntryFieldsBatchRequest {
  by_id?: Record<number, FAQEntryFieldsUpdate>;
  by_tag?: Record<number, FAQEntryFieldsUpdate>;
  exclude_ids?: number[];
}

export function updateFAQEntryFieldsBatch(kbId: string, data: FAQEntryFieldsBatchRequest) {
  return apiPut(`/api/v1/knowledge-bases/${kbId}/faq/entries/fields`, data);
}

export function deleteFAQEntries(kbId: string, ids: number[]) {
  return apiDel(`/api/v1/knowledge-bases/${kbId}/faq/entries`, { ids });
}

export function searchFAQEntries(
  kbId: string,
  data: { query_text: string; vector_threshold?: number; match_count?: number },
) {
  return apiPost(`/api/v1/knowledge-bases/${kbId}/faq/search`, data);
}

/** Export FAQ entries as CSV or JSON file. */
export function exportFAQEntries(kbId: string, format: "csv" | "json" = "csv"): Promise<Blob> {
  const suffix = format === "json" ? "?format=json" : "";
  return apiDownload(`/api/v1/knowledge-bases/${kbId}/faq/entries/export${suffix}`);
}

export interface FAQBlockedEntry {
  index: number;
  standard_question: string;
  reason: string;
}

export interface FAQSuccessEntry {
  index: number;
  seq_id: number;
  tag_id?: number;
  tag_name?: string;
  standard_question: string;
}

export interface FAQImportProgress {
  task_id: string;
  kb_id: string;
  knowledge_id: string;
  status: "pending" | "processing" | "completed" | "failed";
  progress: number;
  total: number;
  processed: number;
  blocked: number;
  blocked_entries?: FAQBlockedEntry[];
  success_entries?: FAQSuccessEntry[];
  message: string;
  error: string;
  created_at: number;
  updated_at: number;
}

export function getFAQImportProgress(taskId: string) {
  return apiGet(`/api/v1/faq/import/progress/${taskId}`);
}

export function updateFAQImportResultDisplayStatus(
  knowledgeBaseId: string,
  displayStatus: "open" | "close",
) {
  return apiPut(`/api/v1/knowledge-bases/${knowledgeBaseId}/faq/import/last-result/display`, {
    display_status: displayStatus,
  });
}

// ---- search / retrieval ---------------------------------------------------------

export function searchKnowledge(
  keyword: string,
  offset = 0,
  limit = 20,
  fileTypes?: string[],
  options?: { agent_id?: string; agent_source_tenant_id?: string; recent?: boolean },
) {
  const query = new URLSearchParams();
  if (keyword) query.set("keyword", keyword);
  query.set("offset", String(offset));
  query.set("limit", String(limit));
  if (fileTypes?.length) query.set("file_types", fileTypes.join(","));
  if (options?.agent_id) query.set("agent_id", options.agent_id);
  if (options?.agent_source_tenant_id)
    query.set("agent_source_tenant_id", options.agent_source_tenant_id);
  if (options?.recent) query.set("recent", "true");
  return apiGet(`/api/v1/knowledge/search?${query.toString()}`);
}

export function batchReparseKnowledge(
  kbId: string,
  ids: string[],
  processConfig?: KnowledgeProcessOverrides,
) {
  return apiPost(`/api/v1/knowledge/batch-reparse`, {
    kb_id: kbId,
    ids,
    process_config: processConfig,
  });
}

/* Global retrieval/search config for a tenant, shared by knowledge search
 * and message search — ported from frontend/src/api/retrieval.ts. */
export interface RetrievalConfig {
  embedding_top_k: number;
  vector_threshold: number;
  keyword_threshold: number;
  rerank_top_k: number;
  rerank_threshold: number;
  rerank_model_id: string;
}

export function getTenantRetrievalConfig() {
  return apiGet("/api/v1/tenants/kv/retrieval-config");
}

export function updateTenantRetrievalConfig(config: RetrievalConfig) {
  return apiPut("/api/v1/tenants/kv/retrieval-config", config);
}

/* Chunker debug / preview — runs the adaptive chunker over sample text
 * without touching DB or embeddings (frontend/src/api/chunker/index.ts,
 * types mirror internal/handler/chunker_debug.go). */
export type StrategyTier = "heading" | "heuristic" | "recursive" | "legacy";

export interface TierRejection {
  tier: StrategyTier;
  reason: string;
}

export interface DocProfile {
  total_chars: number;
  total_lines: number;
  avg_line_len: number;
  std_line_len: number;
  md_heading_counts: Record<string, number>;
  md_heading_total: number;
  numbered_section_count: number;
  all_caps_short_line_count: number;
  blank_paragraph_breaks: number;
  form_feed_count: number;
  visual_sep_count: number;
  german_chapter_count: number;
  english_chapter_count: number;
  chinese_chapter_count: number;
  repeated_footer_count: number;
  has_tables: boolean;
  has_code: boolean;
  code_ratio: number;
  detected_langs: string[];
}

export interface PreviewChunk {
  seq: number;
  start: number;
  end: number;
  size_chars: number;
  size_tokens_approx: number;
  context_header?: string;
  content: string;
}

export interface PreviewChunkingStats {
  count: number;
  avg_chars: number;
  min_chars: number;
  max_chars: number;
  stddev_chars: number;
  truncated_to?: number;
}

export interface PreviewChunkingResponse {
  selected_tier: StrategyTier;
  tier_chain: StrategyTier[];
  rejected: TierRejection[];
  profile: DocProfile;
  chunks: PreviewChunk[];
  stats: PreviewChunkingStats;
}

export interface PreviewChunkingRequest {
  text: string;
  chunking_config: {
    chunk_size: number;
    chunk_overlap: number;
    separators: string[];
    enable_parent_child?: boolean;
    parent_chunk_size?: number;
    child_chunk_size?: number;
    strategy?: string;
    token_limit?: number;
    languages?: string[];
  };
}

export function previewChunking(
  body: PreviewChunkingRequest,
): Promise<{ success: boolean; data: PreviewChunkingResponse }> {
  return apiPost("/api/v1/chunker/preview", body);
}
