/* Ported from frontend/src/api/agent/index.ts. */
import { apiDel, apiGet, apiPost, apiPut } from "@/lib/api-client";

// Type presets for smart-reasoning mode:
//   'rag-qa'          : classic document/FAQ chunked RAG
//   'wiki-qa'         : wiki graph-navigation QA
//   'hybrid-rag-wiki' : wiki + chunk hybrid retrieval
//   'custom'          : fully custom (no preset applied)
export type AgentType = "rag-qa" | "wiki-qa" | "hybrid-rag-wiki" | "data-analysis" | "custom";

export interface QuestionSuggestionConfig {
  starters: {
    enabled: boolean;
    mode: "curated" | "knowledge" | "hybrid";
    items: string[];
    count: number;
  };
  follow_ups: {
    enabled: boolean;
    mode: "generated" | "knowledge" | "hybrid";
    count: number;
    model_id?: string;
    additional_instruction?: string;
    categories: Array<"clarify" | "deepen" | "action">;
    max_context_turns: number;
    suppress_on_fallback: boolean;
    suppress_when_answer_asks_question: boolean;
    knowledge_fallback: boolean;
    allow_regenerate: boolean;
  };
}

export interface CustomAgentConfig {
  // ===== basics =====
  agent_mode?: "quick-answer" | "smart-reasoning"; // quick-answer=RAG, smart-reasoning=ReAct agent
  // Type preset for one-click "system prompt + tools + KB compat" combos;
  // only effective in smart-reasoning mode, ignored by quick-answer.
  agent_type?: AgentType;
  system_prompt?: string; // {{web_search_status}} placeholder toggles behavior dynamically
  system_prompt_id?: string; // referenced prompt template id (presets fill this)
  context_template_id?: string; // inherit referenced context template when text is empty
  context_template?: string;

  // ===== model =====
  model_id?: string;
  rerank_model_id?: string;
  temperature?: number;
  max_completion_tokens?: number; // 0 = system default
  thinking?: boolean;
  citation_enabled?: boolean; // cite KB/web sources in the final answer (default on)

  // ===== agent mode =====
  max_iterations?: number; // -1 = unlimited
  llm_call_timeout?: number; // seconds
  allowed_tools?: string[];
  reflection_enabled?: boolean;
  // MCP selection: all=every enabled service, selected=listed ids, none=off
  mcp_selection_mode?: "all" | "selected" | "none";
  mcp_services?: string[];
  // Seconds to wait on an in-conversation OAuth prompt before auto-skipping;
  // <=0 uses the server default. Only affects OAuth-enabled MCP services.
  mcp_auth_wait_timeout?: number;

  // ===== skills (agent mode only) =====
  skills_selection_mode?: "all" | "selected" | "none";
  selected_skills?: string[];

  // ===== sandbox =====
  // Sandbox config the agent's skill scripts run on; empty disables sandbox
  // execution. Points at the logical config, not a version, so credential
  // rotation doesn't require re-assigning every agent.
  sandbox_config_id?: string;

  // ===== knowledge bases =====
  kb_selection_mode?: "all" | "selected" | "none";
  knowledge_bases?: string[];
  // true: only retrieve when the user explicitly @-mentions a KB/document
  retrieve_kb_only_when_mentioned?: boolean;

  // ===== image upload / multimodal =====
  image_upload_enabled?: boolean;
  vlm_model_id?: string;
  image_storage_provider?: string;
  audio_upload_enabled?: boolean;
  asr_model_id?: string;
  attachment_image_understanding?: boolean;
  attachment_ocr_max_pages?: number; // 0 = global default
  attachment_parse_wait_timeout_sec?: number; // 0 = global default

  // ===== chat attachment parser-engine policy =====
  // Priority: request parser_engine > agent rules > tenant rules > auto
  chat_parser_engine_rules?: { file_types: string[]; engine: string }[];

  // ===== file type limits =====
  supported_file_types?: string[]; // empty = all types

  // ===== web search =====
  web_search_enabled?: boolean;
  web_search_provider_id?: string;
  web_search_max_results?: number;

  // ===== multi-turn =====
  multi_turn_enabled?: boolean;
  history_turns?: number;

  // ===== long-term memory =====
  // Default (legacy rows) behaves as true — a disable-only switch: turning
  // it on here cannot re-enable memory when the workspace setting is off.
  memory_enabled?: boolean;

  // ===== retrieval strategy =====
  embedding_top_k?: number;
  keyword_threshold?: number;
  vector_threshold?: number;
  rerank_top_k?: number;
  rerank_threshold?: number;

  // ===== advanced (mainly quick-answer mode) =====
  enable_query_expansion?: boolean;
  enable_rewrite?: boolean;
  rewrite_prompt_system?: string;
  rewrite_prompt_user?: string;
  fallback_strategy?: "fixed" | "model";
  fallback_response?: string;
  fallback_prompt?: string;
  // Intent prompts override the main system prompt for non-retrieval
  // intents (greetings, small talk).
  intent_prompts?: Record<string, string>;

  // ===== deprecated (kept for compat) =====
  welcome_message?: string;
  question_suggestions?: QuestionSuggestionConfig;
}

export interface CustomAgent {
  id: string;
  name: string;
  description?: string;
  avatar?: string;
  is_builtin: boolean;
  tenant_id?: number;
  created_by?: string;
  // creator_name is batch-backfilled by the list endpoint for the source
  // badge on list cards.
  creator_name?: string;
  config: CustomAgentConfig;
  created_at?: string;
  updated_at?: string;
}

export interface CreateAgentRequest {
  name: string;
  description?: string;
  avatar?: string;
  config?: CustomAgentConfig;
}

export interface UpdateAgentRequest {
  name: string;
  description?: string;
  avatar?: string;
  config?: CustomAgentConfig;
}

// Built-in agent ids
export const BUILTIN_QUICK_ANSWER_ID = "builtin-quick-answer";
export const BUILTIN_SMART_REASONING_ID = "builtin-smart-reasoning";

export const AGENT_MODE_QUICK_ANSWER = "quick-answer";
export const AGENT_MODE_SMART_REASONING = "smart-reasoning";

/** @deprecated Use BUILTIN_QUICK_ANSWER_ID */
export const BUILTIN_AGENT_NORMAL_ID = BUILTIN_QUICK_ANSWER_ID;
/** @deprecated Use BUILTIN_SMART_REASONING_ID */
export const BUILTIN_AGENT_AGENT_ID = BUILTIN_SMART_REASONING_ID;

/* Agent list including built-ins. The response may also carry
 * disabled_own_agent_ids: ids of "my" agents disabled in this workspace's
 * chat dropdown (workspace-scoped only). */
export function listAgents(params?: {
  /* Creator filter; built-in agents (is_builtin=true) are always returned
   * regardless so the dropdown never silently loses the presets. */
  creator?: "all" | "mine" | "others";
}) {
  const qs = params?.creator && params.creator !== "all" ? `?creator=${params.creator}` : "";
  return apiGet<{ data: CustomAgent[]; disabled_own_agent_ids?: string[] }>(`/api/v1/agents${qs}`);
}

export function getAgentById(id: string) {
  return apiGet<{ data: CustomAgent }>(`/api/v1/agents/${id}`);
}

export function createAgent(data: CreateAgentRequest) {
  return apiPost<{ data: CustomAgent }>("/api/v1/agents", data);
}

export function updateAgent(id: string, data: UpdateAgentRequest) {
  return apiPut<{ data: CustomAgent }>(`/api/v1/agents/${id}`, data);
}

export function deleteAgent(id: string) {
  return apiDel<{ success: boolean }>(`/api/v1/agents/${id}`);
}

export function copyAgent(id: string) {
  return apiPost<{ data: CustomAgent }>(`/api/v1/agents/${id}/copy`, {});
}

export function isBuiltinAgent(agentId: string): boolean {
  return agentId.startsWith("builtin-");
}

export interface PlaceholderDefinition {
  name: string;
  label: string;
  description: string;
}

export interface PlaceholdersResponse {
  all: PlaceholderDefinition[];
  system_prompt: PlaceholderDefinition[];
  agent_system_prompt: PlaceholderDefinition[];
  context_template: PlaceholderDefinition[];
  rewrite_system_prompt: PlaceholderDefinition[];
  rewrite_prompt: PlaceholderDefinition[];
  fallback_prompt: PlaceholderDefinition[];
}

export function getPlaceholders() {
  return apiGet<{ data: PlaceholdersResponse }>("/api/v1/agents/placeholders");
}

// ===== agent type presets =====

// Backend kb_filter shape (internal/types/agent_type_preset.go)
export interface AgentTypeKBFilter {
  any_of?: string[]; // KB must have at least one
  all_of?: string[]; // KB must have all
  none_of?: string[]; // KB must have none
}

// KB capability tags (JSON of backend types.KBCapabilities)
export interface KBCapabilities {
  vector: boolean;
  keyword: boolean;
  wiki: boolean;
  graph: boolean;
  faq: boolean;
}

// Preset auto-fill payload: only fields the preset overrides; others untouched.
export interface AgentTypePresetConfig {
  system_prompt_id?: string;
  temperature?: number;
  max_iterations?: number;
  allowed_tools?: string[];
  retain_retrieval_history?: boolean;
  faq_priority_enabled?: boolean;
  web_search_enabled?: boolean;
  supported_file_types?: string[];
  kb_selection_mode?: "all" | "selected" | "none";
}

export interface AgentTypePresetI18n {
  label: string;
  description: string;
}

export interface AgentTypePreset {
  id: AgentType;
  i18n: Record<string, AgentTypePresetI18n>;
  config?: AgentTypePresetConfig; // empty = "custom" type (no preset)
  kb_filter?: AgentTypeKBFilter; // empty = all KBs selectable
}

export function getAgentTypePresets() {
  return apiGet<{ data: AgentTypePreset[] }>("/api/v1/agents/type-presets");
}

// ===== IM channels =====

export interface IMChannel {
  id: string;
  tenant_id?: number;
  agent_id: string;
  // 'lark' is Feishu's international edition; shares Feishu's credentials/modes.
  platform:
    | "wecom"
    | "feishu"
    | "lark"
    | "slack"
    | "telegram"
    | "dingtalk"
    | "mattermost"
    | "wechat"
    | "qqbot"
    | "yunzhijia";
  name: string;
  enabled: boolean;
  mode: "webhook" | "websocket" | "longpoll";
  output_mode: "stream" | "full";
  session_mode?: "user" | "thread";
  knowledge_base_id?: string;
  credentials: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
}

export function listIMChannels(agentId: string) {
  return apiGet<{ data: IMChannel[] }>(`/api/v1/agents/${agentId}/im-channels`);
}

/* Tenant-wide overview row. Credentials intentionally omitted — use
 * listIMChannels(agentId) to edit a specific channel. */
export interface IMChannelOverview {
  id: string;
  tenant_id: number;
  agent_id: string;
  agent_name: string; // localized built-in name when the agent is built-in
  platform: IMChannel["platform"];
  name: string;
  enabled: boolean;
  mode: IMChannel["mode"];
  output_mode: IMChannel["output_mode"];
  session_mode?: IMChannel["session_mode"];
  bot_identity: string;
  created_at: string;
  updated_at: string;
}

export function listAllIMChannels() {
  return apiGet<{ data: IMChannelOverview[] }>("/api/v1/im-channels");
}

export function createIMChannel(agentId: string, data: Partial<IMChannel>) {
  return apiPost<{ data: IMChannel }>(`/api/v1/agents/${agentId}/im-channels`, data);
}

export function updateIMChannel(id: string, data: Partial<IMChannel>) {
  return apiPut<{ data: IMChannel }>(`/api/v1/im-channels/${id}`, data);
}

export function deleteIMChannel(id: string) {
  return apiDel<{ success: boolean }>(`/api/v1/im-channels/${id}`);
}

export function toggleIMChannel(id: string) {
  return apiPost<{ data: IMChannel }>(`/api/v1/im-channels/${id}/toggle`, {});
}

// ===== suggested questions =====

export interface SuggestedQuestion {
  question: string;
  source: "faq" | "document" | "agent_config" | "wiki";
  knowledge_base_id?: string;
  knowledge_id?: string;
}

/* Suggested questions for the chat panel, scoped by the agent's KB range. */
export function getSuggestedQuestions(
  agentId: string,
  params?: {
    knowledge_base_ids?: string[];
    knowledge_ids?: string[];
    tag_scopes?: Array<{ knowledge_base_id: string; tag_ids: string[] }>;
    limit?: number;
  },
) {
  const query = new URLSearchParams();
  if (params?.knowledge_base_ids?.length)
    query.set("knowledge_base_ids", params.knowledge_base_ids.join(","));
  if (params?.knowledge_ids?.length) query.set("knowledge_ids", params.knowledge_ids.join(","));
  if (params?.tag_scopes?.length) query.set("tag_scopes", JSON.stringify(params.tag_scopes));
  if (params?.limit) query.set("limit", String(params.limit));
  const qs = query.toString();
  return apiGet<{ data: { questions: SuggestedQuestion[] } }>(
    `/api/v1/agents/${agentId}/suggested-questions${qs ? "?" + qs : ""}`,
  );
}

// ===== WeChat QR code login =====

export interface WeChatQRCodeResult {
  qrcode_url: string;
  qrcode: string;
}

export interface WeChatQRCodeStatus {
  status: "wait" | "scaned" | "confirmed" | "expired";
  credentials?: {
    bot_token: string;
    ilink_bot_id: string;
    ilink_user_id: string;
  };
  baseurl?: string;
}

export function getWeChatQRCode() {
  return apiPost<{ data: WeChatQRCodeResult }>("/api/v1/wechat/qrcode", {});
}

export function pollWeChatQRCodeStatus(qrcode: string) {
  return apiPost<{ data: WeChatQRCodeStatus }>("/api/v1/wechat/qrcode/status", { qrcode });
}
