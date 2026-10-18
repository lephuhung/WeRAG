"use client";

/* Buy or renew a usage package: pick a plan (for the workspace when you
 * administer it, or for yourself), a duration and seats, apply a promo
 * code, place the order and pay by bank transfer (VietQR, transfer content
 * = order code). SuperAdmin confirms the payment; the page polls until
 * then. Past orders are listed below. */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useAuth } from "@/lib/auth";
import { ApiError } from "@/lib/api-client";
import { useT, type LocaleKey } from "@/lib/i18n";
import { IconCheck, IconCopy, IconRefresh } from "@/components/icons";
import {
  cancelMyUsageOrder,
  createUsageOrder,
  formatPriceVND,
  getBillingInfo,
  getUsageOrder,
  listMyUsageOrders,
  listPurchasablePlans,
  promoErrorKey,
  quoteUsageOrder,
  usageLimitLabel,
  type UsageBillingInfo,
  type UsageOrder,
  type UsagePlan,
  type UsageQuote,
  type UsageScopeKind,
} from "@/lib/api/usage";
import { formatDateTime } from "@/lib/datetime";

const MONTH_CHOICES = [1, 3, 6, 12];

function fmtDate(v: string | null | undefined): string {
  if (!v) return "";
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? "" : formatDateTime(d);
}

function statusKey(s: UsageOrder["status"]): LocaleKey {
  return `bill.status.${s}` as LocaleKey;
}

function CopyButton({ value, label, copied }: { value: string; label: string; copied: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      className="btn btn-outline btn-sm"
      title={label}
      onClick={() => {
        void navigator.clipboard?.writeText(value).then(() => {
          setDone(true);
          setTimeout(() => setDone(false), 1500);
        });
      }}
    >
      {done ? <IconCheck className="h-3.5 w-3.5" /> : <IconCopy className="h-3.5 w-3.5" />} {done ? copied : label}
    </button>
  );
}

/** The current seat count when the server refused a renewal with fewer
 * seats (reason seats_below_current), else null. */
function seatsBelowCurrent(e: unknown): number | null {
  if (!(e instanceof ApiError)) return null;
  const details = (e.payload as { error?: { details?: { reason?: string; current_seats?: number } } } | undefined)?.error?.details;
  return details?.reason === "seats_below_current" && typeof details.current_seats === "number" ? details.current_seats : null;
}

export function BillingPage() {
  const { t } = useT();
  const auth = useAuth();
  const tenantId = Number(auth.selectedTenantId ?? auth.tenant?.id ?? 0);
  const role = auth.memberships.find((m) => String(m.tenant_id) === String(tenantId))?.role ?? "";
  const isAdmin = role === "admin" || role === "owner" || auth.user?.is_system_admin === true;

  const [info, setInfo] = useState<UsageBillingInfo | null>(null);
  const [scope, setScope] = useState<UsageScopeKind>(isAdmin ? "tenant" : "user");
  const [plans, setPlans] = useState<UsagePlan[]>([]);
  const [planId, setPlanId] = useState("");
  const [months, setMonths] = useState(1);
  const [seats, setSeats] = useState(1);
  const [promoText, setPromoText] = useState("");
  const [promo, setPromo] = useState("");
  const [quote, setQuote] = useState<UsageQuote | null>(null);
  const [quoteError, setQuoteError] = useState("");
  const [orders, setOrders] = useState<UsageOrder[]>([]);
  const [order, setOrder] = useState<UsageOrder | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const plan = useMemo(() => plans.find((p) => p.id === planId) ?? null, [plans, planId]);
  const limitNames = { questions: t("usg.metricQuestions"), tokens: t("usg.metricTokens"), day: t("usg.periodDay"), month: t("usg.periodMonth") };
  const seatMode = scope === "tenant" && plan?.mode === "seat";

  const load = useCallback(async () => {
    try {
      const [i, pl, os] = await Promise.all([getBillingInfo(), listPurchasablePlans(scope), listMyUsageOrders()]);
      setInfo(i);
      setPlans(pl);
      setOrders(os);
      setPlanId((cur) => (pl.some((p) => p.id === cur) ? cur : (pl[0]?.id ?? "")));
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : t("bill.loadFailed"));
    }
  }, [scope, t]);

  useEffect(() => {
    void load();
  }, [load]);

  // Re-price whenever the choice changes.
  useEffect(() => {
    if (!planId) {
      setQuote(null);
      return;
    }
    let alive = true;
    quoteUsageOrder({ scope_type: scope, plan_id: planId, months, seats: seatMode ? seats : 0, promo_code: promo })
      .then((q) => {
        if (!alive) return;
        setQuote(q);
        setQuoteError("");
      })
      .catch((e) => {
        if (!alive) return;
        setQuote(null);
        const current = seatsBelowCurrent(e);
        setQuoteError(current !== null ? t("bill.seatsBelowCurrent", { n: current }) : e instanceof Error ? e.message : t("bill.loadFailed"));
      });
    return () => {
      alive = false;
    };
  }, [scope, planId, months, seats, seatMode, promo, t]);

  // Poll a pending order until SuperAdmin confirms it.
  const pollRef = useRef<number | null>(null);
  useEffect(() => {
    if (pollRef.current) window.clearInterval(pollRef.current);
    pollRef.current = null;
    if (!order || order.status !== "pending") return;
    pollRef.current = window.setInterval(() => {
      getUsageOrder(order.order_code)
        .then((o) => {
          setOrder(o);
          if (o.status !== "pending") void load();
        })
        .catch(() => undefined);
    }, 10_000);
    return () => {
      if (pollRef.current) window.clearInterval(pollRef.current);
    };
  }, [order, load]);

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError("");
    try {
      await fn();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("bill.opFailed"));
    } finally {
      setBusy(false);
    }
  };

  const placeOrder = () =>
    run(async () => {
      const o = await createUsageOrder({ scope_type: scope, plan_id: planId, months, seats: seatMode ? seats : 0, promo_code: promo });
      setOrder(o);
      await load();
    });

  const cancelOrder = (o: UsageOrder) => {
    if (!window.confirm(t("bill.cancelConfirm", { code: o.order_code }))) return;
    void run(async () => {
      const u = await cancelMyUsageOrder(o.order_code);
      setOrder(u);
      await load();
    });
  };

  const canBuy = !!info?.enabled && !!quote && !busy && (!seatMode || seats > 0);

  // ---- payment view ----------------------------------------------------------
  if (order) {
    const pending = order.status === "pending";
    return (
      <div className="mx-auto w-full max-w-[760px] px-5 py-6">
        <button type="button" className="btn btn-outline btn-sm mb-4" onClick={() => setOrder(null)}>
          ← {t("bill.back")}
        </button>
        <div className="rounded-lg border border-hairline">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-hairline px-4 py-3">
            <div>
              <div className="text-[14px] font-medium text-ink">
                {t("bill.order")} <span className="font-mono">{order.order_code}</span>
              </div>
              <div className="caption text-muted">
                {order.plan_name} · {t("bill.monthsN", { n: order.months })}
                {order.mode === "seat" ? ` · ${t("bill.seatsN", { n: order.seats })}` : ""} · {order.scope_name}
              </div>
            </div>
            <span className={`badge-pill text-[11px] ${order.status === "paid" ? "text-emerald-700" : pending ? "text-amber-700" : "text-muted"}`}>{t(statusKey(order.status))}</span>
          </div>
          <div className="space-y-1 px-4 py-3 text-[14px]">
            <div className="flex justify-between"><span className="text-muted">{t("bill.subtotal")}</span><span className="tabular-nums">{formatPriceVND(order.subtotal_vnd)}</span></div>
            {order.discount_vnd > 0 && (
              <div className="flex justify-between"><span className="text-muted">{t("bill.discount")} ({order.promo_code})</span><span className="tabular-nums">−{formatPriceVND(order.discount_vnd)}</span></div>
            )}
            <div className="flex justify-between font-medium text-ink"><span>{t("bill.total")}</span><span className="tabular-nums">{formatPriceVND(order.total_vnd)}</span></div>
          </div>
          {pending && (
            <div className="border-t border-hairline px-4 py-3">
              <div className="text-[14px] font-medium text-ink">{t("bill.payTitle")}</div>
              <div className="mt-3 flex flex-wrap gap-5">
                {order.qr_url && (
                  <div className="shrink-0 text-center">
                    {/* eslint-disable-next-line @next/next/no-img-element */}
                    <img src={order.qr_url} alt="VietQR" width={220} height={260} className="rounded-md border border-hairline" />
                    <div className="caption mt-1 text-muted">{t("bill.qrHint")}</div>
                  </div>
                )}
                <div className="min-w-[260px] flex-1 space-y-2 text-[14px]">
                  {info?.bank_name && <div><span className="text-muted">{t("bill.bank")}: </span><span className="text-ink">{info.bank_name}</span></div>}
                  {info?.bank_account_no && (
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-muted">{t("bill.accountNo")}: </span>
                      <span className="font-mono text-ink">{info.bank_account_no}</span>
                      <CopyButton value={info.bank_account_no} label={t("bill.copy")} copied={t("bill.copied")} />
                    </div>
                  )}
                  {info?.bank_account_name && <div><span className="text-muted">{t("bill.accountName")}: </span><span className="text-ink">{info.bank_account_name}</span></div>}
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-muted">{t("bill.amount")}: </span>
                    <span className="font-medium text-ink tabular-nums">{formatPriceVND(order.total_vnd)}</span>
                    <CopyButton value={String(order.total_vnd)} label={t("bill.copy")} copied={t("bill.copied")} />
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-muted">{t("bill.content")}: </span>
                    <span className="font-mono font-medium text-ink">{order.transfer_content}</span>
                    <CopyButton value={order.transfer_content} label={t("bill.copy")} copied={t("bill.copied")} />
                  </div>
                  <p className="caption text-muted-soft">{t("bill.contentHint")}</p>
                  {order.expires_at && <p className="caption text-amber-700">{t("bill.expiresAt", { date: fmtDate(order.expires_at) })}</p>}
                  {info?.contact && <p className="caption text-muted">{info.contact}</p>}
                  <p className="caption flex items-center gap-1 text-muted">
                    <IconRefresh className="h-3.5 w-3.5 animate-spin" /> {t("bill.waiting")}
                  </p>
                </div>
              </div>
              <div className="mt-3 flex justify-end">
                <button type="button" className="btn btn-outline btn-sm text-rose-600" disabled={busy} onClick={() => cancelOrder(order)}>
                  {t("bill.cancel")}
                </button>
              </div>
            </div>
          )}
          {order.status === "paid" && (
            <div className="border-t border-hairline px-4 py-3 text-[14px] text-emerald-700">
              {t("bill.paid")} {order.paid_at ? fmtDate(order.paid_at) : ""}
            </div>
          )}
          {(order.status === "cancelled" || order.status === "expired") && (
            <div className="border-t border-hairline px-4 py-3 text-[14px] text-muted">{t(order.status === "expired" ? "bill.expired" : "bill.cancelled")}</div>
          )}
        </div>
        {error && <p className="caption mt-3 text-error">{error}</p>}
      </div>
    );
  }

  // ---- shop view ---------------------------------------------------------------
  return (
    <div className="mx-auto w-full max-w-[860px] space-y-6 px-5 py-6">
      <div>
        <h2 className="text-[16px] font-medium text-ink">{t("bill.title")}</h2>
        <p className="caption text-muted">{t("bill.intro")}</p>
      </div>
      {error && <p className="caption text-error">{error}</p>}
      {info && !info.enabled && (
        <div className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-[13px] text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/40 dark:text-amber-300">
          {t("bill.disabled")} {info.contact}
        </div>
      )}

      {isAdmin && (
        <div className="flex gap-2">
          {(["tenant", "user"] as UsageScopeKind[]).map((s) => (
            <button key={s} type="button" className={`btn btn-sm ${scope === s ? "btn-primary" : "btn-outline"}`} onClick={() => { setScope(s); setPromo(""); setPromoText(""); }}>
              {s === "tenant" ? t("bill.scopeTenant") : t("bill.scopeUser")}
            </button>
          ))}
        </div>
      )}

      {plans.length === 0 ? (
        <p className="caption text-muted-soft">{t("bill.noPlans")}</p>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {plans.map((p) => (
            <button
              key={p.id}
              type="button"
              onClick={() => setPlanId(p.id)}
              className={`rounded-lg border p-4 text-left transition ${planId === p.id ? "border-ink ring-1 ring-ink" : "border-hairline hover:border-hairline-strong"}`}
            >
              <div className="text-[14px] font-medium text-ink">{p.name}</div>
              <div className="mt-1 text-[18px] font-semibold tabular-nums text-ink">
                {formatPriceVND(p.price_vnd)} <span className="caption font-normal text-muted">{p.mode === "seat" ? t("bill.perSeatMonth") : t("bill.perMonth")}</span>
              </div>
              {p.description && <p className="caption mt-1 text-muted">{p.description}</p>}
              <ul className="caption mt-2 space-y-0.5 text-body">
                {p.limits.length === 0 ? <li>{t("usg.unlimited")}</li> : p.limits.map((l, i) => <li key={i}>• {usageLimitLabel(l, limitNames)}</li>)}
              </ul>
            </button>
          ))}
        </div>
      )}

      {plan && (
        <div className="rounded-lg border border-hairline p-4">
          <div className="grid gap-3 sm:grid-cols-3">
            <label className="block">
              <span className="caption mb-1 block text-muted">{t("bill.months")}</span>
              <select className="input" value={months} onChange={(e) => setMonths(Number(e.target.value))}>
                {MONTH_CHOICES.map((m) => (
                  <option key={m} value={m}>{t("bill.monthsN", { n: m })}</option>
                ))}
              </select>
            </label>
            {seatMode && (
              <label className="block">
                <span className="caption mb-1 block text-muted">{t("bill.seats")}</span>
                <input type="number" min={1} className="input" value={seats} onChange={(e) => setSeats(Math.max(1, Number(e.target.value) || 1))} />
              </label>
            )}
            <div className="block">
              <span className="caption mb-1 block text-muted">{t("bill.promo")}</span>
              <div className="flex gap-2">
                <input className="input uppercase" value={promoText} placeholder="VD: TET2027" onChange={(e) => setPromoText(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); setPromo(promoText.trim()); } }} />
                <button type="button" className="btn btn-outline" onClick={() => setPromo(promoText.trim())}>{t("bill.promoApply")}</button>
              </div>
              {quote?.promo_code && quote.promo_error && <span className="caption mt-1 block text-error">{t(promoErrorKey(quote.promo_error) as LocaleKey)}</span>}
              {quote?.promo_code && !quote.promo_error && (
                <span className="caption mt-1 block text-emerald-700">{t("bill.promoApplied", { name: quote.promo_name || quote.promo_code, amount: formatPriceVND(quote.discount_vnd) })}</span>
              )}
            </div>
          </div>
          {quoteError && <p className="caption mt-2 text-error">{quoteError}</p>}
          {quote && (
            <div className="mt-4 space-y-1 border-t border-hairline pt-3 text-[14px]">
              {!!quote.proration_vnd && (
                <div className="flex justify-between">
                  <span className="text-muted">{t("bill.proration", { seats: quote.seats - (quote.current_seats ?? 0), days: quote.prorated_days ?? 0 })}</span>
                  <span className="tabular-nums">{formatPriceVND(quote.proration_vnd)}</span>
                </div>
              )}
              <div className="flex justify-between"><span className="text-muted">{t("bill.subtotal")}</span><span className="tabular-nums">{formatPriceVND(quote.subtotal_vnd)}</span></div>
              {quote.discount_vnd > 0 && <div className="flex justify-between"><span className="text-muted">{t("bill.discount")}</span><span className="tabular-nums">−{formatPriceVND(quote.discount_vnd)}</span></div>}
              <div className="flex justify-between text-[15px] font-medium text-ink"><span>{t("bill.total")}</span><span className="tabular-nums">{formatPriceVND(quote.total_vnd)}</span></div>
            </div>
          )}
          <p className="caption mt-2 text-muted-soft">{t("bill.renewHint")}</p>
          <div className="mt-3 flex justify-end">
            <button type="button" className="btn btn-primary" disabled={!canBuy} onClick={() => void placeOrder()}>
              {t("bill.buy")}
            </button>
          </div>
        </div>
      )}

      <section>
        <div className="mb-2 flex items-center justify-between">
          <h3 className="text-[14px] font-medium text-ink">{t("bill.orders")}</h3>
          <button type="button" className="btn btn-outline btn-sm" onClick={() => void load()} title={t("bill.refresh")}>
            <IconRefresh className="h-3.5 w-3.5" />
          </button>
        </div>
        {orders.length === 0 ? (
          <p className="caption text-muted-soft">{t("bill.noOrders")}</p>
        ) : (
          <div className="divide-y divide-hairline rounded-lg border border-hairline">
            {orders.map((o) => (
              <button key={o.id} type="button" className="flex w-full flex-wrap items-center gap-3 px-3 py-2 text-left text-[13.5px] hover:bg-surface-strong" onClick={() => setOrder(o)}>
                <span className="font-mono text-ink">{o.order_code}</span>
                <span className="min-w-0 flex-1 truncate text-muted">{o.plan_name} · {t("bill.monthsN", { n: o.months })} · {o.scope_name}</span>
                <span className="tabular-nums text-ink">{formatPriceVND(o.total_vnd)}</span>
                <span className={`badge-pill text-[11px] ${o.status === "paid" ? "text-emerald-700" : o.status === "pending" ? "text-amber-700" : "text-muted"}`}>{t(statusKey(o.status))}</span>
                <span className="caption text-muted-soft">{fmtDate(o.created_at)}</span>
              </button>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
