/* One policy for the document editor's layout gate.
 *
 * The editor (ONLYOFFICE) needs room next to the assistant. Every host
 * that renders the editor reads its thresholds from here so the chat page
 * and the document workspace can never disagree about when the editor
 * fits.
 *
 *  · from `sideBySideMinWidth` × `minHeight` on, the editor and the
 *    assistant sit side by side; below `roomyWidth` the app rail folds
 *    away while a document is open (a 1024 / 1366 px laptop has no room
 *    for rail + editor + assistant), the edge tab brings it back;
 *  · from `stackedMinWidth` up to `sideBySideMinWidth` (tablets, where the
 *    rail is already a drawer) the editor is stacked above the assistant;
 *  · below `minHeight`, or narrower than `stackedMinWidth`, the editor does
 *    not fit at all — the host shows a "screen too small" notice instead. */

export const DOCUMENT_EDITOR = {
  /** Editor and assistant side by side from this viewport width… */
  sideBySideMinWidth: 1024,
  /** …the rail stays docked next to them from this width on (1920 px at
   * 125 % zoom; below it a docked rail costs the editor ~260 px). */
  roomyWidth: 1536,
  /** Below this width (and above `stackedMinWidth`) the panes stack. */
  stackedMinWidth: 768,
  /** Below this viewport height the editor is not shown at all. */
  minHeight: 560,
  /** The assistant column never gets narrower than this (side by side). */
  minAssistantPx: 360,
  /** The stacked editor takes this share of the viewport height. */
  stackedEditorHeight: "55vh",
  /** The "window too low" notice: small enough to leave the chat usable. */
  blockedNoticeHeight: "min(55vh, 230px)",
  /** Shown in the "screen too small" notice. */
  minSizeLabel: "768 × 560 px",
} as const;

/** The editor and the assistant side by side. */
export const DOCUMENT_SIDE_BY_SIDE_QUERY = `(min-width: ${DOCUMENT_EDITOR.sideBySideMinWidth}px) and (min-height: ${DOCUMENT_EDITOR.minHeight}px)`;

/** The editor stacked above the assistant (still usable). */
export const DOCUMENT_STACKED_QUERY = `(min-width: ${DOCUMENT_EDITOR.stackedMinWidth}px) and (min-height: ${DOCUMENT_EDITOR.minHeight}px)`;

/** Wide enough to keep the app rail docked next to an open document. */
export const DOCUMENT_ROOMY_QUERY = `(min-width: ${DOCUMENT_EDITOR.roomyWidth}px)`;

/** Side-by-side breakpoint of a non-document split (the legal draft review). */
export const PANES_SIDE_BY_SIDE_QUERY = `(min-width: ${DOCUMENT_EDITOR.sideBySideMinWidth}px)`;

export type DocumentLayout = "side" | "side-folded" | "stacked" | "blocked";

/** The layout of a document page (editor present) for a viewport. Pure, so
 * the decision table is testable; SplitPane evaluates the same thresholds
 * through matchMedia. `desktopShell` = the rail is docked (≥ 1024 px). */
export function resolveDocumentLayout(width: number, height: number, desktopShell: boolean): DocumentLayout {
  const e = DOCUMENT_EDITOR;
  if (height < e.minHeight || width < e.stackedMinWidth) return "blocked";
  if (width < e.sideBySideMinWidth) return "stacked";
  return desktopShell && width < e.roomyWidth ? "side-folded" : "side";
}
