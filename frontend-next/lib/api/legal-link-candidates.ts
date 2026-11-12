/* SuperAdmin review of provision links pushed by sources other than
 * vbpl.vn (POST /api/v1/system/legal-documents/push, spec
 * docs/legal-document-feed-spec.md §9). An approved candidate is copied
 * into legal_provision_links; nothing pushed reaches answers before that.
 * Every route is SuperAdmin-only; the server enforces it. */
import { apiGet, apiPost, apiPut } from "@/lib/api-client";

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
  /** The legal document request of B's number while B is not in the catalogue. */
  amender_request?: { id: string; document_number: string; status: string } | null;
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

/** One amended document A with candidates, and their counts. */
export interface LegalLinkTargetGroup {
  target_doc_id: string;
  document?: LegalLinkDoc | null;
  /** A public knowledge base holds A's full text. */
  held: boolean;
  knowledge_id?: string;
  /** Pending, amending document known: a person can decide them. */
  pending_approvable: number;
  /** Pending while the amending document is not in the catalogue. */
  pending_blocked: number;
  approved: number;
  rejected: number;
  auto: number;
}

export function listLegalLinkDocuments(): Promise<{
  success: boolean;
  data: { items: LegalLinkTargetGroup[]; totals: { pending_approvable: number; pending_blocked: number } };
}> {
  return apiGet("/api/v1/system/legal-link-candidates/documents");
}

export type LegalProvisionTextReason = "unknown_doc" | "not_held" | "no_path" | "appendix" | "not_found";

export interface LegalProvisionText {
  doc_id: string;
  path: string;
  label: string;
  document?: LegalLinkDoc | null;
  held: boolean;
  found: boolean;
  text: string;
  whole_article?: boolean;
  truncated?: boolean;
  knowledge_id?: string;
  knowledge_base_id?: string;
  reason?: LegalProvisionTextReason;
}

export function getLegalProvisionText(docId: string, path: string): Promise<{ success: boolean; data: LegalProvisionText }> {
  const q = new URLSearchParams({ doc_id: docId, path });
  return apiGet(`/api/v1/system/legal-link-candidates/provision-text?${q.toString()}`);
}

export interface LegalHeldProvisionList {
  doc_id: string;
  held: boolean;
  knowledge_id?: string;
  provisions: { path: string; label: string }[];
}

export function listLegalHeldProvisions(docId: string): Promise<{ success: boolean; data: LegalHeldProvisionList }> {
  return apiGet(`/api/v1/system/legal-link-candidates/provisions?doc_id=${encodeURIComponent(docId)}`);
}

export interface LegalLinkRecheckResult {
  checked: number;
  resolved: number;
  rematch_checked: number;
  rematched: number;
  matched: number;
  auto_approved: number;
  duplicates: number;
  still_missing: number;
  ambiguous: number;
}

export function recheckLegalLinkCandidates(targetDocId?: string): Promise<{ success: boolean; data: LegalLinkRecheckResult }> {
  return apiPost("/api/v1/system/legal-link-candidates/recheck", targetDocId ? { target_doc_id: targetDocId } : {});
}

/** At most this many ids per batch (the server refuses more). */
export const LEGAL_LINK_BATCH_MAX = 200;

export function decideLegalLinkCandidates(body: {
  ids: number[];
  action: "approve" | "reject";
  note?: string;
}): Promise<{ success: boolean; data: { done: number[]; failed: { id: number; error: string }[] } }> {
  return apiPost("/api/v1/system/legal-link-candidates/decide-batch", body);
}

/** Search hits in the amending document for a link that names no provision
 * of it: passages mentioning the amended document's number (and, unless
 * weak, the Điều the link is on). Not the linked provision. */
export interface LegalLinkMentions {
  candidate_id: number;
  amender_doc_id: string;
  held: boolean;
  target_number: string;
  target_article?: string;
  passages: { chunk_id: string; article?: string; text: string; weak: boolean }[];
  weak: boolean;
  reason?: "no_amender" | "not_held" | "no_target_number";
}

export function getLegalLinkMentions(candidateId: number): Promise<{ success: boolean; data: LegalLinkMentions }> {
  return apiGet(`/api/v1/system/legal-link-candidates/mentions?candidate_id=${candidateId}`);
}
