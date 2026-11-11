/* Validity lookup of legal documents without the chat (GET /legal-lookup):
 * open to members and guests, reads the platform legal catalogue only — no
 * AI, no question or token quota. Guests are rate-limited per IP. */
import { apiDel, apiGet } from "@/lib/api-client";
import type { LegalStatusKind } from "@/lib/legal-status";

export interface LegalValidityRef {
  id: string;
  document_number: string;
  title: string;
  doc_type: string;
  /* Dates are YYYY-MM-DD, "" when the source gives none. */
  issued_on: string;
  effective_on: string;
  status: LegalStatusKind;
  /* On a related document: what it did to the match. */
  relation?: string;
}

export interface LegalKnowledgeRef {
  knowledge_id: string;
  knowledge_base_id: string;
  knowledge_base_name: string;
  title: string;
}

export interface LegalDerivedValidity {
  status_text: string;
  expired_on: string;
  relation: string;
  by: LegalValidityRef;
}

export interface LegalValidityMatch extends LegalValidityRef {
  issuer: string;
  /* The source's own wording; `status` is its kind after reconciling. */
  status_text: string;
  expired_on: string;
  derived?: LegalDerivedValidity | null;
  source: string;
  source_label: string;
  source_updated_on: string;
  unverified: boolean;
  unverified_note?: string;
  replaced_by: LegalValidityRef[] | null;
  replaced_by_total: number;
  ended_by: LegalValidityRef[] | null;
  ended_by_total: number;
  amended_by: LegalValidityRef[] | null;
  amended_by_total: number;
  knowledge: LegalKnowledgeRef[] | null;
}

export interface LegalValidityLookup {
  query: string;
  by: "number" | "title" | "";
  matches: LegalValidityMatch[];
  /* More documents match than `matches` lists. */
  more: boolean;
  /* The số hiệu asked for when the catalogue lacks it: `matches` then only
   * mention it in their title. */
  number_missing?: string;
  /* Date (YYYY-MM-DD) of the newest vbpl.vn snapshot, "" when unknown. */
  catalog_snapshot_date: string;
}

/** Look a document up by số hiệu or title words. A guest sends no
 * organization (a stored one would be refused as not theirs). */
export async function lookupLegalValidity(
  q: string,
  opts: { isGuest?: boolean; signal?: AbortSignal } = {},
): Promise<LegalValidityLookup> {
  const res = await apiGet<{ success: boolean; data: LegalValidityLookup }>(
    `/api/v1/legal-lookup?q=${encodeURIComponent(q)}`,
    { skipTenant: opts.isGuest === true, signal: opts.signal },
  );
  return res.data;
}

/* A lookup that found nothing (or only documents mentioning a missing số
 * hiệu), per normalized query — the SuperAdmin list of catalogue gaps. */
export interface LegalLookupMiss {
  id: number;
  query: string;
  sample: string;
  number: string;
  /* not_found: nothing matched; number_missing: the số hiệu is missing but
   * titles mention it. */
  reason: "not_found" | "number_missing";
  count: number;
  first_at: string;
  last_at: string;
}

export function listLegalLookupMisses(params: {
  sort: "count" | "recent";
  page: number;
  pageSize: number;
}): Promise<{ success: boolean; data: LegalLookupMiss[]; total: number }> {
  const q = new URLSearchParams({ sort: params.sort, page: String(params.page), page_size: String(params.pageSize) });
  return apiGet(`/api/v1/system/legal-lookup-misses?${q.toString()}`);
}

export function deleteLegalLookupMiss(id: number): Promise<{ success: boolean }> {
  return apiDel(`/api/v1/system/legal-lookup-misses/${id}`);
}
