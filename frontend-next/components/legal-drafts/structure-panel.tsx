"use client";

/* The structure check of a legal OCR draft: whether it can be approved,
 * the Phần > Chương > Mục > Điều tree, the khoản/điểm the legal parser
 * accepted, and the warnings by severity (blocking ones in red, info
 * muted). A heading or a warning position selects that paragraph in the
 * editor. */

import { useMemo } from "react";
import { IconErrorCircleFilled, IconInfoCircle, IconInfoCircleFilled, IconRefresh } from "@/components/icons";
import { useT, type LocaleKey } from "@/lib/i18n";
import { formatDateTime } from "@/lib/datetime";
import { headingLead, type LegalDraftHeading, type LegalDraftStructure, type LegalDraftWarning } from "@/lib/api/legal-drafts";

const WARNING_CODES = new Set([
  "too_few_articles",
  "article_gap",
  "article_duplicate",
  "article_order",
  "heading_without_title",
  "subdivision_rejected",
  "page_marker",
  "ocr_suspect",
]);

const SEVERITIES = ["error", "warning", "info"] as const;

function depthOf(headings: readonly LegalDraftHeading[], i: number): number {
  let depth = 0;
  let p = headings[i]?.parent ?? -1;
  // a malformed parent chain stops after a few steps
  while (p >= 0 && p < headings.length && depth < 4) {
    depth++;
    p = headings[p].parent;
  }
  return depth;
}

function severityOf(w: LegalDraftWarning): (typeof SEVERITIES)[number] {
  if (w.blocking || w.severity === "error") return "error";
  return w.severity === "info" ? "info" : "warning";
}

export function LegalDraftStructurePanel({
  structure,
  checking,
  error,
  locale,
  onCheck,
  onGoTo,
}: {
  structure: LegalDraftStructure | null;
  checking: boolean;
  error: string;
  locale: string;
  onCheck: () => void;
  /** Selects paragraph `index` in the editor (`lead`: how it starts). */
  onGoTo: (index: number, lead?: string, copyText?: string) => void;
}) {
  const { t } = useT();
  const headings = useMemo(() => structure?.headings ?? [], [structure]);
  const grouped = useMemo(() => {
    const out: Record<(typeof SEVERITIES)[number], LegalDraftWarning[]> = { error: [], warning: [], info: [] };
    for (const w of structure?.warnings ?? []) out[severityOf(w)].push(w);
    return out;
  }, [structure]);

  if (!structure) {
    return (
      <div className="flex flex-col items-start gap-2">
        <p className="caption text-muted">{t("ldraft.structure.none")}</p>
        {error && <p className="caption text-error">{error}</p>}
        <button type="button" className="btn btn-outline btn-sm" disabled={checking} onClick={onCheck}>
          {checking && <IconRefresh className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none" />}
          {checking ? t("ldraft.structure.checking") : t("ldraft.structure.check")}
        </button>
      </div>
    );
  }

  const sub = structure.subdivisions;
  const articles = structure.articles?.length ?? 0;

  return (
    <div className={`flex flex-col gap-4 ${checking ? "opacity-60" : ""}`}>
      {/* verdict */}
      <div className="flex flex-col gap-1">
        <span
          className={`badge-pill inline-block w-fit text-[11px] ${
            structure.ready_to_approve ? "bg-success-surface text-success" : "bg-error-surface text-error"
          }`}
        >
          {structure.ready_to_approve
            ? t("ldraft.structureReady")
            : t("ldraft.structureBlocking", { n: structure.blocking })}
        </span>
        <span className="caption tabular-nums text-muted-soft">
          {t("ldraft.structure.checkedAt", {
            time: formatDateTime(structure.checked_at, locale) || "—",
            revision: structure.revision,
          })}
        </span>
        {checking && <span className="caption text-muted">{t("ldraft.structure.checking")}</span>}
        {error && <span className="caption text-error">{error}</span>}
      </div>

      {/* counts */}
      <dl className="grid grid-cols-2 gap-x-3 gap-y-1 text-[12.5px]">
        <dt className="text-muted">{t("ldraft.structure.paragraphs")}</dt>
        <dd className="tabular-nums text-ink">{structure.paragraphs}</dd>
        <dt className="text-muted">{t("ldraft.structure.articles")}</dt>
        <dd className="tabular-nums text-ink">{articles}</dd>
        <dt className="text-muted">{t("ldraft.structure.khoan")}</dt>
        <dd className="tabular-nums text-ink">
          {t("ldraft.structure.accepted", { accepted: sub.khoan_accepted, total: sub.khoan_candidates })}
        </dd>
        <dt className="text-muted">{t("ldraft.structure.diem")}</dt>
        <dd className="tabular-nums text-ink">
          {t("ldraft.structure.accepted", { accepted: sub.diem_accepted, total: sub.diem_candidates })}
        </dd>
        <dt className="text-muted">{t("ldraft.structure.rejected")}</dt>
        <dd className={`tabular-nums ${sub.rejected > 0 ? "text-warning" : "text-ink"}`}>{sub.rejected}</dd>
        <dt className="text-muted">{t("ldraft.structure.pageMarkers")}</dt>
        <dd className="tabular-nums text-ink">{structure.page_markers}</dd>
      </dl>

      {/* warnings — one disclosure model for every severity */}
      {SEVERITIES.map((sev) => {
        const list = grouped[sev];
        if (list.length === 0) return null;
        const body = (
          <ul className="flex flex-col gap-1.5">
            {list.map((w, i) => (
              <WarningRow key={`${w.code}-${w.paragraph}-${i}`} warning={w} severity={sev} onGoTo={onGoTo} />
            ))}
          </ul>
        );
        const title = `${t(`ldraft.sev.${sev}` as LocaleKey)} (${list.length})`;
        return (
          <details key={sev} className="group" open={sev !== "info"}>
            <summary
              className={`caption-uppercase cursor-pointer ${sev === "error" ? "text-error" : sev === "warning" ? "text-warning" : "text-muted-soft"}`}
            >
              {title}
            </summary>
            <div className="mt-1.5">{body}</div>
          </details>
        );
      })}

      {/* tree */}
      <section>
        <h3 className="caption-uppercase mb-1.5 text-muted">{t("ldraft.structure.tree")}</h3>
        {headings.length === 0 ? (
          <p className="caption text-muted">{t("ldraft.structure.noHeadings")}</p>
        ) : (
          <ul className="-mx-1 flex flex-col">
            {headings.map((h, i) => {
              const lead = headingLead(h);
              const depth = depthOf(headings, i);
              return (
                <li key={`${h.paragraph}-${i}`}>
                  <button
                    type="button"
                    onClick={() => onGoTo(h.paragraph, lead, [lead, h.title].filter(Boolean).join(". "))}
                    className="flex w-full items-baseline gap-1.5 rounded-[6px] px-1 py-1 text-left hover:bg-surface-strong"
                    style={{ paddingLeft: `${4 + depth * 14}px` }}
                    title={t("ldraft.structure.goTo")}
                  >
                    <span
                      className={`shrink-0 text-[12.5px] ${h.kind === "dieu" ? "font-medium text-ink" : "font-semibold uppercase text-ink"}`}
                    >
                      {lead || "—"}
                    </span>
                    <span className={`min-w-0 truncate text-[12.5px] ${h.title ? "text-body" : "italic text-error"}`}>
                      {h.title || t("ldraft.structure.noTitle")}
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </div>
  );
}

function WarningRow({
  warning: w,
  severity,
  onGoTo,
}: {
  warning: LegalDraftWarning;
  severity: (typeof SEVERITIES)[number];
  onGoTo: (index: number) => void;
}) {
  const { t } = useT();
  const positions = [...new Set([w.paragraph, ...(w.paragraphs ?? [])].filter((p) => p >= 0))].slice(0, 12);
  /* The severity is carried by three channels, not by the colour alone:
     the left border, a distinct glyph and a screen-reader prefix. */
  const tone =
    severity === "error"
      ? "border border-error/30 border-l-4 border-l-error bg-error-surface"
      : severity === "warning"
        ? "border border-warning/30 border-l-4 border-l-warning bg-warning-surface"
        : "border border-hairline border-l-4 border-l-muted-soft bg-canvas-soft";
  const severityLabel = t(`ldraft.sev.${severity}` as LocaleKey);
  const SeverityIcon = severity === "error" ? IconErrorCircleFilled : severity === "warning" ? IconInfoCircleFilled : IconInfoCircle;
  const severityIconCls =
    severity === "error" ? "text-error" : severity === "warning" ? "text-warning" : "text-muted-soft";
  return (
    <li className={`rounded-md2 border px-2.5 py-1.5 ${tone}`}>
      <div className={`flex items-start gap-1.5 text-[12px] font-medium ${severity === "info" ? "text-muted" : "text-ink"}`}>
        <SeverityIcon className={`mt-[1px] h-3.5 w-3.5 shrink-0 ${severityIconCls}`} />
        <span>
          <span className="sr-only">{severityLabel}: </span>
          {WARNING_CODES.has(w.code) ? t(`ldraft.w.${w.code}` as LocaleKey) : w.code}
        </span>
      </div>
      {w.message && <div className={`mt-0.5 text-[12px] leading-snug ${severity === "info" ? "text-muted" : "text-body"}`}>{w.message}</div>}
      {positions.length > 0 && (
        <div className="mt-1 flex flex-wrap gap-1">
          {positions.map((p) => (
            <button
              key={p}
              type="button"
              onClick={() => onGoTo(p)}
              className="rounded-full bg-surface-strong px-1.5 py-0.5 text-[10.5px] tabular-nums text-muted hover:text-ink"
              title={t("ldraft.structure.goTo")}
            >
              {t("ldraft.structure.paragraph", { n: p + 1 })}
            </button>
          ))}
        </div>
      )}
    </li>
  );
}
