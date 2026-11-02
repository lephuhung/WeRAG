"use client";

/* SuperAdmin review of provision links pushed by sources other than
 * vbpl.vn (thuvienphapluat…): "khoản X of A is amended by khoản Y of B".
 * Shown as the "Provision links" tab of the legal documents page
 * (admin-legal-documents.tsx). Approve copies a link into the links the
 * current-wording resolver reads; reject keeps it out (the feed's next push
 * does not ask again). Backend: /api/v1/system/legal-link-candidates —
 * SuperAdmin-only. Spec: docs/legal-document-feed-spec.md §9. */

import { useCallback, useEffect, useState } from "react";
import { IconRefresh } from "@/components/icons";
import { Modal } from "@/components/modal";
import { useToast } from "@/components/toast";
import { ConsolePage } from "@/components/settings/console-page";
import { DataTable } from "@/components/settings/data-table";
import { Pager } from "@/components/settings/pager";
import { Pill, PillGroup } from "@/components/settings/pill";
import { Notice } from "@/components/settings/notice";
import { FieldLabel } from "@/components/settings/field";
import { useT, type LocaleKey } from "@/lib/i18n";
import { ApiError } from "@/lib/api-client";
import {
  decideLegalLinkCandidate,
  listLegalLinkCandidates,
  type LegalLinkCandidate,
  type LegalLinkCounts,
  type LegalLinkMatchStatus,
  type LegalLinkReviewFilter,
} from "@/lib/api/legal-link-candidates";
import { provisionLabel } from "@/lib/legal-provision-label";

const PAGE_SIZE = 50;

const REVIEW_FILTERS: { key: LegalLinkReviewFilter; labelKey: LocaleKey }[] = [
  { key: "pending", labelKey: "llink.filter.pending" },
  { key: "approved", labelKey: "llink.filter.approved" },
  { key: "rejected", labelKey: "llink.filter.rejected" },
  { key: "auto", labelKey: "llink.filter.auto" },
  { key: "all", labelKey: "llink.filter.all" },
];

const MATCH_STATUSES: LegalLinkMatchStatus[] = ["matched", "doc_only", "target_missing", "unresolved", "duplicate"];

function matchTone(s: LegalLinkMatchStatus): string {
  switch (s) {
    case "matched":
      return "badge-success";
    case "duplicate":
      return "badge-info";
    case "doc_only":
      return "badge-warning";
    default:
      return "badge-error";
  }
}

function docText(number: string, title?: string): string {
  if (title && number && title.includes(number)) return title;
  return [number, title].filter(Boolean).join(" — ");
}

export default function LegalLinksSection({ onDenied }: { onDenied?: () => void }) {
  const { t } = useT();
  const toast = useToast();
  const [review, setReview] = useState<LegalLinkReviewFilter>("pending");
  const [match, setMatch] = useState<LegalLinkMatchStatus | "">("");
  const [items, setItems] = useState<LegalLinkCandidate[]>([]);
  const [total, setTotal] = useState(0);
  const [counts, setCounts] = useState<LegalLinkCounts>({});
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<number | null>(null);
  const [editing, setEditing] = useState<LegalLinkCandidate | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await listLegalLinkCandidates({ reviewStatus: review, matchStatus: match, page, pageSize: PAGE_SIZE });
      setItems(res.data?.items ?? []);
      setTotal(res.data?.total ?? 0);
      setCounts(res.data?.counts ?? {});
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) onDenied?.();
      else setError(e instanceof Error && e.message ? e.message : t("llink.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [review, match, page, t, onDenied]);

  useEffect(() => {
    void load();
  }, [load]);

  const decide = async (c: LegalLinkCandidate, action: "approve" | "reject") => {
    setBusy(c.id);
    setError("");
    try {
      await decideLegalLinkCandidate(c.id, { action });
      toast.success(t(action === "approve" ? "llink.approved" : "llink.rejected"));
      await load();
    } catch (e) {
      setError(e instanceof Error && e.message ? e.message : t("llink.decideFailed"));
    } finally {
      setBusy(null);
    }
  };

  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <ConsolePage size="wide">
      <div className="mb-5 flex flex-wrap items-center gap-3">
        <span className="caption text-muted">{t("llink.desc")}</span>
        <div className="ml-auto flex items-center gap-2">
          <select
            className="input h-8 py-0 text-[12.5px]"
            value={match}
            aria-label={t("llink.col.match")}
            onChange={(e) => {
              setMatch(e.target.value as LegalLinkMatchStatus | "");
              setPage(1);
            }}
          >
            <option value="">{t("llink.match.any")}</option>
            {MATCH_STATUSES.map((s) => (
              <option key={s} value={s}>
                {t(`llink.match.${s}` as LocaleKey)}
              </option>
            ))}
          </select>
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

      <PillGroup ariaLabel={t("common.filters")} className="mb-5 flex-wrap">
        {REVIEW_FILTERS.map((f) => (
          <Pill
            key={f.key}
            active={review === f.key}
            onClick={() => {
              setReview(f.key);
              setPage(1);
            }}
          >
            {t(f.labelKey)}
            {f.key !== "all" && (counts[f.key] ?? 0) > 0 ? ` (${counts[f.key]})` : ""}
          </Pill>
        ))}
      </PillGroup>

      <DataTable<LegalLinkCandidate>
        columns={[
          { key: "target", header: t("llink.col.target"), className: "min-w-[240px]" },
          { key: "op", header: t("llink.col.op"), className: "w-[130px]" },
          { key: "amender", header: t("llink.col.amender"), className: "min-w-[240px]" },
          { key: "match", header: t("llink.col.match"), className: "min-w-[220px]" },
          { key: "note", header: t("llink.col.note"), className: "min-w-[220px]" },
          { key: "action", header: t("llink.col.action"), className: "w-[200px]" },
        ]}
        rows={items}
        rowKey={(c) => String(c.id)}
        renderRow={(c) => (
          <LinkCells
            c={c}
            busy={busy === c.id}
            onApprove={() => void decide(c, "approve")}
            onReject={() => void decide(c, "reject")}
            onEdit={() => setEditing(c)}
          />
        )}
        rowClassName="align-top"
        loading={loading}
        loadingLabel={t("common.loading")}
        emptyLabel={t("llink.empty")}
        minWidth="min-w-[1180px]"
      />

      <Pager page={page} pageCount={pages} onPage={setPage} loading={loading} />

      {editing && (
        <EditDialog
          c={editing}
          onClose={() => setEditing(null)}
          onDone={async () => {
            setEditing(null);
            toast.success(t("llink.approved"));
            await load();
          }}
        />
      )}
    </ConsolePage>
  );
}

/* The cells of one candidate row (the <tr> comes from <DataTable>). */
function LinkCells({
  c,
  busy,
  onApprove,
  onReject,
  onEdit,
}: {
  c: LegalLinkCandidate;
  busy: boolean;
  onApprove: () => void;
  onReject: () => void;
  onEdit: () => void;
}) {
  const { t, locale } = useT();
  const pending = c.review_status === "pending";
  const canApprove = pending && c.amender_doc_id !== "" && c.target_path !== "";
  return (
    <>
      <td className="px-5 py-3.5">
        <div className="text-[13px] font-semibold text-ink">{provisionLabel(c.target_path, locale) || "—"}</div>
        <div className="caption mt-0.5 line-clamp-2 text-body" title={c.target?.title}>
          {docText(c.target?.document_number ?? c.target_doc_id, c.target?.title)}
        </div>
      </td>
      <td className="px-5 py-3.5">
        <span className="badge-pill inline-block w-fit text-[10.5px] badge-neutral">
          {t(`llink.op.${c.op}` as LocaleKey)}
        </span>
        {c.eff_from && (
          <div className="caption mt-1 tabular-nums text-muted">
            {t("llink.effFrom", { day: c.eff_from.slice(0, 10).split("-").reverse().join("/") })}
          </div>
        )}
      </td>
      <td className="px-5 py-3.5">
        <div className="text-[13px] font-semibold text-ink">
          {c.source_path ? provisionLabel(c.source_path, locale) : t("llink.noPath")}
        </div>
        <div className="caption mt-0.5 line-clamp-2 text-body" title={c.amender?.title}>
          {c.amender
            ? docText(c.amender.document_number, c.amender.title)
            : [c.amender_doc_number, c.amender_issue_date, c.amender_issuer].filter(Boolean).join(" · ")}
        </div>
      </td>
      <td className="px-5 py-3.5">
        <span className={`badge-pill inline-block w-fit text-[10.5px] ${matchTone(c.match_status)}`}>
          {t(`llink.match.${c.match_status}` as LocaleKey)}
        </span>
        {c.match_reason && <div className="caption mt-1 line-clamp-3 text-muted" title={c.match_reason}>{c.match_reason}</div>}
        {c.other_links.length > 0 && (
          <div className="caption mt-1 text-body">
            {t("llink.others", { n: c.other_links.length })}:{" "}
            {c.other_links
              .slice(0, 3)
              .map((l) => `${t(`llink.op.${l.op}` as LocaleKey)} (${l.source})`)
              .join(", ")}
          </div>
        )}
      </td>
      <td className="px-5 py-3.5">
        {c.note ? (
          <div className="caption line-clamp-4 text-body" title={c.note}>
            {c.note}
          </div>
        ) : (
          <span className="caption text-muted">—</span>
        )}
        <a href={c.feed_url} target="_blank" rel="noreferrer" className="caption mt-1 inline-block text-link hover:underline">
          {c.feed} · {c.feed_doc_id}
        </a>
      </td>
      <td className="px-5 py-3.5">
        {pending ? (
          <div className="flex flex-wrap gap-1.5">
            <button
              type="button"
              className="btn btn-primary btn-sm py-0.5 text-[12px]"
              disabled={busy || !canApprove}
              title={canApprove ? undefined : t("llink.needFix")}
              onClick={onApprove}
            >
              {t("llink.approve")}
            </button>
            <button type="button" className="btn btn-outline btn-sm py-0.5 text-[12px]" disabled={busy} onClick={onEdit}>
              {t("llink.edit")}
            </button>
            <button type="button" className="btn btn-danger btn-sm py-0.5 text-[12px]" disabled={busy} onClick={onReject}>
              {t("llink.reject")}
            </button>
          </div>
        ) : (
          <div className="caption text-muted">
            {t(`llink.filter.${c.review_status}` as LocaleKey)}
            {c.reviewed_by ? ` · ${c.reviewed_by}` : ""}
          </div>
        )}
      </td>
    </>
  );
}

/* Correct the amending document or a position, then approve. */
function EditDialog({ c, onClose, onDone }: { c: LegalLinkCandidate; onClose: () => void; onDone: () => Promise<void> }) {
  const { t } = useT();
  const [amender, setAmender] = useState(c.amender_doc_id);
  const [sourcePath, setSourcePath] = useState(c.source_path);
  const [targetPath, setTargetPath] = useState(c.target_path);
  const [note, setNote] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const save = async () => {
    setSaving(true);
    setError("");
    try {
      await decideLegalLinkCandidate(c.id, {
        action: "approve",
        amender_doc_id: amender.trim() || undefined,
        source_path: sourcePath.trim() || undefined,
        target_path: targetPath.trim() || undefined,
        note: note.trim() || undefined,
      });
      await onDone();
    } catch (e) {
      setError(e instanceof Error && e.message ? e.message : t("llink.decideFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal open title={t("llink.dialog.title")} onClose={onClose} width="w-[520px]">
      <div className="flex flex-col gap-4">
        {c.note && (
          <div className="rounded-[12px] border border-hairline bg-canvas-soft p-3">
            <div className="caption text-body">{c.note}</div>
          </div>
        )}
        <div className="flex flex-col gap-1">
          <FieldLabel htmlFor="llink-amender">{t("llink.f.amender")}</FieldLabel>
          <input
            id="llink-amender"
            className="input h-9 font-mono text-[13px]"
            value={amender}
            onChange={(e) => setAmender(e.target.value)}
          />
          <span className="caption text-muted-soft">{t("llink.f.amenderHint")}</span>
        </div>
        <div className="flex flex-col gap-1">
          <FieldLabel htmlFor="llink-source-path">{t("llink.f.sourcePath")}</FieldLabel>
          <input
            id="llink-source-path"
            className="input h-9 font-mono text-[13px]"
            value={sourcePath}
            placeholder="dieu:1/khoan:3"
            onChange={(e) => setSourcePath(e.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1">
          <FieldLabel htmlFor="llink-target-path">{t("llink.f.targetPath")}</FieldLabel>
          <input
            id="llink-target-path"
            className="input h-9 font-mono text-[13px]"
            value={targetPath}
            placeholder="dieu:5/khoan:2"
            onChange={(e) => setTargetPath(e.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1">
          <FieldLabel htmlFor="llink-note">{t("llink.f.note")}</FieldLabel>
          <textarea
            id="llink-note"
            className="input min-h-[72px] w-full resize-y py-2 text-[13px] leading-relaxed"
            value={note}
            onChange={(e) => setNote(e.target.value)}
          />
        </div>
        {error && <Notice tone="error">{error}</Notice>}
        <div className="flex justify-end gap-2 border-t border-hairline pt-4">
          <button className="btn btn-ghost btn-sm" onClick={onClose} disabled={saving}>
            {t("common.cancel")}
          </button>
          <button
            className="btn btn-primary btn-sm"
            onClick={() => void save()}
            disabled={saving || !amender.trim() || !targetPath.trim()}
          >
            {saving ? t("common.loading") : t("llink.saveApprove")}
          </button>
        </div>
      </div>
    </Modal>
  );
}
