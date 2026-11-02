"use client";

/* Legal identity review from a public KB's own page: each document card
 * shows its review state and opens the same dialog as Administration →
 * Legal documents; the toolbar scans new documents and counts what is
 * pending. SuperAdmin only — the server refuses everyone else
 * (/api/v1/system/legal-reviews). */

import { useCallback, useEffect, useState } from "react";
import { IconFileSearch } from "@/components/icons";
import { mismatches, ReviewDialog } from "@/components/settings/pages/admin-legal-documents";
import { useT, type LocaleKey } from "@/lib/i18n";
import {
  decideLegalReview,
  listLegalReviews,
  scanLegalReviews,
  type LegalReviewItem,
} from "@/lib/api/legal-review";

/* The server caps a page at 200. */
const PAGE_SIZE = 200;

/* The KB's review items keyed by knowledge id; disabled → empty. */
export function useKbLegalReviews(kbId: string, enabled: boolean) {
  const [items, setItems] = useState<Record<string, LegalReviewItem>>({});
  const [error, setError] = useState("");

  const reload = useCallback(async () => {
    if (!enabled) return;
    try {
      const all: LegalReviewItem[] = [];
      for (let page = 1; ; page++) {
        const res = await listLegalReviews({ knowledgeBaseId: kbId, page, pageSize: PAGE_SIZE });
        all.push(...(res.data ?? []));
        if (all.length >= (res.total ?? 0) || (res.data ?? []).length === 0) break;
      }
      setItems(Object.fromEntries(all.map((it) => [it.link.knowledge_id, it])));
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "");
    }
  }, [kbId, enabled]);

  useEffect(() => {
    if (enabled) void reload();
    else setItems({});
  }, [enabled, reload]);

  return { items, error, reload };
}

const STATE_TONE: Record<string, string> = {
  pending: "bg-warning-surface text-warning",
  approved: "bg-success-surface text-success",
  rejected: "bg-surface-strong text-muted",
};

/* Review state and actions on a document card. A pending document whose
 * header matched exactly one catalogue record (number, type and date
 * agree) is approved in one click with that record; otherwise "Duyệt"
 * opens the review dialog to pick or correct it. */
export function LegalReviewActions({
  item,
  onOpen,
  onDecided,
}: {
  item?: LegalReviewItem;
  onOpen: () => void;
  onDecided: () => Promise<void>;
}) {
  const { t } = useT();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  /* an approved document needs nothing more */
  if (!item || item.link.review_status === "approved") return null;
  const status = item.link.review_status;
  const mm = mismatches(item);
  const doc = item.document;
  const quick = !!doc && !mm.number && !mm.type && !mm.issued;
  const stop = (e: React.SyntheticEvent) => e.stopPropagation();

  const decide = async (action: "approve" | "reject") => {
    setBusy(true);
    setError("");
    try {
      await decideLegalReview(
        item.link.knowledge_id,
        action === "approve" ? { action, legal_document_id: doc?.id } : { action },
      );
      await onDecided();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("lrev.loadFailed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mt-2 flex flex-wrap items-center gap-1.5" onClick={stop} onKeyDown={stop}>
      <button
        type="button"
        onClick={onOpen}
        className={`badge-pill text-[10px] py-0 px-1.5 font-medium hover:ring-1 hover:ring-current ${STATE_TONE[status] ?? ""}`}
        title={t("lrev.kb.openReview")}
      >
        {t(`lrev.state.${status}` as LocaleKey)}
      </button>
      {status === "pending" && (
        <>
          <button
            type="button"
            disabled={busy}
            onClick={() => (quick ? void decide("approve") : onOpen())}
            className="btn btn-primary h-6 px-2 text-[11px]"
            title={quick && doc ? t("lrev.kb.quickApprove", { doc: doc.document_number || doc.title }) : t("lrev.kb.reviewNow")}
          >
            {busy ? "…" : quick ? t("lrev.approve") : t("lrev.kb.approveCheck")}
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={() => void decide("reject")}
            className="btn btn-outline h-6 px-2 text-[11px]"
          >
            {t("lrev.reject")}
          </button>
        </>
      )}
      {error && (
        <span className="caption w-full truncate text-error" title={error}>
          {error}
        </span>
      )}
    </div>
  );
}

/* Toolbar: pending count + scan for documents without a suggestion. */
export function KbLegalReviewBar({
  items,
  onScanned,
}: {
  items: Record<string, LegalReviewItem>;
  onScanned: () => Promise<void>;
}) {
  const { t } = useT();
  const [scanning, setScanning] = useState(false);
  const [notice, setNotice] = useState("");
  const pending = Object.values(items).filter((it) => it.link.review_status === "pending").length;

  const scan = async () => {
    setScanning(true);
    setNotice("");
    try {
      const res = await scanLegalReviews();
      await onScanned();
      setNotice(t("lrev.scanDone", { count: res.data?.created ?? 0 }));
    } catch (e) {
      setNotice(e instanceof Error ? e.message : t("lrev.loadFailed"));
    } finally {
      setScanning(false);
    }
  };

  return (
    <>
      {pending > 0 && (
        <span className={`badge-pill whitespace-nowrap text-[11px] ${STATE_TONE.pending}`}>
          {t("lrev.kb.pendingCount", { count: pending })}
        </span>
      )}
      {notice && <span className="caption max-w-[240px] truncate text-muted" title={notice}>{notice}</span>}
      <button
        type="button"
        onClick={() => void scan()}
        disabled={scanning}
        className="btn btn-outline btn-sm h-8"
        title={t("lrev.desc")}
      >
        <IconFileSearch className="h-3.5 w-3.5" />
        {scanning ? t("lrev.scanning") : t("lrev.scan")}
      </button>
    </>
  );
}

export { ReviewDialog };
