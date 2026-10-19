"use client";

/* SuperAdmin billing: the bank account that receives transfers (VietQR),
 * promo codes, the orders to confirm once the money arrived, and the
 * secret of the payment checker that confirms them automatically. */

import { useCallback, useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { IconCopy, IconEdit, IconPlus, IconRefresh } from "@/components/icons";
import { copyToClipboard } from "@/lib/clipboard";
import { useT, type LocaleKey } from "@/lib/i18n";
import { getCurrentUser } from "@/lib/api/auth";
import { getSystemSetting, updateSystemSetting } from "@/lib/api/system";
import {
  BILLING_SETTING_KEYS,
  cancelUsageOrder,
  confirmUsageOrder,
  createUsagePromo,
  deleteUsagePromo,
  formatPriceVND,
  listAllUsageOrders,
  listUsagePlans,
  listUsagePromos,
  updateUsagePromo,
  type UsageOrder,
  type UsageOrderStatus,
  type UsagePlan,
  type UsagePromoCode,
  type UsagePromoInput,
} from "@/lib/api/usage";
import { formatDateTime } from "@/lib/datetime";

type BankForm = { bankBin: string; bankName: string; accountNo: string; accountName: string; orderTTLHours: string; contact: string };
const emptyBank: BankForm = { bankBin: "", bankName: "", accountNo: "", accountName: "", orderTTLHours: "72", contact: "" };

/** 32 random bytes as hex: what the webhook secret setting accepts. */
function newWebhookSecret(): string {
  const b = new Uint8Array(32);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
}

function todayVN(): string {
  return new Date(Date.now() + 7 * 3600_000).toISOString().slice(0, 10);
}
function dateOf(v: string | null | undefined): string {
  if (!v) return "";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return "";
  return new Date(d.getTime() + 7 * 3600_000).toISOString().slice(0, 10);
}
function fmtDateTime(v: string | null | undefined): string {
  if (!v) return "";
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? "" : formatDateTime(d);
}

type PromoForm = { code: string; name: string; discount_type: "percent" | "amount"; discount_value: number; plan_ids: string[]; max_uses: number; starts_at: string; ends_at: string; is_active: boolean; note: string };
const emptyPromo: PromoForm = { code: "", name: "", discount_type: "percent", discount_value: 10, plan_ids: [], max_uses: 0, starts_at: todayVN(), ends_at: "", is_active: true, note: "" };

export default function AdminBilling() {
  const { t } = useT();
  const [allowed, setAllowed] = useState<boolean | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [bank, setBank] = useState<BankForm>(emptyBank);
  const [bankSaved, setBankSaved] = useState(false);
  // masked current secret ("" = webhook off); the full one only right after
  // it was generated, until the page is left
  const [secretMasked, setSecretMasked] = useState("");
  const [secretShown, setSecretShown] = useState("");
  const [secretCopied, setSecretCopied] = useState(false);
  const [bankOpen, setBankOpen] = useState(false);
  const [plans, setPlans] = useState<UsagePlan[]>([]);
  const [promos, setPromos] = useState<UsagePromoCode[]>([]);
  const [orders, setOrders] = useState<UsageOrder[]>([]);
  const [filter, setFilter] = useState<UsageOrderStatus | "">("");
  const [activeTab, setActiveTab] = useState<"orders" | "promos">("orders");
  const [promoOpen, setPromoOpen] = useState(false);
  const [promoEditing, setPromoEditing] = useState<UsagePromoCode | null>(null);
  const [promoForm, setPromoForm] = useState<PromoForm>(emptyPromo);

  const loadOrders = useCallback(async (f: UsageOrderStatus | "") => {
    setOrders(await listAllUsageOrders(f || undefined));
  }, []);

  const load = useCallback(async () => {
    try {
      const K = BILLING_SETTING_KEYS;
      const [bin, name, no, acct, ttl, contact, secret, pl, pr] = await Promise.all([
        getSystemSetting(K.bankBin), getSystemSetting(K.bankName), getSystemSetting(K.accountNo),
        getSystemSetting(K.accountName), getSystemSetting(K.orderTTLHours), getSystemSetting(K.contact),
        getSystemSetting(K.webhookSecret), listUsagePlans(true), listUsagePromos(true),
      ]);
      setSecretMasked(String(secret.value ?? ""));
      setBank({
        bankBin: String(bin.value ?? ""), bankName: String(name.value ?? ""), accountNo: String(no.value ?? ""),
        accountName: String(acct.value ?? ""), orderTTLHours: String(ttl.value ?? "72"), contact: String(contact.value ?? ""),
      });
      setPlans(pl);
      setPromos(pr);
      await loadOrders(filter);
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : t("badm.loadFailed"));
    }
  }, [filter, loadOrders, t]);

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
      setError(e instanceof Error ? e.message : t("badm.opFailed"));
      return false;
    } finally {
      setBusy(false);
    }
  };

  const saveBank = () =>
    run(async () => {
      const K = BILLING_SETTING_KEYS;
      await updateSystemSetting(K.bankBin, bank.bankBin.trim());
      await updateSystemSetting(K.bankName, bank.bankName.trim());
      await updateSystemSetting(K.accountNo, bank.accountNo.trim());
      await updateSystemSetting(K.accountName, bank.accountName.trim());
      await updateSystemSetting(K.orderTTLHours, Math.max(1, Number(bank.orderTTLHours) || 72));
      await updateSystemSetting(K.contact, bank.contact.trim());
      setBankSaved(true);
      setTimeout(() => setBankSaved(false), 2000);
    });

  // ---- payment webhook secret
  const generateSecret = () => {
    if (secretMasked && !window.confirm(t("badm.webhookRegenConfirm"))) return;
    void run(async () => {
      const secret = newWebhookSecret();
      const row = await updateSystemSetting(BILLING_SETTING_KEYS.webhookSecret, secret);
      setSecretMasked(String(row.value ?? ""));
      setSecretShown(secret);
      setSecretCopied(false);
    });
  };
  const disableWebhook = () => {
    if (!window.confirm(t("badm.webhookOffConfirm"))) return;
    void run(async () => {
      await updateSystemSetting(BILLING_SETTING_KEYS.webhookSecret, "");
      setSecretMasked("");
      setSecretShown("");
    });
  };
  const copySecret = async () => setSecretCopied(await copyToClipboard(secretShown));

  // ---- promos
  const openNewPromo = () => {
    setPromoEditing(null);
    setPromoForm({ ...emptyPromo, starts_at: todayVN() });
    setPromoOpen(true);
  };
  const openEditPromo = (p: UsagePromoCode) => {
    setPromoEditing(p);
    setPromoForm({
      code: p.code, name: p.name, discount_type: p.discount_type, discount_value: p.discount_value, plan_ids: p.plan_ids ?? [],
      max_uses: p.max_uses, starts_at: dateOf(p.starts_at) || todayVN(), ends_at: dateOf(p.ends_at), is_active: p.is_active, note: p.note,
    });
    setPromoOpen(true);
  };
  const submitPromo = async (e: React.FormEvent) => {
    e.preventDefault();
    const input: UsagePromoInput = {
      code: promoForm.code, name: promoForm.name, discount_type: promoForm.discount_type, discount_value: promoForm.discount_value,
      plan_ids: promoForm.plan_ids, max_uses: promoForm.max_uses,
      starts_at: `${promoForm.starts_at}T00:00:00+07:00`, ends_at: promoForm.ends_at ? `${promoForm.ends_at}T23:59:59+07:00` : null,
      is_active: promoForm.is_active, note: promoForm.note,
    };
    const ok = await run(async () => {
      if (promoEditing) await updateUsagePromo(promoEditing.id, input);
      else await createUsagePromo(input);
      setPromos(await listUsagePromos(true));
    });
    if (ok) setPromoOpen(false);
  };
  const removePromo = (p: UsagePromoCode) => {
    if (!window.confirm(t("badm.deletePromoConfirm", { code: p.code }))) return;
    void run(async () => {
      await deleteUsagePromo(p.id);
      setPromos(await listUsagePromos(true));
    });
  };

  // ---- orders
  const confirm = (o: UsageOrder) => {
    if (!window.confirm(t("badm.confirmConfirm", { code: o.order_code, total: formatPriceVND(o.total_vnd) }))) return;
    void run(async () => {
      await confirmUsageOrder(o.id);
      await loadOrders(filter);
    });
  };
  const cancel = (o: UsageOrder) => {
    if (!window.confirm(t("badm.cancelConfirm", { code: o.order_code }))) return;
    void run(async () => {
      await cancelUsageOrder(o.id);
      await loadOrders(filter);
    });
  };
  const changeFilter = (f: UsageOrderStatus | "") => {
    setFilter(f);
    void run(() => loadOrders(f));
  };

  if (allowed === null) return <div className="mx-auto w-full max-w-[1100px] px-5 py-12 text-muted">…</div>;
  if (!allowed) {
    return (
      <div className="mx-auto w-full max-w-[1100px] px-5 py-16 text-center">
        <p className="body-sm text-muted">{t("badm.denied")}</p>
      </div>
    );
  }

  const planName = (id: string) => plans.find((p) => p.id === id)?.name ?? id;
  const statusKey = (s: UsageOrderStatus): LocaleKey => `bill.status.${s}` as LocaleKey;

  return (
    <div className="mx-auto w-full max-w-[1100px] px-5 py-6">
      {error && <p className="caption text-error">{error}</p>}

      <div className="mb-6 flex flex-wrap items-center gap-3 border-b border-hairline" role="tablist" aria-label={t("badm.orders")}>
        <button
          type="button"
          role="tab"
          aria-selected={activeTab === "orders"}
          className={`border-b-2 px-1 pb-3 text-[14px] font-medium transition-colors ${activeTab === "orders" ? "border-ink text-ink" : "border-transparent text-muted hover:text-ink"}`}
          onClick={() => setActiveTab("orders")}
        >
          {t("badm.orders")}
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={activeTab === "promos"}
          className={`border-b-2 px-1 pb-3 text-[14px] font-medium transition-colors ${activeTab === "promos" ? "border-ink text-ink" : "border-transparent text-muted hover:text-ink"}`}
          onClick={() => setActiveTab("promos")}
        >
          {t("badm.promos")}
        </button>
        <button type="button" className="btn btn-outline btn-sm mb-2 ml-auto shrink-0" onClick={() => setBankOpen(true)}>
          <IconEdit className="h-3.5 w-3.5" /> {t("badm.bankTitle")}
        </button>
      </div>

      {/* Orders */}
      {activeTab === "orders" && <section>
        <div className="mb-3 flex flex-wrap items-center gap-3">
          <h3 className="text-[14px] font-medium text-ink">{t("badm.orders")}</h3>
          <select className="input h-9 w-44 py-0" value={filter} onChange={(e) => changeFilter(e.target.value as UsageOrderStatus | "")}>
            <option value="">{t("badm.filterAll")}</option>
            {(["pending", "paid", "cancelled", "expired"] as UsageOrderStatus[]).map((s) => (
              <option key={s} value={s}>{t(statusKey(s))}</option>
            ))}
          </select>
          <button type="button" className="btn btn-outline btn-sm" disabled={busy} onClick={() => void run(() => loadOrders(filter))} title={t("badm.refresh")}>
            <IconRefresh className="h-3.5 w-3.5" />
          </button>
        </div>
        {orders.length === 0 ? (
          <p className="caption text-muted-soft">{t("badm.noOrders")}</p>
        ) : (
          <div className="min-w-0 overflow-hidden rounded-lg border border-hairline">
            <table className="w-full table-fixed text-[13px]">
              <colgroup>
                <col className="w-[14%]" />
                <col className="w-[15%]" />
                <col className="w-[16%]" />
                <col className="w-[11%]" />
                <col className="w-[17%]" />
                <col className="w-[17%]" />
                <col className="w-[10%]" />
              </colgroup>
              <thead className="caption text-muted">
                <tr className="border-b border-hairline text-left">
                  <th className="px-3 py-2 font-normal">{t("badm.colCode")}</th>
                  <th className="px-3 py-2 font-normal">{t("badm.colBuyer")}</th>
                  <th className="px-3 py-2 font-normal">{t("badm.colPlan")}</th>
                  <th className="px-3 py-2 text-right font-normal">{t("badm.colTotal")}</th>
                  <th className="px-3 py-2 font-normal">{t("badm.colStatus")}</th>
                  <th className="px-3 py-2 font-normal">{t("badm.colCreated")}</th>
                  <th className="px-3 py-2" />
                </tr>
              </thead>
              <tbody className="divide-y divide-hairline">
                {orders.map((o) => (
                  <tr key={o.id}>
                    <td className="whitespace-nowrap px-2.5 py-2 font-mono text-[12px] text-ink">{o.order_code}</td>
                    <td className="break-words px-2.5 py-2 text-ink">
                      {o.scope_name} <span className="caption text-muted-soft">({o.scope_type === "tenant" ? t("usg.scopeTenant") : t("usg.scopeUser")})</span>
                    </td>
                    <td className="break-words px-2.5 py-2 text-ink">
                      {o.plan_name} · {o.kind === "addon" ? t("bill.addonOrder") : t("bill.monthsN", { n: o.months })}{o.mode === "seat" ? ` · ${t("bill.seatsN", { n: o.seats })}` : ""}
                      {(o.addons ?? []).length > 0 ? ` · ${(o.addons ?? []).map((a) => a.name || a.agent_id).join(", ")}` : ""}
                      {o.promo_code ? <span className="caption block text-muted">{o.promo_code} −{formatPriceVND(o.discount_vnd)}</span> : null}
                    </td>
                    <td className="px-2.5 py-2 text-right tabular-nums text-ink">{formatPriceVND(o.total_vnd)}</td>
                    <td className="px-2.5 py-2">
                      <span className={`badge-pill text-[11px] ${o.status === "paid" ? "text-emerald-700" : o.status === "pending" ? "text-amber-700" : "text-muted"}`}>{t(statusKey(o.status))}</span>
                      {o.status === "paid" && o.paid_at ? <span className="caption block text-muted-soft">{fmtDateTime(o.paid_at)}</span> : null}
                    </td>
                    <td className="caption whitespace-nowrap px-2.5 py-2 text-muted">{fmtDateTime(o.created_at)}</td>
                    <td className="px-2 py-2 text-right">
                      {o.status === "pending" && (
                        <div className="flex flex-col items-stretch gap-1.5">
                          <button type="button" className="btn btn-primary btn-sm w-full px-2" disabled={busy} onClick={() => confirm(o)}>{t("badm.confirm")}</button>
                          <button type="button" className="btn btn-outline btn-sm w-full px-2 text-rose-600" disabled={busy} onClick={() => cancel(o)}>{t("badm.cancelOrder")}</button>
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>}

      {/* Promo codes */}
      {activeTab === "promos" && <section>
        <div className="mb-3 flex items-center gap-3">
          <h3 className="text-[14px] font-medium text-ink">{t("badm.promos")}</h3>
          <button type="button" className="btn btn-outline btn-sm ml-auto" onClick={openNewPromo}>
            <IconPlus className="h-3.5 w-3.5" /> {t("badm.newPromo")}
          </button>
        </div>
        {promos.length === 0 ? (
          <p className="caption text-muted-soft">{t("badm.noPromos")}</p>
        ) : (
          <div className="divide-y divide-hairline rounded-lg border border-hairline px-3">
            {promos.map((p) => (
              <div key={p.id} className="flex flex-wrap items-center gap-3 py-2.5 text-[14px]">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="font-mono font-medium text-ink">{p.code}</span>
                    {p.name && <span className="truncate text-muted">{p.name}</span>}
                    {!p.is_active && <span className="badge-pill text-[11px]">{t("badm.inactive")}</span>}
                  </div>
                  <div className="caption text-muted">
                    {p.discount_type === "percent" ? `−${p.discount_value}%` : `−${formatPriceVND(p.discount_value)}`}
                    {" · "}{p.plan_ids.length === 0 ? t("badm.allPlans") : p.plan_ids.map(planName).join(", ")}
                    {" · "}{t("badm.used", { used: p.used_count, max: p.max_uses > 0 ? String(p.max_uses) : "∞" })}
                    {" · "}{dateOf(p.starts_at)}{p.ends_at ? ` → ${dateOf(p.ends_at)}` : ""}
                  </div>
                </div>
                <button type="button" className="btn btn-outline btn-sm" disabled={busy} onClick={() => openEditPromo(p)}>
                  <IconEdit className="h-3.5 w-3.5" /> {t("common.edit")}
                </button>
                <button type="button" className="btn btn-outline btn-sm text-rose-600" disabled={busy} onClick={() => removePromo(p)}>
                  {t("common.delete")}
                </button>
              </div>
            ))}
          </div>
        )}
      </section>}

      {/* Bank account modal */}
      <Modal open={bankOpen} title={t("badm.bankTitle")} onClose={() => setBankOpen(false)} width="w-[680px]">
        <p className="caption mb-4 text-muted">{t("badm.bankDesc")}</p>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <label className="min-w-0"><span className="caption mb-1 block text-muted">{t("badm.bankName")}</span><input className="input w-full" value={bank.bankName} onChange={(e) => setBank({ ...bank, bankName: e.target.value })} placeholder="Vietcombank" /></label>
          <label className="min-w-0"><span className="caption mb-1 block text-muted">{t("badm.bankBin")}</span><input className="input w-full" inputMode="numeric" value={bank.bankBin} onChange={(e) => setBank({ ...bank, bankBin: e.target.value.replace(/[^\d]/g, "") })} placeholder="970436" /></label>
          <label className="min-w-0"><span className="caption mb-1 block text-muted">{t("badm.accountNo")}</span><input className="input w-full" value={bank.accountNo} onChange={(e) => setBank({ ...bank, accountNo: e.target.value })} /></label>
          <label className="min-w-0"><span className="caption mb-1 block text-muted">{t("badm.accountName")}</span><input className="input w-full" value={bank.accountName} onChange={(e) => setBank({ ...bank, accountName: e.target.value })} /></label>
          <label className="min-w-0"><span className="caption mb-1 block text-muted">{t("badm.ttl")}</span><input className="input w-full" type="number" min={1} value={bank.orderTTLHours} onChange={(e) => setBank({ ...bank, orderTTLHours: e.target.value })} /></label>
          <label className="min-w-0"><span className="caption mb-1 block text-muted">{t("badm.contact")}</span><input className="input w-full" value={bank.contact} onChange={(e) => setBank({ ...bank, contact: e.target.value })} /></label>
        </div>
        <div className="mt-5 flex items-center justify-end gap-3">
          {bankSaved && <span className="caption text-emerald-700">{t("badm.saved")}</span>}
          <button
            type="button"
            className="btn btn-primary"
            disabled={busy}
            onClick={() => void saveBank().then((ok) => ok && setBankOpen(false))}
          >
            {t("common.save")}
          </button>
        </div>
      </Modal>

      {/* Payment webhook */}
      <section className="rounded-lg border border-hairline p-4">
        <h3 className="text-[14px] font-medium text-ink">{t("badm.webhookTitle")}</h3>
        <p className="caption mb-3 text-muted">{t("badm.webhookDesc")}</p>
        <div className="flex flex-wrap items-center gap-3">
          <span className="caption text-muted">{t("badm.webhookSecret")}</span>
          {secretMasked ? (
            <code className="rounded bg-surface-strong px-2 py-1 font-mono text-[12px] text-ink">{secretMasked}</code>
          ) : (
            <span className="badge-pill text-[11px]">{t("badm.webhookOff")}</span>
          )}
          <div className="ml-auto flex gap-2">
            {secretMasked && (
              <button type="button" className="btn btn-outline btn-sm text-rose-600" disabled={busy} onClick={disableWebhook}>{t("badm.webhookDisable")}</button>
            )}
            <button type="button" className="btn btn-primary btn-sm" disabled={busy} onClick={generateSecret}>{t("badm.webhookGenerate")}</button>
          </div>
        </div>
        {secretShown && (
          <div className="mt-3 rounded-md border border-amber-300 bg-amber-50 p-3 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/40 dark:text-amber-300">
            <p className="caption mb-2">{t("badm.webhookShownOnce")}</p>
            <div className="flex items-center gap-2">
              <code className="min-w-0 flex-1 break-all rounded bg-surface-strong px-2 py-1 font-mono text-[12px] text-ink">{secretShown}</code>
              <button type="button" className="btn btn-outline btn-sm" onClick={() => void copySecret()}>
                <IconCopy className="h-3.5 w-3.5" /> {secretCopied ? t("badm.webhookCopied") : t("badm.webhookCopy")}
              </button>
            </div>
          </div>
        )}
        <p className="caption mt-3 text-muted-soft">{t("badm.webhookHelp")}</p>
      </section>

      {/* Promo modal */}
      <Modal open={promoOpen} title={promoEditing ? t("badm.editPromo") : t("badm.newPromo")} onClose={() => setPromoOpen(false)}>
        <form className="space-y-4" onSubmit={submitPromo}>
          <div className="grid gap-3 sm:grid-cols-2">
            <label className="block">
              <span className="caption mb-1 block text-muted">{t("badm.code")}</span>
              <input className="input uppercase" required value={promoForm.code} onChange={(e) => setPromoForm({ ...promoForm, code: e.target.value.toUpperCase() })} />
            </label>
            <label className="block">
              <span className="caption mb-1 block text-muted">{t("badm.name")}</span>
              <input className="input" value={promoForm.name} onChange={(e) => setPromoForm({ ...promoForm, name: e.target.value })} />
            </label>
          </div>
          <div className="grid gap-3 sm:grid-cols-3">
            <label className="block">
              <span className="caption mb-1 block text-muted">{t("badm.type")}</span>
              <select className="input" value={promoForm.discount_type} onChange={(e) => setPromoForm({ ...promoForm, discount_type: e.target.value as "percent" | "amount" })}>
                <option value="percent">{t("badm.percent")}</option>
                <option value="amount">{t("badm.amount")}</option>
              </select>
            </label>
            <label className="block">
              <span className="caption mb-1 block text-muted">{promoForm.discount_type === "percent" ? "%" : "VNĐ"}</span>
              <input className="input" type="number" min={1} max={promoForm.discount_type === "percent" ? 100 : undefined} value={promoForm.discount_value} onChange={(e) => setPromoForm({ ...promoForm, discount_value: Math.max(0, Number(e.target.value) || 0) })} />
            </label>
            <label className="block">
              <span className="caption mb-1 block text-muted">{t("badm.maxUses")}</span>
              <input className="input" type="number" min={0} value={promoForm.max_uses} onChange={(e) => setPromoForm({ ...promoForm, max_uses: Math.max(0, Number(e.target.value) || 0) })} />
            </label>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <label className="block">
              <span className="caption mb-1 block text-muted">{t("badm.startsAt")}</span>
              <input className="input" type="date" value={promoForm.starts_at} onChange={(e) => setPromoForm({ ...promoForm, starts_at: e.target.value })} />
            </label>
            <label className="block">
              <span className="caption mb-1 block text-muted">{t("badm.endsAt")}</span>
              <input className="input" type="date" value={promoForm.ends_at} onChange={(e) => setPromoForm({ ...promoForm, ends_at: e.target.value })} />
            </label>
          </div>
          <div>
            <span className="caption mb-1 block text-muted">{t("badm.plans")}</span>
            <div className="flex flex-wrap gap-x-4 gap-y-1">
              {plans.map((p) => (
                <label key={p.id} className="flex items-center gap-1.5 text-[13.5px]">
                  <input
                    type="checkbox"
                    checked={promoForm.plan_ids.includes(p.id)}
                    onChange={(e) => setPromoForm({ ...promoForm, plan_ids: e.target.checked ? [...promoForm.plan_ids, p.id] : promoForm.plan_ids.filter((x) => x !== p.id) })}
                  />
                  {p.name}
                </label>
              ))}
            </div>
            <span className="caption mt-1 block text-muted-soft">{t("badm.plansHint")}</span>
          </div>
          <label className="block">
            <span className="caption mb-1 block text-muted">{t("badm.note")}</span>
            <input className="input" value={promoForm.note} onChange={(e) => setPromoForm({ ...promoForm, note: e.target.value })} />
          </label>
          <label className="flex items-center gap-2">
            <input type="checkbox" checked={promoForm.is_active} onChange={(e) => setPromoForm({ ...promoForm, is_active: e.target.checked })} />
            <span className="caption text-muted">{t("badm.active")}</span>
          </label>
          <div className="flex justify-end">
            <button type="submit" className="btn btn-primary" disabled={busy || !promoForm.code.trim() || promoForm.discount_value <= 0}>{t("common.save")}</button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
