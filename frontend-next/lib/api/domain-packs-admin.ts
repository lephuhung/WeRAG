/* SuperAdmin view of the domain packs shipped in this build: their cited
 * documents ("Văn bản trích dẫn") inspected against the legal catalogue,
 * their formulas ("Công thức") and the agents using them. Reading is
 * side-effect free; only reconcile records legal document requests.
 * Backend: /api/v1/system/domain-packs (+ /:name, /:name/reconcile),
 * SuperAdmin-only (the server enforces it). Mirrors
 * internal/types/domain_pack_reconcile.go. */
import { apiGet, apiPost } from "@/lib/api-client";

export interface DomainPackSummary {
  name: string;
  description: string;
  version: string;
  /** YYYY-MM-DD */
  as_of: string;
  /** YYYY-MM-DD */
  review_by: string;
  overdue: boolean;
  domain: string;
  licence: string;
  attribution: string;
  origin: string;
  required_tools: string[];
  document_count: number;
  formula_count: number;
  agent_count: number;
}

export interface DomainPackDraft {
  title: string;
  version: string;
  /** YYYY-MM-DD; absent when the source does not know which draft it is. */
  date?: string;
}

export interface DomainPackRequestRef {
  id: string;
  status: string;
  /** true: recorded by this reconcile; false: an existing request. */
  created: boolean;
}

/** One legal_documents row as the inspection reports it. */
export interface DomainPackCatalogueDocument {
  id: string;
  document_number: string;
  title: string;
  doc_type: string;
  issuer: string;
  status: string;
  effective_on?: string;
  expired_on?: string;
  status_note?: string;
  held_in_kb: boolean;
  request?: DomainPackRequestRef | null;
}

export type DomainPackLookup = "found" | "missing" | "ambiguous" | "";

export interface DomainPackDocumentView {
  number: string;
  title?: string;
  used_for: string;
  articles: string[];
  cited_in: string[];
  formulas: string[];
  lookup: DomainPackLookup;
  documents: DomainPackCatalogueDocument[];
  warning: boolean;
}

export interface DomainPackFormulaVar {
  name: string;
  description: string;
  unit: string;
  source: "input" | "kb" | string;
}

export interface DomainPackFormulaBasis {
  number: string;
  article?: string;
  status: "ok" | "warning" | string;
  note?: string;
}

export interface DomainPackFormulaSample {
  inputs: Record<string, number>;
  expected: Record<string, number>;
}

export interface DomainPackFormulaView {
  id: string;
  name: string;
  expression: string;
  tool: string;
  variables: DomainPackFormulaVar[];
  basis: DomainPackFormulaBasis[];
  examples: DomainPackFormulaSample[];
  warning: boolean;
}

export interface DomainPackAgent {
  id: string;
  name: string;
  tenant_id: number;
  tenant_name: string;
}

export interface DomainPackError {
  number: string;
  error: string;
}

export interface DomainPackDetail extends DomainPackSummary {
  answer_footer: string;
  draft_documents: DomainPackDraft[];
  documents: DomainPackDocumentView[];
  formulas: DomainPackFormulaView[];
  agents: DomainPackAgent[];
  errors: DomainPackError[];
}

export interface DomainPackReconcileRef {
  number: string;
  title?: string;
  used_for: string;
}

export interface DomainPackReconcileResult {
  pack: string;
  version: string;
  found: (DomainPackReconcileRef & { document: DomainPackCatalogueDocument })[];
  missing: (DomainPackReconcileRef & { request?: DomainPackRequestRef | null })[];
  ambiguous: (DomainPackReconcileRef & { documents: DomainPackCatalogueDocument[] })[];
  errors: DomainPackError[];
}

export function listDomainPacks(): Promise<{ success: boolean; data: DomainPackSummary[] }> {
  return apiGet(`/api/v1/system/domain-packs`);
}

export function getDomainPack(name: string): Promise<{ success: boolean; data: DomainPackDetail }> {
  return apiGet(`/api/v1/system/domain-packs/${encodeURIComponent(name)}`);
}

/** Checks the catalogue and records requests for texts no public KB holds. */
export function reconcileDomainPack(name: string): Promise<{ success: boolean; data: DomainPackReconcileResult }> {
  return apiPost(`/api/v1/system/domain-packs/${encodeURIComponent(name)}/reconcile`, {});
}
