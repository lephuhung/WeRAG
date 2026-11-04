export const SPLIT_MIN_PCT = 55;
export const SPLIT_MAX_PCT = 75;
export const SPLIT_DEFAULT_PCT = 66;

/** Width limits of the left pane, in percent. */
export type SplitBounds = { min: number; max: number; def: number };

export const DEFAULT_SPLIT_BOUNDS: SplitBounds = { min: SPLIT_MIN_PCT, max: SPLIT_MAX_PCT, def: SPLIT_DEFAULT_PCT };

/** Clamp the editor (left) pane width in percent; garbage → default. */
export function clampLeftPct(v: number, bounds: SplitBounds = DEFAULT_SPLIT_BOUNDS): number {
  if (!Number.isFinite(v)) return bounds.def;
  return Math.min(bounds.max, Math.max(bounds.min, v));
}

/** Width of the draggable separator between the panes, in px. */
export const SPLIT_DIVIDER_PX = 6;

/** Bounds for a container `widthPx` wide that must leave the right pane at
 * least `minRightPx` (plus the separator). On a narrow window the ceiling
 * drops below the nominal one; the floor never exceeds the ceiling, so the
 * separator, its aria values and the rendered width always agree. */
export function effectiveBounds(bounds: SplitBounds, widthPx: number, minRightPx: number): SplitBounds {
  if (!Number.isFinite(widthPx) || widthPx <= 0) return bounds;
  const ceiling = Math.max(0, (100 * (widthPx - minRightPx - SPLIT_DIVIDER_PX)) / widthPx);
  const max = Math.min(bounds.max, ceiling);
  const min = Math.min(bounds.min, max);
  return { min, max, def: Math.min(Math.max(bounds.def, min), max) };
}
