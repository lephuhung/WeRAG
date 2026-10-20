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
import { ConsolePage } from "@/components/settings/console-page";
import { DataTable } from "@/components/settings/data-table";
import { Pager } from "@/components/settings/pager";
import { Pill, PillGroup } from "@/components/settings/pill";
import { Notice } from "@/components/settings/notice";
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
      return "badge-success";
    case "reviewing":
      return "badge-info";
    case "failed":
      return "badge-error";
    case "ready":
    case "fetching":
    case "ocr":
      return "badge-warning";
    default:
      return "badge-neutral";
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
  const [actionError, setActionError] = useState("");
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
    setActionError("");
    try {
      await discardLegalDraft(d.id);
      toast.success(t("ldraft.discarded"));
      await load(true);
    } catch (e) {
      setActionError(e instanceof Error && e.message ? e.message : t("ldraft.discardFailed"));
    } finally {
      setDeleting(null);
    }
  };

  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <ConsolePage size="wide">
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

      {error && (
        <Notice
          tone="error"
          className="mb-3"
          action={
            <button type="button" className="btn btn-outline btn-sm" onClick={() => void load()}>
              {t("common.retry")}
            </button>
          }
        >
          {error}
        </Notice>
      )}
      {actionError && <Notice tone="error" className="mb-3">{actionError}</Notice>}

      <PillGroup ariaLabel={t("common.filters")} className="mb-5 flex-wrap">
        {FILTERS.map((f) => (
          <Pill
            key={f.key}
            active={filter === f.key}
            onClick={() => {
              setFilter(f.key);
              setPage(1);
            }}
          >
            {t(f.labelKey)}
          </Pill>
        ))}
      </PillGroup>

      <DataTable<LegalDraftItem>
        columns={[
          { key: "document", header: t("ldraft.col.document"), className: "min-w-[280px]" },
          { key: "pages", header: t("ldraft.col.pages"), className: "w-[80px]" },
          { key: "status", header: t("ldraft.col.status"), className: "w-[150px]" },
          { key: "reviewer", header: t("ldraft.col.reviewer"), className: "w-[150px]" },
          { key: "dates", header: t("ldraft.col.dates"), className: "w-[170px]" },
          { key: "action", header: t("ldraft.col.action"), className: "min-w-[200px]" },
        ]}
        rows={items}
        rowKey={(d) => d.id}
        renderRow={(d) => (
          <DraftCells d={d} locale={locale} deleting={deleting} onDiscard={() => void discard(d)} />
        )}
        rowClassName="align-top"
        loading={loading}
        loadingLabel={t("common.loading")}
        emptyLabel={t("ldraft.empty")}
        minWidth="min-w-[980px]"
      />

      <Pager page={page} pageCount={pages} onPage={setPage} loading={loading} />
    </ConsolePage>
  );
}

/* The cells of one draft row (the <tr> comes from <DataTable>). */
function DraftCells({
  d,
  locale,
  deleting,
  onDiscard,
}: {
  d: LegalDraftItem;
  locale: string;
  deleting: string | null;
  onDiscard: () => void;
}) {
  const { t } = useT();
  const label = draftLabel(d);
  const reviewer = d.approved_by || d.reviewed_by;
  return (
    <>
      <td className="px-5 py-3.5 align-middle">
        <div className="text-[13px] font-semibold text-ink">{label || "—"}</div>
        {d.title && (
          <div className="caption mt-0.5 line-clamp-2 text-body" title={d.title}>
            {d.title}
          </div>
        )}
        {d.pdf_file_name && <div className="caption mt-0.5 truncate text-muted">{d.pdf_file_name}</div>}
      </td>
      <td className="px-5 py-3.5 align-middle">
        <span className="text-[13px] tabular-nums text-ink">{d.ocr_pages || "—"}</span>
      </td>
      <td className="px-5 py-3.5 align-middle">
        <span className={`badge-pill inline-block w-fit text-[10px] ${draftStatusTone(d.status)}`}>
          {t(`ldraft.status.${d.status}` as LocaleKey)}
        </span>
        {d.status === "failed" && d.error && (
          <div className="caption mt-0.5 line-clamp-3 text-error" title={d.error}>
            {d.error}
          </div>
        )}
        {d.structure && d.status !== "approved" && (
          <div className={`caption mt-0.5 ${d.structure.ready_to_approve ? "text-success" : "text-muted"}`}>
            {d.structure.ready_to_approve
              ? t("ldraft.structureReady")
              : t("ldraft.structureBlocking", { n: d.structure.blocking })}
          </div>
        )}
      </td>
      <td className="px-5 py-3.5 align-middle">
        <span className="caption text-body">{reviewer || "—"}</span>
      </td>
      <td className="px-5 py-3.5 align-middle">
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
      <td className="px-5 py-3.5 align-middle">
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
              className="btn btn-danger btn-sm py-0.5 text-[12px]"
              disabled={deleting === d.id}
              onClick={onDiscard}
            >
              {deleting === d.id ? t("common.loading") : t("ldraft.discard")}
            </button>
          )}
        </div>
      </td>
    </>
  );
}
