// Live spellcheck of the document assistant: which paragraphs of the open
// editor still need a review. A paragraph is identified by a hash of its
// normalised text, so any edit makes it "new" again; the hashes already
// reviewed are kept per document in localStorage, so a reload does not
// review (and re-mark) the whole document again.
// Runs with: node --experimental-strip-types --test lib/live-spelling.test.ts
import { normalizeAnchorText } from "./api/document-ops.ts";

export type LiveParagraph = { text: string; occurrence?: number; edited?: boolean };
/** keys: the distinct hashes of the plan; paraKeys: each paragraph's hash,
 * aligned with paragraphs. */
export type LivePlan = { paragraphs: LiveParagraph[]; keys: string[]; paraKeys: string[] };

/** Server bounds of one review request (tools.LiveSpellMax*). */
export const LIVE_SPELL_MAX_PARAGRAPHS = 60;
export const LIVE_SPELL_MAX_RUNES = 8000;
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

/** The paragraphs of `texts` not reviewed yet, in request-sized chunks,
 * all `edited`: their old underlines are cleared before the new ones go
 * on — on a document's first review too, since the file may still carry
 * underlines from an earlier session, another computer or a cleared
 * browser store (a corrected word stayed underlined). Paragraphs without a
 * letter, or longer than the server accepts, are skipped. */
export function planLiveSpelling(texts: string[], reviewed: ReadonlySet<string>): LivePlan[] {
  const seen = new Map<string, number>();
  const plans: LivePlan[] = [];
  let cur: LivePlan = { paragraphs: [], keys: [], paraKeys: [] };
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
    p.edited = true;
    cur.paragraphs.push(p);
    cur.paraKeys.push(key);
    if (!cur.keys.includes(key)) cur.keys.push(key);
    if (cur.paragraphs.length >= LIVE_SPELL_MAX_PARAGRAPHS) {
      plans.push(cur);
      cur = { paragraphs: [], keys: [], paraKeys: [] };
    }
  }
  if (cur.paragraphs.length > 0) plans.push(cur);
  return plans;
}

/** The hashes of a plan to record as reviewed once its review answered:
 * every key but those of the paragraphs the model missed (positions in
 * plan.paragraphs). A key shared with a missed paragraph waits too. */
export function reviewedKeys(plan: LivePlan, missed: readonly number[]): string[] {
  const out = new Set(plan.keys);
  for (const i of missed) {
    const k = plan.paraKeys[i];
    if (k) out.delete(k);
  }
  return [...out];
}

type KV = Pick<Storage, "getItem" | "setItem">;
// v3 (2026-10-15): every paragraph is reviewed once more with its old
// underlines cleared (v1/v2 left a first pass's stale underlines on); v2
// kept the mistakes, not only their count, so the list can show them.
const storeKey = (documentId: string) => `werag.docspell3.${documentId}`;

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

/** One underlined mistake: the word (its textOccurrence-th instance in
 * the paragraph whose text is `paragraph`, the occurrence-th paragraph
 * with that text) and the model's correction. */
export type LiveMistake = {
  paragraph: string;
  occurrence?: number;
  text: string;
  textOccurrence?: number;
  suggestion?: string;
};

/** Progress of the live spellcheck, for the ring in the chat header.
 * checking: `checked` of `total` new paragraphs answered; done: every
 * paragraph reviewed, `found` mistakes underlined in the document (listed
 * in `mistakes`, document order); failed: the model did not answer (tried
 * again on the next pause); off: the live spellcheck does not apply to
 * this document. */
export type LiveSpellingStatus = {
  phase: "idle" | "checking" | "done" | "failed" | "off";
  total: number;
  checked: number;
  found: number;
  mistakes: LiveMistake[];
  /** The last read found no paragraph with text (undefined: not read yet). */
  empty?: boolean;
};

/** What selecting a listed mistake in the editor did: ok (selected),
 * missing (the text is not there any more), outdated (the editor runs a
 * plugin without the select op — its cached index.html, see the plugin
 * README) or noAnswer (no result in time). */
export type GoToResult = "ok" | "missing" | "outdated" | "noAnswer";

/** What the spelling list can do in the editor: select a mistake, or take
 * its underline away after the user said it is no mistake (false: the
 * verdict was not recorded). */
export type SpellingActions = {
  goTo: (m: LiveMistake) => Promise<GoToResult>;
  dismiss: (m: LiveMistake) => Promise<boolean>;
};

/** The GoToResult of the editor's answer to a one-op select batch. */
export function goToResult(o: { kind: "result" | "timeout"; applied?: number; failed?: readonly { error: string }[] }): GoToResult {
  if (o.kind !== "result") return "noAnswer";
  if ((o.applied ?? 0) > 0) return "ok";
  const errors = (o.failed ?? []).map((f) => f.error);
  return errors.some((e) => /unknown op|select unsupported/.test(e)) ? "outdated" : "missing";
}

/** Whether a and b are the same underline. */
export function sameMistake(a: LiveMistake, b: LiveMistake): boolean {
  return (
    a.paragraph === b.paragraph &&
    (a.occurrence ?? 1) === (b.occurrence ?? 1) &&
    a.text === b.text &&
    (a.textOccurrence ?? 1) === (b.textOccurrence ?? 1)
  );
}

/** The raw ops that take the underline of m away: the paragraph loses its
 * underlines, and the others of that paragraph (in list) are put back. */
export function dismissOps(m: LiveMistake, list: readonly LiveMistake[]): unknown[] {
  const anchor = m.occurrence ? { text: m.paragraph, occurrence: m.occurrence } : { text: m.paragraph };
  const ops: unknown[] = [{ op: "unmark", anchor }];
  for (const o of list) {
    if (sameMistake(o, m) || o.paragraph !== m.paragraph || (o.occurrence ?? 1) !== (m.occurrence ?? 1)) continue;
    ops.push({
      op: "mark",
      anchor,
      text: o.text,
      style: "underline",
      ...(o.textOccurrence ? { textOccurrence: o.textOccurrence } : {}),
      ...(o.suggestion ? { suggestion: o.suggestion } : {}),
    });
  }
  return ops;
}

export const IDLE_SPELLING: LiveSpellingStatus = { phase: "idle", total: 0, checked: 0, found: 0, mistakes: [] };

const posInt = (v: unknown): number | undefined => (typeof v === "number" && Number.isInteger(v) && v > 1 ? v : undefined);

/** The mistakes of a review's ops, per paragraph hash (mark ops with a
 * text only). */
export function mistakesByParagraph(ops: readonly unknown[]): Map<string, LiveMistake[]> {
  const out = new Map<string, LiveMistake[]>();
  for (const op of ops) {
    const o = op as { op?: unknown; anchor?: { text?: unknown; occurrence?: unknown }; text?: unknown; textOccurrence?: unknown; suggestion?: unknown };
    if (o?.op !== "mark" || typeof o.anchor?.text !== "string" || typeof o.text !== "string" || !o.text) continue;
    const m: LiveMistake = { paragraph: o.anchor.text, text: o.text };
    const occ = posInt(o.anchor.occurrence);
    if (occ) m.occurrence = occ;
    const tocc = posInt(o.textOccurrence);
    if (tocc) m.textOccurrence = tocc;
    if (typeof o.suggestion === "string" && o.suggestion.trim()) m.suggestion = o.suggestion.trim();
    const k = textKey(normalizeAnchorText(o.anchor.text));
    const list = out.get(k);
    if (list) list.push(m);
    else out.set(k, [m]);
  }
  return out;
}

/** The mistakes underlined in the document, in its order (a paragraph
 * repeated word for word is listed once). */
export function mistakesIn(texts: readonly string[], found: ReadonlyMap<string, readonly LiveMistake[]>): LiveMistake[] {
  const out: LiveMistake[] = [];
  const seen = new Set<string>();
  for (const raw of texts) {
    const norm = normalizeAnchorText(raw);
    if (!norm) continue;
    const k = textKey(norm);
    if (seen.has(k)) continue;
    seen.add(k);
    out.push(...(found.get(k) ?? []));
  }
  return out;
}

/** A few words either side of a mistake, for the list. */
export function mistakeContext(m: LiveMistake, radius = 40): string {
  const flat = m.paragraph.replace(/\s+/g, " ").trim();
  let at = -1;
  for (let n = m.textOccurrence ?? 1; n > 0; n--) {
    at = flat.indexOf(m.text, at + 1);
    if (at < 0) break;
  }
  if (at < 0) return flat.length > radius * 2 ? `${flat.slice(0, radius * 2)}…` : flat;
  const from = Math.max(0, flat.lastIndexOf(" ", Math.max(0, at - radius)) + 1);
  let to = flat.indexOf(" ", Math.min(flat.length, at + m.text.length + radius));
  if (to < 0) to = flat.length;
  return `${from > 0 ? "…" : ""}${flat.slice(from, to)}${to < flat.length ? "…" : ""}`;
}

const foundKey = (documentId: string) => `werag.docspell3.found.${documentId}`;

function validMistake(v: unknown): v is LiveMistake {
  const m = v as LiveMistake;
  return !!m && typeof m === "object" && typeof m.paragraph === "string" && typeof m.text === "string" && m.text !== "";
}

/** The mistakes per paragraph hash of a document (survive a reload). */
export function readFound(store: KV | null | undefined, documentId: string): Map<string, LiveMistake[]> {
  try {
    const raw = store?.getItem(foundKey(documentId));
    const v = raw ? JSON.parse(raw) : null;
    if (!v || typeof v !== "object" || Array.isArray(v)) return new Map();
    const out = new Map<string, LiveMistake[]>();
    for (const [k, list] of Object.entries(v)) {
      const ok = Array.isArray(list) ? list.filter(validMistake) : [];
      if (ok.length > 0) out.set(k, ok);
    }
    return out;
  } catch {
    return new Map();
  }
}

/** Keeps the paragraphs with mistakes, the newest STORE_MAX of them. */
export function writeFound(store: KV | null | undefined, documentId: string, found: ReadonlyMap<string, readonly LiveMistake[]>): void {
  try {
    const kept = [...found].filter(([, list]) => list.length > 0).slice(-STORE_MAX);
    store?.setItem(foundKey(documentId), JSON.stringify(Object.fromEntries(kept)));
  } catch {
    /* storage full / disabled */
  }
}
