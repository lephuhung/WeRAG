/* SuperAdmin review of provision links pushed by sources other than
 * vbpl.vn (POST /api/v1/system/legal-documents/push, spec
 * docs/legal-document-feed-spec.md §9). An approved candidate is copied
 * into legal_provision_links; nothing pushed reaches answers before that.
 * Every route is SuperAdmin-only; the server enforces it. */
import { apiGet, apiPut } from "@/lib/api-client";

export type LegalLinkReviewStatus = "pending" | "approved" | "rejected" | "auto";
export type LegalLinkReviewFilter = LegalLinkReviewStatus | "all";
export type LegalLinkMatchStatus = "matched" | "doc_only" | "target_missing" | "unresolved" | "duplicate";

export interface LegalLinkDoc {
  id: string;
  document_number: string;
  title: string;
  doc_type: string;
  status: string;
}

/** A link legal_provision_links already holds on the same provision. */
export interface LegalHeldLink {
  id: number;
  relation_code: number;
  op: string;
  source_doc_id: string;
  source_path: string;
  source: string;
}

export interface LegalLinkCandidate {
  id: number;
  feed: string;
  feed_doc_id: string;
  feed_url: string;
  target_doc_id: string;
  target_element_id: string;
  target_provision_id: string;
  target_level: string;
  target_path: string;
  relation_code: number;
  op: string;
  amender_doc_number: string;
  amender_issue_date: string;
  amender_issuer: string;
  amender_doc_id: string;
  source_provision_id: string;
  source_path: string;
  eff_from: string | null;
  eff_to: string | null;
  note: string;
  match_status: LegalLinkMatchStatus;
  match_reason: string;
  review_status: LegalLinkReviewStatus;
  link_id: number | null;
  reviewed_by: string;
  reviewed_at: string | null;
  created_at: string;
  updated_at: string;
  target?: LegalLinkDoc | null;
  amender?: LegalLinkDoc | null;
  other_links: LegalHeldLink[];
}

export type LegalLinkCounts = Partial<Record<LegalLinkReviewStatus, number>>;

export interface LegalLinkDecision {
  action: "approve" | "reject";
  amender_doc_id?: string;
  source_path?: string;
  target_path?: string;
  note?: string;
}

export function listLegalLinkCandidates(params: {
  reviewStatus?: LegalLinkReviewFilter;
  matchStatus?: LegalLinkMatchStatus | "";
  legalDocumentId?: string;
  page?: number;
  pageSize?: number;
}): Promise<{
  success: boolean;
  data: { items: LegalLinkCandidate[]; total: number; counts: LegalLinkCounts };
}> {
  const q = new URLSearchParams();
  if (params.reviewStatus) q.set("review_status", params.reviewStatus);
  if (params.matchStatus) q.set("match_status", params.matchStatus);
  if (params.legalDocumentId) q.set("legal_document_id", params.legalDocumentId);
  if (params.page) q.set("page", String(params.page));
  if (params.pageSize) q.set("page_size", String(params.pageSize));
  const qs = q.toString();
  return apiGet(`/api/v1/system/legal-link-candidates${qs ? `?${qs}` : ""}`);
}

export function decideLegalLinkCandidate(
  id: number,
  body: LegalLinkDecision,
): Promise<{ success: boolean; data: LegalLinkCandidate }> {
  return apiPut(`/api/v1/system/legal-link-candidates/${id}`, body);
}
