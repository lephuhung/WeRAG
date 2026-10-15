/* Per-session caps of the usage plan (backend types.UsageSessionStatus):
 * how full a session is, read from the `complete` event's `session_usage`,
 * and the plan feature keys the SuperAdmin plan editor sets. */

export type SessionUsage = {
  sessionId: string;
  turns: number;
  maxTurns: number;
  tokens: number;
  maxTokens: number;
  /** A cap is used up (the next question is refused unless soft). */
  reached: boolean;
  /** A reached cap only warns: document-bound sessions carry on. */
  soft: boolean;
};

const num = (v: unknown) => (typeof v === "number" && Number.isFinite(v) ? v : 0);

/** The session's standing from a `complete` event's data, when the backend
 * flagged it (80% of a cap or more); null otherwise. */
export function sessionUsageFromComplete(data: unknown): SessionUsage | null {
  const raw = (data as { session_usage?: unknown } | undefined)?.session_usage;
  if (!raw || typeof raw !== "object") return null;
  const r = raw as Record<string, unknown>;
  if (r.warning !== true) return null;
  return {
    sessionId: typeof r.session_id === "string" ? r.session_id : "",
    turns: num(r.turns),
    maxTurns: num(r.max_turns),
    tokens: num(r.tokens),
    maxTokens: num(r.max_tokens),
    reached: r.reached === true,
    soft: r.soft === true,
  };
}

/** Questions used after the turn that just finished (the status is read
 * before the turn, so the turn itself is added). */
export function turnsUsed(u: SessionUsage): number {
  return u.turns + 1;
}

/** Share of the token cap used, in whole percent (0 without a token cap). */
export function tokenPercent(u: SessionUsage): number {
  if (u.maxTokens <= 0) return 0;
  return Math.min(100, Math.round((u.tokens / u.maxTokens) * 100));
}

/** Plan feature keys (usage_plans.features); absent or 0 = no cap. */
export const PLAN_FEATURE_KEYS = [
  "agent.turn_token_cap",
  "agent.max_iterations",
  "context.history_tokens",
  "session.max_turns",
  "session.max_tokens",
] as const;

export type PlanFeatureKey = (typeof PLAN_FEATURE_KEYS)[number];

/** The value of one feature key as the editor shows it (0 = unset). */
export function planFeatureValue(features: Record<string, unknown> | undefined, key: PlanFeatureKey): number {
  const v = features?.[key];
  return typeof v === "number" && v > 0 ? Math.floor(v) : 0;
}

/** features with key set to value; 0 removes the key. Other keys —
 * including ones this editor does not know — are kept as they are. */
export function withPlanFeature(
  features: Record<string, unknown> | undefined,
  key: PlanFeatureKey,
  value: number,
): Record<string, unknown> {
  const out = { ...(features ?? {}) };
  const n = Math.max(0, Math.floor(Number.isFinite(value) ? value : 0));
  if (n > 0) out[key] = n;
  else delete out[key];
  return out;
}
