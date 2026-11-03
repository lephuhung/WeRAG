"use client";

/* VAT invoice (hóa đơn GTGT) parts of the buy page: the details form a
 * buyer fills when ordering, and the order page's invoice panel (status,
 * details, edit until a SuperAdmin marks the invoice issued). */

import { useId, useState } from "react";
import { useT, type LocaleKey } from "@/lib/i18n";
import { FieldLabel } from "@/components/settings/field";
import { formatDate } from "@/lib/datetime";
import {
  EMPTY_INVOICE,
  invoiceEditable,
  invoiceNumberLabel,
  invoiceProblems,
  invoiceState,
  normalizeInvoice,
  type InvoiceDetails,
} from "@/lib/billing-invoice";
import type { UsageOrder } from "@/lib/api/usage";

/** The invoice part of an order request: the checkbox and, once ticked,
 * the four fields. `showErrors` shows what is wrong (after a try). */
export function InvoiceFields({
  requested,
  onRequestedChange,
  value,
  onChange,
  showCheckbox = true,
}: {
  requested: boolean;
  onRequestedChange?: (v: boolean) => void;
  value: InvoiceDetails;
  onChange: (v: InvoiceDetails) => void;
  showCheckbox?: boolean;
}) {
  const { t } = useT();
  const id = useId();
  // a field shows its problem once it was left (or typed in) once
  const [touched, setTouched] = useState<Partial<Record<keyof InvoiceDetails, boolean>>>({});
  const problems = invoiceProblems(value);
  const field = (key: keyof InvoiceDetails, label: LocaleKey, opts: { hint?: LocaleKey; type?: string; inputMode?: "numeric" | "email"; wide?: boolean } = {}) => {
    const err = touched[key] ? problems[key] : undefined;
    return (
      <div className={`min-w-0 ${opts.wide ? "sm:col-span-2" : ""}`}>
        <FieldLabel htmlFor={`${id}-${key}`} required>
          {t(label)}
        </FieldLabel>
        <input
          id={`${id}-${key}`}
          className="input w-full"
          type={opts.type ?? "text"}
          inputMode={opts.inputMode}
          aria-required="true"
          aria-invalid={err ? true : undefined}
          value={value[key]}
          onChange={(e) => onChange({ ...value, [key]: e.target.value })}
          onBlur={() => setTouched((cur) => ({ ...cur, [key]: true }))}
        />
        {err ? (
          <span className="caption mt-1 block text-error">{t(`bill.err.${err}` as LocaleKey)}</span>
        ) : opts.hint ? (
          <span className="caption mt-1 block text-muted-soft">{t(opts.hint)}</span>
        ) : null}
      </div>
    );
  };
  return (
    <div className="mt-4">
      {showCheckbox && (
        <label className="flex cursor-pointer items-center gap-2 select-none">
          <input type="checkbox" checked={requested} onChange={(e) => onRequestedChange?.(e.target.checked)} />
          <span className="text-[13.5px] text-ink">{t("bill.invoiceNeed")}</span>
        </label>
      )}
      {requested && (
        <div className="mt-2 rounded-md border border-hairline p-3">
          <p className="caption mb-3 text-muted">{t("bill.invoiceNeedHint")}</p>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            {field("company_name", "bill.invoiceCompany", { wide: true })}
            {field("tax_code", "bill.invoiceTaxCode", { hint: "bill.invoiceTaxCodeHint", inputMode: "numeric" })}
            {field("email", "bill.invoiceEmail", { type: "email", inputMode: "email" })}
            {field("address", "bill.invoiceAddress", { wide: true })}
          </div>
        </div>
      )}
    </div>
  );
}

/** The invoice part of an order request, or undefined when none was asked
 * for; null when asked for with details that are not valid yet. */
export function invoiceRequestPart(requested: boolean, value: InvoiceDetails): { invoice?: InvoiceDetails } | null {
  if (!requested) return {};
  if (Object.keys(invoiceProblems(value)).length > 0) return null;
  return { invoice: normalizeInvoice(value) };
}

export function orderInvoiceDetails(o: UsageOrder): InvoiceDetails {
  return {
    company_name: o.invoice_company_name ?? "",
    tax_code: o.invoice_tax_code ?? "",
    address: o.invoice_address ?? "",
    email: o.invoice_email ?? "",
  };
}

/** "Đã xuất hóa đơn GTGT số 1C26TAA 0000123 ngày 03/11/2026." */
export function issuedInvoiceText(o: UsageOrder, t: (k: LocaleKey, v?: Record<string, string | number>) => string, prefix: "bill" | "rcpt" = "bill"): string {
  const no = invoiceNumberLabel(o);
  const date = formatDate(o.invoice_date ?? o.invoice_issued_at ?? null);
  return no
    ? t(`${prefix}.invoiceIssuedNo` as LocaleKey, { no, date })
    : t(`${prefix}.invoiceIssued` as LocaleKey, { date });
}

/** The order page's invoice panel. `save` sends the request (requested
 * false withdraws it); `prefill` are the details to start from when none
 * were given on the order. */
export function OrderInvoicePanel({
  order,
  busy,
  prefill,
  save,
}: {
  order: UsageOrder;
  busy: boolean;
  prefill: InvoiceDetails | null;
  save: (requested: boolean, details?: InvoiceDetails) => Promise<boolean>;
}) {
  const { t } = useT();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<InvoiceDetails>(EMPTY_INVOICE);
  const state = invoiceState(order);
  const editable = invoiceEditable(order);
  if (state === "none" && !editable) return null;

  const open = () => {
    setDraft(order.invoice_requested ? orderInvoiceDetails(order) : (prefill ?? EMPTY_INVOICE));
    setEditing(true);
  };
  const part = invoiceRequestPart(true, draft);
  const d = orderInvoiceDetails(order);

  return (
    <div className="border-t border-hairline px-4 py-3 text-[14px]">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="font-medium text-ink">{t("bill.invoiceTitle")}</div>
        {editable && !editing && (
          <div className="flex gap-2">
            <button type="button" className="btn btn-outline btn-sm" disabled={busy} onClick={open}>
              {state === "none" ? t("bill.invoiceAsk") : t("common.edit")}
            </button>
            {state === "requested" && (
              <button type="button" className="btn btn-outline btn-sm" disabled={busy} onClick={() => void save(false)}>
                {t("bill.invoiceWithdraw")}
              </button>
            )}
          </div>
        )}
      </div>
      {!editing && (
        <>
          <p className={`caption mt-1 ${state === "issued" ? "text-success" : "text-muted"}`}>
            {state === "none"
              ? t("bill.invoiceNone")
              : state === "issued"
                ? issuedInvoiceText(order, t)
                : order.status === "paid"
                  ? t("bill.invoiceRequestedPaid")
                  : t("bill.invoiceRequestedPending")}
          </p>
          {state !== "none" && (
            <dl className="mt-2 grid grid-cols-[max-content_1fr] gap-x-4 gap-y-0.5 text-[13.5px]">
              <dt className="text-muted">{t("bill.invoiceCompany")}</dt>
              <dd className="min-w-0 break-words text-ink">{d.company_name}</dd>
              <dt className="text-muted">{t("bill.invoiceTaxCode")}</dt>
              <dd className="font-mono text-ink">{d.tax_code}</dd>
              <dt className="text-muted">{t("bill.invoiceAddress")}</dt>
              <dd className="min-w-0 break-words text-ink">{d.address}</dd>
              <dt className="text-muted">{t("bill.invoiceEmail")}</dt>
              <dd className="min-w-0 break-words text-ink">{d.email}</dd>
            </dl>
          )}
          {state === "issued" && <p className="caption mt-2 text-muted-soft">{t("bill.invoiceLocked")}</p>}
        </>
      )}
      {editing && (
        <>
          <InvoiceFields requested showCheckbox={false} value={draft} onChange={setDraft} />
          <div className="mt-3 flex justify-end gap-2">
            <button type="button" className="btn btn-outline btn-sm" disabled={busy} onClick={() => setEditing(false)}>
              {t("common.cancel")}
            </button>
            <button
              type="button"
              className="btn btn-primary btn-sm"
              disabled={busy || part === null}
              onClick={() =>
                void save(true, part?.invoice).then((ok) => {
                  if (ok) setEditing(false);
                })
              }
            >
              {t("common.save")}
            </button>
          </div>
        </>
      )}
    </div>
  );
}
