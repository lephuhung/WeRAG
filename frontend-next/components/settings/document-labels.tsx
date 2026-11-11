/* Labels on the document assistant's spelling underlines and NĐ30 format
 * check reports, as training data: a table (the paragraph with the flagged
 * span highlighted, the suggestion, the verdict) and a JSONL export of the
 * filtered set. Tenant Admin sees their own organization; the SuperAdmin
 * every organization, with an organization filter. Server guards decide. */
"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { IconDownload, IconRefresh } from "@/components/icons";
import { Select } from "@/components/select";
import { DataTable } from "@/components/settings/data-table";
import { Notice } from "@/components/settings/notice";
import { Pager } from "@/components/settings/pager";
import { SectionHeader } from "@/components/settings/section-header";
import { useToast } from "@/components/toast";
import { useAuth } from "@/lib/auth";
import { formatDateTime } from "@/lib/datetime";
import { useT, type LocaleKey } from "@/lib/i18n";
import { listAllTenants, type TenantInfo } from "@/lib/api/tenants";
import {
  exportDocumentLabels,
  getDocumentLabelFacets,
  listDocumentLabels,
  type DocumentLabel,
  type DocumentLabelFilters,
} from "@/lib/api/document-labels";

const PAGE_SIZE = 50;
/* The list pages through the first 2,000 rows (server cap); the export has all. */
const MAX_ROWS = 2000;
/* Code points of a long paragraph shown around the flagged span. */
const PARAGRAPH_WINDOW = 160;

const VERDICTS: Record<string, LocaleKey> = {
  not_error: "labels.verdict.not_error",
  false_alarm: "labels.verdict.false_alarm",
  not_nd30: "labels.verdict.not_nd30",
  wrong_type: "labels.verdict.wrong_type",
  other: "labels.verdict.other",
};
const SPELLING_VERDICTS = ["not_error"];
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

export function DocumentLabels() {
  const { t, locale } = useT();
  const toast = useToast();
  const auth = useAuth();
  const isSystemAdmin = auth.user?.is_system_admin === true;

  const [filters, setFilters] = useState<DocumentLabelFilters>({});
  const [page, setPage] = useState(1);
  const [items, setItems] = useState<DocumentLabel[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [exporting, setExporting] = useState(false);
  const [models, setModels] = useState<string[]>([]);
  const [tenants, setTenants] = useState<TenantInfo[]>([]);

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
      toast.error(e instanceof Error ? e.message : t("labels.exportFailed"));
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
  const columns = [
    { key: "time", header: t("labels.col.time"), className: "w-[150px]" },
    ...(isSystemAdmin ? [{ key: "org", header: t("labels.col.organization"), className: "w-[160px]" }] : []),
    { key: "paragraph", header: t("labels.col.paragraph") },
    { key: "suggestion", header: t("labels.col.suggestion"), className: "w-[200px]" },
    { key: "verdict", header: t("labels.col.verdict"), className: "w-[170px]" },
    { key: "model", header: t("labels.col.model"), className: "w-[170px]" },
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
        minWidth="min-w-[1000px]"
        renderRow={(l) => (
          <>
            <td className="px-5 py-3 align-top text-[13px] tabular-nums text-muted">{formatDateTime(l.created_at, locale)}</td>
            {isSystemAdmin && (
              <td className="px-5 py-3 align-top text-[13px]">{l.organization_name || `#${l.organization_id}`}</td>
            )}
            <td className="px-5 py-3 align-top text-[13px] leading-relaxed">
              <div className="mb-1 flex flex-wrap items-center gap-1.5">
                <span className="badge-pill text-[10px] uppercase">{t(l.kind === "format" ? "labels.kind.format" : "labels.kind.spelling")}</span>
                {l.finding_title && <span className="caption text-muted">{l.finding_title}</span>}
              </div>
              <HighlightedParagraph label={l} />
              {l.note && <p className="caption mt-1 text-muted">{l.note}</p>}
            </td>
            <td className="px-5 py-3 align-top text-[13px]">
              {l.kind === "format" && l.current ? (
                <span>
                  <span className="text-muted line-through">{l.current}</span> → {l.suggestion || "—"}
                </span>
              ) : (
                l.suggestion || "—"
              )}
            </td>
            <td className="px-5 py-3 align-top text-[13px]">
              <div>{VERDICTS[l.verdict] ? t(VERDICTS[l.verdict]) : l.verdict}</div>
              <div className="caption text-muted">{t(l.source === "verifier" ? "labels.source.verifier" : "labels.source.user")}</div>
            </td>
            <td className="break-all px-5 py-3 align-top text-[12px] text-muted">{l.model || "—"}</td>
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
