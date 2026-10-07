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

export const DOCUMENT_OPS_TOOLS = new Set(["insert_paragraphs", "rewrite_paragraphs", "mark_passages", "apply_format_fixes"]);

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
  | { op: "mark"; anchor: ParagraphAnchor; text?: string; style: MarkStyle }
  | {
      op: "formatParagraph";
      anchor: ParagraphAnchor;
      alignment?: Alignment;
      font?: string;
      sizePt?: number;
      bold?: boolean;
      italic?: boolean;
    }
  | { op: "pageSetup"; marginsMm?: { top?: number; bottom?: number; left?: number; right?: number }; a4?: true };

export type OpsBatch = { batchId: string; ops: DocumentOp[] };

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
      return isStr(raw.style) && MARK_STYLES.has(raw.style) ? null : "bad style";
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
  return { batchId, ops, rejected };
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
