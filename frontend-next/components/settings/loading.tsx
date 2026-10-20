/* One loading placeholder.
 *
 * The pages used a bare "…", a translated string or a hardcoded English
 * "Loading…" depending on the file. This component always renders the
 * translated label.
 */
"use client";

import { useT } from "@/lib/i18n";

export function Loading({ label, className = "" }: { label?: string; className?: string }) {
  const { t } = useT();
  return (
    <div className={`flex items-center justify-center py-8 ${className}`} role="status" aria-live="polite">
      <span className="caption text-muted">{label ?? t("common.loading")}</span>
    </div>
  );
}

export default Loading;
