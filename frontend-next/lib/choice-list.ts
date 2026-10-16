/* A question answered by picking one option under it (document assistant):
 * "Nghị định 53 — which document?" (display_type legal_candidates) and
 * "ABC — which meaning?" / "what is the word X?" (display_type
 * abbreviation_choice). Picking an option sends option.resend as the next
 * user message and the assistant continues; an option with an input asks
 * for text first and sends its template with "{text}" filled in.
 *
 * The payload arrives as a tool result (live) or an agent step (history).
 * Pure helpers here; the component is choice-list.tsx.
 */

export const LEGAL_CANDIDATES_TYPE = "legal_candidates";
export const ABBREVIATION_CHOICE_TYPE = "abbreviation_choice";
/** "luật đất đai — which group?" (a broad question over dozens of documents). */
export const BROAD_QUERY_TYPE = "broad_query_clarification";

export interface ChoiceOption {
  /** Main text of the option (a số hiệu, a full form). */
  label: string;
  /** Second line (a title, a description). */
  detail?: string;
  /** Short tag after the label (a validity status). */
  badge?: string;
  /** Whether the badge is the "good" state (in force). */
  badgeOk?: boolean;
  /** Small muted text at the end of the first line (a date). */
  meta?: string;
  /** The message to send when picked (empty for an option with an input). */
  resend: string;
  /** Placeholder of the text box shown when picked; the message is then
   * `template` with "{text}" replaced by what was typed. */
  input?: string;
  template?: string;
}

/** The message an option sends: its resend, or its template filled with text. */
export function optionMessage(o: ChoiceOption, text = ""): string {
  return o.input ? (o.template ?? "").split("{text}").join(text.trim()) : o.resend;
}

export interface ChoiceList {
  kind: typeof LEGAL_CANDIDATES_TYPE | typeof ABBREVIATION_CHOICE_TYPE | typeof BROAD_QUERY_TYPE;
  /** What is being asked about ("Nghị định 53", "ABC"). */
  subject: string;
  /** How many options there are in all (the list may be shorter). */
  total: number;
  options: ChoiceOption[];
}

function rec(v: unknown): Record<string, unknown> | null {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
}

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function statusInForce(status: string): boolean {
  const s = status.toLowerCase();
  return s.startsWith("còn hiệu lực") || s.startsWith("chưa có hiệu lực");
}

/** Reads a choice-list payload out of tool-result data; null when it is not one. */
export function parseChoiceList(data: unknown): ChoiceList | null {
  const d = rec(data);
  if (!d) return null;
  if (d.display_type === LEGAL_CANDIDATES_TYPE) {
    const options: ChoiceOption[] = [];
    for (const raw of Array.isArray(d.candidates) ? d.candidates : []) {
      const c = rec(raw);
      if (!c || !str(c.document_number)) continue;
      const status = str(c.status);
      options.push({
        label: str(c.document_number),
        detail: str(c.title) || undefined,
        badge: status || undefined,
        badgeOk: status ? statusInForce(status) : undefined,
        meta: str(c.issued_on).slice(0, 10) || undefined,
        resend: str(c.resend) || `${str(d.query)} (${str(c.document_number)})`.trim(),
      });
    }
    if (options.length === 0) return null;
    return { kind: LEGAL_CANDIDATES_TYPE, subject: str(d.reference), total: Number(d.total) || options.length, options };
  }
  if (d.display_type === BROAD_QUERY_TYPE) {
    const options: ChoiceOption[] = [];
    for (const raw of Array.isArray(d.options) ? d.options : []) {
      const o = rec(raw);
      if (!o || !str(o.label) || !str(o.resend)) continue;
      options.push({ label: str(o.label), detail: str(o.detail) || undefined, resend: str(o.resend) });
    }
    if (options.length === 0) return null;
    return { kind: BROAD_QUERY_TYPE, subject: str(d.topic), total: options.length, options };
  }
  if (d.display_type === ABBREVIATION_CHOICE_TYPE) {
    const options: ChoiceOption[] = [];
    for (const raw of Array.isArray(d.options) ? d.options : []) {
      const o = rec(raw);
      if (!o || !str(o.label)) continue;
      if (str(o.input) && str(o.template).includes("{text}")) {
        options.push({ label: str(o.label), detail: str(o.detail) || undefined, resend: "", input: str(o.input), template: str(o.template) });
      } else if (str(o.resend)) {
        options.push({ label: str(o.label), detail: str(o.detail) || undefined, resend: str(o.resend) });
      }
    }
    if (options.length === 0) return null;
    return { kind: ABBREVIATION_CHOICE_TYPE, subject: str(d.subject), total: options.length, options };
  }
  return null;
}

/** Splits a history/live step list into the choice list (its last one)
 * and the steps the timeline shows. */
export function takeChoiceList<S extends object>(steps: S[] | undefined): {
  card: ChoiceList | null;
  steps: S[] | undefined;
} {
  if (!steps?.length) return { card: null, steps };
  let card: ChoiceList | null = null;
  const rest: S[] = [];
  for (const s of steps) {
    const c = parseChoiceList((s as { tool_data?: unknown }).tool_data);
    if (c) card = c;
    else rest.push(s);
  }
  return { card, steps: rest.length === steps.length ? steps : rest.length ? rest : undefined };
}
