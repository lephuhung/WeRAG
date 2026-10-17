/* The package pill's "water level": how much of the package is used up and
 * how many tokens are left. The level follows the limit closest to
 * exhaustion (any exhausted limit refuses the next question), the token
 * count follows the tightest token limit. Pure helpers so they run under
 * node --test. */

type Limit = { metric: string; period: string; limit: number; used: number; remaining: number; exhausted: boolean };
type Entitlement = { expired: boolean; mode: string; has_seat: boolean; limits: Limit[] };

export type UsageMeter = {
  /** 0–100: share used of the tightest capped limit; 100 when blocked. */
  level: number;
  /** Tightest capped token limit, or null when tokens are not capped. */
  tokens: Limit | null;
  /** Expired, seatless, or a limit is used up. */
  blocked: boolean;
};

function tightest(limits: Limit[], metric?: string): Limit | null {
  let best: Limit | null = null;
  let bestRatio = -1;
  for (const l of limits) {
    if (l.limit <= 0 || (metric && l.metric !== metric)) continue;
    const ratio = l.used / l.limit;
    if (ratio > bestRatio) {
      best = l;
      bestRatio = ratio;
    }
  }
  return best;
}

export function usageMeter(ent: Entitlement): UsageMeter {
  const blocked = ent.expired || (ent.mode === "seat" && !ent.has_seat) || ent.limits.some((l) => l.exhausted);
  const tight = tightest(ent.limits);
  const level = blocked ? 100 : tight ? Math.max(0, Math.min(100, Math.round((tight.used / tight.limit) * 100))) : 0;
  return { level, tokens: tightest(ent.limits, "tokens"), blocked };
}

/** Fill colour of the water: a pale blue when little is used, a deep navy
 * near the cap, rose once blocked. `ink` is the text colour readable on it. */
export function meterColors(level: number, blocked: boolean): { fill: string; ink: string } {
  if (blocked) return { fill: "hsl(350 75% 45%)", ink: "#fff" };
  const t = Math.max(0, Math.min(100, level)) / 100;
  const lightness = Math.round(86 - t * 58); // 86% → 28%
  const saturation = Math.round(85 - t * 15); // 85% → 70%
  const hue = Math.round(210 + t * 15); // 210 → 225
  return { fill: `hsl(${hue} ${saturation}% ${lightness}%)`, ink: lightness < 58 ? "#fff" : "hsl(222 70% 22%)" };
}
