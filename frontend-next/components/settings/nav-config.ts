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
      { key: "mymemory", labelKey: "settingsNav.mymemory", fallbackLabel: "My memory", minRole: "member" },
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
      { key: "chathistory", labelKey: "systemNav.chathistory", fallbackLabel: "Chat history", minRole: "owner" },
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
 * /platform/system. Every section is a real route now. */
export const LEGACY_SECTION_ROUTES: Record<string, string> = {
  general: "/platform/system/workspace",
  envvars: "/platform/system/extensions/envvars",
  mymemory: "/platform/system/workspace/mymemory",
  browserconnection: "/platform/system/extensions/browserconnection",
  tenant: "/platform/system/workspace/tenant",
  members: "/platform/system/workspace/members",
  "api-keys": "/platform/system/workspace/api-keys",
  orgs: "/platform/system/workspace/orgs",
  abbreviations: "/platform/system/extensions/abbreviations",
  chathistory: "/platform/system/workspace/chathistory",
  memory: "/platform/system/workspace/memory",
  models: "/platform/system/models",
  "system-overview": "/platform/system/admin",
  "system-users": "/platform/system/admin/users",
  "system-services": "/platform/system/engines",
  "system-logs": "/platform/system/admin/logs",
  "system-models": "/platform/system/models",
  "system-extensions": "/platform/system/extensions/mcp",
  ollama: "/platform/system/models/ollama",
  weknoracloud: "/platform/system/models/weknoracloud",
  parser: "/platform/system/engines",
  vectorstore: "/platform/system/engines/vector",
  storage: "/platform/system/engines/storage",
  websearch: "/platform/system/engines/search",
  sandbox: "/platform/system/engines/sandbox",
  skills: "/platform/system/extensions/skills",
  mcp: "/platform/system/extensions/mcp",
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
