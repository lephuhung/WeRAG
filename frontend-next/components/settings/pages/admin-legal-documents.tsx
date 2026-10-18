"use client";

/* SuperAdmin review of the legal identity of documents in platform public
 * KBs (số hiệu, dates, validity). The server suggests an identity from each
 * document's header and matches it against the vbpl.vn catalogue; the
 * reviewer confirms or corrects it here. Backend:
 * /api/v1/system/legal-reviews (+ /scan, /:knowledge_id) and
 * /api/v1/system/legal-catalog/search — all SuperAdmin-only. */

import { useCallback, useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { IconRefresh, IconSearch, IconFileSearch } from "@/components/icons";
import { useT, type LocaleKey } from "@/lib/i18n";
import { getCurrentUser } from "@/lib/api/auth";
import { ApiError } from "@/lib/api-client";
import {
  decideLegalReview,
  listLegalReviews,
  scanLegalReviews,
  searchLegalCatalog,
  type LegalDocument,
  type LegalDocumentFields,
  type LegalRelation,
  type LegalReviewFilter,
  type LegalReviewItem,
} from "@/lib/api/legal-review";
import { LegalContentTab, LegalDisplayTab } from "@/components/settings/pages/legal-review-edit";
import { useSearchParams } from "next/navigation";
import LegalRequestsSection from "@/components/settings/pages/admin-legal-requests";
import LegalDraftsSection from "@/components/settings/pages/admin-legal-drafts";
import { listLegalRequests, type LegalRequestCounts } from "@/lib/api/legal-requests";

const PAGE_SIZE = 50;

/* The page's sections: the identity review queue, the users' requests for
 * documents no knowledge base holds, and the OCR drafts of documents
 * vbpl.vn has only as a scanned PDF. ?view= opens one directly (the draft
 * review page links back with view=requests / view=drafts). */
type PageView = "review" | "requests" | "drafts";

const VIEWS: { key: PageView; labelKey: LocaleKey }[] = [
  { key: "review", labelKey: "lreq.view.review" },
  { key: "requests", labelKey: "lreq.view.requests" },
  { key: "drafts", labelKey: "lreq.view.drafts" },
];

function pageViewOf(v: string | null): PageView {
  return v === "requests" || v === "drafts" ? v : "review";
}

type DialogTab = "identity" | "content" | "display";

const DIALOG_TABS: { key: DialogTab; labelKey: LocaleKey }[] = [
  { key: "identity", labelKey: "lrev.tab.identity" },
  { key: "content", labelKey: "lrev.tab.content" },
  { key: "display", labelKey: "lrev.tab.display" },
];

const FILTERS: { key: LegalReviewFilter; labelKey: LocaleKey }[] = [
  { key: "pending", labelKey: "lrev.filter.pending" },
  { key: "approved", labelKey: "lrev.filter.approved" },
  { key: "alerts", labelKey: "lrev.filter.alerts" },
  { key: "source_changed", labelKey: "lrev.filter.sourceChanged" },
  { key: "rejected", labelKey: "lrev.filter.rejected" },
];

/* The portal's validity wording; the reviewer may also type another one. */
const STATUS_OPTIONS = [
  "Còn hiệu lực",
  "Hết hiệu lực một phần",
  "Hết hiệu lực toàn bộ",
  "Chưa có hiệu lực",
  "Ngưng hiệu lực",
  "Chưa xác định",
];

const RELATION_KEYS: Partial<Record<LegalRelation, LocaleKey>> = {
  replaces: "lrev.rel.replaces",
  repeals: "lrev.rel.repeals",
  expires: "lrev.rel.expires",
};

const PENDING_FIELD_KEYS: Record<string, LocaleKey> = {
  document_number: "lrev.f.number",
  title: "lrev.f.title",
  doc_type: "lrev.f.type",
  issuer: "lrev.f.issuer",
  issued_on: "lrev.f.issued",
  effective_on: "lrev.f.effective",
  expired_on: "lrev.f.expired",
  status: "lrev.f.status",
};

/* Same folding as the server's CatalogNumberKey: case, spaces, Đ/D, dashes. */
function numberKey(s: string | undefined): string {
  return (s ?? "")
    .normalize("NFC")
    .replace(/\s+/g, "")
    .replace(/[đĐ]/g, "D")
    .replace(/[–—‐−]/g, "-")
    .toUpperCase();
}

/* ISO timestamp → YYYY-MM-DD (the date part as stored). */
function isoDay(s: string | null | undefined): string {
  return s ? s.slice(0, 10) : "";
}

/* dd/mm/yyyy (as printed in a header) → YYYY-MM-DD. */
function dmyToDay(s: string | undefined): string {
  const m = /^(\d{1,2})\/(\d{1,2})\/(\d{4})$/.exec((s ?? "").trim());
  return m ? `${m[3]}-${m[2].padStart(2, "0")}-${m[1].padStart(2, "0")}` : "";
}

function showDay(day: string): string {
  if (!day) return "—";
  const [y, m, d] = day.split("-");
  return `${d}/${m}/${y}`;
}

function statusTone(status: string): string {
  if (status.startsWith("Còn hiệu lực")) return "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400";
  if (status === "Hết hiệu lực toàn bộ" || status.startsWith("Ngưng"))
    return "bg-red-500/10 text-red-700 dark:text-red-400";
  if (status.startsWith("Hết hiệu lực")) return "bg-amber-500/10 text-amber-700 dark:text-amber-400";
  return "bg-surface-strong text-muted";
}

function docLabel(d: LegalDocument): string {
  const num = d.document_number.trim();
  if (num && d.title.includes(num)) return d.title;
  return [d.doc_type, num].filter(Boolean).join(" ") + (d.title ? ` — ${d.title}` : "");
}

function fieldsFrom(d: LegalDocument): LegalDocumentFields {
  return {
    document_number: d.document_number,
    title: d.title,
    doc_type: d.doc_type,
    issuer: d.issuer,
    signer: d.signer,
    field: d.field,
    issued_on: isoDay(d.issued_on),
    effective_on: isoDay(d.effective_on),
    expired_on: isoDay(d.expired_on),
    status: d.status,
  };
}

function fieldsFromSuggestion(item: LegalReviewItem): LegalDocumentFields {
  const s = item.suggestion ?? {};
  return {
    document_number: s.document_number ?? "",
    title: "",
    doc_type: s.doc_type ?? "",
    issuer: s.issuer ?? "",
    signer: "",
    field: "",
    issued_on: dmyToDay(s.issued_on),
    effective_on: "",
    expired_on: "",
    status: "",
  };
}

/* Which parsed values disagree with the linked catalogue row. */
export function mismatches(item: LegalReviewItem): { number: boolean; type: boolean; issued: boolean } {
  const s = item.suggestion;
  const d = item.document;
  if (!s || !d) return { number: false, type: false, issued: false };
  const parsedDay = dmyToDay(s.issued_on);
  return {
    number: !!s.document_number && numberKey(s.document_number) !== numberKey(d.document_number),
    type: !!s.doc_type && !!d.doc_type && s.doc_type.toLowerCase() !== d.doc_type.toLowerCase(),
    issued: !!parsedDay && !!d.issued_on && parsedDay !== isoDay(d.issued_on),
  };
}

export default function AdminLegalDocuments() {
  const { t } = useT();
  const [allowed, setAllowed] = useState<boolean | null>(null);
  const [filter, setFilter] = useState<LegalReviewFilter>("pending");
  const [items, setItems] = useState<LegalReviewItem[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [scanning, setScanning] = useState(false);
  const [notice, setNotice] = useState("");
  const [editing, setEditing] = useState<LegalReviewItem | null>(null);
  const searchParams = useSearchParams();
  const [view, setView] = useState<PageView>(() => pageViewOf(searchParams.get("view")));
  const [requestCounts, setRequestCounts] = useState<LegalRequestCounts | null>(null);
  const denyRequests = useCallback(() => setAllowed(false), []);

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const me = await getCurrentUser();
        if (alive) setAllowed(me.data?.user?.is_system_admin === true);
      } catch {
        if (alive) setAllowed(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await listLegalReviews({ status: filter, page, pageSize: PAGE_SIZE });
      setItems(res.data ?? []);
      setTotal(res.total ?? 0);
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) setAllowed(false);
      else setError(e instanceof Error ? e.message : t("lrev.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [filter, page, t]);

  useEffect(() => {
    if (allowed && view === "review") void load();
  }, [allowed, view, load]);

  /* the pending-request count on the tab, before the tab is opened */
  useEffect(() => {
    if (!allowed) return;
    let alive = true;
    (async () => {
      try {
        const res = await listLegalRequests({ status: "requested", page: 1, pageSize: 1 });
        if (alive && res.data?.counts) setRequestCounts(res.data.counts);
      } catch {
        /* the badge is optional */
      }
    })();
    return () => {
      alive = false;
    };
  }, [allowed]);

  const scan = async () => {
    setScanning(true);
    setNotice("");
    setError("");
    try {
      const res = await scanLegalReviews();
      setNotice(t("lrev.scanDone", { count: res.data?.created ?? 0 }));
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("lrev.loadFailed"));
    } finally {
      setScanning(false);
    }
  };

  if (allowed === null) {
    return <div className="mx-auto w-full max-w-[1100px] px-5 py-12 text-muted">{t("usrp.loading")}</div>;
  }
  if (!allowed) {
    return (
      <div className="mx-auto w-full max-w-[1100px] px-5 py-16 text-center">
        <h2 className="title-md mb-2">{t("systemNav.legalDocuments")}</h2>
        <p className="body-sm text-muted">{t("lrev.denied")}</p>
      </div>
    );
  }

  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const alerts = filter === "alerts";

  return (
    <div className="mx-auto w-full max-w-[1400px]">
      <div className="mb-5 flex items-center gap-1 border-b border-hairline">
        {VIEWS.map(({ key: v, labelKey }) => (
          <button
            key={v}
            type="button"
            onClick={() => setView(v)}
            className={`-mb-px flex items-center gap-1.5 border-b-2 px-3 py-2 text-[13px] font-medium transition-colors ${
              view === v ? "border-ink text-ink" : "border-transparent text-muted hover:text-ink"
            }`}
          >
            {t(labelKey)}
            {v === "requests" && (requestCounts?.requested ?? 0) > 0 && (
              <span className="badge-pill bg-amber-500/15 text-[10.5px] tabular-nums text-amber-700 dark:text-amber-400">
                {requestCounts?.requested}
              </span>
            )}
          </button>
        ))}
      </div>

      {view === "requests" ? (
        <LegalRequestsSection onCounts={setRequestCounts} onDenied={denyRequests} />
      ) : view === "drafts" ? (
        <LegalDraftsSection onDenied={denyRequests} />
      ) : (
        <>
          <div className="mb-5 flex flex-wrap items-center gap-3">
            <span className="caption text-muted">{t("lrev.desc")}</span>
            <div className="ml-auto flex items-center gap-2">
              <button
                className="btn btn-primary btn-sm flex items-center gap-1.5"
                onClick={() => void scan()}
                disabled={scanning}
              >
                <IconFileSearch className="h-3.5 w-3.5" />
                {scanning ? t("lrev.scanning") : t("lrev.scan")}
              </button>
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

          {notice && <p className="caption mb-3 text-emerald-700 dark:text-emerald-400">{notice}</p>}
          {error && <p className="caption mb-3 text-error">{error}</p>}

          <div className="mb-5 inline-flex items-center gap-0.5 rounded-full border border-hairline bg-surface-strong/60 p-0.5">
            {FILTERS.map((f) => (
              <button
                key={f.key}
                type="button"
                onClick={() => {
                  setFilter(f.key);
                  setPage(1);
                }}
                className={`rounded-full px-3 py-1 text-[12px] font-medium transition-colors ${
                  filter === f.key
                    ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.08)]"
                    : "text-muted hover:text-ink"
                }`}
              >
                {t(f.labelKey)}
              </button>
            ))}
          </div>

          <div className="card overflow-x-auto shadow-sm">
            <table className="w-full min-w-[760px] border-collapse text-left">
              <thead>
                <tr className="caption-uppercase border-b border-hairline bg-surface-strong/30 text-muted">
                  <th className="min-w-[220px] px-5 py-3 font-medium">{t("lrev.col.document")}</th>
                  <th className="w-[170px] px-5 py-3 font-medium">{t("lrev.col.number")}</th>
                  <th className="w-[110px] px-5 py-3 font-medium">{t("lrev.col.issued")}</th>
                  <th className="w-[160px] px-5 py-3 font-medium">{t("lrev.col.validity")}</th>
                  <th className={`${alerts ? "min-w-[260px]" : "w-[120px]"} px-5 py-3 font-medium`}>
                    {alerts ? t("lrev.col.supersededBy") : t("lrev.col.review")}
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-hairline">
                {items.map((item) => (
                  <ReviewRow key={item.link.knowledge_id} item={item} alerts={alerts} onOpen={() => setEditing(item)} />
                ))}
                {items.length === 0 && (
                  <tr>
                    <td colSpan={5} className="px-5 py-12 text-center text-[14px] text-muted">
                      {loading ? t("usrp.loading") : t("lrev.empty")}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>

          {pages > 1 && (
            <div className="mt-4 flex items-center justify-center gap-3">
              <button className="btn btn-outline btn-sm" disabled={page <= 1 || loading} onClick={() => setPage(page - 1)}>
                ‹
              </button>
              <span className="caption text-muted tabular-nums">
                {page} / {pages}
              </span>
              <button
                className="btn btn-outline btn-sm"
                disabled={page >= pages || loading}
                onClick={() => setPage(page + 1)}
              >
                ›
              </button>
            </div>
          )}

          {editing && (
            <ReviewDialog
              item={editing}
              onClose={() => setEditing(null)}
              onDone={() => {
                setEditing(null);
                void load();
              }}
            />
          )}
        </>
      )}
    </div>
  );
}

function ReviewRow({
  item,
  alerts,
  onOpen,
}: {
  item: LegalReviewItem;
  alerts: boolean;
  onOpen: () => void;
}) {
  const { t } = useT();
  const doc = item.document;
  const parsed = item.suggestion?.document_number ?? "";
  const mm = mismatches(item);
  const pending = doc?.source_pending && Object.keys(doc.source_pending).length > 0;
  const issued = doc ? isoDay(doc.issued_on) : dmyToDay(item.suggestion?.issued_on);
  return (
    <tr className="cursor-pointer align-top transition-colors hover:bg-surface-strong/20" onClick={onOpen}>
      <td className="px-5 py-3.5">
        <div className="truncate text-[13px] font-medium text-ink" title={item.knowledge_title}>
          {item.knowledge_title}
        </div>
        <div className="caption truncate text-muted">{item.knowledge_base_name}</div>
        {doc && (
          <div className="caption mt-0.5 line-clamp-2 text-body" title={doc.title}>
            {doc.title}
          </div>
        )}
        {pending && (
          <span className="badge-pill mt-1 inline-block bg-amber-500/10 text-[10px] text-amber-700 dark:text-amber-400">
            {t("lrev.sourcePending")}
          </span>
        )}
      </td>
      <td className="px-5 py-3.5">
        <div className="font-mono text-[12px] text-ink">{doc?.document_number || parsed || "—"}</div>
        {doc && parsed && mm.number && (
          <div className="caption mt-0.5 text-amber-700 dark:text-amber-400">
            {t("lrev.parsedDiffers", { value: parsed })}
          </div>
        )}
        {!doc && (
          <div className="caption mt-0.5 text-muted">
            {(item.suggestion?.catalog_matches?.length ?? 0) > 1
              ? t("lrev.manyMatches", { count: item.suggestion?.catalog_matches?.length ?? 0 })
              : t("lrev.noMatch")}
          </div>
        )}
        {mm.type && (
          <div className="caption mt-0.5 text-amber-700 dark:text-amber-400">
            {t("lrev.typeDiffers", { parsed: item.suggestion?.doc_type ?? "", catalog: doc?.doc_type ?? "" })}
          </div>
        )}
      </td>
      <td className="px-5 py-3.5">
        <span className={`caption tabular-nums ${mm.issued ? "text-amber-700 dark:text-amber-400" : "text-body"}`}>
          {showDay(issued)}
        </span>
        {mm.issued && (
          <div className="caption text-amber-700 dark:text-amber-400">
            {t("lrev.parsedDiffers", { value: item.suggestion?.issued_on ?? "" })}
          </div>
        )}
      </td>
      <td className="px-5 py-3.5">
        {doc?.status ? (
          <span className={`badge-pill inline-block whitespace-normal text-[10px] leading-snug ${statusTone(doc.status)}`}>{doc.status}</span>
        ) : (
          <span className="caption text-muted-soft">—</span>
        )}
        {doc?.expired_on && (
          <div className="caption mt-0.5 text-muted">
            {t("lrev.expiredOn", { date: showDay(isoDay(doc.expired_on)) })}
          </div>
        )}
      </td>
      <td className="px-5 py-3.5">
        {alerts ? (
          <Supersessions item={item} />
        ) : (
          <span className="caption text-muted">{t(`lrev.state.${item.link.review_status}` as LocaleKey)}</span>
        )}
      </td>
    </tr>
  );
}

function Supersessions({ item }: { item: LegalReviewItem }) {
  const { t } = useT();
  const list = item.superseded_by ?? [];
  if (list.length === 0) {
    return <span className="caption text-muted">{t("lrev.expiredByStatus")}</span>;
  }
  return (
    <ul className="flex flex-col gap-1.5">
      {list.map((s) => (
        <li key={`${s.relation}-${s.document.id}`} className="text-[12px]">
          <span className="text-muted">{t(RELATION_KEYS[s.relation] ?? "lrev.rel.expires")} </span>
          <span className="font-medium text-ink">{docLabel(s.document)}</span>
          {!s.in_kb && (
            <div className="caption mt-0.5 rounded-[8px] bg-red-500/10 px-2 py-1 text-red-700 dark:text-red-400">
              {t("lrev.missingInKB")}
            </div>
          )}
        </li>
      ))}
    </ul>
  );
}

/* Also opened from a public KB's document cards (kb-legal-review.tsx). */
export function ReviewDialog({
  item,
  onClose,
  onDone,
}: {
  item: LegalReviewItem;
  onClose: () => void;
  onDone: () => void;
}) {
  const { t } = useT();
  const linked = item.document;
  const [pickedId, setPickedId] = useState<string>(linked?.id ?? "");
  const [options, setOptions] = useState<LegalDocument[]>(() => {
    const all = [...(linked ? [linked] : []), ...(item.candidates ?? [])];
    return all.filter((d, i) => all.findIndex((x) => x.id === d.id) === i);
  });
  const [form, setForm] = useState<LegalDocumentFields>(() =>
    linked ? fieldsFrom(linked) : fieldsFromSuggestion(item),
  );
  const [query, setQuery] = useState(item.suggestion?.document_number ?? "");
  const [searching, setSearching] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [tab, setTab] = useState<DialogTab>("identity");
  /* Content / display edits are saved on their own; closing after one of
   * them refreshes the queue (the title may have changed). */
  const [touched, setTouched] = useState(false);

  const pick = (id: string) => {
    setPickedId(id);
    const d = options.find((o) => o.id === id);
    setForm(d ? fieldsFrom(d) : fieldsFromSuggestion(item));
  };

  const search = async () => {
    if (!query.trim()) return;
    setSearching(true);
    setError("");
    try {
      const res = await searchLegalCatalog(query.trim());
      const found = res.data ?? [];
      setOptions((prev) => [...prev, ...found.filter((d) => !prev.some((p) => p.id === d.id))]);
      if (found.length === 0) setError(t("lrev.searchEmpty"));
    } catch (e) {
      setError(e instanceof Error ? e.message : t("lrev.loadFailed"));
    } finally {
      setSearching(false);
    }
  };

  const decide = async (action: "approve" | "reject") => {
    setSaving(true);
    setError("");
    try {
      await decideLegalReview(
        item.link.knowledge_id,
        action === "reject"
          ? { action }
          : { action, legal_document_id: pickedId || undefined, document: form },
      );
      onDone();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("lrev.loadFailed"));
      setSaving(false);
    }
  };

  const set = (k: keyof LegalDocumentFields) => (v: string) => setForm((f) => ({ ...f, [k]: v }));
  const pending = linked?.source_pending ?? null;
  const s = item.suggestion;

  return (
    <Modal open title={item.knowledge_title} onClose={touched ? onDone : onClose} width="w-[880px]">
      <div className="mb-5 inline-flex items-center gap-0.5 rounded-full border border-hairline bg-surface-strong/60 p-0.5">
        {DIALOG_TABS.map((d) => (
          <button
            key={d.key}
            type="button"
            onClick={() => {
              setTab(d.key);
              if (d.key !== "identity") setTouched(true);
            }}
            className={`rounded-full px-3 py-1 text-[12px] font-medium transition-colors ${
              tab === d.key
                ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.08)]"
                : "text-muted hover:text-ink"
            }`}
          >
            {t(d.labelKey)}
          </button>
        ))}
      </div>
      {/* Tabs stay mounted once opened so unsaved drafts survive switching. */}
      {touched && (
        <div className={tab === "content" ? "" : "hidden"}>
          <LegalContentTab knowledgeId={item.link.knowledge_id} />
        </div>
      )}
      {touched && (
        <div className={tab === "display" ? "" : "hidden"}>
          <LegalDisplayTab knowledgeId={item.link.knowledge_id} initialTitle={item.knowledge_title} />
        </div>
      )}
      <div className={`flex flex-col gap-5 ${tab === "identity" ? "" : "hidden"}`}>
        <div className="rounded-[12px] border border-hairline bg-canvas-soft p-3">
          <div className="caption-uppercase mb-1 text-muted">{t("lrev.parsed")}</div>
          <div className="text-[13px] text-body">
            {[s?.doc_type, s?.document_number, s?.issued_on].filter(Boolean).join(" · ") || "—"}
            {s?.issuer ? <span className="text-muted"> — {s.issuer}</span> : null}
          </div>
          <div className="caption mt-1 text-muted">{item.knowledge_base_name}</div>
        </div>

        {(item.superseded_by?.length ?? 0) > 0 && (
          <div className="rounded-[12px] border border-red-500/30 bg-red-500/5 p-3">
            <Supersessions item={item} />
          </div>
        )}

        {pending && Object.keys(pending).length > 0 && (
          <div className="rounded-[12px] border border-amber-500/30 bg-amber-500/5 p-3">
            <div className="mb-1.5 flex items-center gap-2">
              <span className="text-[13px] font-medium text-amber-700 dark:text-amber-400">{t("lrev.sourcePending")}</span>
              <button
                type="button"
                className="btn btn-outline btn-sm ml-auto py-0.5 text-[12px]"
                onClick={() => setForm((f) => ({ ...f, ...pending }))}
              >
                {t("lrev.usePortal")}
              </button>
            </div>
            <dl className="grid grid-cols-[140px_minmax(0,1fr)] gap-x-3 gap-y-1">
              {Object.entries(pending).map(([k, v]) => (
                <div key={k} className="contents">
                  <dt className="caption text-muted">{PENDING_FIELD_KEYS[k] ? t(PENDING_FIELD_KEYS[k]) : k}</dt>
                  <dd className="text-[12px] text-body">{v || "—"}</dd>
                </div>
              ))}
            </dl>
          </div>
        )}

        <div>
          <div className="caption-uppercase mb-2 text-muted">{t("lrev.catalogRow")}</div>
          <div className="mb-2 flex gap-2">
            <div className="relative flex-1">
              <IconSearch className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-soft" />
              <input
                className="input h-9 pl-9 text-[13px]"
                placeholder={t("lrev.searchPh")}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && void search()}
              />
            </div>
            <button className="btn btn-outline btn-sm" onClick={() => void search()} disabled={searching}>
              {searching ? "…" : t("lrev.search")}
            </button>
          </div>
          <div className="flex max-h-[180px] flex-col gap-1 overflow-y-auto">
            {options.map((d) => (
              <label
                key={d.id}
                className={`flex cursor-pointer items-start gap-2 rounded-[10px] border px-3 py-2 text-[13px] ${
                  pickedId === d.id ? "border-ink/40 bg-surface-strong/40" : "border-hairline"
                }`}
              >
                <input type="radio" className="mt-1" checked={pickedId === d.id} onChange={() => pick(d.id)} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-ink">{docLabel(d)}</span>
                  <span className="caption text-muted">
                    {[d.issuer, showDay(isoDay(d.issued_on)), d.status].filter(Boolean).join(" · ")}
                    {d.source === "manual" ? ` · ${t("lrev.manual")}` : ""}
                  </span>
                </span>
              </label>
            ))}
            <label
              className={`flex cursor-pointer items-center gap-2 rounded-[10px] border px-3 py-2 text-[13px] ${
                pickedId === "" ? "border-ink/40 bg-surface-strong/40" : "border-hairline"
              }`}
            >
              <input type="radio" checked={pickedId === ""} onChange={() => pick("")} />
              <span className="text-body">{t("lrev.createManual")}</span>
            </label>
          </div>
        </div>

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label={t("lrev.f.number")} value={form.document_number} onChange={set("document_number")} mono />
          <Field label={t("lrev.f.type")} value={form.doc_type} onChange={set("doc_type")} />
          <div className="sm:col-span-2">
            <Field label={t("lrev.f.title")} value={form.title} onChange={set("title")} />
          </div>
          <Field label={t("lrev.f.issuer")} value={form.issuer} onChange={set("issuer")} />
          <Field label={t("lrev.f.signer")} value={form.signer} onChange={set("signer")} />
          <Field label={t("lrev.f.issued")} value={form.issued_on} onChange={set("issued_on")} type="date" />
          <Field label={t("lrev.f.effective")} value={form.effective_on} onChange={set("effective_on")} type="date" />
          <Field label={t("lrev.f.expired")} value={form.expired_on} onChange={set("expired_on")} type="date" />
          <label className="flex flex-col gap-1">
            <span className="caption text-muted">{t("lrev.f.status")}</span>
            <input
              className="input h-9 text-[13px]"
              list="lrev-status-options"
              value={form.status}
              onChange={(e) => set("status")(e.target.value)}
            />
            <datalist id="lrev-status-options">
              {STATUS_OPTIONS.map((o) => (
                <option key={o} value={o} />
              ))}
            </datalist>
          </label>
          <div className="sm:col-span-2">
            <Field label={t("lrev.f.field")} value={form.field} onChange={set("field")} />
          </div>
        </div>

        {error && <p className="caption text-error">{error}</p>}

        <div className="flex flex-wrap items-center gap-2 border-t border-hairline pt-4">
          <button className="btn btn-outline btn-sm" onClick={() => void decide("reject")} disabled={saving}>
            {t("lrev.reject")}
          </button>
          <div className="ml-auto flex gap-2">
            <button className="btn btn-ghost btn-sm" onClick={touched ? onDone : onClose} disabled={saving}>
              {t("common.cancel")}
            </button>
            <button className="btn btn-primary btn-sm" onClick={() => void decide("approve")} disabled={saving}>
              {saving ? "…" : t("lrev.approve")}
            </button>
          </div>
        </div>
      </div>
    </Modal>
  );
}

function Field({
  label,
  value,
  onChange,
  type = "text",
  mono = false,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  type?: "text" | "date";
  mono?: boolean;
}) {
  return (
    <label className="flex flex-col gap-1">
      <span className="caption text-muted">{label}</span>
      <input
        className={`input h-9 text-[13px] ${mono ? "font-mono" : ""}`}
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </label>
  );
}
