/* Tenant-wide knowledge-base access grants (backend kb_access_grants).
 * RETIRED: the tenant-wide grant clients were removed from
 * lib/api/knowledge, so this component keeps its export as a compatibility
 * alias only and performs no grant fetches. Cross-workspace reads now use
 * recipient-bound invitations (see KBInvitePanel): a Tenant Admin opens a
 * KB in /platform/knowledge-bases and invites one member at a time.
 */
"use client";

import Link from "next/link";
import { useT } from "@/lib/i18n";

export function KBAccessGrants() {
  const { t } = useT();
  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-hairline pb-5">
        <div>
          <h2 className="title-md font-semibold text-ink">{t("kbGrants.title")}</h2>
          <p className="caption text-muted mt-1">{t("kbGrants.desc")}</p>
        </div>
      </div>

      <div className="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-700 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-400">
        {t("kbGrants.retiredNoticePre")}{" "}
        <Link href="/platform/knowledge-bases" className="underline">
          {t("nav.knowledgeBases")}
        </Link>{" "}
        {t("kbGrants.retiredNoticePost")}
      </div>
    </div>
  );
}
