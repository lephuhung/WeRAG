"use client";

/* The package a new account receives (SuperAdmin setting
 * usage.signup_plan_id), on the landing page: a line under the hero and a
 * card above the paid plans. Read from the public endpoint, so it always
 * matches what sign-up grants — nothing here hard-codes the quota. */

import Link from "next/link";
import { useEffect, useState } from "react";
import { useT } from "@/lib/i18n";
import { getPublicSignupGift, type PublicSignupGift } from "@/lib/api/usage";
import styles from "./page.module.css";

/** The gift, or null while loading, when none is configured, or on error
 * (the page then just leaves it out). */
export function useSignupGift(): PublicSignupGift | null {
  const [gift, setGift] = useState<PublicSignupGift | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    getPublicSignupGift(controller.signal)
      .then((g) => {
        if (!controller.signal.aborted) setGift(g);
      })
      .catch(() => {});
    return () => controller.abort();
  }, []);
  return gift;
}

/** "1.500.000 token/tháng, dùng trong 30 ngày" — the gift's quota and
 * length; the plan's name when it carries no quota line. */
export function useGiftOffer(gift: PublicSignupGift | null): string {
  const { t } = useT();
  if (!gift) return "";
  const limits = (gift.limits ?? [])
    .map(
      (l) =>
        `${l.limit === 0 ? t("pricing.unlimited") : l.limit.toLocaleString("vi-VN")} ${
          l.metric === "questions" ? t("pricing.questions") : t("pricing.token")
        }/${l.period === "day" ? t("pricing.day") : t("pricing.month")}`,
    )
    .join(", ");
  const offer = limits || gift.name;
  if (gift.days <= 0) return offer;
  return gift.days === 1
    ? t("landing.giftOfferOneDay", { limits: offer })
    : t("landing.giftOfferDays", { limits: offer, days: gift.days });
}

/** The card above the paid plans; nothing without a gift. */
export function LandingGiftCard({ gift }: { gift: PublicSignupGift | null }) {
  const { t } = useT();
  const offer = useGiftOffer(gift);
  if (!gift || !offer) return null;
  const agents = gift.included_agents.map((a) => a.name).join(", ");
  return (
    <div className={styles.giftCard} role="group" aria-labelledby="landing-gift-title">
      <div>
        <p className={styles.eyebrow}>{t("landing.giftEyebrow")}</p>
        <h3 id="landing-gift-title">{gift.name}</h3>
        <p className={styles.giftOffer}>{t("landing.giftHero", { offer })}</p>
        {agents && <p className={styles.giftAgents}>{t("landing.giftAgents", { agents })}</p>}
      </div>
      <Link href="/register" className={styles.primary}>
        {t("landing.giftCta")} <span aria-hidden="true">↗</span>
      </Link>
    </div>
  );
}
