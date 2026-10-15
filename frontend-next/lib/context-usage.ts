/* The chat's context indicator: how full the history of the session's next
 * turn is (GET /sessions/:id/context-usage, backend
 * types.SessionContextUsage), with the plan's per-session caps. Pure
 * helpers so they run under node --test. */

export type ContextUsage = {
  model_window: number;
  history_budget: number;
  history_tokens: number;
  summary_tokens: number;
  compacted: boolean;
  compacted_turns: number;
  seeded: boolean;
  mode: string;
  last_turn: { prompt_tokens: number; completion_tokens: number; rounds: number } | null;
  session: {
    turns: number;
    max_turns: number;
    tokens: number;
    max_tokens: number;
    warning: boolean;
    reached: boolean;
    soft: boolean;
  };
  turn_token_cap: number;
  plan_name: string;
  enforced: boolean;
};

export type ContextTone = "neutral" | "amber" | "red";

const num = (v: unknown) => (typeof v === "number" && Number.isFinite(v) && v > 0 ? v : 0);

/** The endpoint's data, with missing or malformed fields read as 0/false;
 * null when it is not an object. */
export function parseContextUsage(raw: unknown): ContextUsage | null {
  if (!raw || typeof raw !== "object") return null;
  const r = raw as Record<string, unknown>;
  const s = (r.session && typeof r.session === "object" ? r.session : {}) as Record<string, unknown>;
  const lt = r.last_turn && typeof r.last_turn === "object" ? (r.last_turn as Record<string, unknown>) : null;
  return {
    model_window: num(r.model_window),
    history_budget: num(r.history_budget),
    history_tokens: num(r.history_tokens),
    summary_tokens: num(r.summary_tokens),
    compacted: r.compacted === true,
    compacted_turns: num(r.compacted_turns),
    seeded: r.seeded === true,
    mode: typeof r.mode === "string" ? r.mode : "",
    last_turn: lt
      ? { prompt_tokens: num(lt.prompt_tokens), completion_tokens: num(lt.completion_tokens), rounds: num(lt.rounds) }
      : null,
    session: {
      turns: num(s.turns),
      max_turns: num(s.max_turns),
      tokens: num(s.tokens),
      max_tokens: num(s.max_tokens),
      warning: s.warning === true,
      reached: s.reached === true,
      soft: s.soft === true,
    },
    turn_token_cap: num(r.turn_token_cap),
    plan_name: typeof r.plan_name === "string" ? r.plan_name : "",
    enforced: r.enforced === true,
  };
}

/** How full the history is, 0..1 (history over its budget; the window
 * when the budget is unknown). */
export function contextFill(u: ContextUsage): number {
  const budget = u.history_budget || u.model_window;
  if (budget <= 0) return 0;
  return Math.min(1, u.history_tokens / budget);
}

/** contextFill in whole percent. */
export function contextPercent(u: ContextUsage): number {
  return Math.round(contextFill(u) * 100);
}

/** Neutral below 60%, amber from 60% to 85%, red above 85%. */
export function contextTone(fill: number): ContextTone {
  if (fill > 0.85) return "red";
  if (fill >= 0.6) return "amber";
  return "neutral";
}

/** A token count, short: 950, 12,3k (vi) / 12.3k (en), 250k, 1,2M. One
 * decimal below 100k / 100M, none above. */
export function formatTokens(n: number, locale: string): string {
  const v = Math.max(0, Math.round(n));
  const fmt = (x: number, digits: number) =>
    new Intl.NumberFormat(locale, { maximumFractionDigits: digits }).format(x);
  if (v < 1000) return fmt(v, 0);
  if (v < 1_000_000) {
    const k = v / 1000;
    return `${fmt(k, k < 100 ? 1 : 0)}k`;
  }
  const m = v / 1_000_000;
  return `${fmt(m, m < 100 ? 1 : 0)}M`;
}

/** A plain count with the locale's grouping (5, 1.234 / 1,234). */
export function formatCount(n: number, locale: string): string {
  return new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }).format(Math.max(0, Math.round(n)));
}
