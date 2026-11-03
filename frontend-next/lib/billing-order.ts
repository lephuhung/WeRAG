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
 * already has a plan order waiting for payment), null for anything else. */
export function pendingPlanOrderCode(details: { reason?: string; order_code?: string } | undefined): string | null {
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
