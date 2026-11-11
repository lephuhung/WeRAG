"use client";

/* SuperAdmin list of catalogue gaps: validity lookups (/platform/legal-
 * lookup) that found nothing, or found only documents mentioning a số hiệu
 * the catalogue lacks, counted per normalized query. Shown as the "Lookup
 * misses" tab of the legal documents page (admin-legal-documents.tsx).
 * "Bỏ qua" removes a row once the gap is settled (a later miss adds it back).
 * Backend: /api/v1/system/legal-lookup-misses — SuperAdmin-only. */

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { IconRefresh } from "@/components/icons";
import { useToast } from "@/components/toast";
import { ConsolePage } from "@/components/settings/console-page";
import { DataTable } from "@/components/settings/data-table";
import { Pager } from "@/components/settings/pager";
import { Pill, PillGroup } from "@/components/settings/pill";
import { Notice } from "@/components/settings/notice";
import { useT } from "@/lib/i18n";
import { ApiError } from "@/lib/api-client";
import { formatDateTime } from "@/lib/datetime";
import { legalLookupHref } from "@/lib/legal-status";
import { deleteLegalLookupMiss, listLegalLookupMisses, type LegalLookupMiss } from "@/lib/api/legal-lookup";

const PAGE_SIZE = 50;

export default function LegalLookupMissesSection({ onDenied }: { onDenied?: () => void }) {
  const { t, locale } = useT();
  const toast = useToast();
  const [sort, setSort] = useState<"count" | "recent">("count");
  const [items, setItems] = useState<LegalLookupMiss[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<number | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await listLegalLookupMisses({ sort, page, pageSize: PAGE_SIZE });
      setItems(res.data ?? []);
      setTotal(res.total ?? 0);
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) onDenied?.();
      else setError(t("lmiss.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [sort, page, t, onDenied]);

  useEffect(() => {
    void load();
  }, [load]);

  const dismiss = async (m: LegalLookupMiss) => {
    setBusy(m.id);
    setError("");
    try {
      await deleteLegalLookupMiss(m.id);
      toast.success(t("lmiss.dismissed"));
      await load();
    } catch {
      setError(t("lmiss.dismissFailed"));
    } finally {
      setBusy(null);
    }
  };

  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <ConsolePage size="wide">
      <div className="mb-5 flex flex-wrap items-center gap-3">
        <span className="caption text-muted">{t("lmiss.desc")}</span>
        <div className="ml-auto flex items-center gap-2">
          <PillGroup ariaLabel={t("lmiss.sort")}>
            <Pill active={sort === "count"} onClick={() => { setSort("count"); setPage(1); }}>
              {t("lmiss.sortCount")}
            </Pill>
            <Pill active={sort === "recent"} onClick={() => { setSort("recent"); setPage(1); }}>
              {t("lmiss.sortRecent")}
            </Pill>
          </PillGroup>
          <button
            className="btn btn-outline btn-sm h-8 w-8 p-0"
            onClick={() => void load()}
            disabled={loading}
            title={t("common.refresh")}
            aria-label={t("common.refresh")}
          >
            <IconRefresh className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
          </button>
        </div>
      </div>

      {error && <Notice tone="error" className="mb-3">{error}</Notice>}

      <DataTable<LegalLookupMiss>
        columns={[
          { key: "query", header: t("lmiss.col.query"), className: "min-w-[280px]" },
          { key: "reason", header: t("lmiss.col.reason"), className: "w-[200px]" },
          { key: "count", header: t("lmiss.col.count"), className: "w-[90px]" },
          { key: "last", header: t("lmiss.col.last"), className: "w-[170px]" },
          { key: "action", header: t("lmiss.col.action"), className: "w-[170px]" },
        ]}
        rows={items}
        rowKey={(m) => String(m.id)}
        renderRow={(m) => (
          <>
            <td className="px-5 py-3.5">
              <div className="text-[13px] font-medium text-ink">{m.sample || m.query}</div>
              {m.number && <div className="caption mt-0.5 text-muted">{t("lmiss.number", { number: m.number })}</div>}
            </td>
            <td className="px-5 py-3.5">
              <span className={`badge-pill text-[10.5px] ${m.reason === "number_missing" ? "badge-warning" : "badge-neutral"}`}>
                {t(m.reason === "number_missing" ? "lmiss.reason.number" : "lmiss.reason.none")}
              </span>
            </td>
            <td className="px-5 py-3.5">
              <span className="text-[13px] tabular-nums text-ink">{m.count}</span>
            </td>
            <td className="px-5 py-3.5">
              <span className="caption tabular-nums text-body">{formatDateTime(m.last_at, locale) || "—"}</span>
            </td>
            <td className="px-5 py-3.5">
              <div className="flex flex-wrap gap-1.5">
                <Link href={legalLookupHref(m.sample || m.query)} className="btn btn-outline btn-sm py-0.5 text-[12px]">
                  {t("lmiss.retry")}
                </Link>
                <button
                  type="button"
                  className="btn btn-outline btn-sm py-0.5 text-[12px]"
                  disabled={busy === m.id}
                  onClick={() => void dismiss(m)}
                >
                  {t("lmiss.dismiss")}
                </button>
              </div>
            </td>
          </>
        )}
        loading={loading}
        loadingLabel={t("common.loading")}
        emptyLabel={t("lmiss.empty")}
        minWidth="min-w-[900px]"
      />

      <Pager page={page} pageCount={pages} onPage={setPage} loading={loading} />
    </ConsolePage>
  );
}
