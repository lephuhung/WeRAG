"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useT } from "@/lib/i18n";

const TABS = [
  { href: "/platform/system/overview", labelKey: "system.overview" },
  { href: "/platform/system/services", labelKey: "system.services" },
  { href: "/platform/system/models", labelKey: "system.models" },
  { href: "/platform/system/users", labelKey: "system.users" },
  { href: "/platform/system/logs", labelKey: "system.logs" },
] as const;

export function SystemTabs() {
  const pathname = usePathname();
  const { t } = useT();
  return (
    <div className="flex items-center gap-1 rounded-full bg-surface-strong p-1">
      {TABS.map((tab) => (
        <Link
          key={tab.href}
          href={tab.href}
          className={`rounded-full px-3.5 py-1.5 text-[13px] font-medium transition-colors ${
            pathname.startsWith(tab.href)
              ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.06)]"
              : "text-muted hover:text-ink"
          }`}
        >
          {t(tab.labelKey)}
        </Link>
      ))}
    </div>
  );
}
