/* Document-assistant edit plans: the editing tools no longer write the file
 * on the server — their tool_result carries `document_ops` that the WeRAG
 * ONLYOFFICE plugin applies inside the open editor (one undoable batch).
 *
 * Host → editor iframe:  {source:"werag-host", type:"apply_ops", batchId, ops}
 * Plugin → host:         {source:"werag-onlyoffice", type:"ops_ack", batchId}
 *                        {source:"werag-onlyoffice", type:"ops_result", batchId, applied, failed:[{index,error}]}
 *
 * normalizeAnchorText / findAnchorParagraph are mirrored in
 * docker/onlyoffice/plugins/werag-assistant/plugin.js — keep them in sync. */

export const DOCUMENT_OPS_TOOLS = new Set([
  // marks the measured format findings where they are
  "check_document_format",
  "insert_paragraphs",
  "rewrite_paragraphs",
  "mark_passages",
  "apply_format_fixes",
  "check_spelling",
]);

export type ParagraphAnchor = { text: string; occurrence?: number };
export type StartAnchor = { atStart: true };
export type Alignment = "left" | "center" | "right" | "both";
export type MarkStyle = "underline" | "highlight" | "color";

export type DocumentOp =
  | { op: "replaceText"; anchor: ParagraphAnchor; old: string; new: string }
  | { op: "replaceParagraph"; anchor: ParagraphAnchor; new: string }
  | {
      op: "insertAfter";
      anchor: ParagraphAnchor | StartAnchor;
      text: string;
      like?: { text: string };
      alignment?: Alignment;
      bold?: boolean;
      italic?: boolean;
    }
  | {
      op: "mark";
      anchor: ParagraphAnchor;
      text?: string;
      /** 1-based instance of `text` inside the paragraph (omitted = first). */
      textOccurrence?: number;
      style: MarkStyle;
    }
  | {
      /** Selects a listed mistake so the editor scrolls to it (page-built). */
      op: "select";
      anchor: ParagraphAnchor;
      text?: string;
      textOccurrence?: number;
    }
  | {
      op: "formatParagraph";
      anchor: ParagraphAnchor;
      alignment?: Alignment;
      font?: string;
      sizePt?: number;
      bold?: boolean;
      italic?: boolean;
    }
  /** Clears the underlines of an edited paragraph (live spellcheck). */
  | { op: "unmark"; anchor: ParagraphAnchor }
  | { op: "pageSetup"; marginsMm?: { top?: number; bottom?: number; left?: number; right?: number }; a4?: true }
  | {
      /** Built here from an illustration card, never sent by the server. */
      op: "insertImage";
      anchor: ParagraphAnchor | StartAnchor;
      /** PNG/JPEG data URL */
      src: string;
      widthMm: number;
      heightMm: number;
      caption?: string;
    };

/** documentId routes the batch to that document's editor tab; absent on
 * results from before multi-document sessions (→ the active tab). */
export type OpsBatch = { batchId: string; ops: DocumentOp[]; documentId?: string };

export type OpsFailure = { index: number; error: string };
export type OpsResult = { batchId: string; applied: number; failed: OpsFailure[] };

/* ---------- anchor matching (mirrored in the plugin) ---------- */

/** NFC, every whitespace run (incl. NBSP, tabs, \r line breaks from
 * ApiParagraph.GetText, zero-width spaces) → one space, trimmed. */
export function normalizeAnchorText(s: string): string {
  return String(s ?? "")
    .normalize("NFC")
    .replace(/[\u200B-\u200D\uFEFF]/g, "")
    .replace(/[\s\u00A0]+/g, " ")
    .trim();
}

/** Index of the paragraph whose normalised text equals the anchor's, taking
 * the `occurrence`-th match (1-based, default 1); -1 when absent. */
export function findAnchorParagraph(paragraphTexts: string[], anchor: ParagraphAnchor): number {
  const want = normalizeAnchorText(anchor.text);
  if (!want) return -1;
  let n = Math.max(1, Math.floor(anchor.occurrence ?? 1));
  for (let i = 0; i < paragraphTexts.length; i++) {
    if (normalizeAnchorText(paragraphTexts[i]) === want && --n === 0) return i;
  }
  return -1;
}

/* ---------- validation ---------- */

const ALIGNMENTS = new Set(["left", "center", "right", "both"]);
const MARK_STYLES = new Set(["underline", "highlight", "color"]);

const isObj = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v);
const isStr = (v: unknown): v is string => typeof v === "string";
const optBool = (v: unknown) => v === undefined || typeof v === "boolean";
const optAlign = (v: unknown) => v === undefined || (isStr(v) && ALIGNMENTS.has(v));
const optNum = (v: unknown) => v === undefined || (typeof v === "number" && Number.isFinite(v) && v >= 0);

function validAnchor(a: unknown, allowStart = false): boolean {
  if (!isObj(a)) return false;
  if (allowStart && a.atStart === true) return true;
  if (!isStr(a.text) || !normalizeAnchorText(a.text)) return false;
  return a.occurrence === undefined || (typeof a.occurrence === "number" && Number.isInteger(a.occurrence) && a.occurrence >= 1);
}

/** Why `raw` is not a valid op, or null when it is. */
export function opValidationError(raw: unknown): string | null {
  if (!isObj(raw) || !isStr(raw.op)) return "not an op object";
  switch (raw.op) {
    case "replaceText":
      if (!validAnchor(raw.anchor)) return "bad anchor";
      if (!isStr(raw.old) || !raw.old) return "missing old";
      if (!isStr(raw.new)) return "missing new";
      return null;
    case "replaceParagraph":
      if (!validAnchor(raw.anchor)) return "bad anchor";
      return isStr(raw.new) ? null : "missing new";
    case "insertAfter":
      if (!validAnchor(raw.anchor, true)) return "bad anchor";
      // The server omits an empty `text` (blank line, Go omitempty).
      if (raw.text !== undefined && (!isStr(raw.text) || /[\r\n]/.test(raw.text))) return "text must be one line";
      if (raw.like !== undefined && !(isObj(raw.like) && isStr(raw.like.text))) return "bad like";
      if (!optAlign(raw.alignment)) return "bad alignment";
      return optBool(raw.bold) && optBool(raw.italic) ? null : "bad bold/italic";
    case "mark":
      if (!validAnchor(raw.anchor)) return "bad anchor";
      if (raw.text !== undefined && !isStr(raw.text)) return "bad text";
      if (
        raw.textOccurrence !== undefined &&
        !(typeof raw.textOccurrence === "number" && Number.isInteger(raw.textOccurrence) && raw.textOccurrence >= 1)
      )
        return "bad textOccurrence";
      return isStr(raw.style) && MARK_STYLES.has(raw.style) ? null : "bad style";
    case "unmark":
      return validAnchor(raw.anchor) ? null : "bad anchor";
    case "select":
      // moves the selection to a listed mistake (built by the page)
      if (!validAnchor(raw.anchor)) return "bad anchor";
      if (raw.text !== undefined && !isStr(raw.text)) return "bad text";
      return raw.textOccurrence === undefined ||
        (typeof raw.textOccurrence === "number" && Number.isInteger(raw.textOccurrence) && raw.textOccurrence >= 1)
        ? null
        : "bad textOccurrence";
    case "formatParagraph":
      if (!validAnchor(raw.anchor)) return "bad anchor";
      if (!optAlign(raw.alignment)) return "bad alignment";
      if (raw.font !== undefined && !isStr(raw.font)) return "bad font";
      if (!optNum(raw.sizePt)) return "bad sizePt";
      return optBool(raw.bold) && optBool(raw.italic) ? null : "bad bold/italic";
    case "pageSetup": {
      if (raw.a4 !== undefined && raw.a4 !== true) return "bad a4";
      const m = raw.marginsMm;
      if (m === undefined) return raw.a4 ? null : "empty pageSetup";
      if (!isObj(m)) return "bad marginsMm";
      return ["top", "bottom", "left", "right"].every((k) => optNum(m[k])) ? null : "bad marginsMm";
    }
    case "insertImage":
      if (!validAnchor(raw.anchor, true)) return "bad anchor";
      if (!isStr(raw.src) || !/^data:image\/(png|jpeg);base64,/.test(raw.src)) return "bad src";
      if (!(typeof raw.widthMm === "number" && raw.widthMm > 0) || !(typeof raw.heightMm === "number" && raw.heightMm > 0))
        return "bad size";
      return raw.caption === undefined || isStr(raw.caption) ? null : "bad caption";
    default:
      return `unknown op ${raw.op}`;
  }
}

/** Edit-plan batch carried by a document tool result, or null. Invalid ops
 * are dropped (reported in `rejected` with their original index). */
export function opsBatchFromToolData(
  toolName: string | undefined,
  data: Record<string, unknown> | undefined,
): (OpsBatch & { rejected: OpsFailure[] }) | null {
  const name = toolName || (isStr(data?.tool_name) ? data.tool_name : "");
  if (!DOCUMENT_OPS_TOOLS.has(name) || !data) return null;
  // a rewrite proposal applies only when the user picks a version
  if (data.proposal === true) return null;
  const raw = data.document_ops;
  const batchId = isStr(data.ops_batch_id) ? data.ops_batch_id.trim() : "";
  if (!Array.isArray(raw) || raw.length === 0 || !batchId) return null;
  const ops: DocumentOp[] = [];
  const rejected: OpsFailure[] = [];
  raw.forEach((op, index) => {
    const err = opValidationError(op);
    if (err) rejected.push({ index, error: err });
    else if (op.op === "insertAfter" && op.text === undefined) ops.push({ ...op, text: "" } as DocumentOp);
    else ops.push(op as DocumentOp);
  });
  const documentId = isStr(data.document_id) && data.document_id.trim() ? data.document_id.trim() : undefined;
  return documentId ? { batchId, ops, rejected, documentId } : { batchId, ops, rejected };
}

/** Batch id prefix of the live spellcheck's underlines: applied silently
 * and never written to the applied-batch ledger (there are many). */
export const LIVE_SPELLING_BATCH_PREFIX = "spell-live-";

export const isLiveSpellingBatch = (batchId: string) => batchId.startsWith(LIVE_SPELLING_BATCH_PREFIX);

/* ---------- rewrite proposals ---------- */

/* rewrite_paragraphs on a highlighted passage proposes (Data.proposal):
 * nothing is applied until the user picks a version in the chat card. The
 * card then asks the server for a snapshot (proposals/apply) and feeds the
 * version's ops to the editor as the batch `${batchId}:${variantId}`. */

export type RewriteVariant = { id: string; label: string; old: string; new: string; ops: DocumentOp[] };

export type RewriteProposal = {
  documentId: string;
  /** "vb1 · file.docx" */
  document: string;
  batchId: string;
  selectionText: string;
  variants: RewriteVariant[];
};

/** The rewrite proposal a tool result carries, or null. A version whose ops
 * do not all validate is dropped (it would apply only in part). */
export function rewriteProposalFromToolData(
  toolName: string | undefined,
  data: Record<string, unknown> | undefined,
): RewriteProposal | null {
  const name = toolName || (isStr(data?.tool_name) ? data.tool_name : "");
  if (name !== "rewrite_paragraphs" || !data || data.proposal !== true) return null;
  const batchId = isStr(data.ops_batch_id) ? data.ops_batch_id.trim() : "";
  const documentId = isStr(data.document_id) ? data.document_id.trim() : "";
  if (!batchId || !documentId || !Array.isArray(data.variants)) return null;
  const variants: RewriteVariant[] = [];
  const seen = new Set<string>();
  for (const v of data.variants) {
    if (!isObj(v) || !isStr(v.id) || !v.id.trim() || seen.has(v.id) || !Array.isArray(v.ops) || v.ops.length === 0) continue;
    if (v.ops.some((op) => opValidationError(op) !== null)) continue;
    seen.add(v.id);
    variants.push({
      id: v.id,
      label: isStr(v.label) && v.label.trim() ? v.label.trim() : v.id,
      old: isStr(v.old) ? v.old : "",
      new: isStr(v.new) ? v.new : "",
      ops: v.ops as DocumentOp[],
    });
  }
  if (variants.length === 0) return null;
  return {
    documentId,
    document: isStr(data.document) ? data.document : "",
    batchId,
    selectionText: isStr(data.selection_text) ? data.selection_text : "",
    variants,
  };
}

/** Batch id of one version of a proposal, as the editor ledger records it. */
export function proposalBatchId(batchId: string, variantId: string): string {
  return `${batchId}:${variantId}`;
}

/** The edit batch that applies `variant` of `proposal` in its document. */
export function proposalOpsBatch(proposal: RewriteProposal, variant: RewriteVariant): OpsBatch {
  return { batchId: proposalBatchId(proposal.batchId, variant.id), ops: variant.ops, documentId: proposal.documentId };
}

/** Whether an editor outcome means the version landed (at least one op). */
export function proposalOutcomeApplied(o: { kind: "result"; applied: number } | { kind: "timeout" }): boolean {
  return o.kind === "result" && o.applied > 0;
}

/** Proposals of one session that the user applied: batchId → variantId. */
const proposalsKey = (sessionId: string) => `werag.docops.proposals.${sessionId}`;

export function readAppliedProposals(store: KV | null | undefined, sessionId: string): Record<string, string> {
  try {
    const raw = store?.getItem(proposalsKey(sessionId));
    const v = raw ? JSON.parse(raw) : {};
    if (!isObj(v)) return {};
    const out: Record<string, string> = {};
    for (const [k, id] of Object.entries(v)) if (isStr(id)) out[k] = id;
    return out;
  } catch {
    return {};
  }
}

export function rememberAppliedProposal(
  store: KV | null | undefined,
  sessionId: string,
  batchId: string,
  variantId: string,
): void {
  try {
    const all = readAppliedProposals(store, sessionId);
    delete all[batchId];
    all[batchId] = variantId;
    const entries = Object.entries(all).slice(-LEDGER_MAX);
    store?.setItem(proposalsKey(sessionId), JSON.stringify(Object.fromEntries(entries)));
  } catch {
    /* storage unavailable: the card keeps the state until a reload */
  }
}

/** Batch id of the newest proposal of each document, in message order: only
 * that card can apply; older ones are read-only. */
export function newestProposalByDocument(proposals: Iterable<RewriteProposal>): Map<string, string> {
  const out = new Map<string, string>();
  for (const p of proposals) out.set(p.documentId, p.batchId);
  return out;
}

/* ---------- illustrations ---------- */

/* draw_diagram / generate_illustration propose a picture (Data.illustration):
 * a Mermaid diagram the browser draws, or an image the image model painted
 * (persisted as a message artifact, found by its sha256). "Chèn vào văn bản"
 * snapshots on the server (proposals/apply, kind illustration) and sends one
 * insertImage op as the batch `${batchId}:v1`. */

export type Illustration = {
  kind: "diagram" | "image";
  batchId: string;
  /** "" when there is no document to insert into */
  documentId: string;
  document: string;
  title: string;
  caption: string;
  /** null: no place known — download only */
  anchor: ParagraphAnchor | null;
  widthMm: number;
  mermaid?: string;
  prompt?: string;
  imageSha256?: string;
};

export const ILLUSTRATION_TOOLS = new Set(["draw_diagram", "generate_illustration"]);

/** The illustration a tool result proposes, or null. */
export function illustrationFromToolData(
  toolName: string | undefined,
  data: Record<string, unknown> | undefined,
): Illustration | null {
  const name = toolName || (isStr(data?.tool_name) ? data.tool_name : "");
  if (!ILLUSTRATION_TOOLS.has(name) || !data || !isObj(data.illustration)) return null;
  const ill = data.illustration;
  const batchId = isStr(data.ops_batch_id) ? data.ops_batch_id.trim() : "";
  const kind = ill.kind === "diagram" || ill.kind === "image" ? ill.kind : null;
  if (!batchId || !kind) return null;
  if (kind === "diagram" && !(isStr(ill.mermaid) && ill.mermaid.trim())) return null;
  if (kind === "image" && !(isStr(ill.image_sha256) && /^[0-9a-f]{64}$/.test(ill.image_sha256))) return null;
  const anchor = validAnchor(ill.anchor) ? (ill.anchor as ParagraphAnchor) : null;
  const width = typeof ill.width_mm === "number" && ill.width_mm > 20 && ill.width_mm <= 200 ? ill.width_mm : 150;
  return {
    kind,
    batchId,
    documentId: isStr(data.document_id) ? data.document_id.trim() : "",
    document: isStr(data.document) ? data.document : "",
    title: isStr(ill.title) ? ill.title : "",
    caption: isStr(ill.caption) ? ill.caption : "",
    anchor,
    widthMm: width,
    ...(kind === "diagram" ? { mermaid: ill.mermaid as string } : {}),
    ...(kind === "image" ? { imageSha256: ill.image_sha256 as string, prompt: isStr(ill.prompt) ? ill.prompt : "" } : {}),
  };
}

/** The edit batch inserting an illustration's picture (`src`, w×h pixels). */
export function illustrationOpsBatch(
  ill: Illustration,
  src: string,
  pixels: { width: number; height: number },
): OpsBatch {
  // a very tall diagram keeps to roughly one page
  const MAX_HEIGHT_MM = 230;
  let widthMm = ill.widthMm;
  let heightMm = Math.round((widthMm * pixels.height) / Math.max(1, pixels.width));
  if (heightMm > MAX_HEIGHT_MM) {
    widthMm = Math.round((widthMm * MAX_HEIGHT_MM) / heightMm);
    heightMm = MAX_HEIGHT_MM;
  }
  const op: DocumentOp = {
    op: "insertImage",
    anchor: ill.anchor ?? { atStart: true },
    src,
    widthMm,
    heightMm,
    ...(ill.caption ? { caption: ill.caption } : {}),
  };
  return { batchId: proposalBatchId(ill.batchId, "v1"), ops: [op], documentId: ill.documentId || undefined };
}

/* ---------- plugin messages ---------- */

export type PluginOpsMessage =
  | { type: "ops_ack"; batchId: string }
  | ({ type: "ops_result" } & OpsResult);

export function parsePluginOpsMessage(data: unknown): PluginOpsMessage | null {
  if (!isObj(data) || data.source !== "werag-onlyoffice" || !isStr(data.batchId) || !data.batchId) return null;
  if (data.type === "ops_ack") return { type: "ops_ack", batchId: data.batchId };
  if (data.type !== "ops_result") return null;
  const applied = typeof data.applied === "number" && Number.isFinite(data.applied) ? data.applied : 0;
  const failed: OpsFailure[] = Array.isArray(data.failed)
    ? data.failed
        .filter(isObj)
        .map((f) => ({ index: typeof f.index === "number" ? f.index : -1, error: isStr(f.error) ? f.error : "error" }))
    : [];
  return { type: "ops_result", batchId: data.batchId, applied, failed };
}

/* ---------- live spellcheck: paragraph texts from the plugin ---------- */

export function readParagraphsMessage(requestId: string) {
  return { source: "werag-host" as const, type: "read_paragraphs" as const, requestId };
}

/** The plugin's reply to read_paragraphs: texts null when the read failed. */
export function parsePluginParagraphsMessage(data: unknown): { requestId: string; texts: string[] | null } | null {
  if (!isObj(data) || data.source !== "werag-onlyoffice" || data.type !== "paragraphs") return null;
  if (!isStr(data.requestId) || !data.requestId) return null;
  const texts = Array.isArray(data.texts) ? data.texts.map((t) => (isStr(t) ? t : "")) : null;
  return { requestId: data.requestId, texts };
}

export function applyOpsMessage(batch: OpsBatch) {
  return { source: "werag-host" as const, type: "apply_ops" as const, batchId: batch.batchId, ops: batch.ops };
}

/* ---------- applied-batch ledger (survives reloads / stream replays) ---------- */

const LEDGER_MAX = 200;
const ledgerKey = (sessionId: string) => `werag.docops.applied.${sessionId}`;

type KV = Pick<Storage, "getItem" | "setItem">;

export function readAppliedBatches(store: KV | null | undefined, sessionId: string): string[] {
  try {
    const raw = store?.getItem(ledgerKey(sessionId));
    const v = raw ? JSON.parse(raw) : [];
    return Array.isArray(v) ? v.filter(isStr) : [];
  } catch {
    return [];
  }
}

export function rememberAppliedBatch(store: KV | null | undefined, sessionId: string, batchId: string): void {
  try {
    const list = readAppliedBatches(store, sessionId).filter((id) => id !== batchId);
    list.push(batchId);
    store?.setItem(ledgerKey(sessionId), JSON.stringify(list.slice(-LEDGER_MAX)));
  } catch {
    /* storage unavailable: the plugin still dedupes within the editor session */
  }
}
