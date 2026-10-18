/* "Văn bản chưa có trong kho tri thức": the chat named a legal document the
 * system has no full text of. The backend answers the turn itself (the
 * message is the assistant text) and sends this card alongside it
 * (display_type legal_document_request) — the catalogue rows it matched,
 * whether a request went to the SuperAdmin, a rejection note, or the
 * document that replaced an expired one. Informational only: nothing to
 * pick. Arrives as a tool result (live) or an agent step (history).
 * Pure helpers here; the component is components/chat/legal-request-card.tsx.
 */

export const LEGAL_DOCUMENT_REQUEST_TYPE = "legal_document_request";

export type LegalRequestCardMode = "requested" | "rejected" | "expired";

export interface LegalRequestCardDocument {
  id: string;
  documentNumber: string;
  title: string;
  docType: string;
  issuer: string;
  status: string;
  /** "YYYY-MM-DD" as sent (may carry a time part). */
  issuedOn: string;
  url: string;
}

export interface LegalRequestCard {
  mode: LegalRequestCardMode;
  reference: string;
  docType: string;
  documents: LegalRequestCardDocument[];
  requestCount: number;
  decisionNote: string;
  replacement: LegalRequestCardDocument | null;
}

function rec(v: unknown): Record<string, unknown> | null {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
}

function str(v: unknown): string {
  return typeof v === "string" ? v : typeof v === "number" ? String(v) : "";
}

function doc(v: unknown): LegalRequestCardDocument | null {
  const d = rec(v);
  if (!d) return null;
  const out: LegalRequestCardDocument = {
    id: str(d.id),
    documentNumber: str(d.document_number),
    title: str(d.title),
    docType: str(d.doc_type),
    issuer: str(d.issuer),
    status: str(d.status),
    issuedOn: str(d.issued_on),
    url: str(d.url),
  };
  return out.documentNumber || out.title ? out : null;
}

/** Reads the card out of tool-result data; null when it is not one. */
export function parseLegalRequestCard(data: unknown): LegalRequestCard | null {
  const d = rec(data);
  if (!d || d.display_type !== LEGAL_DOCUMENT_REQUEST_TYPE) return null;
  const m = str(d.mode);
  const mode: LegalRequestCardMode = m === "rejected" || m === "expired" ? m : "requested";
  const documents: LegalRequestCardDocument[] = [];
  for (const raw of Array.isArray(d.documents) ? d.documents : []) {
    const x = doc(raw);
    if (x) documents.push(x);
  }
  return {
    mode,
    reference: str(d.reference),
    docType: str(d.doc_type),
    documents,
    requestCount: Number(d.request_count) || 0,
    decisionNote: str(d.decision_note).trim(),
    replacement: mode === "expired" ? doc(d.replacement) : null,
  };
}

/** Splits a history/live step list into the card (its last one) and the
 * steps the timeline shows. */
export function takeLegalRequestCard<S extends object>(steps: S[] | undefined): {
  card: LegalRequestCard | null;
  steps: S[] | undefined;
} {
  if (!steps?.length) return { card: null, steps };
  let card: LegalRequestCard | null = null;
  const rest: S[] = [];
  for (const s of steps) {
    const c = parseLegalRequestCard((s as { tool_data?: unknown }).tool_data);
    if (c) card = c;
    else rest.push(s);
  }
  return { card, steps: rest.length === steps.length ? steps : rest.length ? rest : undefined };
}

/** "2022-08-15" (or an ISO timestamp) → "15/08/2022"; date-only values are
 * read as written, not shifted through a time zone. */
export function showIssuedOn(s: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(s.trim());
  return m ? `${m[3]}/${m[2]}/${m[1]}` : s.trim();
}

/** Green badge only for documents still in force. */
export function statusInForce(status: string): boolean {
  return status.trim().toLowerCase().startsWith("còn hiệu lực");
}
