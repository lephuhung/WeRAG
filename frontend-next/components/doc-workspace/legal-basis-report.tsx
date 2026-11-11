"use client";

import { useState } from "react";
import { Modal } from "@/components/modal";
import { DocMentionIcon } from "@/components/doc-mention";
import { useT } from "@/lib/i18n";
import { formatDate, formatDateTime } from "@/lib/datetime";
import {
  LEGAL_BASIS_STATUS_ORDER,
  type LegalBasisDocument,
  type LegalBasisFinding,
  type LegalBasisPosition,
  type LegalBasisRelation,
  type LegalBasisReport,
  type LegalBasisStatus,
} from "@/lib/api/document-workspace";

/* "Rà soát căn cứ pháp lý": the documents the open working document cites,
 * checked against the legal catalogue by code (no model). The report lists
 * them grouped by status — expired first — with the documents replacing or
 * amending them; the places can be selected or underlined in the editor.
 * The assistant only points out: nothing in the document is changed. */

const STATUS_TONE: Record<LegalBasisStatus, string> = {
  expired: "bg-error/10 text-error",
  suspended: "bg-error/10 text-error",
  partly_expired: "bg-warning/15 text-warning",
  not_yet_effective: "bg-warning/15 text-warning",
  ambiguous: "bg-warning/15 text-warning",
  amended: "bg-surface-strong text-ink",
  unknown: "bg-surface-strong text-muted",
  not_found: "bg-surface-strong text-muted",
  in_force: "bg-success/10 text-success",
};

const RELATION_ORDER: LegalBasisRelation[] = [
  "replaced_by",
  "repealed_by",
  "expired_by",
  "partly_expired_by",
  "suspended_by",
  "amended_by",
];

/** Starts the legal basis check of the visible document. */
export function LegalBasisButton({ busy, onClick }: { busy: boolean; onClick: () => void }) {
  const { t } = useT();
  return (
    <button
      type="button"
      disabled={busy}
      onClick={onClick}
      title={t("docws.lbStartHint")}
      aria-label={t("docws.lbStart")}
      aria-busy={busy || undefined}
      data-tip={t("docws.lbStartHint")}
      data-tip-side="top"
      data-tip-align="start"
      className="word-tip flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-muted hover:bg-surface-strong hover:text-ink disabled:cursor-wait disabled:opacity-60"
    >
      {busy ? (
        <svg className="h-4 w-4 animate-spin motion-reduce:animate-none" viewBox="0 0 24 24" fill="none" aria-hidden="true">
          <circle cx="12" cy="12" r="9" stroke="currentColor" strokeOpacity="0.25" strokeWidth="2.5" />
          <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" />
        </svg>
      ) : (
        /* scales of justice */
        <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M12 3v18M7 21h10M5 7h14M12 5l-7 2M12 5l7 2" />
          <path d="m5 7-3 6a3 3 0 0 0 6 0L5 7ZM19 7l-3 6a3 3 0 0 0 6 0l-3-6Z" />
        </svg>
      )}
    </button>
  );
}

export function LegalBasisReportModal({
  open,
  report,
  document,
  busy,
  onClose,
  onRecheck,
  onMark,
  onLocate,
}: {
  open: boolean;
  report: LegalBasisReport | null;
  /** The tab the report belongs to. */
  document: { id: string; file_name: string } | null;
  /** A check is running. */
  busy: boolean;
  onClose: () => void;
  onRecheck: () => void;
  /** Underlines the given places in the editor (nothing else changes). */
  onMark: (positions: LegalBasisPosition[]) => Promise<void>;
  /** Selects one place in the editor (it scrolls there); throws a message
   * when it cannot. */
  onLocate: (position: LegalBasisPosition) => Promise<void>;
}) {
  const { t, locale } = useT();
  const [marking, setMarking] = useState(false);
  const checkedAt = report?.checked_at ? new Date(report.checked_at) : null;
  const findings = report?.findings ?? [];
  const groups = LEGAL_BASIS_STATUS_ORDER.map((status) => ({
    status,
    items: findings.filter((f) => f.status === status),
  })).filter((g) => g.items.length > 0);
  const attention = findings.filter((f) => f.status !== "in_force").flatMap((f) => f.positions);
  const skipped = report?.skipped;
  const skippedParts = [
    skipped?.names_without_year ? t("docws.lbSkippedNames", { n: skipped.names_without_year }) : "",
    skipped?.own_number ? t("docws.lbSkippedOwn") : "",
    skipped?.paperwork ? t("docws.lbSkippedPaperwork", { n: skipped.paperwork }) : "",
  ].filter(Boolean);

  const mark = async (positions: LegalBasisPosition[]) => {
    setMarking(true);
    try {
      await onMark(positions);
    } finally {
      setMarking(false);
    }
  };

  return (
    <Modal open={open} title={t("docws.lbTitle")} onClose={onClose} width="w-[760px]">
      {document && (
        <div className="-mt-3 mb-4 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-[13px] text-muted">
          <span className="flex min-w-0 items-center gap-1.5 font-medium text-ink">
            <DocMentionIcon name={document.file_name} />
            <span className="truncate">{document.file_name}</span>
          </span>
          {checkedAt && !Number.isNaN(checkedAt.getTime()) && (
            <span className="caption">{t("docws.lbCheckedAt", { time: formatDateTime(checkedAt, locale) })}</span>
          )}
        </div>
      )}

      {!report && busy && <p className="py-8 text-center text-[13px] text-muted">{t("docws.lbRunning")}</p>}

      {report && (
        <>
          <p className="caption mb-3 rounded-[10px] bg-surface-strong px-3 py-2 text-ink">{t("docws.lbIntro")}</p>
          {report.unsaved && <p className="caption mb-3 text-warning">{t("docws.lbUnsaved")}</p>}
          {report.truncated && <p className="caption mb-3 text-warning">{t("docws.lbTruncated")}</p>}

          {findings.length === 0 ? (
            <p className="py-6 text-center text-[13px] text-muted">{t("docws.lbNone")}</p>
          ) : (
            <div className="mb-4 flex flex-wrap gap-1.5">
              {groups.map((g) => (
                <span key={g.status} className={`caption rounded-full px-2 py-0.5 ${STATUS_TONE[g.status]}`}>
                  {t(`docws.lbStatus.${g.status}`)}: {g.items.length}
                </span>
              ))}
            </div>
          )}

          <div className="flex flex-col gap-4">
            {groups.map((g) => (
              <section key={g.status}>
                <h3 className="mb-1.5 text-[13px] font-semibold text-ink">
                  {t(`docws.lbStatus.${g.status}`)} <span className="font-normal text-muted">({g.items.length})</span>
                </h3>
                <ul className="flex flex-col gap-2">
                  {g.items.map((f) => (
                    <FindingItem key={f.key} finding={f} marking={marking} onMark={mark} onLocate={onLocate} />
                  ))}
                </ul>
              </section>
            ))}
          </div>

          {skippedParts.length > 0 && (
            <p className="caption mt-4 text-muted">{t("docws.lbSkipped", { list: skippedParts.join("; ") })}</p>
          )}
          <p className="caption mt-2 text-muted">{t("docws.lbManual")}</p>

          <div className="mt-4 flex flex-wrap justify-end gap-2">
            <button type="button" className="btn btn-outline btn-sm" onClick={onClose}>
              {t("docws.fcReportClose")}
            </button>
            <button type="button" className="btn btn-outline btn-sm disabled:opacity-50" disabled={busy} onClick={onRecheck}>
              {t("docws.lbRecheck")}
            </button>
            <button
              type="button"
              className="btn btn-primary btn-sm disabled:opacity-50"
              disabled={marking || attention.length === 0}
              title={t("docws.lbMarkHint")}
              onClick={() => void mark(attention)}
            >
              {t("docws.lbMarkAll", { n: attention.length })}
            </button>
          </div>
        </>
      )}
    </Modal>
  );
}

function FindingItem({
  finding: f,
  marking,
  onMark,
  onLocate,
}: {
  finding: LegalBasisFinding;
  marking: boolean;
  onMark: (positions: LegalBasisPosition[]) => Promise<void>;
  onLocate: (position: LegalBasisPosition) => Promise<void>;
}) {
  const { t } = useT();
  const [locating, setLocating] = useState<number | null>(null);
  const [lost, setLost] = useState<{ index: number; message: string } | null>(null);
  const d = f.document ?? null;
  const locate = async (i: number) => {
    setLocating(i);
    setLost(null);
    try {
      await onLocate(f.positions[i]);
    } catch (e) {
      setLost({ index: i, message: e instanceof Error ? e.message : String(e) });
    } finally {
      setLocating(null);
    }
  };

  return (
    <li className="rounded-[10px] border border-hairline px-3 py-2.5">
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
        <span className="text-[14px] font-semibold text-ink">{f.citation}</span>
        {f.doc_type && !f.citation.startsWith(f.doc_type) && <span className="caption text-muted">{f.doc_type}</span>}
        <span className={`caption whitespace-nowrap rounded-full px-2 py-0.5 ${STATUS_TONE[f.status]}`}>
          {t(`docws.lbStatus.${f.status}`)}
        </span>
        {d?.unverified && <span className="caption text-warning">{t("docws.lbUnverified")}</span>}
      </div>
      {d && <DocLine doc={d} />}
      <StatusDetail finding={f} />

      {f.candidates && f.candidates.length > 0 && (
        <div className="mt-1.5">
          <p className="caption text-muted">
            {t(f.incomplete ? "docws.lbCandidatesIncomplete" : "docws.lbCandidates", { n: f.candidates_total ?? f.candidates.length })}
          </p>
          <ul className="mt-1 flex flex-col gap-1 border-l-2 border-hairline pl-2.5">
            {f.candidates.map((c) => (
              <li key={c.id} className="text-[12.5px] leading-snug text-ink">
                <span className="font-medium">{label(c)}</span>
                <span className={`caption ml-1.5 whitespace-nowrap rounded-full px-1.5 ${STATUS_TONE[c.status]}`}>
                  {t(`docws.lbStatus.${c.status}`)}
                </span>
                <DocLine doc={c} compact />
              </li>
            ))}
          </ul>
        </div>
      )}

      {f.related && f.related.length > 0 && (
        <div className="mt-1.5 flex flex-col gap-0.5">
          {RELATION_ORDER.map((rel) => {
            const docs = (f.related ?? []).filter((r) => r.relation === rel);
            if (docs.length === 0) return null;
            return (
              <p key={rel} className="text-[12.5px] leading-snug text-ink">
                <span className="text-muted">{t(`docws.lbRel.${rel}`)}: </span>
                {docs.map((r, i) => (
                  <span key={r.id}>
                    {i > 0 && ", "}
                    <span className="font-medium" title={r.title}>
                      {label(r)}
                    </span>
                    <span className="text-muted"> ({t(`docws.lbStatus.${r.status}`).toLowerCase()})</span>
                  </span>
                ))}
              </p>
            );
          })}
        </div>
      )}

      <div className="mt-2 flex flex-wrap items-center gap-1.5">
        <span className="caption text-muted">{t("docws.lbPlaces")}</span>
        {f.positions.map((p, i) => (
          <button
            key={`${p.paragraph}-${p.start}`}
            type="button"
            disabled={locating !== null}
            onClick={() => void locate(i)}
            title={t("docws.lbGoTo")}
            className="caption rounded-[6px] border border-hairline px-1.5 py-0.5 text-ink hover:bg-surface-strong disabled:cursor-wait"
          >
            {t("docws.lbParagraph", { n: p.paragraph + 1 })}
            {locating === i && " …"}
          </button>
        ))}
        {f.status !== "in_force" && (
          <button
            type="button"
            disabled={marking}
            onClick={() => void onMark(f.positions)}
            title={t("docws.lbMarkHint")}
            className="caption ml-auto rounded-[6px] px-1.5 py-0.5 text-muted underline hover:text-ink disabled:opacity-50"
          >
            {t("docws.lbMarkOne")}
          </button>
        )}
      </div>
      {lost && <p className="caption mt-1 text-error">{lost.message}</p>}
    </li>
  );
}

function label(d: LegalBasisDocument): string {
  return d.document_number ? `${d.doc_type} ${d.document_number}`.trim() : d.title;
}

/** Title, issuer and dates of a catalogue document. */
function DocLine({ doc, compact = false }: { doc: LegalBasisDocument; compact?: boolean }) {
  const { t } = useT();
  const dates = [
    doc.issued_on && t("docws.lbIssued", { date: formatDate(doc.issued_on) }),
    doc.effective_on && t("docws.lbEffective", { date: formatDate(doc.effective_on) }),
    doc.expired_on && t("docws.lbExpiredOn", { date: formatDate(doc.expired_on) }),
  ].filter(Boolean);
  return (
    <p className={`${compact ? "text-[12px]" : "mt-0.5 text-[12.5px]"} leading-snug text-muted`}>
      {!compact && doc.title && <span className="text-ink">{doc.title}</span>}
      {!compact && doc.title && (doc.issuer || dates.length > 0) && " · "}
      {compact && doc.title && <span>{doc.title} · </span>}
      {[doc.issuer, ...dates].filter(Boolean).join(" · ")}
      {doc.catalog_status && (
        <span>
          {" · "}
          {t("docws.lbCatalogStatus", { status: doc.catalog_status })}
        </span>
      )}
    </p>
  );
}

/** What the status rests on, when the list of related documents does not
 * say it: a status inferred from a replacement, a not-found citation. */
function StatusDetail({ finding: f }: { finding: LegalBasisFinding }) {
  const { t } = useT();
  const lines: string[] = [];
  if (f.derived_from) {
    lines.push(
      t("docws.lbDerived", {
        number: f.derived_from,
        date: f.derived_expired_on ? formatDate(f.derived_expired_on) : "—",
      }),
    );
  }
  if (f.maybe_expired) lines.push(t("docws.lbMaybeExpired"));
  if (f.status === "not_found") lines.push(t("docws.lbNotFoundHint"));
  if (f.status === "unknown") lines.push(t("docws.lbUnknownHint"));
  if (f.status === "partly_expired") lines.push(t("docws.lbPartlyHint"));
  if (lines.length === 0) return null;
  return (
    <>
      {lines.map((l) => (
        <p key={l} className="mt-1 text-[12.5px] leading-snug text-ink">
          {l}
        </p>
      ))}
    </>
  );
}
