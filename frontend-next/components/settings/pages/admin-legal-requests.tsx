"use client";

/* SuperAdmin queue of "đề nghị bổ sung văn bản": legal documents users named
 * in chat that no knowledge base holds the full text of. Shown as the
 * "Document requests" tab of the legal documents page
 * (admin-legal-documents.tsx). Backend: /api/v1/system/legal-requests
 * (+ /:id, /:id/portal-check, /public-knowledge-bases) — SuperAdmin-only.
 * A document vbpl.vn has only as an original PDF scan can be loaded with
 * OCR (allow_pdf); the portal check says which form exists. */

import { useCallback, useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { IconRefresh } from "@/components/icons";
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

const PAGE_SIZE = 50;

const FILTERS: { key: LegalRequestFilter; labelKey: LocaleKey }[] = [
  { key: "requested", labelKey: "lreq.filter.requested" },
  { key: "added", labelKey: "lreq.filter.added" },
  { key: "rejected", labelKey: "lreq.filter.rejected" },
  { key: "all", labelKey: "lreq.filter.all" },
];

type Action = "ingest" | "reject" | "added";

function statusTone(status: string): string {
  if (status.startsWith("Còn hiệu lực")) return "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400";
  return "bg-surface-strong text-muted";
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
  html: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
  pdf: "bg-amber-500/10 text-amber-700 dark:text-amber-400",
  none: "bg-surface-strong text-muted",
  gone: "bg-surface-strong text-muted",
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
  return c ? c.requested + c.added + c.rejected : 0;
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
  const [filter, setFilter] = useState<LegalRequestFilter>("requested");
  const [items, setItems] = useState<LegalRequestItem[]>([]);
  const [total, setTotal] = useState(0);
  const [counts, setCounts] = useState<LegalRequestCounts | null>(null);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
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
    counts ? (k === "all" ? countTotal(counts) : counts[k]) : null;

  return (
    <div>
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

      {notice && <p className="caption mb-3 text-emerald-700 dark:text-emerald-400">{notice}</p>}
      {error && <p className="caption mb-3 text-error">{error}</p>}

      <div className="mb-5 inline-flex flex-wrap items-center gap-0.5 rounded-full border border-hairline bg-surface-strong/60 p-0.5">
        {FILTERS.map((f) => {
          const n = countOf(f.key);
          return (
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
              {n !== null && <span className="ml-1 tabular-nums text-muted-soft">({n})</span>}
            </button>
          );
        })}
      </div>

      <div className="card overflow-x-auto shadow-sm">
        <table className="w-full min-w-[980px] border-collapse text-left">
          <thead>
            <tr className="caption-uppercase border-b border-hairline bg-surface-strong/30 text-muted">
              <th className="min-w-[260px] px-5 py-3 font-medium">{t("lreq.col.document")}</th>
              <th className="w-[150px] px-5 py-3 font-medium">{t("lreq.col.validity")}</th>
              <th className="w-[80px] px-5 py-3 font-medium">{t("lreq.col.count")}</th>
              <th className="w-[150px] px-5 py-3 font-medium">{t("lreq.col.lastAsked")}</th>
              <th className="min-w-[200px] px-5 py-3 font-medium">{t("lreq.col.lastQuery")}</th>
              <th className="min-w-[200px] px-5 py-3 font-medium">{t("lreq.col.action")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-hairline">
            {items.map((item) => (
              <RequestRow
                key={item.id}
                item={item}
                locale={locale}
                onAct={(action) => {
                  setNotice("");
                  setActing({ item, action });
                }}
                onUpdated={(updated) => setItems((cur) => cur.map((it) => (it.id === updated.id ? updated : it)))}
              />
            ))}
            {items.length === 0 && (
              <tr>
                <td colSpan={6} className="px-5 py-12 text-center text-[14px] text-muted">
                  {loading ? t("usrp.loading") : t("lreq.empty")}
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

      {acting && (
        <DecisionDialog
          item={acting.item}
          action={acting.action}
          onReload={async () => (await load())?.find((it) => it.id === acting.item.id) ?? null}
          onClose={() => setActing(null)}
          onDone={(updated) => {
            setActing(null);
            setNotice(
              t(`lreq.done.${acting.action}` as LocaleKey, {
                number: updated.document_number || acting.item.document_number,
              }),
            );
            void load();
          }}
        />
      )}
    </div>
  );
}

function RequestRow({
  item,
  locale,
  onAct,
  onUpdated,
}: {
  item: LegalRequestItem;
  locale: string;
  onAct: (action: Action) => void;
  onUpdated: (item: LegalRequestItem) => void;
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
  const decided = item.status !== "requested";
  return (
    <tr className="align-top">
      <td className="px-5 py-3.5">
        <div className="text-[13px] font-semibold text-ink">{label || "—"}</div>
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
            className="caption mt-0.5 inline-block text-[#1f6feb] hover:underline"
          >
            vbpl.vn
          </a>
        )}
      </td>
      <td className="px-5 py-3.5">
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
      <td className="px-5 py-3.5">
        <span className="text-[13px] tabular-nums text-ink">{item.request_count}</span>
      </td>
      <td className="px-5 py-3.5">
        <span className="caption tabular-nums text-body">{formatDateTime(item.last_requested_at, locale) || "—"}</span>
      </td>
      <td className="px-5 py-3.5">
        <div className="caption line-clamp-2 max-w-[320px] text-body" title={item.last_query}>
          {item.last_query || "—"}
        </div>
      </td>
      <td className="px-5 py-3.5">
        {decided ? (
          <div className="flex flex-col gap-0.5">
            <span
              className={`badge-pill inline-block w-fit text-[10px] ${
                item.status === "added"
                  ? "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400"
                  : "bg-red-500/10 text-red-700 dark:text-red-400"
              }`}
            >
              {t(`lreq.state.${item.status}` as LocaleKey)}
            </span>
            <span className="caption tabular-nums text-muted">
              {[formatDateTime(item.decided_at, locale), item.decided_by].filter(Boolean).join(" · ")}
            </span>
            {item.decision_note && (
              <span className="caption text-body" title={item.decision_note}>
                {item.decision_note}
              </span>
            )}
            {item.status === "added" && parsing && (
              <span className="badge-pill inline-block w-fit bg-amber-500/10 text-[10px] text-amber-700 dark:text-amber-400">
                {t("lreq.parse.processing")}
              </span>
            )}
            {item.status === "added" && item.knowledge_parse_status === "failed" && (
              <span className="badge-pill inline-block w-fit bg-red-500/10 text-[10px] text-red-700 dark:text-red-400">
                {t("lreq.parse.failed")}
              </span>
            )}
          </div>
        ) : (
          <div className="flex flex-col gap-1.5">
            <div className="flex flex-wrap gap-1.5">
              {item.legal_document_id && (
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
            {checkError && <span className="caption text-error">{checkError}</span>}
          </div>
        )}
      </td>
    </tr>
  );
}

function DecisionDialog({
  item: initial,
  action,
  onReload,
  onClose,
  onDone,
}: {
  item: LegalRequestItem;
  action: Action;
  /** Reloads the queue and returns this request as the server now has it. */
  onReload: () => Promise<LegalRequestItem | null>;
  onClose: () => void;
  onDone: (updated: LegalRequestItem) => void;
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
    !saving && (action === "ingest" ? !!kbId && !blocked : action === "reject" ? note.trim() !== "" : true);

  const submit = async () => {
    if (!canSubmit) return;
    setSaving(true);
    setError("");
    try {
      const res = await decideLegalRequest(
        item.id,
        action === "ingest"
          ? { action, knowledge_base_id: kbId, ...(pdfOnly ? { allow_pdf: true } : {}) }
          : action === "reject"
            ? { action, note: note.trim() }
            : { action, note: note.trim() || undefined, knowledge_id: knowledgeId.trim() || undefined },
      );
      onDone(res.data ?? item);
    } catch (e) {
      // a refusal for lack of HTML (only a PDF scan, or nothing) stored the
      // portal availability on the request: reload it to offer the PDF
      // option or say why nothing can be loaded
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

        {action === "ingest" && (
          <label className="flex flex-col gap-1">
            <span className="caption text-muted">{t("lreq.f.kb")}</span>
            {kbs === null ? (
              <span className="caption text-muted">{t("usrp.loading")}</span>
            ) : kbs.length === 0 ? (
              <span className="caption text-muted">{t("lreq.noKb")}</span>
            ) : (
              <select className="input h-9 py-0 text-[13px]" value={kbId} onChange={(e) => setKbId(e.target.value)}>
                {kbs.map((k) => (
                  <option key={k.id} value={k.id}>
                    {k.name}
                    {k.id === item.suggested_kb_id ? ` (${t("lreq.suggested")})` : ""}
                  </option>
                ))}
              </select>
            )}
            <span className="caption text-muted-soft">{t("lreq.ingestHint")}</span>
          </label>
        )}

        {portal && (
          <div className="flex flex-col gap-1.5">
            <PortalBadge portal={portal} locale={locale} />
            {pdfOnly && (
              <div className="rounded-[10px] border border-amber-500/30 bg-amber-500/5 p-2.5">
                <p className="caption text-body">{t("lreq.pdf.only", { file: portal.pdf_file })}</p>
                <p className="caption mt-1 text-amber-700 dark:text-amber-400">{t("lreq.pdf.warning")}</p>
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
          <label className="flex flex-col gap-1">
            <span className="caption text-muted">
              {action === "reject" ? t("lreq.f.noteRequired") : t("lreq.f.noteOptional")}
            </span>
            <textarea
              className="input min-h-[96px] w-full resize-y py-2 text-[13px] leading-relaxed"
              value={note}
              autoFocus
              onChange={(e) => setNote(e.target.value)}
            />
            {action === "reject" && <span className="caption text-muted-soft">{t("lreq.rejectHint")}</span>}
          </label>
        )}

        {action === "added" && (
          <label className="flex flex-col gap-1">
            <span className="caption text-muted">{t("lreq.f.knowledgeId")}</span>
            <input
              className="input h-9 font-mono text-[13px]"
              value={knowledgeId}
              onChange={(e) => setKnowledgeId(e.target.value)}
            />
          </label>
        )}

        {error && <p className="caption text-error">{error}</p>}

        <div className="flex justify-end gap-2 border-t border-hairline pt-4">
          <button className="btn btn-ghost btn-sm" onClick={onClose} disabled={saving}>
            {t("common.cancel")}
          </button>
          <button className="btn btn-primary btn-sm" onClick={() => void submit()} disabled={!canSubmit}>
            {saving
              ? action === "ingest"
                ? t("lreq.ingesting")
                : "…"
              : pdfOnly
                ? t("lreq.pdf.confirm")
                : t(`lreq.confirm.${action}` as LocaleKey)}
          </button>
        </div>
      </div>
    </Modal>
  );
}
