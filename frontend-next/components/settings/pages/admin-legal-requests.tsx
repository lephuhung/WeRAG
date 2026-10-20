"use client";

/* SuperAdmin queue of "đề nghị bổ sung văn bản": legal documents users named
 * in chat that no knowledge base holds the full text of. Shown as the
 * "Document requests" tab of the legal documents page
 * (admin-legal-documents.tsx). Backend: /api/v1/system/legal-requests
 * (+ /:id, /:id/portal-check, /public-knowledge-bases) — SuperAdmin-only.
 * A document vbpl.vn has only as an original PDF scan is never loaded as
 * raw OCR: "Kéo về bản nháp" fetches it into an OCR draft
 * (lib/api/legal-drafts.ts) that the SuperAdmin edits and approves on the
 * review page; the portal check says which form exists. */

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { Modal } from "@/components/modal";
import { IconRefresh } from "@/components/icons";
import { useToast } from "@/components/toast";
import { ConsolePage } from "@/components/settings/console-page";
import { DataTable } from "@/components/settings/data-table";
import { Pager } from "@/components/settings/pager";
import { Pill, PillGroup } from "@/components/settings/pill";
import { Notice } from "@/components/settings/notice";
import { Loading } from "@/components/settings/loading";
import { FieldLabel } from "@/components/settings/field";
import { useT, type LocaleKey } from "@/lib/i18n";
import { ApiError } from "@/lib/api-client";
import { formatDateTime } from "@/lib/datetime";
import {
  checkLegalRequestPortal,
  decideLegalRequest,
  listLegalRequestKnowledgeBases,
  listLegalRequests,
  type LegalRequestCounts,
  type LegalRequestFilter,
  type LegalRequestItem,
  type LegalRequestPortal,
} from "@/lib/api/legal-requests";
import {
  fetchDraftForRequest,
  getLegalDraft,
  legalDraftBusy,
  legalDraftEditable,
  legalDraftHref,
  type LegalDraft,
  type LegalDraftSummary,
} from "@/lib/api/legal-drafts";
import { packOriginName } from "@/lib/domain-pack-view";

const PAGE_SIZE = 50;

const FILTERS: { key: LegalRequestFilter; labelKey: LocaleKey }[] = [
  { key: "requested", labelKey: "lreq.filter.requested" },
  { key: "fetched", labelKey: "lreq.filter.fetched" },
  { key: "reviewing", labelKey: "lreq.filter.reviewing" },
  { key: "added", labelKey: "lreq.filter.added" },
  { key: "rejected", labelKey: "lreq.filter.rejected" },
  { key: "all", labelKey: "lreq.filter.all" },
];

type Action = "ingest" | "reject" | "added";

function statusTone(status: string): string {
  if (status.startsWith("Còn hiệu lực")) return "badge-success";
  return "badge-neutral";
}

function errorText(e: unknown, fallback: string): string {
  if (e instanceof ApiError || e instanceof Error) return e.message || fallback;
  return fallback;
}

function itemLabel(item: LegalRequestItem): string {
  const d = item.document;
  return [d?.doc_type || item.doc_type, d?.document_number || item.document_number].filter(Boolean).join(" ");
}

/** What vbpl.vn offers, as one of the badge kinds. */
type PortalKind = "html" | "pdf" | "none" | "gone";

function portalOf(item: LegalRequestItem): LegalRequestPortal | null {
  const p = item.detail?.portal;
  return p && typeof p === "object" ? p : null;
}

function portalKind(p: LegalRequestPortal): PortalKind {
  if (!p.exists) return "gone";
  if (p.html) return "html";
  if (p.pdf_file) return "pdf";
  return "none";
}

const PORTAL_TONE: Record<PortalKind, string> = {
  html: "badge-success",
  pdf: "badge-warning",
  none: "badge-neutral",
  gone: "badge-neutral",
};

function PortalBadge({ portal, locale }: { portal: LegalRequestPortal; locale: string }) {
  const { t } = useT();
  const kind = portalKind(portal);
  const when = formatDateTime(portal.checked_at, locale);
  return (
    <div className="flex flex-col gap-0.5">
      <span
        className={`badge-pill inline-block w-fit text-[10px] ${PORTAL_TONE[kind]}`}
        title={portal.pdf_file || undefined}
      >
        {t(`lreq.portal.${kind}` as LocaleKey)}
      </span>
      {when && <span className="caption tabular-nums text-muted-soft">{t("lreq.portal.checkedAt", { time: when })}</span>}
    </div>
  );
}

export function countTotal(c: LegalRequestCounts | null): number {
  return c ? c.requested + (c.fetched ?? 0) + (c.reviewing ?? 0) + c.added + c.rejected : 0;
}

/** The request queue. `onCounts` reports the per-status counts after each
 * load (the page shows the pending one on its tab). */
export default function LegalRequestsSection({
  onCounts,
  onDenied,
}: {
  onCounts?: (c: LegalRequestCounts) => void;
  onDenied?: () => void;
}) {
  const { t, locale } = useT();
  const toast = useToast();
  const [filter, setFilter] = useState<LegalRequestFilter>("requested");
  const [items, setItems] = useState<LegalRequestItem[]>([]);
  const [total, setTotal] = useState(0);
  const [counts, setCounts] = useState<LegalRequestCounts | null>(null);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [acting, setActing] = useState<{ item: LegalRequestItem; action: Action } | null>(null);

  const load = useCallback(async (): Promise<LegalRequestItem[] | null> => {
    setLoading(true);
    setError("");
    try {
      const res = await listLegalRequests({ status: filter, page, pageSize: PAGE_SIZE });
      const list = res.data?.items ?? [];
      setItems(list);
      setTotal(res.data?.total ?? 0);
      if (res.data?.counts) {
        setCounts(res.data.counts);
        onCounts?.(res.data.counts);
      }
      return list;
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) onDenied?.();
      else setError(errorText(e, t("lreq.loadFailed")));
      return null;
    } finally {
      setLoading(false);
    }
  }, [filter, page, t, onCounts, onDenied]);

  useEffect(() => {
    void load();
  }, [load]);

  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const countOf = (k: LegalRequestFilter): number | null =>
    counts ? (k === "all" ? countTotal(counts) : (counts[k] ?? 0)) : null;

  return (
    <ConsolePage size="wide">
      <div className="mb-5 flex flex-wrap items-center gap-3">
        <span className="caption text-muted">{t("lreq.desc")}</span>
        <div className="ml-auto flex items-center gap-2">
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

      {error && (
        <Notice
          tone="error"
          className="mb-3"
          action={
            <button type="button" className="btn btn-outline btn-sm" onClick={() => void load()}>
              {t("common.retry")}
            </button>
          }
        >
          {error}
        </Notice>
      )}

      <PillGroup ariaLabel={t("common.filters")} className="mb-5 flex-wrap">
        {FILTERS.map((f) => {
          const n = countOf(f.key);
          return (
            <Pill
              key={f.key}
              active={filter === f.key}
              onClick={() => {
                setFilter(f.key);
                setPage(1);
              }}
            >
              {t(f.labelKey)}
              {n !== null && <span className="ml-1 tabular-nums text-muted-soft">({n})</span>}
            </Pill>
          );
        })}
      </PillGroup>

      <DataTable<LegalRequestItem>
        columns={[
          { key: "document", header: t("lreq.col.document"), className: "min-w-[260px]" },
          { key: "validity", header: t("lreq.col.validity"), className: "w-[150px]" },
          { key: "count", header: t("lreq.col.count"), className: "w-[80px]" },
          { key: "lastAsked", header: t("lreq.col.lastAsked"), className: "w-[150px]" },
          { key: "lastQuery", header: t("lreq.col.lastQuery"), className: "min-w-[200px]" },
          { key: "action", header: t("lreq.col.action"), className: "min-w-[200px]" },
        ]}
        rows={items}
        rowKey={(item) => item.id}
        renderRow={(item) => (
          <RequestCells
            item={item}
            locale={locale}
            onAct={(action) => setActing({ item, action })}
            onUpdated={(updated) => setItems((cur) => cur.map((it) => (it.id === updated.id ? updated : it)))}
            onReload={() => void load()}
          />
        )}
        rowClassName="align-top"
        loading={loading}
        loadingLabel={t("common.loading")}
        emptyLabel={t("lreq.empty")}
        minWidth="min-w-[980px]"
      />

      <Pager page={page} pageCount={pages} onPage={setPage} loading={loading} />

      {acting && (
        <DecisionDialog
          item={acting.item}
          action={acting.action}
          onReload={async () => (await load())?.find((it) => it.id === acting.item.id) ?? null}
          onClose={() => setActing(null)}
          onFetched={(draft) => {
            setActing(null);
            toast.success(
              t(legalDraftBusy(draft.status) ? "lreq.draft.fetchedOcr" : "lreq.draft.fetched", {
                number: draft.document_number || acting.item.document_number,
                pages: draft.ocr_pages,
              }),
            );
            void load();
          }}
          onDone={(updated) => {
            setActing(null);
            toast.success(
              t(`lreq.done.${acting.action}` as LocaleKey, {
                number: updated.document_number || acting.item.document_number,
              }),
            );
            void load();
          }}
        />
      )}
    </ConsolePage>
  );
}

/* The cells of one request row (the <tr> comes from <DataTable>). */
function RequestCells({
  item,
  locale,
  onAct,
  onUpdated,
  onReload,
}: {
  item: LegalRequestItem;
  locale: string;
  onAct: (action: Action) => void;
  onUpdated: (item: LegalRequestItem) => void;
  onReload: () => void;
}) {
  const { t } = useT();
  const [checking, setChecking] = useState(false);
  const [checkError, setCheckError] = useState("");
  const portal = portalOf(item);
  const parsing = item.knowledge_parse_status === "pending" || item.knowledge_parse_status === "processing";
  const checkPortal = async () => {
    setChecking(true);
    setCheckError("");
    try {
      const res = await checkLegalRequestPortal(item.id);
      if (res.data) onUpdated(res.data);
    } catch (e) {
      setCheckError(errorText(e, t("lreq.portal.checkFailed")));
    } finally {
      setChecking(false);
    }
  };
  const doc = item.document ?? null;
  const title = doc?.title || item.title;
  const label = itemLabel(item);
  const decided = item.status === "added" || item.status === "rejected";
  const draft = item.draft ?? null;
  // a request recorded by a domain pack's catalogue reconcile
  const packOrigin = packOriginName(item.detail);
  return (
    <>
      <td className="px-5 py-3.5 align-middle">
        <div className="text-[13px] font-semibold text-ink">{label || "—"}</div>
        {packOrigin && (
          <span className="badge-pill badge-accent mt-1 inline-block text-[10.5px]">
            {t("lreq.packOrigin", { name: packOrigin })}
          </span>
        )}
        {title && (
          <div className="caption mt-0.5 line-clamp-2 text-body" title={title}>
            {title}
          </div>
        )}
        {doc?.issuer && <div className="caption mt-0.5 truncate text-muted">{doc.issuer}</div>}
        {doc?.url && (
          <a
            href={doc.url}
            target="_blank"
            rel="noopener noreferrer"
            className="caption mt-0.5 inline-block text-accent hover:underline"
          >
            vbpl.vn
          </a>
        )}
      </td>
      <td className="px-5 py-3.5 align-middle">
        {!item.legal_document_id ? (
          <span className="caption text-muted">{t("lreq.notInCatalog")}</span>
        ) : doc?.status ? (
          <span className={`badge-pill inline-block whitespace-normal text-[10px] leading-snug ${statusTone(doc.status)}`}>
            {doc.status}
          </span>
        ) : (
          <span className="caption text-muted-soft">—</span>
        )}
      </td>
      <td className="px-5 py-3.5 align-middle">
        <span className="text-[13px] tabular-nums text-ink">{item.request_count}</span>
      </td>
      <td className="px-5 py-3.5 align-middle">
        <span className="caption tabular-nums text-body">{formatDateTime(item.last_requested_at, locale) || "—"}</span>
      </td>
      <td className="px-5 py-3.5 align-middle">
        <div className="caption line-clamp-2 max-w-[320px] text-body" title={item.last_query}>
          {item.last_query || "—"}
        </div>
      </td>
      <td className="px-5 py-3.5 align-middle">
        {decided ? (
          <div className="flex flex-col gap-0.5">
            <span
              className={`badge-pill inline-block w-fit text-[10px] ${
                item.status === "added" ? "badge-success" : "badge-error"
              }`}
            >
              {t(`lreq.state.${item.status}` as LocaleKey)}
            </span>
            <span className="caption tabular-nums text-muted">
              {[formatDateTime(item.decided_at, locale), item.decided_by].filter(Boolean).join(" · ")}
            </span>
            {draft?.status === "approved" && (
              <span className="badge-pill badge-success inline-block w-fit text-[10px]">
                {t("lreq.draft.approved")}
              </span>
            )}
            {item.decision_note && (
              <span className="caption text-body" title={item.decision_note}>
                {item.decision_note}
              </span>
            )}
            {item.status === "added" && parsing && (
              <span className="badge-pill badge-warning inline-block w-fit text-[10px]">
                {t("lreq.parse.processing")}
              </span>
            )}
            {item.status === "added" && item.knowledge_parse_status === "failed" && (
              <span className="badge-pill badge-error inline-block w-fit text-[10px]">
                {t("lreq.parse.failed")}
              </span>
            )}
          </div>
        ) : (
          <div className="flex flex-col gap-1.5">
            <div className="flex flex-wrap gap-1.5">
              {item.legal_document_id && !draft && (
                <button type="button" className="btn btn-primary btn-sm py-0.5 text-[12px]" onClick={() => onAct("ingest")}>
                  {t("lreq.act.ingest")}
                </button>
              )}
              <button type="button" className="btn btn-outline btn-sm py-0.5 text-[12px]" onClick={() => onAct("added")}>
                {t("lreq.act.added")}
              </button>
              <button type="button" className="btn btn-outline btn-sm py-0.5 text-[12px]" onClick={() => onAct("reject")}>
                {t("lreq.act.reject")}
              </button>
              {item.legal_document_id && (
                <button
                  type="button"
                  className="btn btn-ghost btn-sm py-0.5 text-[12px]"
                  onClick={() => void checkPortal()}
                  disabled={checking}
                >
                  {checking ? t("lreq.checkingPortal") : t("lreq.act.checkPortal")}
                </button>
              )}
            </div>
            {portal && <PortalBadge portal={portal} locale={locale} />}
            {checkError && (
              <Notice tone="error" className="mt-1">
                {checkError}
              </Notice>
            )}
            {item.legal_document_id && (draft || (portal && portalKind(portal) === "pdf")) && (
              <DraftControls
                item={item}
                onDraft={(d) => onUpdated({ ...item, draft: d })}
                onSettled={onReload}
              />
            )}
          </div>
        )}
      </td>
    </>
  );
}

function DecisionDialog({
  item: initial,
  action,
  onReload,
  onClose,
  onDone,
  onFetched,
}: {
  item: LegalRequestItem;
  action: Action;
  /** Reloads the queue and returns this request as the server now has it. */
  onReload: () => Promise<LegalRequestItem | null>;
  onClose: () => void;
  onDone: (updated: LegalRequestItem) => void;
  /** The PDF scan was fetched into an OCR draft instead. */
  onFetched: (draft: LegalDraft) => void;
}) {
  const { t, locale } = useT();
  // the request as last read: an ingest refused for lack of HTML stores
  // what vbpl.vn offers (detail.portal), reloaded into this dialog
  const [item, setItem] = useState(initial);
  const portal = action === "ingest" ? portalOf(item) : null;
  const kind = portal ? portalKind(portal) : null;
  const pdfOnly = kind === "pdf";
  const blocked = kind === "none" || kind === "gone";
  const [kbs, setKbs] = useState<{ id: string; name: string }[] | null>(null);
  const [kbId, setKbId] = useState(item.suggested_kb_id ?? "");
  const [note, setNote] = useState("");
  const [knowledgeId, setKnowledgeId] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (action !== "ingest") return;
    let alive = true;
    (async () => {
      try {
        const res = await listLegalRequestKnowledgeBases();
        if (!alive) return;
        const list = res.data ?? [];
        setKbs(list);
        setKbId((cur) => (cur && list.some((k) => k.id === cur) ? cur : (list[0]?.id ?? "")));
      } catch (e) {
        if (alive) {
          setKbs([]);
          setError(errorText(e, t("lreq.loadFailed")));
        }
      }
    })();
    return () => {
      alive = false;
    };
  }, [action, t]);

  const canSubmit =
    !saving &&
    (action === "ingest" ? (pdfOnly || !!kbId) && !blocked : action === "reject" ? note.trim() !== "" : true);

  const submit = async () => {
    if (!canSubmit) return;
    setSaving(true);
    setError("");
    if (pdfOnly) {
      // a scan is never ingested as raw OCR: it becomes a draft to review
      try {
        const res = await fetchDraftForRequest(item.id);
        onFetched(res.data);
      } catch (e) {
        setError(errorText(e, t("lreq.draft.fetchFailed")));
        setSaving(false);
      }
      return;
    }
    try {
      const res = await decideLegalRequest(
        item.id,
        action === "ingest"
          ? { action, knowledge_base_id: kbId }
          : action === "reject"
            ? { action, note: note.trim() }
            : { action, note: note.trim() || undefined, knowledge_id: knowledgeId.trim() || undefined },
      );
      onDone(res.data ?? item);
    } catch (e) {
      // a refusal for lack of HTML (only a PDF scan, or nothing) stored the
      // portal availability on the request: reload it to offer the OCR
      // draft or say why nothing can be loaded
      if (action === "ingest" && e instanceof ApiError && e.status === 400 && !pdfOnly) {
        const fresh = await onReload().catch(() => null);
        if (fresh && portalOf(fresh)) {
          setItem(fresh);
          const k = portalKind(portalOf(fresh)!);
          setError(k === "pdf" ? "" : errorText(e, t("lreq.actFailed")));
          setSaving(false);
          return;
        }
      }
      setError(errorText(e, t("lreq.actFailed")));
      setSaving(false);
    }
  };

  const label = itemLabel(item);
  const title = item.document?.title || item.title;

  return (
    <Modal open title={t(`lreq.dialog.${action}` as LocaleKey)} onClose={onClose} width="w-[520px]">
      <div className="flex flex-col gap-4">
        <div className="rounded-[12px] border border-hairline bg-canvas-soft p-3">
          <div className="text-[13px] font-semibold text-ink">{label || "—"}</div>
          {title && <div className="caption mt-0.5 text-body">{title}</div>}
        </div>

        {action === "ingest" && !pdfOnly && (
          <div className="flex flex-col gap-1">
            <FieldLabel htmlFor="lreq-kb">{t("lreq.f.kb")}</FieldLabel>
            {kbs === null ? (
              <span className="caption text-muted">{t("common.loading")}</span>
            ) : kbs.length === 0 ? (
              <span className="caption text-muted">{t("lreq.noKb")}</span>
            ) : (
              <select
                id="lreq-kb"
                className="input h-9 py-0 text-[13px]"
                value={kbId}
                onChange={(e) => setKbId(e.target.value)}
              >
                {kbs.map((k) => (
                  <option key={k.id} value={k.id}>
                    {k.name}
                    {k.id === item.suggested_kb_id ? ` (${t("lreq.suggested")})` : ""}
                  </option>
                ))}
              </select>
            )}
            <span className="caption text-muted-soft">{t("lreq.ingestHint")}</span>
          </div>
        )}

        {portal && (
          <div className="flex flex-col gap-1.5">
            <PortalBadge portal={portal} locale={locale} />
            {pdfOnly && (
              <div className="rounded-[10px] border border-warning/30 bg-warning-surface p-2.5">
                <p className="caption text-body">{t("lreq.pdf.only", { file: portal.pdf_file })}</p>
                <p className="caption mt-1 text-warning">{t("lreq.draft.hint")}</p>
              </div>
            )}
            {blocked && (
              <div className="rounded-[10px] border border-hairline bg-canvas-soft p-2.5">
                <p className="caption text-body">{t(kind === "gone" ? "lreq.gone.reason" : "lreq.none.reason")}</p>
                <p className="caption mt-1 text-muted">{t("lreq.none.hint")}</p>
              </div>
            )}
          </div>
        )}

        {action !== "ingest" && (
          <div className="flex flex-col gap-1">
            <FieldLabel htmlFor="lreq-note" required={action === "reject"}>
              {action === "reject" ? t("lreq.f.noteRequired") : t("lreq.f.noteOptional")}
            </FieldLabel>
            <textarea
              id="lreq-note"
              className="input min-h-[96px] w-full resize-y py-2 text-[13px] leading-relaxed"
              value={note}
              autoFocus
              required={action === "reject"}
              aria-required={action === "reject"}
              onChange={(e) => setNote(e.target.value)}
            />
            {action === "reject" && note.trim() === "" && (
              <span className="caption text-error">{t("common.required")}</span>
            )}
            {action === "reject" && <span className="caption text-muted-soft">{t("lreq.rejectHint")}</span>}
          </div>
        )}

        {action === "added" && (
          <div className="flex flex-col gap-1">
            <FieldLabel htmlFor="lreq-knowledge-id">{t("lreq.f.knowledgeId")}</FieldLabel>
            <input
              id="lreq-knowledge-id"
              className="input h-9 font-mono text-[13px]"
              value={knowledgeId}
              onChange={(e) => setKnowledgeId(e.target.value)}
            />
          </div>
        )}

        {error && <Notice tone="error">{error}</Notice>}

        <div className="flex justify-end gap-2 border-t border-hairline pt-4">
          <button className="btn btn-ghost btn-sm" onClick={onClose} disabled={saving}>
            {t("common.cancel")}
          </button>
          <button
            className={`btn btn-sm ${action === "reject" ? "btn-danger" : "btn-primary"}`}
            onClick={() => void submit()}
            disabled={!canSubmit}
          >
            {saving
              ? pdfOnly
                ? t("lreq.draft.fetching")
                : action === "ingest"
                  ? t("lreq.ingesting")
                  : t("common.loading")
              : pdfOnly
                ? t("lreq.draft.fetch")
                : t(`lreq.confirm.${action}` as LocaleKey)}
          </button>
        </div>
      </div>
    </Modal>
  );
}

const DRAFT_POLL_MS = 5_000;

/* The OCR draft of a request whose document vbpl.vn has only as a scanned
 * PDF: "Kéo về bản nháp" fetches it (scans of up to 20 pages are read while
 * the request waits, longer ones in the background, polled here every 5 s),
 * a failed fetch can be retried, a ready one opens the review page. */
function DraftControls({
  item,
  onDraft,
  onSettled,
}: {
  item: LegalRequestItem;
  /** The draft summary changed (fetched, polled). */
  onDraft: (d: LegalDraftSummary) => void;
  /** The background OCR ended: the queue reloads (the request moved). */
  onSettled: () => void;
}) {
  const { t } = useT();
  const draft = item.draft ?? null;
  const [fetching, setFetching] = useState(false);
  const [error, setError] = useState("");
  const onDraftRef = useRef(onDraft);
  onDraftRef.current = onDraft;
  const onSettledRef = useRef(onSettled);
  onSettledRef.current = onSettled;
  const busy = legalDraftBusy(draft?.status);
  const draftId = draft?.id ?? "";

  useEffect(() => {
    if (!busy || !draftId) return;
    let alive = true;
    const timer = setInterval(async () => {
      if (typeof document !== "undefined" && document.visibilityState === "hidden") return;
      try {
        const res = await getLegalDraft(draftId);
        const d = res.data;
        if (!alive || !d) return;
        onDraftRef.current({ id: d.id, status: d.status, ocr_pages: d.ocr_pages, error: d.error });
        if (!legalDraftBusy(d.status)) onSettledRef.current();
      } catch {
        /* the next tick tries again */
      }
    }, DRAFT_POLL_MS);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, [busy, draftId]);

  const fetchDraft = async () => {
    setFetching(true);
    setError("");
    try {
      const res = await fetchDraftForRequest(item.id);
      const d = res.data;
      if (d) onDraft({ id: d.id, status: d.status, ocr_pages: d.ocr_pages, error: d.error });
      if (d && !legalDraftBusy(d.status)) onSettled();
    } catch (e) {
      setError(errorText(e, t("lreq.draft.fetchFailed")));
    } finally {
      setFetching(false);
    }
  };

  if (fetching) return <Loading label={t("lreq.draft.fetching")} className="py-0" />;

  if (!draft || draft.status === "failed") {
    return (
      <div className="flex flex-col gap-1">
        {draft?.status === "failed" && (
          <span className="caption text-error" title={draft.error || undefined}>
            {t("lreq.draft.failed")}
            {draft.error ? `: ${draft.error}` : ""}
          </span>
        )}
        <button
          type="button"
          className="btn btn-outline btn-sm w-fit py-0.5 text-[12px]"
          onClick={() => void fetchDraft()}
          title={t("lreq.draft.fetchHint")}
        >
          {draft ? t("lreq.draft.retry") : t("lreq.draft.fetch")}
        </button>
        {error && (
          <Notice tone="error" className="mt-1">
            {error}
          </Notice>
        )}
      </div>
    );
  }

  if (busy) {
    return (
      <span className="badge-pill badge-warning inline-flex w-fit items-center gap-1 text-[10px]">
        <IconRefresh className="h-3 w-3 animate-spin motion-reduce:animate-none" />
        {draft.ocr_pages > 0 ? t("lreq.draft.ocrPages", { n: draft.ocr_pages }) : t("lreq.draft.ocr")}
      </span>
    );
  }

  if (draft.status === "approved") {
    return (
      <span className="badge-pill badge-success inline-block w-fit text-[10px]">
        {t("lreq.draft.approved")}
      </span>
    );
  }

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span
        className={`badge-pill inline-block w-fit text-[10px] ${
          draft.status === "reviewing"
            ? "badge-info"
            : "badge-warning"
        }`}
      >
        {t(draft.status === "reviewing" ? "lreq.draft.reviewing" : "lreq.draft.ready")}
        {draft.ocr_pages > 0 ? ` · ${t("lreq.draft.pages", { n: draft.ocr_pages })}` : ""}
      </span>
      {legalDraftEditable(draft.status) && (
        <Link href={legalDraftHref(draft.id, item.suggested_kb_id)} className="btn btn-primary btn-sm py-0.5 text-[12px]">
          {t("lreq.draft.edit")}
        </Link>
      )}
    </div>
  );
}
