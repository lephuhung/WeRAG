/* Ported from frontend/src/config/settingsAccess.ts + Settings.vue navGroups.
 * Role model matches internal/types/tenant_member.go: member < admin < owner,
 * and SYSTEM_ADMIN_SECTIONS gate on the platform-wide system-admin flag.
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

/* Sections rendered by panels inside the settings page. Sections whose
 * functionality already lives on dedicated routes (/platform/system/*)
 * link there instead of rendering here. */
export const SETTINGS_NAV_GROUPS: SettingsNavGroup[] = [
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
  {
    key: "models_runtime",
    labelKey: "settingsNav.groups.models",
    items: [
      { key: "models", labelKey: "settingsNav.models", fallbackLabel: "Models", minRole: "owner" },
      { key: "ollama", fallbackLabel: "Ollama", minRole: "system" },
      { key: "weknoracloud", fallbackLabel: "WeRAG Cloud", minRole: "system" },
    ],
  },
  {
    key: "data_extensions",
    labelKey: "settingsNav.groups.data",
    items: [
      /* These keys redirect to /platform/system/* (SECTION_ROUTES), which is
       * platform-level — gate them on system admin, not tenant owner. */
      { key: "websearch", labelKey: "settingsNav.websearch", fallbackLabel: "Web search", minRole: "system" },
      { key: "vectorstore", labelKey: "settingsNav.vectorstore", fallbackLabel: "Vector store engine", minRole: "system" },
      { key: "parser", labelKey: "settingsNav.parser", fallbackLabel: "Parser engine", minRole: "system" },
      { key: "storage", labelKey: "settingsNav.storage", fallbackLabel: "Storage engine", minRole: "system" },
      { key: "sandbox", labelKey: "settingsNav.sandbox", fallbackLabel: "Sandbox", minRole: "system" },
      { key: "skills", labelKey: "settingsNav.skills", fallbackLabel: "Skills", minRole: "system" },
      { key: "mcp", labelKey: "settingsNav.mcp", fallbackLabel: "MCP", minRole: "system" },
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
  "system-extensions": "/platform/system/extensions",
  ollama: "/platform/system/models?tab=ollama",
  weknoracloud: "/platform/system/models?tab=weknoracloud",
  parser: "/platform/system/services?category=parser",
  vectorstore: "/platform/system/services?category=vector",
  storage: "/platform/system/services?category=storage",
  websearch: "/platform/system/services?category=websearch",
  sandbox: "/platform/system/services?category=sandbox",
  skills: "/platform/system/extensions?tab=skills",
  mcp: "/platform/system/extensions?tab=mcp",
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
