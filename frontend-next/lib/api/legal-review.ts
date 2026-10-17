/* SuperAdmin review of the legal identity (số hiệu, dates, validity) of
 * documents in platform public KBs. Every route requires an explicit human
 * SuperAdmin; the server enforces it, the UI only hides the page. */
import { apiGet, apiPost, apiPut } from "@/lib/api-client";

export type LegalReviewStatus = "pending" | "approved" | "rejected";
export type LegalReviewFilter = LegalReviewStatus | "alerts" | "source_changed";

export type LegalRelation =
  | "replaces"
  | "repeals"
  | "expires"
  | "partially_expires"
  | "suspends"
  | "amends"
  | "guides"
  | "interprets"
  | "consolidates"
  | "corrects"
  | "based_on"
  | "references"
  | "related";

export interface LegalDocument {
  id: string;
  source: string;
  document_number: string;
  title: string;
  doc_type: string;
  issuer: string;
  signer: string;
  signer_title: string;
  field: string;
  issued_on: string | null;
  effective_on: string | null;
  expired_on: string | null;
  status: string;
  source_updated_at: string | null;
  reviewed_by: string;
  reviewed_at: string | null;
  /* Portal values that differ from the reviewed row (field → value). */
  source_pending: Record<string, string> | null;
}

export interface LegalDocumentLink {
  knowledge_id: string;
  knowledge_base_id: string;
  legal_document_id: string;
  review_status: LegalReviewStatus;
  reviewed_by: string;
  reviewed_at: string | null;
}

export interface LegalIdentitySuggestion {
  document_number?: string;
  doc_type?: string;
  issuer?: string;
  issued_on?: string; // dd/mm/yyyy as printed
  catalog_matches?: string[];
}

export interface LegalSupersession {
  relation: LegalRelation;
  document: LegalDocument;
  in_kb: boolean;
}

export interface LegalReviewItem {
  link: LegalDocumentLink;
  knowledge_title: string;
  knowledge_base_name: string;
  suggestion?: LegalIdentitySuggestion;
  document?: LegalDocument;
  candidates?: LegalDocument[];
  superseded_by?: LegalSupersession[];
}

/* Reviewer-editable fields. Dates are YYYY-MM-DD; empty clears. */
export interface LegalDocumentFields {
  document_number: string;
  title: string;
  doc_type: string;
  issuer: string;
  signer: string;
  field: string;
  issued_on: string;
  effective_on: string;
  expired_on: string;
  status: string;
}

export interface LegalReviewDecision {
  action: "approve" | "reject";
  /* Omit to create a manual catalogue row from `document`. */
  legal_document_id?: string;
  document?: LegalDocumentFields;
}

export function listLegalReviews(params: {
  status?: LegalReviewFilter;
  /* Only this KB's documents. */
  knowledgeBaseId?: string;
  page?: number;
  pageSize?: number;
}): Promise<{ success: boolean; data: LegalReviewItem[]; total: number }> {
  const q = new URLSearchParams();
  if (params.status) q.set("status", params.status);
  if (params.knowledgeBaseId) q.set("knowledge_base_id", params.knowledgeBaseId);
  if (params.page) q.set("page", String(params.page));
  if (params.pageSize) q.set("page_size", String(params.pageSize));
  const qs = q.toString();
  return apiGet(`/api/v1/system/legal-reviews${qs ? `?${qs}` : ""}`);
}

export function scanLegalReviews(): Promise<{ success: boolean; data: { created: number } }> {
  return apiPost("/api/v1/system/legal-reviews/scan", {});
}

export function decideLegalReview(
  knowledgeId: string,
  body: LegalReviewDecision,
): Promise<{ success: boolean; data: LegalReviewItem }> {
  return apiPut(`/api/v1/system/legal-reviews/${encodeURIComponent(knowledgeId)}`, body);
}

export function searchLegalCatalog(q: string): Promise<{ success: boolean; data: LegalDocument[] }> {
  return apiGet(`/api/v1/system/legal-catalog/search?q=${encodeURIComponent(q)}`);
}

/* One chunk text sent for proofreading — saved content or an unsaved draft. */
export interface LegalSpellcheckItem {
  chunk_id: string;
  content: string;
}

/* `wrong` is copied verbatim from the submitted content. */
export interface LegalSpellcheckFinding {
  chunk_id: string;
  wrong: string;
  correct: string;
  reason: string;
}

/* Proofreads up to 40 chunk texts with the KB's summary model (an LLM call,
 * 10–60 s). Nothing is saved server-side. */
export function spellcheckLegalReview(
  knowledgeId: string,
  items: LegalSpellcheckItem[],
): Promise<{ success: boolean; data: LegalSpellcheckFinding[] }> {
  return apiPost(`/api/v1/system/legal-reviews/${encodeURIComponent(knowledgeId)}/spellcheck`, { items });
}
