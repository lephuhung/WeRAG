/* Ported from frontend/src/api/system/index.ts.
 * Note the old axios layer unwrapped response.data AND attached
 * $httpStatus — request() in api-client.ts reproduces both.
 */
import { apiDel, apiGet, apiPatch, apiPost, apiPut, apiUpload, type ApiError } from "@/lib/api-client";
import type {
  CreatedTenantAPIKey,
  TenantAPIKey,
  TenantAPIKeyCapability,
  TenantRole,
} from "./tenants";
import {
  auditLogQueryString,
  type AuditLog,
  type AuditAction,
  type AuditOutcome,
  type ListAuditLogParams,
  type ListAuditLogResponse,
} from "./audit";

export type { AuditLog, AuditAction, AuditOutcome, ListAuditLogParams, ListAuditLogResponse };

// ---- platform API keys ---------------------------------------------------------

export interface CreatePlatformAPIKeyPayload {
  name: string;
  capabilities: TenantAPIKeyCapability[];
  expires_at_unix?: number;
}

export function listPlatformAPIKeys(): Promise<{ success: boolean; data?: TenantAPIKey[] }> {
  return apiGet("/api/v1/system/admin/api-keys");
}

export function createPlatformAPIKey(
  payload: CreatePlatformAPIKeyPayload,
): Promise<{ success: boolean; data?: CreatedTenantAPIKey }> {
  return apiPost("/api/v1/system/admin/api-keys", payload);
}

export function deletePlatformAPIKey(keyId: number): Promise<{ success: boolean }> {
  return apiDel(`/api/v1/system/admin/api-keys/${keyId}`);
}

// ---- system info / capabilities --------------------------------------------------

export interface SystemInfo {
  version: string;
  edition?: string;
  commit_id?: string;
  build_time?: string;
  go_version?: string;
  keyword_index_engine?: string;
  vector_store_engine?: string;
  graph_database_engine?: string;
  minio_enabled?: boolean;
  db_version?: string;
  /** Human-readable error when startup migration failed — surface a
   * troubleshooting banner (docs/migration-troubleshooting.md). */
  db_migration_error?: string;
  /** Server process boot time (RFC3339, UTC). */
  started_at?: string;
  /** Seconds since process start. */
  uptime_seconds?: number;
}

export interface DeploymentCapability {
  supported: boolean;
  reason?: string;
}

export interface DeploymentCapabilitiesResponse {
  edition: string;
  capabilities: Record<string, DeploymentCapability>;
}

export function getDeploymentCapabilities(): Promise<{ data: DeploymentCapabilitiesResponse }> {
  return apiGet("/api/v1/system/capabilities");
}

export interface PlaceholderDefinition {
  name: string;
  label: string;
  description: string;
}

export interface PromptTemplate {
  id: string;
  name: string;
  description: string;
  content: string;
  user?: string;
  has_knowledge_base?: boolean;
  has_web_search?: boolean;
  default?: boolean;
  mode?: string;
}

export interface PromptTemplatesConfig {
  system_prompt: PromptTemplate[];
  context_template: PromptTemplate[];
  // Rewrite templates — each template carries both content (system) + user fields
  rewrite: PromptTemplate[];
  // Fallback templates — fixed responses + model fallback prompts (mode: "model")
  fallback: PromptTemplate[];
  generate_session_title?: PromptTemplate[];
  generate_summary?: PromptTemplate[];
  keywords_extraction?: PromptTemplate[];
  chat_summary?: PromptTemplate[];
  agent_system_prompt?: PromptTemplate[];
  intent_prompts?: PromptTemplate[];
}

export function getSystemInfo(): Promise<{ data: SystemInfo }> {
  return apiGet("/api/v1/system/info");
}

export function getPromptTemplates(): Promise<{ data: PromptTemplatesConfig }> {
  return apiGet("/api/v1/tenants/kv/prompt-templates");
}

// ---- parser engines ---------------------------------------------------------------

export interface ParserEngineInfo {
  Name: string;
  Description: string;
  FileTypes: string[];
  Available?: boolean;
  UnavailableReason?: string;
}

export type MinerUParseMethod = "auto" | "ocr" | "txt";

/* Parser engine config — connection params live on the workspace; chat
 * attachment parse strategy is configured on the agent. */
export interface ParserEngineConfig {
  docreader_addr?: string;
  docreader_transport?: string;
  mineru_endpoint?: string;
  mineru_api_key?: string;
  // MinerU self-hosted
  mineru_model?: string;
  mineru_vlm_server_url?: string;
  mineru_enable_formula?: boolean | null;
  mineru_enable_table?: boolean | null;
  mineru_parse_method?: MinerUParseMethod;
  mineru_enable_ocr?: boolean | null;
  mineru_language?: string;
  // MinerU cloud API
  mineru_cloud_model?: string;
  mineru_cloud_enable_formula?: boolean | null;
  mineru_cloud_enable_table?: boolean | null;
  mineru_cloud_enable_ocr?: boolean | null;
  mineru_cloud_language?: string;
  // PaddleOCR-VL self-hosted
  paddleocr_vl_endpoint?: string;
  paddleocr_vl_use_seal_recognition?: boolean | null;
  paddleocr_vl_use_chart_recognition?: boolean | null;
  // PaddleOCR-VL cloud API
  paddleocr_vl_cloud_token?: string;
  paddleocr_vl_cloud_model?: string;
  paddleocr_vl_cloud_use_seal_recognition?: boolean | null;
  paddleocr_vl_cloud_use_chart_recognition?: boolean | null;
}

export interface ParserEnginesResponse {
  data: ParserEngineInfo[];
  docreader_addr?: string;
  /** Transport: grpc | http, decided by server env/config. */
  docreader_transport?: string;
  connected?: boolean;
}

export function getParserEngines(): Promise<ParserEnginesResponse> {
  return apiGet("/api/v1/system/parser-engines");
}

/** Probe availability with the currently typed params (not saved). */
export function checkParserEngines(config: ParserEngineConfig): Promise<ParserEnginesResponse> {
  return apiPost("/api/v1/system/parser-engines/check", config);
}

export function getParserEngineConfig(): Promise<{ data: ParserEngineConfig }> {
  return apiGet("/api/v1/tenants/kv/parser-engine-config");
}

export function updateParserEngineConfig(
  config: ParserEngineConfig,
): Promise<{ data: ParserEngineConfig }> {
  return apiPut("/api/v1/tenants/kv/parser-engine-config", config);
}

export function reconnectDocReader(addr: string): Promise<ParserEnginesResponse & { msg?: string }> {
  return apiPost("/api/v1/system/docreader/reconnect", { addr });
}

// ---- storage engines (workspace-scoped) --------------------------------------------

export interface StorageEngineConfig {
  default_provider: string; // "local" | "minio" | "cos" | "tos" | "s3" | "oss" | "ks3" | "obs"
  local: { path_prefix: string };
  minio: {
    mode: string;
    endpoint: string;
    access_key_id: string;
    secret_access_key: string;
    bucket_name: string;
    use_ssl: boolean;
    path_prefix: string;
  };
  cos: {
    secret_id: string;
    secret_key: string;
    region: string;
    bucket_name: string;
    app_id: string;
    path_prefix: string;
  };
  tos: {
    endpoint: string;
    region: string;
    access_key: string;
    secret_key: string;
    bucket_name: string;
    path_prefix: string;
  };
  s3: {
    endpoint: string; // optional for standard AWS S3
    region: string;
    access_key: string; // both keys empty => AWS default credential chain
    secret_key: string;
    bucket_name: string;
    path_prefix: string;
  };
  oss: {
    endpoint: string;
    region: string;
    access_key: string;
    secret_key: string;
    bucket_name: string;
    path_prefix: string;
    use_temp_bucket: boolean;
    temp_bucket_name: string;
    temp_region: string;
  };
  ks3: {
    endpoint: string;
    region: string;
    access_key: string;
    secret_key: string;
    bucket_name: string;
    path_prefix: string;
  };
  obs: {
    endpoint: string;
    region: string;
    access_key: string;
    secret_key: string;
    bucket_name: string;
    path_prefix: string;
  };
}

export interface StorageEngineStatusItem {
  name: string;
  allowed?: boolean;
  available: boolean;
  description: string;
}

export interface GetStorageEngineStatusResponse {
  engines: StorageEngineStatusItem[];
  allowed_providers?: string[];
  minio_env_available: boolean;
}

export function getStorageEngineConfig(): Promise<{ data: StorageEngineConfig }> {
  return apiGet("/api/v1/tenants/kv/storage-engine-config");
}

export function updateStorageEngineConfig(
  config: StorageEngineConfig,
): Promise<{ data: StorageEngineConfig }> {
  return apiPut("/api/v1/tenants/kv/storage-engine-config", config);
}

export function getStorageEngineStatus(): Promise<{ data: GetStorageEngineStatusResponse }> {
  return apiGet("/api/v1/system/storage-engine-status");
}

export interface StorageCheckRequest {
  provider: string;
  minio?: StorageEngineConfig["minio"];
  cos?: StorageEngineConfig["cos"];
  tos?: StorageEngineConfig["tos"];
  s3?: StorageEngineConfig["s3"];
  oss?: StorageEngineConfig["oss"];
  ks3?: StorageEngineConfig["ks3"];
  obs?: StorageEngineConfig["obs"];
}

export interface StorageCheckResponse {
  ok: boolean;
  message: string;
  bucket_created?: boolean;
}

export function checkStorageEngine(
  req: StorageCheckRequest,
): Promise<{ data: StorageCheckResponse }> {
  return apiPost("/api/v1/system/storage-engine-check", req);
}

// ---- system admin management ---------------------------------------------------------

export interface SystemUserMembership {
  tenant_id: number;
  tenant_name: string;
  role: TenantRole;
}

/** Organization membership of one of the user's workspaces. Org roles are
 * tenant-keyed (admin/editor/viewer) — a user reaches an org through their
 * workspace, so the row records which tenant carries which role. */
export interface SystemUserOrgMembership {
  org_id: string;
  org_name: string;
  tenant_id: number;
  tenant_name: string;
  role: OrgMemberRole;
}

export interface SystemAdminUser {
  id: string;
  username: string;
  email: string;
  avatar?: string;
  is_active: boolean;
  is_system_admin: boolean;
  memberships?: SystemUserMembership[];
  org_memberships?: SystemUserOrgMembership[];
  created_at: string;
  updated_at: string;
}

export interface ListSystemAdminsResponse {
  total: number;
  admins: SystemAdminUser[];
}

export interface PromoteUserToSystemAdminRequest {
  /** UUID of the user to promote. Optional; supply this OR `email`. */
  user_id?: string;
  /** Email of the user to promote. Optional; supply this OR `user_id`. */
  email?: string;
}

/* Identify the target by user_id (API clients) or email (the UI path);
 * user_id wins when both are set. The handler returns the updated UserInfo
 * directly as the body — no {data: ...} wrapping. */
export function promoteUserToSystemAdmin(
  req: PromoteUserToSystemAdminRequest,
): Promise<SystemAdminUser> {
  return apiPost("/api/v1/system/admin/promote", req);
}

export function revokeSystemAdmin(userId: string): Promise<SystemAdminUser> {
  return apiPost("/api/v1/system/admin/revoke", { user_id: userId });
}

/** Returns {total, admins[]} directly — no {data: ...} wrapping. */
export function listSystemAdmins(params?: {
  offset?: number;
  limit?: number;
}): Promise<ListSystemAdminsResponse> {
  const qs = new URLSearchParams();
  if (params?.offset != null) qs.set("offset", String(params.offset));
  if (params?.limit != null) qs.set("limit", String(params.limit));
  const suffix = qs.toString() ? `?${qs.toString()}` : "";
  return apiGet(`/api/v1/system/admin/list${suffix}`);
}

export interface ListSystemUsersResponse {
  total: number;
  users: SystemAdminUser[];
}

/* GET /api/v1/system/admin/users — every account (admins + regular
 * users), not just the admin subset. `q` filters username/email
 * server-side. Returns {total, users[]} directly. SystemAdmin only. */
export function listSystemUsers(params?: {
  offset?: number;
  limit?: number;
  q?: string;
}): Promise<ListSystemUsersResponse> {
  const qs = new URLSearchParams();
  if (params?.offset != null) qs.set("offset", String(params.offset));
  if (params?.limit != null) qs.set("limit", String(params.limit));
  if (params?.q) qs.set("q", params.q);
  const suffix = qs.toString() ? `?${qs.toString()}` : "";
  return apiGet(`/api/v1/system/admin/users${suffix}`);
}

/* PUT /api/v1/system/admin/tenants/:tenant_id/members/:user_id — change a
 * user's role inside any workspace (SystemAdmin only; does not require the
 * caller to be Owner of that tenant). The backend still rejects demoting
 * the last owner. */
export function updateSystemUserRole(
  tenantId: number,
  userId: string,
  role: TenantRole,
): Promise<{ success: boolean }> {
  return apiPut(`/api/v1/system/admin/tenants/${tenantId}/members/${userId}`, { role });
}

export type OrgMemberRole = "admin" | "editor" | "viewer";

/* PUT /api/v1/system/admin/organizations/:org_id/members/:tenant_id —
 * change a workspace's role inside an organization (SystemAdmin only).
 * Org membership is tenant-keyed, so this applies to every user of that
 * workspace; the org's owner tenant cannot have its role changed. */
export function updateSystemOrgTenantRole(
  orgId: string,
  tenantId: number,
  role: OrgMemberRole,
): Promise<{ success: boolean }> {
  return apiPut(`/api/v1/system/admin/organizations/${orgId}/members/${tenantId}`, { role });
}

export interface ResetUserPasswordRequest {
  email: string;
  new_password: string;
}

/* Replace another user's password and revoke all their active sessions.
 * SystemAdmin only; the backend rejects resetting the caller's own password. */
export function resetUserPassword(req: ResetUserPasswordRequest): Promise<{ message: string }> {
  return apiPost("/api/v1/system/admin/users/reset-password", req);
}

export interface CreateSystemUserRequest {
  /** 2-50 characters. */
  username: string;
  email: string;
  /** Omit (or null) to have the server mint a random password, returned
   * exactly once in `generated_password`. A provided value — including ""
   * — is subject to password policy. */
  password?: string;
}

export interface CreateSystemUserResponse {
  user: SystemAdminUser;
  /** Present only when `password` was omitted: the server-minted plaintext,
   * returned exactly once. */
  generated_password?: string;
}

export interface CreateSystemUserResult extends CreateSystemUserResponse {
  /** True on HTTP 201 (created); false on the idempotent 200 retry. */
  created: boolean;
}

export async function createSystemUser(
  req: CreateSystemUserRequest,
): Promise<CreateSystemUserResult> {
  const response = await apiPost<CreateSystemUserResponse>(
    "/api/v1/system/admin/users/create",
    req,
  );
  return { ...response, created: (response as { $httpStatus?: number }).$httpStatus === 201 };
}

// ---- system settings (P1) -------------------------------------------------------------

/* Mirrors backend types.SystemSetting. `value` is unknown because the JSONB
 * column can hold int / string / bool depending on `value_type` — narrow
 * via that field. New fields must also land in types/system_setting.go. */
export interface SystemSettingItem {
  id: number;
  key: string;
  /** Raw JSON value — narrow via value_type before rendering. */
  value: unknown;
  value_type: "int" | "string" | "bool" | "string_list";
  category: string;
  description: string;
  /** P3+ — currently always false. May surface a "redacted" state when true. */
  is_secret: boolean;
  /** P3+ — currently always false. May show "needs restart" badge when true. */
  requires_restart: boolean;
  last_modified_by: string;
  /* Display label resolved from last_modified_by (UUID) server-side —
   * username when known, email as fallback. Empty for virtual rows never
   * persisted; the UI then falls back to the UUID prefix. */
  last_modified_by_name?: string;
  created_at: string;
  updated_at: string;
  /** Constrained values for `value`, populated from the in-code registry;
   * absent/empty means free-form (render an input, not a select). */
  enum?: string[];
}

export function listSystemSettings(): Promise<SystemSettingItem[]> {
  return apiGet("/api/v1/system/admin/settings");
}

export function getSystemSetting(key: string): Promise<SystemSettingItem> {
  return apiGet(`/api/v1/system/admin/settings/${encodeURIComponent(key)}`);
}

/* The backend validates value against the registry-declared value_type and
 * rejects mismatches with 400. Successful updates emit an audit row
 * (system.setting_changed) carrying old/new values. */
export function updateSystemSetting(key: string, value: unknown): Promise<SystemSettingItem> {
  return apiPut(`/api/v1/system/admin/settings/${encodeURIComponent(key)}`, { value });
}

/** Reset to ENV/built-in default by deleting the DB override. Idempotent. */
export function resetSystemSetting(key: string): Promise<void> {
  return apiDel(`/api/v1/system/admin/settings/${encodeURIComponent(key)}`);
}

export interface ApplyDefaultStorageQuotaResult {
  affected: number;
  quota_bytes: number;
  quota_gb: number;
}

/* Apply the resolved `tenant.default_storage_quota_gb` (DB > ENV > default)
 * to every existing tenant row. SystemAdmin only. Idempotent. */
export function applyDefaultStorageQuotaToAllTenants(): Promise<ApplyDefaultStorageQuotaResult> {
  return apiPost("/api/v1/system/admin/tenants/apply-default-storage-quota", {});
}

// ---- platform audit log (system-scope, tenant_id=0) ----------------------------------

/* GET /api/v1/system/admin/audit-log (SystemAdmin only). Covers
 * system.setting_changed / system.admin_promoted / system.admin_revoked etc.
 * Cursor-paginated by descending id — pass after_id=next_cursor until 0. */
export function listSystemAuditLog(
  params: ListAuditLogParams = {},
): Promise<ListAuditLogResponse> {
  const tail = auditLogQueryString(params);
  return apiGet(`/api/v1/system/admin/audit-log${tail ? "?" + tail : ""}`);
}

// ---- runtime queue observability (system-scope) ---------------------------------------

/* QueueStat mirrors types.QueueStat: a read-only depth snapshot of one asynq
 * queue. `active` = tasks being processed, `pending` = backlog. */
export interface QueueStat {
  name: string;
  pool: string;
  weight: number;
  size: number;
  pending: number;
  active: number;
  scheduled: number;
  retry: number;
  archived: number;
  completed: number;
  processed: number;
  failed: number;
  paused: boolean;
  latency_ms: number;
  memory_usage_bytes: number;
}

export interface RuntimeWorkerPool {
  name: string;
  concurrency: number;
  queue_count: number;
  instances: number;
  cluster_capacity: number;
  active: number;
  utilization: number;
}

export interface ModelRuntimeStat {
  model_id: string;
  name: string;
  active: number;
  waiting: number;
  limit: number;
}

/* `available` is false in Lite mode (no Redis/asynq) — render an
 * "unavailable in this deployment" state rather than an empty table. */
export interface RuntimeQueuesResponse {
  available: boolean;
  upstream_concurrency: number;
  parse_concurrency: number;
  wiki_concurrency: number;
  pools: RuntimeWorkerPool[];
  queues: QueueStat[];
  model_limiter_available: boolean;
  models: ModelRuntimeStat[];
  timestamp: number;
}

export type RuntimeTaskState =
  | "pending"
  | "active"
  | "scheduled"
  | "retry"
  | "archived"
  | "completed";
export type RuntimeTaskAction = "cancel" | "run_now" | "delete";

export interface RuntimeTask {
  id: string;
  queue: string;
  type: string;
  state: RuntimeTaskState;
  allowed_actions: RuntimeTaskAction[];
  last_error?: string;
  last_failed_at?: string;
  next_process_at?: string;
  started_at?: string;
  completed_at?: string;
  deadline?: string;
  enqueued_at?: string;
  retried: number;
  max_retry: number;
  is_orphaned?: boolean;
  worker?: string;
  tenant_id?: number;
  knowledge_base_id?: string;
  knowledge_id?: string;
  task_id?: string;
  source_id?: string;
  target_id?: string;
  source_kb_id?: string;
  target_kb_id?: string;
  data_source_id?: string;
  sync_log_id?: string;
  knowledge_count?: number;
}

export interface RuntimeTasksResponse {
  available: boolean;
  tasks: RuntimeTask[];
  page_size: number;
  has_more: boolean;
  next_cursor?: string;
}

/** GET /api/v1/system/admin/runtime/queues (SystemAdmin only). */
export function getRuntimeQueues(): Promise<RuntimeQueuesResponse> {
  return apiGet("/api/v1/system/admin/runtime/queues");
}

export function getRuntimeTasks(
  queue: string,
  state: RuntimeTaskState,
  cursor = "",
  pageSize = 20,
): Promise<RuntimeTasksResponse> {
  const qs = new URLSearchParams({ state, page_size: String(pageSize) });
  if (cursor) qs.set("cursor", cursor);
  return apiGet(`/api/v1/system/admin/runtime/queues/${encodeURIComponent(queue)}/tasks?${qs}`);
}

export async function mutateRuntimeTask(
  queue: string,
  taskID: string,
  action: RuntimeTaskAction,
): Promise<void> {
  await apiPost(
    `/api/v1/system/admin/runtime/queues/${encodeURIComponent(queue)}/tasks/${encodeURIComponent(taskID)}/actions/${encodeURIComponent(action)}`,
    {},
  );
}

/* Clear every archived (finally-failed) task in one queue. Only touches the
 * dead-letter set — live tasks are never affected.
 * DELETE /api/v1/system/admin/runtime/queues/{queue}/archived */
export function purgeArchivedRuntimeTasks(
  queue: string,
): Promise<{ success: boolean; deleted: number }> {
  return apiDel(`/api/v1/system/admin/runtime/queues/${encodeURIComponent(queue)}/archived`);
}

// ---- sandbox backend configuration (per workspace) -------------------------------------

export interface SandboxVolumeMountConfig {
  enabled: boolean;
  mount_path?: string;
  provider?: string;
  volume_id?: string;
  volume_name?: string;
  volume_owner_fingerprint?: string;
}

export interface SandboxCubeConfig {
  api_url?: string;
  proxy_url?: string;
  sandbox_domain?: string;
  api_key?: string;
  template_id?: string;
  http_timeout_sec?: number;
  cube_sandbox_ttl_seconds?: number;
  dns_servers?: string[];
}

export interface SandboxE2BConfig {
  api_url?: string;
  proxy_url?: string;
  sandbox_domain?: string;
  api_key?: string;
  template_id?: string;
  http_timeout_sec?: number;
  e2b_sandbox_ttl_seconds?: number;
}

export interface SandboxSkillImage {
  snapshot_id?: string;
  generation?: number;
  built_at?: string;
  base_template_id?: string;
  owner_fingerprint?: string;
}

export interface SandboxConfig {
  sandbox_type?: string;
  default_timeout_sec?: number;
  terminal_idle_disconnect_sec?: number;
  desktop_enabled?: boolean;
  allow_private_endpoints?: boolean;
  env_vars?: Record<string, string>;
  volume_mount?: SandboxVolumeMountConfig;
  skill_image?: SandboxSkillImage;
  skill_rollout?: "next_turn" | "new_session";
  network?: SandboxNetworkPolicy;
  cube?: SandboxCubeConfig;
  e2b?: SandboxE2BConfig;
  docker?: SandboxDockerConfig;
}

/** Docker backend: one daemon, one long-lived container per session. */
export interface SandboxDockerConfig {
  image?: string;
  host?: string;
  tls_cert_path?: string;
  cpu_limit?: number;
  memory_limit_mb?: number;
  pids_limit?: number;
  network_mode?: string;
  runtime?: string;
  idle_ttl_seconds?: number;
  http_timeout_sec?: number;
}

/** One injected credential header on a Cube L7 rule. */
export interface SandboxCubeHeaderInject {
  header: string;
  /** Masked as '***' in responses; send the placeholder back to keep it. */
  secret?: string;
  /** Defaults to '${SECRET}' server-side. */
  format?: string;
}

/** One CubeEgress L7 rule. Match fields are AND-ed; methods are OR-ed. */
export interface SandboxCubeEgressRule {
  name: string;
  scheme?: string;
  sni?: string;
  host?: string;
  methods?: string[];
  path?: string;
  /** Absent means allow. A deny rule still needs host or sni. */
  deny?: boolean;
  audit?: string;
  inject?: SandboxCubeHeaderInject[];
}

/** One E2B per-host request transform. host must also be in allow_out. */
export interface SandboxE2BHostRule {
  host: string;
  /** Values are masked as '***' in responses. */
  headers?: Record<string, string>;
}

/* Network policy for every sandbox created from this config. Absent fields
 * mean egress allowed. Inbound is always credential-required:
 * allow_public_inbound is accepted then ignored/cleared. */
export interface SandboxNetworkPolicy {
  deny_egress_by_default?: boolean;
  /** Ignored. Inbound is always credential-required. */
  allow_public_inbound?: boolean;
  allow_out?: string[];
  deny_out?: string[];
  cube_rules?: SandboxCubeEgressRule[];
  e2b_host_rules?: SandboxE2BHostRule[];
}

/** `ok: null` means the probe was not executed in this run. */
export interface SandboxCheckItem {
  name: string;
  ok: boolean | null;
  message?: string;
  /** Stable code for why a probe was skipped; localized by the caller. */
  reason?: string;
  latency_ms?: number;
}

export interface SandboxCheckResult {
  ok: boolean;
  provider: string;
  checks: SandboxCheckItem[];
  capabilities?: Record<string, boolean>;
}

export interface SandboxTemplate {
  id: string;
  name: string;
  status?: string;
  version?: string;
  image?: string;
  created_at?: string;
  updated_at?: string;
  standard: boolean;
  /** XFCE sibling of `standard`. The admin picks one ID as boot target. */
  desktop?: boolean;
  /** Provider's own explanation for a failed build, when reported. */
  error?: string;
  instance_type?: string;
  network_type?: string;
  allow_internet_access?: boolean;
}

export interface SandboxTemplateCatalog {
  templates: SandboxTemplate[];
  standard_template_id?: string;
  desktop_template_id?: string;
  provisioned: boolean;
}

/** One named sandbox backend config. Credentials arrive masked. */
export interface SandboxConfigRecord {
  id: string;
  name: string;
  description?: string;
  sandbox_type: string;
  config: SandboxConfig;
  created_at: string;
  updated_at: string;
}

export interface SandboxConfigUpsert {
  name: string;
  description?: string;
  config: SandboxConfig;
}

/* What a config currently holds. `sandbox_count` comes from the provider —
 * non-zero is authoritative: identity edits and deletion are refused until
 * it reaches zero. `unverifiable` means the count is UNKNOWN (provider
 * unreachable), not zero — never render as "0 sandboxes". */
export interface SandboxInventory {
  sandbox_count: number;
  session_ids?: string[];
  agent_names?: string[];
  unverifiable?: boolean;
}

/** Sandbox backends managed as named workspace configurations. */
export const NAMED_SANDBOX_BACKEND_TYPES = ["cube", "e2b", "docker"] as const;

export function isNamedSandboxBackend(type: string): boolean {
  return (NAMED_SANDBOX_BACKEND_TYPES as readonly string[]).includes(type);
}

export function listSandboxConfigs(): Promise<{
  data: SandboxConfigRecord[];
  workspace_scripts_disabled?: boolean;
}> {
  return apiGet("/api/v1/sandbox-configs");
}

export function setSandboxWorkspacePolicy(scriptsDisabled: boolean): Promise<{
  workspace_scripts_disabled: boolean;
}> {
  return apiPut("/api/v1/sandbox-configs/workspace-policy", {
    scripts_disabled: scriptsDisabled,
  });
}

export function createSandboxConfig(
  payload: SandboxConfigUpsert,
): Promise<{ data: SandboxConfigRecord }> {
  return apiPost("/api/v1/sandbox-configs", payload);
}

export function getSandboxConfigById(id: string): Promise<{ data: SandboxConfigRecord }> {
  return apiGet(`/api/v1/sandbox-configs/${id}`);
}

export function updateSandboxConfigById(
  id: string,
  payload: SandboxConfigUpsert,
): Promise<{ data: SandboxConfigRecord }> {
  return apiPut(`/api/v1/sandbox-configs/${id}`, payload);
}

/* `force` only overrides an inventory the backend could not verify; it never
 * overrides sandboxes the backend can actually see. Ask for it exclusively in
 * response to a `sandbox_inventory_unverifiable` conflict. */
export function deleteSandboxConfig(id: string, force = false): Promise<void> {
  const query = force ? "?force=true" : "";
  return apiDel(`/api/v1/sandbox-configs/${id}${query}`);
}

export function getSandboxConfigInventory(id: string): Promise<{ data: SandboxInventory }> {
  return apiGet(`/api/v1/sandbox-configs/${id}/sandboxes`);
}

/* Fetch templates using the connection currently entered in the drawer.
 * `ensure_standard`/`ensure_desktop` start provider-side CLI builds when the
 * template is missing (the settings UI only sends ensure_desktop on explicit
 * Create — listing must not provision as a side effect). `replace_standard` /
 * `replace_desktop` rebuild so a new spec takes effect; they need config_id.
 * Poll the returned building item through the same endpoint. */
export function querySandboxTemplates(payload: {
  config: SandboxConfig;
  config_id?: string;
  ensure_standard?: boolean;
  replace_standard?: boolean;
  ensure_desktop?: boolean;
  replace_desktop?: boolean;
}): Promise<{ data: SandboxTemplateCatalog }> {
  return apiPost("/api/v1/sandbox-configs/templates/query", payload);
}

/* Probe a sandbox config without saving. Redacted secrets resolve server-side —
 * pass `config_id` alongside an edited `config` to test unsaved changes.
 * Omit `config` to probe a stored config as-is. `deep` additionally creates
 * and destroys one sandbox (the only way to validate template ID, Cube proxy
 * data plane and egress) — it consumes real sandbox time. */
export function checkSandboxConfig(payload: {
  config?: SandboxConfig;
  config_id?: string;
  deep?: boolean;
}): Promise<{ data: SandboxCheckResult }> {
  return apiPost("/api/v1/system/sandbox-check", payload);
}

/* The two/three refusals a save or delete can hit:
 * - `sandboxes_still_live`: backend counted live sandboxes — end owning
 *   sessions or create a second config.
 * - `sandbox_inventory_unverifiable`: backend unreachable, nothing could be
 *   counted — the one case a force delete may override. */
export type SandboxConflictCode =
  | "sandboxes_still_live"
  | "sandbox_inventory_unverifiable"
  | "skill_snapshot_blocks_template";

export interface SandboxConflict {
  code: SandboxConflictCode;
  message?: string;
  /** Present for `sandboxes_still_live`; nothing to report otherwise. */
  inventory?: SandboxInventory;
}

/* Reads a sandbox-config conflict out of a rejected request, or null when the
 * failure is anything else. The ApiError carries the body on `payload`, so the
 * code sits at err.payload.error.code. */
export function parseSandboxConflict(err: unknown): SandboxConflict | null {
  if (typeof err !== "object" || err === null) return null;
  const payload = (err as ApiError).payload;
  const detail =
    payload && typeof payload === "object"
      ? (payload as { error?: { code?: string; message?: string; data?: SandboxInventory } }).error
      : undefined;
  if (!detail || typeof detail !== "object") return null;
  if (
    detail.code !== "sandboxes_still_live" &&
    detail.code !== "sandbox_inventory_unverifiable" &&
    detail.code !== "skill_snapshot_blocks_template"
  ) {
    return null;
  }
  return { code: detail.code, message: detail.message, inventory: detail.data };
}

// ---- agent skills installed onto a sandbox config's image ----------------------------

export type ConfigSkillStatus = "installing" | "ready" | "failed" | "removing" | "removed";

/* One env var the skill's installer declared. `is_set` reports whether a
 * workspace-wide value exists; the value itself is never returned. */
export interface ConfigSkillEnv {
  name: string;
  description?: string;
  required?: boolean;
  is_set: boolean;
}

export interface ConfigSkill {
  id: string;
  name: string;
  version?: string;
  description?: string;
  enabled: boolean;
  status: ConfigSkillStatus | string;
  error?: string;
  bundle_sha256?: string;
  installed_snapshot_id?: string;
  // Locators for this skill's most recent install conversation. Absent for
  // skills installed before transcripts existed — that's how the drawer
  // decides whether to offer "view install".
  install_session_id?: string;
  install_message_id?: string;
  // Present while a newer install is in flight/failed and the sandbox still
  // runs the previous version.
  served?: { version?: string };
  created_at: string;
  updated_at: string;
  // Absent when the installer declared nothing — controls whether the env
  // var editor is offered at all.
  envs?: ConfigSkillEnv[];
}

export interface ConfigSkillInstallEvent {
  percent: number;
  stage: string;
  log?: string;
  status?: string;
  done: boolean;
}

export function listConfigSkills(configId: string): Promise<{ data: ConfigSkill[] }> {
  return apiGet(`/api/v1/sandbox-configs/${configId}/skills`);
}

export function uploadConfigSkill(
  configId: string,
  file: File,
  onProgress?: (percent: number) => void,
): Promise<{ data: { skill_id: string } }> {
  const form = new FormData();
  form.append("file", file);
  return apiUpload<{ data: { skill_id: string } }>(
    `/api/v1/sandbox-configs/${configId}/skills`,
    form,
    onProgress,
    { timeoutMs: 5 * 60 * 1000 },
  );
}

export function installConfigSkillFromSource(
  configId: string,
  payload: { source: string },
): Promise<{ data: { skill_id: string } }> {
  return apiPost(`/api/v1/sandbox-configs/${configId}/skills`, payload, {
    timeoutMs: 2 * 60 * 1000,
  });
}

/* Retries an install from the archive the server already stores — a failure
 * unrelated to the bundle shouldn't send the operator hunting for the
 * original zip or registry URL. */
export function reinstallConfigSkill(
  configId: string,
  skillId: string,
  instructions = "",
): Promise<{ data: { skill_id: string } }> {
  return apiPost(`/api/v1/sandbox-configs/${configId}/skills/${skillId}/reinstall`, {
    instructions,
  });
}

/* Aborts an in-flight install so retry/uninstall become available. After a
 * process restart the row may still say "installing" with nothing running. */
export function stopConfigSkill(
  configId: string,
  skillId: string,
): Promise<{ data: ConfigSkill }> {
  return apiPost(`/api/v1/sandbox-configs/${configId}/skills/${skillId}/stop`, {});
}

/* Partial update: absent fields are left alone. `envs` names only the
 * variables to write — empty string clears the stored value while keeping
 * the declaration; undeclared names are ignored server-side. */
export function patchConfigSkill(
  configId: string,
  skillId: string,
  payload: { enabled?: boolean; envs?: Record<string, string> },
): Promise<{ data: ConfigSkill }> {
  return apiPatch(`/api/v1/sandbox-configs/${configId}/skills/${skillId}`, payload);
}

export function deleteConfigSkill(
  configId: string,
  skillId: string,
): Promise<{ data: { skill_id: string } }> {
  return apiDel(`/api/v1/sandbox-configs/${configId}/skills/${skillId}`);
}

export function getConfigSkill(
  configId: string,
  skillId: string,
): Promise<{ data: ConfigSkill }> {
  return apiGet(`/api/v1/sandbox-configs/${configId}/skills/${skillId}`);
}

export function configSkillInstallEventsUrl(configId: string, skillId: string): string {
  return `/api/v1/sandbox-configs/${configId}/skills/${skillId}/install-events`;
}

/* The installer agent's own transcript — prompt, thinking, commands and
 * output, replayed then followed live. Answers 404 once the event log
 * expires: the signal to read durable message history instead. */
export function configSkillTranscriptUrl(configId: string, skillId: string): string {
  return `/api/v1/sandbox-configs/${configId}/skills/${skillId}/transcript`;
}

export interface ConfigSkillFileEntry {
  path: string;
  size: number;
}

export interface ConfigSkillFileContent {
  path: string;
  size: number;
  encoding: "utf-8" | "base64" | "binary" | string;
  content?: string;
  media_type?: string;
  truncated?: boolean;
  binary?: boolean;
}

export function listConfigSkillFiles(
  configId: string,
  skillId: string,
): Promise<{ data: ConfigSkillFileEntry[] }> {
  return apiGet(`/api/v1/sandbox-configs/${configId}/skills/${skillId}/files`);
}

export function getConfigSkillFile(
  configId: string,
  skillId: string,
  path: string,
): Promise<{ data: ConfigSkillFileContent }> {
  return apiGet(
    `/api/v1/sandbox-configs/${configId}/skills/${skillId}/files/content?path=${encodeURIComponent(path)}`,
  );
}

export interface SkillInstallGuidanceState {
  accepting: boolean;
  messages: Array<{ id: string; content: string; status: "pending" | "injected" | "unprocessed" }>;
}

export function getConfigSkillGuidance(
  configId: string,
  skillId: string,
): Promise<{ data: SkillInstallGuidanceState }> {
  return apiGet(`/api/v1/sandbox-configs/${configId}/skills/${skillId}/guidance`);
}
