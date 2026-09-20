/* Ported from frontend/src/api/memory.ts. */
import { apiDel, apiGet, apiPost, apiPut } from "@/lib/api-client";

// Kinds mirror internal/types/memory.go. profile and preference make up the
// block injected on every turn; fact and task are pulled in only when the
// current question matches them.
export type MemoryKind = "profile" | "preference" | "fact" | "task" | "interest";
export type MemoryStatus = "active" | "superseded" | "archived" | "pending";
export type MemoryOrigin = "explicit" | "extracted" | "manual";

export interface MemoryItem {
  id: string;
  kind: MemoryKind;
  content: string;
  topic: string;
  importance: number;
  origin: MemoryOrigin;
  status: MemoryStatus;
  source_session_id: string;
  source_message_id: string;
  valid_from: string;
  invalid_at: string | null;
  superseded_by: string;
  last_used_at: string | null;
  use_count: number;
  created_at: string;
  updated_at: string;
}

// Already merged server-side — the UI never has to combine a workspace
// switch with a personal one itself.
export interface MemorySettings {
  workspace_enabled: boolean;
  user_enabled: boolean;
  effective: boolean;
  write_mode: string;
  item_count: number;
  max_items: number;
}

export interface MemoryConfig {
  enabled: boolean;
  write_mode: "explicit_only" | "auto";
  extract_model_id: string;
  max_items: number;
  /** Debounce before distillation runs, in seconds. */
  extract_delay_seconds: number;
  /** Floor between two distillation runs for one person, in seconds. */
  extract_min_interval_seconds: number;
  /** Workspace-specific rules appended to the distillation prompt. */
  extract_instructions: string;
  /** Conversations touching a topic before it becomes an interest. */
  interest_threshold: number;
  /** Whether memory may shape retrieval, not only the answer prompt. */
  retrieval_conditioning: boolean;
  /** Model scoring memory against a question. Blank = lexical matching only. */
  embedding_model_id: string;
  /** Whether recall also matches on meaning, not only wording. */
  vector_recall: boolean;
}

// ---- personal memory (server derives the owner from the principal) ------------

export function getMemorySettings() {
  return apiGet<{ success: boolean; data: MemorySettings }>("/api/v1/memory/settings");
}

export function updateMemoryEnabled(enabled: boolean) {
  return apiPut<{ success: boolean; data: MemorySettings }>("/api/v1/memory/settings", { enabled });
}

export function listMemoryItems(
  params: { status?: MemoryStatus; limit?: number; offset?: number } = {},
) {
  const query = new URLSearchParams();
  if (params.status) query.set("status", params.status);
  if (params.limit != null) query.set("limit", String(params.limit));
  if (params.offset != null) query.set("offset", String(params.offset));
  const suffix = query.toString() ? `?${query.toString()}` : "";
  return apiGet<{ success: boolean; data: MemoryItem[]; total: number }>(
    `/api/v1/memory/items${suffix}`,
  );
}

/** Accept a memory the system inferred, so it starts being used. */
export function confirmMemoryItem(id: string) {
  return apiPost<{ success: boolean; data: MemoryItem }>(`/api/v1/memory/items/${id}/confirm`, {});
}

/** Decline an inference. The refusal is remembered — not re-proposed. */
export function rejectMemoryItem(id: string) {
  return apiPost<{ success: boolean }>(`/api/v1/memory/items/${id}/reject`, {});
}

export function createMemoryItem(payload: {
  kind: MemoryKind;
  content: string;
  importance?: number;
}) {
  return apiPost<{ success: boolean; data: MemoryItem }>("/api/v1/memory/items", payload);
}

export function updateMemoryItem(id: string, payload: { content: string; importance: number }) {
  return apiPut<{ success: boolean; data: MemoryItem }>(
    `/api/v1/memory/items/${encodeURIComponent(id)}`,
    payload,
  );
}

export function deleteMemoryItem(id: string) {
  return apiDel<{ success: boolean }>(`/api/v1/memory/items/${encodeURIComponent(id)}`);
}

export function clearMemoryItems() {
  return apiDel<{ success: boolean; removed: number }>("/api/v1/memory/items");
}

export function exportMemoryItems() {
  return apiGet<{ success: boolean; total: number; data: MemoryItem[] }>("/api/v1/memory/export");
}

/** Why a review changed nothing. Empty when it did change something. */
export type MemoryConsolidationSkip =
  | "too_few_items"
  | "no_candidates"
  | "model_unavailable"
  | "model_declined";

export interface MemoryConsolidationResult {
  merged: number;
  demoted: number;
  expired: number;
  reviewed: number;
  candidates: number;
  skipped?: MemoryConsolidationSkip;
}

/** Merge near-duplicates now, without waiting for the daily distillation pass. */
export function consolidateMemory() {
  return apiPost<{ success: boolean; data: MemoryConsolidationResult }>(
    "/api/v1/memory/consolidate",
    {},
  );
}

export interface MemoryTopic {
  id: string;
  topic: string;
  aliases: string[];
  hits: number;
  threshold: number;
  last_seen_at: string;
}

export function listMemoryTopics(params: { limit?: number; offset?: number } = {}) {
  const query = new URLSearchParams();
  if (params.limit != null) query.set("limit", String(params.limit));
  if (params.offset != null) query.set("offset", String(params.offset));
  const suffix = query.toString() ? `?${query.toString()}` : "";
  return apiGet<{ success: boolean; data: MemoryTopic[]; total: number }>(
    `/api/v1/memory/topics${suffix}`,
  );
}

/** Promote a counted topic into a long-term interest without waiting. */
export function promoteMemoryTopic(id: string) {
  return apiPost<{ success: boolean; data: MemoryItem }>(
    `/api/v1/memory/topics/${encodeURIComponent(id)}/promote`,
    {},
  );
}

/** Stop tracking a topic; the refusal is remembered so it isn't auto-promoted. */
export function deleteMemoryTopic(id: string) {
  return apiDel<{ success: boolean }>(`/api/v1/memory/topics/${encodeURIComponent(id)}`);
}

export interface MemoryDoc {
  id: string;
  knowledge_id: string;
  knowledge_base_id: string;
  title: string;
  hits: number;
  last_used_at: string;
}

export function listMemoryDocuments(params: { limit?: number; offset?: number } = {}) {
  const query = new URLSearchParams();
  if (params.limit != null) query.set("limit", String(params.limit));
  if (params.offset != null) query.set("offset", String(params.offset));
  const suffix = query.toString() ? `?${query.toString()}` : "";
  return apiGet<{ success: boolean; data: MemoryDoc[]; total: number }>(
    `/api/v1/memory/documents${suffix}`,
  );
}

/** Stop using one document as a personal retrieval signal. */
export function deleteMemoryDocument(id: string) {
  return apiDel<{ success: boolean }>(`/api/v1/memory/documents/${encodeURIComponent(id)}`);
}

// ---- workspace configuration (tenant KV) -------------------------------------------

export function getTenantMemoryConfig() {
  return apiGet<{ success: boolean; data: MemoryConfig }>("/api/v1/tenants/kv/memory-config");
}

export function updateTenantMemoryConfig(config: MemoryConfig) {
  return apiPut<{ success: boolean; data: MemoryConfig }>(
    "/api/v1/tenants/kv/memory-config",
    config,
  );
}
