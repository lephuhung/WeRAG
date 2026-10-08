/* Document scope of a document-assistant session (the part of the
 * documents the conversation is about) and the clarification card the
 * backend sends when a generic request would read a long document blindly.
 *
 *   GET    /api/v1/sessions/:id/documents        → {…, scope}
 *   PUT    /api/v1/sessions/:id/documents/scope  {document_ids, sections, task, set_by: "user"}
 *   DELETE /api/v1/sessions/:id/documents/scope
 *
 * The card arrives as a tool result (live) or an agent step (history) whose
 * data has display_type "document_scope_clarification". Pure helpers here;
 * the components are document-scope-card.tsx and the chip in chat-client.
 */

export const SCOPE_CLARIFICATION_TYPE = "document_scope_clarification";

/** Tasks a stored scope may name (types.DocumentScopeTask*). */
export type DocumentScopeTask = "" | "format" | "spelling" | "summary" | "lookup" | "compare" | "edit";

export interface DocumentScopeSection {
  document_id: string;
  from: number;
  to: number;
  title?: string;
}

export interface DocumentScope {
  document_ids: string[];
  sections?: DocumentScopeSection[];
  task?: DocumentScopeTask | string;
  set_by: "user" | "router" | string;
  query?: string;
  at?: string;
}

/** The card's task chips. "part" stores a scope of task lookup. */
export type ScopeCardTask = "format" | "spelling" | "summary" | "part" | "compare" | "other";

export interface ScopeCardSection {
  title: string;
  from: number;
  to: number;
}

export interface ScopeCardDocument {
  id: string;
  handle: string;
  file_name: string;
  role: "target" | "source" | string;
  unit?: string;
  paragraphs: number;
  runes: number;
  long?: boolean;
  sections: ScopeCardSection[];
}

export interface ScopeClarification {
  /** The request that was asked about; re-sent after the scope is set ("" from the chip). */
  query: string;
  documents: ScopeCardDocument[];
  tasks: { key: ScopeCardTask; label: string }[];
  suggested: { document_id: string; task?: ScopeCardTask | ""; sections: ScopeCardSection[] };
}

/** Most sections one scope holds (types.DocumentScopeMaxSections). */
export const SCOPE_MAX_SECTIONS = 12;

const CARD_TASKS: ScopeCardTask[] = ["format", "spelling", "summary", "part", "compare", "other"];

function rec(v: unknown): Record<string, unknown> | null {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
}

function sectionsOf(v: unknown): ScopeCardSection[] {
  if (!Array.isArray(v)) return [];
  const out: ScopeCardSection[] = [];
  for (const raw of v) {
    const s = rec(raw);
    if (!s) continue;
    const from = Number(s.from);
    const to = Number(s.to);
    if (!Number.isFinite(from) || !Number.isFinite(to)) continue;
    out.push({ title: typeof s.title === "string" ? s.title : "", from, to });
  }
  return out;
}

/** Reads the card payload out of tool-result data; null when it is not one. */
export function parseScopeClarification(data: unknown): ScopeClarification | null {
  const d = rec(data);
  if (!d || d.display_type !== SCOPE_CLARIFICATION_TYPE) return null;
  const documents: ScopeCardDocument[] = [];
  for (const raw of Array.isArray(d.documents) ? d.documents : []) {
    const doc = rec(raw);
    if (!doc || typeof doc.id !== "string" || !doc.id) continue;
    documents.push({
      id: doc.id,
      handle: typeof doc.handle === "string" ? doc.handle : "",
      file_name: typeof doc.file_name === "string" ? doc.file_name : "",
      role: typeof doc.role === "string" ? doc.role : "target",
      unit: typeof doc.unit === "string" ? doc.unit : undefined,
      paragraphs: Number(doc.paragraphs) || 0,
      runes: Number(doc.runes) || 0,
      long: doc.long === true,
      sections: sectionsOf(doc.sections),
    });
  }
  if (documents.length === 0) return null;
  const tasks: ScopeClarification["tasks"] = [];
  for (const raw of Array.isArray(d.tasks) ? d.tasks : []) {
    const t = rec(raw);
    const key = t?.key as ScopeCardTask;
    if (t && CARD_TASKS.includes(key)) tasks.push({ key, label: typeof t.label === "string" ? t.label : key });
  }
  const sug = rec(d.suggested) ?? {};
  const sugDoc = typeof sug.document_id === "string" && documents.some((x) => x.id === sug.document_id) ? sug.document_id : documents[0].id;
  const sugTask = CARD_TASKS.includes(sug.task as ScopeCardTask) ? (sug.task as ScopeCardTask) : "";
  return {
    query: typeof d.query === "string" ? d.query : "",
    documents,
    tasks: tasks.length > 0 ? tasks : CARD_TASKS.map((key) => ({ key, label: key })),
    suggested: { document_id: sugDoc, task: sugTask, sections: sectionsOf(sug.sections) },
  };
}

/** Splits a history/live step list into the card (its last one) and the
 * steps the timeline shows. */
export function takeScopeClarification<S extends object>(steps: S[] | undefined): {
  card: ScopeClarification | null;
  steps: S[] | undefined;
} {
  if (!steps?.length) return { card: null, steps };
  let card: ScopeClarification | null = null;
  const rest: S[] = [];
  for (const s of steps) {
    const c = parseScopeClarification((s as { tool_data?: unknown }).tool_data);
    if (c) card = c;
    else rest.push(s);
  }
  return { card, steps: rest.length === steps.length ? steps : rest.length ? rest : undefined };
}

/** What confirming the card does. */
export type ScopeCardAction =
  | { kind: "scope"; scope: { document_ids: string[]; sections: DocumentScopeSection[]; task: DocumentScopeTask; set_by: "user" }; resend: string }
  | { kind: "message"; text: string }
  | { kind: "compose" };

export interface ScopeCardChoice {
  /** "" until a task chip is picked. */
  task: ScopeCardTask | "";
  documentId: string;
  /** Indexes into the chosen document's sections. */
  sections: number[];
}

/** Whether the task shows the section checklist. */
export function taskTakesSections(task: ScopeCardTask | "" | undefined): boolean {
  return task === "summary" || task === "part" || task === "compare";
}

/** Whether the choice can be confirmed: a task, and sections for "part". */
export function scopeChoiceReady(card: ScopeClarification, choice: ScopeCardChoice): boolean {
  if (!choice.task || !card.documents.some((d) => d.id === choice.documentId)) return false;
  if (choice.task === "part") return choice.sections.length > 0;
  return true;
}

/** The action of a confirmed choice: format and spelling send a canned
 * request naming the document (those tools need no text); "other" hands
 * the composer back; the rest store a user scope then re-send the
 * question. */
export function scopeCardAction(card: ScopeClarification, choice: ScopeCardChoice): ScopeCardAction | null {
  const doc = card.documents.find((d) => d.id === choice.documentId);
  if (!doc || !choice.task) return null;
  const name = doc.handle || doc.file_name;
  switch (choice.task) {
    case "format":
      return { kind: "message", text: `Kiểm tra thể thức của ${name}` };
    case "spelling":
      return { kind: "message", text: `Kiểm tra chính tả của ${name}` };
    case "other":
      return { kind: "compose" };
  }
  const picked = [...new Set(choice.sections)]
    .filter((i) => i >= 0 && i < doc.sections.length)
    .sort((a, b) => a - b)
    .slice(0, SCOPE_MAX_SECTIONS)
    .map((i) => ({ document_id: doc.id, from: doc.sections[i].from, to: doc.sections[i].to, title: doc.sections[i].title }));
  if (choice.task === "part" && picked.length === 0) return null;
  const task: DocumentScopeTask = choice.task === "part" ? "lookup" : choice.task;
  const ids = choice.task === "compare" ? [doc.id, ...card.documents.filter((d) => d.id !== doc.id).map((d) => d.id)] : [doc.id];
  return { kind: "scope", scope: { document_ids: ids, sections: picked, task, set_by: "user" }, resend: card.query };
}

/** The card's starting choice: the suggestion, its sections ticked. */
export function initialScopeChoice(card: ScopeClarification): ScopeCardChoice {
  const doc = card.documents.find((d) => d.id === card.suggested.document_id) ?? card.documents[0];
  const sections = card.suggested.sections
    .map((s) => doc.sections.findIndex((x) => x.from === s.from && x.to === s.to))
    .filter((i) => i >= 0);
  return { task: card.suggested.task ?? "", documentId: doc.id, sections };
}

/** A card to change the current scope from the chip: the session's
 * documents with the sections of their cards; the current scope is the
 * suggestion. Nothing is re-sent after it. */
export function scopeCardFromDocuments(
  docs: { id: string; handle?: string; file_name: string; role?: string; profile?: { sections?: ScopeCardSection[] } }[],
  scope: DocumentScope | null,
  tasks: ScopeClarification["tasks"],
): ScopeClarification | null {
  if (docs.length === 0) return null;
  const documents: ScopeCardDocument[] = docs.map((d) => ({
    id: d.id,
    handle: d.handle ?? "",
    file_name: d.file_name,
    role: d.role === "source" ? "source" : "target",
    paragraphs: 0,
    runes: 0,
    sections: sectionsOf(d.profile?.sections ?? []),
  }));
  const first = scope?.document_ids.find((id) => documents.some((d) => d.id === id)) ?? documents[0].id;
  const fromTask: Record<string, ScopeCardTask> = { summary: "summary", lookup: "part", compare: "compare", format: "format", spelling: "spelling" };
  return {
    query: "",
    documents,
    tasks: tasks.filter((t) => t.key !== "compare" || documents.length > 1),
    suggested: {
      document_id: first,
      task: scope?.task ? (fromTask[scope.task] ?? "") : "",
      sections: (scope?.sections ?? []).filter((s) => s.document_id === first).map((s) => ({ title: s.title ?? "", from: s.from, to: s.to })),
    },
  };
}

const SECTION_REF = /^(Điều|Chương|Mục|Phụ lục|Phần)\s+([0-9]+|[IVXLC]+)\b/i;

/** Short label of a scope's sections: "Điều 3–5" for a run of one kind,
 * "Điều 3, 7", or the first title and how many more. */
export function sectionsLabel(sections: { title?: string; from: number; to: number }[]): string {
  if (sections.length === 0) return "";
  const refs = sections.map((s) => SECTION_REF.exec((s.title ?? "").trim()));
  if (refs.every(Boolean)) {
    const kind = refs[0]![1];
    if (refs.every((r) => r![1].toLowerCase() === kind.toLowerCase())) {
      const nums = refs.map((r) => r![2]);
      if (nums.every((n) => /^\d+$/.test(n))) {
        const v = nums.map(Number).sort((a, b) => a - b);
        const run = v.every((n, i) => i === 0 || n === v[i - 1] + 1);
        if (v.length === 1) return `${kind} ${v[0]}`;
        return run ? `${kind} ${v[0]}–${v[v.length - 1]}` : `${kind} ${v.join(", ")}`;
      }
      return `${kind} ${nums.join(", ")}`;
    }
  }
  const first = (sections[0].title ?? "").trim() || `[${sections[0].from}–${sections[0].to}]`;
  const clipped = first.length > 32 ? `${first.slice(0, 32)}…` : first;
  return sections.length > 1 ? `${clipped} +${sections.length - 1}` : clipped;
}

/** The chip's parts: "vb1", "Điều 3–5", the task; null when the scope
 * names none of the session's documents. */
export function scopeChipParts(
  scope: DocumentScope | null | undefined,
  docs: { id: string; handle?: string; file_name: string }[],
): { documents: string; sections: string; task: string; byUser: boolean } | null {
  if (!scope?.document_ids?.length) return null;
  const named = scope.document_ids
    .map((id) => docs.find((d) => d.id === id))
    .filter((d): d is { id: string; handle?: string; file_name: string } => Boolean(d));
  if (named.length === 0) return null;
  const live = new Set(named.map((d) => d.id));
  return {
    documents: named.map((d) => d.handle || d.file_name).join(", "),
    sections: sectionsLabel((scope.sections ?? []).filter((s) => live.has(s.document_id))),
    task: typeof scope.task === "string" ? scope.task : "",
    byUser: scope.set_by === "user",
  };
}

/** Reads `scope` off GET /documents; null when absent or malformed. */
export function parseDocumentScope(raw: unknown): DocumentScope | null {
  const s = rec(raw);
  if (!s || !Array.isArray(s.document_ids)) return null;
  const ids = s.document_ids.filter((x): x is string => typeof x === "string" && x !== "");
  if (ids.length === 0) return null;
  const sections: DocumentScopeSection[] = [];
  for (const r of Array.isArray(s.sections) ? s.sections : []) {
    const x = rec(r);
    if (x && typeof x.document_id === "string" && Number.isFinite(Number(x.from)) && Number.isFinite(Number(x.to))) {
      sections.push({ document_id: x.document_id, from: Number(x.from), to: Number(x.to), title: typeof x.title === "string" ? x.title : undefined });
    }
  }
  return {
    document_ids: ids,
    sections,
    task: typeof s.task === "string" ? s.task : "",
    set_by: typeof s.set_by === "string" ? s.set_by : "router",
    query: typeof s.query === "string" ? s.query : undefined,
    at: typeof s.at === "string" ? s.at : undefined,
  };
}

/** Pages a card's document fills (about 2000 characters an A4 page). */
export function estimatedPages(runes: number): number {
  return Math.max(1, Math.ceil(runes / 2000));
}
