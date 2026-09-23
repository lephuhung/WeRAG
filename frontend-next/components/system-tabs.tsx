"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { roleAtLeast } from "@/components/settings/nav-config";
import { SYSTEM_TABS } from "@/components/system/system-nav";

/* Tab bar under /platform/system — five grouped areas. Models is open to
 * tenant owners (they managed models in the legacy settings page); Engines,
 * Extensions and Administration require the platform-wide system-admin flag.
 * Server-side guards stay authoritative. */
export function SystemTabs() {
  const pathname = usePathname();
  const { t } = useT();
  const auth = useAuth();
  const isSystemAdmin = auth.user?.is_system_admin === true;
  const currentRole =
    auth.memberships.find(
      (m) => String(m.tenant_id) === String(auth.selectedTenantId ?? auth.tenant?.id ?? ""),
    )?.role ?? "";

  const visible = SYSTEM_TABS.filter(
    (tab) =>
      tab.minRole === undefined ||
      isSystemAdmin ||
      (tab.minRole !== "system" && roleAtLeast(currentRole, tab.minRole)),
  );

  return (
    <div className="flex max-w-full items-center gap-1 overflow-x-auto rounded-full bg-surface-strong p-1">
      {visible.map((tab) => (
        <Link
          key={tab.href}
          href={tab.href}
          className={`shrink-0 whitespace-nowrap rounded-full px-3.5 py-1.5 text-[13px] font-medium transition-colors ${
            Boolean(pathname === tab.href || pathname?.startsWith(tab.href + "/"))
              ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.06)]"
              : "text-muted hover:text-ink"
          }`}
        >
          {tab.labelKey ? t(tab.labelKey as never) : tab.label}
        </Link>
      ))}
    </div>
  );
}
