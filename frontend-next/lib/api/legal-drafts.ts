/* SuperAdmin review of OCR drafts: a legal document vbpl.vn publishes only
 * as a scanned PDF is fetched into a draft, OCRed, edited in the embedded
 * editor (a hidden document-assistant session of the platform
 * organization) and, once its structure check passes, approved: the server
 * converts it to vbpl-style HTML and ingests it. Every route is
 * SuperAdmin-only; the server enforces it. Backend:
 * /api/v1/system/legal-requests/:id/fetch and /api/v1/system/legal-drafts. */
import { apiDel, apiGet, apiPost } from "@/lib/api-client";
import type { DocumentEditorBootstrap } from "@/lib/api/document-workspace";
import type { LegalRequestItem } from "@/lib/api/legal-requests";

export type LegalDraftStatus = "fetching" | "ocr" | "ready" | "reviewing" | "approved" | "failed";
export type LegalDraftFilter = LegalDraftStatus | "all";

export interface LegalDraft {
  id: string;
  request_id: string;
  legal_document_id: string;
  document_number: string;
  doc_type: string;
  title: string;
  pdf_file_name: string;
  pdf_bytes: number;
  /** Raw OCR output (GET of one draft only). */
  ocr_markdown?: string;
  ocr_pages: number;
  ocr_engine: string;
  status: LegalDraftStatus;
  error: string;
  session_id: string;
  attachment_id: string;
  workspace_id: string;
  /** The last structure report, when one was made. */
  structure?: LegalDraftStructure | null;
  approved_knowledge_id: string;
  approved_kb_id: string;
  created_by: string;
  reviewed_by: string;
  approved_by: string;
  approved_at: string | null;
  created_at: string;
  updated_at: string;
}

/** A draft as a request row shows it. */
export interface LegalDraftSummary {
  id: string;
  status: LegalDraftStatus;
  ocr_pages: number;
  error: string;
}

/** The request a draft was fetched for, as stored (no catalogue row or KB
 * suggestion). */
export type LegalDraftRequest = Omit<LegalRequestItem, "document" | "suggested_kb_id" | "knowledge_parse_status" | "draft">;

/** One row of the draft list (and the GET of one draft): the draft and its
 * request (null when none). */
export type LegalDraftItem = LegalDraft & { request: LegalDraftRequest | null };

export type LegalDraftHeadingKind = "phan" | "chuong" | "muc" | "dieu";

export interface LegalDraftHeading {
  /** 1 Phần, 2 Chương, 3 Mục, 4 Điều. */
  level: number;
  kind: LegalDraftHeadingKind | string;
  number: string;
  title: string;
  /** Paragraph index in the document. */
  paragraph: number;
  /** Index in headings of the enclosing heading, -1 at the top. */
  parent: number;
}

export interface LegalDraftArticle {
  number: string;
  title: string;
  paragraph: number;
}

export interface LegalDraftSubdivisions {
  khoan_candidates: number;
  khoan_accepted: number;
  diem_candidates: number;
  diem_accepted: number;
  rejected: number;
}

export type LegalDraftWarningCode =
  | "too_few_articles"
  | "article_gap"
  | "article_duplicate"
  | "article_order"
  | "heading_without_title"
  | "subdivision_rejected"
  | "page_marker"
  | "ocr_suspect";

export interface LegalDraftWarning {
  code: LegalDraftWarningCode | string;
  /** "error" blocks approval, "warning", "info". */
  severity: "error" | "warning" | "info" | string;
  message: string;
  /** -1 when the finding has no position. */
  paragraph: number;
  paragraphs?: number[];
  blocking: boolean;
}

export interface LegalDraftStructure {
  checked_at: string;
  revision: number;
  paragraphs: number;
  headings: LegalDraftHeading[] | null;
  articles: LegalDraftArticle[] | null;
  subdivisions: LegalDraftSubdivisions;
  page_markers: number;
  warnings: LegalDraftWarning[] | null;
  blocking: number;
  ready_to_approve: boolean;
}

export interface LegalDraftOpenResult {
  draft: LegalDraft;
  /** Null when the editor (ONLYOFFICE) is not configured. */
  editor: DocumentEditorBootstrap | null;
  session_id: string;
  document_id: string;
  /** The organization the session routes are called in (X-Tenant-ID). */
  tenant_id: number;
  editor_key: string;
  revision: number;
  /** A 2-hour link to the original scan ("/r/<token>", same origin). */
  pdf_url: string;
}

export interface LegalDraftApproveResult {
  draft: LegalDraft;
  request: LegalRequestItem | null;
  /** "created", "replaced" or "unchanged". */
  result: string;
  knowledge_id: string;
  articles: number;
}

type Envelope<T> = { success: boolean; data: T };

const draftPath = (id: string) => `/api/v1/system/legal-drafts/${encodeURIComponent(id)}`;

/** Draft states still being fetched or OCRed (poll until they change). */
export function legalDraftBusy(status: string | undefined | null): boolean {
  return status === "fetching" || status === "ocr";
}

/** Draft states the editor can open. */
export function legalDraftEditable(status: string | undefined | null): boolean {
  return status === "ready" || status === "reviewing";
}

/** Fetches the request's scanned PDF into a draft (an existing one comes back
 * as it is; a failed one is fetched again). Up to 20 pages come back
 * "ready", longer scans "ocr" (poll getLegalDraft). */
export function fetchDraftForRequest(requestId: string): Promise<Envelope<LegalDraft>> {
  return apiPost(`/api/v1/system/legal-requests/${encodeURIComponent(requestId)}/fetch`, {}, { timeoutMs: 300_000 });
}

export function listLegalDrafts(params: {
  status?: LegalDraftFilter;
  page?: number;
  pageSize?: number;
}): Promise<Envelope<{ items: LegalDraftItem[]; total: number }>> {
  const q = new URLSearchParams();
  if (params.status) q.set("status", params.status);
  if (params.page) q.set("page", String(params.page));
  if (params.pageSize) q.set("page_size", String(params.pageSize));
  const qs = q.toString();
  return apiGet(`/api/v1/system/legal-drafts${qs ? `?${qs}` : ""}`);
}

export function getLegalDraft(id: string): Promise<Envelope<LegalDraftItem>> {
  return apiGet(draftPath(id));
}

/** Opens the draft in the editor (the first call builds its .docx). 409
 * while it is still fetched/OCRed or when it failed. */
export function openLegalDraft(id: string): Promise<Envelope<LegalDraftOpenResult>> {
  return apiPost(`${draftPath(id)}/open`, {}, { timeoutMs: 120_000 });
}

/** Checks the structure of the edited text; the server asks the editor to
 * save first (up to 15 s). */
export function legalDraftStructure(id: string): Promise<Envelope<LegalDraftStructure>> {
  return apiGet(`${draftPath(id)}/structure`, { timeoutMs: 90_000 });
}

/** Converts the draft to HTML and ingests it into the knowledge base. 400
 * (with the reasons) when the structure check does not pass. */
export function approveLegalDraft(
  id: string,
  knowledgeBaseId: string,
): Promise<Envelope<LegalDraftApproveResult>> {
  return apiPost(`${draftPath(id)}/approve`, { knowledge_base_id: knowledgeBaseId }, { timeoutMs: 300_000 });
}

/** Discards a draft (409 once it is approved). */
export function discardLegalDraft(id: string): Promise<{ success: boolean }> {
  return apiDel(draftPath(id));
}

/** Path of the review page of a draft; `kb` preselects the knowledge base
 * to approve into (the request queue knows the suggested one). */
export function legalDraftHref(id: string, kb?: string): string {
  const path = `/platform/admin/legal-drafts/${encodeURIComponent(id)}`;
  return kb ? `${path}?kb=${encodeURIComponent(kb)}` : path;
}

export { headingLead, locateParagraph, pickDraftKnowledgeBase } from "@/lib/legal-draft-nav";
