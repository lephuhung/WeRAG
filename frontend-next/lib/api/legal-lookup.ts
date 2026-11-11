/* Validity lookup of legal documents without the chat (GET /legal-lookup):
 * open to members and guests, reads the platform legal catalogue only — no
 * AI, no question or token quota. Guests are rate-limited per IP. */
import { apiGet } from "@/lib/api-client";
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
