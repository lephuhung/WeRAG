export const SPLIT_MIN_PCT = 55;
export const SPLIT_MAX_PCT = 75;
export const SPLIT_DEFAULT_PCT = 66;

/** Clamp the editor (left) pane width in percent; garbage → default. */
export function clampLeftPct(v: number): number {
  if (!Number.isFinite(v)) return SPLIT_DEFAULT_PCT;
  return Math.min(SPLIT_MAX_PCT, Math.max(SPLIT_MIN_PCT, v));
}
