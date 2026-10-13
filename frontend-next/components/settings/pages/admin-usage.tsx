"use client";

/* SuperAdmin usage packages: the enforcement switch, the plans (runtime
 * data, no seeded plans), every workspace next to its subscription, and
 * personal subscriptions. Limits are a list of {metric, period, limit};
 * a subscription may override its plan's list. */

import { useCallback, useEffect, useMemo, useState } from "react";
import { Modal } from "@/components/modal";
import { Toggle } from "@/components/settings/toggle";
import { LimitStatusList } from "@/components/settings/usage-limits";
import { IconEdit, IconPlus, IconSearch } from "@/components/icons";
import { useT } from "@/lib/i18n";
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
  planPrice,
} from "@/lib/api/usage";

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
        <div key={i} className="flex items-center gap-2">
          <select className="input h-9 py-0 w-36" value={l.metric} onChange={(e) => set(i, { metric: e.target.value as UsageLimit["metric"] })}>
            <option value="questions">{t("usg.metricQuestions")}</option>
            <option value="tokens">{t("usg.metricTokens")}</option>
          </select>
          <span className="caption text-muted">/</span>
          <select className="input h-9 py-0 w-28" value={l.period} onChange={(e) => set(i, { period: e.target.value as UsageLimit["period"] })}>
            <option value="day">{t("usg.periodDay")}</option>
            <option value="month">{t("usg.periodMonth")}</option>
          </select>
          <input
            type="number"
            min={0}
            className="input h-9 py-0 w-32"
            value={l.limit}
            onChange={(e) => set(i, { limit: Math.max(0, Number(e.target.value) || 0) })}
          />
          <button type="button" className="btn btn-outline btn-sm" onClick={() => onChange(value.filter((_, j) => j !== i))}>
            ✕
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

  const [enforced, setEnforced] = useState(false);
  const [plans, setPlans] = useState<UsagePlan[]>([]);
  const [tenants, setTenants] = useState<UsageTenantOverview[]>([]);
  const [personal, setPersonal] = useState<UsageSubscriptionView[]>([]);
  const [q, setQ] = useState("");

  const [planOpen, setPlanOpen] = useState(false);
  const [planEditing, setPlanEditing] = useState<UsagePlan | null>(null);
  const [planForm, setPlanForm] = useState<UsagePlanInput>(emptyPlan);

  const [subOpen, setSubOpen] = useState(false);
  const [subTarget, setSubTarget] = useState<{ kind: UsageScopeKind; id: string; name: string; existing?: UsageSubscriptionView | null } | null>(null);
  const [subForm, setSubForm] = useState<SubForm>(subFormFrom(null, [], "tenant"));

  const [userPickOpen, setUserPickOpen] = useState(false);
  const [userQ, setUserQ] = useState("");
  const [userHits, setUserHits] = useState<SystemAdminUser[]>([]);

  const load = useCallback(async () => {
    try {
      const [setting, pl, tn, ps] = await Promise.all([
        getSystemSetting(USAGE_ENFORCE_SETTING_KEY),
        listUsagePlans(true),
        listUsageTenantOverview(),
        listUsageSubscriptions({ scope_type: "user" }),
      ]);
      setEnforced(setting.value === true || setting.value === "true");
      setPlans(pl);
      setTenants(tn);
      setPersonal(ps);
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : t("usg.loadFailed"));
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

  const toggleEnforce = (v: boolean) =>
    run(async () => {
      await updateSystemSetting(USAGE_ENFORCE_SETTING_KEY, v);
      setEnforced(v);
    });

  // ---- plans
  const openNewPlan = () => {
    setPlanEditing(null);
    setPlanForm(emptyPlan);
    setPlanOpen(true);
  };
  const openEditPlan = (p: UsagePlan) => {
    setPlanEditing(p);
    setPlanForm({
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
    });
    setPlanOpen(true);
  };
  const submitPlan = async (e: React.FormEvent) => {
    e.preventDefault();
    const ok = await run(async () => {
      if (planEditing) await updateUsagePlan(planEditing.id, planForm);
      else await createUsagePlan(planForm);
      await load();
    });
    if (ok) setPlanOpen(false);
  };
  const removePlan = (p: UsagePlan) => {
    if (!window.confirm(t("usg.deletePlanConfirm", { name: p.name }))) return;
    void run(async () => {
      await deleteUsagePlan(p.id);
      await load();
    });
  };

  // ---- subscriptions
  const openAssign = (kind: UsageScopeKind, id: string, name: string, existing?: UsageSubscriptionView | null) => {
    setSubTarget({ kind, id, name, existing });
    setSubForm(subFormFrom(existing, plans, kind));
    setSubOpen(true);
  };
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
    if (ok) setSubOpen(false);
  };
  const cancelSub = (sub: UsageSubscriptionView) => {
    if (!window.confirm(t("usg.cancelSubConfirm", { name: sub.scope_name }))) return;
    void run(async () => {
      await deleteUsageSubscription(sub.id);
      await load();
    });
  };

  const searchUsers = async (text: string) => {
    setUserQ(text);
    if (!text.trim()) {
      setUserHits([]);
      return;
    }
    try {
      const res = await listSystemUsers({ q: text.trim(), limit: 8 });
      setUserHits(res.users ?? []);
    } catch {
      setUserHits([]);
    }
  };

  if (allowed === null) {
    return <div className="mx-auto w-full max-w-[1100px] px-5 py-12 text-muted">…</div>;
  }
  if (!allowed) {
    return (
      <div className="mx-auto w-full max-w-[1100px] px-5 py-16 text-center">
        <h2 className="title-md mb-2">{t("usg.title")}</h2>
        <p className="body-sm text-muted">{t("usg.denied")}</p>
      </div>
    );
  }

  const planByID = (id: string) => plans.find((p) => p.id === id);
  const endsBeforeStart = !!subForm.ends_at && !!subForm.starts_at && subForm.ends_at < subForm.starts_at;
  const selectablePlans = plans.filter((p) => p.scope_kind === (subTarget?.kind ?? "tenant") && (p.is_active || p.id === subForm.plan_id));
  const filteredTenants = tenants.filter((x) => !q || x.tenant_name.toLowerCase().includes(q.toLowerCase()));

  return (
    <div className="mx-auto w-full max-w-[1400px] space-y-6">
      {error && <p className="caption text-error">{error}</p>}

      {/* Enforcement */}
      <div className="card flex flex-wrap items-center gap-4 p-4">
        <div className="min-w-0 flex-1">
          <div className="text-[14px] font-medium text-ink">{t("usg.enforceTitle")}</div>
          <p className="caption text-muted">{t("usg.enforceDesc")}</p>
          {withoutPlan > 0 && (
            <p className={`caption mt-1 ${enforced ? "text-error" : "text-amber-600"}`}>
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
          <div className="divide-y divide-hairline">
            {plans.map((p) => (
              <div key={p.id} className="flex flex-wrap items-center gap-3 py-2.5 text-[14px]">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium text-ink">{p.name}</span>
                    <span className="caption font-mono text-muted-soft">{p.code}</span>
                    {!p.is_active && <span className="badge-pill text-[11px]">{t("usg.inactive")}</span>}
                  </div>
                  <div className="caption text-muted">
                    {p.scope_kind === "tenant" ? t("usg.scopeTenant") : t("usg.scopeUser")}
                    {p.scope_kind === "tenant" ? ` · ${p.mode === "seat" ? t("usg.modeSeat") : t("usg.modePool")}` : ""}
                    {planPrice(p) ? ` · ${planPrice(p)}${p.mode === "seat" ? t("usg.perSeatMonth") : t("usg.perMonth")}` : ""}
                    {p.description ? ` · ${p.description}` : ""}
                  </div>
                </div>
                <div className="caption text-body">
                  {p.limits.length ? p.limits.map((l) => limitText(l, t as never)).join(" · ") : t("usg.unlimited")}
                </div>
                <button type="button" className="btn btn-outline btn-sm" disabled={busy} onClick={() => openEditPlan(p)}>
                  <IconEdit className="h-3.5 w-3.5" /> {t("common.edit")}
                </button>
                <button type="button" className="btn btn-outline btn-sm text-rose-600" disabled={busy} onClick={() => removePlan(p)}>
                  {t("common.delete")}
                </button>
              </div>
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
            <input className="input h-9 py-0 w-56 pl-9" placeholder={t("usg.searchTenant")} value={q} onChange={(e) => setQ(e.target.value)} />
          </div>
        </div>
        <div className="divide-y divide-hairline">
          {filteredTenants.map((row) => {
            const sub = row.subscription;
            return (
              <div key={row.tenant_id} className="flex flex-wrap items-start gap-3 py-2.5 text-[14px]">
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium text-ink">
                    {row.tenant_name} <span className="caption text-muted-soft">#{row.tenant_id}</span>
                  </div>
                  <div className="caption text-muted">
                    {t("usg.monthUsage", { questions: row.month.questions.toLocaleString(), tokens: formatTokens(row.month.tokens) })}
                  </div>
                </div>
                <div className="min-w-[220px] flex-1">
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
                    <span className="badge-pill text-[11px] text-amber-700">{t("usg.noSubscription")}</span>
                  )}
                </div>
                <button type="button" className="btn btn-outline btn-sm" disabled={busy} onClick={() => openAssign("tenant", String(row.tenant_id), row.tenant_name, sub)}>
                  {sub ? t("usg.changePlan") : t("usg.assignPlan")}
                </button>
                {sub && (
                  <button type="button" className="btn btn-outline btn-sm text-rose-600" disabled={busy} onClick={() => cancelSub(sub)}>
                    {t("usg.cancelSub")}
                  </button>
                )}
              </div>
            );
          })}
        </div>
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
              <div key={sub.id} className="flex flex-wrap items-start gap-3 py-2.5 text-[14px]">
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium text-ink">{sub.scope_name}</div>
                  <div className="caption text-muted">
                    {sub.plan?.name ?? sub.plan_id}
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
                    <button type="button" className="btn btn-outline btn-sm text-rose-600" disabled={busy} onClick={() => cancelSub(sub)}>
                      {t("usg.cancelSub")}
                    </button>
                  </>
                )}
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Plan modal */}
      <Modal open={planOpen} title={planEditing ? t("usg.editPlan") : t("usg.newPlan")} onClose={() => setPlanOpen(false)}>
        <form className="space-y-4" onSubmit={submitPlan}>
          <div className="grid gap-3 sm:grid-cols-2">
            <label className="block">
              <span className="caption mb-1 block text-muted">{t("usg.planCode")}</span>
              <input className="input" required value={planForm.code} onChange={(e) => setPlanForm({ ...planForm, code: e.target.value })} />
            </label>
            <label className="block">
              <span className="caption mb-1 block text-muted">{t("usg.planName")}</span>
              <input className="input" required value={planForm.name} onChange={(e) => setPlanForm({ ...planForm, name: e.target.value })} />
            </label>
          </div>
          <label className="block">
            <span className="caption mb-1 block text-muted">{t("usg.planDesc")}</span>
            <input className="input" value={planForm.description} onChange={(e) => setPlanForm({ ...planForm, description: e.target.value })} />
          </label>
          <div className="grid gap-3 sm:grid-cols-3">
            <label className="block">
              <span className="caption mb-1 block text-muted">{t("usg.planScope")}</span>
              <select className="input" value={planForm.scope_kind} disabled={!!planEditing} onChange={(e) => setPlanForm({ ...planForm, scope_kind: e.target.value as UsageScopeKind })}>
                <option value="tenant">{t("usg.scopeTenant")}</option>
                <option value="user">{t("usg.scopeUser")}</option>
              </select>
            </label>
            {planForm.scope_kind === "tenant" && (
              <label className="block">
                <span className="caption mb-1 block text-muted">{t("usg.planMode")}</span>
                <select className="input" value={planForm.mode} onChange={(e) => setPlanForm({ ...planForm, mode: e.target.value as UsageMode })}>
                  <option value="pool">{t("usg.modePool")}</option>
                  <option value="seat">{t("usg.modeSeat")}</option>
                </select>
              </label>
            )}
            <label className="block">
              <span className="caption mb-1 block text-muted">{t("usg.planPrice")}</span>
              <div className="relative">
                <input
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
            </label>
          </div>
          <p className="caption text-muted-soft">{planForm.mode === "seat" && planForm.scope_kind === "tenant" ? t("usg.modeSeatHint") : t("usg.modePoolHint")}</p>
          <div>
            <span className="caption mb-2 block text-muted">{t("usg.planLimits")}</span>
            <LimitsEditor value={planForm.limits} onChange={(limits) => setPlanForm({ ...planForm, limits })} />
          </div>
          <div className="flex items-center gap-4">
            <label className="flex items-center gap-2">
              <span className="caption text-muted">{t("usg.planOrder")}</span>
              <input type="number" className="input h-9 py-0 w-20" value={planForm.sort_order} onChange={(e) => setPlanForm({ ...planForm, sort_order: Number(e.target.value) || 0 })} />
            </label>
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={planForm.is_active} onChange={(e) => setPlanForm({ ...planForm, is_active: e.target.checked })} />
              <span className="caption text-muted">{t("usg.planActive")}</span>
            </label>
          </div>
          <div className="flex justify-end">
            <button type="submit" className="btn btn-primary" disabled={busy || !planForm.code.trim() || !planForm.name.trim()}>
              {t("common.save")}
            </button>
          </div>
        </form>
      </Modal>

      {/* Subscription modal */}
      <Modal open={subOpen} title={subTarget ? `${subTarget.existing ? t("usg.changePlan") : t("usg.assignPlan")} · ${subTarget.name}` : ""} onClose={() => setSubOpen(false)}>
        <form className="space-y-4" onSubmit={submitSub}>
          <label className="block">
            <span className="caption mb-1 block text-muted">{t("usg.plan")}</span>
            <select
              className="input"
              required
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
          </label>
          {subTarget?.kind === "tenant" && (
            <div className="grid gap-3 sm:grid-cols-2">
              <label className="block">
                <span className="caption mb-1 block text-muted">{t("usg.planMode")}</span>
                <select className="input" value={subForm.mode} onChange={(e) => setSubForm({ ...subForm, mode: e.target.value as UsageMode })}>
                  <option value="pool">{t("usg.modePool")}</option>
                  <option value="seat">{t("usg.modeSeat")}</option>
                </select>
              </label>
              {subForm.mode === "seat" && (
                <label className="block">
                  <span className="caption mb-1 block text-muted">{t("usg.seats")}</span>
                  <input type="number" min={1} className="input" value={subForm.seats} onChange={(e) => setSubForm({ ...subForm, seats: Math.max(0, Number(e.target.value) || 0) })} />
                </label>
              )}
            </div>
          )}
          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <span className="caption mb-1 block text-muted">{t("usg.startsAtLabel")}</span>
              <input
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
            <label className="block">
              <span className="caption mb-1 block text-muted">{t("usg.endsAtLabel")}</span>
              <input type="date" className="input" value={subForm.ends_at} onChange={(e) => setSubForm({ ...subForm, ends_at: e.target.value })} />
              {endsBeforeStart && <span className="caption mt-1 block text-error">{t("usg.endsBeforeStart")}</span>}
            </label>
          </div>
          <label className="block">
            <span className="caption mb-1 block text-muted">{t("usg.note")}</span>
            <input className="input" value={subForm.note} onChange={(e) => setSubForm({ ...subForm, note: e.target.value })} />
          </label>
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
            <button type="submit" className="btn btn-primary" disabled={busy || !subForm.plan_id || endsBeforeStart || (subForm.mode === "seat" && subTarget?.kind === "tenant" && subForm.seats < 1)}>
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
            <input className="input h-9 py-0 pl-9" autoFocus placeholder={t("usg.searchUser")} value={userQ} onChange={(e) => void searchUsers(e.target.value)} />
          </div>
          <div className="max-h-72 divide-y divide-hairline overflow-y-auto">
            {userHits.map((u) => (
              <button
                key={u.id}
                type="button"
                className="flex w-full items-center justify-between px-2 py-2 text-left text-[14px] hover:bg-surface-strong/40"
                onClick={() => {
                  setUserPickOpen(false);
                  const existing = personal.find((s) => s.scope_id === u.id && s.status === "active") ?? null;
                  openAssign("user", u.id, u.username || u.email, existing);
                }}
              >
                <span className="truncate text-ink">{u.username}</span>
                <span className="caption truncate text-muted">{u.email}</span>
              </button>
            ))}
            {userQ && userHits.length === 0 && <p className="caption px-2 py-2 text-muted-soft">{t("usg.noUserHits")}</p>}
          </div>
        </div>
      </Modal>
    </div>
  );
}
