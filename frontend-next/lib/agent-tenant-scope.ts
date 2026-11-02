/* "Tổ chức áp dụng": the workspaces (Tổ chức) allowed to use a custom agent.
 * An empty list means every Tổ chức. Built-in agents are never scoped (the
 * server refuses a list on them). Kept free of app imports so the node test
 * runner can load it. */

export interface ScopeTenantOption {
  id: number;
  name: string;
  kind?: string;
  config_profile_id?: number | null;
}

// The Tổ chức an agent can be restricted to: workspaces that inherit the
// shared configuration (the hidden configuration profile itself and
// workspaces without a profile never see shared agents), sorted by name.
export function scopeTenantOptions(tenants: ScopeTenantOption[]): ScopeTenantOption[] {
  return tenants
    .filter((t) => (t.kind ?? "workspace") === "workspace" && t.config_profile_id != null)
    .sort((a, b) => a.name.localeCompare(b.name, "vi") || a.id - b.id);
}

// Adds or removes one id, keeping the list sorted and duplicate-free like the
// server stores it.
export function toggleScopeTenant(selected: number[], id: number): number[] {
  const next = selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id];
  return [...new Set(next)].sort((a, b) => a - b);
}

// Badge text for a scoped agent: the names when there are few, otherwise the
// first ones and a count. null = every Tổ chức (no badge).
export function scopeSummary(
  ids: number[] | null | undefined,
  nameOf: (id: number) => string | undefined,
  max = 2,
): { names: string[]; more: number } | null {
  if (!ids || ids.length === 0) return null;
  const names = ids.slice(0, max).map((id) => nameOf(id) || `#${id}`);
  return { names, more: Math.max(0, ids.length - max) };
}

// The agent a chat should use when `selected` (from settings in
// localStorage, or a session's last_request_state) may no longer be listed:
// a custom agent outside the Tổ chức's "Tổ chức áp dụng", a deleted one, or
// a switched-off built-in. Unlisted → `preferred` when listed, else the
// first listed agent; null when nothing is listed (keep the choice).
export function availableAgentId(listed: string[], selected: string, preferred: string): string | null {
  if (listed.length === 0) return null;
  if (listed.includes(selected)) return selected;
  return listed.includes(preferred) ? preferred : listed[0];
}

/* "Cách phân phối" of a custom agent (docs/agent-addon-plans.md §3):
 * basic = every Tổ chức, addon = only buyers (a service-plan add-on),
 * internal = the "Tổ chức áp dụng" list. Built-ins are always basic. */
export type AgentDistribution = "basic" | "addon" | "internal";

export const AGENT_DISTRIBUTIONS: AgentDistribution[] = ["basic", "addon", "internal"];

export function normalizeDistribution(value: unknown): AgentDistribution {
  return value === "addon" || value === "internal" ? value : "basic";
}

// The distribution a new agent starts with, mirroring the server default:
// internal with a list, addon with a skill set, else basic.
export function defaultDistribution(skillSets: string[], scopeIds: number[]): AgentDistribution {
  if (scopeIds.length > 0) return "internal";
  if (skillSets.length > 0) return "addon";
  return "basic";
}

// The "Tổ chức áp dụng" list to save: only an internal agent keeps one; the
// server refuses a list with basic or addon, so [] clears it there.
export function scopeForSave(distribution: AgentDistribution, scopeIds: number[]): number[] {
  return distribution === "internal" ? scopeIds : [];
}

/* The basic agents every service plan includes (backend
 * types.UsageIncludedAgentIDs, plan decision 7.1). Switching one off asks
 * for confirmation. */
export const PLAN_BASIC_AGENT_IDS = ["builtin-document-assistant", "builtin-smart-reasoning"];

// Whether switching `agentId` to `next` needs the "every plan includes it"
// confirmation.
export function switchOffNeedsConfirm(agentId: string, next: boolean): boolean {
  return !next && PLAN_BASIC_AGENT_IDS.includes(agentId);
}

// An agent's on/off state as the API reports it; absent = on.
export function agentEnabled(agent: { enabled?: boolean | null }): boolean {
  return agent.enabled !== false;
}
