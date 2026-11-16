/* SuperAdmin read-out of the BM25 index behind keyword retrieval. It loses
 * performance as it fragments, and does so invisibly: the cost it adds is
 * fixed per query rather than proportional to the rows matched, so nothing
 * about a slow search points at it. This deployment once reached 187
 * segments and 864 ms per keyword query, against 311 ms after a rebuild.
 *
 * Read-only on purpose. The remedy is a REINDEX, which holds SHARE on the
 * embeddings table and blocks ingest while it runs, so an operator picks the
 * moment rather than a button here. */
"use client";

import { type SearchIndexHealth } from "@/lib/api/system";
import { useT, type LocaleKey } from "@/lib/i18n";
import { formatDateTime } from "@/lib/datetime";
import { formatFileSize } from "@/components/use-attachments";
import { Notice } from "@/components/settings/notice";

/* The backend sends a code, not a sentence, so the wording is translated
 * here and a provider's error text stays in the server log. */
const REASON_KEY: Record<string, LocaleKey> = {
  not_configured: "q.searchIndexReasonNotConfigured",
  no_database: "q.searchIndexReasonUnavailable",
  not_postgres: "q.searchIndexReasonNotPostgres",
  pg_search_missing: "q.searchIndexReasonNoExtension",
  index_missing: "q.searchIndexReasonIndexMissing",
  query_failed: "q.searchIndexReasonQueryFailed",
};

export function SearchIndexHealthCard({ status }: { status: SearchIndexHealth }) {
  const { t, locale } = useT();

  const reasonText = t(REASON_KEY[status.reason ?? ""] ?? "q.searchIndexReasonUnavailable");
  const state = !status.available
    ? t("q.searchIndexUnavailable")
    : status.needs_rebuild
      ? t("q.searchIndexFragmented")
      : t("q.searchIndexHealthy");

  const rows: { label: string; value: string }[] = status.available
    ? [
        {
          label: t("q.searchIndexSegments"),
          value:
            status.threshold > 0
              ? t("q.searchIndexSegmentsOf", { count: status.segments, threshold: status.threshold })
              : String(status.segments),
        },
        { label: t("q.searchIndexDeleted"), value: status.deleted_docs.toLocaleString(locale) },
        {
          label: t("q.searchIndexSize"),
          value: `${formatFileSize(status.total_bytes)} (${formatFileSize(status.largest_bytes)})`,
        },
        { label: t("q.searchIndexChecked"), value: formatDateTime(status.checked_at, locale) },
      ]
    : [];

  return (
    <div className="card mb-6 p-5">
      <div className="mb-1 flex flex-wrap items-center justify-between gap-2">
        <span className="text-[14px] font-medium text-ink">{t("q.searchIndex")}</span>
        <span className={`caption font-medium ${status.needs_rebuild ? "text-warning" : "text-muted"}`}>
          {state}
        </span>
      </div>
      <p className="caption mb-4 text-muted">{t("q.searchIndexDesc")}</p>

      {status.available ? (
        <>
          <dl className="grid grid-cols-[repeat(auto-fit,minmax(min(100%,180px),1fr))] gap-4">
            {rows.map((r) => (
              <div key={r.label} className="min-w-0">
                <dt className="caption text-muted">{r.label}</dt>
                <dd className="text-[14px] text-ink">{r.value}</dd>
              </div>
            ))}
          </dl>
          {status.needs_rebuild ? (
            <Notice tone="warning" className="mt-4">
              {t("q.searchIndexRebuildHint", { index: status.index })}
            </Notice>
          ) : null}
        </>
      ) : (
        <p className="caption text-muted">{reasonText}</p>
      )}
    </div>
  );
}
