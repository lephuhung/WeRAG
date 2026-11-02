/* The status colour language of the knowledge surfaces.
 *
 * Two vocabularies live here:
 *  · the document parse status (pending / processing / finalizing /
 *    completed / failed / cancelled / deleting) the KB page, the document
 *    panel and the upload tasks show;
 *  · the trace span status (done / running / failed / pending / skipped /
 *    cancelled) the processing waterfall shows.
 *
 * Every colour is a CSS custom property (never a hex) so the dark mode
 * and the Anthropic preset follow it. */

import type { LocaleKey } from "@/lib/i18n";

/* ---------- Document parse status ---------- */

export interface KnowledgeStatusStyle {
  /** The i18n key of the label. */
  labelKey: LocaleKey;
  /** The English label, used when the key is missing. */
  fallback: string;
  /** The text colour utility. */
  cls: string;
  /** The status dot colour (a CSS custom property). */
  dot: string;
}

export const KNOWLEDGE_STATUS: Record<string, KnowledgeStatusStyle> = {
  completed: { labelKey: "status.indexed", fallback: "Indexed", cls: "text-success", dot: "var(--color-success)" },
  processing: { labelKey: "status.processing", fallback: "Processing", cls: "text-running", dot: "var(--color-running)" },
  finalizing: { labelKey: "status.processing", fallback: "Processing", cls: "text-running", dot: "var(--color-running)" },
  pending: { labelKey: "status.pending", fallback: "Pending", cls: "text-muted", dot: "var(--color-muted-soft)" },
  failed: { labelKey: "status.failed", fallback: "Failed", cls: "text-error", dot: "var(--color-error)" },
  cancelled: { labelKey: "status.cancelled", fallback: "Cancelled", cls: "text-muted", dot: "var(--color-muted-soft)" },
  deleting: { labelKey: "status.deleting", fallback: "Deleting", cls: "text-running", dot: "var(--color-running)" },
};

/** The style of a document's parse status; unknown statuses read as pending. */
export function knowledgeStatusStyle(status?: string): KnowledgeStatusStyle {
  return KNOWLEDGE_STATUS[status ?? ""] ?? KNOWLEDGE_STATUS.pending;
}

/* ---------- Trace span status ---------- */

export const STAGES = ["docreader", "chunking", "embedding", "multimodal", "postprocess"] as const;

export const STAGE_LABEL: Record<string, LocaleKey> = {
  docreader: "ks.stage.docreader",
  chunking: "ks.stage.chunking",
  embedding: "ks.stage.embedding",
  multimodal: "ks.stage.multimodal",
  postprocess: "ks.stage.postprocess",
};

export const STATUS_LABEL: Record<string, LocaleKey> = {
  pending: "ks.status.pending",
  running: "ks.status.running",
  processing: "ks.status.running",
  finalizing: "ks.status.finalizing",
  done: "ks.status.done",
  completed: "ks.status.done",
  failed: "ks.status.failed",
  skipped: "ks.status.skipped",
  cancelled: "ks.status.cancelled",
};

export const DOT_CLS: Record<string, string> = {
  done: "bg-success",
  completed: "bg-success",
  failed: "bg-error",
  running: "bg-running kp-live-pulse",
  processing: "bg-running kp-live-pulse",
  finalizing: "bg-running kp-live-pulse",
  pending: "border border-hairline-strong bg-transparent",
  skipped: "bg-muted-soft/40",
  cancelled: "border border-dashed border-muted-soft bg-transparent",
};

export const BAR_CLS: Record<string, string> = {
  done: "bg-success",
  completed: "bg-success",
  failed: "bg-error",
  running: "kp-bar-running kp-bar-sweep",
  processing: "kp-bar-running kp-bar-sweep",
  finalizing: "kp-bar-running kp-bar-sweep",
  skipped: "bg-muted-soft/40",
  cancelled: "border border-dashed border-error/70 bg-transparent",
};

export const WRAP_CLS: Record<string, string> = {
  done: "border-success/40",
  completed: "border-success/40",
  failed: "border-error/50",
  running: "border-running/50",
  processing: "border-running/50",
  finalizing: "border-running/50",
  cancelled: "border-error/30",
};

export const CHIP_CLS: Record<string, string> = {
  done: "bg-success/10 text-success",
  completed: "bg-success/10 text-success",
  running: "bg-running/15 text-running",
  processing: "bg-running/15 text-running",
  finalizing: "bg-running/15 text-running",
  failed: "bg-error/10 text-error",
  cancelled: "bg-surface-strong text-muted",
  skipped: "bg-surface-strong text-muted-soft",
  pending: "bg-surface-strong text-muted",
};

/** The dot of a span (the placeholder rows show an empty dashed dot). */
export function dotCls(status: string, placeholder = false) {
  if (placeholder) return "border border-dashed border-hairline-strong bg-transparent";
  return DOT_CLS[status] ?? "bg-muted-soft";
}

/** The bar of a span. */
export function barCls(status: string) {
  return BAR_CLS[status] ?? "bg-muted-soft";
}
