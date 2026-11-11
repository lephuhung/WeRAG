/* Labels given on the document assistant's spelling underlines and NĐ30
 * format check reports (GET /document-labels…). Tenant Admin: their own
 * organization; SuperAdmin: every organization, tenant_id filters. */
import { apiDownload, apiGet } from "@/lib/api-client";

export type DocumentLabelKind = "spelling" | "format";

export interface DocumentLabel {
  id: string;
  kind: DocumentLabelKind;
  organization_id: number;
  organization_name?: string;
  created_at: string;
  paragraph: string;
  /** Code-point offsets into paragraph, end exclusive; null when unknown. */
  span_start: number | null;
  span_end: number | null;
  flagged_text: string;
  suggestion: string;
  current?: string;
  verdict: string;
  source: "user" | "verifier" | string;
  check_id: string;
  finding_id?: string;
  finding_title?: string;
  document_type?: string;
  scope?: string;
  note?: string;
  model: string;
}

export interface DocumentLabelFilters {
  kind?: string;
  verdict?: string;
  source?: string;
  model?: string;
  tenant_id?: number;
  /** YYYY-MM-DD (Vietnam time), both inclusive. */
  from?: string;
  to?: string;
}

function params(f: DocumentLabelFilters, extra: Record<string, string | number> = {}): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries({ ...f, ...extra })) {
    if (v !== undefined && v !== "" && v !== 0) q.set(k, String(v));
  }
  const s = q.toString();
  return s ? `?${s}` : "";
}

export function listDocumentLabels(f: DocumentLabelFilters, page: number, pageSize: number) {
  return apiGet<{
    success: boolean;
    data?: { items: DocumentLabel[]; total: number; page: number; page_size: number };
  }>(`/api/v1/document-labels${params(f, { page, page_size: pageSize })}`);
}

export function getDocumentLabelFacets(tenantId?: number) {
  return apiGet<{ success: boolean; data?: { models: string[] } }>(
    `/api/v1/document-labels/facets${params({ tenant_id: tenantId })}`,
  );
}

export function exportDocumentLabels(f: DocumentLabelFilters): Promise<Blob> {
  return apiDownload(`/api/v1/document-labels/export${params(f)}`);
}
