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
  billingErrorKey,
  cancelMyUsageOrder,
  createUsageOrder,
  formatPriceVND,
  planPriceForMonths,
  planTermPrices,
  getBillingInfo,
  getScopeAddons,
  getUsageOrder,
  listMyUsageOrders,
  listPurchasablePlans,
  promoErrorKey,
  quoteUsageOrder,
  usageLimitLabel,
  type UsageBillingInfo,
  type UsageOrder,
  type UsagePlan,
  type UsageOrderAddonLine,
  type UsageQuote,
  type UsageScopeAddons,
  type UsageScopeKind,
} from "@/lib/api/usage";
import { addonLineDetail, keepOffered, toggleAddonId } from "@/lib/agent-addon-catalogue";
import { orderAmountDue, orderPaymentHeld, orderPlanLineVND, pendingPlanOrderCode } from "@/lib/billing-order";
import { formatDateTime, formatLastDay } from "@/lib/datetime";
import { BILLING_INTENT_EVENT, clearBillingIntent, readBillingIntent, type BillingIntent } from "@/lib/billing-intent";
import { useConfirm } from "@/components/confirm-dialog";
import { ConsolePage } from "@/components/settings/console-page";
import { FieldLabel } from "@/components/settings/field";
import { Loading } from "@/components/settings/loading";
import { Notice } from "@/components/settings/notice";
import { SectionHeader } from "@/components/settings/section-header";
import { useToast } from "@/components/toast";

const MONTH_CHOICES = [1, 3, 6, 12];

/* The month counts a plan can be bought for: those a priced term fits. */
function monthChoicesFor(plan: UsagePlan | null): number[] {
  if (!plan) return MONTH_CHOICES;
  return MONTH_CHOICES.filter((m) => planPriceForMonths(plan, m) != null);
}

function fmtDate(v: string | null | undefined, locale: string): string {
  if (!v) return "";
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? "" : formatDateTime(d, locale);
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

type ErrDetails = { reason?: string; current_seats?: number; order_code?: string; received_vnd?: number };
function errDetails(e: unknown): ErrDetails | undefined {
  if (!(e instanceof ApiError)) return undefined;
  return (e.payload as { error?: { details?: ErrDetails } } | undefined)?.error?.details;
}

/** The current seat count when the server refused a renewal with fewer
 * seats (reason seats_below_current), else null. */
function seatsBelowCurrent(e: unknown): number | null {
  const details = errDetails(e);
  return details?.reason === "seats_below_current" && typeof details.current_seats === "number" ? details.current_seats : null;
}

/** A billing refusal in the reader's language: known reasons have their own
 * text, anything else falls back to the server message. */
function useBillingError() {
  const { t } = useT();
  return useCallback(
    (e: unknown, fallback: LocaleKey) => {
      const current = seatsBelowCurrent(e);
      if (current !== null) return t("bill.seatsBelowCurrent", { n: current });
      const details = errDetails(e);
      const key = billingErrorKey(details?.reason);
      if (key) return t(key as LocaleKey, { amount: formatPriceVND(details?.received_vnd ?? 0) });
      return e instanceof Error && e.message ? e.message : t(fallback);
    },
    [t],
  );
}

export function BillingPage() {
  const { t, locale } = useT();
  const auth = useAuth();
  const confirmAction = useConfirm();
  const toast = useToast();
  const billingError = useBillingError();
  const tenantId = Number(auth.selectedTenantId ?? auth.tenant?.id ?? 0);
  const role = auth.memberships.find((m) => String(m.tenant_id) === String(tenantId))?.role ?? "";
  const isAdmin = role === "admin" || role === "owner" || auth.user?.is_system_admin === true;

  const [info, setInfo] = useState<UsageBillingInfo | null>(null);
  // a banner's Renew/Buy button names the scope and plan to open on
  const [intent] = useState(() => readBillingIntent());
  useEffect(() => clearBillingIntent(), []);
  useEffect(() => {
    const onIntent = (e: Event) => {
      const next = (e as CustomEvent<BillingIntent>).detail;
      clearBillingIntent();
      if (!next || (next.scope === "tenant" && !isAdmin)) return;
      setScope(next.scope);
      setPlanId(next.planId);
    };
    window.addEventListener(BILLING_INTENT_EVENT, onIntent);
    return () => window.removeEventListener(BILLING_INTENT_EVENT, onIntent);
  }, [isAdmin]);
  // (a scope the person may not buy for drops the intent's plan too)
  const intentAllowed = !!intent && (intent.scope === "user" || isAdmin);
  const [scope, setScope] = useState<UsageScopeKind>(intentAllowed ? intent.scope : isAdmin ? "tenant" : "user");
  const [plans, setPlans] = useState<UsagePlan[]>([]);
  const [planId, setPlanId] = useState(intentAllowed ? intent.planId : "");
  const [months, setMonths] = useState(1);
  // the seats box re-prices once typing pauses
  const [seatsText, setSeatsText] = useState(1);
  const [seats, setSeats] = useState(1);
  useEffect(() => {
    const id = window.setTimeout(() => setSeats(seatsText), 300);
    return () => window.clearTimeout(id);
  }, [seatsText]);
  // plan change: held add-ons carried over (null = the server default)
  const [keepIds, setKeepIds] = useState<string[] | null>(null);
  const [quoting, setQuoting] = useState(false);
  const [promoText, setPromoText] = useState("");
  const [promo, setPromo] = useState("");
  const [quote, setQuote] = useState<UsageQuote | null>(null);
  const [quoteError, setQuoteError] = useState("");
  const [orders, setOrders] = useState<UsageOrder[]>([]);
  const [order, setOrder] = useState<UsageOrder | null>(null);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  // specialist agents bought with the plan
  const [addonIds, setAddonIds] = useState<string[]>([]);
  // the scope's current subscription, its add-ons, and a mid-term purchase
  const [scopeAddons, setScopeAddons] = useState<UsageScopeAddons | null>(null);
  const [extraIds, setExtraIds] = useState<string[]>([]);
  const [extraQuote, setExtraQuote] = useState<UsageQuote | null>(null);
  const [extraError, setExtraError] = useState("");

  const plan = useMemo(() => plans.find((p) => p.id === planId) ?? null, [plans, planId]);
  const limitNames = { questions: t("usg.metricQuestions"), tokens: t("usg.metricTokens"), day: t("usg.periodDay"), month: t("usg.periodMonth") };
  const seatMode = scope === "tenant" && plan?.mode === "seat";

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [i, pl, os] = await Promise.all([getBillingInfo(), listPurchasablePlans(scope), listMyUsageOrders()]);
      setInfo(i);
      setPlans(pl);
      setOrders(os);
      getScopeAddons(scope)
        .then((sa) => {
          setScopeAddons(sa);
          setExtraIds((cur) => keepOffered(cur, sa.purchasable));
        })
        .catch(() => setScopeAddons(null));
      setPlanId((cur) => (pl.some((p) => p.id === cur) ? cur : (pl[0]?.id ?? "")));
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : t("bill.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [scope, t]);

  useEffect(() => {
    void load();
  }, [load]);

  // a plan switch keeps only the add-ons the new plan sells; renewing the
  // package in use ticks the agents it holds, so they do not stop at the
  // old end unnoticed
  const currentPlanId = scopeAddons?.subscription?.plan_id ?? "";
  // a string key: every load() brings a new array, and an unticked agent
  // must not be ticked again by a refresh
  const heldKey = (scopeAddons?.held ?? []).map((h) => h.agent_id).sort().join(",");
  useEffect(() => {
    setAddonIds((cur) => {
      const offered = plan?.addons ?? [];
      const kept = keepOffered(cur, offered);
      if (!plan || plan.id !== currentPlanId) return kept;
      const held = heldKey ? heldKey.split(",") : [];
      return keepOffered([...new Set([...kept, ...held])], offered);
    });
    setKeepIds(null);
  }, [plan, currentPlanId, heldKey]);
  const renewing = !!plan && plan.id === currentPlanId;

  // ...and a month count the new plan is sold for (a yearly-only plan → 12)
  const monthChoices = useMemo(() => monthChoicesFor(plan), [plan]);
  useEffect(() => {
    if (monthChoices.length > 0 && !monthChoices.includes(months)) setMonths(monthChoices[0]);
  }, [monthChoices, months]);

  /* "12 tháng · gói năm", "3 tháng · gói quý (tiết kiệm 15.000 VNĐ)". */
  const monthOptionLabel = (m: number) => {
    const priced = plan ? planPriceForMonths(plan, m) : null;
    const base = t("bill.monthsN", { n: m });
    if (!plan || !priced || priced.term === "month") return base;
    const full = plan.price_vnd > 0 ? plan.price_vnd * m : 0;
    const termName = priced.term === "year" ? t("bill.termYear") : t("bill.termQuarter");
    return full > priced.total
      ? `${base} · ${termName} (${t("bill.saving", { price: formatPriceVND(full - priced.total) })})`
      : `${base} · ${termName}`;
  };
  /* An order's length: "12 tháng · gói năm". */
  const orderLength = (o: UsageOrder) => {
    if (o.kind === "addon") return t("bill.addonOrder");
    const base = t("bill.monthsN", { n: o.months });
    if (o.billing_term === "year") return `${base} · ${t("bill.termYear")}`;
    if (o.billing_term === "quarter") return `${base} · ${t("bill.termQuarter")}`;
    return base;
  };

  // Re-price whenever the choice changes.
  useEffect(() => {
    if (!planId) {
      setQuote(null);
      return;
    }
    // a plan switch first moves months to one the plan is sold for (the
    // effect above); pricing the stale count would only flash a 400
    if (plan?.id === planId && !monthChoices.includes(months)) return;
    let alive = true;
    setQuoting(true);
    quoteUsageOrder({
      scope_type: scope,
      plan_id: planId,
      months,
      seats: seatMode ? seats : 0,
      promo_code: promo,
      kind: "plan",
      addon_agent_ids: addonIds,
      ...(keepIds ? { keep_addon_agent_ids: keepIds } : {}),
    })
      .then((q) => {
        if (!alive) return;
        setQuote(q);
        setQuoteError("");
      })
      .catch((e) => {
        if (!alive) return;
        setQuote(null);
        setQuoteError(billingError(e, "bill.loadFailed"));
      })
      .finally(() => {
        if (alive) setQuoting(false);
      });
    return () => {
      alive = false;
    };
  }, [scope, planId, plan, monthChoices, months, seats, seatMode, promo, addonIds, keepIds, billingError]);

  // Price the add-ons bought on the current subscription.
  useEffect(() => {
    if (extraIds.length === 0) {
      setExtraQuote(null);
      setExtraError("");
      return;
    }
    let alive = true;
    quoteUsageOrder({ scope_type: scope, plan_id: "", months: 0, kind: "addon", addon_agent_ids: extraIds, promo_code: promo })
      .then((q) => {
        if (!alive) return;
        setExtraQuote(q);
        setExtraError("");
      })
      .catch((e) => {
        if (!alive) return;
        setExtraQuote(null);
        setExtraError(billingError(e, "bill.loadFailed"));
      });
    return () => {
      alive = false;
    };
  }, [scope, extraIds, promo, billingError]);

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
      setError(billingError(e, "bill.opFailed"));
    } finally {
      setBusy(false);
    }
  };

  const placeOrder = () =>
    run(async () => {
      const req = {
        scope_type: scope,
        plan_id: planId,
        months,
        seats: seatMode ? seats : 0,
        promo_code: promo,
        kind: "plan" as const,
        addon_agent_ids: addonIds,
        ...(keepIds ? { keep_addon_agent_ids: keepIds } : {}),
      };
      let o: UsageOrder;
      try {
        o = await createUsageOrder(req);
      } catch (e) {
        // the scope already has a package order waiting for payment:
        // paying both would apply both, so ask first
        const code = pendingPlanOrderCode(errDetails(e));
        if (code === null) throw e;
        const yes = await confirmAction({
          title: t("common.confirm"),
          message: t("bill.duplicateConfirm", { code }),
          confirmLabel: t("bill.duplicateYes"),
        });
        if (!yes) return;
        o = await createUsageOrder({ ...req, confirm_duplicate: true });
      }
      setOrder(o);
      await load();
      toast.success(t("bill.waiting"));
    });

  const placeAddonOrder = () =>
    run(async () => {
      const o = await createUsageOrder({ scope_type: scope, plan_id: "", months: 0, kind: "addon", addon_agent_ids: extraIds, promo_code: promo });
      setOrder(o);
      setExtraIds([]);
      await load();
      toast.success(t("bill.waiting"));
    });

  const lineLabels = {
    months: (n: number) => t("bill.monthsN", { n }),
    half: (days: number) => t("bill.addonHalf", { days }),
    full: (days: number) => t("bill.addonFull", { days }),
    gap: (span: string) => t("bill.addonGap", { span }),
    extraSeats: (n: number, amount: string) => t("bill.addonExtraSeats", { n, amount }),
  };
  // add-ons the scope holds now, by agent (shown next to the plan's list)
  const heldUntil = useMemo(() => {
    const m: Record<string, string | null> = {};
    for (const h of scopeAddons?.held ?? []) m[h.agent_id] = h.ends_at ?? scopeAddons?.subscription?.ends_at ?? null;
    return m;
  }, [scopeAddons]);
  const renderLines = (lines: UsageOrderAddonLine[] | undefined) =>
    (lines ?? []).map((l) => (
      <div key={l.agent_id} className="flex justify-between gap-3">
        <span className="min-w-0 text-muted">
          {t("bill.addonLine", { name: l.name || l.agent_id })}{" "}
          <span className="caption text-muted-soft">{addonLineDetail(l, formatPriceVND, lineLabels)}</span>
        </span>
        <span className="tabular-nums">{formatPriceVND(l.amount_vnd)}</span>
      </div>
    ));

  // stay: cancelled from the shop page's reminder, which stays put
  const cancelOrder = async (o: UsageOrder, stay = false) => {
    const ok = await confirmAction({
      title: t("common.confirm"),
      message: t("bill.cancelConfirm", { code: o.order_code }),
      danger: true,
    });
    if (!ok) return;
    await run(async () => {
      const u = await cancelMyUsageOrder(o.order_code);
      if (!stay) setOrder(u);
      await load();
    });
  };

  // the list rows lack the single-order details (transfers received, the
  // package's new end): show the row at once, then the full view
  const openOrder = (o: UsageOrder) => {
    setOrder(o);
    getUsageOrder(o.order_code)
      .then((full) => setOrder((cur) => (cur && cur.id === full.id ? full : cur)))
      .catch(() => undefined);
  };
  // the scope's orders still waiting, reminded at the top of the page
  const scopeId = scope === "tenant" ? String(tenantId) : String(auth.user?.id ?? "");
  const pendingOrders = orders.filter((o) => o.status === "pending" && o.scope_type === scope && o.scope_id === scopeId);

  // a code typed but not applied would silently be ignored by the order
  const promoPending = promoText.trim().toUpperCase() !== promo.trim().toUpperCase();
  const canBuy = !!info?.enabled && !!quote && !busy && !quoting && !promoPending && seats === seatsText && (!seatMode || seats > 0);
  const toggleKeep = (id: string) => {
    const cur = (quote?.held_addons ?? []).filter((h) => h.kept && !h.bought_again).map((h) => h.agent_id);
    setKeepIds(toggleAddonId(cur, id));
  };

  // ---- payment view ----------------------------------------------------------
  if (order) {
    const pending = order.status === "pending";
    // money arrived without confirming the order: a SuperAdmin handles it
    const held = pending && orderPaymentHeld(order);
    return (
      <ConsolePage size="form" className="px-5 py-6">
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
                {order.plan_name} ·{" "}
                {orderLength(order)}
                {order.mode === "seat" ? ` · ${t("bill.seatsN", { n: order.seats })}` : ""} · {order.scope_name}
              </div>
            </div>
            <span className={`badge-pill text-[11px] ${order.status === "paid" ? "badge-success" : pending ? "badge-warning" : "badge-neutral"}`}>{t(statusKey(order.status))}</span>
          </div>
          <div className="space-y-1 px-4 py-3 text-[14px]">
            {order.kind !== "addon" && (
              <div className="flex justify-between gap-3">
                <span className="min-w-0 text-muted">
                  {t("bill.planLine", { name: order.plan_name })}{" "}
                  <span className="caption text-muted-soft">
                    {order.mode === "seat"
                      ? t("bill.planLineDetailSeats", { length: orderLength(order), seats: order.seats, price: formatPriceVND(order.unit_price_vnd) })
                      : t("bill.planLineDetail", { length: orderLength(order), price: formatPriceVND(order.unit_price_vnd) })}
                  </span>
                </span>
                <span className="tabular-nums">{formatPriceVND(orderPlanLineVND(order))}</span>
              </div>
            )}
            {!!order.proration_vnd && (
              <div className="flex justify-between gap-3">
                <span className="min-w-0 text-muted">{t("bill.prorationOrder", { days: order.prorated_days ?? 0 })}</span>
                <span className="tabular-nums">{formatPriceVND(order.proration_vnd)}</span>
              </div>
            )}
            {renderLines(order.addons)}
            <div className="flex justify-between"><span className="text-muted">{t("bill.subtotal")}</span><span className="tabular-nums">{formatPriceVND(order.subtotal_vnd)}</span></div>
            {!!order.savings_vnd && (
              <div className="caption flex justify-between text-success">
                <span>{order.billing_term === "year" ? t("bill.savedYear") : t("bill.savedQuarter")}</span>
                <span className="tabular-nums">{formatPriceVND(order.savings_vnd)}</span>
              </div>
            )}
            {!!order.credit_vnd && (
              <div className="flex justify-between"><span className="text-muted">{t("bill.credit")}</span><span className="tabular-nums">−{formatPriceVND(order.credit_vnd)}</span></div>
            )}
            {/* what the credit is made of (capped at the subtotal above) */}
            {(order.credit_lines ?? []).map((c) => (
              <div key={`${c.kind}:${c.agent_id ?? ""}`} className="caption flex justify-between gap-3 pl-3 text-muted-soft">
                <span className="min-w-0">
                  {c.paid
                    ? t("bill.creditLine", { name: c.name || c.agent_id || "", days: c.days })
                    : t("bill.creditUnpaid", { name: c.name || c.agent_id || "" })}
                </span>
                <span className="tabular-nums">{c.paid ? formatPriceVND(c.amount_vnd) : ""}</span>
              </div>
            ))}
            {order.discount_vnd > 0 && (
              <div className="flex justify-between"><span className="text-muted">{t("bill.discount")} ({order.promo_code})</span><span className="tabular-nums">−{formatPriceVND(order.discount_vnd)}</span></div>
            )}
            <div className="flex justify-between font-medium text-ink"><span>{t("bill.total")}</span><span className="tabular-nums">{formatPriceVND(order.total_vnd)}</span></div>
          </div>
          {pending && order.total_vnd <= 0 && (
            <div className="border-t border-hairline px-4 py-3 text-[14px] text-muted">
              <p>{t("bill.freeOrderWaits")}</p>
              <p className="caption mt-1 text-muted">{t("bill.freeNoDeadline")}</p>
              <p className="caption mt-1 flex items-center gap-1 text-muted">
                <IconRefresh className="h-3.5 w-3.5 animate-spin" /> {t("bill.waiting")}
              </p>
              <div className="mt-3 flex justify-end">
                <button type="button" className="btn btn-danger btn-sm" disabled={busy} onClick={() => void cancelOrder(order)}>
                  {t("bill.cancel")}
                </button>
              </div>
            </div>
          )}
          {pending && order.total_vnd > 0 && held && (
            <div className="border-t border-hairline px-4 py-3 text-[14px]">
              <p className="text-ink">{t("bill.paymentHeld")}</p>
              {!!order.received_vnd && (
                <p className="caption mt-1 text-muted">{t("bill.receivedSoFar", { amount: formatPriceVND(order.received_vnd) })}</p>
              )}
              {info?.contact && <p className="caption mt-1 text-muted">{info.contact}</p>}
            </div>
          )}
          {pending && order.total_vnd > 0 && !held && (
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
                  {!!order.received_vnd && order.received_vnd < order.total_vnd && (
                    <p className="text-warning">
                      {t("bill.underpaid", {
                        received: formatPriceVND(order.received_vnd),
                        total: formatPriceVND(order.total_vnd),
                        rest: formatPriceVND(orderAmountDue(order)),
                      })}
                    </p>
                  )}
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-muted">{t("bill.amount")}: </span>
                    <span className="font-medium text-ink tabular-nums">{formatPriceVND(orderAmountDue(order))}</span>
                    <CopyButton value={String(orderAmountDue(order))} label={t("bill.copy")} copied={t("bill.copied")} />
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-muted">{t("bill.content")}: </span>
                    <span className="font-mono font-medium text-ink">{order.transfer_content}</span>
                    <CopyButton value={order.transfer_content} label={t("bill.copy")} copied={t("bill.copied")} />
                  </div>
                  <p className="caption text-muted-soft">{t("bill.contentHint")}</p>
                  {order.expires_at && <p className="caption text-warning">{t("bill.expiresAt", { date: fmtDate(order.expires_at, locale) })}</p>}
                  {info?.contact && <p className="caption text-muted">{info.contact}</p>}
                  <p className="caption flex items-center gap-1 text-muted">
                    <IconRefresh className="h-3.5 w-3.5 animate-spin" /> {t("bill.waiting")}
                  </p>
                </div>
              </div>
              {/* an order that has received money is not cancelled here */}
              {!order.received_vnd && (
                <div className="mt-3 flex justify-end">
                  <button type="button" className="btn btn-danger btn-sm" disabled={busy} onClick={() => void cancelOrder(order)}>
                    {t("bill.cancel")}
                  </button>
                </div>
              )}
            </div>
          )}
          {order.status === "paid" && (
            <div className="border-t border-hairline px-4 py-3 text-[14px] text-success">
              {order.subscription_ends_at
                ? t("bill.paidUntil", { time: fmtDate(order.paid_at, locale), plan: order.plan_name, date: formatLastDay(order.subscription_ends_at) })
                : t("bill.paidAt", { time: fmtDate(order.paid_at, locale) })}
            </div>
          )}
          {(order.status === "cancelled" || order.status === "expired") && (
            <div className="border-t border-hairline px-4 py-3 text-[14px] text-muted">
              <p>{t(order.status === "expired" ? "bill.expired" : "bill.cancelled")}</p>
              {order.total_vnd > 0 && (
                <p className="caption mt-1 text-muted">
                  {info?.contact
                    ? t("bill.closedTransferHint", { code: order.transfer_content, contact: info.contact })
                    : t("bill.closedTransferHintAdmin", { code: order.transfer_content })}
                </p>
              )}
            </div>
          )}
        </div>
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
      </ConsolePage>
    );
  }

  // ---- shop view ---------------------------------------------------------------
  return (
    <ConsolePage size="form" className="space-y-6 px-5 py-6">
      <SectionHeader title={t("bill.title")} subtitle={t("bill.intro")} />
      {loading && !info && <Loading />}
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
      {info && !info.enabled && (
        <Notice tone="warning">
          {t("bill.disabled")} {info.contact}
        </Notice>
      )}

      {isAdmin && (
        <div className="flex gap-2">
          {(["tenant", "user"] as UsageScopeKind[]).map((s) => (
            <button key={s} type="button" className={`btn btn-sm ${scope === s ? "btn-primary" : "btn-outline"}`} onClick={() => { setScope(s); setPromo(""); setPromoText(""); setKeepIds(null); }}>
              {s === "tenant" ? t("bill.scopeTenant") : t("bill.scopeUser")}
            </button>
          ))}
        </div>
      )}

      {pendingOrders.map((o) => (
        <Notice
          key={o.id}
          tone="warning"
          action={
            <div className="flex gap-2">
              <button type="button" className="btn btn-outline btn-sm" onClick={() => openOrder(o)}>
                {t("bill.open")}
              </button>
              {!o.received_vnd && (
                <button type="button" className="btn btn-outline btn-sm" disabled={busy} onClick={() => void cancelOrder(o, true)}>
                  {t("bill.cancel")}
                </button>
              )}
            </div>
          }
        >
          {o.total_vnd > 0
            ? t("bill.pendingBanner", { code: o.order_code, total: formatPriceVND(o.total_vnd) })
            : t("bill.pendingFreeBanner", { code: o.order_code })}
        </Notice>
      ))}

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
              {planTermPrices(p).map((x, i) => (
                <div
                  key={x.term}
                  className={i === 0 ? "mt-1 text-[18px] font-semibold tabular-nums text-ink" : "caption tabular-nums text-body"}
                >
                  {formatPriceVND(x.price)}{" "}
                  <span className={i === 0 ? "caption font-normal text-muted" : "text-muted"}>
                    {p.mode === "seat" ? `${t("bill.perSeat")} ` : ""}
                    {x.term === "year" ? t("bill.perYear") : x.term === "quarter" ? t("bill.perQuarter") : t("bill.perMonthShort")}
                  </span>
                  {x.saving > 0 && <span className="ml-1 text-success">({t("bill.saving", { price: formatPriceVND(x.saving) })})</span>}
                </div>
              ))}
              {p.description && <p className="caption mt-1 text-muted">{p.description}</p>}
              <ul className="caption mt-2 space-y-0.5 text-body">
                {p.limits.length === 0 ? <li>{t("usg.unlimited")}</li> : p.limits.map((l, i) => <li key={i}>• {usageLimitLabel(l, limitNames)}</li>)}
              </ul>
              {(p.included_agents ?? []).length > 0 && (
                <p className="caption mt-2 text-muted">
                  {t("bill.includedAgents", { names: (p.included_agents ?? []).map((a) => a.name).join(", ") })}
                </p>
              )}
              {(p.addons ?? []).length > 0 && (
                <p className="caption mt-0.5 text-muted-soft">{t("bill.addonsAvailable", { n: (p.addons ?? []).length })}</p>
              )}
            </button>
          ))}
        </div>
      )}

      {plan && (
        <div className="rounded-lg border border-hairline p-4">
          <div className="grid gap-3 sm:grid-cols-3">
            <div className="block">
              <FieldLabel htmlFor="bill-months">{t("bill.months")}</FieldLabel>
              <select
                id="bill-months"
                className="input"
                value={months}
                onChange={(e) => setMonths(Number(e.target.value))}
              >
                {monthChoices.map((m) => (
                  <option key={m} value={m}>{monthOptionLabel(m)}</option>
                ))}
              </select>
            </div>
            {seatMode && (
              <div className="block">
                <FieldLabel htmlFor="bill-seats">{t("bill.seats")}</FieldLabel>
                <input
                  id="bill-seats"
                  type="number"
                  min={1}
                  className="input"
                  value={seatsText}
                  onChange={(e) => setSeatsText(Math.max(1, Number(e.target.value) || 1))}
                />
              </div>
            )}
            <div className="block">
              <FieldLabel htmlFor="bill-promo">{t("bill.promo")}</FieldLabel>
              <div className="flex gap-2">
                <input
                  id="bill-promo"
                  aria-label={t("bill.promo")}
                  className="input uppercase"
                  value={promoText} placeholder="VD: TET2027" onChange={(e) => setPromoText(e.target.value)} onBlur={() => setPromo(promoText.trim())} onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); setPromo(promoText.trim()); } }} />
                <button type="button" className="btn btn-outline" onClick={() => setPromo(promoText.trim())}>{t("bill.promoApply")}</button>
              </div>
              {promoPending && <span className="caption mt-1 block text-warning">{t("bill.promoNotApplied")}</span>}
              {!promoPending && quote?.promo_code && quote.promo_error && <span className="caption mt-1 block text-error">{t(promoErrorKey(quote.promo_error) as LocaleKey)}</span>}
              {!promoPending && quote?.promo_code && !quote.promo_error && (
                <span className="caption mt-1 block text-success">{t("bill.promoApplied", { name: quote.promo_name || quote.promo_code, amount: formatPriceVND(quote.discount_vnd) })}</span>
              )}
            </div>
          </div>
          {(plan.addons ?? []).length > 0 && (
            <div className="mt-4">
              <span className="caption mb-1 block text-muted">{t("bill.addonsTitle")}</span>
              <div className="space-y-1.5">
                {(plan.addons ?? []).map((a) => (
                  <label key={a.agent_id} className="flex cursor-pointer items-start gap-2.5 rounded-md border border-hairline px-3 py-2 select-none">
                    <input type="checkbox" className="mt-0.5" checked={addonIds.includes(a.agent_id)} onChange={() => setAddonIds((cur) => toggleAddonId(cur, a.agent_id))} />
                    <span className="min-w-0 flex-1">
                      <span className="block text-[13.5px] text-ink">{a.name}</span>
                      {a.description && <span className="caption block text-muted">{a.description}</span>}
                      {a.agent_id in heldUntil && (
                        <span className="caption block text-warning">
                          {renewing && !addonIds.includes(a.agent_id) && heldUntil[a.agent_id]
                            ? t("bill.addonStops", { date: formatLastDay(heldUntil[a.agent_id]) })
                            : heldUntil[a.agent_id]
                              ? t("bill.addonHeldUntil", { date: formatLastDay(heldUntil[a.agent_id]) })
                              : t("bill.addonHeld")}
                        </span>
                      )}
                    </span>
                    <span className="caption shrink-0 tabular-nums text-ink">
                      {formatPriceVND(a.price_vnd)}
                      {a.per_seat ? t("bill.perSeatMonth") : t("bill.perMonth")}
                    </span>
                  </label>
                ))}
              </div>
            </div>
          )}
          {quoteError && <p className="caption mt-2 text-error">{quoteError}</p>}
          {quote?.replaces && (
            <div className="mt-4">
              <Notice tone="warning">
                {quote.replaces.ends_at
                  ? t("bill.replaces", { plan: quote.replaces.plan_name, date: formatLastDay(quote.replaces.ends_at) })
                  : t("bill.replacesOpen", { plan: quote.replaces.plan_name })}
              </Notice>
              {(quote.held_addons ?? []).length > 0 && (
                <div className="mt-3">
                  <span className="caption mb-1 block text-muted">{t("bill.heldTitle")}</span>
                  <div className="space-y-1.5">
                    {(quote.held_addons ?? []).map((h) => (
                      <label key={h.agent_id} className={`flex items-start gap-2.5 rounded-md border border-hairline px-3 py-2 select-none ${h.bought_again ? "opacity-70" : "cursor-pointer"}`}>
                        <input type="checkbox" className="mt-0.5" checked={h.kept} disabled={h.bought_again || quoting} onChange={() => toggleKeep(h.agent_id)} />
                        <span className="min-w-0 flex-1">
                          <span className="block text-[13.5px] text-ink">{t("bill.heldKeep", { name: h.name || h.agent_id })}</span>
                          <span className="caption block text-muted">
                            {h.bought_again
                              ? t("bill.heldBoughtAgain")
                              : h.kept
                                ? h.carry_vnd > 0
                                  ? t("bill.heldKeepCost", { amount: formatPriceVND(h.carry_vnd), date: formatLastDay(h.ends_at) })
                                  : t("bill.heldKeepFree", { date: formatLastDay(h.ends_at) })
                                : h.credit_vnd > 0
                                  ? t("bill.heldDropCredit", { amount: formatPriceVND(h.credit_vnd) })
                                  : t("bill.heldDrop")}
                          </span>
                        </span>
                      </label>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}
          {!!quote?.seats_carried && <p className="caption mt-2 text-muted">{t("bill.seatsCarried", { n: quote.seats_carried })}</p>}
          {!!quote?.seats_held && quote.seats_held > (quote.seats_carried ?? 0) && (
            <p className="caption mt-1 text-warning">{t("bill.seatsDropped", { n: quote.seats_held - (quote.seats_carried ?? 0), total: quote.seats_held })}</p>
          )}
          {quote && (
            <div className={`mt-4 space-y-1 border-t border-hairline pt-3 text-[14px] ${quoting ? "opacity-50" : ""}`} aria-busy={quoting}>
              {renderLines(quote.addons)}
              {!!quote.proration_vnd && (
                <div className="flex justify-between">
                  <span className="text-muted">{t("bill.proration", { seats: quote.seats - (quote.current_seats ?? 0), days: quote.prorated_days ?? 0 })}</span>
                  <span className="tabular-nums">{formatPriceVND(quote.proration_vnd)}</span>
                </div>
              )}
              <div className="flex justify-between"><span className="text-muted">{t("bill.subtotal")}</span><span className="tabular-nums">{formatPriceVND(quote.subtotal_vnd)}</span></div>
              {!!quote.savings_vnd && (
                <div className="caption flex justify-between text-success">
                  <span>{quote.billing_term === "year" ? t("bill.savedYear") : t("bill.savedQuarter")}</span>
                  <span className="tabular-nums">{formatPriceVND(quote.savings_vnd)}</span>
                </div>
              )}
              {(quote.credit_lines ?? []).map((c) => (
                <div key={`${c.kind}:${c.agent_id ?? ""}`} className="flex justify-between gap-3">
                  <span className="min-w-0 text-muted">
                    {c.paid
                      ? t("bill.creditLine", { name: c.name || c.agent_id || "", days: c.days })
                      : t("bill.creditUnpaid", { name: c.name || c.agent_id || "" })}
                  </span>
                  <span className="tabular-nums">{c.paid ? `−${formatPriceVND(c.amount_vnd)}` : ""}</span>
                </div>
              ))}
              {!!quote.credit_unused_vnd && (
                <p className="caption text-warning">{t("bill.creditUnused", { amount: formatPriceVND(quote.credit_unused_vnd) })}</p>
              )}
              {quote.discount_vnd > 0 && <div className="flex justify-between"><span className="text-muted">{t("bill.discount")}</span><span className="tabular-nums">−{formatPriceVND(quote.discount_vnd)}</span></div>}
              <div className="flex justify-between text-[15px] font-medium text-ink"><span>{t("bill.total")}</span><span className="tabular-nums">{formatPriceVND(quote.total_vnd)}</span></div>
              {quote.subscription_ends_at && (
                <p className="caption text-muted">{t("bill.newEnd", { date: formatLastDay(quote.subscription_ends_at) })}</p>
              )}
            </div>
          )}
          <p className="caption mt-2 text-muted">{t("bill.renewHint")}</p>
          <div className="mt-3 flex justify-end">
            <button type="button" className="btn btn-primary" disabled={!canBuy} onClick={() => void placeOrder()}>
              {t("bill.buy")}
            </button>
          </div>
        </div>
      )}

      {scopeAddons?.subscription && (scopeAddons.held.length > 0 || scopeAddons.purchasable.length > 0) && (
        <section className="rounded-lg border border-hairline p-4">
          <h3 className="text-[14px] font-medium text-ink">{t("bill.currentAddonsTitle")}</h3>
          <p className="caption text-muted">
            {scopeAddons.subscription.ends_at
              ? t("bill.currentAddonsIntro", { date: formatLastDay(scopeAddons.subscription.ends_at) })
              : t("bill.currentAddonsOpen")}
          </p>
          {scopeAddons.held.length > 0 && (
            <ul className="caption mt-2 space-y-0.5 text-body">
              {scopeAddons.held.map((h) => (
                <li key={h.agent_id}>
                  • {h.agent_name || h.agent_id}
                  {h.ends_at ? ` — ${t("bill.addonUntil", { date: formatLastDay(h.ends_at) })}` : ` — ${t("bill.addonWithSub")}`}
                </li>
              ))}
            </ul>
          )}
          {scopeAddons.purchasable.length > 0 && scopeAddons.subscription.ends_at && (
            <>
              <div className="mt-3 space-y-1.5">
                {scopeAddons.purchasable.map((a) => (
                  <label key={a.agent_id} className="flex cursor-pointer items-start gap-2.5 rounded-md border border-hairline px-3 py-2 select-none">
                    <input type="checkbox" className="mt-0.5" checked={extraIds.includes(a.agent_id)} onChange={() => setExtraIds((cur) => toggleAddonId(cur, a.agent_id))} />
                    <span className="min-w-0 flex-1">
                      <span className="block text-[13.5px] text-ink">{a.name}</span>
                      {a.description && <span className="caption block text-muted">{a.description}</span>}
                    </span>
                    <span className="caption shrink-0 tabular-nums text-ink">
                      {formatPriceVND(a.price_vnd)}
                      {a.per_seat ? t("bill.perSeatMonth") : t("bill.perMonth")}
                    </span>
                  </label>
                ))}
              </div>
              <p className="caption mt-2 text-muted-soft">{t("bill.midTermRule")}</p>
              {extraError && <p className="caption mt-2 text-error">{extraError}</p>}
              {extraQuote && (
                <div className="mt-3 space-y-1 border-t border-hairline pt-3 text-[14px]">
                  {renderLines(extraQuote.addons)}
                  {extraQuote.discount_vnd > 0 && <div className="flex justify-between"><span className="text-muted">{t("bill.discount")}</span><span className="tabular-nums">−{formatPriceVND(extraQuote.discount_vnd)}</span></div>}
                  <div className="flex justify-between text-[15px] font-medium text-ink"><span>{t("bill.total")}</span><span className="tabular-nums">{formatPriceVND(extraQuote.total_vnd)}</span></div>
                </div>
              )}
              <div className="mt-3 flex justify-end">
                <button type="button" className="btn btn-primary" disabled={!info?.enabled || !extraQuote || busy} onClick={() => void placeAddonOrder()}>
                  {t("bill.buyAddons")}
                </button>
              </div>
            </>
          )}
        </section>
      )}

      <section>
        <div className="mb-2 flex items-center justify-between">
          <h3 className="text-[14px] font-medium text-ink">{t("bill.orders")}</h3>
          <button
            type="button"
            className="btn btn-outline btn-sm"
            onClick={() => void load()}
            title={t("bill.refresh")}
            aria-label={t("bill.refresh")}
          >
            <IconRefresh className="h-3.5 w-3.5" />
          </button>
        </div>
        {orders.length === 0 ? (
          <p className="caption text-muted-soft">{t("bill.noOrders")}</p>
        ) : (
          <div className="divide-y divide-hairline rounded-lg border border-hairline">
            {orders.map((o) => (
              <button key={o.id} type="button" className="flex w-full flex-wrap items-center gap-3 px-3 py-2 text-left text-[13.5px] hover:bg-surface-strong" onClick={() => openOrder(o)}>
                <span className="font-mono text-ink">{o.order_code}</span>
                <span className="min-w-0 flex-1 truncate text-muted">
                  {o.plan_name} · {orderLength(o)}
                  {(o.addons ?? []).length > 0 ? ` · ${t("bill.addonsN", { n: (o.addons ?? []).length })}` : ""} · {o.scope_name}
                </span>
                <span className="tabular-nums text-ink">{formatPriceVND(o.total_vnd)}</span>
                <span className={`badge-pill text-[11px] ${o.status === "paid" ? "badge-success" : o.status === "pending" ? "badge-warning" : "badge-neutral"}`}>{t(statusKey(o.status))}</span>
                <span className="caption text-muted-soft">{fmtDate(o.created_at, locale)}</span>
              </button>
            ))}
          </div>
        )}
      </section>
    </ConsolePage>
  );
}
