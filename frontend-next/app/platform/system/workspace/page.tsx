/* Personal and tenant-level settings — the workspace half of the old
 * /platform/settings page, now a tab under /platform/system. Left nav groups
 * come from nav-config.ts (role-gated); the URL contract is `?section=<key>`.
 */
"use client";

import { Suspense, useEffect } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useT } from "@/lib/i18n";
import { useAuth } from "@/lib/auth";
import {
  WORKSPACE_NAV_GROUPS,
  canSeeSection,
} from "@/components/settings/nav-config";
import { GeneralSettings } from "@/components/settings/general-settings";
import { TenantInfo } from "@/components/settings/tenant-info";
import { ApiKeysSection } from "@/components/settings/api-keys";
import { EnvVarsSettings } from "@/components/settings/env-vars-settings";
import { TenantMembers } from "@/components/settings/tenant-members";
import { TenantOrgs } from "@/components/settings/tenant-orgs";
import { MemoryPersonalSettings } from "@/components/settings/memory-personal-settings";
import { MemoryWorkspaceSettings } from "@/components/settings/memory-workspace-settings";
import { BrowserConnectionSettings } from "@/components/settings/browser-connection-settings";
import { ChatHistorySettings } from "@/components/settings/chat-history-settings";

export default function WorkspaceSettingsPage() {
  return (
    <Suspense fallback={null}>
      <WorkspaceSettingsBody />
    </Suspense>
  );
}

function WorkspaceSettingsBody() {
  const { t } = useT();
  const router = useRouter();
  const params = useSearchParams();
  const auth = useAuth();

  const active = params.get("section") ?? "general";

  const currentRole =
    auth.memberships.find(
      (m) => String(m.tenant_id) === String(auth.selectedTenantId ?? auth.tenant?.id ?? ""),
    )?.role ?? "";
  const isSystemAdmin = auth.user?.is_system_admin === true;

  const visibleItems = WORKSPACE_NAV_GROUPS.flatMap((g) => g.items).filter((it) =>
    canSeeSection(it, currentRole, isSystemAdmin),
  );
  const activeItem = visibleItems.find((it) => it.key === active);

  useEffect(() => {
    // Hand-crafted ?section= values must not bypass the role gate — fall back
    // to the first visible section instead of rendering the panel anyway.
    if (!activeItem && visibleItems.length > 0) {
      router.replace(`/platform/system/workspace?section=${visibleItems[0].key}`);
    }
  }, [active, activeItem, visibleItems.length, router]);

  return (
    <div className="mx-auto w-full max-w-[1200px]">
      <div className="flex gap-10">
        {/* section nav */}
        <div className="w-[240px] shrink-0">
          {WORKSPACE_NAV_GROUPS.map((group) => {
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
                    onClick={() =>
                      router.replace(`/platform/system/workspace?section=${it.key}`)
                    }
                    className={`nav-item mb-0.5 ${active === it.key ? "active" : ""}`}
                  >
                    <span className="truncate">
                      {it.labelKey ? t(it.labelKey as never) : it.fallbackLabel}
                    </span>
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
            ) : null}
          </div>
        </div>
      </div>
    </div>
  );
}
