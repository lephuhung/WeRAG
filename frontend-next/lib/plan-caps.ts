/* What a plan allows beyond its day/month quota, one line per cap it sets:
 * shared by the public pricing page (caps) and the in-app plan cards
 * (features). Pure, so it runs under node --test. */

/** A plan's per-turn and per-session caps (usage_plans.features, known keys
 * only); an absent key is no cap. Mirrors types.UsagePlanFeatures. */
export interface PlanCaps {
  "agent.turn_token_cap"?: number;
  "agent.max_iterations"?: number;
  "context.history_tokens"?: number;
  "session.max_turns"?: number;
  "session.max_tokens"?: number;
}

const CAP_KEYS: (keyof PlanCaps)[] = [
  "session.max_turns",
  "session.max_tokens",
  "context.history_tokens",
  "agent.turn_token_cap",
  "agent.max_iterations",
];

/** The known caps of a plan's raw features object (GET /billing/plans):
 * positive whole numbers only. */
export function planCapsFromFeatures(features: Record<string, unknown> | null | undefined): PlanCaps {
  const out: PlanCaps = {};
  if (!features) return out;
  for (const k of CAP_KEYS) {
    const v = features[k];
    const n = typeof v === "number" ? v : typeof v === "string" && /^\d+$/.test(v) ? Number(v) : NaN;
    if (Number.isFinite(n) && n > 0) out[k] = Math.floor(n);
  }
  return out;
}

type Translate = (key: never, vars?: Record<string, string | number>) => string;

const num = (n: number) => n.toLocaleString("vi-VN");

/** One line per cap ("Tối đa 15 câu hỏi mỗi cuộc trò chuyện"…), led for a
 * Tổ chức plan by how its quota is shared. Texts are the pricing.cap* keys. */
export function capLines(
  caps: PlanCaps | undefined,
  mode: "pool" | "seat" | string,
  scope: "user" | "tenant",
  translate: Translate,
): string[] {
  const t = translate as unknown as (key: string, vars?: Record<string, string | number>) => string;
  const lines: string[] = [];
  if (scope === "tenant") lines.push(mode === "seat" ? t("pricing.capSeat") : t("pricing.capShared"));
  const c = caps ?? {};
  if (c["session.max_turns"]) lines.push(t("pricing.capTurns", { n: num(c["session.max_turns"]) }));
  if (c["session.max_tokens"]) lines.push(t("pricing.capTokens", { n: num(c["session.max_tokens"]) }));
  if (c["context.history_tokens"]) lines.push(t("pricing.capHistory", { n: num(c["context.history_tokens"]) }));
  if (c["agent.turn_token_cap"]) lines.push(t("pricing.capAgentTokens", { n: num(c["agent.turn_token_cap"]) }));
  if (c["agent.max_iterations"]) lines.push(t("pricing.capAgentSteps", { n: num(c["agent.max_iterations"]) }));
  return lines;
}
