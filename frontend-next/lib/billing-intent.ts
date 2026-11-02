/* The package the billing page should open on, handed over by a banner's
 * "Renew"/"Buy" button. Read by the next BillingPage mount, which then clears it. */

export type BillingIntent = { scope: "tenant" | "user"; planId: string };

export const BILLING_INTENT_EVENT = "werag:billing-intent";

/* Kept a few seconds rather than cleared on read: React's StrictMode runs a
 * state initializer twice, and the second read must still see it. */
const FRESH_MS = 3_000;
let pending: { intent: BillingIntent; at: number } | null = null;

export function setBillingIntent(intent: BillingIntent | null) {
  pending = intent ? { intent, at: Date.now() } : null;
}

export function readBillingIntent(): BillingIntent | null {
  return pending && Date.now() - pending.at <= FRESH_MS ? pending.intent : null;
}

export function clearBillingIntent() {
  pending = null;
}

/** Open the settings modal on the billing page (AccountMenu listens). */
export function openBilling(intent: BillingIntent | null = null) {
  setBillingIntent(intent);
  window.dispatchEvent(new CustomEvent("weknora:open-settings", { detail: "billing" }));
  // a billing page already on screen is not remounted: tell it directly
  if (intent) window.dispatchEvent(new CustomEvent<BillingIntent>(BILLING_INTENT_EVENT, { detail: intent }));
}
