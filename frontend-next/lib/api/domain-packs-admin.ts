/* SuperAdmin view of the domain packs shipped in this build: their cited
 * documents ("Văn bản trích dẫn") inspected against the legal catalogue,
 * their formulas ("Công thức") and the agents using them. Reading is
 * side-effect free; only reconcile records legal document requests.
 * A SuperAdmin may edit a formula's wording (PUT …/formulas/:id, DELETE
 * restores formulas.yaml); the calculation stays Go code.
 * Backend: /api/v1/system/domain-packs (+ /:name, /:name/reconcile,
 * /:name/formulas/:id),
 * SuperAdmin-only (the server enforces it). Mirrors
 * internal/types/domain_pack_reconcile.go. */
import { ApiError, apiDel, apiGet, apiPost, apiPut } from "@/lib/api-client";

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
  // a SuperAdmin's edit replaces the wording of formulas.yaml
  edited?: boolean;
  edited_by?: string;
  edited_at?: string;
  shipped?: DomainPackFormulaEdit;
  // an edit is stored but this build refuses it; formulas.yaml applies
  edit_not_applied?: boolean;
}

/* The editable wording of a formula. Variable names are fixed (the Go
 * implementation is bound to them); every example must be reproduced by
 * that implementation or the server refuses the edit. */
export interface DomainPackFormulaEdit {
  name: string;
  expression: string;
  variables: DomainPackFormulaVar[];
  basis: { number: string; article?: string }[];
  examples: DomainPackFormulaSample[];
}

export interface DomainPackAgent {
  id: string;
  name: string;
  tenant_id: number;
  tenant_name: string;
  // "Tổ chức áp dụng"; [] = every Tổ chức
  applies_to_tenant_ids?: number[];
  applies_to?: { tenant_id: number; tenant_name: string }[];
  // "Cách phân phối"; the list above only counts for internal
  distribution?: "basic" | "addon" | "internal";
  // the agent's row in the add-on catalogue (null = not listed) and the
  // current subscriptions holding the add-on
  addon_price_vnd?: number | null;
  addon_active?: boolean | null;
  addon_holders?: number;
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

const formulaPath = (name: string, id: string) =>
  `/api/v1/system/domain-packs/${encodeURIComponent(name)}/formulas/${encodeURIComponent(id)}`;

/** Saves the edit of a formula; returns the pack detail. */
export function updateDomainPackFormula(
  name: string,
  id: string,
  edit: DomainPackFormulaEdit,
): Promise<{ success: boolean; data: DomainPackDetail }> {
  return apiPut(formulaPath(name, id), edit);
}

/** Drops the edit: formulas.yaml applies again; returns the pack detail. */
export function resetDomainPackFormula(name: string, id: string): Promise<{ success: boolean; data: DomainPackDetail }> {
  return apiDel(formulaPath(name, id));
}

/** The server's list of reasons an edit was refused (error.details). */
export function formulaEditProblems(e: unknown): string[] {
  if (!(e instanceof ApiError) || !e.payload || typeof e.payload !== "object") return [];
  const details = (e.payload as { error?: { details?: unknown } }).error?.details;
  return Array.isArray(details) ? details.filter((d): d is string => typeof d === "string") : [];
}
