/* Validity status of a legal catalogue document as the UI shows it: the
 * server reduces the source wording to a kind (types.ClassifyLegalStatus);
 * here the kind picks a badge tone and a label. Shared by the validity
 * lookup page; no React, so node --test runs it. */

export type LegalStatusKind =
  | "in_force"
  | "expired"
  | "partly_expired"
  | "not_yet_in_force"
  | "suspended"
  | "unknown";

/** Badge class of a status kind (globals.css badge-*). */
export function legalStatusBadgeClass(kind: string | null | undefined): string {
  switch (kind) {
    case "in_force":
      return "badge-success";
    case "expired":
    case "suspended":
      return "badge-error";
    case "partly_expired":
    case "not_yet_in_force":
      return "badge-warning";
    default:
      return "badge-neutral";
  }
}

/** i18n key of a status kind's label; unknown kinds read "chưa rõ". */
export function legalStatusLabelKey(
  kind: string | null | undefined,
):
  | "llookup.status.in_force"
  | "llookup.status.expired"
  | "llookup.status.partly_expired"
  | "llookup.status.not_yet_in_force"
  | "llookup.status.suspended"
  | "llookup.status.unknown" {
  switch (kind) {
    case "in_force":
    case "expired":
    case "partly_expired":
    case "not_yet_in_force":
    case "suspended":
      return `llookup.status.${kind}`;
    default:
      return "llookup.status.unknown";
  }
}

/** A catalogue date (YYYY-MM-DD, a calendar day with no zone) as
 * dd/mm/yyyy; "" for none or a malformed value. */
export function formatLegalDate(value: string | null | undefined): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(value ?? "");
  return m ? `${m[3]}/${m[2]}/${m[1]}` : "";
}

/** Path of the lookup page, with the query filled in when given. */
export function legalLookupHref(q?: string | null): string {
  const query = (q ?? "").trim();
  return query ? `/platform/legal-lookup?q=${encodeURIComponent(query)}` : "/platform/legal-lookup";
}

/** Path of a knowledge base document page (deep link opens the document). */
export function knowledgeDocHref(kbId: string, knowledgeId: string): string {
  return `/platform/knowledge-bases/${encodeURIComponent(kbId)}?knowledge_id=${encodeURIComponent(knowledgeId)}`;
}

/** A document's short name: "Nghị định 30/2020/NĐ-CP", or the title. */
export function legalDocShortLabel(d: { doc_type: string; document_number: string; title: string }): string {
  const num = d.document_number.trim();
  if (num) return [d.doc_type.trim(), num].filter(Boolean).join(" ");
  return d.title.trim();
}
