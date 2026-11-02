"use client";

import { useMemo, useRef, useState } from "react";
import { useT } from "@/lib/i18n";
import { findingNavigation, parseFormatFindings, type FormatFinding, type ParagraphFormatOp } from "@/lib/format-findings";

export type FindingReviewActions = {
  /** Selects the finding in the editor (it scrolls there). */
  onLocateFinding?: (finding: FormatFinding) => Promise<void>;
  /** Applies the finding's one op, then reads the paragraph back. */
  onApplyFinding?: (finding: FormatFinding, revision: number) => Promise<{ verified: boolean }>;
  /** Reads the paragraph back: true when it has the op's formatting. */
  onVerifyFinding?: (finding: FormatFinding) => Promise<boolean>;
};

/* The report's findings one at a time (dots, Back / Next): go to the place,
 * apply its one formatting change, check it again in the editor. The
 * counts here are local to this review; the report's summary is not
 * changed. Ported from the Word add-in's FormatFindingReview. */
export function FormatFindingReview({
  raw,
  revision,
  disabled,
  onLocateFinding,
  onApplyFinding,
  onVerifyFinding,
}: FindingReviewActions & {
  raw: unknown;
  /** The revision the report checked; no revision → nothing is applied. */
  revision?: number;
  disabled: boolean;
}) {
  const { t } = useT();
  const findings = useMemo(() => parseFormatFindings(raw), [raw]);
  const [index, setIndex] = useState(0);
  const dots = useRef<(HTMLButtonElement | null)[]>([]);
  const [statuses, setStatuses] = useState<Record<string, "verified" | "unverified">>({});
  const [working, setWorking] = useState(false);
  const [error, setError] = useState("");
  const finding = findings[Math.min(index, findings.length - 1)];
  if (!finding) return <p className="caption mb-3 rounded-[10px] bg-surface-strong px-3 py-2 text-muted">{t("docws.fcRvNone")}</p>;

  const navigation = findingNavigation(finding);
  const locked = disabled || working;
  const canApply =
    finding.ops.length === 1 && Number.isInteger(revision) && (revision ?? -1) >= 0 && !!onApplyFinding;
  const verifiedCount = Object.values(statuses).filter((s) => s === "verified").length;
  const run = async (action: () => Promise<void>) => {
    setError("");
    setWorking(true);
    try {
      await action();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setWorking(false);
    }
  };
  const navigate = (next: number) => {
    const target = Math.max(0, Math.min(next, findings.length - 1));
    if (target === index || locked) return;
    setIndex(target);
    setError("");
    dots.current[target]?.scrollIntoView({ block: "nearest", inline: "nearest" });
  };
  const alignLabel = (a: NonNullable<ParagraphFormatOp["alignment"]>) =>
    t(
      a === "left"
        ? "docws.fcRvAlignLeft"
        : a === "right"
          ? "docws.fcRvAlignRight"
          : a === "center"
            ? "docws.fcRvAlignCenter"
            : "docws.fcRvAlignBoth",
    );
  const yesNo = (v: boolean) => t(v ? "docws.fcRvYes" : "docws.fcRvNo");
  const opSummary = (op: ParagraphFormatOp) =>
    [
      op.font !== undefined ? t("docws.fcRvFont", { v: op.font }) : "",
      op.sizePt !== undefined ? t("docws.fcRvSize", { v: op.sizePt }) : "",
      op.alignment ? t("docws.fcRvAlign", { v: alignLabel(op.alignment) }) : "",
      op.bold !== undefined ? t("docws.fcRvBold", { v: yesNo(op.bold) }) : "",
      op.italic !== undefined ? t("docws.fcRvItalic", { v: yesNo(op.italic) }) : "",
    ]
      .filter(Boolean)
      .join(" · ");

  return (
    <section
      className="mb-3 min-w-0 overflow-hidden rounded-[10px] border border-hairline px-4 py-3"
      aria-label={t("docws.fcRvTitle")}
      aria-roledescription="carousel"
    >
      <header className="mb-3 flex items-center gap-2 border-b border-hairline pb-2">
        <span className="shrink-0 whitespace-nowrap rounded-md bg-surface-strong px-2 py-1 text-[12px] font-semibold text-ink">
          {t("docws.fcRvItem", { n: index + 1, total: findings.length })}
        </span>
        <nav
          aria-label={t("docws.fcRvTitle")}
          className="min-w-0 flex-1 overflow-x-auto [scrollbar-width:thin]"
          onKeyDown={(event) => {
            if (locked || !["ArrowLeft", "ArrowRight"].includes(event.key)) return;
            event.preventDefault();
            const next = Math.max(0, Math.min(index + (event.key === "ArrowRight" ? 1 : -1), findings.length - 1));
            navigate(next);
            dots.current[next]?.focus();
          }}
        >
          <div className="mx-auto flex w-max items-center gap-0.5">
            {findings.map((item, at) => (
              <button
                key={item.id}
                ref={(element) => {
                  dots.current[at] = element;
                }}
                type="button"
                aria-label={t("docws.fcRvPick", { n: at + 1, title: item.title })}
                aria-current={at === index ? "step" : undefined}
                title={t("docws.fcRvPick", { n: at + 1, title: item.title })}
                disabled={locked}
                onClick={() => navigate(at)}
                className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full disabled:opacity-40"
              >
                <span
                  aria-hidden="true"
                  className={`h-1.5 rounded-full transition-[width,background-color] motion-reduce:transition-none ${
                    at === index
                      ? "w-4 bg-ink"
                      : statuses[item.id] === "verified"
                        ? "w-1.5 bg-success"
                        : "w-1.5 bg-hairline-strong"
                  }`}
                />
              </button>
            ))}
          </div>
        </nav>
        {verifiedCount > 0 && (
          <span className="caption shrink-0 whitespace-nowrap text-muted">
            {t("docws.fcRvVerifiedCount", { n: verifiedCount })}
          </span>
        )}
        {navigation && onLocateFinding && (
          <button
            type="button"
            aria-label={t(navigation === "document-start" ? "docws.fcRvGoStart" : "docws.fcRvGoTo")}
            title={t(navigation === "document-start" ? "docws.fcRvGoStart" : "docws.fcRvGoTo")}
            className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-muted transition-colors hover:bg-surface-strong hover:text-ink disabled:opacity-40"
            disabled={locked}
            onClick={() => void run(() => onLocateFinding(finding))}
          >
            <svg
              aria-hidden="true"
              viewBox="0 0 24 24"
              className="h-4 w-4"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.7"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              {navigation === "document-start" ? (
                <path d="M5 4h14M12 20V8m-5 5 5-5 5 5" />
              ) : (
                <>
                  <circle cx="12" cy="12" r="6" />
                  <path d="M12 2v4m0 12v4M2 12h4m12 0h4" />
                </>
              )}
            </svg>
          </button>
        )}
      </header>
      <p className="sr-only" role="status">
        {t("docws.fcRvItem", { n: index + 1, total: findings.length })}: {finding.title}
      </p>
      <div key={finding.id} role="group" aria-roledescription="slide" className="min-w-0 space-y-2 break-words">
        <h3 className="text-[14px] font-semibold text-ink">{finding.title}</h3>
        {finding.reason && <p className="text-[13px] text-ink">{finding.reason}</p>}
        {finding.anchor && (
          <blockquote className="max-h-28 overflow-y-auto whitespace-pre-wrap border-l-2 border-hairline-strong pl-2 text-[12px] text-muted">
            {finding.text || finding.anchor.text}
          </blockquote>
        )}
        {finding.current && (
          <p className="text-[13px] text-ink">
            <strong>{t("docws.fcRvCurrent")}</strong> {finding.current}
          </p>
        )}
        {finding.expected && (
          <p className="rounded-md2 bg-success-surface px-3 py-2 text-[13px] text-success">
            <strong>{t("docws.fcRvExpected")}</strong> {finding.expected}
          </p>
        )}
        {finding.ops.length > 0 && (
          <ul className="caption text-muted">
            {finding.ops.map((op, at) => (
              <li key={at}>{opSummary(op)}</li>
            ))}
          </ul>
        )}
        {statuses[finding.id] && (
          <p
            role="status"
            className={`caption ${statuses[finding.id] === "verified" ? "text-success" : "text-muted"}`}
          >
            {t(statuses[finding.id] === "verified" ? "docws.fcRvVerified" : "docws.fcRvUnverified")}
          </p>
        )}
        {(canApply || (finding.ops.length > 0 && onVerifyFinding)) && (
          <div className="flex flex-wrap gap-1.5">
            {canApply && (
              <button
                type="button"
                className="btn btn-primary btn-sm disabled:opacity-50"
                title={t("docws.fcRvApplyTip")}
                disabled={locked || statuses[finding.id] === "verified"}
                onClick={() =>
                  void run(async () => {
                    const result = await onApplyFinding!(finding, revision!);
                    setStatuses((prev) => ({ ...prev, [finding.id]: result.verified ? "verified" : "unverified" }));
                  })
                }
              >
                {t("docws.fcRvApply")}
              </button>
            )}
            {finding.ops.length > 0 && onVerifyFinding && (
              <button
                type="button"
                className="btn btn-outline btn-sm disabled:opacity-50"
                disabled={locked}
                onClick={() =>
                  void run(async () => {
                    const verified = await onVerifyFinding(finding);
                    setStatuses((prev) => ({ ...prev, [finding.id]: verified ? "verified" : "unverified" }));
                  })
                }
              >
                {t("docws.fcRvVerify")}
              </button>
            )}
          </div>
        )}
        {working && (
          <p role="status" className="caption text-muted">
            {t("docws.fcRvWorking")}
          </p>
        )}
        {error && (
          <p role="alert" className="caption text-error">
            {error}
          </p>
        )}
      </div>
      {findings.length > 1 && (
        <div className="mt-3 flex items-center justify-between gap-2 border-t border-hairline pt-2">
          <button
            type="button"
            className="btn btn-outline btn-sm disabled:opacity-40"
            disabled={locked || index === 0}
            onClick={() => navigate(index - 1)}
          >
            {t("docws.fcRvPrev")}
          </button>
          <button
            type="button"
            className="btn btn-outline btn-sm disabled:opacity-40"
            disabled={locked || index >= findings.length - 1}
            onClick={() => navigate(index + 1)}
          >
            {t("docws.fcRvNext")}
          </button>
        </div>
      )}
    </section>
  );
}
