"use client";

import { SystemTabs } from "@/components/system-tabs";
import { useT } from "@/lib/i18n";

export default function SystemLayout({ children }: { children: React.ReactNode }) {
  const { t } = useT();
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <div className="flex shrink-0 flex-col gap-4 px-4 pt-5 pb-4 sm:px-6 sm:pt-8 sm:pb-6 lg:flex-row lg:items-end lg:justify-between lg:px-10">
        <div>
          <div className="caption-uppercase mb-3 text-muted">{t("hdr.administration")}</div>
          <h1 className="display-lg">{t("nav.system")}</h1>
        </div>
        <SystemTabs />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-6 sm:px-6 lg:px-10 lg:pb-10">{children}</div>
    </div>
  );
}
