/* SuperAdmin queue of "đề nghị bổ sung văn bản": legal documents users asked
 * about that no knowledge base holds the full text of. The chat records a
 * request; the SuperAdmin ingests it from vbpl.vn, marks it added by hand,
 * or rejects it (the note is shown to users who ask again). Every route is
 * SuperAdmin-only; the server enforces it. */
import { apiGet, apiPut } from "@/lib/api-client";

export type LegalRequestStatus = "requested" | "added" | "rejected";
export type LegalRequestFilter = LegalRequestStatus | "all";

/** The catalogue row a request matched (when it is in the vbpl.vn catalogue). */
export interface LegalRequestDocument {
  id: string;
  document_number: string;
  title: string;
  doc_type: string;
  issuer: string;
  status: string;
  issued_on: string | null;
  url: string;
}

export interface LegalRequestItem {
  id: string;
  number_key: string;
  document_number: string;
  doc_type: string;
  title: string;
  legal_document_id: string;
  source: string;
  status: LegalRequestStatus;
  request_count: number;
  first_requested_at: string | null;
  last_requested_at: string | null;
  last_tenant_id: number | string | null;
  last_user_id: string;
  last_session_id: string;
  last_query: string;
  knowledge_id: string;
  knowledge_base_id: string;
  decided_by: string;
  decided_at: string | null;
  decision_note: string;
  detail: unknown;
  created_at: string;
  updated_at: string;
  document?: LegalRequestDocument | null;
  /** The public KB the server would ingest into (empty when it has no guess). */
  suggested_kb_id?: string;
}

export interface LegalRequestCounts {
  requested: number;
  added: number;
  rejected: number;
}

export interface LegalRequestDecision {
  action: "ingest" | "reject" | "added";
  knowledge_base_id?: string;
  knowledge_id?: string;
  note?: string;
}

export function listLegalRequests(params: {
  status?: LegalRequestFilter;
  page?: number;
  pageSize?: number;
}): Promise<{
  success: boolean;
  data: { items: LegalRequestItem[]; total: number; counts: LegalRequestCounts };
}> {
  const q = new URLSearchParams();
  if (params.status) q.set("status", params.status);
  if (params.page) q.set("page", String(params.page));
  if (params.pageSize) q.set("page_size", String(params.pageSize));
  const qs = q.toString();
  return apiGet(`/api/v1/system/legal-requests${qs ? `?${qs}` : ""}`);
}

export function decideLegalRequest(
  id: string,
  body: LegalRequestDecision,
): Promise<{ success: boolean; data: LegalRequestItem }> {
  return apiPut(`/api/v1/system/legal-requests/${encodeURIComponent(id)}`, body);
}

export function listLegalRequestKnowledgeBases(): Promise<{ success: boolean; data: { id: string; name: string }[] }> {
  return apiGet(`/api/v1/system/legal-requests/public-knowledge-bases`);
}
