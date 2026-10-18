"use client";

/* SuperAdmin list of OCR drafts: legal documents vbpl.vn publishes only as
 * a scanned PDF, fetched and OCRed for review before they are ingested.
 * Shown as the "OCR drafts" tab of the legal documents page
 * (admin-legal-documents.tsx); each draft is edited and approved on its
 * review page (app/platform/admin/legal-drafts/[id]). Backend:
 * /api/v1/system/legal-drafts — SuperAdmin-only. */

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { IconRefresh } from "@/components/icons";
import { useConfirm } from "@/components/confirm-dialog";
import { useToast } from "@/components/toast";
import { useT, type LocaleKey } from "@/lib/i18n";
import { ApiError } from "@/lib/api-client";
import { formatDateTime } from "@/lib/datetime";
import {
  discardLegalDraft,
  legalDraftBusy,
  legalDraftEditable,
  legalDraftHref,
  listLegalDrafts,
  type LegalDraftFilter,
  type LegalDraftItem,
} from "@/lib/api/legal-drafts";

const PAGE_SIZE = 50;
const POLL_MS = 5_000;

const FILTERS: { key: LegalDraftFilter; labelKey: LocaleKey }[] = [
  { key: "all", labelKey: "ldraft.filter.all" },
  { key: "ocr", labelKey: "ldraft.status.ocr" },
  { key: "ready", labelKey: "ldraft.status.ready" },
  { key: "reviewing", labelKey: "ldraft.status.reviewing" },
  { key: "approved", labelKey: "ldraft.status.approved" },
  { key: "failed", labelKey: "ldraft.status.failed" },
];

export function draftStatusTone(status: string): string {
  switch (status) {
    case "approved":
      return "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400";
    case "reviewing":
      return "bg-sky-500/10 text-sky-700 dark:text-sky-400";
    case "failed":
      return "bg-red-500/10 text-red-700 dark:text-red-400";
    case "ready":
    case "fetching":
    case "ocr":
      return "bg-amber-500/10 text-amber-700 dark:text-amber-400";
    default:
      return "bg-surface-strong text-muted";
  }
}

function draftLabel(d: { doc_type: string; document_number: string }): string {
  return [d.doc_type, d.document_number].filter(Boolean).join(" ");
}

export default function LegalDraftsSection({ onDenied }: { onDenied?: () => void }) {
  const { t, locale } = useT();
  const confirm = useConfirm();
  const toast = useToast();
  const [filter, setFilter] = useState<LegalDraftFilter>("all");
  const [items, setItems] = useState<LegalDraftItem[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [deleting, setDeleting] = useState<string | null>(null);

  const load = useCallback(
    async (quiet = false) => {
      if (!quiet) setLoading(true);
      setError("");
      try {
        const res = await listLegalDrafts({ status: filter, page, pageSize: PAGE_SIZE });
        setItems(res.data?.items ?? []);
        setTotal(res.data?.total ?? 0);
      } catch (e) {
        if (e instanceof ApiError && e.status === 403) onDenied?.();
        else if (!quiet) setError(e instanceof Error && e.message ? e.message : t("ldraft.loadFailed"));
      } finally {
        if (!quiet) setLoading(false);
      }
    },
    [filter, page, t, onDenied],
  );

  useEffect(() => {
    void load();
  }, [load]);

  // a draft still being OCRed: refresh the list until it is done
  const anyBusy = items.some((d) => legalDraftBusy(d.status));
  useEffect(() => {
    if (!anyBusy) return;
    const timer = setInterval(() => {
      if (document.visibilityState !== "hidden") void load(true);
    }, POLL_MS);
    return () => clearInterval(timer);
  }, [anyBusy, load]);

  const discard = async (d: LegalDraftItem) => {
    const ok = await confirm({
      title: t("ldraft.discardTitle"),
      message: t("ldraft.discardMessage", { number: draftLabel(d) || d.title || d.id }),
      confirmLabel: t("ldraft.discard"),
      danger: true,
    });
    if (!ok) return;
    setDeleting(d.id);
    try {
      await discardLegalDraft(d.id);
      toast.success(t("ldraft.discarded"));
      await load(true);
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t("ldraft.discardFailed"));
    } finally {
      setDeleting(null);
    }
  };

  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <div>
      <div className="mb-5 flex flex-wrap items-center gap-3">
        <span className="caption text-muted">{t("ldraft.desc")}</span>
        <div className="ml-auto flex items-center gap-2">
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

      {error && <p className="caption mb-3 text-error">{error}</p>}

      <div className="mb-5 inline-flex flex-wrap items-center gap-0.5 rounded-full border border-hairline bg-surface-strong/60 p-0.5">
        {FILTERS.map((f) => (
          <button
            key={f.key}
            type="button"
            onClick={() => {
              setFilter(f.key);
              setPage(1);
            }}
            className={`rounded-full px-3 py-1 text-[12px] font-medium transition-colors ${
              filter === f.key
                ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.08)]"
                : "text-muted hover:text-ink"
            }`}
          >
            {t(f.labelKey)}
          </button>
        ))}
      </div>

      <div className="card overflow-x-auto shadow-sm">
        <table className="w-full min-w-[980px] border-collapse text-left">
          <thead>
            <tr className="caption-uppercase border-b border-hairline bg-surface-strong/30 text-muted">
              <th className="min-w-[280px] px-5 py-3 font-medium">{t("ldraft.col.document")}</th>
              <th className="w-[80px] px-5 py-3 font-medium">{t("ldraft.col.pages")}</th>
              <th className="w-[150px] px-5 py-3 font-medium">{t("ldraft.col.status")}</th>
              <th className="w-[150px] px-5 py-3 font-medium">{t("ldraft.col.reviewer")}</th>
              <th className="w-[170px] px-5 py-3 font-medium">{t("ldraft.col.dates")}</th>
              <th className="min-w-[200px] px-5 py-3 font-medium">{t("ldraft.col.action")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-hairline">
            {items.map((d) => {
              const label = draftLabel(d);
              const reviewer = d.approved_by || d.reviewed_by;
              return (
                <tr key={d.id} className="align-top">
                  <td className="px-5 py-3.5">
                    <div className="text-[13px] font-semibold text-ink">{label || "—"}</div>
                    {d.title && (
                      <div className="caption mt-0.5 line-clamp-2 text-body" title={d.title}>
                        {d.title}
                      </div>
                    )}
                    {d.pdf_file_name && <div className="caption mt-0.5 truncate text-muted">{d.pdf_file_name}</div>}
                  </td>
                  <td className="px-5 py-3.5">
                    <span className="text-[13px] tabular-nums text-ink">{d.ocr_pages || "—"}</span>
                  </td>
                  <td className="px-5 py-3.5">
                    <span className={`badge-pill inline-block w-fit text-[10px] ${draftStatusTone(d.status)}`}>
                      {t(`ldraft.status.${d.status}` as LocaleKey)}
                    </span>
                    {d.status === "failed" && d.error && (
                      <div className="caption mt-0.5 line-clamp-3 text-error" title={d.error}>
                        {d.error}
                      </div>
                    )}
                    {d.structure && d.status !== "approved" && (
                      <div className={`caption mt-0.5 ${d.structure.ready_to_approve ? "text-emerald-700 dark:text-emerald-400" : "text-muted"}`}>
                        {d.structure.ready_to_approve
                          ? t("ldraft.structureReady")
                          : t("ldraft.structureBlocking", { n: d.structure.blocking })}
                      </div>
                    )}
                  </td>
                  <td className="px-5 py-3.5">
                    <span className="caption text-body">{reviewer || "—"}</span>
                  </td>
                  <td className="px-5 py-3.5">
                    <div className="caption tabular-nums text-body">
                      {t("ldraft.createdAt", { time: formatDateTime(d.created_at, locale) || "—" })}
                    </div>
                    {d.approved_at && (
                      <div className="caption tabular-nums text-muted">
                        {t("ldraft.approvedAt", { time: formatDateTime(d.approved_at, locale) })}
                      </div>
                    )}
                    {!d.approved_at && d.updated_at && d.updated_at !== d.created_at && (
                      <div className="caption tabular-nums text-muted">
                        {t("ldraft.updatedAt", { time: formatDateTime(d.updated_at, locale) })}
                      </div>
                    )}
                  </td>
                  <td className="px-5 py-3.5">
                    <div className="flex flex-wrap gap-1.5">
                      {legalDraftEditable(d.status) && (
                        <Link href={legalDraftHref(d.id)} className="btn btn-primary btn-sm py-0.5 text-[12px]">
                          {t("ldraft.edit")}
                        </Link>
                      )}
                      {d.status === "approved" && d.approved_kb_id && d.approved_knowledge_id && (
                        <Link
                          href={`/platform/knowledge-bases/${encodeURIComponent(d.approved_kb_id)}?knowledge_id=${encodeURIComponent(d.approved_knowledge_id)}`}
                          className="btn btn-outline btn-sm py-0.5 text-[12px]"
                        >
                          {t("ldraft.openKnowledge")}
                        </Link>
                      )}
                      {d.status !== "approved" && (
                        <button
                          type="button"
                          className="btn btn-ghost btn-sm py-0.5 text-[12px] text-error"
                          disabled={deleting === d.id}
                          onClick={() => void discard(d)}
                        >
                          {deleting === d.id ? "…" : t("ldraft.discard")}
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              );
            })}
            {items.length === 0 && (
              <tr>
                <td colSpan={6} className="px-5 py-12 text-center text-[14px] text-muted">
                  {loading ? t("usrp.loading") : t("ldraft.empty")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {pages > 1 && (
        <div className="mt-4 flex items-center justify-center gap-3">
          <button className="btn btn-outline btn-sm" disabled={page <= 1 || loading} onClick={() => setPage(page - 1)}>
            ‹
          </button>
          <span className="caption text-muted tabular-nums">
            {page} / {pages}
          </span>
          <button className="btn btn-outline btn-sm" disabled={page >= pages || loading} onClick={() => setPage(page + 1)}>
            ›
          </button>
        </div>
      )}
    </div>
  );
}
