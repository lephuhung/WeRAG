/* Ported from frontend/src/config/settingsAccess.ts + Settings.vue navGroups.
 * Role model matches frontend/src/stores/auth.ts: viewer < contributor < admin
 * < owner, and SYSTEM_ADMIN_SECTIONS gate on the platform-wide system-admin
 * flag. Server-side guards stay authoritative; this only hides UI entries.
 */

export type SettingsRoleKey = "viewer" | "contributor" | "admin" | "owner" | "system";

const ROLE_LEVEL: Record<string, number> = {
  viewer: 10,
  contributor: 20,
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

/* Sections rendered by panels inside the settings page. Sections whose
 * functionality already lives on dedicated routes (/platform/system/*)
 * link there instead of rendering here. */
export const SETTINGS_NAV_GROUPS: SettingsNavGroup[] = [
  {
    key: "account",
    labelKey: "settingsNav.groups.account",
    items: [
      { key: "general", labelKey: "settingsNav.general", fallbackLabel: "General", minRole: "viewer" },
      { key: "envvars", labelKey: "settingsNav.envvars", fallbackLabel: "Environment variables", minRole: "viewer" },
      { key: "mymemory", labelKey: "settingsNav.mymemory", fallbackLabel: "My memory", minRole: "viewer" },
    ],
  },
  {
    key: "workspace",
    labelKey: "settingsNav.groups.workspace",
    items: [
      { key: "tenant", labelKey: "settingsNav.tenant", fallbackLabel: "Workspace", minRole: "viewer" },
      { key: "members", labelKey: "settingsNav.members", fallbackLabel: "Members", minRole: "viewer" },
      { key: "orgs", labelKey: "settingsNav.orgs", fallbackLabel: "Organizations", minRole: "viewer" },
      { key: "memory", labelKey: "settingsNav.memory", fallbackLabel: "Memory", minRole: "admin" },
    ],
  },
  {
    key: "models_runtime",
    labelKey: "settingsNav.groups.models",
    items: [
      { key: "models", labelKey: "settingsNav.models", fallbackLabel: "Models", minRole: "viewer" },
      { key: "ollama", fallbackLabel: "Ollama", minRole: "system" },
      { key: "weknoracloud", fallbackLabel: "WeRAG Cloud", minRole: "system" },
    ],
  },
  {
    key: "data_extensions",
    labelKey: "settingsNav.groups.data",
    items: [
      { key: "websearch", labelKey: "settingsNav.websearch", fallbackLabel: "Web search", minRole: "admin" },
      { key: "vectorstore", labelKey: "settingsNav.vectorstore", fallbackLabel: "Vector store engine", minRole: "admin" },
      { key: "parser", labelKey: "settingsNav.parser", fallbackLabel: "Parser engine", minRole: "admin" },
      { key: "storage", labelKey: "settingsNav.storage", fallbackLabel: "Storage engine", minRole: "admin" },
      { key: "sandbox", labelKey: "settingsNav.sandbox", fallbackLabel: "Sandbox", minRole: "admin" },
      { key: "skills", labelKey: "settingsNav.skills", fallbackLabel: "Skills", minRole: "admin" },
      { key: "mcp", labelKey: "settingsNav.mcp", fallbackLabel: "MCP", minRole: "admin" },
    ],
  },
  {
    key: "system_administration",
    labelKey: "settingsNav.groups.system",
    items: [
      { key: "system-overview", fallbackLabel: "Runtime overview", minRole: "system" },
      { key: "system-users", fallbackLabel: "Users & admins", minRole: "system" },
      { key: "system-services", fallbackLabel: "Services", minRole: "system" },
      { key: "system-logs", fallbackLabel: "Audit log", minRole: "system" },
    ],
  },
];

export function canSeeSection(
  item: SettingsNavItem,
  currentRole: string,
  isSystemAdmin: boolean,
): boolean {
  if (item.minRole === "system") return isSystemAdmin;
  const level = ROLE_LEVEL[currentRole] ?? 0;
  return level >= ROLE_LEVEL[item.minRole];
}

/* Vue app groups dedicated admin pages under /platform/settings sections
 * (system-global etc.); the Next port gives them real routes under
 * /platform/system/* instead. Map nav keys to those routes. */
export const SECTION_ROUTES: Record<string, string> = {
  "system-overview": "/platform/system/overview",
  "system-users": "/platform/system/users",
  "system-services": "/platform/system/services",
  "system-logs": "/platform/system/logs",
  "system-models": "/platform/system/models",
};

/* Legacy ?section= values from the Vue app → Next nav keys. */
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
