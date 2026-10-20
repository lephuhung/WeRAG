/* The page shell every admin/settings section renders inside.
 *
 * Before this component each admin page re-implemented the same three
 * states — "still checking the role" (loading), "not allowed" (denied)
 * and "allowed" (content) — with slightly different max-widths. This
 * wrapper owns that skeleton; the denied view is shared with the pages
 * that learn about a 403 from the API (engine settings …) and with
 * components/require-system-access.tsx.
 */
"use client";

import type { ReactNode } from "react";
import { useT } from "@/lib/i18n";
import { ConsolePage } from "@/components/settings/console-page";
import { Loading } from "@/components/settings/loading";

/* Shown when the signed-in user may not open the section. */
export function DeniedView({
  title,
  message,
}: {
  title?: string;
  message?: string;
}) {
  const { t } = useT();
  return (
    <div className="mx-auto w-full max-w-[860px] px-5 py-16 text-center">
      <h2 className="title-md mb-2">{title ?? t("sys.deniedTitle")}</h2>
      <p className="body-sm text-muted">{message ?? t("sys.denied")}</p>
    </div>
  );
}

export function SystemSection({
  /* null = the role check is still running. */
  allowed,
  title,
  deniedMessage,
  loading = false,
  loadingLabel,
  children,
}: {
  allowed: boolean | null;
  title?: string;
  deniedMessage?: string;
  /* Shows the loading row inside the content area (data still fetching). */
  loading?: boolean;
  loadingLabel?: string;
  children: ReactNode;
}) {
  const { t } = useT();

  if (allowed === null) {
    return (
      <ConsolePage>
        <Loading label={loadingLabel ?? t("common.loading")} className="py-12" />
      </ConsolePage>
    );
  }

  if (!allowed) {
    return <DeniedView title={title} message={deniedMessage} />;
  }

  return (
    <ConsolePage>
      {loading ? <Loading label={loadingLabel ?? t("common.loading")} className="py-12" /> : children}
    </ConsolePage>
  );
}

export default SystemSection;
