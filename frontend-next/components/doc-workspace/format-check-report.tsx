"use client";

import { useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { Markdown } from "@/components/markdown";
import { DocMentionIcon } from "@/components/doc-mention";
import { useT } from "@/lib/i18n";
import { getDocumentFormatCheck, type DocumentFormatReport } from "@/lib/api/document-workspace";

/* The background format check's evaluation, shown as information in a
 * modal opened from the chat header's ring — not as a chat question and
 * answer. Fixing the errors stays a chat request (it edits the document). */
export function FormatCheckReportModal({
  open,
  sessionId,
  document,
  fixDisabled,
  onClose,
  onFix,
  onAskInChat,
}: {
  open: boolean;
  sessionId: string;
  /** The tab whose check is shown. */
  document: { id: string; file_name: string } | null;
  /** A chat turn is running: the fix waits. */
  fixDisabled: boolean;
  onClose: () => void;
  onFix: () => void;
  /** No kept evaluation (expired or from before a restart): ask instead. */
  onAskInChat: () => void;
}) {
  const { t } = useT();
  const [state, setState] = useState<
    { status: "loading" } | { status: "error" } | { status: "done"; report: DocumentFormatReport | null }
  >({ status: "loading" });
  const documentId = document?.id ?? "";

  useEffect(() => {
    if (!open || !documentId) return;
    let alive = true;
    setState({ status: "loading" });
    getDocumentFormatCheck(sessionId, documentId)
      .then((r) => alive && setState({ status: "done", report: r.report }))
      .catch(() => alive && setState({ status: "error" }));
    return () => {
      alive = false;
    };
  }, [open, sessionId, documentId]);

  const report = state.status === "done" ? state.report : null;
  const checkedAt = report?.checked_at ? new Date(report.checked_at) : null;

  return (
    <Modal open={open} title={t("docws.fcReportTitle")} onClose={onClose} width="w-[720px]">
      {document && (
        <div className="-mt-3 mb-4 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-[13px] text-muted">
          <span className="flex min-w-0 items-center gap-1.5 font-medium text-ink">
            <DocMentionIcon name={document.file_name} />
            <span className="truncate">{document.file_name}</span>
          </span>
          {report?.document_type_label && (
            <span className="caption rounded-full bg-surface-strong px-2 py-0.5 text-ink">{report.document_type_label}</span>
          )}
          {checkedAt && !Number.isNaN(checkedAt.getTime()) && (
            <span className="caption">{t("docws.fcReportCheckedAt", { time: checkedAt.toLocaleString() })}</span>
          )}
        </div>
      )}

      {state.status === "loading" && <p className="py-8 text-center text-[13px] text-muted">{t("docws.fcReportLoading")}</p>}

      {state.status === "error" && <p className="py-6 text-center text-[13px] text-error">{t("docws.fcReportError")}</p>}

      {state.status === "done" && !report && (
        <div className="flex flex-col items-center gap-3 py-6 text-center">
          <p className="max-w-[440px] text-[13px] text-muted">{t("docws.fcReportMissing")}</p>
          <button
            type="button"
            className="btn btn-outline btn-sm"
            disabled={fixDisabled}
            onClick={() => {
              onClose();
              onAskInChat();
            }}
          >
            {t("docws.fcRetry")}
          </button>
        </div>
      )}

      {report && (
        <>
          {report.summary && (
            <div className="mb-4 grid grid-cols-3 gap-2">
              <Stat label={t("docws.fcReportPass")} value={report.summary.pass} tone="text-[#1f7a3d] dark:text-[#6fd08f]" />
              <Stat label={t("docws.fcReportFail")} value={report.summary.fail} tone="text-error" />
              <Stat label={t("docws.fcReportWarn")} value={report.summary.warn} tone="text-[#a15c00] dark:text-[#f0b35a]" />
            </div>
          )}
          <div className="rounded-[10px] border border-hairline px-4 py-3 text-[14px]">
            <Markdown text={report.evaluation} />
          </div>
          <p className="caption mt-3 text-muted">{t("docws.fcReportManual")}</p>
          <div className="mt-4 flex flex-wrap justify-end gap-2">
            <button type="button" className="btn btn-outline btn-sm" onClick={onClose}>
              {t("docws.fcReportClose")}
            </button>
            <button
              type="button"
              className="btn btn-primary btn-sm disabled:opacity-50"
              disabled={fixDisabled}
              title={fixDisabled ? t("docws.fcReportBusy") : undefined}
              onClick={() => {
                onClose();
                onFix();
              }}
            >
              {t("docws.fcFix")}
            </button>
          </div>
        </>
      )}
    </Modal>
  );
}

function Stat({ label, value, tone }: { label: string; value: number; tone: string }) {
  return (
    <div className="rounded-[10px] border border-hairline px-3 py-2">
      <div className={`text-[18px] font-semibold leading-tight ${tone}`}>{value}</div>
      <div className="caption text-muted">{label}</div>
    </div>
  );
}
