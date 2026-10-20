/* Route tree for /platform/system — the index of every deep link of the
 * settings surfaces (indexable by the command palette). Each tab points at
 * a real section: the /platform/system/* paths are not pages of their own,
 * the catch-all turns them into the settings modal (or the admin console),
 * so a path that is not a section would land on "general".
 * minRole mirrors nav-config.ts: undefined/member = any signed-in user,
 * tenant roles gate on membership, "system" gates on the platform-wide
 * system-admin flag. Server-side guards stay authoritative. */

import {
  WORKSPACE_NAV_GROUPS,
  type SettingsRoleKey,
} from "@/components/settings/nav-config";
import { ADMIN_GROUPS } from "@/components/admin/admin-sections";
import { adminSectionHref } from "@/components/admin/admin-routes";

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
    label: "Organization",
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
    /* The tab itself points at a real section: /platform/system/engines is
     * not a section (the catch-all would fall back to "general"). */
    href: "/platform/system/engines/vector",
    minRole: "system",
    groups: [
      {
        key: "engines",
        items: [
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
            key: "queues",
            label: "Runtime queues",
            labelKey: "systemNav.queues",
            href: adminSectionHref("queues"),
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
    /* Same rule as the engines tab: point at a real section. */
    href: "/platform/system/extensions/browserconnection",
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
            key: "imchannels",
            label: "IM channels",
            labelKey: "systemNav.imChannels",
            href: "/platform/system/extensions/im-channels",
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
            minRole: "system",
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
    /* Derived from the console's own section list, so every admin section
     * (tenants, users, logs, legal documents, skill sets, usage, billing,
     * runtime queues) is reachable from the ⌘K palette. */
    groups: [
      {
        key: "admin",
        items: ADMIN_GROUPS.flatMap((g) => g.items).map((s) => ({
          key: s.key,
          label: s.fallback,
          labelKey: s.labelKey,
          href: adminSectionHref(s.key),
          minRole: "system" as const,
        })),
      },
    ],
  },
];
