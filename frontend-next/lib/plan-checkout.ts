/* The links that carry a plan picked on the public pricing table through
 * sign-up / sign-in to the shop. The shop is a section of the settings
 * modal, so it is reached through the /platform/system deep link (the
 * catch-all page queues the section and its query for the modal). */

export type CheckoutScope = "user" | "tenant";

const BILLING_DEEP_LINK = "/platform/system/workspace/billing";
const MAX_PLAN_ID = 64;

/** The shop, opened on `planId` for `scope`. */
export function planCheckoutPath(scope: CheckoutScope, planId: string): string {
  const q = new URLSearchParams({ scope, plan: planId });
  return `${BILLING_DEEP_LINK}?${q.toString()}`;
}

/** `base` (/register, /login, /onboarding/workspace) carrying `next`, or
 * `base` alone when there is nothing to carry. */
export function withNext(base: string, next: string | null | undefined): string {
  return next ? `${base}?next=${encodeURIComponent(next)}` : base;
}

/** The scope and plan of a shop deep link's query ("?scope=user&plan=…"),
 * or null when it names no valid pair. */
export function checkoutFromSearch(search: string | URLSearchParams): { scope: CheckoutScope; planId: string } | null {
  const q = typeof search === "string" ? new URLSearchParams(search) : search;
  const scope = q.get("scope");
  const planId = (q.get("plan") ?? "").trim();
  if ((scope !== "user" && scope !== "tenant") || !planId || planId.length > MAX_PLAN_ID) return null;
  return { scope, planId };
}
