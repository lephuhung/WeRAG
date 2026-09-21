/* Ported from frontend/src/views/settings/Settings.vue.
 * Left nav groups come from nav-config.ts (role-gated); the URL contract is
 * `?section=<key>` with the same legacy aliases as the Vue app
 * (normalizeSection). Panel bodies are ported per-section under
 * components/settings/; sections without a ported body render a stub
 * instead of being hidden, so nothing is silently missing.
 */
"use client";

import { Suspense, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useT } from "@/lib/i18n";
import { useAuth } from "@/lib/auth";
import {
  SETTINGS_NAV_GROUPS,
  SECTION_ROUTES,
  canSeeSection,
  normalizeSection,
  type SettingsNavItem,
} from "@/components/settings/nav-config";
import { GeneralSettings } from "@/components/settings/general-settings";
import { McpServicesPanel } from "@/components/settings/mcp-services";
import { TenantInfo } from "@/components/settings/tenant-info";
import { ApiKeysSection } from "@/components/settings/api-keys";
import { ModelsSettings } from "@/components/settings/models-settings";
import { OllamaSettings } from "@/components/settings/ollama-settings";
import { WebSearchSettings } from "@/components/settings/web-search-settings";
import { StorageSettings } from "@/components/settings/storage-settings";
import { VectorStoreSettings } from "@/components/settings/vector-store-settings";
import { EnvVarsSettings } from "@/components/settings/env-vars-settings";
import { TenantMembers } from "@/components/settings/tenant-members";
import { TenantOrgs } from "@/components/settings/tenant-orgs";
import { MemoryPersonalSettings } from "@/components/settings/memory-personal-settings";
import { MemoryWorkspaceSettings } from "@/components/settings/memory-workspace-settings";
import { SkillsSettings } from "@/components/settings/skills-settings";
import { SandboxSettings } from "@/components/settings/sandbox-settings";
import { BrowserConnectionSettings } from "@/components/settings/browser-connection-settings";
import { ChatHistorySettings } from "@/components/settings/chat-history-settings";
import { WeKnoraCloudSettings } from "@/components/settings/weknora-cloud-settings";
import { ParserEngineSettings } from "@/components/settings/parser-engine-settings";

export default function SettingsPage() {
  return (
    <Suspense fallback={null}>
      <SettingsBody />
    </Suspense>
  );
}

function SettingsBody() {
  const { t } = useT();
  const router = useRouter();
  const params = useSearchParams();
  const auth = useAuth();

  const active = normalizeSection(params.get("section"));

  const currentRole =
    auth.memberships.find(
      (m) => String(m.tenant_id) === String(auth.selectedTenantId ?? auth.tenant?.id ?? ""),
    )?.role ?? "";
  const isSystemAdmin = auth.user?.is_system_admin === true;

  useEffect(() => {
    if (SECTION_ROUTES[active]) {
      router.replace(SECTION_ROUTES[active]);
    }
  }, [active, router]);

  const setActive = (key: string) => {
    if (SECTION_ROUTES[key]) {
      router.push(SECTION_ROUTES[key]);
      return;
    }
    router.replace(`/platform/settings?section=${key}`);
  };

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-[1200px] px-12 py-12">
        {isSystemAdmin && (
          <div className="mb-8 flex flex-col gap-4 rounded-xl border border-primary/20 bg-primary/5 p-4 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <div className="flex items-center gap-2">
                <span className="badge-pill bg-primary/20 text-primary font-semibold text-[11px] uppercase tracking-wider">
                  SuperAdmin
                </span>
                <span className="text-[14px] font-medium text-ink">System Administration Center</span>
              </div>
              <p className="caption mt-0.5 text-muted">
                System engines, models, storage, and extensions are managed in the Card-based administration portal.
              </p>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <Link href="/platform/system/services" className="btn btn-outline btn-sm text-[12px]">
                Engines
              </Link>
              <Link href="/platform/system/models" className="btn btn-outline btn-sm text-[12px]">
                Models
              </Link>
              <Link href="/platform/system/extensions" className="btn btn-primary btn-sm text-[12px]">
                Extensions & MCP
              </Link>
            </div>
          </div>
        )}

        <div className="flex gap-10">
          {/* section nav */}
          <div className="w-[240px] shrink-0">
            {SETTINGS_NAV_GROUPS.map((group) => {
              const items = group.items.filter((it) =>
                canSeeSection(it, currentRole, isSystemAdmin),
              );
              if (items.length === 0) return null;
              return (
                <div key={group.key} className="mb-6">
                  <div className="caption-uppercase mb-2 px-3 text-muted-soft">
                    {t(group.labelKey as never)}
                  </div>
                  {items.map((it) => (
                    <button
                      key={it.key}
                      onClick={() => setActive(it.key)}
                      className={`nav-item mb-0.5 ${active === it.key ? "active" : ""}`}
                    >
                      <span className="truncate">{it.labelKey ? t(it.labelKey as never) : it.fallbackLabel}</span>
                    </button>
                  ))}
                </div>
              );
            })}
          </div>

          {/* panel */}
          <div className="min-w-0 flex-1">
            <div className="card p-8">
              {active === "general" ? (
                <GeneralSettings />
              ) : active === "envvars" ? (
                <EnvVarsSettings />
              ) : active === "mymemory" ? (
                <MemoryPersonalSettings />
              ) : active === "browserconnection" ? (
                <BrowserConnectionSettings />
              ) : active === "tenant" ? (
                <TenantInfo />
              ) : active === "members" ? (
                <TenantMembers />
              ) : active === "api-keys" ? (
                <ApiKeysSection />
              ) : active === "orgs" ? (
                <TenantOrgs />
              ) : active === "chathistory" ? (
                <ChatHistorySettings />
              ) : active === "memory" ? (
                <MemoryWorkspaceSettings />
              ) : active === "models" ? (
                <ModelsSettings />
              ) : active === "ollama" ? (
                <OllamaSettings />
              ) : active === "weknoracloud" ? (
                <WeKnoraCloudSettings />
              ) : active === "websearch" ? (
                <WebSearchSettings />
              ) : active === "storage" ? (
                <StorageSettings />
              ) : active === "vectorstore" ? (
                <VectorStoreSettings />
              ) : active === "parser" ? (
                <ParserEngineSettings />
              ) : active === "sandbox" ? (
                <SandboxSettings />
              ) : active === "skills" ? (
                <SkillsSettings />
              ) : active === "mcp" ? (
                <McpSection />
              ) : (
                <SectionStub section={active} />
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

/* MCP settings — the panel itself is a slide-over because the service
 * editor is a full modal flow in the Vue app too. */
function McpSection() {
  const [open, setOpen] = useState(true);
  return (
    <>
      <p className="caption mb-3 text-muted">
        Register external Model-Context-Protocol servers once; agents pick them via the mention picker.
      </p>
      <button className="btn btn-primary btn-sm" onClick={() => setOpen(true)}>
        Manage services
      </button>
      <McpServicesPanel open={open} onClose={() => setOpen(false)} />
    </>
  );
}

/* Placeholder for sections whose bodies are still being ported from the Vue
 * app — deliberately visible rather than hidden, so unported functionality
 * is never silently absent. */
function SectionStub({ section }: { section: string }) {
  return (
    <p className="caption text-muted">
      {`"${section}" settings section is being ported from the Vue app — not yet available.`}
    </p>
  );
}
