/* Surfaces that only work at a certain size.
 *
 * Some views are read at a glance and driven by dragging: a graph canvas, a
 * two-pane editor. Squeezed onto a phone they still *render*, but nothing can
 * be read or grabbed there — a picture the user cannot use is worse than a
 * sentence saying why. Each of those surfaces gets a threshold here, and the
 * gate (components/large-view-only.tsx) reads it, the same way
 * document-layout-policy.ts holds the editor's numbers.
 *
 * A surface's `sizeLabel` is what the notice shows the user; keep it in step
 * with the numbers next to it. */

export type LargeViewId = "graph" | "timeline";

export type LargeView = { minWidth: number; minHeight: number; sizeLabel: string };

export const LARGE_VIEW = {
  /** The knowledge-base graph (wiki / entity / legal): dragged and zoomed,
   *  node labels collide well before a tablet gets narrow. */
  graph: { minWidth: 900, minHeight: 520, sizeLabel: "900 × 520 px" },
  /** The processing waterfall: a fixed 220 px name column, a duration column
   *  and the bars, in a drawer that is at most 96vw wide — on a phone the
   *  bars are left with a few dozen pixels. */
  timeline: { minWidth: 760, minHeight: 400, sizeLabel: "760 × 400 px" },
} satisfies Record<LargeViewId, LargeView>;

/** True when the surface has the room it needs. Pure, so the decision table
 *  is testable without a browser. */
export function fitsLargeView(id: LargeViewId, width: number, height: number): boolean {
  const view: LargeView = LARGE_VIEW[id];
  return width >= view.minWidth && height >= view.minHeight;
}

/** The same decision as a media query, for the in-browser gate. */
export function largeViewQuery(id: LargeViewId): string {
  const view = LARGE_VIEW[id];
  return `(min-width: ${view.minWidth}px) and (min-height: ${view.minHeight}px)`;
}
