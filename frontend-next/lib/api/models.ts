/* Ported from frontend/src/api/model/index.ts + model/modelUsage.ts.
 * i18n t() fallbacks replaced with plain strings. */
import { apiDel, apiGet, apiPost, apiPut, apiUpload, ApiError } from "@/lib/api-client";

// ---- model usage (modelUsage.ts) ------------------------------------------------

export const MODEL_IN_USE_ERROR_CODE = 2300;

export const KNOWN_MODEL_USAGE_BINDINGS = [
  "embedding_model",
  "summary_model",
  "image_processing_model",
  "vlm_model",
  "asr_model",
  "wiki_synthesis_model",
  "auto_tag_model",
  "chat_model",
  "rerank_model",
  "query_understand_model",
  "follow_up_model",
  "extract_model",
] as const;

export type KnownModelUsageBinding = (typeof KNOWN_MODEL_USAGE_BINDINGS)[number];

export interface ModelUsageResource {
  id: string;
  name: string;
  bindings: string[];
}

export interface ModelUsageDetails {
  knowledge_bases: ModelUsageResource[];
  agents: ModelUsageResource[];
  long_term_memory: { bindings: string[] };
  knowledge_base_total: number;
  agent_total: number;
}

export class ModelInUseError extends Error {
  readonly details: ModelUsageDetails;

  constructor(details: ModelUsageDetails) {
    super("model is in use");
    this.name = "ModelInUseError";
    this.details = details;
  }
}

type UnknownRecord = Record<string, unknown>;

function isRecord(value: unknown): value is UnknownRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function parseBindings(value: unknown): string[] | null {
  if (
    !Array.isArray(value) ||
    value.some((binding) => typeof binding !== "string" || binding.trim() === "")
  ) {
    return null;
  }
  return [...value];
}

function parseResources(value: unknown): ModelUsageResource[] | null {
  if (!Array.isArray(value)) return null;
  const resources: ModelUsageResource[] = [];
  for (const item of value) {
    if (
      !isRecord(item) ||
      typeof item.id !== "string" ||
      item.id.trim() === "" ||
      typeof item.name !== "string"
    ) {
      return null;
    }
    const bindings = parseBindings(item.bindings);
    if (!bindings || bindings.length === 0) return null;
    resources.push({ id: item.id, name: item.name, bindings });
  }
  return resources;
}

function parseTotal(value: unknown, listed: number): number {
  if (typeof value === "number" && Number.isFinite(value) && value >= listed) {
    return Math.floor(value);
  }
  return listed;
}

export function parseModelUsageDetails(value: unknown): ModelUsageDetails | null {
  if (!isRecord(value) || !isRecord(value.long_term_memory)) return null;
  const knowledgeBases = parseResources(value.knowledge_bases);
  const agents = parseResources(value.agents);
  const memoryBindings = parseBindings(value.long_term_memory.bindings);
  if (!knowledgeBases || !agents || !memoryBindings) return null;
  const details: ModelUsageDetails = {
    knowledge_bases: knowledgeBases,
    agents,
    long_term_memory: { bindings: memoryBindings },
    knowledge_base_total: parseTotal(value.knowledge_base_total, knowledgeBases.length),
    agent_total: parseTotal(value.agent_total, agents.length),
  };
  if (knowledgeBases.length === 0 && agents.length === 0 && memoryBindings.length === 0) {
    return null;
  }
  return details;
}

/* The old axios rejection spread the body onto the error (err.error.code);
 * api-client carries the body on ApiError.payload — check both shapes. */
export function modelInUseErrorFromRequest(error: unknown): ModelInUseError | null {
  const body =
    error instanceof ApiError && isRecord(error.payload)
      ? error.payload
      : isRecord(error)
        ? error
        : null;
  if (!body || !isRecord(body.error) || body.error.code !== MODEL_IN_USE_ERROR_CODE) {
    return null;
  }
  const details = parseModelUsageDetails(body.error.details);
  return details ? new ModelInUseError(details) : null;
}

const knownBindings = new Set<string>(KNOWN_MODEL_USAGE_BINDINGS);

export function modelUsageBindingI18nKey(binding: string): string {
  const key = knownBindings.has(binding) ? binding : "unknown";
  return `modelSettings.usage.bindings.${key}`;
}

export function modelUsageResourceCount(
  resources: readonly ModelUsageResource[],
  total?: number,
): number {
  return typeof total === "number" && Number.isFinite(total) && total >= resources.length
    ? Math.floor(total)
    : resources.length;
}

export function modelUsageListTruncated(
  resources: readonly ModelUsageResource[],
  total?: number,
): boolean {
  return modelUsageResourceCount(resources, total) > resources.length;
}

export type ModelUsageResourceKind = "knowledge_base" | "agent";
export type KnowledgeBaseModelUsageSection = "models" | "multimodal" | "asr";
export type AgentModelUsageSection = "model" | "multimodal" | "suggestions";

const knowledgeBaseUsageSections: Partial<
  Record<KnownModelUsageBinding, KnowledgeBaseModelUsageSection>
> = {
  embedding_model: "models",
  summary_model: "models",
  wiki_synthesis_model: "models",
  auto_tag_model: "models",
  image_processing_model: "multimodal",
  vlm_model: "multimodal",
  asr_model: "asr",
};

const agentUsageSections: Partial<Record<KnownModelUsageBinding, AgentModelUsageSection>> = {
  chat_model: "model",
  rerank_model: "model",
  query_understand_model: "model",
  vlm_model: "multimodal",
  asr_model: "multimodal",
  follow_up_model: "suggestions",
};

export function modelUsageKnowledgeBaseSection(
  bindings: readonly string[],
): KnowledgeBaseModelUsageSection {
  for (const binding of bindings) {
    const section = knowledgeBaseUsageSections[binding as KnownModelUsageBinding];
    if (section) return section;
  }
  return "models";
}

export function modelUsageAgentSection(bindings: readonly string[]): AgentModelUsageSection {
  for (const binding of bindings) {
    const section = agentUsageSections[binding as KnownModelUsageBinding];
    if (section) return section;
  }
  return "model";
}

export function modelUsageResourceRoute(
  kind: ModelUsageResourceKind,
  id: string,
  bindings: readonly string[] = [],
) {
  if (kind === "knowledge_base") {
    return { path: `/platform/knowledge-bases/${encodeURIComponent(id)}` };
  }
  return {
    path: "/platform/agents",
    query: { edit: id, section: modelUsageAgentSection(bindings) },
  };
}

// ---- model CRUD ------------------------------------------------------------------

export interface ModelConfig {
  id?: string;
  tenant_id?: number;
  name: string;
  display_name?: string;
  type: "KnowledgeQA" | "Embedding" | "Rerank" | "VLLM" | "ASR";
  source: "local" | "remote";
  description?: string;
  parameters: {
    base_url?: string;
    api_key?: string;
    provider?: string; // openai, aliyun, zhipu, generic
    embedding_parameters?: {
      dimension?: number;
      truncate_prompt_tokens?: number;
      supports_dimension_override?: boolean;
    };
    interface_type?: "ollama" | "openai"; // VLLM only
    parameter_size?: string; // Ollama param size, e.g. "7B"
    extra_config?: Record<string, string>;
    /* Extra HTTP headers on remote model API calls (like Python OpenAI
     * SDK extra_headers). Reserved headers (Authorization, Content-Type)
     * are ignored. */
    custom_headers?: Record<string, string>;
    supports_vision?: boolean;
    // Chat/VLM context window in tokens; 0/empty = backend default 200000.
    context_window?: number;
    max_output_tokens?: number;
    // Per-model concurrency cap for background tasks (ingest/enrichment),
    // shared across replicas by model id. 0 = global default. Only
    // effective for chat/embedding/vllm.
    max_concurrency?: number;
    app_id?: string;
    // Secret fields are never returned in this shape — they live behind
    // the /credentials subresource. Typed so create-mode payloads can
    // still carry them in the initial POST body.
    app_secret?: string;
  };
  is_default?: boolean;
  is_builtin?: boolean;
  status?: string;
  // Per-field configured? metadata. For builtin models it's returned only
  // to system administrators.
  credentials?: Record<ModelCredentialField, { configured: boolean }>;
  created_at?: string;
  updated_at?: string;
  deleted_at?: string | null;
}

type ModelEnvelope<T> = { success: boolean; data?: T; message?: string };

export async function createModel(data: ModelConfig): Promise<ModelConfig> {
  const response = await apiPost<ModelEnvelope<ModelConfig>>("/api/v1/models", data);
  if (response.success && response.data) return response.data;
  throw new Error(response.message || "Failed to create model");
}

/* Rejects (rather than resolving []) on failure so callers/caches can tell
 * "request failed" from "zero models" — a transient failure must not be
 * cached as an empty list. */
export async function listModels(type?: string): Promise<ModelConfig[]> {
  const response = await apiGet<ModelEnvelope<ModelConfig[]>>("/api/v1/models");
  const rows = response.success && response.data ? response.data : [];
  return type ? rows.filter((item) => item.type === type) : rows;
}

export async function getModel(id: string): Promise<ModelConfig> {
  const response = await apiGet<ModelEnvelope<ModelConfig>>(`/api/v1/models/${id}`);
  if (response.success && response.data) return response.data;
  throw new Error(response.message || "Failed to load model");
}

export async function updateModel(id: string, data: Partial<ModelConfig>): Promise<ModelConfig> {
  const response = await apiPut<ModelEnvelope<ModelConfig>>(`/api/v1/models/${id}`, data);
  if (response.success && response.data) return response.data;
  throw new Error(response.message || "Failed to update model");
}

export async function deleteModel(id: string): Promise<void> {
  try {
    const response = await apiDel<ModelEnvelope<unknown>>(`/api/v1/models/${id}`);
    if (response.success) return;
    const conflict = modelInUseErrorFromRequest(response);
    throw conflict ?? new Error(response.message || "Failed to delete model");
  } catch (error) {
    if (error instanceof ModelInUseError) throw error;
    const conflict = modelInUseErrorFromRequest(error);
    if (conflict) throw conflict;
    throw error;
  }
}

export interface ModelDebugOptions {
  system_prompt?: string;
  temperature?: number;
  top_p?: number;
  max_tokens?: number;
  thinking?: boolean;
}

export interface ModelDebugResult {
  ok: boolean;
  elapsed_ms: number;
  request: Record<string, unknown>;
  raw_response: unknown;
  observations: Record<string, unknown>;
  error?: string;
}

export async function debugModel(
  id: string,
  data: { input?: string; documents?: string[]; options?: ModelDebugOptions; file?: File | null },
): Promise<ModelDebugResult> {
  const form = new FormData();
  form.append("input", data.input || "");
  form.append("documents", JSON.stringify(data.documents || []));
  form.append("options", JSON.stringify(data.options || {}));
  if (data.file) form.append("file", data.file);
  const response = await apiUpload<ModelEnvelope<ModelDebugResult>>(
    `/api/v1/models/${id}/debug`,
    form,
    undefined,
    { timeoutMs: 300_000 },
  );
  if (response?.success && response?.data) return response.data;
  throw new Error(response?.message || "Model debug failed");
}

// ---- model credential subresource ---------------------------------------------------
// Secrets travel through /credentials instead of the main PUT body (see
// mcp.ts for the matching shape).

export type ModelCredentialField = "api_key" | "app_secret";

export interface ModelCredentialsResponse {
  fields: Record<ModelCredentialField, { configured: boolean }>;
}

export async function putModelCredentials(
  id: string,
  body: Partial<Record<ModelCredentialField, string>>,
): Promise<ModelCredentialsResponse> {
  const response = await apiPut<ModelEnvelope<ModelCredentialsResponse> | ModelCredentialsResponse>(
    `/api/v1/models/${id}/credentials`,
    body,
  );
  return (
    (response as ModelEnvelope<ModelCredentialsResponse>).data ?? response
  ) as ModelCredentialsResponse;
}

export async function deleteModelCredentialField(
  id: string,
  field: ModelCredentialField,
): Promise<void> {
  await apiDel(`/api/v1/models/${id}/credentials/${field}`);
}

// ---- WeKnoraCloud -------------------------------------------------------------------

export interface InitializeWeKnoraCloudRequest {
  app_id: string;
  app_secret: string;
}

/** Save WeKnoraCloud credentials only — does not auto-create models. */
export async function saveWeKnoraCloudCredentials(
  data: InitializeWeKnoraCloudRequest,
): Promise<{ success: boolean; message: string }> {
  const response = await apiPost<{ success: boolean; message?: string; error?: string }>(
    "/api/v1/weknoracloud/credentials",
    data,
  );
  if (response.success) return { success: true, message: response.message ?? "" };
  throw new Error(response.message || response.error || "Failed to save credentials");
}

export interface WeKnoraCloudStatusResult {
  has_models: boolean;
  needs_reinit: boolean;
  reason?: string;
}

export async function getWeKnoraCloudStatus(): Promise<WeKnoraCloudStatusResult> {
  try {
    const response = await apiGet<
      | (WeKnoraCloudStatusResult & { success?: boolean; data?: WeKnoraCloudStatusResult })
    >("/api/v1/models/weknoracloud/status");
    // The status endpoint returns the object directly, not wrapped.
    if (response && typeof response.has_models === "boolean") return response;
    if (response?.success && response?.data) return response.data;
    return { has_models: false, needs_reinit: false };
  } catch {
    return { has_models: false, needs_reinit: false };
  }
}
