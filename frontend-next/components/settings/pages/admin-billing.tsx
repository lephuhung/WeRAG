"use client";

/* SuperAdmin billing: the bank account that receives transfers (VietQR),
 * promo codes, the orders to confirm once the money arrived, and the
 * secret of the payment checker that confirms them automatically. */

import { useCallback, useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { IconCopy, IconEdit, IconPlus, IconRefresh } from "@/components/icons";
import { SystemSection } from "@/components/settings/system-section";
import { Notice } from "@/components/settings/notice";
import { DataTable, ROW_HOVER } from "@/components/settings/data-table";
import { TabStrip } from "@/components/settings/tab-strip";
import { Pill, PillGroup } from "@/components/settings/pill";
import { FieldLabel } from "@/components/settings/field";
import { useConfirm } from "@/components/confirm-dialog";
import { useToast } from "@/components/toast";
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
import { WebhookGuide } from "./admin-billing-webhook-guide";

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
  const [loading, setLoading] = useState(true);
  const confirm = useConfirm();
  const toast = useToast();
  const [bank, setBank] = useState<BankForm>(emptyBank);
  /* The values the bank form was loaded with: the save button stays
   * disabled until the form differs from them. */
  const [bankBase, setBankBase] = useState<BankForm>(emptyBank);
  // masked current secret ("" = webhook off); the full one only right after
  // it was generated, until the page is left
  const [secretMasked, setSecretMasked] = useState("");
  const [secretShown, setSecretShown] = useState("");
  const [secretCopied, setSecretCopied] = useState(false);
  const [bankOpen, setBankOpen] = useState(false);
  const [webhookOpen, setWebhookOpen] = useState(false);
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
      const bankForm: BankForm = {
        bankBin: String(bin.value ?? ""), bankName: String(name.value ?? ""), accountNo: String(no.value ?? ""),
        accountName: String(acct.value ?? ""), orderTTLHours: String(ttl.value ?? "72"), contact: String(contact.value ?? ""),
      };
      setBank(bankForm);
      setBankBase(bankForm);
      setPlans(pl);
      setPromos(pr);
      await loadOrders(filter);
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : t("badm.loadFailed"));
    } finally {
      setLoading(false);
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

  const bankDirty = JSON.stringify(bank) !== JSON.stringify(bankBase);

  const saveBank = async (): Promise<boolean> => {
    const ok = await run(async () => {
      const K = BILLING_SETTING_KEYS;
      await updateSystemSetting(K.bankBin, bank.bankBin.trim());
      await updateSystemSetting(K.bankName, bank.bankName.trim());
      await updateSystemSetting(K.accountNo, bank.accountNo.trim());
      await updateSystemSetting(K.accountName, bank.accountName.trim());
      await updateSystemSetting(K.orderTTLHours, Math.max(1, Number(bank.orderTTLHours) || 72));
      await updateSystemSetting(K.contact, bank.contact.trim());
    });
    if (!ok) return false;
    setBankBase({ ...bank, orderTTLHours: String(Math.max(1, Number(bank.orderTTLHours) || 72)) });
    toast.success(t("common.saved"));
    return true;
  };

  // ---- payment webhook secret
  const generateSecret = async () => {
    if (secretMasked) {
      const yes = await confirm({
        title: t("badm.webhookGenerate"),
        message: t("badm.webhookRegenConfirm"),
        danger: true,
      });
      if (!yes) return;
    }
    const ok = await run(async () => {
      const secret = newWebhookSecret();
      const row = await updateSystemSetting(BILLING_SETTING_KEYS.webhookSecret, secret);
      setSecretMasked(String(row.value ?? ""));
      setSecretShown(secret);
      setSecretCopied(false);
    });
    if (ok) toast.success(t("common.saved"));
  };
  const disableWebhook = async () => {
    const yes = await confirm({
      title: t("badm.webhookDisable"),
      message: t("badm.webhookOffConfirm"),
      danger: true,
    });
    if (!yes) return;
    const ok = await run(async () => {
      await updateSystemSetting(BILLING_SETTING_KEYS.webhookSecret, "");
      setSecretMasked("");
      setSecretShown("");
    });
    if (ok) toast.success(t("common.saved"));
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
    if (ok) {
      toast.success(t("common.saved"));
      setPromoOpen(false);
    }
  };
  const removePromo = async (p: UsagePromoCode) => {
    const yes = await confirm({
      title: t("common.delete"),
      message: t("badm.deletePromoConfirm", { code: p.code }),
      danger: true,
    });
    if (!yes) return;
    const ok = await run(async () => {
      await deleteUsagePromo(p.id);
      setPromos(await listUsagePromos(true));
    });
    if (ok) toast.success(t("common.saved"));
  };

  // ---- orders
  const confirmOrder = async (o: UsageOrder) => {
    const yes = await confirm({
      title: t("badm.confirm"),
      message: t("badm.confirmConfirm", { code: o.order_code, total: formatPriceVND(o.total_vnd) }),
    });
    if (!yes) return;
    const ok = await run(async () => {
      await confirmUsageOrder(o.id);
      await loadOrders(filter);
    });
    if (ok) toast.success(t("common.saved"));
  };
  const cancelOrder = async (o: UsageOrder) => {
    const yes = await confirm({
      title: t("badm.cancelOrder"),
      message: t("badm.cancelConfirm", { code: o.order_code }),
      danger: true,
    });
    if (!yes) return;
    const ok = await run(async () => {
      await cancelUsageOrder(o.id);
      await loadOrders(filter);
    });
    if (ok) toast.success(t("common.saved"));
  };
  const changeFilter = (f: UsageOrderStatus | "") => {
    setFilter(f);
    void run(() => loadOrders(f));
  };

  const planName = (id: string) => plans.find((p) => p.id === id)?.name ?? id;
  const statusKey = (s: UsageOrderStatus): LocaleKey => `bill.status.${s}` as LocaleKey;

  return (
    <SystemSection
      allowed={allowed}
      title={t("systemNav.billing")}
      deniedMessage={t("badm.denied")}
      loading={loading}
      loadingLabel={t("common.loading")}
    >
      <div className="py-6">
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

      <div className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <TabStrip<"orders" | "promos">
          tabs={[
            { id: "orders", label: t("badm.orders") },
            { id: "promos", label: t("badm.promos") },
          ]}
          value={activeTab}
          onChange={setActiveTab}
          ariaLabel={t("badm.orders")}
          className="min-w-0 flex-1"
        />
        <div className="flex flex-wrap gap-2">
        <button type="button" className="btn btn-outline btn-sm shrink-0" onClick={() => setWebhookOpen(true)}>
          {t("badm.webhookTitle")}
        </button>
        <button type="button" className="btn btn-outline btn-sm shrink-0" onClick={() => setBankOpen(true)}>
          <IconEdit className="h-3.5 w-3.5" /> {t("badm.bankTitle")}
        </button>
        </div>
      </div>

      {/* Orders */}
      {activeTab === "orders" && (
      <section id="tabpanel-orders" role="tabpanel" aria-labelledby="tab-orders">
        <div className="mb-3 flex flex-wrap items-center gap-3">
          <h3 className="text-[14px] font-medium text-ink">{t("badm.orders")}</h3>
          <PillGroup ariaLabel={t("common.filters")}>
            <Pill active={filter === ""} onClick={() => changeFilter("")}>
              {t("badm.filterAll")}
            </Pill>
            {(["pending", "paid", "cancelled", "expired"] as UsageOrderStatus[]).map((s) => (
              <Pill key={s} active={filter === s} onClick={() => changeFilter(s)}>
                {t(statusKey(s))}
              </Pill>
            ))}
          </PillGroup>
          <button
            type="button"
            className="btn btn-outline btn-sm h-9 w-9 p-0"
            disabled={busy}
            onClick={() => void run(() => loadOrders(filter))}
            title={t("badm.refresh")}
            aria-label={t("badm.refresh")}
          >
            <IconRefresh className="h-3.5 w-3.5" />
          </button>
        </div>
        <DataTable<UsageOrder>
          columns={[
            { key: "code", header: t("badm.colCode"), className: "w-[14%]" },
            { key: "buyer", header: t("badm.colBuyer"), className: "w-[15%]" },
            { key: "plan", header: t("badm.colPlan"), className: "w-[16%]" },
            { key: "total", header: t("badm.colTotal"), className: "w-[11%]", align: "right" },
            { key: "status", header: t("badm.colStatus"), className: "w-[17%]" },
            { key: "created", header: t("badm.colCreated"), className: "w-[17%]" },
            { key: "actions", header: "", className: "w-[10%]", align: "right" },
          ]}
          rows={orders}
          rowKey={(o) => o.id}
          emptyLabel={t("badm.noOrders")}
          renderRow={(o) => (
            <>
              <td className="whitespace-nowrap px-5 py-3.5 align-middle font-mono text-[12px] text-ink">{o.order_code}</td>
              <td className="break-words px-5 py-3.5 align-middle text-ink">
                {o.scope_name} <span className="caption text-muted-soft">({o.scope_type === "tenant" ? t("usg.scopeTenant") : t("usg.scopeUser")})</span>
              </td>
              <td className="break-words px-5 py-3.5 align-middle text-ink">
                {o.plan_name} · {o.kind === "addon" ? t("bill.addonOrder") : t("bill.monthsN", { n: o.months })}{o.mode === "seat" ? ` · ${t("bill.seatsN", { n: o.seats })}` : ""}
                {(o.addons ?? []).length > 0 ? ` · ${(o.addons ?? []).map((a) => a.name || a.agent_id).join(", ")}` : ""}
                {o.promo_code ? <span className="caption block text-muted">{o.promo_code} −{formatPriceVND(o.discount_vnd)}</span> : null}
              </td>
              <td className="px-5 py-3.5 text-right align-middle tabular-nums text-ink">{formatPriceVND(o.total_vnd)}</td>
              <td className="px-5 py-3.5 align-middle">
                <span className={`badge-pill text-[11px] ${o.status === "paid" ? "text-success" : o.status === "pending" ? "text-warning" : "text-muted"}`}>{t(statusKey(o.status))}</span>
                {o.status === "paid" && o.paid_at ? <span className="caption block text-muted-soft">{fmtDateTime(o.paid_at)}</span> : null}
              </td>
              <td className="caption whitespace-nowrap px-5 py-3.5 align-middle text-muted">{fmtDateTime(o.created_at)}</td>
              <td className="px-5 py-3.5 text-right align-middle">
                {o.status === "pending" && (
                  <div className="flex flex-col items-stretch gap-1.5">
                    <button type="button" className="btn btn-primary btn-sm w-full px-2" disabled={busy} onClick={() => void confirmOrder(o)}>{t("badm.confirm")}</button>
                    <button type="button" className="btn btn-danger btn-sm w-full px-2" disabled={busy} onClick={() => void cancelOrder(o)}>{t("badm.cancelOrder")}</button>
                  </div>
                )}
              </td>
            </>
          )}
        />
      </section>
      )}

      {/* Promo codes */}
      {activeTab === "promos" && (
      <section id="tabpanel-promos" role="tabpanel" aria-labelledby="tab-promos">
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
              <div key={p.id} className={`flex flex-wrap items-center gap-3 py-2.5 text-[14px] ${ROW_HOVER}`}>
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
                <button type="button" className="btn btn-danger btn-sm" disabled={busy} onClick={() => void removePromo(p)}>
                  {t("common.delete")}
                </button>
              </div>
            ))}
          </div>
        )}
      </section>
      )}

      {/* Bank account modal */}
      <Modal open={bankOpen} title={t("badm.bankTitle")} onClose={() => setBankOpen(false)} width="w-[680px]">
        <p className="caption mb-4 text-muted">{t("badm.bankDesc")}</p>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div className="min-w-0">
            <FieldLabel htmlFor="bank-name">{t("badm.bankName")}</FieldLabel>
            <input id="bank-name" className="input w-full" value={bank.bankName} onChange={(e) => setBank({ ...bank, bankName: e.target.value })} placeholder="Vietcombank" />
          </div>
          <div className="min-w-0">
            <FieldLabel htmlFor="bank-bin">{t("badm.bankBin")}</FieldLabel>
            <input id="bank-bin" className="input w-full" inputMode="numeric" value={bank.bankBin} onChange={(e) => setBank({ ...bank, bankBin: e.target.value.replace(/[^\d]/g, "") })} placeholder="970436" />
          </div>
          <div className="min-w-0">
            <FieldLabel htmlFor="bank-account-no">{t("badm.accountNo")}</FieldLabel>
            <input id="bank-account-no" className="input w-full" value={bank.accountNo} onChange={(e) => setBank({ ...bank, accountNo: e.target.value })} />
          </div>
          <div className="min-w-0">
            <FieldLabel htmlFor="bank-account-name">{t("badm.accountName")}</FieldLabel>
            <input id="bank-account-name" className="input w-full" value={bank.accountName} onChange={(e) => setBank({ ...bank, accountName: e.target.value })} />
          </div>
          <div className="min-w-0">
            <FieldLabel htmlFor="bank-ttl">{t("badm.ttl")}</FieldLabel>
            <input id="bank-ttl" className="input w-full" type="number" min={1} value={bank.orderTTLHours} onChange={(e) => setBank({ ...bank, orderTTLHours: e.target.value })} />
          </div>
          <div className="min-w-0">
            <FieldLabel htmlFor="bank-contact">{t("badm.contact")}</FieldLabel>
            <input id="bank-contact" className="input w-full" value={bank.contact} onChange={(e) => setBank({ ...bank, contact: e.target.value })} />
          </div>
        </div>
        <div className="mt-5 flex items-center justify-end gap-3">
          <button
            type="button"
            className="btn btn-primary"
            disabled={busy || !bankDirty}
            onClick={() => void saveBank().then((ok) => { if (ok) setBankOpen(false); })}
          >
            {t("common.save")}
          </button>
        </div>
      </Modal>

      {/* Payment webhook */}
      <Modal open={webhookOpen} title={t("badm.webhookTitle")} onClose={() => setWebhookOpen(false)} width="w-[760px]">
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
              <button type="button" className="btn btn-danger btn-sm" disabled={busy} onClick={() => void disableWebhook()}>{t("badm.webhookDisable")}</button>
            )}
            <button type="button" className="btn btn-primary btn-sm" disabled={busy} onClick={() => void generateSecret()}>{t("badm.webhookGenerate")}</button>
          </div>
        </div>
        {secretShown && (
          <Notice tone="warning" className="mt-3">
            <span className="block w-full">
              <span className="caption mb-2 block">{t("badm.webhookShownOnce")}</span>
              <span className="flex items-center gap-2">
                <code className="min-w-0 flex-1 break-all rounded bg-surface-strong px-2 py-1 font-mono text-[12px] text-ink">{secretShown}</code>
                <button type="button" className="btn btn-outline btn-sm" onClick={() => void copySecret()}>
                  <IconCopy className="h-3.5 w-3.5" /> {secretCopied ? t("badm.webhookCopied") : t("badm.webhookCopy")}
                </button>
              </span>
            </span>
          </Notice>
        )}
        <p className="caption mt-3 text-muted-soft">{t("badm.webhookHelp")}</p>
        <WebhookGuide key={secretShown} enabled={!!secretMasked} secretShown={secretShown} />
      </Modal>

      {/* Promo modal */}
      <Modal open={promoOpen} title={promoEditing ? t("badm.editPromo") : t("badm.newPromo")} onClose={() => setPromoOpen(false)}>
        <form className="space-y-4" onSubmit={submitPromo}>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="block">
              <FieldLabel htmlFor="promo-code" required>{t("badm.code")}</FieldLabel>
              <input id="promo-code" className="input uppercase" required aria-required="true" value={promoForm.code} onChange={(e) => setPromoForm({ ...promoForm, code: e.target.value.toUpperCase() })} />
            </div>
            <div className="block">
              <FieldLabel htmlFor="promo-name">{t("badm.name")}</FieldLabel>
              <input id="promo-name" className="input" value={promoForm.name} onChange={(e) => setPromoForm({ ...promoForm, name: e.target.value })} />
            </div>
          </div>
          <div className="grid gap-3 sm:grid-cols-3">
            <div className="block">
              <FieldLabel htmlFor="promo-type">{t("badm.type")}</FieldLabel>
              <select id="promo-type" className="input" value={promoForm.discount_type} onChange={(e) => setPromoForm({ ...promoForm, discount_type: e.target.value as "percent" | "amount" })}>
                <option value="percent">{t("badm.percent")}</option>
                <option value="amount">{t("badm.amount")}</option>
              </select>
            </div>
            <div className="block">
              <FieldLabel htmlFor="promo-value">{promoForm.discount_type === "percent" ? "%" : "VNĐ"}</FieldLabel>
              <input id="promo-value" className="input" type="number" min={1} max={promoForm.discount_type === "percent" ? 100 : undefined} value={promoForm.discount_value} onChange={(e) => setPromoForm({ ...promoForm, discount_value: Math.max(0, Number(e.target.value) || 0) })} />
            </div>
            <div className="block">
              <FieldLabel htmlFor="promo-max-uses">{t("badm.maxUses")}</FieldLabel>
              <input id="promo-max-uses" className="input" type="number" min={0} value={promoForm.max_uses} onChange={(e) => setPromoForm({ ...promoForm, max_uses: Math.max(0, Number(e.target.value) || 0) })} />
            </div>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="block">
              <FieldLabel htmlFor="promo-start">{t("badm.startsAt")}</FieldLabel>
              <input id="promo-start" className="input" type="date" value={promoForm.starts_at} onChange={(e) => setPromoForm({ ...promoForm, starts_at: e.target.value })} />
            </div>
            <div className="block">
              <FieldLabel htmlFor="promo-end">{t("badm.endsAt")}</FieldLabel>
              <input id="promo-end" className="input" type="date" value={promoForm.ends_at} onChange={(e) => setPromoForm({ ...promoForm, ends_at: e.target.value })} />
            </div>
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
          <div className="block">
            <FieldLabel htmlFor="promo-note">{t("badm.note")}</FieldLabel>
            <input id="promo-note" className="input" value={promoForm.note} onChange={(e) => setPromoForm({ ...promoForm, note: e.target.value })} />
          </div>
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
    </SystemSection>
  );
}
