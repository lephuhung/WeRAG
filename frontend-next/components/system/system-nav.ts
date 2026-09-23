/* Route tree for /platform/system — five tabs, each rendered as a card hub
 * of real routes (deep-linkable, indexable by the command palette).
 * The first item of every tab lives on the tab's index route; deeper items
 * get their own path. minRole mirrors nav-config.ts: undefined/member = any
 * signed-in user, tenant roles gate on membership, "system" gates on the
 * platform-wide system-admin flag. Server-side guards stay authoritative. */

import {
  WORKSPACE_NAV_GROUPS,
  type SettingsRoleKey,
} from "@/components/settings/nav-config";

export interface SystemNavItem {
  key: string;
  label: string;
  labelKey?: string;
  href: string;
  minRole?: SettingsRoleKey;
}

export interface SystemNavGroup {
  key: string;
  labelKey?: string;
  items: SystemNavItem[];
}

export interface SystemTab {
  key: string;
  label: string;
  labelKey?: string;
  href: string;
  minRole?: SettingsRoleKey;
  groups: SystemNavGroup[];
}

const W = "/platform/system/workspace";

const workspaceGroups: SystemNavGroup[] = WORKSPACE_NAV_GROUPS.map((g) => ({
  key: g.key,
  labelKey: g.labelKey,
  items: g.items.map((it) => ({
    key: it.key,
    label: it.fallbackLabel,
    labelKey: it.labelKey,
    href: it.key === "general" ? W : `${W}/${it.key}`,
    minRole: it.minRole,
  })),
}));

export const SYSTEM_TABS: SystemTab[] = [
  {
    key: "workspace",
    label: "Workspace",
    labelKey: "systemNav.workspace",
    href: W,
    groups: workspaceGroups,
  },
  {
    key: "models",
    label: "Models",
    labelKey: "systemNav.models",
    href: "/platform/system/models",
    minRole: "owner",
    groups: [
      {
        key: "models",
        items: [
          {
            key: "catalog",
            label: "Model catalog",
            labelKey: "systemNav.catalog",
            href: "/platform/system/models",
            minRole: "owner",
          },
          {
            key: "ollama",
            label: "Ollama (local)",
            href: "/platform/system/models/ollama",
            minRole: "owner",
          },
          {
            key: "weknoracloud",
            label: "WeRAG Cloud",
            href: "/platform/system/models/weknoracloud",
            minRole: "owner",
          },
        ],
      },
    ],
  },
  {
    key: "engines",
    label: "Engines",
    labelKey: "systemNav.engines",
    href: "/platform/system/engines",
    minRole: "system",
    groups: [
      {
        key: "engines",
        items: [
          {
            key: "engines",
            label: "Engines",
            labelKey: "systemNav.engines",
            href: "/platform/system/engines",
            minRole: "system",
          },
          {
            key: "vector",
            label: "Vector store",
            labelKey: "settingsNav.vectorstore",
            href: "/platform/system/engines/vector",
            minRole: "system",
          },
          {
            key: "storage",
            label: "Storage",
            labelKey: "settingsNav.storage",
            href: "/platform/system/engines/storage",
            minRole: "system",
          },
          {
            key: "search",
            label: "Web search",
            labelKey: "settingsNav.websearch",
            href: "/platform/system/engines/search",
            minRole: "system",
          },
          {
            key: "sandbox",
            label: "Sandbox",
            labelKey: "settingsNav.sandbox",
            href: "/platform/system/engines/sandbox",
            minRole: "system",
          },
        ],
      },
      {
        key: "pipeline",
        labelKey: "systemNav.pipeline",
        items: [
          {
            key: "parse-defaults",
            label: "Parse defaults",
            labelKey: "pd.title",
            href: "/platform/system/engines/parse-defaults",
            minRole: "system",
          },
          {
            key: "queues",
            label: "Runtime queues",
            labelKey: "systemNav.queues",
            href: "/platform/system/engines/queues",
            minRole: "system",
          },
        ],
      },
    ],
  },
  {
    /* Extensions is visible to every member: it also hosts the personal /
     * workspace tooling configs (env vars, browser connection,
     * abbreviations). Platform extensions stay system-gated per item. */
    key: "extensions",
    label: "Extensions",
    labelKey: "systemNav.extensions",
    href: "/platform/system/extensions",
    groups: [
      {
        key: "platform",
        labelKey: "systemNav.admin",
        items: [
          {
            key: "agents",
            label: "Agents",
            labelKey: "systemNav.agents",
            href: "/platform/system/extensions/agents",
            minRole: "system",
          },
          {
            key: "mcp",
            label: "MCP servers",
            labelKey: "systemNav.mcpServers",
            href: "/platform/system/extensions/mcp",
            minRole: "system",
          },
          {
            key: "skills",
            label: "Skills",
            labelKey: "settingsNav.skills",
            href: "/platform/system/extensions/skills",
            minRole: "system",
          },
        ],
      },
      {
        key: "workspace-tools",
        labelKey: "settingsNav.groups.workspace",
        items: [
          {
            key: "browserconnection",
            label: "Browser connection",
            labelKey: "systemNav.browserconnection",
            href: "/platform/system/extensions/browserconnection",
            minRole: "member",
          },
          {
            key: "envvars",
            label: "Environment variables",
            labelKey: "settingsNav.envvars",
            href: "/platform/system/extensions/envvars",
            minRole: "member",
          },
          {
            key: "abbreviations",
            label: "Abbreviations",
            labelKey: "settingsNav.abbreviations",
            href: "/platform/system/extensions/abbreviations",
            minRole: "member",
          },
        ],
      },
    ],
  },
  {
    key: "admin",
    label: "Administration",
    labelKey: "systemNav.admin",
    href: "/platform/system/admin",
    minRole: "system",
    groups: [
      {
        key: "admin",
        items: [
          {
            key: "overview",
            label: "Overview",
            labelKey: "systemNav.overview",
            href: "/platform/system/admin",
            minRole: "system",
          },
          {
            key: "users",
            label: "Users",
            labelKey: "systemNav.users",
            href: "/platform/system/admin/users",
            minRole: "system",
          },
          {
            key: "logs",
            label: "Audit logs",
            labelKey: "systemNav.logs",
            href: "/platform/system/admin/logs",
            minRole: "system",
          },
        ],
      },
    ],
  },
];
