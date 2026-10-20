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
  type UsageAgentAddonCatalogue,
} from "@/lib/api/usage";
import { addonsForPlan } from "@/lib/agent-addon-catalogue";

const USER_PAGE = 30;

const emptyPlan: UsagePlanInput = {
  code: "",
  name: "",
  description: "",
  scope_kind: "tenant",
  mode: "pool",
  limits: [{ metric: "questions", period: "month", limit: 0 }],
  features: {},
  price_label: "",
  price_vnd: 0,
  sort_order: 0,
  is_active: true,
};

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
  // Render in Vietnam time to match endsAtFromDate.
  const vn = new Date(d.getTime() + 7 * 3600_000);
  return vn.toISOString().slice(0, 10);
}

function notStarted(sub: { starts_at: string }): boolean {
  const d = new Date(sub.starts_at);
  return !Number.isNaN(d.getTime()) && d.getTime() > Date.now();
}

function LimitsEditor({ value, onChange }: { value: UsageLimit[]; onChange: (v: UsageLimit[]) => void }) {
  const { t } = useT();
  const set = (i: number, patch: Partial<UsageLimit>) =>
    onChange(value.map((l, j) => (j === i ? { ...l, ...patch } : l)));
  return (
    <div className="space-y-2">
      {value.map((l, i) => (
        <div key={i} className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-2 sm:flex">
          <select
            className="input h-9 min-w-0 py-0 sm:w-36"
            aria-label={l.metric === "tokens" ? t("usg.metricTokens") : t("usg.metricQuestions")}
            value={l.metric}
            onChange={(e) => set(i, { metric: e.target.value as UsageLimit["metric"] })}
          >
            <option value="questions">{t("usg.metricQuestions")}</option>
            <option value="tokens">{t("usg.metricTokens")}</option>
          </select>
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
  override: boolean;
  limits: UsageLimit[];
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
      override: false,
      limits: [],
    };
  }
  return {
    plan_id: sub.plan_id,
    mode: sub.mode,
    seats: sub.seats,
    starts_at: dateFromEndsAt(sub.starts_at) || todayVN(),
    custom_start: true,
    ends_at: dateFromEndsAt(sub.ends_at),
    note: sub.note,
    override: sub.limits_override != null,
    limits: sub.limits_override ?? sub.plan?.limits ?? [],
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
  const [plans, setPlans] = useState<UsagePlan[]>([]);
  const [tenants, setTenants] = useState<UsageTenantOverview[]>([]);
  const [personal, setPersonal] = useState<UsageSubscriptionView[]>([]);
  // basic agents every plan includes + specialist agents sold with plans
  const [catalogue, setCatalogue] = useState<UsageAgentAddonCatalogue>({ addons: [], included_agents: [] });
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
      const [setting, pl, tn, ps] = await Promise.all([
        getSystemSetting(USAGE_ENFORCE_SETTING_KEY),
        listUsagePlans(true),
        listUsageTenantOverview(),
        listUsageSubscriptions({ scope_type: "user" }),
      ]);
      setEnforced(!(setting.value === false || setting.value === "false"));
      setPlans(pl);
      setTenants(tn);
      setPersonal(ps);
      setError("");
      // the plan cards still render if the catalogue cannot be read
      listAgentAddons(true)
        .then(setCatalogue)
        .catch(() => setCatalogue({ addons: [], included_agents: [] }));
    } catch (e) {
      setError(e instanceof Error ? e.message : t("usg.loadFailed"));
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
      setError(e instanceof Error ? e.message : t("usg.opFailed"));
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
      sort_order: p.sort_order,
      is_active: p.is_active,
    };
    setPlanForm(form);
    setPlanBase(form);
    setPlanOpen(true);
  };
  const planDirty = JSON.stringify(planForm) !== JSON.stringify(planBase);
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
      limits_override: subForm.override ? subForm.limits : null,
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
  const endsBeforeStart = !!subForm.ends_at && !!subForm.starts_at && subForm.ends_at < subForm.starts_at;
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
              <article key={p.id} className="flex min-w-0 flex-col rounded-xl border border-hairline bg-surface-card p-5 text-[14px]">
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
                  {p.description && <p className="caption whitespace-pre-line leading-relaxed text-muted">{p.description}</p>}
                  {catalogue.included_agents.length > 0 && (
                    <div className="caption text-muted">
                      {t("usg.includedAgents", {
                        n: catalogue.included_agents.length,
                        names: catalogue.included_agents.map((a) => a.name).join(", "),
                      })}
                    </div>
                  )}
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
                <button type="button" className="btn btn-outline btn-sm" disabled={busy} onClick={() => openEditPlan(p)}>
                  <IconEdit className="h-3.5 w-3.5" /> {t("common.edit")}
                </button>
                <button type="button" className="btn btn-danger btn-sm" disabled={busy} onClick={() => void removePlan(p)}>
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
                            {notStarted(sub) ? t("usg.notStarted", { date: dateFromEndsAt(sub.starts_at) }) : t("usg.expired")}
                          </span>
                        )}
                        {sub.limits_override && <span className="badge-pill text-[11px]">{t("usg.overridden")}</span>}
                      </div>
                      <LimitStatusList items={sub.limit_status} />
                      {sub.ends_at && <div className="caption text-muted-soft">{t("usg.endsAt", { date: dateFromEndsAt(sub.ends_at) })}</div>}
                    </>
                  ) : (
                    <span className="badge-pill text-[11px] text-warning">{t("usg.noSubscription")}</span>
                  )}
                </div>
                <div className="flex shrink-0 flex-col gap-1.5">
                  <button type="button" className="btn btn-outline btn-sm min-w-[86px]" disabled={busy} onClick={() => openAssign("tenant", String(row.tenant_id), row.tenant_name, sub)}>
                    {sub ? t("usg.changePlan") : t("usg.assignPlan")}
                  </button>
                  {sub && (
                    <button type="button" className="btn btn-danger btn-sm min-w-[86px]" disabled={busy} onClick={() => void cancelSub(sub)}>
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
          <div className="divide-y divide-hairline">
            {personal.map((sub) => (
              <div key={sub.id || `implicit-${sub.scope_id}`} className={`flex flex-wrap items-start gap-3 py-2.5 text-[14px] ${ROW_HOVER}`}>
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium text-ink">{sub.scope_name}</div>
                  <div className="caption text-muted">
                    {sub.plan?.name ?? sub.plan_id}
                    {sub.implicit ? ` · ${t("usg.implicitGuest")}` : ""}
                    {sub.status !== "active" ? ` · ${t("usg.cancelled")}` : !sub.current ? ` · ${notStarted(sub) ? t("usg.notStarted", { date: dateFromEndsAt(sub.starts_at) }) : t("usg.expired")}` : ""}
                    {sub.ends_at ? ` · ${t("usg.endsAt", { date: dateFromEndsAt(sub.ends_at) })}` : ""}
                  </div>
                </div>
                <div className="min-w-[220px] flex-1">
                  <LimitStatusList items={sub.limit_status} />
                </div>
                {sub.status === "active" && (
                  <>
                    <button type="button" className="btn btn-outline btn-sm" disabled={busy} onClick={() => openAssign("user", sub.scope_id, sub.scope_name, sub)}>
                      {t("usg.changePlan")}
                    </button>
                    {!sub.implicit && (
                      <button type="button" className="btn btn-danger btn-sm" disabled={busy} onClick={() => void cancelSub(sub)}>
                        {t("usg.cancelSub")}
                      </button>
                    )}
                  </>
                )}
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
            <div className="block">
              <FieldLabel htmlFor="plan-price">{t("usg.planPrice")}</FieldLabel>
              <div className="relative">
                <input
                  id="plan-price"
                  className="input pr-14"
                  inputMode="numeric"
                  placeholder="0"
                  value={planForm.price_vnd ? String(planForm.price_vnd) : ""}
                  onChange={(e) => setPlanForm({ ...planForm, price_vnd: Number(e.target.value.replace(/[^\d]/g, "")) || 0 })}
                />
                <span className="caption pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-muted-soft">VNĐ</span>
              </div>
              <span className="caption mt-1 block text-muted-soft">
                {planForm.price_vnd > 0 ? t("usg.planPriceHint", { price: formatPriceVND(planForm.price_vnd) }) : t("usg.planPriceFree")}
              </span>
            </div>
          </div>
          <p className="caption text-muted-soft">{planForm.mode === "seat" && planForm.scope_kind === "tenant" ? t("usg.modeSeatHint") : t("usg.modePoolHint")}</p>
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
          <div className="flex items-center gap-4">
            <div className="flex items-center gap-2">
              <FieldLabel htmlFor="plan-order" className="mb-0">{t("usg.planOrder")}</FieldLabel>
              <input id="plan-order" type="number" className="input h-9 py-0 w-20" value={planForm.sort_order} onChange={(e) => setPlanForm({ ...planForm, sort_order: Number(e.target.value) || 0 })} />
            </div>
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={planForm.is_active} onChange={(e) => setPlanForm({ ...planForm, is_active: e.target.checked })} />
              <span className="caption text-muted">{t("usg.planActive")}</span>
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
                  limits: subForm.override ? subForm.limits : (p?.limits ?? []),
                  custom_start: samePlan,
                  starts_at: samePlan ? dateFromEndsAt(ex.starts_at) || todayVN() : todayVN(),
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
          <div className="block">
            <FieldLabel htmlFor="sub-note">{t("usg.note")}</FieldLabel>
            <input id="sub-note" className="input" value={subForm.note} onChange={(e) => setSubForm({ ...subForm, note: e.target.value })} />
          </div>
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={subForm.override}
              onChange={(e) => setSubForm({ ...subForm, override: e.target.checked, limits: e.target.checked ? (subForm.limits.length ? subForm.limits : (planByID(subForm.plan_id)?.limits ?? [])) : subForm.limits })}
            />
            <span className="caption text-muted">{t("usg.overrideLimits")}</span>
          </label>
          {subForm.override && <LimitsEditor value={subForm.limits} onChange={(limits) => setSubForm({ ...subForm, limits })} />}
          {subTarget?.existing && subTarget.existing.plan_id !== subForm.plan_id && (
            <p className="caption text-muted-soft">{t("usg.changePlanHint")}</p>
          )}
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
                  className={`flex w-full flex-wrap items-start justify-between gap-x-3 gap-y-1 px-2 py-2 text-left text-[14px] disabled:opacity-50 ${ROW_HOVER}`}
                  onClick={() => {
                    setUserPickOpen(false);
                    openAssign("user", u.id, u.username || u.email, sub);
                  }}
                >
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-ink">{u.username || u.email}</span>
                    <span className="caption block truncate text-muted">
                      {u.email}
                      {unit ? ` · ${unit}` : ""}
                    </span>
                  </span>
                  <span className="flex flex-wrap justify-end gap-1">
                    {u.is_system_admin ? (
                      <span className="badge-pill text-[11px]">{t("usg.kindSuperAdmin")}</span>
                    ) : orgs.length > 0 ? (
                      <span className="badge-pill max-w-[220px] truncate text-[11px]" title={orgs.join(", ")}>
                        {orgs.join(", ")}
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
