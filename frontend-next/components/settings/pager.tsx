/* One pager for the settings/admin surfaces.
 *
 * The legal pages had a numbered pager (‹ page / pages ›), the audit log
 * had a "Load more" button and several pages had no pagination at all.
 * This component renders the numbered pager and — for cursor based lists
 * that cannot offer page numbers — the "load more" variant.
 */
"use client";

import { useT } from "@/lib/i18n";

export function Pager({
  page,
  pageCount,
  onPage,
  loading = false,
  className = "",
}: {
  /* 1-based current page. */
  page: number;
  pageCount: number;
  onPage: (page: number) => void;
  loading?: boolean;
  className?: string;
}) {
  const { t } = useT();
  if (pageCount <= 1) return null;
  return (
    <nav className={`mt-4 flex items-center justify-center gap-3 ${className}`} aria-label={t("common.pagination")}>
      <button
        type="button"
        className="btn btn-outline btn-sm"
        disabled={page <= 1 || loading}
        onClick={() => onPage(page - 1)}
        aria-label={t("common.prev")}
      >
        ‹
      </button>
      <span className="caption tabular-nums text-muted" aria-live="polite">
        {page} / {pageCount}
      </span>
      <button
        type="button"
        className="btn btn-outline btn-sm"
        disabled={page >= pageCount || loading}
        onClick={() => onPage(page + 1)}
        aria-label={t("common.next")}
      >
        ›
      </button>
    </nav>
  );
}

/* Cursor based variant for endpoints that only expose "after_id". */
export function LoadMore({
  onMore,
  loading = false,
  label,
  className = "",
}: {
  onMore: () => void;
  loading?: boolean;
  label?: string;
  className?: string;
}) {
  const { t } = useT();
  return (
    <div className={`mt-4 flex justify-center ${className}`}>
      <button type="button" className="btn btn-outline btn-sm" onClick={onMore} disabled={loading}>
        {loading ? t("common.loading") : (label ?? t("common.loadMore"))}
      </button>
    </div>
  );
}

export default Pager;
