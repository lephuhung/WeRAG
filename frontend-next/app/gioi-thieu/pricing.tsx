"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { useT, type LocaleKey } from "@/lib/i18n";
import {
  formatPriceVND,
  listPublicPlans,
  planPrice,
  type PublicUsagePlan,
  type UsagePlanCaps,
} from "@/lib/api/usage";
import styles from "./page.module.css";

const num = (n: number) => n.toLocaleString("vi-VN");

/* Every plan gets its own accent. The variant is picked from the plan's ID
 * — not from its position in the grid — so switching the Cá nhân / Tổ chức
 * tab keeps the same colour on the same plan.
 *
 * Which plan is the recommended one is NOT decided yet (the catalogue is
 * still being tested): no plan is highlighted, and .planFeatured stays
 * unused until that tier is picked. */
const PLAN_ACCENTS = ["planAccent0", "planAccent1", "planAccent2", "planAccent3"] as const;

/** A stable accent variant for a plan id (the same plan keeps its colour). */
function planAccent(planId: string): string {
  let hash = 0;
  for (let i = 0; i < planId.length; i++) hash = (hash * 31 + planId.charCodeAt(i)) >>> 0;
  return styles[PLAN_ACCENTS[hash % PLAN_ACCENTS.length]];
}

/* The plan the page highlights. The accent used to follow the card's
 * position in the grid, so switching the Cá nhân / Tổ chức tab recoloured
 * the same plan. The flag is now explicit: a plan that names itself as the
 * recommended tier wins, otherwise the most expensive plan of the
 * catalogue is highlighted. */
const FEATURED_PLAN_NAMES: Record<string, true> = {
  pro: true,
  professional: true,
  business: true,
  team: true,
  enterprise: true,
  "nâng cao": true,
  "chuyên nghiệp": true,
};

type Translate = (key: LocaleKey, vars?: Record<string, string | number>) => string;

/* What a plan allows beyond its day/month quota, one line per cap it sets. */
function capLines(
  caps: UsagePlanCaps | undefined,
  mode: PublicUsagePlan["mode"],
  scope: "user" | "tenant",
  t: Translate,
): string[] {
  const lines: string[] = [];
  if (scope === "tenant") {
    lines.push(mode === "seat" ? t("pricing.capSeat") : t("pricing.capShared"));
  }
  const c = caps ?? {};
  if (c["session.max_turns"]) {
    lines.push(t("pricing.capTurns", { n: num(c["session.max_turns"]) }));
  }
  if (c["session.max_tokens"]) {
    lines.push(t("pricing.capTokens", { n: num(c["session.max_tokens"]) }));
  }
  if (c["context.history_tokens"]) {
    lines.push(t("pricing.capHistory", { n: num(c["context.history_tokens"]) }));
  }
  if (c["agent.turn_token_cap"]) {
    lines.push(t("pricing.capAgentTokens", { n: num(c["agent.turn_token_cap"]) }));
  }
  if (c["agent.max_iterations"]) {
    lines.push(t("pricing.capAgentSteps", { n: num(c["agent.max_iterations"]) }));
  }
  return lines;
}

export function LandingPricing() {
  const { t } = useT();
  const [scope, setScope] = useState<"user" | "tenant">("user");
  const [plans, setPlans] = useState<PublicUsagePlan[]>([]);
  const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    setStatus("loading");
    listPublicPlans(scope, controller.signal)
      .then((result) => {
        if (!controller.signal.aborted) {
          setPlans(result);
          setStatus("ready");
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) setStatus("error");
      });
    return () => controller.abort();
  }, [scope, attempt]);

  return (
    <div>
      <div className={styles.scope} role="group" aria-label={t("pricing.scopeAria")}>
        <button
          type="button"
          aria-pressed={scope === "user"}
          onClick={() => setScope("user")}
        >
          {t("pricing.scopeUser")}
        </button>
        <button
          type="button"
          aria-pressed={scope === "tenant"}
          onClick={() => setScope("tenant")}
        >
          {t("pricing.scopeTenant")}
        </button>
      </div>
      <div aria-live="polite" aria-busy={status === "loading"}>
        {status === "loading" ? (
          <p className={styles.pricingNote}>{t("pricing.loading")}</p>
        ) : status === "error" ? (
          <div className={styles.pricingNote}>
            <p>{t("pricing.loadError")}</p>
            <button
              type="button"
              className={styles.retryButton}
              onClick={() => setAttempt((value) => value + 1)}
            >
              {t("pricing.retry")}
            </button>
          </div>
        ) : plans.length === 0 ? (
          <p className={styles.pricingNote}>{t("pricing.empty")}</p>
        ) : (
          <div className={styles.planGrid}>
            {plans.map((plan) => {
              const price = planPrice(plan);
              const caps = capLines(plan.caps, plan.mode, scope, t);
              return (
                <article
                  key={plan.id}
                  className={`${styles.plan} ${planAccent(plan.id)}`}
                >
                  <p className={styles.eyebrow}>
                    {plan.mode === "seat"
                      ? t("pricing.modeSeat")
                      : scope === "user"
                        ? t("pricing.modeUser")
                        : t("pricing.modeTenant")}
                  </p>
                  <h3>{plan.name}</h3>
                  <p>{plan.description}</p>
                  {price ? (
                    <div className={styles.price}>
                      {price}
                      <small>
                        {" "}
                        {plan.mode === "seat" ? `${t("pricing.perSeat")} ` : ""}
                        {t("pricing.perMonth")}
                      </small>
                    </div>
                  ) : (
                    /* No online price for this plan: no empty amount with a
                       dangling "/ tháng" — the buyer is told to ask. */
                    <p className={styles.pricingNote}>{t("pricing.contactAdmin")}</p>
                  )}
                  <div className={styles.planDetails}>
                    <ul className={styles.planLimits}>
                      {(plan.limits ?? []).map((limit, i) => (
                        <li key={i}>
                          {limit.limit === 0
                            ? t("pricing.unlimited")
                            : limit.limit.toLocaleString("vi-VN")}{" "}
                          {limit.metric === "questions"
                            ? t("pricing.questions")
                            : t("pricing.token")}{" "}
                          / {limit.period === "day" ? t("pricing.day") : t("pricing.month")}
                        </li>
                      ))}
                    </ul>
                    {caps.length > 0 && (
                      <div className={styles.planAgents}>
                        <h4>{t("pricing.benefits")}</h4>
                        <ul>
                          {caps.map((line) => (
                            <li key={line}>{line}</li>
                          ))}
                        </ul>
                      </div>
                    )}
                    {!!plan.included_agents?.length && (
                      <div className={styles.planAgents}>
                        <h4>{t("pricing.includedAgents")}</h4>
                        <ul>
                          {plan.included_agents.map((agent) => (
                            <li key={agent.id}>{agent.name}</li>
                          ))}
                        </ul>
                      </div>
                    )}
                    {!!plan.addons?.length && (
                      <div className={styles.planAddons}>
                        <h4>{t("pricing.addons")}</h4>
                        <ul>
                          {plan.addons.map((addon) => (
                            <li key={addon.agent_id}>
                              <strong>{addon.name}</strong>
                              {addon.description && <p>{addon.description}</p>}
                              <span>
                                {addon.price_vnd > 0
                                  ? `+${formatPriceVND(addon.price_vnd)}`
                                  : t("pricing.noExtraFee")}
                                {addon.price_vnd > 0 &&
                                  (addon.per_seat
                                    ? ` ${t("pricing.perSeatMonth")}`
                                    : ` ${t("pricing.perMonth")}`)}
                              </span>
                            </li>
                          ))}
                        </ul>
                      </div>
                    )}
                  </div>
                  <Link href="/platform/creatChat" className={styles.planCta}>
                    {t("pricing.chooseInApp")} <span aria-hidden="true">↗</span>
                  </Link>
                </article>
              );
            })}
          </div>
        )}
      </div>
      <p className={styles.pricingNote}>{t("pricing.note")}</p>
    </div>
  );
}
