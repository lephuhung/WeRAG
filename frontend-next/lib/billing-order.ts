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
