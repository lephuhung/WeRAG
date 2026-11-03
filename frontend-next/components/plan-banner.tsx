"use client";

/* The package banner above every page of the app: a welcome with the
 * packages on offer for a new account, and a renew/upgrade call when the
 * package has run out, is about to expire or is nearly used up. Its button
 * opens the billing page on the right package, so buying no longer starts
 * with a trip through Settings. Which banner shows is decided in
 * lib/plan-banner.ts. */
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  formatPriceVND,
  getBillingInfo,
  getMyUsage,
  getTenantUsage,
  listPurchasablePlans,
  planTermPrices,
  type UsageBillingInfo,
  type UsagePlan,
  type UsageSummary,
  type UsageTenantUsage,
} from "@/lib/api/usage";
import { useAuth, useTenantRole } from "@/lib/auth";
import { openBilling } from "@/lib/billing-intent";
import { planNameAfterWord } from "@/lib/billing-order";
import { formatDate } from "@/lib/datetime";
import { useT, type LocaleKey } from "@/lib/i18n";
import { lastUsableDay, planBanner, type PlanBanner } from "@/lib/plan-banner";
import { IconClose } from "@/components/icons";

const REFRESH_MS = 5 * 60_000;
const FOCUS_REFRESH_MS = 60_000;
const DISMISS_PREFIX = "werag_plan_banner";

function dismissStorageKey(userId: string, key: string) {
  return `${DISMISS_PREFIX}:${userId}:${key}`;
}

/* One key per banner and subscription; the value is the VN day it was
 * hidden for (or "forever"), so daily dismissals expire on their own. */
function storedDismissal(userId: string, key: string): string | null {
  try {
    return localStorage.getItem(dismissStorageKey(userId, key));
  } catch {
    return null;
  }
}

const TONE_CLASS: Record<PlanBanner["tone"], string> = {
  error: "border-error/30 bg-error-surface text-error",
  warning: "border-warning/30 bg-warning-surface text-warning",
  info: "border-info/30 bg-info-surface text-info",
};

export function PlanBannerBar() {
  const { t } = useT();
  const auth = useAuth();
  const { isSystemAdmin, isTenantAdmin } = useTenantRole();
  const userId = auth.user?.id ?? "";
  const tenantKey = String(auth.selectedTenantId ?? auth.tenant?.id ?? "");
  const [usage, setUsage] = useState<UsageSummary | null>(null);
  // the Tổ chức package's add-ons and chỗ, for its admin; undefined while
  // loading, so the no_seat button does not flip from "add" to "give"
  const [tenantUsage, setTenantUsage] = useState<UsageTenantUsage | null | undefined>(undefined);
  // undefined while loading: the banner waits for it, so the buy button
  // does not pop in after the text
  const [info, setInfo] = useState<UsageBillingInfo | null | undefined>(undefined);
  const [plans, setPlans] = useState<UsagePlan[]>([]);
  // bumped on dismiss so the banner re-reads localStorage
  const [dismissTick, setDismissTick] = useState(0);

  const adminTenant = isTenantAdmin && !isSystemAdmin && tenantKey !== "" && tenantKey !== "0" ? tenantKey : "";
  const refresh = useCallback(() => {
    getMyUsage()
      .then((r) => setUsage(r.data ?? null))
      .catch(() => {
        /* non-fatal: no banner */
      });
    if (adminTenant) {
      getTenantUsage(adminTenant)
        .then(setTenantUsage)
        .catch(() => setTenantUsage(null));
    }
  }, [adminTenant]);

  useEffect(() => {
    if (!userId || isSystemAdmin) return;
    // a Tổ chức switch must not show the old Tổ chức's package
    setUsage(null);
    setTenantUsage(undefined);
    refresh();
    getBillingInfo()
      .then(setInfo)
      .catch(() => setInfo(null));
    let last = Date.now();
    const onFocus = () => {
      if (Date.now() - last < FOCUS_REFRESH_MS) return;
      last = Date.now();
      refresh();
    };
    const timer = window.setInterval(() => {
      last = Date.now();
      refresh();
    }, REFRESH_MS);
    window.addEventListener("focus", onFocus);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("focus", onFocus);
    };
  }, [userId, tenantKey, isSystemAdmin, refresh]);

  const banner = useMemo(() => {
    void dismissTick;
    if (!usage || !auth.user || info === undefined) return null;
    if (adminTenant && tenantUsage === undefined) return null;
    return planBanner({
      now: new Date(),
      isSystemAdmin,
      isTenantAdmin,
      hasTenant: tenantKey !== "" && tenantKey !== "0",
      enforced: usage.enforced,
      entitlement: usage.entitlement,
      userCreatedAt: auth.user.created_at ?? "",
      dismissed: (key) => storedDismissal(userId, key),
      tenantAddons: tenantUsage?.addons ?? [],
      tenantEndedAddons: tenantUsage?.ended_addons ?? [],
      tenantSeats:
        tenantUsage?.subscription?.mode === "seat"
          ? { total: tenantUsage.subscription.seats, used: tenantUsage.subscription.seats_used }
          : null,
    });
  }, [usage, tenantUsage, adminTenant, auth.user, info, isSystemAdmin, isTenantAdmin, tenantKey, userId, dismissTick]);

  // the welcome banner lists the packages on offer (when they can be bought)
  const welcomeScope = banner?.kind === "welcome" && info?.enabled === true ? banner.scope : null;
  useEffect(() => {
    if (!welcomeScope) return;
    let cancelled = false;
    listPurchasablePlans(welcomeScope)
      .then((pl) => {
        if (!cancelled) setPlans(pl.filter((p) => planTermPrices(p).length > 0).slice(0, 3));
      })
      .catch(() => {
        if (!cancelled) setPlans([]);
      });
    return () => {
      cancelled = true;
    };
  }, [welcomeScope]);

  // with billing off, a welcome without a package has nothing to show
  if (!banner || (banner.kind === "welcome" && !usage?.entitlement && info?.enabled !== true)) return null;

  const dismiss = () => {
    try {
      localStorage.setItem(dismissStorageKey(userId, banner.dismissKey), banner.dismissValue);
    } catch {
      /* storage disabled: hidden until the next reload */
    }
    setDismissTick((n) => n + 1);
  };

  const billingOn = info?.enabled === true;
  const buy = () =>
    banner.assignSeat
      ? window.dispatchEvent(new CustomEvent("weknora:open-settings", { detail: "usage" }))
      : openBilling({ scope: banner.scope, planId: banner.kind === "welcome" ? "" : banner.planId, ...(banner.addSeats ? { addSeats: true } : {}) });
  // "Gói {plan}" with a plan named "Gói cơ bản" must not read "Gói Gói cơ bản"
  const plan = planNameAfterWord(banner.planName, t("bill.planWord"));
  // shown as the last day the package can be used
  const date = banner.endsAt ? formatDate(lastUsableDay(banner.endsAt)) : "";
  const limitName = banner.limit?.metric === "tokens" ? t("pbn.tokens") : t("pbn.questions");

  let message = "";
  let action: LocaleKey = "pbn.renew";
  switch (banner.kind) {
    case "expired":
      message = date ? t("pbn.expired", { plan, date }) : t("pbn.expiredNoDate", { plan });
      break;
    case "not_started":
      message = banner.startsAt
        ? t("pbn.notStarted", { plan, date: formatDate(banner.startsAt) })
        : t("pbn.notStartedNoDate", { plan });
      break;
    case "expiring":
      message = banner.daysLeft > 1 ? t("pbn.expiring", { plan, n: banner.daysLeft, date }) : t("pbn.expiringToday", { plan });
      break;
    case "exhausted_month":
      message = t("pbn.exhaustedMonth", { plan, limit: limitName });
      action = "pbn.upgrade";
      break;
    case "exhausted_day":
      message = t("pbn.exhaustedDay", { plan, limit: limitName });
      action = "pbn.upgrade";
      break;
    case "near_cap":
      message = t("pbn.nearCap", { plan, percent: banner.percent });
      action = "pbn.upgrade";
      break;
    case "no_seat":
      message = banner.assignSeat ? t("pbn.noSeatFree", { plan }) : t("pbn.noSeat", { plan });
      action = banner.assignSeat ? "pbn.assignSeat" : "pbn.addSeats";
      break;
    case "addon_expired":
      message = banner.addonEnded
        ? t("pbn.addonEnded", { name: banner.agentName, date })
        : t("pbn.addonEnding", { name: banner.agentName, date });
      action = "pbn.addonBuyAgain";
      break;
    case "no_plan":
      message = t("pbn.noPlan");
      action = "pbn.buy";
      break;
    case "welcome": {
      const ent = usage?.entitlement;
      message = ent
        ? ent.ends_at
          ? t("pbn.welcomeHasPlan", { plan: planNameAfterWord(ent.plan_name, t("bill.planWord")), date: formatDate(lastUsableDay(ent.ends_at)) })
          : t("pbn.welcomeHasPlanNoEnd", { plan: planNameAfterWord(ent.plan_name, t("bill.planWord")) })
        : usage?.enforced
          ? t("pbn.welcomeNoPlan")
          : t("pbn.welcomeBrowse");
      action = "pbn.seePlans";
      break;
    }
  }
  // a package bought ahead needs nobody to act
  const askAdmin = !banner.canBuy && banner.kind !== "not_started";
  // giving out a chỗ needs no payment
  const showBuy = banner.canBuy && (billingOn || banner.assignSeat);
  const contact = !askAdmin && banner.kind !== "not_started" && !billingOn && info?.contact ? info.contact : "";

  return (
    <div className="shrink-0 px-3 pt-2 sm:px-4">
      <div
        role={banner.tone === "error" ? "alert" : "status"}
        className={`flex flex-wrap items-center gap-x-3 gap-y-1.5 rounded-[12px] border px-3.5 py-2 text-[13px] leading-relaxed ${TONE_CLASS[banner.tone]}`}
      >
        <span className="min-w-0 flex-1">
          {banner.kind === "welcome" && <span className="mr-1 font-medium">{t("pbn.welcomeTitle")}</span>}
          {message}
          {askAdmin && <> {banner.kind === "no_seat" ? t("pbn.askAdminSeat") : t("pbn.askAdmin")}</>}
          {contact && <> {t("pbn.contact", { contact })}</>}
          {banner.kind === "welcome" && plans.length > 0 && (
            <span className="mt-0.5 block text-ink/80">
              {plans
                .map((p) => {
                  const lead = planTermPrices(p)[0];
                  const per = lead.term === "year" ? t("bill.perYear") : lead.term === "quarter" ? t("bill.perQuarter") : t("bill.perMonthShort");
                  const save = planTermPrices(p).some((x) => x.saving > 0) ? ` (${t("pbn.termSaving")})` : "";
                  return `${p.name}: ${formatPriceVND(lead.price)}${per}${save}`;
                })
                .join(" · ")}
            </span>
          )}
        </span>
        <span className="ml-auto flex shrink-0 items-center gap-1.5">
          {showBuy && (
            <button type="button" className="btn btn-sm btn-primary" onClick={buy}>
              {t(action)}
            </button>
          )}
          {banner.kind === "welcome" ? (
            <button type="button" className="btn btn-sm btn-outline" onClick={dismiss}>
              {t("pbn.later")}
            </button>
          ) : (
            <button type="button" className="btn btn-ghost btn-sm px-1.5" aria-label={t("pbn.hide")} title={banner.dismissValue === "forever" ? t("pbn.hide") : t("pbn.hideToday")} onClick={dismiss}>
              <IconClose className="h-4 w-4" />
            </button>
          )}
        </span>
      </div>
    </div>
  );
}
