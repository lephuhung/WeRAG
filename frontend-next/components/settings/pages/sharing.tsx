"use client";

import Link from "next/link";
import { RequireSystemAccess } from "@/components/require-system-access";
import { useT } from "@/lib/i18n";

/* Compatibility route: tenant-wide Sharing grants are retired (no grant
 * list endpoints are called here). Sharing is managed per knowledge base
 * through a recipient-bound invite issued by a Tenant Admin. */
export default function SharingPage() {
  const { t } = useT();
  return (
    <RequireSystemAccess minRole="admin">
      <div className="card p-4 sm:p-6 lg:p-8">
        <div className="space-y-8">
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
      </div>
    </RequireSystemAccess>
  );
}
