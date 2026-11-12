"use client";

import { parseErrorText } from "@/components/knowledge/trace-status";
import { useT } from "@/lib/i18n";

/* The body of a failed parse: a plain-language title and what to do about
 * it first, the raw code and server message tucked into "Technical details"
 * so an administrator can still read them. */
export function ParseErrorNotice({ code, message }: { code?: string | null; message?: string | null }) {
  const { t } = useT();
  const text = parseErrorText(code);
  const raw = (message ?? "").trim();
  const rawCode = (code ?? "").trim();
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <div className="flex items-center gap-2">
        <span className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-error text-[11px] font-bold text-white">
          !
        </span>
        <span className="caption font-semibold text-error">{t(text.title)}</span>
      </div>
      <div className="caption text-ink">{t(text.hint)}</div>
      {(rawCode || raw) && (
        <details className="mt-0.5">
          <summary className="cursor-pointer select-none text-[11px] text-muted hover:text-ink">
            {t("ks.err.technicalDetails")}
          </summary>
          <div className="mt-1 flex flex-col gap-1">
            {rawCode && <span className="font-mono text-[10px] text-muted-soft">{rawCode}</span>}
            {raw && (
              <pre className="m-0 max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-md border border-hairline bg-surface-card px-2.5 py-2 font-mono text-[11px] text-muted">
                {raw}
              </pre>
            )}
          </div>
        </details>
      )}
    </div>
  );
}
