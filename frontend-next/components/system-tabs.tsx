"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useAuth } from "@/lib/auth";
import { roleAtLeast, type SettingsRoleKey } from "@/components/settings/nav-config";

/* Tabs under /platform/system. Admin tabs are gated on the platform-wide
 * system-admin flag; Models is also open to tenant owners (they managed
 * models in the legacy settings page); Workspace is everyone's
 * personal/tenant settings. Server-side guards stay authoritative. */
const TABS: { href: string; label: string; minRole?: SettingsRoleKey }[] = [
  { href: "/platform/system/workspace", label: "Workspace" },
  { href: "/platform/system/overview", label: "Overview", minRole: "system" },
  { href: "/platform/system/services", label: "Engines", minRole: "system" },
  { href: "/platform/system/models", label: "Models", minRole: "owner" },
  { href: "/platform/system/extensions", label: "Extensions", minRole: "system" },
  { href: "/platform/system/agents", label: "Agents", minRole: "system" },
  { href: "/platform/system/users", label: "Users", minRole: "system" },
  { href: "/platform/system/logs", label: "Logs", minRole: "system" },
];

export function SystemTabs() {
  const pathname = usePathname();
  const auth = useAuth();
  const isSystemAdmin = auth.user?.is_system_admin === true;
  const currentRole =
    auth.memberships.find(
      (m) => String(m.tenant_id) === String(auth.selectedTenantId ?? auth.tenant?.id ?? ""),
    )?.role ?? "";

  const visible = TABS.filter(
    (tab) =>
      tab.minRole === undefined ||
      isSystemAdmin ||
      (tab.minRole !== "system" && roleAtLeast(currentRole, tab.minRole)),
  );

  return (
    <div className="flex items-center gap-1 rounded-full bg-surface-strong p-1">
      {visible.map((tab) => (
        <Link
          key={tab.href}
          href={tab.href}
          className={`rounded-full px-3.5 py-1.5 text-[13px] font-medium transition-colors ${
            Boolean(pathname?.startsWith(tab.href))
              ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.06)]"
              : "text-muted hover:text-ink"
          }`}
        >
          {tab.label}
        </Link>
      ))}
    </div>
  );
}
