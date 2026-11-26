"use client";

/* SuperAdmin "Thông báo": the platform's outgoing notification channels in
 * one page, one tab each — Email (the SMTP account) and Telegram (the
 * system alert bot). The tab follows ?tab=, and the old ?section=email /
 * ?section=telegram links open their tab. */

import { lazy, Suspense } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { TabStrip } from "@/components/settings/tab-strip";
import { Loading } from "@/components/settings/loading";
import { useT } from "@/lib/i18n";

const AdminEmail = lazy(() => import("@/components/settings/pages/admin-email"));
const AdminTelegram = lazy(() => import("@/components/settings/pages/admin-telegram"));

export type NotificationTab = "email" | "telegram";

export function notificationTabOf(tab: string | null, section: string | null): NotificationTab {
  if (tab === "email" || tab === "telegram") return tab;
  return section === "telegram" ? "telegram" : "email";
}

export default function AdminNotifications() {
  const { t } = useT();
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const tab = notificationTabOf(params.get("tab"), params.get("section"));

  const open = (next: NotificationTab) => {
    const q = new URLSearchParams(params.toString());
    q.set("section", "notifications");
    q.set("tab", next);
    router.replace(`${pathname}?${q.toString()}`, { scroll: false });
  };

  return (
    <div className="space-y-2">
      <TabStrip
        tabs={[
          { id: "email", label: t("systemNav.email") },
          { id: "telegram", label: t("systemNav.telegram") },
        ]}
        value={tab}
        onChange={open}
        ariaLabel={t("systemNav.notifications")}
      />
      <div id={`tabpanel-${tab}`} role="tabpanel" aria-labelledby={`tab-${tab}`}>
        <Suspense fallback={<Loading label={t("common.loading")} className="py-12" />}>
          {tab === "email" ? <AdminEmail key="email" /> : <AdminTelegram key="telegram" />}
        </Suspense>
      </div>
    </div>
  );
}
