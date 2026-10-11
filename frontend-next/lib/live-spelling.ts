// Live spellcheck of the document assistant: which paragraphs of the open
// editor still need a review. A paragraph is identified by a hash of its
// normalised text, so any edit makes it "new" again; the hashes already
// reviewed are kept per document in localStorage, so a reload does not
// review (and re-mark) the whole document again.
// Runs with: node --experimental-strip-types --test lib/live-spelling.test.ts
import { normalizeAnchorText } from "./api/document-ops.ts";

export type LiveParagraph = { text: string; occurrence?: number; edited?: boolean };
export type LivePlan = { paragraphs: LiveParagraph[]; keys: string[] };

/** Server bounds of one review request (tools.LiveSpellMax*). */
export const LIVE_SPELL_MAX_PARAGRAPHS = 60;
export const LIVE_SPELL_MAX_RUNES = 4000;
const STORE_MAX = 4000;

/** cyrb53: a fast 53-bit string hash, plus the length against collisions. */
export function textKey(text: string): string {
  let h1 = 0xdeadbeef;
  let h2 = 0x41c6ce57;
  for (let i = 0; i < text.length; i++) {
    const ch = text.charCodeAt(i);
    h1 = Math.imul(h1 ^ ch, 2654435761);
    h2 = Math.imul(h2 ^ ch, 1597334677);
  }
  h1 = Math.imul(h1 ^ (h1 >>> 16), 2246822507) ^ Math.imul(h2 ^ (h2 >>> 13), 3266489909);
  h2 = Math.imul(h2 ^ (h2 >>> 16), 2246822507) ^ Math.imul(h1 ^ (h1 >>> 13), 3266489909);
  return (4294967296 * (2097151 & h2) + (h1 >>> 0)).toString(36) + "." + text.length.toString(36);
}

/** The paragraphs of `texts` not reviewed yet, in request-sized chunks.
 * `edited` (their old red underlines are cleared first) is set unless this
 * is the document's first review. Paragraphs without a letter, or longer
 * than the server accepts, are skipped. */
export function planLiveSpelling(texts: string[], reviewed: ReadonlySet<string>, firstPass: boolean): LivePlan[] {
  const seen = new Map<string, number>();
  const plans: LivePlan[] = [];
  let cur: LivePlan = { paragraphs: [], keys: [] };
  for (const raw of texts) {
    const norm = normalizeAnchorText(raw);
    if (!norm) continue;
    const occurrence = (seen.get(norm) ?? 0) + 1;
    seen.set(norm, occurrence);
    if (!/\p{L}/u.test(norm) || [...raw].length > LIVE_SPELL_MAX_RUNES) continue;
    const key = textKey(norm);
    if (reviewed.has(key)) continue;
    const p: LiveParagraph = { text: raw };
    if (occurrence > 1) p.occurrence = occurrence;
    if (!firstPass) p.edited = true;
    cur.paragraphs.push(p);
    if (!cur.keys.includes(key)) cur.keys.push(key);
    if (cur.paragraphs.length >= LIVE_SPELL_MAX_PARAGRAPHS) {
      plans.push(cur);
      cur = { paragraphs: [], keys: [] };
    }
  }
  if (cur.paragraphs.length > 0) plans.push(cur);
  return plans;
}

type KV = Pick<Storage, "getItem" | "setItem">;
const storeKey = (documentId: string) => `werag.docspell.${documentId}`;

/** The reviewed hashes of a document; null when it was never reviewed. */
export function readReviewed(store: KV | null | undefined, documentId: string): string[] | null {
  try {
    const raw = store?.getItem(storeKey(documentId));
    if (!raw) return null;
    const v = JSON.parse(raw);
    return Array.isArray(v) ? v.filter((x) => typeof x === "string") : null;
  } catch {
    return null;
  }
}

/** Keeps the newest STORE_MAX hashes (insertion order). */
export function writeReviewed(store: KV | null | undefined, documentId: string, keys: Iterable<string>): void {
  try {
    store?.setItem(storeKey(documentId), JSON.stringify([...keys].slice(-STORE_MAX)));
  } catch {
    /* storage full / disabled: reviews again after a reload */
  }
}
