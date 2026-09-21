/* Ported from frontend/src/config/settingsAccess.ts + Settings.vue navGroups.
 * Role model matches internal/types/tenant_member.go: member < admin < owner,
 * and "system" gates on the platform-wide system-admin flag.
 * Server-side guards stay authoritative; this only hides UI entries.
 */

export type SettingsRoleKey = "member" | "admin" | "owner" | "system";

const ROLE_LEVEL: Record<string, number> = {
  member: 10,
  admin: 30,
  owner: 40,
};

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

/* Sections rendered inside /platform/system/workspace — personal and
 * tenant-level settings. Platform-level configuration lives on the sibling
 * admin tabs (overview, engines, models, extensions, agents, users, logs). */
export const WORKSPACE_NAV_GROUPS: SettingsNavGroup[] = [
  {
    key: "account",
    labelKey: "settingsNav.groups.account",
    items: [
      { key: "general", labelKey: "settingsNav.general", fallbackLabel: "General", minRole: "member" },
      { key: "envvars", labelKey: "settingsNav.envvars", fallbackLabel: "Environment variables", minRole: "member" },
      { key: "mymemory", labelKey: "settingsNav.mymemory", fallbackLabel: "My memory", minRole: "member" },
      { key: "browserconnection", fallbackLabel: "Browser connection", minRole: "member" },
    ],
  },
  {
    key: "workspace",
    labelKey: "settingsNav.groups.workspace",
    items: [
      { key: "tenant", labelKey: "settingsNav.tenant", fallbackLabel: "Workspace", minRole: "member" },
      { key: "members", labelKey: "settingsNav.members", fallbackLabel: "Members", minRole: "admin" },
      { key: "api-keys", labelKey: "settingsNav.apiKeys", fallbackLabel: "API keys", minRole: "owner" },
      { key: "orgs", labelKey: "settingsNav.orgs", fallbackLabel: "Organizations", minRole: "admin" },
      { key: "chathistory", fallbackLabel: "Chat history", minRole: "owner" },
      { key: "memory", labelKey: "settingsNav.memory", fallbackLabel: "Memory", minRole: "owner" },
    ],
  },
];

export function roleAtLeast(role: string, min: Exclude<SettingsRoleKey, "system">): boolean {
  return (ROLE_LEVEL[role] ?? 0) >= ROLE_LEVEL[min];
}

export function canSeeSection(
  item: SettingsNavItem,
  currentRole: string,
  isSystemAdmin: boolean,
): boolean {
  if (item.minRole === "system") return isSystemAdmin;
  return isSystemAdmin || roleAtLeast(currentRole, item.minRole);
}

/* Legacy /platform/settings?section=<key> URLs → canonical routes under
 * /platform/system. Workspace sections keep their key via ?section=. */
export const LEGACY_SECTION_ROUTES: Record<string, string> = {
  general: "/platform/system/workspace?section=general",
  envvars: "/platform/system/workspace?section=envvars",
  mymemory: "/platform/system/workspace?section=mymemory",
  browserconnection: "/platform/system/workspace?section=browserconnection",
  tenant: "/platform/system/workspace?section=tenant",
  members: "/platform/system/workspace?section=members",
  "api-keys": "/platform/system/workspace?section=api-keys",
  orgs: "/platform/system/workspace?section=orgs",
  chathistory: "/platform/system/workspace?section=chathistory",
  memory: "/platform/system/workspace?section=memory",
  models: "/platform/system/models",
  "system-overview": "/platform/system/overview",
  "system-users": "/platform/system/users",
  "system-services": "/platform/system/services",
  "system-logs": "/platform/system/logs",
  "system-models": "/platform/system/models",
  "system-extensions": "/platform/system/extensions",
  ollama: "/platform/system/models?tab=ollama",
  weknoracloud: "/platform/system/models?tab=weknoracloud",
  parser: "/platform/system/services?category=parsers",
  vectorstore: "/platform/system/services?category=vector",
  storage: "/platform/system/services?category=storage",
  websearch: "/platform/system/services?category=search",
  sandbox: "/platform/system/services?category=sandbox",
  skills: "/platform/system/extensions?tab=skills",
  mcp: "/platform/system/extensions?tab=mcp",
};

/* Legacy ?section= values from the Vue app → nav keys. */
const SECTION_ALIASES: Record<string, string> = {
  "system-global": "system-overview",
  "runtime-queues": "system-services",
  "platform-api-keys": "system-services",
  "system-audit-log": "system-logs",
};

export function normalizeSection(raw: string | null): string {
  if (!raw) return "general";
  return SECTION_ALIASES[raw] ?? raw;
}

export function legacySectionTarget(raw: string | null): string {
  const key = normalizeSection(raw);
  return LEGACY_SECTION_ROUTES[key] ?? "/platform/system/workspace";
}
