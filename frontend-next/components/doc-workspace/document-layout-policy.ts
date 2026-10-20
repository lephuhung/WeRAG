/* One policy for the document editor's layout gate.
 *
 * The editor (ONLYOFFICE) needs room next to the assistant. Every host
 * that renders the editor reads its thresholds from here so the chat page
 * and the document workspace can never disagree about when the editor
 * fits.
 *
 *  · below `minWidth` the editor does not fit at all — the host shows a
 *    "screen too small" notice instead of the editor;
 *  · from `minWidth` up to `sideBySideMinWidth` the editor is stacked
 *    above the assistant (`stackedEditorHeight`);
 *  · from `sideBySideMinWidth` × `sideBySideMinHeight` on, the editor and
 *    the assistant sit side by side. */

export const DOCUMENT_EDITOR = {
  /** Below this viewport width the editor cannot be shown at all. */
  minWidth: 1024,
  /** The side-by-side layout needs at least this width… */
  sideBySideMinWidth: 1440,
  /** …and this height. */
  sideBySideMinHeight: 800,
  /** The stacked editor takes this share of the viewport height. */
  stackedEditorHeight: "55vh",
  /** Shown in the "screen too small" notice. */
  minSizeLabel: "1440 × 800 px",
} as const;

/** The editor and the assistant side by side. */
export const DOCUMENT_SIDE_BY_SIDE_QUERY = `(min-width: ${DOCUMENT_EDITOR.sideBySideMinWidth}px) and (min-height: ${DOCUMENT_EDITOR.sideBySideMinHeight}px)`;

/** The editor stacked above the assistant (still usable). */
export const DOCUMENT_STACKED_QUERY = `(min-width: ${DOCUMENT_EDITOR.minWidth}px)`;
