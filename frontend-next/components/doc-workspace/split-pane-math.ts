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
