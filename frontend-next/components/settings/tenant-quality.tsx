"use client";

/* Settings modal section for Tenant Admins: the answer-quality inbox of
 * their own organization (disliked answers, ungrounded questions). The
 * SuperAdmin console has the same inbox for every organization
 * (pages/admin-quality.tsx). */

import { useState } from "react";
import { SectionHeader } from "@/components/settings/section-header";
import { DeniedView } from "@/components/settings/system-section";
import { QualityInbox } from "@/components/settings/quality-inbox";
import { useT } from "@/lib/i18n";

export default function TenantQuality() {
  const { t } = useT();
  const [denied, setDenied] = useState(false);
  if (denied) return <DeniedView />;
  return (
    <div className="space-y-5">
      <SectionHeader title={t("qinbox.title")} subtitle={t("qinbox.subtitle")} />
      <QualityInbox scope="tenant" onDenied={() => setDenied(true)} />
    </div>
  );
}
