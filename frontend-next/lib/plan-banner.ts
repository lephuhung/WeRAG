/* Which package banner the app shell shows, if any. Pure so it runs under
 * node --test; the component (components/plan-banner.tsx) only renders it.
 *
 * Urgent banners (the package stops or is about to stop answering) win over
 * the welcome banner a new account sees once. A banner whose package belongs
 * to the Tổ chức has no buy button for a member who cannot renew it: they are
 * told to ask an admin instead. */

type Limit = { metric: string; period: string; limit: number; used: number; remaining: number; exhausted: boolean };
type Entitlement = {
  source: "tenant" | "user";
  subscription_id: string;
  plan_id: string;
  plan_name: string;
  mode: string;
  has_seat: boolean;
  ends_at?: string | null;
  expired: boolean;
  not_started?: boolean;
  starts_at?: string | null;
  limits: Limit[];
  lapsed_tenant?: { subscription_id: string; plan_id: string; plan_name: string; ends_at?: string | null; not_started?: boolean } | null;
};

export type PlanBannerKind =
  | "expired"
  | "not_started"
  | "no_seat"
  | "exhausted_day"
  | "exhausted_month"
  | "no_plan"
  | "expiring"
  | "near_cap"
  | "welcome";

export type PlanBanner = {
  kind: PlanBannerKind;
  tone: "error" | "warning" | "info";
  /** The person may buy/renew from the banner (otherwise: ask an admin). */
  canBuy: boolean;
  /** Scope and plan the billing page should open on. */
  scope: "tenant" | "user";
  planId: string;
  planName: string;
  endsAt: string | null;
  /** When a package that has not started yet begins (not_started only). */
  startsAt: string | null;
  /** Usable days left counting today (expiring only), 1 = today is the last. */
  daysLeft: number;
  /** Share of the tightest month limit used (near_cap only). */
  percent: number;
  /** Day/month limit that ran out (exhausted_*). */
  limit: Limit | null;
  /** Dismissal key and how long a dismissal lasts: a VN day (hidden for
   * that day only) or "forever". The caller stores the value under the key. */
  dismissKey: string;
  dismissValue: string;
};

export type PlanBannerInput = {
  now: Date;
  isSystemAdmin: boolean;
  isTenantAdmin: boolean;
  /** The person works in a Tổ chức (not a tenantless portal guest). */
  hasTenant?: boolean;
  enforced: boolean;
  entitlement: Entitlement | null | undefined;
  /** ISO creation time of the account. */
  userCreatedAt: string;
  /** Stored dismissal value for a key (see dismissKey), or null. */
  dismissed: (key: string) => string | null;
};

/** Days before ends_at the renewal banner appears. */
export const PLAN_EXPIRING_DAYS = 7;
/** Share of a month limit used at which the near-cap banner appears. */
export const PLAN_NEAR_CAP_PERCENT = 90;
/** How long after sign-up the welcome banner is offered. */
export const PLAN_WELCOME_DAYS = 14;

const DAY_MS = 24 * 60 * 60 * 1000;

/** YYYY-MM-DD of the Vietnam calendar day, for once-a-day dismissals. */
export function vnDay(now: Date): string {
  return new Date(now.getTime() + 7 * 60 * 60 * 1000).toISOString().slice(0, 10);
}

/** The last day a package can be used: ends_at is the VN midnight after it. */
export function lastUsableDay(endsAt: string): Date {
  return new Date(new Date(endsAt).getTime() - 1);
}
// (lib/datetime.ts formatLastDay formats it for every other page)

/** Usable days from today to the last usable day, both counted. */
function usableDaysLeft(now: Date, endsAt: string): number {
  const a = Date.parse(vnDay(now));
  const b = Date.parse(vnDay(lastUsableDay(endsAt)));
  return Math.round((b - a) / DAY_MS) + 1;
}

export function planBanner(input: PlanBannerInput): PlanBanner | null {
  const { now, entitlement: ent } = input;
  if (input.isSystemAdmin) return null;
  const day = vnDay(now);
  // an admin whose Tổ chức has no package of its own (their personal package
  // or sign-up gift stands in) is sent to buy for the Tổ chức: a personal
  // package would not cover the members
  // (only when the personal package is gone or just a welcome: renewing a
  // running personal package stays the admin's own choice)
  const adminWithoutTenantPlan = input.isTenantAdmin && !!input.hasTenant && (!ent || (ent.source === "user" && !ent.lapsed_tenant));

  const make = (
    kind: PlanBannerKind,
    tone: PlanBanner["tone"],
    extra: Partial<PlanBanner> = {},
    dismissValue = day,
  ): PlanBanner => {
    const toTenant = adminWithoutTenantPlan && (kind === "no_plan" || kind === "expired" || kind === "welcome");
    const scope = toTenant ? "tenant" : (ent?.source ?? "user");
    return {
      kind,
      tone,
      canBuy: scope === "user" || input.isTenantAdmin,
      scope,
      planId: toTenant ? "" : (ent?.plan_id ?? ""),
      planName: ent?.plan_name ?? "",
      endsAt: ent?.ends_at ?? null,
      startsAt: null,
      daysLeft: 0,
      percent: 0,
      limit: null,
      dismissKey: kind === "welcome" ? "welcome" : `${kind}:${ent?.subscription_id ?? "none"}`,
      dismissValue,
      ...extra,
    };
  };
  const pick = (b: PlanBanner | null): PlanBanner | null => {
    if (!b) return null;
    const stored = input.dismissed(b.dismissKey);
    return stored === "forever" || stored === b.dismissValue ? null : b;
  };

  const urgent = ((): PlanBanner | null => {
    if (!ent) {
      // a Tổ chức admin buys for the Tổ chức (a personal package would not
      // cover the members)
      return input.enforced ? make("no_plan", "warning", { canBuy: true }) : null;
    }
    // a Tổ chức package that lapsed while the admin's own package stands in:
    // the admin still has to renew it (members just use their own)
    // (one that has not started yet needs nothing: it starts on its own)
    const lapsed = ent.lapsed_tenant;
    if (lapsed && !lapsed.not_started && input.isTenantAdmin) {
      return {
        ...make("expired", "error"),
        canBuy: true,
        scope: "tenant",
        planId: lapsed.plan_id,
        planName: lapsed.plan_name,
        endsAt: lapsed.ends_at ?? null,
        dismissKey: `expired:${lapsed.subscription_id}`,
      };
    }
    // bought ahead: nothing to renew, it begins on its start date
    // (nothing to act on, so hiding it is for good)
    if (ent.expired && ent.not_started) return make("not_started", "info", { canBuy: false, startsAt: ent.starts_at ?? null }, "forever");
    if (ent.expired) return make("expired", "error");
    if (ent.mode === "seat" && !ent.has_seat) return make("no_seat", "warning");
    const month = ent.limits.find((l) => l.exhausted && l.period === "month");
    if (month) return make("exhausted_month", "error", { limit: month });
    const today = ent.limits.find((l) => l.exhausted && l.period === "day");
    if (today) return make("exhausted_day", "warning", { limit: today });
    if (ent.ends_at) {
      const left = new Date(ent.ends_at).getTime() - now.getTime();
      if (Number.isFinite(left) && left > 0 && left <= PLAN_EXPIRING_DAYS * DAY_MS) {
        return make("expiring", "warning", { daysLeft: Math.max(1, usableDaysLeft(now, ent.ends_at)) });
      }
    }
    let percent = 0;
    for (const l of ent.limits) {
      if (l.period !== "month" || l.limit <= 0) continue;
      percent = Math.max(percent, Math.floor((l.used / l.limit) * 100));
    }
    if (percent >= PLAN_NEAR_CAP_PERCENT) return make("near_cap", "warning", { percent: Math.min(percent, 100) });
    return null;
  })();
  if (urgent) return pick(urgent);

  const created = new Date(input.userCreatedAt).getTime();
  // a member covered by the Tổ chức's package has nothing to buy
  const coveredByTenant = ent?.source === "tenant" && !input.isTenantAdmin;
  if (!coveredByTenant && Number.isFinite(created) && now.getTime() - created <= PLAN_WELCOME_DAYS * DAY_MS) {
    // offered once per account: dismissing it is for good
    return pick(make("welcome", "info", {}, "forever"));
  }
  return null;
}
