"use client";

import { useT } from "@/lib/i18n";
import { TEMPLATE_CHOICES } from "@/lib/template-protocol";
import type { DocumentTemplateId } from "@/lib/document-templates";
import type { TemplateNotice } from "./use-document-template";
import { IconClose } from "@/components/icons";

type TranslationKey = Parameters<ReturnType<typeof useT>["t"]>[0];

const LABELS: Record<DocumentTemplateId, TranslationKey> = {
  "cong-van": "docws.tplCongVan",
  "ke-hoach": "docws.tplKeHoach",
  "bao-cao": "docws.tplBaoCao",
  "to-trinh": "docws.tplToTrinh",
  "thong-bao": "docws.tplThongBao",
  "quyet-dinh": "docws.tplQuyetDinh",
  "nghi-quyet": "docws.tplNghiQuyet",
  "chi-thi": "docws.tplChiThi",
  "cong-dien": "docws.tplCongDien",
  "giay-moi": "docws.tplGiayMoi",
  "giay-gioi-thieu": "docws.tplGiayGioiThieu",
  "bien-ban": "docws.tplBienBan",
  "giay-nghi-phep": "docws.tplGiayNghiPhep",
};

/* Shown while the visible document is empty: pick a document type to have
 * the assistant draft an NĐ30 document. Picking only fills the composer; the
 * user adds details and sends. (The Word add-in's DocumentTemplatePicker.)
 * In a chat that already has messages it sits above the composer in a
 * compact form that can be closed (onClose). */
export function DocumentTemplatePicker({
  disabled,
  onChoose,
  onClose,
}: {
  disabled: boolean;
  onChoose: (id: DocumentTemplateId) => void;
  onClose?: () => void;
}) {
  const { t } = useT();
  const compact = !!onClose;
  return (
    <section
      aria-label={t("docws.tplTitle")}
      className={`rounded-[14px] border border-hairline bg-surface-card ${compact ? "mb-2 px-3 py-2.5" : "p-4"}`}
    >
      <div className={`flex items-start justify-between gap-2 ${compact ? "mb-2" : "mb-1"}`}>
        <p className={`${compact ? "text-[13px]" : "text-[15px]"} font-semibold text-ink`}>{t("docws.tplTitle")}</p>
        {onClose && (
          <button
            type="button"
            aria-label={t("docws.tplDismiss")}
            title={t("docws.tplDismiss")}
            className="shrink-0 rounded-[6px] p-1 text-muted hover:bg-surface-strong hover:text-ink"
            onClick={onClose}
          >
            <IconClose className="h-3 w-3" />
          </button>
        )}
      </div>
      {!compact && <p className="mb-3 text-[13px] leading-5 text-muted">{t("docws.tplDesc")}</p>}
      <div className={compact ? "flex flex-wrap gap-1.5" : "grid grid-cols-2 gap-2 sm:grid-cols-3"}>
        {TEMPLATE_CHOICES.map(([id]) => (
          <button
            type="button"
            key={id}
            disabled={disabled}
            onClick={() => onChoose(id)}
            className={`rounded-[10px] border border-hairline-strong bg-surface font-medium text-ink transition-colors hover:bg-surface-strong disabled:opacity-40 ${
              compact ? "px-2.5 py-1 text-[12px]" : "px-2 py-2.5 text-[13px]"
            }`}
          >
            {t(LABELS[id])}
          </button>
        ))}
      </div>
      {!compact && <p className="caption mt-3 text-muted">{t("docws.tplNote")}</p>}
    </section>
  );
}

/* Above the composer while a template request is open: what it is waiting
 * for, and a way out. */
export function DocumentTemplateBar({
  open,
  hint,
  notice,
  disabled,
  onCancel,
  onSkip,
}: {
  open: boolean;
  /** The type was just picked: say what to type. */
  hint: boolean;
  notice: TemplateNotice | null;
  disabled: boolean;
  onCancel: () => void;
  /** Build the document now, the missing details left blank to fill in. */
  onSkip?: () => void;
}) {
  const { t } = useT();
  const text = notice ? noticeText(notice, t) : "";
  if (!open && !text) return null;
  return (
    <div className="mb-2 text-[12px] leading-5">
      {open && (
        <div className="flex items-center justify-between gap-2 text-muted">
          <span>{t("docws.tplPreparing")}</span>
          <button type="button" disabled={disabled} className="shrink-0 underline hover:text-ink disabled:opacity-40" onClick={onCancel}>
            {t("docws.tplCancel")}
          </button>
        </div>
      )}
      {open && hint && !text && <p className="mt-1 text-muted">{t("docws.tplHint")}</p>}
      {text && (
        <p
          role="status"
          className={`mt-1 rounded-[10px] px-3 py-2 ${
            notice?.kind === "done"
              ? "bg-[#e8f4ec] text-[#14532d] dark:bg-[#12301e] dark:text-[#c7ecd3]"
              : notice?.kind === "failed" || notice?.kind === "invalid" || notice?.kind === "notEmpty"
                ? "bg-surface-strong text-error"
                : "bg-surface-strong text-ink"
          }`}
        >
          {text}
          {open && notice?.kind === "needs" && onSkip && (
            <button
              type="button"
              disabled={disabled}
              className="mt-1.5 block rounded-[8px] border border-hairline-strong bg-surface px-2.5 py-1 text-[12px] font-medium text-ink hover:bg-surface-card disabled:opacity-40"
              onClick={onSkip}
            >
              {t("docws.tplSkip")}
            </button>
          )}
        </p>
      )}
    </div>
  );
}

function noticeText(n: TemplateNotice, t: ReturnType<typeof useT>["t"]): string {
  switch (n.kind) {
    case "needs":
      return t("docws.tplNeeds", { fields: n.fields.join("; ") });
    case "failed":
      return t("docws.tplFailed", { error: n.error });
    case "building":
      return t("docws.tplBuilding");
    case "done":
      return t("docws.tplDone");
    case "invalid":
      return t("docws.tplInvalid");
    case "notEmpty":
      return t("docws.tplNotEmpty");
    case "otherDoc":
      return t("docws.tplOtherDoc");
    case "noData":
      return t("docws.tplNoData");
    case "skipped":
      return t("docws.tplSkipped");
  }
}
