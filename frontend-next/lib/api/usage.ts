/* Usage packages: what the caller and their current workspace used today
 * and this month (phase 1), the package that governs the caller's turns
 * (phase 2), and the SuperAdmin management of plans, subscriptions and
 * seats. Identity for /me/usage comes from the session; the endpoint cannot
 * be pointed at someone else. */
import { apiDel, apiGet, apiPost, apiPut } from "@/lib/api-client";
import type { PlanCaps } from "@/lib/plan-caps";
import type { InvoiceDetails } from "@/lib/billing-invoice";

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
  /**
   * Share of `prompt_tokens` a provider's prompt cache served, and the rest.
   * Both absent when the provider reports no cache figures (which is not the
   * same as "nothing was cached").
   */
  cache_read_tokens?: number;
  cache_miss_tokens?: number;
  /**
   * What these calls cost the deployment, in millionths of a đồng, priced at
   * the call with the model's tariff and the peak/off-peak hour. SuperAdmin
   * only: it is the operator's spend, not the tenant's consumption.
   */
  cost_micro_vnd?: number;
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
  /** expired because it starts later (starts_at), not because it ended */
  not_started?: boolean;
  starts_at?: string | null;
  limits: UsageLimitStatus[];
  /** The Tổ chức package that lapsed while the caller's own package stands in. */
  lapsed_tenant?: { subscription_id: string; plan_id: string; plan_name: string; ends_at?: string | null; not_started?: boolean } | null;
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

/**
 * Millionths of a đồng → "1.234 VNĐ". Calls cost fractions of a đồng and are
 * stored scaled by 1e6 (see UsagePeriodTotals.cost_micro_vnd); a month of them
 * is ordinary money.
 */
export function formatMicroVND(microVND: number | undefined): string {
  if (!microVND) return "0 VNĐ";
  return formatPriceVND(microVND / 1e6);
}

/**
 * Prompt-cache hit rate of a period, or null when the provider reports no cache
 * figures at all (both counters absent), which must not render as 0%.
 */
export function cacheHitRate(totals: UsagePeriodTotals | undefined): number | null {
  if (!totals) return null;
  const read = totals.cache_read_tokens ?? 0;
  const miss = totals.cache_miss_tokens ?? 0;
  const prompt = read + miss;
  if (prompt <= 0) return null;
  return Math.round((read / prompt) * 100);
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

export type UsageBillingTerm = "month" | "quarter" | "year";

type TermPriced = Pick<UsagePlan, "price_vnd"> & Partial<Pick<UsagePlan, "price_quarter_vnd" | "price_year_vnd">>;

/** True when the plan has a price for at least one term (types.UsagePlan.SoldOnline). */
export function planSoldOnline(plan: TermPriced): boolean {
  return plan.price_vnd > 0 || (plan.price_quarter_vnd ?? 0) > 0 || (plan.price_year_vnd ?? 0) > 0;
}

/** Mirrors types.UsagePlan.PriceForMonths: whole years at the year price,
 * whole quarters at the quarter price, otherwise months at the monthly
 * price; null when no priced term fits. Per seat in seat mode. */
export function planPriceForMonths(plan: TermPriced, months: number): { total: number; term: UsageBillingTerm } | null {
  if (months <= 0) return null;
  const year = plan.price_year_vnd ?? 0;
  const quarter = plan.price_quarter_vnd ?? 0;
  if (year > 0 && months % 12 === 0) return { total: year * (months / 12), term: "year" };
  if (quarter > 0 && months % 3 === 0) return { total: quarter * (months / 3), term: "quarter" };
  if (plan.price_vnd > 0) return { total: plan.price_vnd * months, term: "month" };
  return null;
}

/** The term prices a plan lists (month, quarter, year), each with what it
 * saves against paying month by month. */
export function planTermPrices(plan: TermPriced): { term: UsageBillingTerm; months: number; price: number; saving: number }[] {
  const out: { term: UsageBillingTerm; months: number; price: number; saving: number }[] = [];
  const rows: [UsageBillingTerm, number, number][] = [
    ["month", 1, plan.price_vnd],
    ["quarter", 3, plan.price_quarter_vnd ?? 0],
    ["year", 12, plan.price_year_vnd ?? 0],
  ];
  for (const [term, months, price] of rows) {
    if (price <= 0) continue;
    const full = plan.price_vnd > 0 ? plan.price_vnd * months : 0;
    out.push({ term, months, price, saving: full > price ? full - price : 0 });
  }
  return out;
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
  /** Price of 3 months / 12 months (per seat in seat mode). 0 = that term
   * is charged at the monthly price. */
  price_quarter_vnd: number;
  price_year_vnd: number;
  sort_order: number;
  is_active: boolean;
  /** Specialist (addon) agents the plan includes: every current subscriber
   * uses them without buying them. */
  included_agent_ids: string[];
  created_at: string;
  updated_at: string;
  /** GET /billing/plans only: the basic agents plus the specialist agents
   * the plan includes (those flagged `included`). */
  included_agents?: UsagePlanAgent[];
  /** GET /billing/plans only: specialist agents sold with the plan. */
  addons?: UsagePlanAddonOffer[];
}

export type UsagePlanInput = Omit<UsagePlan, "id" | "created_at" | "updated_at" | "included_agents" | "addons">;

export interface UsagePlanAgent {
  id: string;
  name: string;
  /** A specialist agent the plan includes (not a basic one). */
  included?: boolean;
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
  /** Every live addon agent, sold or not: what a plan may include. */
  addon_agents: UsagePlanAgent[];
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
  /** Not current because it starts later, not because it ended. */
  not_started?: boolean;
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
  ).then((r) => ({
    addons: r.data?.addons ?? [],
    included_agents: r.data?.included_agents ?? [],
    addon_agents: r.data?.addon_agents ?? [],
  }));
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
/** system_settings keys of the package every new account receives:
 * a personal plan id ("" = none) and how many days it lasts (0 = no end). */
export const USAGE_SIGNUP_PLAN_SETTING_KEY = "usage.signup_plan_id";
export const USAGE_SIGNUP_PLAN_DAYS_SETTING_KEY = "usage.signup_plan_days";

// ---- workspace-facing (every member reads; Tenant Admin manages seats) ------

/** Mirrors types.UsageTenantUsage. */
export interface UsageTenantUsage {
  enforced: boolean;
  subscription?: UsageSubscriptionView | null;
  seats: UsageSeat[];
  /** Specialist agents the subscription holds, with their end. */
  addons?: UsageSubscriptionAddon[];
  /** Add-ons whose right ended in the last 7 days while the package goes on. */
  ended_addons?: UsageSubscriptionAddon[];
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
  /** Organization usage view: included in the plan, not bought (no end of its own). */
  included?: boolean;
}

/** GET /billing/subscription: the scope's current subscription, the
 * add-ons it holds and those it may still buy on it. */
export interface UsageScopeAddons {
  subscription?: UsageSubscription | null;
  held: UsageSubscriptionAddon[];
  /** Specialist agents the subscription's plan includes (no end of their own). */
  included: UsagePlanAgent[];
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

// ---- per-member report (Tenant Admin of the organization, SuperAdmin) -------

/** One person's line of the member report — mirrors types.UsageMemberUsage. */
export interface UsageMemberUsage {
  user_id: string;
  username: string;
  email: string;
  /** Role today; absent for someone who used the organization but is no longer a member. */
  role?: string;
  member: boolean;
  /** Only when the organization's package is in seat mode. */
  has_seat?: boolean;
  personal_plan?: string;
  usage: UsagePeriodTotals;
}

/** GET /tenants/:id/usage/members — mirrors types.UsageMemberReport. */
export interface UsageMemberReport {
  tenant_id: number;
  /** YYYY-MM, Vietnam calendar. */
  month: string;
  timezone: string;
  /** "counters": exact; "messages": month before per-member counting, summed
   * from chat history (questions and chat tokens only, no OCR, no cost). */
  source: "counters" | "messages";
  /** cost_micro_vnd is filled in (SuperAdmin only). */
  cost_visible: boolean;
  subscription?: UsageSubscriptionView | null;
  /** Sorted by tokens, descending. */
  members: UsageMemberUsage[];
  /** The organization counter: members + IM/API + anything before member counting. */
  total: UsagePeriodTotals;
  members_total: UsagePeriodTotals;
  unattributed: UsagePeriodTotals;
}

export function getTenantMemberUsage(tenantId: number | string, month: string): Promise<UsageMemberReport> {
  const q = month ? `?month=${encodeURIComponent(month)}` : "";
  return apiGet<{ data: UsageMemberReport }>(`/api/v1/tenants/${tenantId}/usage/members${q}`).then((r) => r.data);
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
  /** Term the plan line was priced by; "" on add-on orders and old orders. */
  billing_term?: UsageBillingTerm | "";
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
  /** "plan" (a plan, optional add-ons), "addon" (add-ons on the current
   * subscription) or "seats" (`seats` more seats on it for the rest of its
   * period; proration_vnd/prorated_days price them). */
  kind?: UsageOrderKind;
  target_subscription_id?: string | null;
  /** Seats order: the subscription end it was priced to. */
  target_ends_at?: string | null;
  addons?: UsageOrderAddonLine[];
  credit_vnd?: number;
  credit_lines?: UsageCreditLine[];
  /** Added seats charged for the days left on the current period (in subtotal_vnd). */
  proration_vnd?: number;
  prorated_days?: number;
  /** What the quarterly/yearly term saved (already in the plan line). */
  savings_vnd?: number;
  created_at: string;
  updated_at: string;
  /** What to write in the bank transfer so the payment matches the order. */
  transfer_content: string;
  /** VietQR image for a pending order (for the rest when part was received);
   * "" when billing is not configured. */
  qr_url: string;
  /* Single-order view only (GET /billing/orders/{code}): */
  /** End of the package a paid order went into (stored as the next VN midnight). */
  subscription_ends_at?: string | null;
  /** Short transfers received so far for a pending order. */
  received_vnd?: number;
  /** The latest transfer matched to the order. */
  last_payment?: { amount_vnd: number; status: string; transacted_at: string } | null;
  /* VAT invoice (hóa đơn GTGT) the buyer asked for; editable while the
   * order is pending or paid until a SuperAdmin marks it issued. */
  invoice_requested?: boolean;
  invoice_company_name?: string;
  invoice_tax_code?: string;
  invoice_address?: string;
  invoice_email?: string;
  /** When a SuperAdmin marked the invoice issued (null = not yet). */
  invoice_issued_at?: string | null;
  /** Who marked it issued (SuperAdmin views only). */
  invoice_issued_by?: string;
  invoice_no?: string;
  invoice_series?: string;
  /** The date on the invoice (Vietnam midnight). */
  invoice_date?: string | null;
}

export type UsageOrderKind = "plan" | "addon" | "seats";

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
  /** Plan change: held add-ons to carry over; omitted = the server default
   * (those that cost nothing to carry). */
  keep_addon_agent_ids?: string[];
  /** Place a plan order although the scope has one waiting for payment
   * (otherwise refused with reason plan_order_pending). */
  confirm_duplicate?: boolean;
  /** The buyer wants a VAT invoice with these details. */
  invoice?: InvoiceDetails;
}

/** Mirrors types.UsageCreditLine: unused paid days of the replaced package
 * (kind "plan"), of an add-on the order drops (kind "addon"), or of the
 * seats a kept add-on loses on a plan with fewer seats (kind "addon_units",
 * units = the seats not carried over). */
export interface UsageCreditLine {
  kind: "plan" | "addon" | "addon_units";
  agent_id?: string;
  name: string;
  units: number;
  days: number;
  amount_vnd: number;
  /** false: nobody paid for that time (grant, gift) → not credited. */
  paid: boolean;
}

/** Mirrors types.UsageHeldAddonChoice. */
export interface UsageHeldAddonChoice {
  agent_id: string;
  name: string;
  ends_at?: string | null;
  kept: boolean;
  carry_vnd: number;
  credit_vnd: number;
  bought_again?: boolean;
  /** Kept on a plan with fewer seats: the seats not carried over and their credit. */
  dropped_units?: number;
  units_credit_vnd?: number;
}

/** Mirrors types.UsageQuote. promo_error is a reason label (see promoErrorKey). */
export interface UsageQuote {
  plan: UsagePlan;
  mode: UsageMode;
  months: number;
  seats: number;
  unit_price_vnd: number;
  /** Term the plan line is priced by, and what it saves against monthly. */
  billing_term?: UsageBillingTerm;
  savings_vnd?: number;
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
  /** Plan change: the package replaced, the credit for its unused paid days
   * (credit_vnd applied; credit_unused_vnd above the order, lost), and the
   * carry/drop choice for each held add-on. */
  replaces?: { subscription_id: string; plan_id: string; plan_name: string; ends_at?: string | null } | null;
  credit_lines?: UsageCreditLine[];
  credit_vnd?: number;
  credit_unused_vnd?: number;
  held_addons?: UsageHeldAddonChoice[];
  /** Seats already assigned that the new seat package keeps. */
  seats_carried?: number;
  /** Seats assigned on the previous seat package (more than seats_carried:
   * the latest assigned lose theirs). */
  seats_held?: number;
  /** When the package ends if the order is confirmed now. */
  subscription_ends_at?: string | null;
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
  seller_name?: string;
  seller_tax_code?: string;
  seller_address?: string;
}

/** Mirrors types.UsageOrderReceipt: a paid order with the seller and buyer
 * a receipt prints (empty seller fields are left out). */
export interface UsageOrderReceipt {
  order: UsageOrder;
  seller_name: string;
  seller_tax_code: string;
  seller_address: string;
  seller_contact: string;
  bank_name: string;
  bank_account_no: string;
  buyer_name: string;
  buyer_email?: string;
}

const BILLING = "/api/v1/billing";

export function getBillingInfo(): Promise<UsageBillingInfo> {
  return apiGet<{ data: UsageBillingInfo }>(`${BILLING}/info`).then((r) => r.data);
}

export function getScopeAddons(scope: UsageScopeKind): Promise<UsageScopeAddons> {
  return apiGet<{ data?: UsageScopeAddons }>(`${BILLING}/subscription?scope=${scope}`).then((r) => ({
    subscription: r.data?.subscription ?? null,
    held: r.data?.held ?? [],
    included: r.data?.included ?? [],
    purchasable: r.data?.purchasable ?? [],
  }));
}

export function listPurchasablePlans(scope: UsageScopeKind): Promise<UsagePlan[]> {
  return apiGet<{ data?: UsagePlan[] }>(`${BILLING}/plans?scope=${scope}`).then((r) => r.data ?? []);
}

/** A plan's per-turn and per-session caps (see lib/plan-caps). */
export type UsagePlanCaps = PlanCaps;

export type PublicUsagePlan = Pick<UsagePlan, "id" | "name" | "description" | "scope_kind" | "mode" | "limits" | "price_vnd" | "price_quarter_vnd" | "price_year_vnd" | "price_label" | "sort_order" | "included_agents" | "addons"> & { caps?: UsagePlanCaps };

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

/** The VAT invoice details of the scope's latest order that asked for one
 * (null when none did), to prefill the order form. */
export function getLastInvoiceDetails(scope: UsageScopeKind): Promise<InvoiceDetails | null> {
  return apiGet<{ data?: InvoiceDetails | null }>(`${BILLING}/invoice-details?scope=${scope}`).then((r) => r.data ?? null);
}

/** Asks for, changes (requested true + details) or withdraws (requested
 * false) the VAT invoice of an order. */
export function updateOrderInvoice(code: string, requested: boolean, details?: InvoiceDetails): Promise<UsageOrder> {
  return apiPut<{ data: UsageOrder }>(`${BILLING}/orders/${encodeURIComponent(code)}/invoice`, { requested, ...(details ?? {}) }).then((r) => r.data);
}

/** The receipt of a paid order. */
export function getOrderReceipt(code: string): Promise<UsageOrderReceipt> {
  return apiGet<{ data: UsageOrderReceipt }>(`${BILLING}/orders/${encodeURIComponent(code)}/receipt`).then((r) => r.data);
}

/** The printable receipt page of a paid order. */
export function receiptHref(code: string): string {
  return `/billing/receipt/${encodeURIComponent(code)}`;
}

// SuperAdmin side.

/** Marks the VAT invoice of a paid order issued (number, series and date
 * YYYY-MM-DD optional; again to correct them). */
export function markInvoiceIssued(id: string, input: { invoice_no?: string; invoice_series?: string; date?: string }): Promise<UsageOrder> {
  return apiPost<{ data: UsageOrder }>(`${ADMIN}/orders/${encodeURIComponent(id)}/invoice-issued`, input).then((r) => r.data);
}

/** Undoes markInvoiceIssued: the buyer may edit the details again. */
export function unmarkInvoiceIssued(id: string): Promise<UsageOrder> {
  return apiDel<{ data: UsageOrder }>(`${ADMIN}/orders/${encodeURIComponent(id)}/invoice-issued`).then((r) => r.data);
}

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

/** Every order (SuperAdmin), by status. awaitingAdmin: only the orders the
 * awaiting-admin count counts (pending at 0 đồng, or pending/expired
 * holding money); invoiceRequested: only the orders that asked for a VAT
 * invoice. */
export function listAllUsageOrders(
  status?: UsageOrderStatus,
  opts: { awaitingAdmin?: boolean; invoiceRequested?: boolean } = {},
): Promise<UsageOrder[]> {
  const q = new URLSearchParams();
  if (status) q.set("status", status);
  if (opts.awaitingAdmin) q.set("awaiting_admin", "true");
  if (opts.invoiceRequested) q.set("invoice_requested", "true");
  const qs = q.toString();
  return apiGet<{ data?: UsageOrder[] }>(`${ADMIN}/orders${qs ? `?${qs}` : ""}`).then((r) => r.data ?? []);
}

export function confirmUsageOrder(id: string): Promise<UsageOrder> {
  return apiPost<{ data: UsageOrder }>(`${ADMIN}/orders/${encodeURIComponent(id)}/confirm`, {}).then((r) => r.data);
}

/** force: cancel although the order has received money (refused otherwise
 * with reason payment_received). */
export function cancelUsageOrder(id: string, force = false): Promise<UsageOrder> {
  return apiPost<{ data: UsageOrder }>(`${ADMIN}/orders/${encodeURIComponent(id)}/cancel${force ? "?force=true" : ""}`, {}).then((r) => r.data);
}

/** Puts an expired order back to pending (money that came after expiry);
 * transfers already recorded for it are counted again. force: although
 * another plan order of the scope is pending (refused otherwise with
 * reason plan_order_pending). */
export function reopenUsageOrder(id: string, force = false): Promise<UsageOrder> {
  return apiPost<{ data: UsageOrder }>(`${ADMIN}/orders/${encodeURIComponent(id)}/reopen${force ? "?force=true" : ""}`, {}).then((r) => r.data);
}

/** What needs a SuperAdmin on the billing page: orders only a SuperAdmin
 * can move on (pending at 0 đồng, or pending/expired holding money) and
 * paid orders whose VAT invoice is still to issue. */
export interface BillingAdminCounts {
  awaiting_admin: number;
  invoices_to_issue: number;
}

export function getBillingAdminCounts(): Promise<BillingAdminCounts> {
  return apiGet<{ data?: Partial<BillingAdminCounts> }>(`${ADMIN}/orders/awaiting-admin-count`).then((r) => ({
    awaiting_admin: r.data?.awaiting_admin ?? 0,
    invoices_to_issue: r.data?.invoices_to_issue ?? 0,
  }));
}

/** Window event the billing admin page fires with the new counts (detail:
 * BillingAdminCounts), so the console menu's badge follows. */
export const AWAITING_ORDERS_EVENT = "werag:billing-awaiting-orders";

/** system_settings keys of the receiving bank account and order policy. */
export const BILLING_SETTING_KEYS = {
  bankBin: "billing.bank_bin",
  bankName: "billing.bank_name",
  accountNo: "billing.bank_account_no",
  accountName: "billing.bank_account_name",
  orderTTLHours: "billing.order_ttl_hours",
  contact: "billing.contact",
  /** The seller printed on receipts (empty = left out). */
  sellerName: "billing.seller_name",
  sellerTaxCode: "billing.seller_tax_code",
  sellerAddress: "billing.seller_address",
  /** Secret of the payment checker; reads come back masked. */
  webhookSecret: "billing.webhook_secret",
} as const;

/** i18n key for a quote's promo_error. */
/** Locale key for a billing refusal's details.reason, or null when the
 * reason is unknown (the caller then shows the server message). */
export function billingErrorKey(reason: string | undefined): string | null {
  switch (reason) {
    case "invoice_address_required":
    case "invoice_company_required":
    case "invoice_date_invalid":
    case "invoice_email_invalid":
    case "invoice_issued":
    case "invoice_not_requested":
    case "invoice_order_closed":
    case "invoice_tax_code_invalid":
    case "invoice_too_long":
    case "order_not_paid":
    case "addon_held":
    case "addon_included":
    case "addon_none":
    case "addon_not_sold":
    case "addon_pending":
    case "no_subscription":
    case "not_seat_plan":
    case "open_ended_subscription":
    case "order_not_expired":
    case "order_not_pending":
    case "payment_received":
    case "plan_not_sold":
    case "plan_order_pending":
    case "seats_none":
    case "seats_order_pending":
    case "subscription_changed":
    case "term_not_sold":
      return `bill.err.${reason}`;
    case "promo_not_found":
    case "promo_inactive":
    case "promo_not_started":
    case "promo_expired":
    case "promo_exhausted":
    case "promo_wrong_plan":
      return `bill.${reason}`;
    default:
      return null;
  }
}

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
