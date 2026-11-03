/* Arithmetic of the order page, kept apart from the component so it can be
 * tested: the plan line, what is left to transfer, and the "already have a
 * pending order" refusal. */

import type { UsageOrder } from "./api/usage.ts";

/** The plan line of an order: its subtotal less the prorated extra seats
 * and the add-on lines (both are shown on their own lines). */
export function orderPlanLineVND(o: Pick<UsageOrder, "subtotal_vnd" | "proration_vnd" | "addons">): number {
  const addons = (o.addons ?? []).reduce((sum, l) => sum + l.amount_vnd, 0);
  return Math.max(0, o.subtotal_vnd - (o.proration_vnd ?? 0) - addons);
}

/** What is still to transfer on a pending order: the total less short
 * transfers already received. */
export function orderAmountDue(o: Pick<UsageOrder, "total_vnd" | "received_vnd">): number {
  return Math.max(0, o.total_vnd - (o.received_vnd ?? 0));
}

/** The order code of a refusal with reason plan_order_pending (the scope
 * already has a plan order waiting for payment) that the buyer may
 * override by confirming, null for anything else. A seats order (or
 * reopening one) has no override (details.no_override): null, so the page
 * shows the refusal instead of asking. */
export function pendingPlanOrderCode(
  details: { reason?: string; order_code?: string; no_override?: string | boolean } | undefined,
): string | null {
  if (details?.no_override === true || details?.no_override === "true") return null;
  return details?.reason === "plan_order_pending" ? (details.order_code ?? "") : null;
}

/** A pending order whose money has arrived but did not confirm it (the
 * transfer was refused at confirmation, or what came adds up to the total
 * across checkers): nothing more to transfer, a SuperAdmin handles it. */
export function orderPaymentHeld(o: Pick<UsageOrder, "total_vnd" | "received_vnd" | "last_payment">): boolean {
  if (o.last_payment?.status === "confirm_failed") return true;
  const received = o.received_vnd ?? 0;
  return received > 0 && received >= o.total_vnd;
}

/** The plan name to put after the locale's own word for "package" in a
 * sentence ("Gói {plan}", "gói {plan}"): a name that already starts with
 * that word ("Gói cơ bản") loses it, so the sentence does not read
 * "Gói Gói cơ bản". Case-insensitive; a name that is only the word stays. */
export function planNameAfterWord(name: string | null | undefined, word: string): string {
  const n = (name ?? "").trim();
  const w = word.trim();
  if (!w) return n;
  const head = n.slice(0, w.length);
  const rest = n.slice(w.length);
  if (head.toLocaleLowerCase("vi") === w.toLocaleLowerCase("vi") && /^\s+\S/.test(rest)) return rest.trim();
  return n;
}

const VIETNAMESE_LETTERS = /[ăâđêôơưàáảãạằắẳẵặầấẩẫậèéẻẽẹềếểễệìíỉĩịòóỏõọồốổỗộờớởỡợùúủũụừứửữựỳýỷỹỵ]/i;

/** The server's own message for a billing refusal that has no locale key,
 * when it can be shown as is: a 4xx written in the reader's language.
 * Server errors (5xx) and messages in the other language give null; the
 * page then shows its generic "try again or contact the administrator". */
export function billingServerMessage(message: string | null | undefined, status: number | undefined, locale: string): string | null {
  const m = (message ?? "").trim();
  if (!m || status === undefined || status >= 500 || status < 400) return null;
  const vietnamese = VIETNAMESE_LETTERS.test(m);
  return (locale === "vi") === vietnamese ? m : null;
}

/** Whether retrying the same request can help: not after a 409 (the order
 * or the package is in a state that a retry does not change). */
export function billingErrorRetryable(status: number | undefined): boolean {
  return status !== 409;
}

/** The plan the shop opens on: one picked on purpose (a banner's intent, a
 * click) while still sold; else the plan in use when it is sold, so the
 * page opens on a renewal rather than a plan change; else the current
 * choice, else the first plan. */
export function preferredPlanId(planIds: string[], cur: string, currentPlanId: string, picked: boolean): string {
  if (picked && planIds.includes(cur)) return cur;
  if (currentPlanId && planIds.includes(currentPlanId)) return currentPlanId;
  if (planIds.includes(cur)) return cur;
  return planIds[0] ?? "";
}

/** The seat count the shop starts from on a Tổ chức seat plan: the seats
 * of the current seat subscription (a renewal may not buy fewer, and a
 * seat→seat change keeps the members' seats), or null to leave the box as
 * it is (no current seat subscription, or not a seat plan). */
export function defaultPlanSeats(opts: {
  scope: string;
  planMode: string | undefined;
  currentMode: string | undefined;
  currentSeats: number | undefined;
}): number | null {
  if (opts.scope !== "tenant" || opts.planMode !== "seat" || opts.currentMode !== "seat") return null;
  return Math.max(1, Math.floor(opts.currentSeats ?? 0));
}

/** The add-ons ticked as purchases after the plan choice changed. Renewing
 * the plan in use ticks the agents the scope holds, so they do not stop
 * at the old end unnoticed; a plan change leaves the held ones to the
 * "keep" choice (they carry over to their paid end) instead of buying them
 * again, which the server refuses while they are covered (addon_held). */
export function addonIdsForPlan(cur: string[], offered: string[], held: string[], renewing: boolean): string[] {
  const ok = new Set(offered);
  const kept = cur.filter((id) => ok.has(id));
  if (renewing) return [...new Set([...kept, ...held])].filter((id) => ok.has(id));
  const isHeld = new Set(held);
  return kept.filter((id) => !isHeld.has(id));
}
