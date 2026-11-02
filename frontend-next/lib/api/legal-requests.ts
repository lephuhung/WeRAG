/* SuperAdmin queue of "đề nghị bổ sung văn bản": legal documents users asked
 * about that no knowledge base holds the full text of. The chat records a
 * request; the SuperAdmin ingests it from vbpl.vn, marks it added by hand,
 * or rejects it (the note is shown to users who ask again). Every route is
 * SuperAdmin-only; the server enforces it. */
import { apiGet, apiPost, apiPut } from "@/lib/api-client";
import type { LegalDraftSummary } from "@/lib/api/legal-drafts";

/** fetched: vbpl.vn has only a scanned PDF, fetched into an OCR draft that
 * waits for review; reviewing: the draft is open in the editor. */
export type LegalRequestStatus = "requested" | "fetched" | "reviewing" | "added" | "rejected";
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

/** What vbpl.vn offers for the document (stored by a portal check, or by an
 * ingest refused for lack of full text). */
export interface LegalRequestPortal {
  checked_at: string;
  /** The portal's validity status ("Còn hiệu lực"…). */
  status: string;
  /** HTML full text is published. */
  html: boolean;
  /** The original PDF (a scan), "" when there is none. */
  pdf_file: string;
  /** false: the portal has no document with this id. */
  exists: boolean;
}

export interface LegalRequestDetail {
  portal?: LegalRequestPortal;
  [key: string]: unknown;
}

/** knowledges.parse_status of the linked document ("" when none). */
export type LegalKnowledgeParseStatus = "" | "pending" | "processing" | "completed" | "failed" | string;

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
  detail: LegalRequestDetail | null;
  created_at: string;
  updated_at: string;
  document?: LegalRequestDocument | null;
  /** The public KB the server would ingest into (empty when it has no guess). */
  suggested_kb_id?: string;
  /** Parse status of knowledge_id: an ingested PDF scan is OCRed asynchronously. */
  knowledge_parse_status?: LegalKnowledgeParseStatus;
  /** The OCR draft of the document's scanned PDF, null when none was fetched. */
  draft?: LegalDraftSummary | null;
}

export interface LegalRequestCounts {
  requested: number;
  fetched?: number;
  reviewing?: number;
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

/** Reads what vbpl.vn offers for the request's document and stores it as detail.portal. */
export function checkLegalRequestPortal(id: string): Promise<{ success: boolean; data: LegalRequestItem }> {
  return apiPost(`/api/v1/system/legal-requests/${encodeURIComponent(id)}/portal-check`, {});
}

export function listLegalRequestKnowledgeBases(): Promise<{ success: boolean; data: { id: string; name: string }[] }> {
  return apiGet(`/api/v1/system/legal-requests/public-knowledge-bases`);
}
