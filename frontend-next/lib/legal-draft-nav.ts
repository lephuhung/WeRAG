/* Pure helpers of the legal OCR draft review page (no imports, so the
 * node test runner can load them): the knowledge base to approve into and
 * where a structure-report position is among the editor's paragraphs. */

/** The knowledge base to approve into: the given one when listed, else the
 * public KB named for the document type ("VBPL – Nghị định"), else the
 * first. Mirrors the server's suggestion for requests. */
/** The public KB of the Party's documents (server: LegalPartyKBName). */
export const PARTY_KB_NAME = "Văn bản của Đảng";

const PARTY_NUMBER_RE = /^\d{1,5}[a-zA-Z]?-(NQ|QĐ|QĐi|CT|KL|QC|HD|TB|KH|CTr|CV|BC|TTr|ĐA|CĐ)\/([\p{L}\d-]*TW|TU|HU|QU|ĐU|ĐUK|BCĐ|TGV|CQTTBCĐ|BTGDV)$/iu;

/** A draft of a Party document: pushed by the dangcongsan feed, or numbered
 * the Party's way ("57-NQ/TW"); the server only accepts the Party KB for it. */
export function isPartyDraft(d: { document_number: string; source_meta?: { source?: string } | null }): boolean {
  if (d.source_meta?.source === "dangcongsan") return true;
  return PARTY_NUMBER_RE.test(d.document_number.replace(/\s+/g, "").replace(/Ð/g, "Đ"));
}

export function pickDraftKnowledgeBase(
  kbs: readonly { id: string; name: string }[],
  docType: string,
  preferred?: string | null,
  party = false,
): string {
  if (party) {
    const kb = kbs.find((k) => k.name.trim() === PARTY_KB_NAME);
    if (kb) return kb.id;
  }
  if (preferred && kbs.some((k) => k.id === preferred)) return preferred;
  const type = docType.trim().toLowerCase();
  if (type) {
    const named = kbs.find((k) => {
      const name = k.name.toLowerCase();
      const tail = name.split(/[–—-]/).pop()?.trim() ?? "";
      return tail === type;
    });
    if (named) return named.id;
    const loose = kbs.find((k) => k.name.toLowerCase().includes(type));
    if (loose) return loose.id;
  }
  return kbs[0]?.id ?? "";
}

const HEADING_LABELS: Record<string, string> = { phan: "Phần", chuong: "Chương", muc: "Mục", dieu: "Điều" };

/** "Chương II", "Điều 5": how a heading starts in the text. */
export function headingLead(h: { kind: string; number: string }): string {
  return [HEADING_LABELS[h.kind] ?? "", h.number].filter(Boolean).join(" ");
}

function foldText(s: string): string {
  return s.normalize("NFC").replace(/\s+/g, " ").trim().toLowerCase();
}

/** Where a structure position is among the editor's paragraphs: the
 * paragraph text and its 1-based occurrence (identical paragraphs), for a
 * "select" op. The server's index is tried first; when it does not hold
 * the expected start (the editor counts paragraphs a little differently,
 * or the text changed since the check), the first paragraph starting with
 * `lead` is used. Null when neither is found. */
export function locateParagraph(
  texts: readonly string[],
  index: number,
  lead?: string,
): { text: string; occurrence: number } | null {
  const want = lead ? foldText(lead) : "";
  const fits = (i: number) => {
    const t = texts[i];
    if (t === undefined || !t.trim()) return false;
    const f = foldText(t);
    return !want || (f.startsWith(want) && !/[0-9]/.test(f.charAt(want.length)));
  };
  let at = -1;
  if (index >= 0 && fits(index)) at = index;
  else if (want) at = texts.findIndex((_, i) => fits(i));
  if (at < 0) return null;
  const text = texts[at];
  let occurrence = 0;
  for (let i = 0; i <= at; i++) if (texts[i] === text) occurrence++;
  return { text, occurrence };
}
