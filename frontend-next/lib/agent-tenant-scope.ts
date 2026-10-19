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
