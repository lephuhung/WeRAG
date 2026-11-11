"use client";

/* SuperAdmin console section: the answer-quality inbox of every
 * organization, with an organization filter (Tenant Admins see their own in
 * the settings modal, tenant-quality.tsx). */

import { useState } from "react";
import { ConsolePage } from "@/components/settings/console-page";
import { DeniedView } from "@/components/settings/system-section";
import { QualityInbox } from "@/components/settings/quality-inbox";
import { useT } from "@/lib/i18n";

export default function AdminQualityPage() {
  const { t } = useT();
  const [denied, setDenied] = useState(false);
  if (denied) return <DeniedView />;
  return (
    <ConsolePage size="wide">
      <p className="caption mb-4 text-muted">{t("qinbox.subtitle")}</p>
      <QualityInbox scope="platform" onDenied={() => setDenied(true)} />
    </ConsolePage>
  );
}
