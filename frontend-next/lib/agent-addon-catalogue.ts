/* The add-on catalogue (specialist agents sold with service plans,
 * docs/agent-addon-plans.md §4–§5): pure helpers for the agent editor and
 * the admin plan cards. Type-only imports so the node test runner can load
 * it. */
import type { UsageAgentAddon, UsageAgentAddonInput, UsagePlan } from "./api/usage.ts";

/** The editor's "Bán kèm gói dịch vụ" fields. */
export interface AddonSaleDraft {
  price_vnd: number;
  /** [] = every plan */
  plan_ids: string[];
  description: string;
  is_active: boolean;
  sort_order: number;
}

export function addonSaleDraft(row?: UsageAgentAddon | null): AddonSaleDraft {
  return {
    price_vnd: row?.price_vnd ?? 0,
    plan_ids: [...(row?.plan_ids ?? [])],
    description: row?.description ?? "",
    is_active: row?.is_active ?? true,
    sort_order: row?.sort_order ?? 0,
  };
}

export function addonSaleInput(agentId: string, draft: AddonSaleDraft): UsageAgentAddonInput {
  return {
    agent_id: agentId,
    price_vnd: Math.max(0, Math.round(Number(draft.price_vnd) || 0)),
    plan_ids: [...new Set(draft.plan_ids)].sort(),
    description: draft.description.trim(),
    is_active: draft.is_active,
    sort_order: Math.round(Number(draft.sort_order) || 0),
  };
}

/** Adds or removes one plan from the "sold with" list. */
export function togglePlanId(ids: string[], id: string): string[] {
  return ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id].sort();
}

/** Whether the catalogue row must be written on save: it exists already,
 * or the SuperAdmin filled the section in for this agent. */
export function shouldSaveAddonSale(existing: boolean, touched: boolean, distribution: string): boolean {
  return distribution === "addon" && (existing || touched);
}

/** The add-ons a plan's card lists: active rows whose agent is still a
 * specialist agent and that are sold with this plan, by sort order. */
export function addonsForPlan(rows: UsageAgentAddon[], plan: Pick<UsagePlan, "id">): UsageAgentAddon[] {
  return rows
    .filter((r) => r.is_active && r.agent_available !== false)
    .filter((r) => (r.plan_ids ?? []).length === 0 || r.plan_ids.includes(plan.id))
    .sort((a, b) => a.sort_order - b.sort_order || (a.agent_name ?? "").localeCompare(b.agent_name ?? "", "vi"));
}
