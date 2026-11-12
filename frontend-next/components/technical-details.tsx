"use client";

import { useT } from "@/lib/i18n";

/* A collapsed "Technical details" disclosure under a plain-language error:
 * the raw code and server message stay readable for an administrator
 * without being the first thing anyone sees. Renders nothing for "". */
export function TechnicalDetails({ detail, className = "" }: { detail?: string | null; className?: string }) {
  const { t } = useT();
  const text = (detail ?? "").trim();
  if (!text) return null;
  return (
    <details className={`mt-1 ${className}`}>
      <summary className="cursor-pointer select-none text-[11px] text-muted hover:text-ink">
        {t("common.technicalDetails")}
      </summary>
      <pre className="m-0 mt-1 max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-md border border-hairline bg-surface-card px-2.5 py-2 font-mono text-[11px] text-muted">
        {text}
      </pre>
    </details>
  );
}
