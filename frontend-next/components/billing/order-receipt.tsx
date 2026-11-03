"use client";

/* The payment receipt of a paid order (always in Vietnamese: it is a
 * payment document for Vietnamese organisations; only the on-screen
 * buttons and messages follow the UI language): seller, buyer (with the VAT invoice
 * details when given), the order's lines, the total, how and when it was
 * paid and the package's last usable day. "In / Lưu PDF" prints it on A4
 * (the browser's "Save as PDF" makes the PDF). A receipt is not a VAT
 * invoice; once a SuperAdmin marked the invoice issued its number is shown
 * instead of that note. */

import { useEffect, useState } from "react";
import Link from "next/link";
import { translateIn, useT, type LocaleKey } from "@/lib/i18n";
import { formatDateTime, formatLastDay } from "@/lib/datetime";
import { addonLineDetail } from "@/lib/agent-addon-catalogue";
import { orderPlanLineVND } from "@/lib/billing-order";
import { invoiceState } from "@/lib/billing-invoice";
import { issuedInvoiceText } from "@/components/settings/billing-invoice";
import { formatPriceVND, getOrderReceipt, type UsageOrder, type UsageOrderReceipt } from "@/lib/api/usage";
import { Loading } from "@/components/settings/loading";

const BILLING_PAGE = "/platform/system/workspace/billing";

/* A4 with the browser's header/footer margins; only the sheet prints. */
const PRINT_CSS = `
@page { size: A4; margin: 14mm 14mm 16mm; }
@media print {
  html, body { background: #fff !important; }
  .receipt-sheet { box-shadow: none !important; border: 0 !important; margin: 0 !important; padding: 0 !important; width: auto !important; min-height: 0 !important; }
  .receipt-sheet tr, .receipt-sheet .receipt-block { break-inside: avoid; }
}
`;

type Line = { label: string; detail?: string; amount: number; sign?: "-" };

/** The receipt's own language, whatever the UI language. */
const RECEIPT_LOCALE = "vi" as const;
const tr = (k: LocaleKey, vars?: Record<string, string | number>) => translateIn(RECEIPT_LOCALE, k, vars);

function orderLines(o: UsageOrder | null): { lines: Line[]; totals: Line[] } {
  const t = tr;
  if (!o) return { lines: [], totals: [] };
  const length = (() => {
    const base = t("bill.monthsN", { n: o.months });
    if (o.billing_term === "year") return `${base} · ${o.months > 12 ? t("bill.termYearN", { n: o.months / 12 }) : t("bill.termYear")}`;
    if (o.billing_term === "quarter") return `${base} · ${o.months > 3 ? t("bill.termQuarterN", { n: o.months / 3 }) : t("bill.termQuarter")}`;
    return base;
  })();
  const lineLabels = {
    months: (n: number) => t("bill.monthsN", { n }),
    half: (days: number) => t("bill.addonHalf", { days }),
    full: (days: number) => t("bill.addonFull", { days }),
    gap: (span: string) => t("bill.addonGap", { span }),
    extraSeats: (n: number, amount: string) => t("bill.addonExtraSeats", { n, amount }),
  };
  const lines: Line[] = [];
  if (o.kind === "seats") {
    lines.push({
      label: t("bill.seatsOrderLine", { seats: o.seats, days: o.prorated_days ?? 0 }),
      detail: t("bill.seatsOrderLineDetail", { price: formatPriceVND(o.unit_price_vnd) }),
      amount: o.proration_vnd ?? 0,
    });
  } else {
    if (o.kind !== "addon") {
      lines.push({
        label: t("bill.planLine", { name: o.plan_name }),
        detail:
          o.mode === "seat"
            ? t("bill.planLineDetailSeats", { length, seats: o.seats, price: formatPriceVND(o.unit_price_vnd) })
            : t("bill.planLineDetail", { length, price: formatPriceVND(o.unit_price_vnd) }),
        amount: orderPlanLineVND(o),
      });
    }
    if (o.proration_vnd) lines.push({ label: t("bill.prorationOrder", { days: o.prorated_days ?? 0 }), amount: o.proration_vnd });
  }
  for (const l of o.addons ?? []) {
    lines.push(
      o.kind === "seats"
        ? {
            label: t("bill.seatsAddonLine", { name: l.name || l.agent_id, n: l.extra_units ?? 0, date: formatLastDay(l.covered_until ?? null) }),
            detail: t("bill.seatsOrderLineDetail", { price: formatPriceVND(l.unit_price_vnd) }),
            amount: l.amount_vnd,
          }
        : { label: t("bill.addonLine", { name: l.name || l.agent_id }), detail: addonLineDetail(l, formatPriceVND, lineLabels), amount: l.amount_vnd },
    );
  }
  const totals: Line[] = [{ label: t("bill.subtotal"), amount: o.subtotal_vnd }];
  if (o.credit_vnd) totals.push({ label: t("bill.credit"), amount: o.credit_vnd, sign: "-" });
  if (o.discount_vnd > 0) totals.push({ label: `${t("bill.discount")}${o.promo_code ? ` (${o.promo_code})` : ""}`, amount: o.discount_vnd, sign: "-" });
  return { lines, totals };
}

export function OrderReceiptPage({ code }: { code: string }) {
  // t: on-screen UI (buttons, messages); tr: the receipt document
  const { t } = useT();
  const [receipt, setReceipt] = useState<UsageOrderReceipt | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let alive = true;
    getOrderReceipt(code)
      .then((r) => alive && setReceipt(r))
      .catch(() => alive && setFailed(true));
    return () => {
      alive = false;
    };
  }, [code]);

  useEffect(() => {
    if (receipt) document.title = `${tr("rcpt.title")} ${receipt.order.order_code}`;
  }, [receipt]);

  const o = receipt?.order ?? null;
  const { lines, totals } = orderLines(o);

  if (failed) {
    return (
      <div className="mx-auto max-w-[560px] px-5 py-16 text-center">
        <p className="text-[14px] text-ink">{t("rcpt.unavailable")}</p>
        <Link href={BILLING_PAGE} className="btn btn-outline btn-sm mt-4 inline-flex">
          ← {t("rcpt.back")}
        </Link>
      </div>
    );
  }
  if (!receipt || !o) return <Loading className="py-16" />;

  const r = receipt;
  const invoice = invoiceState(o);
  const sellerRows: [LocaleKey, string][] = (
    [
      ["rcpt.taxCode", r.seller_tax_code],
      ["rcpt.address", r.seller_address],
      ["rcpt.contact", r.seller_contact],
      ["rcpt.account", [r.bank_account_no, r.bank_name].filter(Boolean).join(" — ")],
    ] as [LocaleKey, string][]
  ).filter(([, v]) => v.trim() !== "");
  const buyerKind = o.scope_type === "tenant" ? tr("rcpt.buyerTenant") : tr("rcpt.buyerUser");

  return (
    <div className="min-h-dvh bg-canvas py-6 print:bg-white print:py-0">
      <style>{PRINT_CSS}</style>
      <div className="mx-auto mb-4 flex w-[210mm] max-w-full items-center justify-between gap-3 px-4 print:hidden">
        <Link href={BILLING_PAGE} className="btn btn-outline btn-sm">
          ← {t("rcpt.back")}
        </Link>
        <button type="button" className="btn btn-primary btn-sm" onClick={() => window.print()}>
          {t("rcpt.print")}
        </button>
      </div>

      <article
        lang={RECEIPT_LOCALE}
        className="receipt-sheet mx-auto w-[210mm] max-w-full min-h-[297mm] border border-neutral-200 bg-white px-[16mm] py-[14mm] text-[13px] leading-relaxed text-neutral-900 shadow-sm"
      >
        <header className="receipt-block flex flex-wrap items-start justify-between gap-6 border-b border-neutral-300 pb-4">
          <div className="min-w-0 max-w-[60%]">
            {r.seller_name && <div className="text-[15px] font-semibold uppercase">{r.seller_name}</div>}
            <dl className="mt-1 grid grid-cols-[max-content_1fr] gap-x-3 text-[12px]">
              {sellerRows.map(([k, v]) => (
                <div key={k} className="contents">
                  <dt className="text-neutral-500">{tr(k)}</dt>
                  <dd className="min-w-0 break-words">{v}</dd>
                </div>
              ))}
            </dl>
          </div>
          <div className="text-right">
            <h1 className="text-[19px] font-semibold uppercase tracking-wide">{tr("rcpt.title")}</h1>
            <div className="mt-1 text-[12px]">
              {tr("rcpt.number")}: <span className="font-mono font-semibold">{o.order_code}</span>
            </div>
            {o.paid_at && <div className="text-[12px] text-neutral-600">{formatDateTime(o.paid_at, RECEIPT_LOCALE)}</div>}
          </div>
        </header>

        <section className="receipt-block mt-5">
          <h2 className="text-[12px] font-semibold uppercase text-neutral-500">{tr("rcpt.buyer")}</h2>
          <div className="mt-1 font-medium">
            {r.buyer_name} <span className="font-normal text-neutral-500">({buyerKind})</span>
          </div>
          {r.buyer_email && (
            <div className="text-[12px]">
              {tr("rcpt.email")}: {r.buyer_email}
            </div>
          )}
          {invoice !== "none" && (
            <div className="mt-3 rounded border border-neutral-300 px-3 py-2">
              <div className="text-[12px] font-semibold text-neutral-600">{tr("rcpt.invoiceTo")}</div>
              <dl className="mt-1 grid grid-cols-[max-content_1fr] gap-x-3 text-[12.5px]">
                <dt className="text-neutral-500">{tr("rcpt.invoiceCompany")}</dt>
                <dd className="min-w-0 break-words">{o.invoice_company_name}</dd>
                <dt className="text-neutral-500">{tr("rcpt.taxCode")}</dt>
                <dd className="font-mono">{o.invoice_tax_code}</dd>
                <dt className="text-neutral-500">{tr("rcpt.address")}</dt>
                <dd className="min-w-0 break-words">{o.invoice_address}</dd>
                <dt className="text-neutral-500">{tr("rcpt.invoiceEmail")}</dt>
                <dd className="min-w-0 break-words">{o.invoice_email}</dd>
              </dl>
            </div>
          )}
        </section>

        <table className="mt-6 w-full border-collapse text-[12.5px]">
          <thead>
            <tr className="border-b border-neutral-400 text-left">
              <th className="py-1.5 pr-3 font-semibold">{tr("rcpt.items")}</th>
              <th className="w-[32mm] py-1.5 text-right font-semibold">{tr("rcpt.amount")}</th>
            </tr>
          </thead>
          <tbody>
            {lines.map((l, i) => (
              <tr key={i} className="border-b border-neutral-200 align-top">
                <td className="py-1.5 pr-3">
                  <div>{l.label}</div>
                  {l.detail && <div className="text-[11.5px] text-neutral-500">{l.detail}</div>}
                </td>
                <td className="py-1.5 text-right tabular-nums">{formatPriceVND(l.amount)}</td>
              </tr>
            ))}
            {totals.map((l, i) => (
              <tr key={`t${i}`} className="align-top">
                <td className="py-1 pr-3 text-right text-neutral-600">{l.label}</td>
                <td className="py-1 text-right tabular-nums">
                  {l.sign === "-" ? "−" : ""}
                  {formatPriceVND(l.amount)}
                </td>
              </tr>
            ))}
            <tr className="border-t border-neutral-400 align-top">
              <td className="py-2 pr-3 text-right font-semibold">{tr("rcpt.totalPaid")}</td>
              <td className="py-2 text-right text-[14px] font-semibold tabular-nums">{formatPriceVND(o.total_vnd)}</td>
            </tr>
          </tbody>
        </table>

        <dl className="receipt-block mt-6 grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-[12.5px]">
          {o.total_vnd > 0 ? (
            <>
              <dt className="text-neutral-500">{tr("rcpt.method")}</dt>
              <dd>
                {tr("rcpt.bankTransfer")}
                {/* the transfer content only when a transfer was matched to the order */}
                {o.last_payment && o.transfer_content ? <span className="text-neutral-500"> · {o.transfer_content}</span> : null}
              </dd>
            </>
          ) : (
            <dd className="col-span-2">{tr("rcpt.noPayment")}</dd>
          )}
          <dt className="text-neutral-500">{tr("rcpt.paidAt")}</dt>
          <dd>{formatDateTime(o.paid_at, RECEIPT_LOCALE)}</dd>
          <dt className="text-neutral-500">{tr(o.kind === "addon" || o.kind === "seats" ? "rcpt.packageCurrent" : "rcpt.package")}</dt>
          <dd>{o.plan_name}</dd>
          {o.subscription_ends_at && (
            <>
              <dt className="text-neutral-500">{tr("rcpt.lastDay")}</dt>
              <dd>{formatLastDay(o.subscription_ends_at)}</dd>
            </>
          )}
        </dl>

        <p className="receipt-block mt-8 border-t border-neutral-300 pt-3 text-[12px] italic text-neutral-600">
          {invoice === "issued" ? issuedInvoiceText(o, tr, "rcpt") : tr("rcpt.notInvoice")}
        </p>
      </article>
    </div>
  );
}
