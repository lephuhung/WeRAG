/* Role model matches the backend human model: member < admin, with the
 * legacy "owner" alias normalized to admin (see normalizeNavRole), and
 * "system" gating on the platform-wide system-admin flag.
 * Server-side guards stay authoritative; this only hides UI entries.
 */

export type SettingsRoleKey = "member" | "admin" | "owner" | "system";

const ROLE_LEVEL: Record<string, number> = {
  member: 10,
  admin: 30,
};

/* The backend owner role is retired: treat a legacy "owner" string as
 * admin on both sides of the comparison. */
function normalizeNavRole(role: string): string {
  return role === "owner" ? "admin" : role;
}

export interface SettingsNavItem {
  key: string;
  labelKey?: string;
  fallbackLabel: string;
  minRole: SettingsRoleKey;
}

export interface SettingsNavGroup {
  key: string;
  labelKey: string;
  items: SettingsNavItem[];
}

/* Sections under /platform/system/workspace — personal and tenant-level
 * settings. Tooling configs (env vars, browser connection, abbreviations)
 * live under /platform/system/extensions; platform-level configuration on
 * the engines/admin tabs. */
export const WORKSPACE_NAV_GROUPS: SettingsNavGroup[] = [
  {
    key: "account",
    labelKey: "settingsNav.groups.account",
    items: [
      { key: "general", labelKey: "settingsNav.general", fallbackLabel: "General", minRole: "member" },
    ],
  },
  {
    key: "workspace",
    labelKey: "settingsNav.groups.workspace",
    items: [
      { key: "tenant", labelKey: "settingsNav.tenant", fallbackLabel: "Workspace", minRole: "member" },
      { key: "members", labelKey: "settingsNav.members", fallbackLabel: "Members", minRole: "admin" },
      { key: "agency", labelKey: "settingsNav.agencyProfile", fallbackLabel: "Agency profile", minRole: "member" },
      { key: "api-keys", labelKey: "settingsNav.apiKeys", fallbackLabel: "API keys", minRole: "system" },
      { key: "memory", labelKey: "settingsNav.memory", fallbackLabel: "Memory", minRole: "system" },
    ],
  },
];

export function roleAtLeast(role: string, min: Exclude<SettingsRoleKey, "system">): boolean {
  return (ROLE_LEVEL[normalizeNavRole(role)] ?? 0) >= (ROLE_LEVEL[normalizeNavRole(min)] ?? 0);
}

export function canSeeSection(
  item: SettingsNavItem,
  currentRole: string,
  isSystemAdmin: boolean,
): boolean {
  if (item.minRole === "system") return isSystemAdmin;
  return isSystemAdmin || roleAtLeast(currentRole, item.minRole);
}
