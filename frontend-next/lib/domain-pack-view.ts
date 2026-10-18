/* Pure helpers of the SuperAdmin domain pack pages (no imports with side
 * effects, so node --test can load them). */
import type {
  DomainPackCatalogueDocument,
  DomainPackDocumentView,
  DomainPackReconcileResult,
  DomainPackRequestRef,
} from "./api/domain-packs-admin.ts";

/** The pack a legal document request came from: detail.origin "pack:<name>". */
export function packOriginName(detail: Record<string, unknown> | null | undefined): string {
  const origin = detail?.origin;
  if (typeof origin !== "string" || !origin.startsWith("pack:")) return "";
  return origin.slice("pack:".length).trim();
}

export interface ReconcileRequestRow {
  number: string;
  title: string;
  request: DomainPackRequestRef;
}

/** The requests a reconcile reported, split into recorded now and existing. */
export function reconcileRequests(result: DomainPackReconcileResult): {
  created: ReconcileRequestRow[];
  existing: ReconcileRequestRow[];
} {
  const rows: ReconcileRequestRow[] = [];
  const add = (number: string, title: string, request?: DomainPackRequestRef | null) => {
    if (request?.id) rows.push({ number, title, request });
  };
  for (const m of result.missing ?? []) add(m.number, m.title ?? "", m.request);
  for (const f of result.found ?? []) add(f.number, f.document?.title || f.title || "", f.document?.request);
  for (const a of result.ambiguous ?? []) {
    for (const d of a.documents ?? []) add(a.number, d.title || a.title || "", d.request);
  }
  return { created: rows.filter((r) => r.request.created), existing: rows.filter((r) => !r.request.created) };
}

/** Rows to show: all, or only those needing a check. */
export function visibleDocuments(docs: DomainPackDocumentView[], onlyWarnings: boolean): DomainPackDocumentView[] {
  return onlyWarnings ? docs.filter((d) => d.warning) : docs;
}

/** The title to show: the pack's, else the catalogue row's. */
export function documentTitle(doc: DomainPackDocumentView): string {
  return doc.title || doc.documents.find((d) => d.title)?.title || "";
}

/** True when some catalogue row holds the text in a public KB. */
export function heldInKB(rows: DomainPackCatalogueDocument[]): boolean {
  return rows.some((r) => r.held_in_kb);
}

/** An example value: thousands with dots for amounts, up to 6 decimals with
 * a comma otherwise (1200000000 → "1.200.000.000", 0.075 → "0,075"). */
export function formatFormulaValue(v: number): string {
  if (!Number.isFinite(v)) return String(v);
  if (Number.isInteger(v)) {
    const sign = v < 0 ? "-" : "";
    return sign + String(Math.abs(v)).replace(/\B(?=(\d{3})+(?!\d))/g, ".");
  }
  return String(Number(v.toFixed(6))).replace(".", ",");
}
