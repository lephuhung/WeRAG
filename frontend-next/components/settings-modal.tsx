/* Settings hub opened from the account menu — two-pane layout modeled on the
 * reference design: a left rail (close, search, grouped nav) and a scrollable
 * content pane. Every section renders inline — heavyweight management pages
 * are lazy-imported from their route modules. Cards inside embedded pages
 * resolve through InModalNavContext into a modal-local sub-page stack with a
 * back button, so the modal never navigates away. A small external-link
 * button still offers the full-page route for deep-linking.
 * Role gating mirrors nav-config: "system" = platform admin, admin/owner gate
 * on the tenant membership role. Server-side guards stay authoritative. */
"use client";

import {
  Suspense,
  lazy,
  useCallback,
  useEffect,
  useRef,
  useState,
  type ComponentType,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { usePathname, useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth";
import { useT, type LocaleKey } from "@/lib/i18n";
import { canSeeSection, type SettingsRoleKey } from "@/components/settings/nav-config";
import { Select } from "@/components/select";
import { InModalNavContext } from "@/components/system/in-modal-nav";
import { GeneralSettings } from "@/components/settings/general-settings";
import { ProfileSettings } from "@/components/settings/profile-settings";
import { SecuritySettings } from "@/components/settings/security-settings";
import { MemoryPersonalSettings } from "@/components/settings/memory-personal-settings";
import { TenantInfo } from "@/components/settings/tenant-info";
import { TenantMembers } from "@/components/settings/tenant-members";
import { ApiKeysSection } from "@/components/settings/api-keys";
import { ChatHistorySettings } from "@/components/settings/chat-history-settings";
import { MemoryWorkspaceSettings } from "@/components/settings/memory-workspace-settings";
import { BrowserConnectionSettings } from "@/components/settings/browser-connection-settings";
import { EnvVarsSettings } from "@/components/settings/env-vars-settings";
import { AbbreviationsSettings } from "@/components/settings/abbreviations-settings";
import { SkillsSettings } from "@/components/settings/skills-settings";
import { VectorStoreSettings } from "@/components/settings/vector-store-settings";
import { StorageSettings } from "@/components/settings/storage-settings";
import { WebSearchSettings } from "@/components/settings/web-search-settings";
import { SandboxSettings } from "@/components/settings/sandbox-settings";
import {
  IconAgent,
  IconBook,
  IconBookmark,
  IconBulb,
  IconChevronLeft,
  IconChat,
  IconClock,
  IconClose,
  IconCode,
  IconDocReader,
  IconExternal,
  IconGlobe,
  IconGraph,
  IconInfoCircle,
  IconKey,
  IconLock,
  IconOrg,
  IconPower,
  IconPulse,
  IconSearch,
  IconSettings,
  IconStorageEngine,
  IconUser,
} from "@/components/icons";

/* Heavy sections are lazy so the sidebar chunk doesn't pull every admin page
 * eagerly — they stream in only when the section is opened. The section
 * modules live in components/settings/pages; the old /platform/system/*
 * routes redirect here and open the matching section. */
const SharingPage = lazy(() => import("@/components/settings/pages/sharing"));
const McpServersPage = lazy(() => import("@/components/settings/pages/mcp-servers"));
const AgentsPage = lazy(() => import("@/components/settings/pages/system-agents"));
const IMChannelsPage = lazy(() => import("@/components/settings/pages/im-channels"));
const ModelCatalogPage = lazy(() => import("@/components/settings/pages/model-catalog"));
const ModelOllamaPage = lazy(() => import("@/components/settings/pages/model-ollama"));
const ModelWeknoraCloudPage = lazy(() => import("@/components/settings/pages/model-weknoracloud"));
const RuntimeQueuesPage = lazy(() => import("@/components/settings/pages/runtime-queues"));

const AdminOverviewPage = lazy(() => import("@/components/settings/pages/admin-overview"));
const AdminUsersPage = lazy(() => import("@/components/settings/pages/admin-users"));
const AdminLogsPage = lazy(() => import("@/components/settings/pages/admin-logs"));

type IconCmp = ComponentType<{ className?: string }>;

interface SettingsItem {
  key: string;
  labelKey?: LocaleKey;
  fallback: string;
  icon: IconCmp;
  minRole?: SettingsRoleKey;
  render: () => ReactNode;
  /* Canonical route — used to map in-modal card links to sub-pages and for
   * the "open full page" affordance. */
  route?: string;
  /* Sub-page only: hidden from the nav (a hub page's cards already reach it)
   * but reachable via in-modal card links and search. `parent` is the hub
   * section key that owns it — used so the back button returns there. */
  subOnly?: boolean;
  parent?: string;
}

interface SettingsGroup {
  key: string;
  labelKey: LocaleKey;
  items: SettingsItem[];
}

const SYS = "/platform/system";
const W = `${SYS}/workspace`;
const E = `${SYS}/extensions`;

const GROUPS: SettingsGroup[] = [
  {
    key: "account",
    labelKey: "settingsNav.groups.account",
    items: [
      { key: "general", labelKey: "settingsNav.general", fallback: "General", icon: IconSettings, render: () => <GeneralSettings />, route: W },
      { key: "profile", labelKey: "userSettings.profile", fallback: "Profile", icon: IconUser, render: () => <ProfileSettings /> },
      { key: "security", labelKey: "userSettings.security", fallback: "Security", icon: IconLock, render: () => <SecuritySettings /> },
      { key: "mymemory", labelKey: "settingsNav.mymemory", fallback: "My memory", icon: IconBookmark, render: () => <MemoryPersonalSettings />, route: `${W}/mymemory` },
    ],
  },
  {
    key: "workspace",
    labelKey: "settingsNav.groups.workspace",
    items: [
      { key: "tenant", labelKey: "settingsNav.tenant", fallback: "Workspace info", icon: IconInfoCircle, render: () => <TenantInfo />, route: `${W}/tenant` },
      { key: "members", labelKey: "settingsNav.members", fallback: "Members", icon: IconOrg, minRole: "admin", render: () => <TenantMembers />, route: `${W}/members` },
      { key: "sharing", labelKey: "settingsNav.sharing", fallback: "Sharing", icon: IconGlobe, minRole: "admin", render: () => <SharingPage />, route: `${W}/sharing` },
      { key: "api-keys", labelKey: "settingsNav.apiKeys", fallback: "API keys", icon: IconKey, minRole: "admin", render: () => <ApiKeysSection />, route: `${W}/api-keys` },
      { key: "chathistory", labelKey: "systemNav.chathistory", fallback: "Chat history", icon: IconClock, minRole: "admin", render: () => <ChatHistorySettings />, route: `${W}/chathistory` },
      { key: "memory", labelKey: "settingsNav.memory", fallback: "Memory", icon: IconBulb, minRole: "admin", render: () => <MemoryWorkspaceSettings />, route: `${W}/memory` },
    ],
  },
  {
    key: "extensions",
    labelKey: "systemNav.extensions",
    items: [
      { key: "browserconnection", labelKey: "systemNav.browserconnection", fallback: "Browser connection", icon: IconGlobe, render: () => <BrowserConnectionSettings />, route: `${E}/browserconnection` },
      { key: "envvars", labelKey: "settingsNav.envvars", fallback: "Environment variables", icon: IconCode, render: () => <EnvVarsSettings />, route: `${E}/envvars` },
      { key: "abbreviations", labelKey: "settingsNav.abbreviations", fallback: "Abbreviations", icon: IconBook, render: () => <AbbreviationsSettings />, route: `${E}/abbreviations` },
      { key: "skills", labelKey: "settingsNav.skills", fallback: "Skills", icon: IconBulb, minRole: "system", render: () => <SkillsSettings />, route: `${E}/skills` },
      { key: "mcp", labelKey: "systemNav.mcpServers", fallback: "MCP servers", icon: IconPulse, minRole: "system", render: () => <McpServersPage />, route: `${E}/mcp` },
      { key: "agents", labelKey: "systemNav.agents", fallback: "Agents", icon: IconAgent, minRole: "system", render: () => <AgentsPage />, route: `${E}/agents` },
      { key: "imchannels", labelKey: "systemNav.imChannels", fallback: "IM channels", icon: IconChat, minRole: "system", render: () => <IMChannelsPage />, route: `${E}/im-channels` },
    ],
  },
  {
    key: "engines",
    labelKey: "settingsNav.config",
    items: [
      { key: "catalog", labelKey: "systemNav.catalog", fallback: "Model catalog", icon: IconGraph, minRole: "owner", render: () => <ModelCatalogPage />, route: `${SYS}/models` },
      { key: "ollama", fallback: "Ollama (local)", icon: IconGraph, minRole: "owner", render: () => <ModelOllamaPage />, route: `${SYS}/models/ollama`, subOnly: true, parent: "catalog" },
      { key: "weknoracloud", fallback: "WeRAG Cloud", icon: IconGraph, minRole: "owner", render: () => <ModelWeknoraCloudPage />, route: `${SYS}/models/weknoracloud`, subOnly: true, parent: "catalog" },
      { key: "vector", labelKey: "settingsNav.vectorstore", fallback: "Vector store engine", icon: IconGraph, minRole: "system", render: () => <VectorStoreSettings />, route: `${SYS}/engines/vector` },
      { key: "storage", labelKey: "settingsNav.storage", fallback: "Storage engine", icon: IconStorageEngine, minRole: "system", render: () => <StorageSettings />, route: `${SYS}/engines/storage` },
      { key: "search", labelKey: "settingsNav.websearch", fallback: "Web search", icon: IconSearch, minRole: "system", render: () => <WebSearchSettings />, route: `${SYS}/engines/search` },
      { key: "sandbox", labelKey: "settingsNav.sandbox", fallback: "Sandbox", icon: IconPower, minRole: "system", render: () => <SandboxSettings />, route: `${SYS}/engines/sandbox` },

      { key: "queues", labelKey: "systemNav.queues", fallback: "Runtime queues", icon: IconClock, minRole: "system", render: () => <RuntimeQueuesPage />, route: `${SYS}/engines/queues` },
    ],
  },
  {
    key: "admin",
    labelKey: "systemNav.admin",
    items: [
      { key: "overview", labelKey: "systemNav.overview", fallback: "Overview", icon: IconPulse, minRole: "system", render: () => <AdminOverviewPage />, route: `${SYS}/admin` },
      { key: "users", labelKey: "systemNav.users", fallback: "Users", icon: IconUser, minRole: "system", render: () => <AdminUsersPage />, route: `${SYS}/admin/users` },
      { key: "logs", labelKey: "systemNav.logs", fallback: "Audit logs", icon: IconDocReader, minRole: "system", render: () => <AdminLogsPage />, route: `${SYS}/admin/logs` },
    ],
  },
];

/* route → section key, so links inside embedded pages resolve to modal
 * sub-pages instead of navigating away. Also consumed by the
 * /platform/system catch-all redirect, which turns an old deep link into
 * "open the settings modal at this section". */
export const SECTION_ROUTES: Record<string, string> = Object.fromEntries(
  GROUPS.flatMap((g) => g.items)
    .filter((it) => it.route)
    .map((it) => [it.route as string, it.key]),
);
const ROUTE_TO_KEY = new Map<string, string>(Object.entries(SECTION_ROUTES));
const ITEM_KEYS = new Set<string>();
for (const g of GROUPS)
  for (const it of g.items) {
    if (it.route) ROUTE_TO_KEY.set(it.route, it.key);
    ITEM_KEYS.add(it.key);
  }

export function SettingsModal({
  open,
  onClose,
  initialSection = "general",
  pendingAbbrev = 0,
}: {
  open: boolean;
  onClose: () => void;
  initialSection?: string;
  pendingAbbrev?: number;
}) {
  const { t } = useT();
  const router = useRouter();
  const pathname = usePathname();
  const auth = useAuth();
  const [mounted, setMounted] = useState(false);
  const [stack, setStack] = useState<string[]>([initialSection]);
  const [query, setQuery] = useState("");

  useEffect(() => setMounted(true), []);

  useEffect(() => {
    if (open) {
      setStack([initialSection]);
      setQuery("");
    }
  }, [open, initialSection]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  /* Embedded pages may hold plain <Link> navigation (e.g. sharing →
   * knowledge bases). When the underlying route changes, close the modal so
   * it doesn't linger over a different page. */
  const openRef = useRef(open);
  openRef.current = open;
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    if (openRef.current) closeRef.current();
  }, [pathname]);

  const isSystemAdmin = auth.user?.is_system_admin === true;
  const currentRole =
    auth.memberships.find(
      (m) => String(m.tenant_id) === String(auth.selectedTenantId ?? auth.tenant?.id ?? ""),
    )?.role ?? "";

  const label = (it: SettingsItem) => (it.labelKey ? t(it.labelKey) : it.fallback);

  const q = query.trim().toLowerCase();
  const allowed = (it: SettingsItem) =>
    canSeeSection(
      { key: it.key, fallbackLabel: it.fallback, minRole: it.minRole ?? "member" },
      currentRole,
      isSystemAdmin,
    );

  const visibleGroups = GROUPS.map((g) => ({
    ...g,
    items: g.items.filter(
      (it) => allowed(it) && (!it.subOnly || !!q) && (!q || label(it).toLowerCase().includes(q)),
    ),
  })).filter((g) => g.items.length > 0);

  /* All sections the user may reach — including sub-only ones. */
  const allowedKeys = new Set(
    GROUPS.flatMap((g) => g.items).filter(allowed).map((it) => it.key),
  );
  const allowedKeysRef = useRef(allowedKeys);
  allowedKeysRef.current = allowedKeys;

  const activeKey = stack[stack.length - 1];
  const visibleItems = visibleGroups.flatMap((g) => g.items);
  const active =
    visibleItems.find((it) => it.key === activeKey) ??
    GROUPS.flatMap((g) => g.items).find((it) => it.key === activeKey && allowedKeys.has(it.key)) ??
    visibleItems[0];

  /* Card inside an embedded page → push a modal sub-page when the target (a
   * route href or a section key) maps to a section the user can see.
   * Returns false so the grid falls back to its normal behavior. */
  const inModalNav = useCallback((target: string) => {
    const key = ROUTE_TO_KEY.get(target) ?? (ITEM_KEYS.has(target) ? target : null);
    if (!key || !allowedKeysRef.current.has(key)) return false;
    setStack((s) => (s[s.length - 1] === key ? s : [...s, key]));
    return true;
  }, []);

  const openSection = (it: SettingsItem) =>
    setStack(it.subOnly && it.parent ? [it.parent, it.key] : [it.key]);

  if (!open || !mounted) return null;

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center p-2 sm:p-6">
      <div className="absolute inset-0 bg-ink/20" onClick={onClose} />
      <div
        role="dialog"
        aria-label={t("user.settings")}
        className="card relative flex max-h-[calc(100dvh-1rem)] w-full max-w-[1120px] flex-col overflow-hidden sm:h-[min(760px,calc(100dvh-3rem))] sm:flex-row"
      >
        {/* left rail — close, search, grouped nav */}
        <div className="flex shrink-0 flex-col border-b border-hairline sm:w-[260px] sm:border-b-0 sm:border-r">
          <div className="flex items-center gap-2 px-3 pt-3">
            <button
              onClick={onClose}
              aria-label="Close"
              className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-strong hover:text-ink"
            >
              <IconClose className="h-4 w-4" />
            </button>
            <div className="relative min-w-0 flex-1">
              <IconSearch className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-soft" />
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder={t("settingsModal.search")}
                className="input h-9 rounded-full pl-8 text-[13px]"
              />
            </div>
          </div>
          <nav className="hidden min-h-0 flex-1 flex-col gap-0.5 overflow-y-auto px-3 py-3 sm:flex sm:overflow-x-hidden">
            {visibleGroups.length === 0 && (
              <p className="caption px-3 py-2 text-muted">{t("settingsModal.noResults")}</p>
            )}
            {visibleGroups.map((g) => (
              <div key={g.key} className="sm:mb-1">
                <div className="caption-uppercase px-3 pb-1.5 pt-2.5 text-muted-soft max-sm:hidden">
                  {t(g.labelKey)}
                </div>
                {g.items.map((it) => (
                  <button
                    key={it.key}
                    type="button"
                    onClick={() => openSection(it)}
                    className={`nav-item max-sm:w-auto max-sm:whitespace-nowrap ${
                      activeKey === it.key ? "active" : ""
                    }`}
                  >
                    <it.icon className="h-[18px] w-[18px] shrink-0" />
                    <span className="min-w-0 flex-1 truncate text-left">{label(it)}</span>
                    {it.key === "abbreviations" && pendingAbbrev > 0 && (
                      <span className="badge-pill border border-amber-500/20 bg-amber-500/10 text-amber-600">
                        {pendingAbbrev}
                      </span>
                    )}
                  </button>
                ))}
              </div>
            ))}
          </nav>
          {/* Mobile: the whole nav collapses into one dropdown. */}
          <div className="px-3 py-3 sm:hidden">
            <Select
              value={activeKey}
              onChange={(key) => {
                const it = visibleGroups.flatMap((grp) => grp.items).find((x) => x.key === key);
                if (it) openSection(it);
              }}
              options={visibleGroups.flatMap((grp) =>
                grp.items.map((it) => ({ value: it.key, label: label(it) })),
              )}
            />
          </div>
        </div>

        {/* content pane */}
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          <div className="flex shrink-0 items-center gap-2 border-b border-hairline px-5 py-3.5 sm:px-7">
            {stack.length > 1 && (
              <button
                onClick={() => setStack((s) => s.slice(0, -1))}
                aria-label={t("settingsModal.back")}
                className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-strong hover:text-ink"
              >
                <IconChevronLeft className="h-4 w-4" />
              </button>
            )}
            <h2 className="title-md min-w-0 flex-1 truncate">{active ? label(active) : ""}</h2>
            {active?.route && (
              <button
                onClick={() => {
                  router.push(active.route!);
                  onClose();
                }}
                title={t("settingsModal.openFull")}
                aria-label={t("settingsModal.openFull")}
                className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-muted-soft transition-colors hover:bg-surface-strong hover:text-ink"
              >
                <IconExternal className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-5 py-5 sm:px-7">
            <InModalNavContext.Provider value={inModalNav}>
              <Suspense
                fallback={
                  <div className="flex items-center justify-center py-16">
                    <span className="caption text-muted">Loading…</span>
                  </div>
                }
              >
                {active?.render()}
              </Suspense>
            </InModalNavContext.Provider>
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
}
