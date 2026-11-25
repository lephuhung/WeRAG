/* Labels on the document assistant's spelling underlines and NĐ30 format
 * check reports, as training data: a table (the paragraph with the flagged
 * span highlighted, the suggestion, the verdict) and a JSONL export of the
 * filtered set. Tenant Admin sees their own organization; the SuperAdmin
 * every organization, with an organization filter. Server guards decide.
 * Each label is reviewed in its row (accept the suggestion, type the right
 * form, not a mistake, skip) to build the training set; the page opens on
 * the unreviewed ones, so it works as a queue. */
"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { IconCheck, IconClose, IconDownload, IconRefresh } from "@/components/icons";
import { Select } from "@/components/select";
import { DataTable } from "@/components/settings/data-table";
import { Notice } from "@/components/settings/notice";
import { Pager } from "@/components/settings/pager";
import { SectionHeader } from "@/components/settings/section-header";
import { useToast } from "@/components/toast";
import { useAuth } from "@/lib/auth";
import { formatDate, formatTime } from "@/lib/datetime";
import { useT, type LocaleKey } from "@/lib/i18n";
import { listAllTenants, type TenantInfo } from "@/lib/api/tenants";
import {
  exportDocumentLabels,
  getDocumentLabelFacets,
  listDocumentLabels,
  reviewDocumentLabel,
  type DocumentLabel,
  type DocumentLabelFilters,
  type DocumentLabelReviewVerdict,
} from "@/lib/api/document-labels";
import { userErrorMessage } from "@/lib/user-error";

const PAGE_SIZE = 50;
/* The list pages through the first 2,000 rows (server cap); the export has all. */
const MAX_ROWS = 2000;
/* Code points of a long paragraph shown around the flagged span. */
const PARAGRAPH_WINDOW = 220;

const VERDICTS: Record<string, LocaleKey> = {
  not_error: "labels.verdict.not_error",
  accepted: "labels.verdict.accepted",
  fixed: "labels.verdict.fixed",
  false_alarm: "labels.verdict.false_alarm",
  not_nd30: "labels.verdict.not_nd30",
  wrong_type: "labels.verdict.wrong_type",
  other: "labels.verdict.other",
};
const REVIEWS: Record<string, LocaleKey> = {
  error: "labels.review.error",
  not_error: "labels.review.not_error",
  skip: "labels.review.skip",
};
const SPELLING_VERDICTS = ["not_error", "accepted", "fixed"];
const FORMAT_VERDICTS = ["false_alarm", "not_nd30", "wrong_type", "other"];

/* The paragraph with the flagged span marked; long ones are cut around it.
 * Offsets are code points, so slice Array.from(text), not the UTF-16 string. */
function HighlightedParagraph({ label }: { label: DocumentLabel }) {
  const chars = Array.from(label.paragraph || "");
  if (chars.length === 0) return <span className="text-muted">—</span>;
  const s = label.span_start;
  const e = label.span_end;
  const has = s !== null && e !== null && s >= 0 && e <= chars.length && s < e;
  let from = 0;
  let to = chars.length;
  if (chars.length > 2 * PARAGRAPH_WINDOW) {
    const mid = has ? s : 0;
    from = Math.max(0, mid - PARAGRAPH_WINDOW / 2);
    to = Math.min(chars.length, Math.max(has ? e : 0, from + PARAGRAPH_WINDOW) + PARAGRAPH_WINDOW / 2);
  }
  const lead = from > 0 ? "…" : "";
  const tail = to < chars.length ? "…" : "";
  if (!has) return <span>{lead + chars.slice(from, to).join("") + tail}</span>;
  return (
    <span>
      {lead}
      {chars.slice(from, s).join("")}
      <mark className="rounded bg-amber-200/70 px-0.5 text-ink dark:bg-amber-400/30">{chars.slice(s, e).join("")}</mark>
      {chars.slice(e, to).join("")}
      {tail}
    </span>
  );
}

/* Whether a label with this review verdict belongs in the list under the
 * review filter (a reviewed row leaves the "unreviewed" queue). */
function matchesReview(filter: string | undefined, verdict: string): boolean {
  if (!filter) return true;
  if (filter === "unreviewed") return verdict === "";
  if (filter === "reviewed") return verdict !== "";
  return verdict === filter;
}

/* One row's review: the four buttons, the inline "type it" input, or the
 * review given with "change" / "undo". */
function ReviewCell({
  label,
  pending,
  onReview,
}: {
  label: DocumentLabel;
  pending: boolean;
  onReview: (verdict: DocumentLabelReviewVerdict | "", correction?: string) => void;
}) {
  const { t, locale } = useT();
  const [draft, setDraft] = useState<string | null>(null);
  const [reopened, setReopened] = useState(false);
  // what "Đồng ý" accepts: the user's own correction of a "fixed" row (what
  // they typed instead of the model's suggestion), else the suggestion
  const suggestion = (label.verdict === "fixed" && label.user_correction?.trim()) || label.suggestion.trim();
  const reviewed = label.review_verdict !== "";

  const save = (verdict: DocumentLabelReviewVerdict | "", correction?: string) => {
    setDraft(null);
    setReopened(false);
    onReview(verdict, correction);
  };
  const saveDraft = () => {
    const v = (draft ?? "").trim();
    if (v) save("error", v);
  };

  if (draft !== null) {
    return (
      <div className="flex items-center gap-1">
        <input
          type="text"
          className="input h-[26px] min-w-0 flex-1 px-2 text-[12.5px]"
          value={draft}
          maxLength={255}
          autoFocus
          aria-label={t("labels.review.correction")}
          placeholder={t("labels.review.inputHint")}
          title={t("labels.review.inputHint")}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              saveDraft();
            } else if (e.key === "Escape") {
              e.preventDefault();
              setDraft(null);
            }
          }}
        />
        <button
          type="button"
          className="btn btn-ghost btn-row px-1.5"
          onClick={saveDraft}
          disabled={pending || !draft.trim()}
          aria-label={t("common.save")}
          title={t("common.save")}
        >
          <IconCheck className="h-3.5 w-3.5" />
        </button>
        <button
          type="button"
          className="btn btn-ghost btn-row px-1.5 text-muted"
          onClick={() => setDraft(null)}
          aria-label={t("common.cancel")}
          title={t("common.cancel")}
        >
          <IconClose className="h-3.5 w-3.5" />
        </button>
      </div>
    );
  }

  if (reviewed && !reopened) {
    const tone =
      label.review_verdict === "error"
        ? "bg-accent-surface text-accent"
        : label.review_verdict === "not_error"
          ? "bg-success-surface text-success"
          : "bg-surface-strong text-muted";
    return (
      <div className="flex flex-col gap-1">
        <span className={`self-start rounded px-1.5 py-0.5 text-[12px] font-medium break-words ${tone}`}>
          {REVIEWS[label.review_verdict] ? t(REVIEWS[label.review_verdict]) : label.review_verdict}
          {label.review_verdict === "error" && label.review_correction ? ` → ${label.review_correction}` : ""}
        </span>
        {label.reviewed_at && (
          <span className="caption tabular-nums text-muted">
            {formatTime(label.reviewed_at, locale)} {formatDate(label.reviewed_at)}
          </span>
        )}
        <div className="flex flex-wrap gap-x-3 text-[12px]">
          <button type="button" className="text-accent hover:underline disabled:opacity-50" disabled={pending} onClick={() => setReopened(true)}>
            {t("labels.review.redo")}
          </button>
          <button type="button" className="text-muted hover:text-ink hover:underline disabled:opacity-50" disabled={pending} onClick={() => save("")}>
            {t("labels.review.undo")}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-wrap gap-1">
      <span title={suggestion ? undefined : t("labels.review.noSuggestion")}>
        <button
          type="button"
          className="btn btn-row btn-soft-accent"
          disabled={pending || !suggestion}
          onClick={() => save("error", suggestion)}
        >
          {t("labels.review.accept")}
        </button>
      </span>
      <button type="button" className="btn btn-row btn-soft" disabled={pending} onClick={() => setDraft(label.review_correction || label.user_correction || label.suggestion)}>
        {t("labels.review.edit")}
      </button>
      <button type="button" className="btn btn-row btn-soft" disabled={pending} onClick={() => save("not_error")}>
        {t("labels.review.markNotError")}
      </button>
      <button type="button" className="btn btn-row btn-ghost text-muted" disabled={pending} onClick={() => save("skip")}>
        {t("labels.review.markSkip")}
      </button>
      {reopened && (
        <button type="button" className="btn btn-row btn-ghost px-1.5 text-muted" onClick={() => setReopened(false)} aria-label={t("common.cancel")} title={t("common.cancel")}>
          <IconClose className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  );
}

export function DocumentLabels() {
  const { t, locale } = useT();
  const toast = useToast();
  const auth = useAuth();
  const isSystemAdmin = auth.user?.is_system_admin === true;

  // the page is a review queue: it opens on the labels not reviewed yet
  const [filters, setFilters] = useState<DocumentLabelFilters>({ review: "unreviewed" });
  const [page, setPage] = useState(1);
  const [items, setItems] = useState<DocumentLabel[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [exporting, setExporting] = useState(false);
  const [models, setModels] = useState<string[]>([]);
  const [tenants, setTenants] = useState<TenantInfo[]>([]);
  const [pending, setPending] = useState<Record<string, boolean>>({});
  // set when reviews emptied the page, so the next rows are fetched
  const drained = useRef(false);

  const set = (patch: Partial<DocumentLabelFilters>) => {
    setFilters((f) => ({ ...f, ...patch }));
    setPage(1);
  };

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await listDocumentLabels(filters, page, PAGE_SIZE);
      setItems(res.data?.items ?? []);
      setTotal(res.data?.total ?? 0);
    } catch {
      // keep the last total so the export button and pager stay meaningful
      setItems([]);
      setError(t("labels.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [filters, page, t]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (drained.current && !loading && items.length === 0) {
      drained.current = false;
      if (total > 0) void load();
    }
  }, [items.length, loading, total, load]);

  /* Saves a review optimistically: the row shows it (or leaves the list
   * when it no longer matches the review filter) at once, takes the
   * server's copy on success and is put back on failure. */
  const review = async (l: DocumentLabel, verdict: DocumentLabelReviewVerdict | "", correction = "") => {
    if (pending[l.id]) return;
    const index = items.findIndex((x) => x.id === l.id);
    const stays = matchesReview(filters.review, verdict);
    const optimistic: DocumentLabel = {
      ...l,
      review_verdict: verdict,
      review_correction: verdict === "error" ? correction : "",
      reviewed_at: verdict ? new Date().toISOString() : null,
    };
    if (stays) {
      setItems((cur) => cur.map((x) => (x.id === l.id ? optimistic : x)));
    } else {
      setItems((cur) => cur.filter((x) => x.id !== l.id));
      setTotal((n) => Math.max(0, n - 1));
      drained.current = true;
    }
    setPending((p) => ({ ...p, [l.id]: true }));
    try {
      const res = await reviewDocumentLabel(l.id, {
        review_verdict: verdict,
        review_correction: verdict === "error" ? correction : "",
        review_note: l.review_note ?? "",
      });
      const saved = res.data;
      // no success toast: the row shows the review (or leaves the queue)
      if (stays && saved) setItems((cur) => cur.map((x) => (x.id === l.id ? saved : x)));
    } catch (e) {
      drained.current = false;
      if (stays) {
        setItems((cur) => cur.map((x) => (x.id === l.id ? l : x)));
      } else {
        setItems((cur) => (cur.some((x) => x.id === l.id) ? cur : [...cur.slice(0, Math.max(0, index)), l, ...cur.slice(Math.max(0, index))]));
        setTotal((n) => n + 1);
      }
      toast.error(userErrorMessage(e, t, "labels.review.saveFailed"));
    } finally {
      setPending((p) => {
        const next = { ...p };
        delete next[l.id];
        return next;
      });
    }
  };

  useEffect(() => {
    getDocumentLabelFacets(filters.tenant_id)
      .then((res) => setModels(res.data?.models ?? []))
      .catch(() => setModels([]));
  }, [filters.tenant_id]);

  useEffect(() => {
    if (!isSystemAdmin) return;
    listAllTenants()
      .then((res) => setTenants(res.data?.items ?? []))
      .catch(() => setTenants([]));
  }, [isSystemAdmin]);

  const onExport = async () => {
    setExporting(true);
    try {
      const blob = await exportDocumentLabels(filters);
      // a complete file ends with {"_end":true,...}; warn when it was cut off
      const tail = (await blob.slice(Math.max(0, blob.size - 512)).text()).trimEnd();
      let complete = false;
      try {
        complete = JSON.parse(tail.slice(tail.lastIndexOf("\n") + 1))?._end === true;
      } catch {
        complete = false;
      }
      if (!complete) toast.error(t("labels.exportIncomplete"));
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      const d = new Date();
      const stamp = `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, "0")}${String(d.getDate()).padStart(2, "0")}`;
      a.href = url;
      a.download = `nhan-chinh-ta-the-thuc-${stamp}.jsonl`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      toast.error(userErrorMessage(e, t, "labels.exportFailed"));
    } finally {
      setExporting(false);
    }
  };

  const verdictOptions = useMemo(() => {
    const keys =
      filters.kind === "spelling" ? SPELLING_VERDICTS : filters.kind === "format" ? FORMAT_VERDICTS : [...SPELLING_VERDICTS, ...FORMAT_VERDICTS];
    return [{ value: "", label: t("labels.allVerdicts") }, ...keys.map((k) => ({ value: k, label: t(VERDICTS[k]) }))];
  }, [filters.kind, t]);

  const pageCount = Math.max(1, Math.ceil(Math.min(total, MAX_ROWS) / PAGE_SIZE));
  /* Shares of the table width, not pixels: the paragraph column is the one
   * without a width, so it takes everything the others leave (≈35 % for the
   * SuperAdmin, ≈48 % otherwise) instead of being squeezed to a sliver. */
  const columns = [
    { key: "time", header: t("labels.col.time"), className: "w-[11%]" },
    ...(isSystemAdmin ? [{ key: "org", header: t("labels.col.organization"), className: "w-[13%]" }] : []),
    { key: "paragraph", header: t("labels.col.paragraph") },
    { key: "suggestion", header: t("labels.col.suggestion"), className: "w-[14%]" },
    { key: "verdict", header: t("labels.col.verdict"), className: "w-[13%]" },
    { key: "review", header: t("labels.col.review"), className: "w-[19%]" },
  ];

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        title={t("labels.title")}
        subtitle={isSystemAdmin ? t("labels.subtitleAll") : t("labels.subtitleOwn")}
        actions={
          <>
            <button type="button" className="btn btn-outline btn-sm" onClick={() => void load()} disabled={loading}>
              <IconRefresh className="h-4 w-4" />
              {t("common.refresh")}
            </button>
            <button type="button" className="btn btn-primary btn-sm" onClick={() => void onExport()} disabled={exporting || total === 0}>
              <IconDownload className="h-4 w-4" />
              {exporting ? t("labels.exporting") : t("labels.export", { n: total })}
            </button>
          </>
        }
      />

      <Notice tone="info">{t("labels.privacyNote")}</Notice>

      <div className="flex flex-wrap items-end gap-3">
        <label className="flex flex-col gap-1">
          <span className="caption text-muted">{t("labels.from")}</span>
          <input
            type="date"
            className="input h-9 w-[160px]"
            value={filters.from ?? ""}
            onChange={(e) => set({ from: e.target.value || undefined })}
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="caption text-muted">{t("labels.to")}</span>
          <input
            type="date"
            className="input h-9 w-[160px]"
            value={filters.to ?? ""}
            onChange={(e) => set({ to: e.target.value || undefined })}
          />
        </label>
        {isSystemAdmin && (
          <div className="w-[220px]">
            <Select
              ariaLabel={t("labels.col.organization")}
              value={filters.tenant_id ? String(filters.tenant_id) : ""}
              onChange={(v) => set({ tenant_id: v ? Number(v) : undefined, model: undefined })}
              searchable
              options={[
                { value: "", label: t("labels.allOrganizations") },
                ...tenants.map((tn) => ({ value: String(tn.id), label: tn.name || `#${tn.id}` })),
              ]}
            />
          </div>
        )}
        <div className="w-[200px]">
          <Select
            ariaLabel={t("labels.review.filter")}
            value={filters.review ?? ""}
            onChange={(v) => set({ review: v || undefined })}
            options={[
              { value: "", label: t("labels.review.all") },
              { value: "unreviewed", label: t("labels.review.unreviewed") },
              { value: "reviewed", label: t("labels.review.reviewed") },
              { value: "error", label: t("labels.review.error") },
              { value: "not_error", label: t("labels.review.not_error") },
              { value: "skip", label: t("labels.review.skip") },
            ]}
          />
        </div>
        <div className="w-[160px]">
          <Select
            ariaLabel={t("labels.kind")}
            value={filters.kind ?? ""}
            onChange={(v) => set({ kind: v || undefined, verdict: undefined, source: v === "format" ? undefined : filters.source })}
            options={[
              { value: "", label: t("labels.allKinds") },
              { value: "spelling", label: t("labels.kind.spelling") },
              { value: "format", label: t("labels.kind.format") },
            ]}
          />
        </div>
        <div className="w-[220px]">
          <Select ariaLabel={t("labels.col.verdict")} value={filters.verdict ?? ""} onChange={(v) => set({ verdict: v || undefined })} options={verdictOptions} />
        </div>
        {filters.kind !== "format" && (
          <div className="w-[200px]">
            <Select
              ariaLabel={t("labels.source")}
              value={filters.source ?? ""}
              onChange={(v) => set({ source: v || undefined })}
              options={[
                { value: "", label: t("labels.allSources") },
                { value: "user", label: t("labels.source.user") },
                { value: "verifier", label: t("labels.source.verifier") },
              ]}
            />
          </div>
        )}
        <div className="w-[220px]">
          <Select
            ariaLabel={t("labels.col.model")}
            value={filters.model ?? ""}
            onChange={(v) => set({ model: v || undefined })}
            options={[{ value: "", label: t("labels.allModels") }, ...models.map((m) => ({ value: m, label: m }))]}
          />
        </div>
      </div>

      {error && <Notice tone="error">{error}</Notice>}

      <DataTable
        columns={columns}
        rows={items}
        rowKey={(l) => l.id}
        loading={loading}
        emptyLabel={t("labels.empty")}
        minWidth="min-w-[1040px]"
        fixedLayout
        dense
        renderRow={(l) => (
          <>
            {/* time over date, so the column stays narrow */}
            <td className="px-3 py-2.5 align-top text-[13px] tabular-nums text-muted">
              <div className="whitespace-nowrap">{formatTime(l.created_at, locale)}</div>
              <div className="whitespace-nowrap text-[12px]">{formatDate(l.created_at)}</div>
            </td>
            {isSystemAdmin && (
              <td className="px-3 py-2.5 align-top text-[13px] break-words">{l.organization_name || `#${l.organization_id}`}</td>
            )}
            <td className="px-3 py-2.5 align-top text-[13px] leading-relaxed break-words">
              <div className="mb-1 flex flex-wrap items-center gap-1.5">
                <span className="badge-pill text-[10px] uppercase">{t(l.kind === "format" ? "labels.kind.format" : "labels.kind.spelling")}</span>
                {l.finding_title && <span className="caption text-muted">{l.finding_title}</span>}
              </div>
              <HighlightedParagraph label={l} />
              {l.note && <p className="caption mt-1 text-muted">{l.note}</p>}
            </td>
            <td className="px-3 py-2.5 align-top text-[13px] break-words">
              {l.kind === "format" && l.current ? (
                <span>
                  <span className="text-muted line-through">{l.current}</span> → {l.suggestion || "—"}
                </span>
              ) : l.kind === "spelling" && l.verdict === "fixed" && l.user_correction ? (
                <span>
                  {l.suggestion && <span className="text-muted line-through" title={t("labels.modelSuggestion")}>{l.suggestion}</span>}
                  {l.suggestion && " → "}
                  <span title={t("labels.userCorrection")}>{l.user_correction}</span>
                </span>
              ) : (
                l.suggestion || "—"
              )}
            </td>
            <td className="px-3 py-2.5 align-top text-[13px] break-words">
              <div>{VERDICTS[l.verdict] ? t(VERDICTS[l.verdict]) : l.verdict}</div>
              <div className="caption text-muted">{t(l.source === "verifier" ? "labels.source.verifier" : "labels.source.user")}</div>
              {l.model && (
                <div className="caption break-all text-muted" title={t("labels.col.model")}>
                  {l.model}
                </div>
              )}
            </td>
            <td className="px-3 py-2.5 align-top text-[13px]">
              <ReviewCell label={l} pending={!!pending[l.id]} onReview={(v, c) => void review(l, v, c)} />
            </td>
          </>
        )}
      />
      <Pager page={page} pageCount={pageCount} onPage={setPage} loading={loading} />
      {total > MAX_ROWS && page >= pageCount && (
        <p className="caption -mt-2 text-center text-muted">{t("labels.moreInExport", { n: MAX_ROWS })}</p>
      )}
    </div>
  );
}

export default DocumentLabels;
