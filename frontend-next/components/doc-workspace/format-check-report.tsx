"use client";

import { useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { Markdown } from "@/components/markdown";
import { DocMentionIcon } from "@/components/doc-mention";
import { useT } from "@/lib/i18n";
import { formatDateTime } from "@/lib/datetime";
import { useToast } from "@/components/toast";
import {
  DocumentWorkspaceError,
  getDocumentFormatCheck,
  sendDocumentFormatFeedback,
  type DocumentFormatReport,
  type DocumentFormatVerdict,
} from "@/lib/api/document-workspace";
import { numberFormatReportSections } from "@/lib/format-findings";
import { FormatFindingReview, type FindingReviewActions } from "./format-finding-review";

/* The background format check's evaluation, shown as information in a
 * modal opened from the composer's format ring — not as a chat question and
 * answer. Its findings are reviewed one by one (go to the place, apply the
 * one formatting change a finding carries, check it again); fixing the rest
 * stays a chat request. */
export function FormatCheckReportModal({
  open,
  sessionId,
  document,
  fixDisabled,
  onClose,
  onFix,
  onAskInChat,
  onMark,
  onLocateFinding,
  onApplyFinding,
  onVerifyFinding,
}: FindingReviewActions & {
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
  /** Underlines the findings in the document (nothing is changed). */
  onMark?: () => Promise<void>;
}) {
  const { t, locale } = useT();
  const [marking, setMarking] = useState(false);
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
            <span className="caption">{t("docws.fcReportCheckedAt", { time: formatDateTime(checkedAt, locale) })}</span>
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
          {report.scope === "technical" && report.scope_reason && (
            <p className="caption mb-3 rounded-[10px] bg-surface-strong px-3 py-2 text-ink">
              {t("docws.fcScopeTechnical", { reason: report.scope_reason })}
            </p>
          )}
          <FormatFindingReview
            key={report.report_id || report.checked_at}
            raw={report.findings}
            revision={report.document_revision}
            disabled={fixDisabled}
            onLocateFinding={onLocateFinding}
            onApplyFinding={onApplyFinding}
            onVerifyFinding={onVerifyFinding}
          />
          <details open={!report.findings?.length} className="rounded-[10px] border border-hairline px-4 py-3 text-[14px]">
            <summary className="cursor-pointer text-[13px] font-medium text-muted hover:text-ink">
              {t("docws.fcRvFullReport")}
            </summary>
            <div className="mt-3">
              <Markdown text={numberFormatReportSections(report.evaluation)} />
            </div>
          </details>
          <p className="caption mt-3 text-muted">{t("docws.fcReportManual")}</p>
          {report.report_id && documentId && (
            <FormatFeedback key={report.report_id} sessionId={sessionId} documentId={documentId} report={report} />
          )}
          <div className="mt-4 flex flex-wrap justify-end gap-2">
            <button type="button" className="btn btn-outline btn-sm" onClick={onClose}>
              {t("docws.fcReportClose")}
            </button>
            {onMark && (
              <button
                type="button"
                className="btn btn-outline btn-sm disabled:opacity-50"
                disabled={marking}
                title={t("docws.fcMarkHint")}
                onClick={async () => {
                  setMarking(true);
                  try {
                    await onMark();
                  } finally {
                    setMarking(false);
                  }
                }}
              >
                {t("docws.fcMark")}
              </button>
            )}
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

/* Feedback on the report — a finding that is wrong, a document that is not
 * an NĐ30 văn bản hành chính, a wrong type, or a note. It is only recorded
 * (data for tuning the checker); the report itself is not changed. */
function FormatFeedback({
  sessionId,
  documentId,
  report,
}: {
  sessionId: string;
  documentId: string;
  report: DocumentFormatReport;
}) {
  const { t } = useT();
  const toast = useToast();
  const [open, setOpen] = useState(false);
  const [verdict, setVerdict] = useState<Exclude<DocumentFormatVerdict, "false_alarm"> | null>(null);
  const [wrong, setWrong] = useState<Set<string>>(new Set());
  const [note, setNote] = useState("");
  const [sending, setSending] = useState(false);
  const [sent, setSent] = useState(false);
  const findings = report.findings ?? [];
  const reportId = report.report_id ?? "";

  if (sent) return <p className="caption mt-3 text-[#1f7a3d] dark:text-[#6fd08f]">{t("docws.fcFeedbackThanks")}</p>;
  if (!open)
    return (
      <button type="button" className="caption mt-3 text-muted underline hover:text-ink" onClick={() => setOpen(true)}>
        {t("docws.fcFeedbackOpen")}
      </button>
    );

  const verdicts: { key: Exclude<DocumentFormatVerdict, "false_alarm">; label: string }[] = [
    { key: "not_nd30", label: t("docws.fcFeedbackNotNd30") },
    { key: "wrong_type", label: t("docws.fcFeedbackWrongType") },
    { key: "other", label: t("docws.fcFeedbackOther") },
  ];
  const canSend = !sending && (wrong.size > 0 || (verdict !== null && (verdict !== "other" || note.trim() !== "")));

  const send = async () => {
    setSending(true);
    try {
      const n = note.trim() || undefined;
      for (const id of wrong) {
        await sendDocumentFormatFeedback(sessionId, documentId, { report_id: reportId, verdict: "false_alarm", finding_id: id, note: n });
      }
      if (verdict) await sendDocumentFormatFeedback(sessionId, documentId, { report_id: reportId, verdict, note: n });
      setSent(true);
    } catch (e) {
      toast.error(e instanceof DocumentWorkspaceError && e.status === 409 ? t("docws.fcFeedbackStale") : t("docws.fcFeedbackFailed"));
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="mt-3 rounded-[10px] border border-hairline px-4 py-3">
      <p className="mb-2 text-[13px] font-medium text-ink">{t("docws.fcFeedbackTitle")}</p>
      {findings.length > 0 && (
        <div className="mb-3">
          <p className="caption mb-1.5 text-muted">{t("docws.fcFeedbackFindings")}</p>
          <ul className="flex flex-col gap-1">
            {findings.map((f) => (
              <li key={f.id}>
                <label className="flex cursor-pointer items-start gap-2 text-[13px] text-ink">
                  <input
                    type="checkbox"
                    className="mt-[3px]"
                    checked={wrong.has(f.id)}
                    onChange={(e) =>
                      setWrong((prev) => {
                        const next = new Set(prev);
                        if (e.target.checked) next.add(f.id);
                        else next.delete(f.id);
                        return next;
                      })
                    }
                  />
                  <span className="min-w-0">
                    {f.title}
                    {f.current && <span className="caption ml-1 text-muted">({f.current})</span>}
                  </span>
                </label>
              </li>
            ))}
          </ul>
        </div>
      )}
      <p className="caption mb-1.5 text-muted">{t("docws.fcFeedbackReport")}</p>
      <div className="mb-3 flex flex-wrap gap-1.5">
        {verdicts.map((v) => (
          <button
            key={v.key}
            type="button"
            aria-pressed={verdict === v.key}
            className={`btn btn-sm ${verdict === v.key ? "btn-primary" : "btn-outline"}`}
            onClick={() => setVerdict(verdict === v.key ? null : v.key)}
          >
            {v.label}
          </button>
        ))}
      </div>
      <textarea
        className="input h-auto min-h-[56px] w-full resize-y text-[13px]"
        maxLength={2000}
        placeholder={t("docws.fcFeedbackNote")}
        value={note}
        onChange={(e) => setNote(e.target.value)}
      />
      <div className="mt-2 flex justify-end gap-2">
        <button type="button" className="btn btn-outline btn-sm" onClick={() => setOpen(false)}>
          {t("docws.fcFeedbackCancel")}
        </button>
        <button type="button" className="btn btn-primary btn-sm disabled:opacity-50" disabled={!canSend} onClick={send}>
          {t("docws.fcFeedbackSend")}
        </button>
      </div>
    </div>
  );
}
