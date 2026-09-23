/* Ported from frontend/src/api/chat/index.ts + chat/steer.ts +
 * chat-history.ts + message-suggestion.ts + artifacts.ts.
 * SSE streaming itself lives in ./stream.ts (postChat equivalent).
 */
import { apiDel, apiDownload, apiGet, apiPost, apiPut } from "@/lib/api-client";

// ---- sessions ---------------------------------------------------------------

export type SessionLastRequestState = {
  agent_id?: string;
  agent_enabled?: boolean;
  model_id?: string;
  knowledge_base_ids?: string[];
  knowledge_ids?: string[];
  tag_ids?: string[];
  mcp_service_ids?: string[];
  skill_names?: string[];
  local_browser_enabled?: boolean;
  web_search_enabled?: boolean;
};

export type SessionRow = {
  id: string;
  title?: string;
  description?: string;
  created_at?: string;
  updated_at?: string;
  is_pinned?: boolean;
  last_request_state?: SessionLastRequestState;
};

type SessionListResponse = {
  success: boolean;
  data?: SessionRow[];
  total?: number;
};

export function listSessions(page = 1, pageSize = 30, source?: string) {
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
  if (source) params.set("source", source);
  return apiGet<SessionListResponse>(`/api/v1/sessions?${params.toString()}`);
}

export function createSession(data: Record<string, unknown> = {}) {
  return apiPost<{ success: boolean; data?: { id: string } }>(`/api/v1/sessions`, data);
}

export function getSession(id: string) {
  return apiGet<{ success: boolean; data?: SessionRow }>(`/api/v1/sessions/${id}`);
}

export function updateSession(id: string, data: { title: string; description?: string }) {
  return apiPut(`/api/v1/sessions/${id}`, data);
}

export function deleteSession(id: string) {
  return apiDel(`/api/v1/sessions/${id}`);
}

export function batchDeleteSessions(ids: string[]) {
  return apiDel(`/api/v1/sessions/batch`, { ids });
}

export function deleteAllSessions() {
  return apiDel(`/api/v1/sessions/batch`, { delete_all: true });
}

export function pinSession(id: string) {
  return apiPost(`/api/v1/sessions/${id}/pin`, {});
}

export function unpinSession(id: string) {
  return apiDel(`/api/v1/sessions/${id}/pin`);
}

export function generateSessionTitle(id: string, data: Record<string, unknown>) {
  return apiPost(`/api/v1/sessions/${id}/generate_title`, data);
}

/* Fork can re-run pipeline work server-side — keep the 180s budget. */
export function forkSession(id: string, data: { message_id: string; title?: string }) {
  return apiPost(`/api/v1/sessions/${id}/fork`, data, { timeoutMs: 180_000 });
}

export function stopSession(id: string, messageId: string) {
  return apiPost(`/api/v1/sessions/${id}/stop`, { message_id: messageId });
}

export function clearSessionMessages(id: string) {
  return apiDel(`/api/v1/sessions/${id}/messages`);
}

import type { KnowledgeReferenceItem } from "@/components/chat/references-drawer";

// ---- messages ---------------------------------------------------------------

export type ChatMessageToolCall = {
  name?: string;
  result?: { data?: unknown };
};

export type ChatMessageAgentStep = {
  tool_calls?: ChatMessageToolCall[];
};

export type ChatMessage = {
  id?: string;
  role: "user" | "assistant" | string;
  content?: string;
  created_at?: string;
  is_completed?: boolean;
  knowledge_references?: KnowledgeReferenceItem[];
  agent_steps?: ChatMessageAgentStep[];
};

export function listMessages(sessionId: string, limit = 30, beforeTime = "") {
  const qs = beforeTime
    ? `?before_time=${encodeURIComponent(beforeTime)}&limit=${limit}`
    : `?limit=${limit}`;
  return apiGet<{ success: boolean; data?: ChatMessage[] }>(`/api/v1/messages/${sessionId}/load${qs}`);
}

// ---- steer (append to a running agent turn) ---------------------------------

export type SteerDelivery = "inject" | "after";

export type SteerQueueItem = {
  steer_id: string;
  content: string;
  delivery: SteerDelivery;
  mentioned_items?: unknown[];
  promoting?: boolean;
  awaitingIdleSend?: boolean;
  // True while POST /steer is in flight. The stable client UUID is also the
  // durable server ID; disable actions until its queue entry exists.
  pending?: boolean;
  failed?: boolean;
  expected_assistant_message_id?: string;
  client_id?: string;
};

/* Append a message to a running agent turn.
 * Returns { success, status: 'queued' | 'already_injected' | 'new_run', steer_id?, delivery?, assistant_message_id? }.
 * - delivery 'after' (default): waits until the current run exits, then starts a follow-up turn.
 * - delivery 'inject': the running engine injects it at the next round boundary.
 * - 'new_run': no run is live; the caller should fall back to a normal send.
 * Lookup failures (503) reject rather than returning new_run — treating them
 * as idle would start a second turn on top of the one still generating. */
export function steerSession(
  sessionId: string,
  query: string,
  mentionedItems: unknown[] = [],
  delivery: SteerDelivery = "after",
  expectedAssistantMessageId?: string,
  steerId?: string,
) {
  return apiPost(`/api/v1/sessions/${sessionId}/steer`, {
    query,
    expected_assistant_message_id: expectedAssistantMessageId,
    steer_id: steerId,
    mentioned_items: mentionedItems,
    channel: "web",
    delivery,
  });
}

/** Flip a queued after-message to inject so the running turn reads it next. */
export function promoteSteerSession(sessionId: string, steerId: string) {
  return apiPost(`/api/v1/sessions/${sessionId}/steer/${steerId}/inject`, {});
}

/** Pending overlay items for the live run. Empty when nothing is generating. */
export function listSteerSession(sessionId: string) {
  return apiGet(`/api/v1/sessions/${sessionId}/steer`);
}

/** Drop a queued overlay item so it is neither injected nor sent as a follow-up. */
export function removeSteerSession(sessionId: string, steerId: string) {
  return apiDel(`/api/v1/sessions/${sessionId}/steer/${steerId}`);
}

// ---- artifacts --------------------------------------------------------------
// The backend keeps the physical storage path private and indexes each file by
// its position (:index) in the owning assistant message. List endpoints return
// metadata only — clients must go through downloadArtifact to fetch bytes so
// ownership checks always run server-side.

export interface ArtifactMeta {
  index: number;
  /** `resource://<handle>` — stable identity the answer body references.
   * Empty when the deployment runs without a resource catalog. */
  handle?: string;
  file_name: string;
  file_type: string;
  file_size: number;
  source_path: string;
  mod_time: string;
  created_at: string;
}

export function listMessageArtifacts(sessionId: string, messageId: string) {
  return apiGet(`/api/v1/sessions/${sessionId}/messages/${messageId}/artifacts`);
}

export function listSessionArtifacts(sessionId: string) {
  return apiGet(`/api/v1/sessions/${sessionId}/artifacts`);
}

/* Streams a single artifact's bytes as a Blob for a browser download. A plain
 * `<a href>` can't carry the Bearer token; apiDownload keeps auth intact. */
export function downloadArtifact(
  sessionId: string,
  messageId: string,
  index: number,
): Promise<Blob> {
  return apiDownload(
    `/api/v1/sessions/${sessionId}/messages/${messageId}/artifacts/${index}/download`,
  );
}

// ---- artifact library (cross-session) ---------------------------------------

export interface ArtifactLibraryItem {
  session_id: string;
  session_title: string;
  message_id: string;
  index: number;
  /** `resource://<handle>` when the deployment runs a resource catalog. */
  handle?: string;
  file_name: string;
  file_type: string;
  file_size: number;
  source_path: string;
  created_at: string;
  /** How many times this file was (re)generated in its session. */
  version_count: number;
}

export interface ArtifactLibraryParams {
  keyword?: string;
  /** Extensions such as ".pdf"; empty means every type. */
  fileTypes?: string[];
  page?: number;
  pageSize?: number;
}

export interface ArtifactLibraryPage {
  success: boolean;
  data: ArtifactLibraryItem[];
  total: number;
  page: number;
  page_size: number;
}

export function listArtifactLibrary(params: ArtifactLibraryParams = {}) {
  const query = new URLSearchParams();
  const keyword = params.keyword?.trim();
  if (keyword) query.set("keyword", keyword);
  if (params.fileTypes?.length) query.set("file_types", params.fileTypes.join(","));
  if (params.page) query.set("page", String(params.page));
  if (params.pageSize) query.set("page_size", String(params.pageSize));
  const qs = query.toString();
  return apiGet<ArtifactLibraryPage>(`/api/v1/artifacts${qs ? `?${qs}` : ""}`);
}

// ---- message suggestions -----------------------------------------------------

export interface MessageSuggestionItem {
  id: string;
  text: string;
  category?: "clarify" | "deepen" | "action";
  source: "model" | "faq" | "document" | "wiki" | string;
  knowledge_base_ids?: string[];
}

export interface MessageSuggestionSet {
  id: string;
  session_id: string;
  assistant_message_id: string;
  status: "generating" | "ready" | "suppressed" | "failed";
  allow_regenerate: boolean;
  suppression_reason?: string;
  questions: MessageSuggestionItem[];
  generated_at?: string;
}

export function ensureMessageSuggestions(sessionId: string, messageId: string, regenerate = false) {
  return apiPost<{ data: MessageSuggestionSet }>(
    `/api/v1/sessions/${sessionId}/messages/${messageId}/suggestions`,
    { regenerate },
  );
}

export function getMessageSuggestions(sessionId: string, messageId: string) {
  return apiGet<{ data: MessageSuggestionSet }>(
    `/api/v1/sessions/${sessionId}/messages/${messageId}/suggestions`,
  );
}

export function recordMessageSuggestionEvent(
  sessionId: string,
  suggestionSetId: string,
  eventType: "impression" | "click" | "dismiss",
  questionId = "",
) {
  return apiPost(`/api/v1/sessions/${sessionId}/suggestion-events`, {
    suggestion_set_id: suggestionSetId,
    question_id: questionId,
    event_type: eventType,
  });
}

// ---- chat history KB (tenant-scoped) ----------------------------------------

/* knowledge_base_id is auto-managed by the backend; frontend only sets the
 * other fields. */
export interface ChatHistoryConfig {
  enabled: boolean;
  embedding_model_id: string;
  knowledge_base_id?: string; // read-only, auto-managed
}

export interface ChatHistoryKBStats {
  enabled: boolean;
  embedding_model_id?: string;
  knowledge_base_id?: string;
  knowledge_base_name?: string;
  indexed_message_count: number;
  has_indexed_messages: boolean;
}

export interface MessageSearchRequest {
  query: string;
  mode?: "keyword" | "vector" | "hybrid";
  limit?: number;
  session_ids?: string[];
}

export interface MessageSearchGroupItem {
  request_id: string;
  session_id: string;
  session_title: string;
  query_content: string;
  answer_content: string;
  score: number;
  match_type: string;
  created_at: string;
}

export interface MessageSearchResult {
  items: MessageSearchGroupItem[];
  total: number;
}

export function getTenantChatHistoryConfig() {
  return apiGet("/api/v1/tenants/kv/chat-history-config");
}

export function updateTenantChatHistoryConfig(config: ChatHistoryConfig) {
  return apiPut("/api/v1/tenants/kv/chat-history-config", config);
}

export function getChatHistoryKBStats() {
  return apiGet("/api/v1/messages/chat-history-stats");
}

/** Search messages across all sessions (keyword + vector hybrid search). */
export function searchMessages(data: MessageSearchRequest) {
  return apiPost("/api/v1/messages/search", data);
}
