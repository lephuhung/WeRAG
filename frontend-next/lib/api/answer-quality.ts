/* Answer-quality inbox for admins: answers users disliked (thumbs down,
 * with their comment) and questions a gate answered without evidence (the
 * knowledge base lacks it). A Tenant Admin reads their own organization
 * (/api/v1/quality/*); a SuperAdmin every organization
 * (/api/v1/system/quality/*, tenant_id narrows to one). The admin's cause
 * tag and "reviewed" mark never change the user's message. */
import { apiGet, apiPut } from "@/lib/api-client";

export type QualityScope = "tenant" | "platform";
export type QualityKind = "disliked" | "ungrounded";
export type UngroundedReason = "no_evidence" | "legal_document_missing";

export const DISLIKED_CAUSES = ["wrong_basis", "missing_info", "off_topic", "format_error", "other"] as const;
export const UNGROUNDED_CAUSES = ["kb_missing", "retrieval_miss", "out_of_scope", "other"] as const;
export type QualityCause = (typeof DISLIKED_CAUSES)[number] | (typeof UNGROUNDED_CAUSES)[number];

export interface QualityReview {
  cause: QualityCause | "";
  note: string;
  reviewed: boolean;
  reviewed_by: string;
  reviewer_name: string;
  reviewed_at: string | null;
}

export interface QualityRow {
  message_id: string;
  session_id: string;
  created_at: string;
  tenant_id: number;
  tenant_name: string;
  user_id: string;
  user_name: string;
  agent_id: string;
  agent_name: string;
  question: string;
  answer: string;
  answer_truncated?: boolean;
  references: number;
  feedback?: number;
  comment?: string;
  feedback_at?: string | null;
  reason?: UngroundedReason;
  legal_request?: string;
  review?: QualityReview | null;
}

export interface QualityAgent {
  id: string;
  name: string;
}

export interface QualityPage {
  items: QualityRow[];
  total: number;
  agents: QualityAgent[];
}

export interface UngroundedGroup {
  key: string;
  sample: string;
  count: number;
  organizations: number;
  last_at: string;
  reasons: Record<string, number>;
  legal_request?: string;
  message_id: string;
}

export interface UngroundedGroupPage {
  items: UngroundedGroup[];
  total: number;
  rows: number;
  truncated?: boolean;
}

export interface QualityFilter {
  /* YYYY-MM-DD, Vietnam time; `to` is included. */
  from?: string;
  to?: string;
  /* SuperAdmin console only. */
  tenantId?: number;
  agentId?: string;
  hasComment?: boolean;
  reason?: UngroundedReason | "";
  reviewed?: boolean;
  page?: number;
  pageSize?: number;
}

function base(scope: QualityScope): string {
  return scope === "platform" ? "/api/v1/system/quality" : "/api/v1/quality";
}

function query(f: QualityFilter): string {
  const q = new URLSearchParams();
  if (f.from) q.set("from", f.from);
  if (f.to) q.set("to", f.to);
  if (f.tenantId) q.set("tenant_id", String(f.tenantId));
  if (f.agentId) q.set("agent_id", f.agentId);
  if (f.hasComment) q.set("has_comment", "true");
  if (f.reason) q.set("reason", f.reason);
  if (f.reviewed !== undefined) q.set("reviewed", String(f.reviewed));
  if (f.page) q.set("page", String(f.page));
  if (f.pageSize) q.set("page_size", String(f.pageSize));
  const s = q.toString();
  return s ? `?${s}` : "";
}

export async function listDislikedAnswers(scope: QualityScope, f: QualityFilter): Promise<QualityPage> {
  const res = await apiGet<{ success: boolean; data: QualityPage }>(`${base(scope)}/disliked${query(f)}`);
  return res.data;
}

export async function listUngroundedQuestions(scope: QualityScope, f: QualityFilter): Promise<QualityPage> {
  const res = await apiGet<{ success: boolean; data: QualityPage }>(`${base(scope)}/ungrounded${query(f)}`);
  return res.data;
}

export async function groupUngroundedQuestions(scope: QualityScope, f: QualityFilter): Promise<UngroundedGroupPage> {
  const res = await apiGet<{ success: boolean; data: UngroundedGroupPage }>(`${base(scope)}/ungrounded/groups${query(f)}`);
  return res.data;
}

export interface QualityReviewInput {
  kind: QualityKind;
  cause?: QualityCause | "";
  note?: string;
  reviewed?: boolean;
}

export async function saveQualityReview(scope: QualityScope, messageId: string, input: QualityReviewInput): Promise<void> {
  await apiPut(`${base(scope)}/reviews/${encodeURIComponent(messageId)}`, input);
}
