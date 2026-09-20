/* Ported from frontend/src/api/initialization/index.ts.
 * i18n t() fallbacks replaced with plain strings. */
import { apiGet, apiPost, apiPut, apiUpload } from "@/lib/api-client";

// GET /initialization/config/:kbId exposes credential presence, not values.
export interface ModelCredentialStatus {
  apiKey?: boolean;
}

export interface COSCredentialStatus {
  secretId?: boolean;
  secretKey?: boolean;
}

export interface Node {
  name: string;
  attributes: string[];
}

export interface Relation {
  node1: string;
  node2: string;
  type: string;
}

export interface InitializationConfig {
  llm: {
    source: string;
    modelName: string;
    baseUrl?: string;
    /** @deprecated Use credentials.apiKey from GET responses */
    apiKey?: string;
    credentials?: ModelCredentialStatus;
  };
  embedding: {
    source: string;
    modelName: string;
    baseUrl?: string;
    /** @deprecated Use credentials.apiKey from GET responses */
    apiKey?: string;
    dimension?: number;
    credentials?: ModelCredentialStatus;
  };
  rerank: {
    modelName: string;
    baseUrl: string;
    /** @deprecated Use credentials.apiKey from GET responses */
    apiKey?: string;
    enabled: boolean;
    credentials?: ModelCredentialStatus;
  };
  multimodal: {
    enabled: boolean;
    storageType: "cos" | "minio";
    vlm?: {
      modelName: string;
      baseUrl: string;
      /** @deprecated Use credentials.apiKey from GET responses */
      apiKey?: string;
      interfaceType?: string; // "ollama" or "openai"
      credentials?: ModelCredentialStatus;
    };
    cos?: {
      region: string;
      bucketName: string;
      appId: string;
      pathPrefix?: string;
      /** @deprecated Use credentials from GET responses */
      secretId?: string;
      /** @deprecated Use credentials from GET responses */
      secretKey?: string;
      credentials?: COSCredentialStatus;
    };
    minio?: {
      bucketName: string;
      pathPrefix?: string;
    };
  };
  documentSplitting: {
    chunkSize: number;
    chunkOverlap: number;
    separators: string[];
    // Adaptive chunking strategy. Empty / "legacy" = classic recursive
    // splitter; "auto" lets the backend profiler pick a tier;
    // "heading" / "heuristic" pin the tier explicitly.
    strategy?: string;
    // Cap chunk size in approx tokens. 0 = char-based budget only.
    tokenLimit?: number;
    // Language hints for heuristic patterns ("de", "en", "zh"). Empty = auto.
    languages?: string[];
  };
  /** Frontend-only hint for storage selection UI. */
  storageType?: "cos" | "minio";
  nodeExtract: {
    enabled: boolean;
    text: string;
    tags: string[];
    nodes: Node[];
    relations: Relation[];
  };
}

export interface DownloadTask {
  id: string;
  modelName: string;
  status: "pending" | "downloading" | "completed" | "failed";
  progress: number;
  message: string;
  startTime: string;
  endTime?: string;
}

/** Simplified KB config update — model IDs only. */
export interface KBModelConfigRequest {
  llmModelId: string;
  embeddingModelId: string;
  vlm_config?: {
    enabled: boolean;
    model_id?: string;
    description_language?: string;
    custom_instructions?: string;
  };
  asr_config?: {
    enabled: boolean;
    model_id?: string;
    language?: string;
  };
  documentSplitting: {
    chunkSize: number;
    chunkOverlap: number;
    separators: string[];
    parserEngineRules?: {
      file_types: string[];
      engine: string;
      xlsx_first_row_as_header?: boolean;
    }[];
    enableParentChild?: boolean;
    parentChunkSize?: number;
    childChunkSize?: number;
    /* Adaptive chunking strategy ("auto" | "heading" | "heuristic" |
     * "legacy"). Backend uses pointer DTOs for these three:
     * - undefined / not set → no change on server
     * - "" / 0 / [] explicitly sent → clears the value
     * Send the field whenever the user opened the editor — even empty —
     * so defaults can always be restored. */
    strategy?: string;
    /** Approx token budget per chunk; 0 = char-based. */
    tokenLimit?: number;
    /** Language hints; empty array = auto-detect. */
    languages?: string[];
    tableMetadataInstructions?: string;
  };
  multimodal: {
    enabled: boolean;
  };
  /** Storage engine ("local" | "minio" | "cos" | "obs" ...) for uploads and
   * in-document images. */
  storageBackendId?: string;
  storageProvider?: string;
  nodeExtract: {
    enabled: boolean;
    text: string;
    tags: string[];
    nodes: Node[];
    relations: Relation[];
    customInstructions?: string;
  };
  questionGeneration?: {
    enabled: boolean;
    questionCount: number;
    customInstructions?: string;
  };
}

export function updateKBConfig(kbId: string, config: KBModelConfigRequest): Promise<unknown> {
  return apiPut(`/api/v1/initialization/config/${kbId}`, config);
}

/** Legacy full-config update; kept for compatibility. */
export function initializeSystemByKB(
  kbId: string,
  config: InitializationConfig,
): Promise<unknown> {
  return apiPost(`/api/v1/initialization/initialize/${kbId}`, config);
}

export function checkOllamaStatus(): Promise<{
  available: boolean;
  version?: string;
  error?: string;
  baseUrl?: string;
}> {
  return apiGet<{ data?: { available: boolean; version?: string; baseUrl?: string } }>(
    "/api/v1/initialization/ollama/status",
  )
    .then((r) => r.data || { available: false })
    .catch((error: Error) => ({ available: false, error: error.message || "check failed" }));
}

export interface OllamaModelInfo {
  name: string;
  size: number;
  digest: string;
  modified_at: string;
}

export function listOllamaModels(): Promise<OllamaModelInfo[]> {
  return apiGet<{ data?: { models?: OllamaModelInfo[] } }>(
    "/api/v1/initialization/ollama/models",
  )
    .then((r) => r.data?.models || [])
    .catch(() => []);
}

export function checkOllamaModels(
  models: string[],
): Promise<{ models: Record<string, boolean> }> {
  return apiPost<{ data?: { models: Record<string, boolean> } }>(
    "/api/v1/initialization/ollama/models/check",
    { models },
  ).then((r) => r.data || { models: {} });
}

/** Start an async Ollama model download. */
export function downloadOllamaModel(
  modelName: string,
): Promise<{ taskId: string; modelName: string; status: string; progress: number }> {
  return apiPost<{
    data?: { taskId: string; modelName: string; status: string; progress: number };
  }>("/api/v1/initialization/ollama/models/download", { modelName }).then(
    (r) => r.data || { taskId: "", modelName, status: "failed", progress: 0 },
  );
}

export function getDownloadProgress(taskId: string): Promise<DownloadTask> {
  return apiGet<{ data?: DownloadTask }>(
    `/api/v1/initialization/ollama/download/progress/${taskId}`,
  ).then((r) => r.data as DownloadTask);
}

export function listDownloadTasks(): Promise<DownloadTask[]> {
  return apiGet<{ data?: DownloadTask[] }>("/api/v1/initialization/ollama/download/tasks").then(
    (r) => r.data || [],
  );
}

export function getCurrentConfigByKB(
  kbId: string,
): Promise<InitializationConfig & { hasFiles: boolean }> {
  return apiGet<{ data?: InitializationConfig & { hasFiles: boolean } }>(
    `/api/v1/initialization/config/${kbId}`,
  ).then((r) => (r.data || {}) as InitializationConfig & { hasFiles: boolean });
}

/* Shared optional fields for all "test connection" endpoints.
 * customHeaders / extraConfig / interfaceType pass straight through to the
 * real model assembly, so test-connection exercises the production path. */
interface BaseModelTestPayload {
  customHeaders?: Record<string, string>;
  extraConfig?: Record<string, string>;
  interfaceType?: string;
  /** Second secret leg (e.g. LKEAP Rerank's Tencent Cloud SecretKey). */
  appSecret?: string;
}

export function checkRemoteModel(
  modelConfig: {
    modelName: string;
    baseUrl: string;
    apiKey?: string;
    provider?: string;
    // Editing an existing model: pass modelId and the backend fills in the
    // stored apiKey (the UI no longer echoes plaintext secrets, so test
    // connections must use this refill path).
    modelId?: string;
  } & BaseModelTestPayload,
): Promise<{ available: boolean; message?: string }> {
  return apiPost<{ data?: { available: boolean; message?: string } }>(
    "/api/v1/initialization/remote/check",
    modelConfig,
  ).then((r) => r.data || { available: false });
}

export function testEmbeddingModel(
  modelConfig: {
    source: "local" | "remote";
    modelName: string;
    baseUrl?: string;
    apiKey?: string;
    dimension?: number;
    supportsDimensionOverride?: boolean;
    provider?: string;
    modelId?: string;
  } & BaseModelTestPayload,
): Promise<{ available: boolean; message?: string; dimension?: number }> {
  return apiPost<{ data?: { available: boolean; message?: string; dimension?: number } }>(
    "/api/v1/initialization/embedding/test",
    modelConfig,
  ).then((r) => r.data || { available: false });
}

export function checkRerankModel(
  modelConfig: {
    modelName: string;
    baseUrl: string;
    apiKey?: string;
    provider?: string;
    modelId?: string;
  } & BaseModelTestPayload,
): Promise<{ available: boolean; message?: string }> {
  return apiPost<{ data?: { available: boolean; message?: string } }>(
    "/api/v1/initialization/rerank/check",
    modelConfig,
  ).then((r) => r.data || { available: false });
}

/** Test ASR model connectivity (via the /v1/audio/transcriptions endpoint). */
export function checkASRModel(
  modelConfig: {
    modelName: string;
    baseUrl: string;
    apiKey?: string;
    provider?: string;
    modelId?: string;
  } & BaseModelTestPayload,
): Promise<{ available: boolean; message?: string }> {
  return apiPost<{ data?: { available: boolean; message?: string } }>(
    "/api/v1/initialization/asr/check",
    modelConfig,
  ).then((r) => r.data || { available: false });
}

export function testMultimodalFunction(testData: {
  image: File;
  vlm_model: string;
  vlm_base_url: string;
  vlm_api_key?: string;
  vlm_interface_type?: string;
  storage_type?: "cos" | "minio";
  // COS fields (required only when storage_type === 'cos')
  cos_secret_id?: string;
  cos_secret_key?: string;
  cos_region?: string;
  cos_bucket_name?: string;
  cos_app_id?: string;
  cos_path_prefix?: string;
  // MinIO fields
  minio_bucket_name?: string;
  minio_path_prefix?: string;
  chunk_size: number;
  chunk_overlap: number;
  separators: string[];
}): Promise<{
  success: boolean;
  caption?: string;
  ocr?: string;
  processing_time?: number;
  message?: string;
}> {
  const formData = new FormData();
  formData.append("image", testData.image);
  formData.append("vlm_model", testData.vlm_model);
  formData.append("vlm_base_url", testData.vlm_base_url);
  if (testData.vlm_api_key) formData.append("vlm_api_key", testData.vlm_api_key);
  if (testData.vlm_interface_type)
    formData.append("vlm_interface_type", testData.vlm_interface_type);
  if (testData.storage_type) formData.append("storage_type", testData.storage_type);
  if (testData.storage_type === "cos") {
    if (testData.cos_secret_id) formData.append("cos_secret_id", testData.cos_secret_id);
    if (testData.cos_secret_key) formData.append("cos_secret_key", testData.cos_secret_key);
    if (testData.cos_region) formData.append("cos_region", testData.cos_region);
    if (testData.cos_bucket_name)
      formData.append("cos_bucket_name", testData.cos_bucket_name);
    if (testData.cos_app_id) formData.append("cos_app_id", testData.cos_app_id);
    if (testData.cos_path_prefix) formData.append("cos_path_prefix", testData.cos_path_prefix);
  }
  if (testData.minio_bucket_name)
    formData.append("minio_bucket_name", testData.minio_bucket_name);
  if (testData.minio_path_prefix)
    formData.append("minio_path_prefix", testData.minio_path_prefix);
  formData.append("chunk_size", testData.chunk_size.toString());
  formData.append("chunk_overlap", testData.chunk_overlap.toString());
  formData.append("separators", JSON.stringify(testData.separators));

  // The old code used raw fetch; apiUpload supplies the same Bearer +
  // X-Tenant-ID headers.
  return apiUpload<{ success: boolean; data?: { caption?: string; ocr?: string; processing_time?: number }; message?: string }>(
    "/api/v1/initialization/multimodal/test",
    formData,
  ).then((data) =>
    data.success ? { success: true, ...(data.data || {}) } : { success: false, message: data.message || "test failed" },
  );
}

// ---- relation extraction -------------------------------------------------------------

export interface TextRelationExtractionRequest {
  text: string;
  tags: string[];
  model_id: string;
}

export interface TextRelationExtractionResponse {
  nodes: Node[];
  relations: Relation[];
}

export function extractTextRelations(
  request: TextRelationExtractionRequest,
): Promise<TextRelationExtractionResponse> {
  return apiPost<{ data?: TextRelationExtractionResponse }>(
    "/api/v1/initialization/extract/text-relation",
    request,
    { timeoutMs: 60_000 },
  ).then((r) => r.data || { nodes: [], relations: [] });
}

export interface FabriTextRequest {
  tags: string[];
  model_id: string;
}

export interface FabriTextResponse {
  text: string;
}

export function fabriText(request: FabriTextRequest): Promise<FabriTextResponse> {
  return apiPost<{ data?: FabriTextResponse }>(
    "/api/v1/initialization/extract/fabri-text",
    request,
  ).then((r) => r.data || { text: "" });
}

export type FabriTagRequest = Record<string, never>;

export interface FabriTagResponse {
  tags: string[];
}

export function fabriTag(request: FabriTagRequest): Promise<FabriTagResponse> {
  return apiPost<{ data?: FabriTagResponse }>(
    "/api/v1/initialization/extract/fabri-tag",
    request,
  ).then((r) => r.data || { tags: [] });
}

// ---- model providers ----------------------------------------------------------------------

export interface ModelProviderOption {
  /** Provider identifier. */
  value: string;
  /** Display name. */
  label: string;
  description: string;
  /** Default URLs keyed by model type. */
  defaultUrls: Record<string, string>;
  /** Supported model types. */
  modelTypes: string[];
}

/** Returns [] on failure — the UI falls back to defaults. */
export function listModelProviders(modelType?: string): Promise<ModelProviderOption[]> {
  const url = modelType
    ? `/api/v1/models/providers?model_type=${encodeURIComponent(modelType)}`
    : "/api/v1/models/providers";
  return apiGet<{ data?: ModelProviderOption[] }>(url)
    .then((r) => r.data || [])
    .catch(() => []);
}
