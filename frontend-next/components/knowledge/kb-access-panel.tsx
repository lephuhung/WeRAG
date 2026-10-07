/* Sharing a knowledge base outside its unit: with a whole unit
 * (tenant-wide grant) or with individual people (recipient-bound invite). */
"use client";

import { useState } from "react";
import { useT } from "@/lib/i18n";
import { KBGrantPanel } from "@/components/knowledge/kb-grant-panel";
import { KBInvitePanel } from "@/components/knowledge/kb-invite-panel";

export function KBAccessPanel({
  kbId,
  kbName,
  ownerTenantId,
}: {
  kbId: string;
  kbName?: string;
  ownerTenantId?: number;
}) {
  const { t } = useT();
  const [tab, setTab] = useState<"units" | "people">("units");
  return (
    <div className="space-y-4">
      <div className="inline-flex items-center gap-0.5 rounded-full border border-hairline bg-surface-strong/60 p-0.5">
        {(["units", "people"] as const).map((k) => (
          <button
            key={k}
            type="button"
            onClick={() => setTab(k)}
            className={`rounded-full px-3 py-1 text-[12px] font-medium transition-colors ${
              tab === k ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.08)]" : "text-muted hover:text-ink"
            }`}
          >
            {k === "units" ? t("kbGrant.tabUnits") : t("kbGrant.tabPeople")}
          </button>
        ))}
      </div>
      {tab === "units" ? (
        <KBGrantPanel kbId={kbId} ownerTenantId={ownerTenantId} />
      ) : (
        <KBInvitePanel kbId={kbId} kbName={kbName} ownerTenantId={ownerTenantId} />
      )}
    </div>
  );
}
