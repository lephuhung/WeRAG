/* Usage packages: what the caller and their current workspace used today
 * and this month (phase 1), the package that governs the caller's turns
 * (phase 2), and the SuperAdmin management of plans, subscriptions and
 * seats. Identity for /me/usage comes from the session; the endpoint cannot
 * be pointed at someone else. */
import { apiDel, apiGet, apiPost, apiPut } from "@/lib/api-client";

/** Questions and LLM tokens of one period (Vietnam calendar). */
export interface UsagePeriodTotals {
  /** YYYY-MM-DD for a day, YYYY-MM for a month. */
  period_key: string;
  questions: number;
  tokens: number;
  prompt_tokens: number;
  completion_tokens: number;
  /** Share of `tokens` spent reading chat attachments with the OCR model. */
  ocr_tokens?: number;
  /** Images and scanned pages read by the OCR model. */
  ocr_pages?: number;
}

/** Every workspace summed — mirrors types.UsageStats. */
export interface UsageStats {
  timezone: string;
  today: UsagePeriodTotals;
  month: UsagePeriodTotals;
}

export type UsageMetric = "questions" | "tokens";
export type UsagePeriod = "day" | "month";
export type UsageMode = "pool" | "seat";
export type UsageScopeKind = "tenant" | "user";

export interface UsageScopeSummary {
  scope_type: "tenant" | "user";
  scope_id: string;
  today: UsagePeriodTotals;
  month: UsagePeriodTotals;
}

/** One cap of a plan: `limit` 0 means unlimited. */
export interface UsageLimit {
  metric: UsageMetric;
  period: UsagePeriod;
  limit: number;
}

export interface UsageLimitStatus extends UsageLimit {
  period_key: string;
  used: number;
  remaining: number;
  exhausted: boolean;
}

/** Mirrors types.UsageEntitlement: the subscription governing the caller. */
export interface UsageEntitlement {
  source: "tenant" | "user";
  subscription_id: string;
  plan_id: string;
  plan_code: string;
  plan_name: string;
  mode: UsageMode;
  seats?: number;
  has_seat: boolean;
  counted_scope: "tenant" | "user" | "";
  ends_at?: string | null;
  expired: boolean;
  limits: UsageLimitStatus[];
}

/** Mirrors types.UsageSummary. Either scope is absent when the caller has no
 * such scope (no workspace selected, or a machine principal). */
export interface UsageSummary {
  timezone: string;
  user?: UsageScopeSummary;
  tenant?: UsageScopeSummary;
  enforced: boolean;
  entitlement?: UsageEntitlement | null;
}

/** GET /api/v1/me/usage */
export function getMyUsage(): Promise<{ data: UsageSummary }> {
  return apiGet("/api/v1/me/usage");
}

/** Compact token count for tiles: 1.2K, 3.4M. */
export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(n >= 10_000_000 ? 0 : 1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(n >= 10_000 ? 0 : 1)}K`;
  return n.toLocaleString();
}

/** "49000" / 49000 → "49.000 VNĐ"; non-numeric legacy labels are shown as-is. */
export function formatPriceVND(value: string | number): string {
  if (typeof value === "number") return `${Math.round(value).toLocaleString("vi-VN")} VNĐ`;
  const s = (value ?? "").trim();
  if (!s) return "";
  if (!/^\d+$/.test(s)) return s;
  return `${Number(s).toLocaleString("vi-VN")} VNĐ`;
}

/** A plan's price line: the amount, or "" when it is not sold online. */
export function planPrice(plan: Pick<UsagePlan, "price_vnd" | "price_label">): string {
  if (plan.price_vnd > 0) return formatPriceVND(plan.price_vnd);
  return formatPriceVND(plan.price_label);
}

// ---- SuperAdmin: plans, subscriptions, seats -----------------------------------

export interface UsagePlan {
  id: string;
  code: string;
  name: string;
  description: string;
  scope_kind: UsageScopeKind;
  mode: UsageMode;
  limits: UsageLimit[];
  features: Record<string, unknown>;
  price_label: string;
  /** List price per month in VND; per seat per month in seat mode. 0 = not sold online. */
  price_vnd: number;
  sort_order: number;
  is_active: boolean;
  created_at: string;
  updated_at: string;
  /** GET /billing/plans only: the basic agents the plan includes. */
  included_agents?: UsagePlanAgent[];
  /** GET /billing/plans only: specialist agents sold with the plan. */
  addons?: UsagePlanAddonOffer[];
}

export type UsagePlanInput = Omit<UsagePlan, "id" | "created_at" | "updated_at" | "included_agents" | "addons">;

export interface UsagePlanAgent {
  id: string;
  name: string;
}

export interface UsagePlanAddonOffer {
  agent_id: string;
  name: string;
  description: string;
  /** Per month; × seats when per_seat. */
  price_vnd: number;
  per_seat: boolean;
}

/** A row of the add-on catalogue (usage_agent_addons). */
export interface UsageAgentAddon {
  agent_id: string;
  /** Per month; per seat per month on a seat plan. */
  price_vnd: number;
  /** [] = sold with every plan. */
  plan_ids: string[];
  description: string;
  is_active: boolean;
  sort_order: number;
  created_at?: string;
  updated_at?: string;
  agent_name?: string;
  /** False once the agent is deleted or no longer a specialist agent. */
  agent_available?: boolean;
  /** Current subscriptions holding the add-on. */
  holders?: number;
}

export type UsageAgentAddonInput = Pick<
  UsageAgentAddon,
  "agent_id" | "price_vnd" | "plan_ids" | "description" | "is_active" | "sort_order"
>;

export interface UsageAgentAddonCatalogue {
  addons: UsageAgentAddon[];
  included_agents: UsagePlanAgent[];
}

export interface UsageSubscription {
  id: string;
  scope_type: "tenant" | "user";
  scope_id: string;
  plan_id: string;
  plan?: UsagePlan;
  mode: UsageMode;
  seats: number;
  limits_override: UsageLimit[] | null;
  starts_at: string;
  ends_at: string | null;
  status: "active" | "cancelled";
  note: string;
  created_at: string;
  updated_at: string;
}

export interface UsageSubscriptionInput {
  scope_type: "tenant" | "user";
  scope_id: string;
  plan_id: string;
  mode?: UsageMode;
  seats?: number;
  limits_override?: UsageLimit[] | null;
  starts_at?: string;
  ends_at?: string | null;
  status?: "active" | "cancelled";
  note?: string;
}

/** A subscription as the admin page shows it. */
export interface UsageSubscriptionView extends UsageSubscription {
  scope_name: string;
  current: boolean;
  limit_status: UsageLimitStatus[];
  seats_used: number;
  /* A Guest's guest plan before their first question: no row yet (id is
   * empty), created by the usage guard on that turn. */
  implicit?: boolean;
}

export interface UsageTenantOverview {
  tenant_id: number;
  tenant_name: string;
  subscription?: UsageSubscriptionView | null;
  month: UsagePeriodTotals;
  today: UsagePeriodTotals;
}

export interface UsageSeat {
  subscription_id: string;
  user_id: string;
  assigned_by: string;
  assigned_at: string;
}

const ADMIN = "/api/v1/system/admin/usage";

export function listAgentAddons(includeInactive = true): Promise<UsageAgentAddonCatalogue> {
  return apiGet<{ data?: UsageAgentAddonCatalogue }>(
    `${ADMIN}/agent-addons${includeInactive ? "" : "?include_inactive=false"}`,
  ).then((r) => ({ addons: r.data?.addons ?? [], included_agents: r.data?.included_agents ?? [] }));
}

export function createAgentAddon(input: UsageAgentAddonInput): Promise<UsageAgentAddon> {
  return apiPost<{ data: UsageAgentAddon }>(`${ADMIN}/agent-addons`, input).then((r) => r.data);
}

export function updateAgentAddon(input: UsageAgentAddonInput): Promise<UsageAgentAddon> {
  return apiPut<{ data: UsageAgentAddon }>(
    `${ADMIN}/agent-addons/${encodeURIComponent(input.agent_id)}`,
    input,
  ).then((r) => r.data);
}

export function deleteAgentAddon(agentId: string): Promise<void> {
  return apiDel(`${ADMIN}/agent-addons/${encodeURIComponent(agentId)}`).then(() => undefined);
}

export function listUsagePlans(includeInactive = true): Promise<UsagePlan[]> {
  return apiGet<{ data?: UsagePlan[] }>(`${ADMIN}/plans${includeInactive ? "?include_inactive=true" : ""}`).then(
    (r) => r.data ?? [],
  );
}

export function createUsagePlan(plan: UsagePlanInput): Promise<UsagePlan> {
  return apiPost<{ data: UsagePlan }>(`${ADMIN}/plans`, plan).then((r) => r.data);
}

export function updateUsagePlan(id: string, plan: UsagePlanInput): Promise<UsagePlan> {
  return apiPut<{ data: UsagePlan }>(`${ADMIN}/plans/${encodeURIComponent(id)}`, plan).then((r) => r.data);
}

export function deleteUsagePlan(id: string): Promise<void> {
  return apiDel(`${ADMIN}/plans/${encodeURIComponent(id)}`).then(() => undefined);
}

export function listUsageSubscriptions(params?: {
  scope_type?: "tenant" | "user";
  scope_id?: string;
  status?: "active" | "cancelled";
  plan_id?: string;
}): Promise<UsageSubscriptionView[]> {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(params ?? {})) if (v) qs.set(k, v);
  const suffix = qs.toString() ? `?${qs.toString()}` : "";
  return apiGet<{ data?: UsageSubscriptionView[] }>(`${ADMIN}/subscriptions${suffix}`).then((r) => r.data ?? []);
}

export function createUsageSubscription(input: UsageSubscriptionInput): Promise<UsageSubscriptionView> {
  return apiPost<{ data: UsageSubscriptionView }>(`${ADMIN}/subscriptions`, input).then((r) => r.data);
}

export function updateUsageSubscription(id: string, input: UsageSubscriptionInput): Promise<UsageSubscriptionView> {
  return apiPut<{ data: UsageSubscriptionView }>(`${ADMIN}/subscriptions/${encodeURIComponent(id)}`, input).then(
    (r) => r.data,
  );
}

export function deleteUsageSubscription(id: string): Promise<void> {
  return apiDel(`${ADMIN}/subscriptions/${encodeURIComponent(id)}`).then(() => undefined);
}

export function listUsageTenantOverview(): Promise<UsageTenantOverview[]> {
  return apiGet<{ data?: UsageTenantOverview[] }>(`${ADMIN}/tenants`).then((r) => r.data ?? []);
}

export function listUsageSeats(subscriptionId: string): Promise<UsageSeat[]> {
  return apiGet<{ data?: UsageSeat[] }>(`${ADMIN}/subscriptions/${encodeURIComponent(subscriptionId)}/seats`).then(
    (r) => r.data ?? [],
  );
}

export function assignUsageSeat(subscriptionId: string, userId: string): Promise<void> {
  return apiPost(`${ADMIN}/subscriptions/${encodeURIComponent(subscriptionId)}/seats`, { user_id: userId }).then(
    () => undefined,
  );
}

export function revokeUsageSeat(subscriptionId: string, userId: string): Promise<void> {
  return apiDel(
    `${ADMIN}/subscriptions/${encodeURIComponent(subscriptionId)}/seats/${encodeURIComponent(userId)}`,
  ).then(() => undefined);
}

/** system_settings key of the enforcement switch. */
export const USAGE_ENFORCE_SETTING_KEY = "usage.enforce";

// ---- workspace-facing (every member reads; Tenant Admin manages seats) ------

/** Mirrors types.UsageTenantUsage. */
export interface UsageTenantUsage {
  enforced: boolean;
  subscription?: UsageSubscriptionView | null;
  seats: UsageSeat[];
  /** Specialist agents the subscription holds, with their end. */
  addons?: UsageSubscriptionAddon[];
}

/** A right of a subscription to a specialist agent. */
export interface UsageSubscriptionAddon {
  subscription_id: string;
  agent_id: string;
  agent_name?: string;
  starts_at: string;
  ends_at: string | null;
  price_vnd: number;
  order_id: string | null;
}

/** GET /billing/subscription: the scope's current subscription, the
 * add-ons it holds and those it may still buy on it. */
export interface UsageScopeAddons {
  subscription?: UsageSubscription | null;
  held: UsageSubscriptionAddon[];
  purchasable: UsagePlanAddonOffer[];
}

export function getTenantUsage(tenantId: number | string): Promise<UsageTenantUsage> {
  return apiGet<{ data: UsageTenantUsage }>(`/api/v1/tenants/${tenantId}/usage`).then((r) => r.data);
}

export function assignTenantSeat(tenantId: number | string, userId: string): Promise<void> {
  return apiPost(`/api/v1/tenants/${tenantId}/usage/seats`, { user_id: userId }).then(() => undefined);
}

export function revokeTenantSeat(tenantId: number | string, userId: string): Promise<void> {
  return apiDel(`/api/v1/tenants/${tenantId}/usage/seats/${encodeURIComponent(userId)}`).then(() => undefined);
}

/** Label of one limit for lists: "500 questions/month", "∞ tokens/day". */
export function usageLimitLabel(
  l: UsageLimit,
  names: { questions: string; tokens: string; day: string; month: string },
): string {
  const n = l.limit > 0 ? (l.metric === "tokens" ? formatTokens(l.limit) : l.limit.toLocaleString()) : "∞";
  return `${n} ${l.metric === "tokens" ? names.tokens : names.questions}/${l.period === "day" ? names.day : names.month}`;
}

// ---- billing: promo codes, orders, bank transfer -----------------------------

export type UsagePromoDiscountType = "percent" | "amount";
export type UsageOrderStatus = "pending" | "paid" | "cancelled" | "expired";

/** Mirrors types.UsagePromoCode. */
export interface UsagePromoCode {
  id: string;
  code: string;
  name: string;
  discount_type: UsagePromoDiscountType;
  discount_value: number;
  /** Empty = every plan. */
  plan_ids: string[];
  /** 0 = unlimited. */
  max_uses: number;
  used_count: number;
  starts_at: string;
  ends_at: string | null;
  is_active: boolean;
  note: string;
  created_at: string;
  updated_at: string;
}

export type UsagePromoInput = Omit<UsagePromoCode, "id" | "used_count" | "created_at" | "updated_at">;

/** Mirrors types.UsageOrder + UsageOrderView. */
export interface UsageOrder {
  id: string;
  order_code: string;
  scope_type: UsageScopeKind;
  scope_id: string;
  scope_name: string;
  plan_id: string;
  plan_name: string;
  mode: UsageMode;
  seats: number;
  months: number;
  unit_price_vnd: number;
  subtotal_vnd: number;
  promo_code: string;
  discount_vnd: number;
  total_vnd: number;
  status: UsageOrderStatus;
  payment_method: string;
  created_by: string;
  confirmed_by: string;
  paid_at: string | null;
  expires_at: string | null;
  subscription_id: string | null;
  note: string;
  /** "plan" (a plan, optional add-ons) or "addon" (add-ons on the current subscription). */
  kind?: UsageOrderKind;
  target_subscription_id?: string | null;
  addons?: UsageOrderAddonLine[];
  created_at: string;
  updated_at: string;
  /** What to write in the bank transfer so the payment matches the order. */
  transfer_content: string;
  /** VietQR image for a pending order; "" when billing is not configured. */
  qr_url: string;
}

export type UsageOrderKind = "plan" | "addon";

/** One add-on of a quote/order, priced when ordered. */
export interface UsageOrderAddonLine {
  agent_id: string;
  name: string;
  unit_price_vnd: number;
  /** Seats on a seat plan, else 1. */
  units: number;
  months: number;
  /** Mid-term purchase: days after the whole months, at half price when ≤ 15. */
  leftover_days?: number;
  leftover_factor?: "half" | "full" | "";
  amount_vnd: number;
  paid_until?: string | null;
  /** Already held to this date (renewal, plan change): only later time is charged. */
  covered_until?: string | null;
  /** Renewal: the part of the current period not covered yet. */
  gap_months?: number;
  gap_leftover_days?: number;
  gap_leftover_factor?: "half" | "full" | "";
  gap_vnd?: number;
  /** Seats added on a held right, for the time it still runs. */
  extra_units?: number;
  extra_units_vnd?: number;
  /** A held add-on not bought again: only the added seats are charged. */
  seats_only?: boolean;
  /** Renewal: the subscription end the line was priced against. */
  subscription_end?: string | null;
}

export interface UsageQuoteRequest {
  scope_type: UsageScopeKind;
  plan_id: string;
  months: number;
  seats?: number;
  promo_code?: string;
  note?: string;
  kind?: UsageOrderKind;
  addon_agent_ids?: string[];
}

/** Mirrors types.UsageQuote. promo_error is a reason label (see promoErrorKey). */
export interface UsageQuote {
  plan: UsagePlan;
  mode: UsageMode;
  months: number;
  seats: number;
  unit_price_vnd: number;
  /** Seats of the subscription this order renews; added seats also pay
   * proration_vnd for the prorated_days left on its period. */
  current_seats?: number;
  prorated_days?: number;
  proration_vnd?: number;
  subtotal_vnd: number;
  promo_code: string;
  promo_name: string;
  promo_error: string;
  discount_vnd: number;
  total_vnd: number;
  kind?: UsageOrderKind;
  target_subscription_id?: string;
  addons?: UsageOrderAddonLine[];
  addons_vnd?: number;
}

/** Mirrors types.UsageBillingInfo. */
export interface UsageBillingInfo {
  enabled: boolean;
  bank_bin: string;
  bank_name: string;
  bank_account_no: string;
  bank_account_name: string;
  order_ttl_hours: number;
  contact: string;
}

const BILLING = "/api/v1/billing";

export function getBillingInfo(): Promise<UsageBillingInfo> {
  return apiGet<{ data: UsageBillingInfo }>(`${BILLING}/info`).then((r) => r.data);
}

export function getScopeAddons(scope: UsageScopeKind): Promise<UsageScopeAddons> {
  return apiGet<{ data?: UsageScopeAddons }>(`${BILLING}/subscription?scope=${scope}`).then((r) => ({
    subscription: r.data?.subscription ?? null,
    held: r.data?.held ?? [],
    purchasable: r.data?.purchasable ?? [],
  }));
}

export function listPurchasablePlans(scope: UsageScopeKind): Promise<UsagePlan[]> {
  return apiGet<{ data?: UsagePlan[] }>(`${BILLING}/plans?scope=${scope}`).then((r) => r.data ?? []);
}

/** A plan's per-turn and per-session caps (usage_plans.features, known keys
 * only); an absent key is no cap. */
export interface UsagePlanCaps {
  "agent.turn_token_cap"?: number;
  "agent.max_iterations"?: number;
  "context.history_tokens"?: number;
  "session.max_turns"?: number;
  "session.max_tokens"?: number;
}

export type PublicUsagePlan = Pick<UsagePlan, "id" | "name" | "description" | "scope_kind" | "mode" | "limits" | "price_vnd" | "price_label" | "sort_order" | "included_agents" | "addons"> & { caps?: UsagePlanCaps };

// Anonymous landing-page catalogue: plain fetch, no tokens and no refresh-on-401.
export async function listPublicPlans(scope: UsageScopeKind, signal?: AbortSignal): Promise<PublicUsagePlan[]> {
  const response = await fetch(`/api/v1/public/billing/plans?scope=${scope}`, { signal, credentials: "omit", cache: "no-store" });
  if (!response.ok) throw new Error(`Unable to load plans (${response.status})`);
  const result = await response.json();
  if (result.success !== true || !Array.isArray(result.data)) throw new Error("Invalid catalogue");
  return (result.data as PublicUsagePlan[]).sort((a, b) => a.sort_order - b.sort_order);
}

export function quoteUsageOrder(req: UsageQuoteRequest): Promise<UsageQuote> {
  return apiPost<{ data: UsageQuote }>(`${BILLING}/quote`, req).then((r) => r.data);
}

export function createUsageOrder(req: UsageQuoteRequest): Promise<UsageOrder> {
  return apiPost<{ data: UsageOrder }>(`${BILLING}/orders`, req).then((r) => r.data);
}

export function listMyUsageOrders(): Promise<UsageOrder[]> {
  return apiGet<{ data?: UsageOrder[] }>(`${BILLING}/orders`).then((r) => r.data ?? []);
}

export function getUsageOrder(code: string): Promise<UsageOrder> {
  return apiGet<{ data: UsageOrder }>(`${BILLING}/orders/${encodeURIComponent(code)}`).then((r) => r.data);
}

export function cancelMyUsageOrder(code: string): Promise<UsageOrder> {
  return apiPost<{ data: UsageOrder }>(`${BILLING}/orders/${encodeURIComponent(code)}/cancel`, {}).then((r) => r.data);
}

// SuperAdmin side.

export function listUsagePromos(includeInactive = true): Promise<UsagePromoCode[]> {
  return apiGet<{ data?: UsagePromoCode[] }>(`${ADMIN}/promos${includeInactive ? "?include_inactive=true" : ""}`).then(
    (r) => r.data ?? [],
  );
}

export function createUsagePromo(input: UsagePromoInput): Promise<UsagePromoCode> {
  return apiPost<{ data: UsagePromoCode }>(`${ADMIN}/promos`, input).then((r) => r.data);
}

export function updateUsagePromo(id: string, input: UsagePromoInput): Promise<UsagePromoCode> {
  return apiPut<{ data: UsagePromoCode }>(`${ADMIN}/promos/${encodeURIComponent(id)}`, input).then((r) => r.data);
}

export function deleteUsagePromo(id: string): Promise<void> {
  return apiDel(`${ADMIN}/promos/${encodeURIComponent(id)}`).then(() => undefined);
}

export function listAllUsageOrders(status?: UsageOrderStatus): Promise<UsageOrder[]> {
  return apiGet<{ data?: UsageOrder[] }>(`${ADMIN}/orders${status ? `?status=${status}` : ""}`).then((r) => r.data ?? []);
}

export function confirmUsageOrder(id: string): Promise<UsageOrder> {
  return apiPost<{ data: UsageOrder }>(`${ADMIN}/orders/${encodeURIComponent(id)}/confirm`, {}).then((r) => r.data);
}

export function cancelUsageOrder(id: string): Promise<UsageOrder> {
  return apiPost<{ data: UsageOrder }>(`${ADMIN}/orders/${encodeURIComponent(id)}/cancel`, {}).then((r) => r.data);
}

/** system_settings keys of the receiving bank account and order policy. */
export const BILLING_SETTING_KEYS = {
  bankBin: "billing.bank_bin",
  bankName: "billing.bank_name",
  accountNo: "billing.bank_account_no",
  accountName: "billing.bank_account_name",
  orderTTLHours: "billing.order_ttl_hours",
  contact: "billing.contact",
  /** Secret of the payment checker; reads come back masked. */
  webhookSecret: "billing.webhook_secret",
} as const;

/** i18n key for a quote's promo_error. */
export function promoErrorKey(reason: string): string {
  switch (reason) {
    case "promo_not_found":
    case "promo_inactive":
    case "promo_not_started":
    case "promo_expired":
    case "promo_exhausted":
    case "promo_wrong_plan":
      return `bill.${reason}`;
    default:
      return "bill.promo_not_found";
  }
}
