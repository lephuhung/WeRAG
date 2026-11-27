"use client";

/* SuperAdmin usage packages: the enforcement switch, the plans (runtime
 * data, no seeded plans), every workspace next to its subscription, and
 * personal subscriptions. Limits are a list of {metric, period, limit};
 * a subscription may override its plan's list. */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Modal } from "@/components/modal";
import { Toggle } from "@/components/settings/toggle";
import { LimitStatusList } from "@/components/settings/usage-limits";
import { SystemSection } from "@/components/settings/system-section";
import { Notice } from "@/components/settings/notice";
import { Loading } from "@/components/settings/loading";
import { FieldLabel } from "@/components/settings/field";
import { LoadMore } from "@/components/settings/pager";
import { ROW_HOVER } from "@/components/settings/data-table";
import { useConfirm } from "@/components/confirm-dialog";
import { useToast } from "@/components/toast";
import { IconClose, IconEdit, IconPlus, IconSearch } from "@/components/icons";
import { useT, type LocaleKey } from "@/lib/i18n";
import { planFeatureValue, withPlanFeature, type PlanFeatureKey } from "@/lib/session-limits";
import { getCurrentUser } from "@/lib/api/auth";
import { getSystemSetting, listSystemUsers, updateSystemSetting, type SystemAdminUser } from "@/lib/api/system";
import {
  USAGE_ENFORCE_SETTING_KEY,
  USAGE_SIGNUP_PLAN_DAYS_SETTING_KEY,
  USAGE_SIGNUP_PLAN_SETTING_KEY,
  planTermPrices,
  createUsagePlan,
  createUsageSubscription,
  deleteUsagePlan,
  deleteUsageSubscription,
  formatTokens,
  listUsagePlans,
  listUsageSubscriptions,
  listUsageTenantOverview,
  updateUsagePlan,
  updateUsageSubscription,
  type UsageLimit,
  type UsageMode,
  type UsagePlan,
  type UsagePlanInput,
  type UsageScopeKind,
  type UsageSubscriptionInput,
  type UsageSubscriptionView,
  type UsageTenantOverview,
  formatPriceVND,
  listAgentAddons,
  planPrice,
  planPriceForMonths,
  type UsageAgentAddonCatalogue,
} from "@/lib/api/usage";
import { addonsForPlan, planIncludedAgents, togglePlanId } from "@/lib/agent-addon-catalogue";
import { userErrorMessage } from "@/lib/user-error";

const USER_PAGE = 30;

const emptyPlan: UsagePlanInput = {
  code: "",
  name: "",
  description: "",
  scope_kind: "tenant",
  mode: "pool",
  limits: [{ metric: "tokens", period: "month", limit: 0 }],
  features: {},
  price_label: "",
  price_vnd: 0,
  price_quarter_vnd: 0,
  price_year_vnd: 0,
  sort_order: 0,
  is_active: true,
  included_agent_ids: [],
};

/* One VND amount input: digits only, "" for 0. */
function PriceInput({ id, value, onChange }: { id: string; value: number; onChange: (v: number) => void }) {
  return (
    <div className="relative">
      <input
        id={id}
        className="input pr-14"
        inputMode="numeric"
        placeholder="0"
        value={value ? String(value) : ""}
        onChange={(e) => onChange(Number(e.target.value.replace(/[^\d]/g, "")) || 0)}
      />
      <span className="caption pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-muted-soft">VNĐ</span>
    </div>
  );
}

/* Date input value (YYYY-MM-DD, local) → RFC3339 at end of that day in
 * Vietnam; empty → null (open-ended). */
function endsAtFromDate(d: string): string | null {
  if (!d) return null;
  return `${d}T23:59:59+07:00`;
}

/* Date input value → RFC3339 at the start of that day in Vietnam. */
function startsAtFromDate(d: string): string {
  return `${d}T00:00:00+07:00`;
}

function todayVN(): string {
  return new Date(Date.now() + 7 * 3600_000).toISOString().slice(0, 10);
}

function dateFromEndsAt(v: string | null | undefined): string {
  if (!v) return "";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return "";
  // Render in Vietnam time to match endsAtFromDate. An end is stored as the
  // Vietnam midnight after the last usable day: show (and edit) that day,
  // else every save would move the end one day later.
  const vn = new Date(d.getTime() - 1 + 7 * 3600_000);
  return vn.toISOString().slice(0, 10);
}

/* Starts are stored as the Vietnam midnight that begins the day: that day. */
function dateFromStartsAt(v: string | null | undefined): string {
  if (!v) return "";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return "";
  return new Date(d.getTime() + 7 * 3600_000).toISOString().slice(0, 10);
}

function notStarted(sub: { starts_at: string }): boolean {
  const d = new Date(sub.starts_at);
  return !Number.isNaN(d.getTime()) && d.getTime() > Date.now();
}

/* Calendar arithmetic on YYYY-MM-DD strings, in UTC so no zone drifts a day. */
function addDays(d: string, days: number): string {
  const [y, m, day] = d.split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, day + days)).toISOString().slice(0, 10);
}

/* The last day of a subscription of `months` months starting on `start`,
 * as the server computes it (types.SubscriptionEndVN): the same day of the
 * month `months` later, clamped to that month's last day. 27/11 + 1 month
 * runs through 27/12; 31/01 + 1 month through 28/02. */
function endForMonths(start: string, months: number): string {
  const [y, m, day] = start.split("-").map(Number);
  const lastDay = new Date(Date.UTC(y, m - 1 + months + 1, 0)).getUTCDate();
  return new Date(Date.UTC(y, m - 1 + months, Math.min(day, lastDay))).toISOString().slice(0, 10);
}

/* Whole months and leftover days from start to end (end inclusive);
 * null when the span is empty or open-ended. Leftover days count from the
 * last whole month's end, or from the start day itself when under a month. */
function subscriptionSpan(start: string, end: string): { months: number; days: number } | null {
  if (!start || !end || end < start) return null;
  let months = 0;
  while (months < 1200 && endForMonths(start, months + 1) <= end) months++;
  const from = months > 0 ? endForMonths(start, months) : addDays(start, -1);
  const days = Math.round((Date.parse(end) - Date.parse(from)) / 86_400_000);
  return { months, days };
}

/* The month rate used for leftover days and for month counts no priced
 * term fits: the monthly price, else the quarter or year price spread. */
function monthlyRate(plan: UsagePlan): number {
  if (plan.price_vnd > 0) return plan.price_vnd;
  if (plan.price_quarter_vnd > 0) return plan.price_quarter_vnd / 3;
  if (plan.price_year_vnd > 0) return plan.price_year_vnd / 12;
  return 0;
}

const DAYS_PER_MONTH = 30; // usage_billing.go usageProrationDaysPerMonth

/* Reference price of one unit (one seat, or the whole pool) for a span:
 * whole months as checkout prices them (year/quarter/month terms),
 * leftover days at the month rate per 30 days, rounded up like the
 * server's seat proration. */
function spanPrice(plan: UsagePlan, months: number, days: number): { total: number; term: "month" | "quarter" | "year" | null } {
  const rate = monthlyRate(plan);
  const whole = months > 0 ? planPriceForMonths(plan, months) : null;
  const wholeTotal = months > 0 ? (whole?.total ?? Math.ceil(rate * months)) : 0;
  // never more than a whole month: 01/12–31/12 (31 days) must not cost
  // more than 01/12–01/01 (one month)
  const dayTotal = days > 0 ? Math.min(Math.ceil(rate), Math.ceil((rate * days) / DAYS_PER_MONTH)) : 0;
  return { total: wholeTotal + dayTotal, term: whole?.term ?? null };
}

const SUB_LENGTHS = [1, 3, 6, 12];

/* Live reference price of the subscription being assigned: unit price,
 * seats, length and total, re-computed on every change in the modal.
 * Assigning here creates no order; this is what checkout would charge. */
function SubPriceSummary({ plan, seatMode, seats, start, end }: { plan: UsagePlan | undefined; seatMode: boolean; seats: number; start: string; end: string }) {
  const { t } = useT();
  if (!plan) return null;
  // A seat subscription pays the list price per seat, even on a plan
  // listed as a pool: the admin chose to sell it by seat.
  const perSeat = seatMode;
  const rate = monthlyRate(plan);
  if (rate <= 0) {
    return (
      <div className="rounded-lg border border-hairline bg-surface-strong px-4 py-3">
        <p className="caption text-muted">{plan.price_label ? t("usg.priceLabelOnly", { label: plan.price_label }) : t("usg.priceNone")}</p>
      </div>
    );
  }
  const n = seatMode ? Math.max(0, seats) : 1;
  const span = subscriptionSpan(start, end);
  const unit = plan.price_vnd > 0 ? plan.price_vnd : rate;
  const termName = (term: "quarter" | "year", months: number) => {
    const k = term === "year" ? months / 12 : months / 3;
    if (k > 1) return t(term === "year" ? "bill.termYearN" : "bill.termQuarterN", { n: k });
    return term === "year" ? t("bill.termYear") : t("bill.termQuarter");
  };
  let lengthText = t("usg.priceOpenEnded");
  let total: number | null = null;
  let saving = 0;
  if (span) {
    const priced = spanPrice(plan, span.months, span.days);
    total = priced.total * n;
    const parts: string[] = [];
    if (span.months > 0) {
      parts.push(t("bill.monthsN", { n: span.months }));
      if (priced.term === "quarter" || priced.term === "year") parts.push(termName(priced.term, span.months));
    }
    if (span.days > 0) parts.push(t("usg.priceDaysN", { n: span.days }));
    lengthText = parts.join(" · ");
    if (plan.price_vnd > 0 && span.months > 0 && priced.term && priced.term !== "month") {
      saving = Math.max(0, plan.price_vnd * span.months - (planPriceForMonths(plan, span.months)?.total ?? 0)) * n;
    }
  }
  const row = (label: string, value: React.ReactNode) => (
    <div className="flex items-baseline justify-between gap-3">
      <span className="text-muted">{label}</span>
      <span className="text-right text-ink">{value}</span>
    </div>
  );
  return (
    <div className="space-y-1.5 rounded-lg border border-hairline bg-surface-strong px-4 py-3 text-[14px]" aria-live="polite">
      <div className="caption font-medium uppercase tracking-wide text-muted-soft">{t("usg.priceSummary")}</div>
      {row(t("usg.priceUnit"), `${formatPriceVND(unit)}${perSeat ? t("usg.perSeatMonth") : t("usg.perMonth")}`)}
      {seatMode && row(t("usg.seats"), `× ${n}`)}
      {row(t("usg.priceLength"), lengthText)}
      <div className="border-t border-hairline pt-1.5">
        {total != null
          ? row(t("usg.priceTotal"), <span className="text-base font-semibold">{formatPriceVND(total)}</span>)
          : row(t("usg.pricePerMonth"), <span className="text-base font-semibold">{formatPriceVND(Math.ceil(rate * n))}</span>)}
        {saving > 0 && <div className="caption text-right text-success">{t("usg.saving", { price: formatPriceVND(saving) })}</div>}
      </div>
      <p className="caption text-muted-soft">{t("usg.priceNoOrder")}</p>
    </div>
  );
}

function LimitsEditor({ value, onChange }: { value: UsageLimit[]; onChange: (v: UsageLimit[]) => void }) {
  const { t } = useT();
  const set = (i: number, patch: Partial<UsageLimit>) =>
    onChange(value.map((l, j) => (j === i ? { ...l, ...patch } : l)));
  return (
    <div className="space-y-2">
      {value.map((l, i) => (
        <div key={i} className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-2 sm:flex">
          {/* Quotas count tokens; question limits are legacy (the old guest
           * plan) and only offered so such a row can be switched to tokens. */}
          {l.metric === "tokens" ? (
            <span className="input flex h-9 min-w-0 items-center py-0 text-muted sm:w-36">{t("usg.metricTokens")}</span>
          ) : (
            <select
              className="input h-9 min-w-0 py-0 sm:w-36"
              aria-label={t("usg.metricQuestions")}
              value={l.metric}
              onChange={(e) => set(i, { metric: e.target.value as UsageLimit["metric"] })}
            >
              <option value="questions">{t("usg.metricQuestions")}</option>
              <option value="tokens">{t("usg.metricTokens")}</option>
            </select>
          )}
          <span className="caption text-muted">/</span>
          <select
            className="input h-9 min-w-0 py-0 sm:w-28"
            aria-label={l.period === "day" ? t("usg.periodDay") : t("usg.periodMonth")}
            value={l.period}
            onChange={(e) => set(i, { period: e.target.value as UsageLimit["period"] })}
          >
            <option value="day">{t("usg.periodDay")}</option>
            <option value="month">{t("usg.periodMonth")}</option>
          </select>
          <input
            type="number"
            min={0}
            className="input col-span-2 h-9 min-w-0 py-0 sm:w-32"
            aria-label={t("usg.planLimits")}
            value={l.limit}
            onChange={(e) => set(i, { limit: Math.max(0, Number(e.target.value) || 0) })}
          />
          <button
            type="button"
            className="btn btn-outline btn-sm"
            aria-label={t("common.delete")}
            title={t("common.delete")}
            onClick={() => onChange(value.filter((_, j) => j !== i))}
          >
            <IconClose className="h-3.5 w-3.5" />
          </button>
        </div>
      ))}
      <button
        type="button"
        className="btn btn-outline btn-sm"
        onClick={() => onChange([...value, { metric: "tokens", period: "month", limit: 0 }])}
      >
        <IconPlus className="h-3.5 w-3.5" /> {t("usg.addLimit")}
      </button>
      <p className="caption text-muted-soft">{t("usg.limitZeroHint")}</p>
    </div>
  );
}

/** The per-turn and per-session caps of a plan (usage_plans.features). 0 or
 * empty = no cap; keys this editor does not know are kept untouched. */
const FEATURE_FIELDS: { key: PlanFeatureKey; label: LocaleKey; hint: LocaleKey }[] = [
  { key: "agent.turn_token_cap", label: "usg.feat.turnTokenCap", hint: "usg.feat.turnTokenCapHint" },
  { key: "agent.max_iterations", label: "usg.feat.maxIterations", hint: "usg.feat.maxIterationsHint" },
  { key: "context.history_tokens", label: "usg.feat.historyTokens", hint: "usg.feat.historyTokensHint" },
  { key: "session.max_turns", label: "usg.feat.sessionMaxTurns", hint: "usg.feat.sessionMaxTurnsHint" },
  { key: "session.max_tokens", label: "usg.feat.sessionMaxTokens", hint: "usg.feat.sessionMaxTokensHint" },
];

function FeaturesEditor({
  value,
  onChange,
}: {
  value: Record<string, unknown>;
  onChange: (v: Record<string, unknown>) => void;
}) {
  const { t } = useT();
  return (
    <div className="space-y-2">
      {FEATURE_FIELDS.map((f) => (
        <label key={f.key} className="grid grid-cols-[minmax(0,1fr)_8rem] items-center gap-x-3 gap-y-0.5">
          <span className="caption text-body">{t(f.label)}</span>
          <input
            type="number"
            min={0}
            className="input h-9 py-0"
            value={planFeatureValue(value, f.key) || ""}
            placeholder="∞"
            onChange={(e) => onChange(withPlanFeature(value, f.key, Number(e.target.value) || 0))}
          />
          <span className="caption col-span-2 text-muted-soft">{t(f.hint)}</span>
        </label>
      ))}
    </div>
  );
}

function limitText(l: UsageLimit, t: (k: never, p?: Record<string, string | number>) => string): string {
  const metric = l.metric === "tokens" ? t("usg.metricTokens" as never) : t("usg.metricQuestions" as never);
  const period = l.period === "day" ? t("usg.periodDay" as never) : t("usg.periodMonth" as never);
  const n = l.limit > 0 ? (l.metric === "tokens" ? formatTokens(l.limit) : l.limit.toLocaleString()) : "∞";
  return `${n} ${metric}/${period}`;
}

interface SubForm {
  plan_id: string;
  mode: UsageMode;
  seats: number;
  starts_at: string;
  /** false = start right now (server time); true = the admin picked a date. */
  custom_start: boolean;
  ends_at: string;
  note: string;
}

function subFormFrom(sub: UsageSubscriptionView | null | undefined, plans: UsagePlan[], kind: UsageScopeKind): SubForm {
  const firstPlan = plans.find((p) => p.scope_kind === kind && p.is_active);
  if (!sub) {
    return {
      plan_id: firstPlan?.id ?? "",
      mode: firstPlan?.mode ?? "pool",
      seats: 0,
      starts_at: todayVN(),
      custom_start: false,
      ends_at: "",
      note: "",
    };
  }
  return {
    plan_id: sub.plan_id,
    mode: sub.mode,
    seats: sub.seats,
    starts_at: dateFromStartsAt(sub.starts_at) || todayVN(),
    custom_start: true,
    ends_at: dateFromEndsAt(sub.ends_at),
    note: sub.note,
  };
}

export default function AdminUsage() {
  const { t } = useT();
  const [allowed, setAllowed] = useState<boolean | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const confirm = useConfirm();
  const toast = useToast();

  // usage.enforce is ON unless a SuperAdmin saved false (off = testing only)
  const [enforced, setEnforced] = useState(true);
  // the personal package every new account receives ("" = none)
  const [signup, setSignup] = useState({ planId: "", days: 30 });
  const [signupBase, setSignupBase] = useState({ planId: "", days: 30 });
  const [plans, setPlans] = useState<UsagePlan[]>([]);
  const [tenants, setTenants] = useState<UsageTenantOverview[]>([]);
  const [personal, setPersonal] = useState<UsageSubscriptionView[]>([]);
  // basic agents every plan includes + specialist agents sold with plans
  const [catalogue, setCatalogue] = useState<UsageAgentAddonCatalogue>({ addons: [], included_agents: [], addon_agents: [] });
  const [q, setQ] = useState("");

  const [planOpen, setPlanOpen] = useState(false);
  const [planEditing, setPlanEditing] = useState<UsagePlan | null>(null);
  const [planForm, setPlanForm] = useState<UsagePlanInput>(emptyPlan);
  /* The values the modal was opened with: the save button stays disabled
   * until the form differs from them. */
  const [planBase, setPlanBase] = useState<UsagePlanInput>(emptyPlan);

  const [subOpen, setSubOpen] = useState(false);
  const [subTarget, setSubTarget] = useState<{ kind: UsageScopeKind; id: string; name: string; existing?: UsageSubscriptionView | null } | null>(null);
  const [subForm, setSubForm] = useState<SubForm>(subFormFrom(null, [], "tenant"));
  const [subBase, setSubBase] = useState<SubForm>(subFormFrom(null, [], "tenant"));

  const [userPickOpen, setUserPickOpen] = useState(false);
  const [userQ, setUserQ] = useState("");
  const [userHits, setUserHits] = useState<SystemAdminUser[]>([]);
  const [userTotal, setUserTotal] = useState(0);
  const [userLoading, setUserLoading] = useState(false);

  const load = useCallback(async () => {
    try {
      const [setting, pl, tn, ps, signupPlan, signupDays] = await Promise.all([
        getSystemSetting(USAGE_ENFORCE_SETTING_KEY),
        listUsagePlans(true),
        listUsageTenantOverview(),
        listUsageSubscriptions({ scope_type: "user" }),
        getSystemSetting(USAGE_SIGNUP_PLAN_SETTING_KEY).catch(() => null),
        getSystemSetting(USAGE_SIGNUP_PLAN_DAYS_SETTING_KEY).catch(() => null),
      ]);
      setEnforced(!(setting.value === false || setting.value === "false"));
      const nextSignup = {
        planId: typeof signupPlan?.value === "string" ? signupPlan.value.trim() : "",
        days: Number(signupDays?.value ?? 30) || 0,
      };
      if (signupDays == null || signupDays.value == null) nextSignup.days = 30;
      setSignup(nextSignup);
      setSignupBase(nextSignup);
      setPlans(pl);
      setTenants(tn);
      setPersonal(ps);
      setError("");
      // the plan cards still render if the catalogue cannot be read
      listAgentAddons(true)
        .then(setCatalogue)
        .catch(() => setCatalogue({ addons: [], included_agents: [], addon_agents: [] }));
    } catch (e) {
      setError(userErrorMessage(e, t, "usg.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    let alive = true;
    getCurrentUser()
      .then((me) => {
        if (!alive) return;
        const ok = me.data?.user?.is_system_admin === true;
        setAllowed(ok);
        if (ok) void load();
      })
      .catch(() => alive && setAllowed(false));
    return () => {
      alive = false;
    };
  }, [load]);

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError("");
    try {
      await fn();
      return true;
    } catch (e) {
      setError(userErrorMessage(e, t, "usg.opFailed"));
      return false;
    } finally {
      setBusy(false);
    }
  };

  const withoutPlan = useMemo(() => tenants.filter((x) => !x.subscription?.current).length, [tenants]);

  const toggleEnforce = async (v: boolean) => {
    const ok = await run(async () => {
      await updateSystemSetting(USAGE_ENFORCE_SETTING_KEY, v);
      setEnforced(v);
    });
    if (ok) toast.success(t("common.saved"));
  };

  const signupDirty = signup.planId !== signupBase.planId || signup.days !== signupBase.days;
  const saveSignup = async () => {
    const ok = await run(async () => {
      await updateSystemSetting(USAGE_SIGNUP_PLAN_SETTING_KEY, signup.planId);
      await updateSystemSetting(USAGE_SIGNUP_PLAN_DAYS_SETTING_KEY, Math.max(0, Math.floor(signup.days)));
      setSignupBase(signup);
    });
    if (ok) toast.success(t("common.saved"));
  };
  // personal plans a new account can receive; the saved one stays listed
  // even when deactivated, so the select shows what is stored
  const signupPlans = useMemo(
    () => plans.filter((p) => p.scope_kind === "user" && (p.is_active || p.id === signupBase.planId)),
    [plans, signupBase.planId],
  );
  const savedSignupPlan = plans.find((p) => p.id === signupBase.planId);
  const signupWarn = signupBase.planId
    ? !savedSignupPlan
      ? t("usg.signupMissing")
      : !savedSignupPlan.is_active
        ? t("usg.signupInactive")
        : ""
    : "";

  // ---- plans
  const openNewPlan = () => {
    setPlanEditing(null);
    setPlanForm(emptyPlan);
    setPlanBase(emptyPlan);
    setPlanOpen(true);
  };
  const openEditPlan = (p: UsagePlan) => {
    setPlanEditing(p);
    const form: UsagePlanInput = {
      code: p.code,
      name: p.name,
      description: p.description,
      scope_kind: p.scope_kind,
      mode: p.mode,
      limits: p.limits ?? [],
      features: p.features ?? {},
      price_label: p.price_label,
      price_vnd: p.price_vnd ?? 0,
      price_quarter_vnd: p.price_quarter_vnd ?? 0,
      price_year_vnd: p.price_year_vnd ?? 0,
      sort_order: p.sort_order,
      is_active: p.is_active,
      included_agent_ids: [...(p.included_agent_ids ?? [])],
    };
    setPlanForm(form);
    setPlanBase(form);
    setPlanOpen(true);
  };
  const planDirty = JSON.stringify(planForm) !== JSON.stringify(planBase);
  /* Hint under a quarter/year price: the saving against monthly, or how
   * the term is charged when left empty. */
  const termHint = (price: number, monthly: number, months: number) => {
    if (price <= 0) return monthly > 0 ? t("usg.termAtMonthly", { price: formatPriceVND(monthly * months) }) : t("usg.termNotSold");
    if (monthly > 0 && monthly * months > price) {
      const pct = Math.round((1 - price / (monthly * months)) * 100);
      return t("usg.termSaving", { price: formatPriceVND(monthly * months - price), pct });
    }
    return monthly > 0 && monthly * months < price ? t("usg.termDearer") : "";
  };
  const submitPlan = async (e: React.FormEvent) => {
    e.preventDefault();
    const ok = await run(async () => {
      if (planEditing) await updateUsagePlan(planEditing.id, planForm);
      else await createUsagePlan(planForm);
      await load();
    });
    if (ok) {
      toast.success(t("common.saved"));
      setPlanOpen(false);
    }
  };
  const removePlan = async (p: UsagePlan) => {
    const yes = await confirm({
      title: t("common.delete"),
      message: t("usg.deletePlanConfirm", { name: p.name }),
      danger: true,
    });
    if (!yes) return;
    const ok = await run(async () => {
      await deleteUsagePlan(p.id);
      await load();
    });
    if (ok) toast.success(t("common.saved"));
  };

  // ---- subscriptions
  const openAssign = (kind: UsageScopeKind, id: string, name: string, existing?: UsageSubscriptionView | null) => {
    // a Guest's implicit guest plan has no row yet: assigning creates one
    if (existing?.implicit) existing = null;
    setSubTarget({ kind, id, name, existing });
    const form = subFormFrom(existing, plans, kind);
    setSubForm(form);
    setSubBase(form);
    setSubOpen(true);
  };
  const subDirty = JSON.stringify(subForm) !== JSON.stringify(subBase);
  const submitSub = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!subTarget) return;
    const input: UsageSubscriptionInput = {
      scope_type: subTarget.kind,
      scope_id: subTarget.id,
      plan_id: subForm.plan_id,
      mode: subTarget.kind === "tenant" ? subForm.mode : "pool",
      seats: subForm.mode === "seat" ? subForm.seats : 0,
      // Default: the server stamps "now" on create; an update must carry the
      // existing start or the server would reset it.
      starts_at: subForm.custom_start
        ? startsAtFromDate(subForm.starts_at)
        : subTarget.existing && subTarget.existing.plan_id === subForm.plan_id
          ? subTarget.existing.starts_at
          : undefined,
      ends_at: endsAtFromDate(subForm.ends_at),
      note: subForm.note,
      // Limits come from the plan (token quotas); a per-subscription
      // override is no longer offered. An old one is kept as it is while
      // the plan stays, and dropped with a plan change (a new subscription).
      limits_override:
        subTarget.existing && subTarget.existing.plan_id === subForm.plan_id ? (subTarget.existing.limits_override ?? null) : null,
      status: "active",
    };
    const ok = await run(async () => {
      const ex = subTarget.existing;
      if (ex && ex.plan_id === input.plan_id) await updateUsageSubscription(ex.id, input);
      else await createUsageSubscription(input);
      await load();
    });
    if (ok) {
      toast.success(t("common.saved"));
      setSubOpen(false);
    }
  };
  const cancelSub = async (sub: UsageSubscriptionView) => {
    const yes = await confirm({
      title: t("usg.cancelSub"),
      message: t("usg.cancelSubConfirm", { name: sub.scope_name }),
      danger: true,
    });
    if (!yes) return;
    const ok = await run(async () => {
      await deleteUsageSubscription(sub.id);
      await load();
    });
    if (ok) toast.success(t("common.saved"));
  };

  /* The picker lists every account (newest first) as soon as it opens;
   * typing narrows it by name or e-mail, "show more" pages on. */
  const userSeq = useRef(0);
  const fetchUsers = async (text: string, offset: number) => {
    const seq = ++userSeq.current;
    setUserLoading(true);
    try {
      const res = await listSystemUsers({ q: text.trim() || undefined, limit: USER_PAGE, offset });
      if (seq !== userSeq.current) return;
      setUserHits((prev) => (offset === 0 ? res.users ?? [] : [...prev, ...(res.users ?? [])]));
      setUserTotal(res.total ?? 0);
    } catch {
      if (seq === userSeq.current && offset === 0) setUserHits([]);
    } finally {
      if (seq === userSeq.current) setUserLoading(false);
    }
  };
  useEffect(() => {
    if (!userPickOpen) return;
    const timer = window.setTimeout(() => void fetchUsers(userQ, 0), userQ ? 250 : 0);
    return () => window.clearTimeout(timer);
    // fetchUsers only reads its arguments and stable setters
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [userPickOpen, userQ]);

  const planByID = (id: string) => plans.find((p) => p.id === id);
  // the start the server will use (as submitSub sends it): the picked
  // date, else the kept start of the same plan, else today
  const subExisting = subTarget?.existing;
  const subStart = subForm.custom_start
    ? subForm.starts_at || todayVN()
    : subExisting && subExisting.plan_id === subForm.plan_id
      ? dateFromStartsAt(subExisting.starts_at) || todayVN()
      : todayVN();
  const endsBeforeStart = !!subForm.ends_at && subForm.ends_at < subStart;
  const selectablePlans = plans.filter((p) => p.scope_kind === (subTarget?.kind ?? "tenant") && (p.is_active || p.id === subForm.plan_id));
  const filteredTenants = tenants.filter((x) => !q || x.tenant_name.toLowerCase().includes(q.toLowerCase()));

  return (
    <SystemSection
      allowed={allowed}
      title={t("usg.title")}
      deniedMessage={t("usg.denied")}
      loading={loading}
      loadingLabel={t("common.loading")}
    >
      <div className="space-y-6">
      {error && (
        <Notice
          tone="error"
          action={
            <button type="button" className="btn btn-outline btn-sm" onClick={() => void load()}>
              {t("common.retry")}
            </button>
          }
        >
          {error}
        </Notice>
      )}

      {/* Enforcement */}
      <div className="card flex flex-wrap items-center gap-4 p-4">
        <div className="min-w-0 flex-1">
          <div className="text-[14px] font-medium text-ink">{t("usg.enforceTitle")}</div>
          <p className="caption text-muted">{t("usg.enforceDesc")}</p>
          {withoutPlan > 0 && (
            <p className={`caption mt-1 ${enforced ? "text-error" : "text-warning"}`}>
              {t("usg.enforceWarn", { n: withoutPlan })}
            </p>
          )}
        </div>
        <Toggle checked={enforced} disabled={busy} onChange={(v) => void toggleEnforce(v)} label={t("usg.enforceTitle")} />
      </div>

      {/* Sign-up gift: the personal package every new account receives */}
      <div className="card space-y-3 p-4">
        <div>
          <div className="text-[14px] font-medium text-ink">{t("usg.signupTitle")}</div>
          <p className="caption text-muted">{t("usg.signupDesc")}</p>
          {signupWarn && <p className="caption mt-1 text-warning">{signupWarn}</p>}
        </div>
        <div className="flex flex-wrap items-end gap-3">
          <div className="block min-w-[240px] flex-1">
            <FieldLabel htmlFor="usg-signup-plan">{t("usg.signupPlan")}</FieldLabel>
            <select
              id="usg-signup-plan"
              className="input"
              value={signup.planId}
              disabled={busy}
              onChange={(e) => setSignup({ ...signup, planId: e.target.value })}
            >
              <option value="">{t("usg.signupNone")}</option>
              {signupPlans.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}{p.is_active ? "" : ` (${t("usg.inactive")})`}
                </option>
              ))}
              {signup.planId && !signupPlans.some((p) => p.id === signup.planId) && (
                <option value={signup.planId}>{t("usg.signupMissingOption")}</option>
              )}
            </select>
          </div>
          <div className="block w-40">
            <FieldLabel htmlFor="usg-signup-days">{t("usg.signupDays")}</FieldLabel>
            <input
              id="usg-signup-days"
              className="input"
              inputMode="numeric"
              disabled={busy || !signup.planId}
              value={String(signup.days)}
              onChange={(e) => setSignup({ ...signup, days: Number(e.target.value.replace(/[^\d]/g, "")) || 0 })}
            />
          </div>
          <button type="button" className="btn btn-primary btn-sm" disabled={busy || !signupDirty} onClick={() => void saveSignup()}>
            {t("common.save")}
          </button>
        </div>
        <p className="caption text-muted-soft">
          {signup.planId ? (signup.days > 0 ? t("usg.signupHintDays", { n: signup.days }) : t("usg.signupHintForever")) : t("usg.signupHintOff")}
        </p>
        {signupPlans.length === 0 && <p className="caption text-muted-soft">{t("usg.signupNoPlans")}</p>}
      </div>

      {/* Plans */}
      <section className="card p-4">
        <div className="mb-3 flex items-center gap-3">
          <h3 className="text-[14px] font-medium text-ink">{t("usg.plans")}</h3>
          <span className="caption text-muted">{t("usg.plansCount", { n: plans.length })}</span>
          <button className="btn btn-primary btn-sm ml-auto" onClick={openNewPlan}>
            <IconPlus className="h-3.5 w-3.5" /> {t("usg.newPlan")}
          </button>
        </div>
        {plans.length === 0 ? (
          <p className="caption text-muted-soft">{t("usg.noPlans")}</p>
        ) : (
          <div className="grid grid-cols-[repeat(auto-fit,minmax(min(100%,300px),1fr))] gap-4">
            {plans.map((p) => (
              <article key={p.id} className="card flex min-w-0 flex-col p-5 text-[14px]">
                <div className="min-w-0 flex-1 space-y-3 break-words">
                  <div className="flex flex-wrap items-start justify-between gap-2">
                    <h4 className="text-lg font-semibold leading-snug text-ink">{p.name}</h4>
                    {!p.is_active && <span className="badge-pill text-[11px]">{t("usg.inactive")}</span>}
                  </div>
                  <div className="caption break-all font-mono text-muted-soft">{p.code}</div>
                  <div className="caption text-muted">
                    {p.scope_kind === "tenant" ? t("usg.scopeTenant") : t("usg.scopeUser")}
                    {p.scope_kind === "tenant" ? ` · ${p.mode === "seat" ? t("usg.modeSeat") : t("usg.modePool")}` : ""}
                  </div>
                  {planPrice(p) && <p className="text-xl font-semibold tracking-tight text-ink">{planPrice(p)}<span className="caption ml-1 font-normal tracking-normal text-muted">{p.mode === "seat" ? t("usg.perSeatMonth") : t("usg.perMonth")}</span></p>}
                  {planTermPrices(p)
                    .filter((x) => x.term !== "month")
                    .map((x) => (
                      <p key={x.term} className="caption text-body">
                        {x.term === "quarter" ? t("usg.priceQuarterShort") : t("usg.priceYearShort")}: <span className="font-medium text-ink">{formatPriceVND(x.price)}</span>
                        {p.mode === "seat" ? ` ${t("usg.perSeat")}` : ""}
                        {x.saving > 0 && <span className="ml-1 text-success">({t("usg.saving", { price: formatPriceVND(x.saving) })})</span>}
                      </p>
                    ))}
                  {p.description && <p className="caption whitespace-pre-line leading-relaxed text-muted">{p.description}</p>}
                  {(() => {
                    // basic agents, then the specialist agents this plan includes
                    const agents = [...catalogue.included_agents, ...planIncludedAgents(p, catalogue.addon_agents)];
                    return agents.length > 0 ? (
                      <div className="caption text-muted">
                        {t("usg.includedAgents", { n: agents.length, names: agents.map((a) => a.name).join(", ") })}
                      </div>
                    ) : null;
                  })()}
                  {addonsForPlan(catalogue.addons, p).length > 0 && (
                    <div className="caption text-muted">
                      {t("usg.addonAgents")}{" "}
                      {addonsForPlan(catalogue.addons, p)
                        .map(
                          (a) =>
                            `${a.agent_name || a.agent_id} (${
                              a.price_vnd > 0 ? formatPriceVND(a.price_vnd) : t("usg.addonFree")
                            }${t("usg.perMonth")}${p.mode === "seat" ? t("usg.addonPerSeat") : ""})`,
                        )
                        .join(", ")}
                    </div>
                  )}
                </div>
                <div className="caption mt-4 border-t border-hairline pt-4 leading-relaxed text-body">
                  {p.limits.length ? p.limits.map((l) => limitText(l, t as never)).join(" · ") : t("usg.unlimited")}
                </div>
                <div className="mt-4 flex flex-wrap justify-end gap-2">
                <button type="button" className="btn btn-soft btn-sm" disabled={busy} onClick={() => openEditPlan(p)}>
                  <IconEdit className="h-3.5 w-3.5" /> {t("common.edit")}
                </button>
                <button type="button" className="btn btn-soft-danger btn-sm" disabled={busy} onClick={() => void removePlan(p)}>
                  {t("common.delete")}
                </button>
                </div>
              </article>
            ))}
          </div>
        )}
      </section>

      {/* Workspaces */}
      <section className="card p-4">
        <div className="mb-3 flex flex-wrap items-center gap-3">
          <h3 className="text-[14px] font-medium text-ink">{t("usg.tenants")}</h3>
          <span className="caption text-muted">{t("usg.tenantsCount", { n: tenants.length, missing: withoutPlan })}</span>
          <div className="relative ml-auto">
            <IconSearch className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-soft" />
            <input className="input h-9 py-0 w-56 pl-9" aria-label={t("usg.searchTenant")} placeholder={t("usg.searchTenant")} value={q} onChange={(e) => setQ(e.target.value)} />
          </div>
        </div>
        {filteredTenants.length === 0 ? (
          <p className="caption py-6 text-center text-muted-soft">{q ? t("common.noResults") : t("common.empty")}</p>
        ) : (
        <div className="divide-y divide-hairline">
          {filteredTenants.map((row) => {
            const sub = row.subscription;
            return (
              <div key={row.tenant_id} className={`flex flex-wrap items-start gap-3 py-2.5 text-[14px] ${ROW_HOVER}`}>
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium text-ink">
                    {row.tenant_name} <span className="caption text-muted-soft">#{row.tenant_id}</span>
                  </div>
                  <div className="caption text-muted">
                    {t("usg.monthUsage", { questions: row.month.questions.toLocaleString(), tokens: formatTokens(row.month.tokens) })}
                  </div>
                </div>
                <div className="min-w-[240px] basis-[280px] flex-1">
                  {sub ? (
                    <>
                      <div className="flex items-center gap-2">
                        <span className="text-ink">{sub.plan?.name ?? planByID(sub.plan_id)?.name ?? sub.plan_id}</span>
                        <span className="badge-pill text-[11px]">{sub.mode === "seat" ? t("usg.seatsUsed", { used: sub.seats_used, total: sub.seats }) : t("usg.modePool")}</span>
                        {!sub.current && (
                          <span className="badge-pill text-[11px] text-error">
                            {notStarted(sub) ? t("usg.notStarted", { date: dateFromStartsAt(sub.starts_at) }) : t("usg.expired")}
                          </span>
                        )}
                        {sub.limits_override && <span className="badge-pill text-[11px]">{t("usg.overridden")}</span>}
                      </div>
                      <div className="mt-1.5">
                        <LimitStatusList items={sub.limit_status} variant="ring" />
                      </div>
                      {sub.ends_at && <div className="caption text-muted-soft">{t("usg.endsAt", { date: dateFromEndsAt(sub.ends_at) })}</div>}
                    </>
                  ) : (
                    <span className="badge-pill text-[11px] text-warning">{t("usg.noSubscription")}</span>
                  )}
                </div>
                <div className="flex shrink-0 flex-col gap-1.5">
                  <button type="button" className="btn btn-row btn-soft min-w-[86px]" disabled={busy} onClick={() => openAssign("tenant", String(row.tenant_id), row.tenant_name, sub)}>
                    {sub ? t("usg.changePlan") : t("usg.assignPlan")}
                  </button>
                  {sub && (
                    <button type="button" className="btn btn-row btn-soft-danger min-w-[86px]" disabled={busy} onClick={() => void cancelSub(sub)}>
                      {t("usg.cancelSub")}
                    </button>
                  )}
                </div>
              </div>
            );
          })}
        </div>
        )}
      </section>

      {/* Personal subscriptions */}
      <section className="card p-4">
        <div className="mb-3 flex items-center gap-3">
          <h3 className="text-[14px] font-medium text-ink">{t("usg.personal")}</h3>
          <span className="caption text-muted">{t("usg.personalDesc")}</span>
          <button
            className="btn btn-outline btn-sm ml-auto"
            disabled={busy || !plans.some((p) => p.scope_kind === "user" && p.is_active)}
            onClick={() => {
              setUserQ("");
              setUserHits([]);
              setUserTotal(0);
              setUserPickOpen(true);
            }}
          >
            <IconPlus className="h-3.5 w-3.5" /> {t("usg.assignPersonal")}
          </button>
        </div>
        {personal.length === 0 ? (
          <p className="caption text-muted-soft">{t("usg.noPersonal")}</p>
        ) : (
          /* One grid for the whole list (rows are subgrids) so the usage
           * and action columns line up even when a row has no buttons
           * (cancelled) or only one (implicit guest). */
          <div className="divide-y divide-hairline sm:grid sm:grid-cols-[minmax(0,1fr)_minmax(220px,auto)_auto_auto] sm:gap-x-6">
            {personal.map((sub) => (
              <div
                key={sub.id || `implicit-${sub.scope_id}`}
                className={`flex flex-wrap items-center gap-3 py-2.5 text-[14px] sm:col-span-4 sm:grid sm:grid-cols-subgrid sm:gap-x-2 ${ROW_HOVER}`}
              >
                <div className="min-w-0 flex-1 sm:pr-4">
                  <div className="truncate font-medium text-ink">{sub.scope_name}</div>
                  <div className="caption text-muted">
                    {sub.plan?.name ?? sub.plan_id}
                    {sub.implicit ? ` · ${t("usg.implicitGuest")}` : ""}
                    {sub.status !== "active" ? ` · ${t("usg.cancelled")}` : !sub.current ? ` · ${notStarted(sub) ? t("usg.notStarted", { date: dateFromStartsAt(sub.starts_at) }) : t("usg.expired")}` : ""}
                    {sub.ends_at ? ` · ${t("usg.endsAt", { date: dateFromEndsAt(sub.ends_at) })}` : ""}
                  </div>
                </div>
                <div className="min-w-0">
                  <LimitStatusList items={sub.limit_status} variant="ring" />
                </div>
                {/* Each button owns a grid column, so "Đổi gói" sits in the
                 * same place on every row; empty cells keep the slot. */}
                <div className="sm:ml-4">
                  {sub.status === "active" && (
                    <button type="button" className="btn btn-row btn-soft w-full min-w-[86px]" disabled={busy} onClick={() => openAssign("user", sub.scope_id, sub.scope_name, sub)}>
                      {t("usg.changePlan")}
                    </button>
                  )}
                </div>
                <div>
                  {sub.status === "active" && !sub.implicit && (
                    <button type="button" className="btn btn-row btn-soft-danger w-full min-w-[86px]" disabled={busy} onClick={() => void cancelSub(sub)}>
                      {t("usg.cancelSub")}
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Plan modal */}
      <Modal open={planOpen} title={planEditing ? t("usg.editPlan") : t("usg.newPlan")} onClose={() => setPlanOpen(false)} width="w-[880px]">
        <form className="space-y-4" onSubmit={submitPlan}>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="block">
              <FieldLabel htmlFor="plan-code" required>{t("usg.planCode")}</FieldLabel>
              <input id="plan-code" className="input" required aria-required="true" value={planForm.code} onChange={(e) => setPlanForm({ ...planForm, code: e.target.value })} />
            </div>
            <div className="block">
              <FieldLabel htmlFor="plan-name" required>{t("usg.planName")}</FieldLabel>
              <input id="plan-name" className="input" required aria-required="true" value={planForm.name} onChange={(e) => setPlanForm({ ...planForm, name: e.target.value })} />
            </div>
          </div>
          <div className="block">
            <FieldLabel htmlFor="plan-desc">{t("usg.planDesc")}</FieldLabel>
            <input id="plan-desc" className="input" value={planForm.description} onChange={(e) => setPlanForm({ ...planForm, description: e.target.value })} />
          </div>
          <div className="grid gap-3 sm:grid-cols-3">
            <div className="block">
              <FieldLabel htmlFor="plan-scope">{t("usg.planScope")}</FieldLabel>
              <select id="plan-scope" className="input" value={planForm.scope_kind} disabled={!!planEditing} onChange={(e) => setPlanForm({ ...planForm, scope_kind: e.target.value as UsageScopeKind })}>
                <option value="tenant">{t("usg.scopeTenant")}</option>
                <option value="user">{t("usg.scopeUser")}</option>
              </select>
            </div>
            {planForm.scope_kind === "tenant" && (
              <div className="block">
                <FieldLabel htmlFor="plan-mode">{t("usg.planMode")}</FieldLabel>
                <select id="plan-mode" className="input" value={planForm.mode} onChange={(e) => setPlanForm({ ...planForm, mode: e.target.value as UsageMode })}>
                  <option value="pool">{t("usg.modePool")}</option>
                  <option value="seat">{t("usg.modeSeat")}</option>
                </select>
              </div>
            )}
          </div>
          {/* pool/seat only exist for organization plans */}
          {planForm.scope_kind === "tenant" && (
            <p className="caption text-muted-soft">{planForm.mode === "seat" ? t("usg.modeSeatHint") : t("usg.modePoolHint")}</p>
          )}
          {/* The three prices on one row of their own: sharing a grid with
           * scope/mode split them over two rows (one or two per row). */}
          <div className="grid gap-3 sm:grid-cols-3">
            <div className="block">
              <FieldLabel htmlFor="plan-price">{t("usg.planPrice")}</FieldLabel>
              <PriceInput id="plan-price" value={planForm.price_vnd} onChange={(v) => setPlanForm({ ...planForm, price_vnd: v })} />
              <span className="caption mt-1 block text-muted-soft">
                {planForm.price_vnd > 0 ? t("usg.planPriceHint", { price: formatPriceVND(planForm.price_vnd) }) : t("usg.planPriceFree")}
              </span>
            </div>
            <div className="block">
              <FieldLabel htmlFor="plan-price-quarter">{t("usg.planPriceQuarter")}</FieldLabel>
              <PriceInput id="plan-price-quarter" value={planForm.price_quarter_vnd} onChange={(v) => setPlanForm({ ...planForm, price_quarter_vnd: v })} />
              <span className="caption mt-1 block text-muted-soft">{termHint(planForm.price_quarter_vnd, planForm.price_vnd, 3)}</span>
            </div>
            <div className="block">
              <FieldLabel htmlFor="plan-price-year">{t("usg.planPriceYear")}</FieldLabel>
              <PriceInput id="plan-price-year" value={planForm.price_year_vnd} onChange={(v) => setPlanForm({ ...planForm, price_year_vnd: v })} />
              <span className="caption mt-1 block text-muted-soft">{termHint(planForm.price_year_vnd, planForm.price_vnd, 12)}</span>
            </div>
          </div>
          <div>
            <span className="caption mb-2 block text-muted">{t("usg.planLimits")}</span>
            <LimitsEditor value={planForm.limits} onChange={(limits) => setPlanForm({ ...planForm, limits })} />
          </div>
          <div>
            <span className="caption mb-2 block text-muted">{t("usg.planFeatures")}</span>
            <FeaturesEditor
              value={planForm.features ?? {}}
              onChange={(features) => setPlanForm({ ...planForm, features })}
            />
          </div>
          <fieldset>
            <legend className="caption mb-2 block text-muted">{t("usg.planIncludedAgents")}</legend>
            {(() => {
              const chosen = planForm.included_agent_ids ?? [];
              const liveIds = new Set(catalogue.addon_agents.map((a) => a.id));
              // ticked ids whose agent was switched off, deleted or converted:
              // shown struck through so they can still be unticked
              const dead = chosen
                .filter((id) => !liveIds.has(id))
                .map((id) => ({ id, name: catalogue.addons.find((r) => r.agent_id === id)?.agent_name || id, live: false }));
              const choices = [...catalogue.addon_agents.map((a) => ({ id: a.id, name: a.name, live: true })), ...dead];
              if (choices.length === 0) return <p className="caption text-muted-soft">{t("usg.planIncludedAgentsNone")}</p>;
              return (
                <div className="flex flex-wrap gap-x-4 gap-y-2">
                  {choices.map((a) => (
                    <label key={a.id} className="flex items-center gap-2">
                      <input
                        type="checkbox"
                        checked={chosen.includes(a.id)}
                        onChange={() => setPlanForm({ ...planForm, included_agent_ids: togglePlanId(chosen, a.id) })}
                      />
                      <span className={a.live ? "caption text-body" : "caption text-muted-soft line-through"}>
                        {a.live ? a.name : t("usg.planIncludedAgentUnavailable", { name: a.name })}
                      </span>
                    </label>
                  ))}
                </div>
              );
            })()}
            <p className="caption mt-1 text-muted-soft">{t("usg.planIncludedAgentsHint")}</p>
          </fieldset>
          <div className="flex items-center gap-4">
            <div className="flex items-center gap-2">
              <FieldLabel htmlFor="plan-order" className="mb-0">{t("usg.planOrder")}</FieldLabel>
              <input id="plan-order" type="number" className="input h-9 py-0 w-20" value={planForm.sort_order} onChange={(e) => setPlanForm({ ...planForm, sort_order: Number(e.target.value) || 0 })} />
            </div>
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={planForm.is_active} onChange={(e) => setPlanForm({ ...planForm, is_active: e.target.checked })} />
              <span className="caption text-body">{t("usg.planActive")}</span>
            </label>
          </div>
          <div className="flex justify-end">
            <button type="submit" className="btn btn-primary" disabled={busy || !planDirty || !planForm.code.trim() || !planForm.name.trim()}>
              {t("common.save")}
            </button>
          </div>
        </form>
      </Modal>

      {/* Subscription modal */}
      <Modal open={subOpen} title={subTarget ? `${subTarget.existing ? t("usg.changePlan") : t("usg.assignPlan")} · ${subTarget.name}` : ""} onClose={() => setSubOpen(false)}>
        <form className="space-y-4" onSubmit={submitSub}>
          <div className="block">
            <FieldLabel htmlFor="sub-plan" required>{t("usg.plan")}</FieldLabel>
            <select
              id="sub-plan"
              className="input"
              required
              aria-required="true"
              value={subForm.plan_id}
              onChange={(e) => {
                const p = planByID(e.target.value);
                const ex = subTarget?.existing;
                // Changing the plan opens a new subscription, which starts today
                // unless a date is picked; keeping the plan keeps its start.
                const samePlan = !!ex && ex.plan_id === e.target.value;
                setSubForm({
                  ...subForm,
                  plan_id: e.target.value,
                  mode: p?.mode ?? subForm.mode,
                  custom_start: samePlan,
                  starts_at: samePlan ? dateFromStartsAt(ex.starts_at) || todayVN() : todayVN(),
                });
              }}
            >
              <option value="">—</option>
              {selectablePlans.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} ({p.code}){p.is_active ? "" : ` · ${t("usg.inactive")}`}
                </option>
              ))}
            </select>
          </div>
          {subTarget?.kind === "tenant" && (
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="block">
                <FieldLabel htmlFor="sub-mode">{t("usg.planMode")}</FieldLabel>
                <select id="sub-mode" className="input" value={subForm.mode} onChange={(e) => setSubForm({ ...subForm, mode: e.target.value as UsageMode })}>
                  <option value="pool">{t("usg.modePool")}</option>
                  <option value="seat">{t("usg.modeSeat")}</option>
                </select>
              </div>
              {subForm.mode === "seat" && (
                <div className="block">
                  <FieldLabel htmlFor="sub-seats">{t("usg.seats")}</FieldLabel>
                  <input id="sub-seats" type="number" min={1} className="input" value={subForm.seats} onChange={(e) => setSubForm({ ...subForm, seats: Math.max(0, Number(e.target.value) || 0) })} />
                </div>
              )}
            </div>
          )}
          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <FieldLabel htmlFor="sub-start">{t("usg.startsAtLabel")}</FieldLabel>
              <input
                id="sub-start"
                type="date"
                className="input"
                disabled={!subForm.custom_start}
                value={subForm.starts_at}
                onChange={(e) => setSubForm({ ...subForm, starts_at: e.target.value })}
              />
              <label className="mt-1.5 flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={subForm.custom_start}
                  onChange={(e) => setSubForm({ ...subForm, custom_start: e.target.checked, starts_at: subForm.starts_at || todayVN() })}
                />
                <span className="caption text-muted-soft">{subTarget?.existing ? t("usg.startsKeep") : t("usg.startsCustom")}</span>
              </label>
            </div>
            <div className="block">
              <FieldLabel htmlFor="sub-end">{t("usg.endsAtLabel")}</FieldLabel>
              <input id="sub-end" type="date" className="input" value={subForm.ends_at} onChange={(e) => setSubForm({ ...subForm, ends_at: e.target.value })} />
              {endsBeforeStart && <span className="caption mt-1 block text-error">{t("usg.endsBeforeStart")}</span>}
            </div>
          </div>
          {/* Quick lengths, full width so the two date fields stay level:
           * each sets the end date from the start. */}
          <div role="group" aria-labelledby="sub-length-label">
            <span id="sub-length-label" className="caption mb-1.5 block text-muted">{t("usg.quickLength")}</span>
            <div className="flex flex-wrap items-center gap-1.5">
              {SUB_LENGTHS.map((m) => {
                const end = endForMonths(subStart, m);
                return (
                  <button
                    key={m}
                    type="button"
                    aria-pressed={subForm.ends_at === end}
                    className={`btn btn-row ${subForm.ends_at === end ? "btn-soft-accent" : "btn-soft"}`}
                    onClick={() => setSubForm({ ...subForm, ends_at: end })}
                  >
                    {t("bill.monthsN", { n: m })}
                  </button>
                );
              })}
              <button
                type="button"
                aria-pressed={!subForm.ends_at}
                className={`btn btn-row ${!subForm.ends_at ? "btn-soft-accent" : "btn-soft"}`}
                onClick={() => setSubForm({ ...subForm, ends_at: "" })}
              >
                {t("usg.priceOpenEnded")}
              </button>
            </div>
          </div>
          <div className="block">
            <FieldLabel htmlFor="sub-note">{t("usg.note")}</FieldLabel>
            <input id="sub-note" className="input" value={subForm.note} onChange={(e) => setSubForm({ ...subForm, note: e.target.value })} />
          </div>
          {subTarget?.existing && subTarget.existing.plan_id !== subForm.plan_id && (
            <p className="caption text-muted-soft">{t("usg.changePlanHint")}</p>
          )}
          <SubPriceSummary
            plan={planByID(subForm.plan_id)}
            seatMode={subTarget?.kind === "tenant" && subForm.mode === "seat"}
            seats={subForm.seats}
            start={subStart}
            end={endsBeforeStart ? "" : subForm.ends_at}
          />
          <div className="flex justify-end">
            <button type="submit" className="btn btn-primary" disabled={busy || !subDirty || !subForm.plan_id || endsBeforeStart || (subForm.mode === "seat" && subTarget?.kind === "tenant" && subForm.seats < 1)}>
              {t("common.save")}
            </button>
          </div>
        </form>
      </Modal>

      {/* Personal: pick an account */}
      <Modal open={userPickOpen} title={t("usg.assignPersonal")} onClose={() => setUserPickOpen(false)}>
        <div className="space-y-3">
          <div className="relative">
            <IconSearch className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-soft" />
            <input className="input h-9 py-0 pl-9" autoFocus aria-label={t("usg.searchUser")} placeholder={t("usg.searchUser")} value={userQ} onChange={(e) => setUserQ(e.target.value)} />
          </div>
          <p className="caption text-muted">{t("usg.userCount", { n: userTotal })}</p>
          <div className="max-h-96 divide-y divide-hairline overflow-y-auto">
            {userLoading && <Loading label={t("common.loading")} className="py-4" />}
            {userHits.map((u) => {
              const sub = personal.find((s) => s.scope_id === u.id && s.status === "active" && !s.implicit) ?? null;
              const guestRow = personal.find((s) => s.scope_id === u.id && s.implicit);
              const orgs = (u.memberships ?? []).map((m) => m.tenant_name).filter(Boolean);
              const unit = u.preferences?.work_unit;
              return (
                <button
                  key={u.id}
                  type="button"
                  disabled={!u.is_active}
                  className={`block w-full px-2 py-2 text-left text-[14px] disabled:opacity-50 ${ROW_HOVER}`}
                  onClick={() => {
                    setUserPickOpen(false);
                    openAssign("user", u.id, u.username || u.email, sub);
                  }}
                >
                  {/* Name and e-mail get the full width; the badges go on
                   * their own line (two uppercase badges beside them cut
                   * the name down to a few letters in a 460px dialog). */}
                  <span className="block min-w-0">
                    <span className="block truncate text-ink">{u.username || u.email}</span>
                    <span className="caption block truncate text-muted">
                      {u.email}
                      {unit ? ` · ${unit}` : ""}
                    </span>
                  </span>
                  <span className="mt-1 flex flex-wrap gap-1">
                    {u.is_system_admin ? (
                      <span className="badge-pill text-[11px]">{t("usg.kindSuperAdmin")}</span>
                    ) : orgs.length > 0 ? (
                      <span className="badge-pill max-w-[260px] text-[11px]" title={orgs.join(", ")}>
                        <span className="min-w-0 truncate">{orgs.join(", ")}</span>
                      </span>
                    ) : (
                      <span className="badge-pill text-[11px]">{t("usg.kindGuest")}</span>
                    )}
                    {!u.is_active && <span className="badge-pill text-[11px]">{t("usg.kindInactive")}</span>}
                    <span className="badge-pill text-[11px] text-muted">
                      {sub?.plan?.name ?? guestRow?.plan?.name ?? t("usg.noOwnPlan")}
                    </span>
                  </span>
                </button>
              );
            })}
            {!userLoading && userHits.length === 0 && <p className="caption px-2 py-2 text-muted-soft">{t("usg.noUserHits")}</p>}
          </div>
          {userHits.length < userTotal && (
            <LoadMore
              loading={userLoading}
              label={t("usg.moreUsers", { n: userTotal - userHits.length })}
              onMore={() => void fetchUsers(userQ, userHits.length)}
              className="mt-2"
            />
          )}
        </div>
      </Modal>
      </div>
    </SystemSection>
  );
}
