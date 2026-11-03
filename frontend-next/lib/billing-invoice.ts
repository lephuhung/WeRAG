/* VAT invoice details of an order (hóa đơn GTGT), checked as the server
 * checks them (types.UsageInvoiceDetails.Normalize) so the form can say
 * what is wrong before the order is placed. */

export interface InvoiceDetails {
  company_name: string;
  tax_code: string;
  address: string;
  email: string;
}

export const EMPTY_INVOICE: InvoiceDetails = { company_name: "", tax_code: "", address: "", email: "" };

/** The reasons the server gives for refused details (details.reason). */
export type InvoiceProblem =
  | "invoice_company_required"
  | "invoice_tax_code_invalid"
  | "invoice_address_required"
  | "invoice_email_invalid";

/** A tax code without the spaces people type in it ("0101 234 567 - 001"). */
export function normalizeTaxCode(s: string): string {
  return s.replace(/\s+/g, "");
}

/** A Vietnamese tax code (mã số thuế): 10 digits, or 10 digits, "-" and 3
 * digits for a branch / dependent unit. Spaces are ignored. */
export function validTaxCode(s: string): boolean {
  return /^[0-9]{10}(-[0-9]{3})?$/.test(normalizeTaxCode(s));
}

/** One bare email address (no display name, no list). */
export function validInvoiceEmail(s: string): boolean {
  const v = s.trim();
  return /^[^\s@<>,;"]+@[^\s@<>,;"]+\.[^\s@<>,;".]+$/.test(v);
}

/** The details cleaned as the server stores them. */
export function normalizeInvoice(d: InvoiceDetails): InvoiceDetails {
  const collapse = (s: string) => s.trim().replace(/\s+/g, " ");
  return {
    company_name: collapse(d.company_name),
    tax_code: normalizeTaxCode(d.tax_code),
    address: collapse(d.address),
    email: d.email.trim(),
  };
}

/** What is wrong with the details, field by field (empty = fine). */
export function invoiceProblems(d: InvoiceDetails): Partial<Record<keyof InvoiceDetails, InvoiceProblem>> {
  const n = normalizeInvoice(d);
  const out: Partial<Record<keyof InvoiceDetails, InvoiceProblem>> = {};
  if (!n.company_name) out.company_name = "invoice_company_required";
  if (!validTaxCode(n.tax_code)) out.tax_code = "invoice_tax_code_invalid";
  if (!n.address) out.address = "invoice_address_required";
  if (!validInvoiceEmail(n.email)) out.email = "invoice_email_invalid";
  return out;
}

/** Where an order's invoice stands. */
export type InvoiceState = "none" | "requested" | "issued";

export function invoiceState(o: { invoice_requested?: boolean; invoice_issued_at?: string | null }): InvoiceState {
  if (!o.invoice_requested) return "none";
  return o.invoice_issued_at ? "issued" : "requested";
}

/** The buyer may still change the invoice: a pending or paid order whose
 * invoice was not marked issued. */
export function invoiceEditable(o: { status: string; invoice_issued_at?: string | null }): boolean {
  return (o.status === "pending" || o.status === "paid") && !o.invoice_issued_at;
}

/** "1C26TAA – số 0000123" style label of an issued invoice ("" when no
 * number was recorded). */
export function invoiceNumberLabel(o: { invoice_series?: string; invoice_no?: string }): string {
  return [o.invoice_series ?? "", o.invoice_no ?? ""].filter((x) => x.trim() !== "").join(" ");
}
