"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import {
  batchQueryKnowledge,
  getKnowledgeBase,
  getKnowledgeDetails,
  listKnowledgeFiles,
  listOutgoingKBGrants,
  reparseKnowledge,
  requestKBAccess,
  type KBAccessGrant,
  type KnowledgeBaseRow,
  type KnowledgeDoc,
  type KnowledgeProcessOverrides,
} from "@/lib/api/knowledge";
import { ApiError } from "@/lib/api-client";
import { getSystemParseDefaults, type SystemParseDefaults } from "@/lib/api/system";
import { WikiBrowser, WikiPageView } from "@/components/wiki/wiki-browser";
import { KbSettingsModal } from "@/components/settings/kb-settings";
import { DocPanel } from "@/components/doc-panel";
import { KnowledgeGraph } from "@/components/knowledge-graph";
import { UploadModal } from "@/components/knowledge/upload-modal";
import { DocActionsMenu } from "@/components/knowledge/doc-actions-menu";
import { ParseSettingsDialog } from "@/components/knowledge/parse-settings-dialog";
import { useUploadTasks } from "@/lib/upload-tasks";
import { buildUploadFileName } from "@/lib/upload-queue";
import {
  IconChat,
  IconDoc,
  IconGraph,
  IconPlus,
  IconSearch,
  IconSettings,
} from "@/components/icons";
import { renderFileIconSvg } from "@/components/files/file-icon";
import { useT, type LocaleKey } from "@/lib/i18n";
import { useAuth, useTenantRole } from "@/lib/auth";

/* Backend field is parse_status (types.Knowledge.go ParseStatus); values
 * are pending/processing/finalizing/completed/failed/cancelled. The port
 * previously read a phantom `status` field so every document fell through
 * to "Processing". */
const STATUS_STYLE: Record<
  string,
  { labelKey: LocaleKey; fallback: string; cls: string; dot: string }
> = {
  completed: { labelKey: "status.indexed", fallback: "Indexed", cls: "text-success", dot: "#16a34a" },
  processing: { labelKey: "status.processing", fallback: "Processing", cls: "text-muted", dot: "#a8a29e" },
  finalizing: { labelKey: "status.processing", fallback: "Processing", cls: "text-muted", dot: "#a8a29e" },
  pending: { labelKey: "status.pending", fallback: "Pending", cls: "text-muted", dot: "#a8a29e" },
  failed: { labelKey: "status.failed", fallback: "Failed", cls: "text-error", dot: "#dc2626" },
  cancelled: { labelKey: "status.cancelled", fallback: "Cancelled", cls: "text-muted", dot: "#a8a29e" },
  deleting: { labelKey: "status.deleting", fallback: "Deleting", cls: "text-muted", dot: "#a8a29e" },
};

function statusStyle(status?: string) {
  return STATUS_STYLE[status ?? ""] ?? STATUS_STYLE.pending;
}

/* Ported from the Vue knowledgeNeedsStatusPolling (views/knowledge/
 * wikiStatusRefresh.ts): a row is still "in flight" while parsing runs
 * (pending/processing/finalizing) or when parsing finished but the async
 * summary generation is still running — keep polling so the description
 * fills in without a manual refresh. */
function needsStatusPolling(d: KnowledgeDoc): boolean {
  const ps = d.parse_status ?? d.status;
  if (ps === "pending" || ps === "processing" || ps === "finalizing" || ps === "deleting") return true;
  return (
    ps === "completed" &&
    (d.summary_status === "pending" || d.summary_status === "processing")
  );
}

function docName(d: KnowledgeDoc) {
  return d.title || d.file_name || d.id;
}

function docExt(d: KnowledgeDoc) {
  const name = d.file_name ?? d.title ?? "";
  const dot = name.lastIndexOf(".");
  return dot >= 0 ? name.slice(dot + 1).toUpperCase() : "FILE";
}

type MainTab = "docs-wiki" | "graph";

export function KbDetail({ kbId }: { kbId: string }) {
  const { t } = useT();
  const { isOwner, isAdminOrOwner } = useTenantRole();
  const { selectedTenantId, tenant } = useAuth();
  const [activeTab, setActiveTab] = useState<MainTab>("docs-wiki");
  const [kb, setKb] = useState<KnowledgeBaseRow | null>(null);
  const [docs, setDocs] = useState<KnowledgeDoc[] | null>(null);
  const [openDoc, setOpenDoc] = useState<KnowledgeDoc | null>(null);
  const [selectedWikiSlug, setSelectedWikiSlug] = useState<string | null>(null);
  const [q, setQ] = useState("");
  const [wikiQ, setWikiQ] = useState("");
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [droppedFiles, setDroppedFiles] = useState<File[]>([]);
  /* Files handed off from the picker, awaiting parse-settings confirmation
   * (non-null = the dialog is open in "file" mode). */
  const [confirmFiles, setConfirmFiles] = useState<File[] | null>(null);
  /* Rebuild flow: the doc plus the overrides it was last parsed with. */
  const [reparseTarget, setReparseTarget] = useState<{
    doc: KnowledgeDoc;
    overrides: KnowledgeProcessOverrides | null;
  } | null>(null);
  const [error, setError] = useState("");
  /* 403/404 on the KB fetch usually means a foreign tenant-scoped KB: the
   * owner can file a cross-tenant access request straight from here. */
  const [denied, setDenied] = useState(false);
  const [grantForKb, setGrantForKb] = useState<KBAccessGrant | null>(null);
  const [grantChecked, setGrantChecked] = useState(false);
  const [reqSent, setReqSent] = useState(false);
  const [reqMessage, setReqMessage] = useState("");
  const [reqBusy, setReqBusy] = useState(false);
  /* Platform-wide parse defaults. When enabled, uploads and rebuilds skip
   * the parse-settings dialog and run with these overrides — the backend
   * ignores any per-upload process_config anyway. */
  const [parseDefaults, setParseDefaults] = useState<SystemParseDefaults | null>(null);
  const { enqueue } = useUploadTasks();

  const defaultsLocked = parseDefaults?.enabled === true;
  /* Strip the `enabled` flag — the stored shape is
   * {enabled, ...KnowledgeProcessOverrides} so the rest can be sent as
   * process_config verbatim. */
  const lockedProcessConfig: KnowledgeProcessOverrides | undefined = parseDefaults
    ? (() => {
        const { enabled: _enabled, ...overrides } = parseDefaults;
        return overrides;
      })()
    : undefined;

  const reloadDocs = () => {
    listKnowledgeFiles(kbId, { page: 1, page_size: 100 })
      .then((res) => {
        const data = res.data;
        setDocs(Array.isArray(data) ? data : (data?.items ?? []));
      })
      .catch(() => setDocs([]));
  };

  /* Document actions that change the row set (delete/move/cancel) also
   * affect the header's document counter. */
  const refreshAfterDocChange = () => {
    reloadDocs();
    getKnowledgeBase(kbId)
      .then((row) => setKb(row ?? null))
      .catch(() => {});
  };

  /* Deletion is asynchronous server-side: flag the row so the status poll
   * keeps tracking it and drops it once the batch endpoint stops
   * returning the id. */
  const markDocDeleting = (doc: KnowledgeDoc) => {
    setOpenDoc((prev) => (prev?.id === doc.id ? null : prev));
    setDocs((prev) =>
      prev?.map((d) => (d.id === doc.id ? { ...d, parse_status: "deleting" } : d)) ?? prev,
    );
  };

  useEffect(() => {
    let alive = true;
    getKnowledgeBase(kbId)
      .then((row) => alive && setKb(row ?? null))
      .catch((e) => {
        if (!alive) return;
        /* KBAccessRead returns 403/404 for foreign tenant-scoped KBs —
         * swap the content area for the request-access panel. */
        if (e instanceof ApiError && (e.status === 403 || e.status === 404)) {
          setDenied(true);
          return;
        }
        setError(e instanceof Error ? e.message : "Failed to load");
      });
    listKnowledgeFiles(kbId, { page: 1, page_size: 100 })
      .then((res) => {
        if (!alive) return;
        const data = res.data;
        setDocs(Array.isArray(data) ? data : (data?.items ?? []));
      })
      .catch(() => alive && setDocs([]));
    getSystemParseDefaults()
      .then((def) => alive && setParseDefaults(def))
      .catch(() => alive && setParseDefaults(null));
    return () => {
      alive = false;
    };
  }, [kbId]);

  /* Denied page: check whether this workspace already filed a request for
   * this KB so the panel can show its status instead of a fresh form.
   * The outgoing-grants list is Owner-gated, so skip it for members. */
  useEffect(() => {
    if (!denied || !isOwner) return;
    let alive = true;
    listOutgoingKBGrants(["pending", "approved"])
      .then((rows) => {
        if (!alive) return;
        setGrantForKb(rows.find((g) => g.kb_id === kbId) ?? null);
        setGrantChecked(true);
      })
      .catch(() => alive && setGrantChecked(true));
    return () => {
      alive = false;
    };
  }, [denied, isOwner, kbId]);

  const submitAccessRequest = async (e: React.FormEvent) => {
    e.preventDefault();
    setReqBusy(true);
    setError("");
    try {
      const res = await requestKBAccess(kbId, {
        message: reqMessage.trim() || undefined,
      });
      if (res.success) {
        setReqSent(true);
        if (res.data) setGrantForKb(res.data);
      } else {
        setError(res.message || "Request failed");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Request failed");
    } finally {
      setReqBusy(false);
    }
  };

  /* Open a document card by knowledge id — from the upload panel's "Open"
   * action (weknora:open-knowledge / ?knowledge_id=) or the references
   * drawer. Falls back to the detail endpoint when the row isn't in the
   * currently loaded page. */
  const openDocById = (knowledgeId: string) => {
    const cached = docs?.find((d) => d.id === knowledgeId);
    if (cached) {
      setOpenDoc(cached);
      return;
    }
    getKnowledgeDetails(knowledgeId)
      .then((res) => {
        const detail = (res as { data?: KnowledgeDoc })?.data;
        if (detail && typeof detail === "object") setOpenDoc(detail);
      })
      .catch(() => {});
  };

  /* Rebuild a document through the parse-settings dialog, seeded with the
   * overrides stored at its last parse (Vue KnowledgeBase.vue
   * confirmRebuildKnowledge → uploadConfirm mode 'reparse'). */
  const openReparse = async (doc: KnowledgeDoc) => {
    /* Locked mode: rebuild immediately with the system defaults — no dialog,
     * no per-document overrides to seed. */
    if (defaultsLocked) {
      reparseKnowledge(doc.id, { process_config: lockedProcessConfig })
        .then(reloadDocs)
        .catch(() => setError(t("doc.actionFailed")));
      return;
    }
    let overrides: KnowledgeProcessOverrides | null = null;
    try {
      const res = (await getKnowledgeDetails(doc.id)) as {
        data?: KnowledgeDoc & { metadata?: { process_overrides?: KnowledgeProcessOverrides } };
      };
      overrides = res?.data?.metadata?.process_overrides ?? null;
    } catch {
      /* fall back to the KB defaults when the detail call fails */
    }
    setReparseTarget({ doc, overrides });
  };

  /* The upload queue announces each landed file and the settled batch via
   * `knowledgeFileUploaded`; reload so new rows appear mid-batch, not only
   * at the end (matches Vue KnowledgeBase.vue's handleFileUploaded). */
  useEffect(() => {
    const onUploaded = (e: Event) => {
      const detail = (e as CustomEvent<{ kbId?: string }>).detail;
      if (detail?.kbId && detail.kbId !== kbId) return;
      reloadDocs();
      getKnowledgeBase(kbId)
        .then((row) => setKb(row ?? null))
        .catch(() => {});
    };
    const onOpen = (e: Event) => {
      const detail = (e as CustomEvent<{ kbId?: string; knowledgeId?: string }>).detail;
      if (!detail?.knowledgeId || (detail.kbId && detail.kbId !== kbId)) return;
      openDocById(detail.knowledgeId);
    };
    /* Files dropped anywhere on this page (GlobalFileDrop) open the upload
     * dialog prefilled, matching the Vue confirmation flow. */
    const onDrop = (e: Event) => {
      const detail = (e as CustomEvent<{ kbId?: string; files?: File[] }>).detail;
      if (detail?.kbId !== kbId || !detail.files?.length) return;
      setDroppedFiles(detail.files);
      setUploadOpen(true);
    };
    window.addEventListener("knowledgeFileUploaded", onUploaded);
    window.addEventListener("weknora:open-knowledge", onOpen);
    window.addEventListener("weknora:knowledge-file-drop", onDrop);
    return () => {
      window.removeEventListener("knowledgeFileUploaded", onUploaded);
      window.removeEventListener("weknora:open-knowledge", onOpen);
      window.removeEventListener("weknora:knowledge-file-drop", onDrop);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kbId, docs]);

  /* Deep link: /platform/knowledge-bases/<id>?knowledge_id=<docId> opens the
   * document panel once the list has loaded. */
  useEffect(() => {
    if (docs === null) return;
    const id = new URLSearchParams(window.location.search).get("knowledge_id");
    if (id) openDocById(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [docs === null]);

  /* Ported from KnowledgeBase.vue's updateStatus loop: while any row is
   * still in flight, re-query just those ids every 1.5s so cards move
   * Pending → Processing → Indexed live after an upload. Each poll writes
   * a fresh docs array so the effect re-schedules itself until every row
   * settles (or the component unmounts / kbId changes). */
  useEffect(() => {
    const inflight = (docs ?? []).filter(needsStatusPolling);
    if (inflight.length === 0) return;
    const timer = setTimeout(() => {
      const qs = inflight.map((d) => `ids=${encodeURIComponent(d.id)}`).join("&");
      const requested = new Set(inflight.map((d) => d.id));
      batchQueryKnowledge(qs, kbId)
        .then((res) => {
          const rows = res?.data;
          /* A valid array is authoritative for the queried ids: a doc the
           * response no longer contains was deleted (async delete finishes
           * between polls), so drop it instead of leaving a stale card. */
          if (Array.isArray(rows)) {
            const byId = new Map(rows.map((r) => [r.id, r]));
            setDocs(
              (prev) =>
                prev
                  ?.filter((d) => !requested.has(d.id) || byId.has(d.id))
                  .map((d) => {
                    const r = byId.get(d.id);
                    return r ? { ...d, ...r } : d;
                  }) ?? prev,
            );
          } else {
            setDocs((prev) => (prev ? [...prev] : prev));
          }
        })
        .catch(() => {
          /* Transient poll errors shouldn't stop the loop — force the
           * effect to re-run and try again next tick. */
          setDocs((prev) => (prev ? [...prev] : prev));
        });
    }, 1500);
    return () => clearTimeout(timer);
  }, [docs, kbId]);

  const filtered = (docs ?? []).filter(
    (d) => !q || docName(d).toLowerCase().includes(q.toLowerCase()),
  );

  /* A KB owned by another tenant reaches this view via 'public' visibility
   * or an approved access grant — mark it so the header shows Shared. */
  const activeTenantId = selectedTenantId ?? String(tenant?.id ?? "");
  const isForeignKb =
    kb?.tenant_id !== undefined && String(kb.tenant_id) !== activeTenantId;

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      {/* Header */}
      <div className="shrink-0 px-4 pt-5 pb-4 sm:px-6 lg:px-10 lg:pt-7">
        <div className="caption mb-4 flex items-center gap-2 text-muted">
          <Link href="/platform/knowledge-bases" className="hover:text-ink">
            Knowledge bases
          </Link>
          <span>/</span>
          <span className="text-ink">{kb?.name ?? kbId}</span>
        </div>

        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h1 className="display-lg">{kb?.name ?? "Knowledge base"}</h1>
            <p className="body-sm mt-1.5 max-w-[560px] text-body">{kb?.description ?? ""}</p>
            {error && <p className="caption mt-2 text-error">{error}</p>}
            <div className="caption mt-2.5 flex items-center gap-4 text-muted">
              <span>{kb?.knowledge_count ?? kb?.document_count ?? docs?.length ?? "—"} documents</span>
              {kb?.visibility === "public" && (
                <span className="badge-pill">{t("kbList.publicBadge")}</span>
              )}
              {isForeignKb && <span className="badge-pill">{t("kbList.sharedBadge")}</span>}
              {kb?.updated_at && <span className="whitespace-nowrap">Updated {fmtShortDate(kb.updated_at)}</span>}
            </div>
          </div>
          <div className="flex gap-3">
            {isOwner && !denied && (
              <>
                <button className="btn btn-outline" onClick={() => setSettingsOpen(true)}>
                  <IconSettings className="h-4 w-4" /> Settings
                </button>
                <button className="btn btn-outline" onClick={() => setUploadOpen(true)}>
                  <IconPlus className="h-4 w-4" /> Upload files
                </button>
              </>
            )}
            {!denied && (
              <Link
                href={`/platform/knowledge-bases/${kbId}/creatChat`}
                className="btn btn-primary"
              >
                <IconChat className="h-4 w-4" /> Chat
              </Link>
            )}
          </div>
        </div>

        {/* Page Main Navigation Tabs */}
        <div className="mt-5 flex items-center gap-1 rounded-full bg-surface-strong p-1 w-fit">
          <button
            onClick={() => setActiveTab("docs-wiki")}
            className={`flex items-center gap-2 rounded-full px-4 py-1.5 text-[13px] font-medium transition-colors ${
              activeTab === "docs-wiki"
                ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.08)]"
                : "text-muted hover:text-ink"
            }`}
          >
            <IconDoc className="h-4 w-4" />
            <span>Tài liệu & Wiki</span>
            {docs !== null && (
              <span className="rounded-full bg-surface-strong px-2 py-0.5 text-[11px] font-medium text-muted">
                {docs.length}
              </span>
            )}
          </button>

          <button
            onClick={() => setActiveTab("graph")}
            className={`flex items-center gap-2 rounded-full px-4 py-1.5 text-[13px] font-medium transition-colors ${
              activeTab === "graph"
                ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.08)]"
                : "text-muted hover:text-ink"
            }`}
          >
            <IconGraph className="h-4 w-4" />
            <span>Knowledge graph</span>
          </button>
        </div>
      </div>

      {/* Main Content Area */}
      {denied ? (
        /* Foreign tenant-scoped KB (or a deleted one): the owner can file a
         * cross-tenant access request; other roles only get the notice. */
        <div className="flex min-h-0 flex-1 px-4 pb-4 sm:px-6 lg:px-10 lg:pb-6">
          <div className="card mx-auto mt-10 h-fit w-full max-w-[480px] p-6">
            <h2 className="title-sm font-semibold text-ink">{t("kbGrants.deniedTitle")}</h2>
            <p className="body-sm mt-2 text-muted">{t("kbGrants.deniedDesc")}</p>
            {error && <p className="caption mt-3 text-error">{error}</p>}
            {isOwner ? (
              reqSent || grantForKb?.status === "pending" ? (
                <p className="body-sm mt-4 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-amber-700 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-400">
                  {t("kbGrants.requestSent")}
                </p>
              ) : grantForKb?.status === "approved" ? (
                /* Grant exists but the fetch still failed — likely a stale
                 * cache or expiry; offer a reload. */
                <button
                  className="btn btn-outline btn-sm mt-4"
                  onClick={() => window.location.reload()}
                >
                  {t("common.refresh")}
                </button>
              ) : (
                grantChecked && (
                  <form onSubmit={submitAccessRequest} className="mt-4 space-y-3">
                    <input
                      className="input w-full text-[13px]"
                      placeholder={t("kbGrants.requestMessage")}
                      value={reqMessage}
                      onChange={(e) => setReqMessage(e.target.value)}
                    />
                    <button
                      type="submit"
                      disabled={reqBusy}
                      className="btn btn-primary btn-sm"
                    >
                      {reqBusy ? "…" : t("kbGrants.requestSubmit")}
                    </button>
                  </form>
                )
              )
            ) : (
              <p className="caption mt-4 text-muted-soft">{t("kbGrants.deniedMember")}</p>
            )}
          </div>
        </div>
      ) : (
      <div className="flex min-h-0 flex-1 px-4 pb-4 sm:px-6 lg:px-10 lg:pb-6">
        {activeTab === "docs-wiki" ? (
          /* TAB 1: Split view — Left Wiki (smaller width), Right Document Cards */
          <div className="flex min-h-0 flex-1 flex-col gap-6 lg:flex-row">
            {/* Left: Wiki Section (increased width) */}
            <section className="card flex min-h-0 flex-col overflow-hidden w-full lg:w-[440px] xl:w-[480px] shrink-0">
              {/* Wiki Search & Header */}
              <div className="flex shrink-0 items-center justify-between border-b border-hairline px-4 py-2.5">
                <div className="flex items-center gap-2 font-medium text-[13.5px] text-ink">
                  <IconDoc className="h-4 w-4 text-muted" />
                  <span>Wiki</span>
                </div>
                <span className="text-[11.5px] text-muted-soft">Mục lục tri thức</span>
              </div>
              <div className="caption flex items-center gap-2 border-b border-hairline px-4 py-2">
                <IconSearch className="h-3.5 w-3.5 text-muted-soft shrink-0" />
                <input
                  className="w-full bg-transparent text-[12.5px] outline-none placeholder:text-muted-soft"
                  placeholder="Search wiki pages…"
                  value={wikiQ}
                  onChange={(e) => setWikiQ(e.target.value)}
                />
              </div>
              {/* Wiki Browser Tree */}
              <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
                <WikiBrowser kbId={kbId} q={wikiQ} />
              </div>
            </section>

            {/* Right: Document Cards Grid */}
            <section className="flex min-h-0 flex-1 flex-col min-w-0">
              {/* Document Search and Action Bar */}
              <div className="mb-3.5 flex shrink-0 items-center justify-between gap-4">
                <div className="relative w-full max-w-[320px]">
                  <IconSearch className="pointer-events-none absolute left-3.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-soft" />
                  <input
                    className="input h-9 pl-9 text-[13px]"
                    placeholder="Search documents…"
                    value={q}
                    onChange={(e) => setQ(e.target.value)}
                  />
                </div>
                <div className="flex min-w-0 items-center gap-3">
                  {defaultsLocked && (
                    <span
                      className="caption hidden min-w-0 truncate text-muted md:inline"
                      title={t("pd.lockedBanner")}
                    >
                      {t("pd.lockedBanner")}
                    </span>
                  )}
                  <span className="caption text-muted">{filtered.length} files</span>
                  {isOwner && (
                    <button
                      onClick={() => setUploadOpen(true)}
                      className="btn btn-outline btn-sm h-8"
                    >
                      <IconPlus className="h-3.5 w-3.5" /> Upload
                    </button>
                  )}
                </div>
              </div>

              {/* Cards Grid: 5 cards per row on desktop */}
              <div className="grid min-h-0 flex-1 auto-rows-min grid-cols-1 gap-2.5 overflow-y-auto pr-1 sm:grid-cols-2 md:grid-cols-3 xl:grid-cols-5">
                {filtered.map((d) => {
                  const st = statusStyle(d.parse_status ?? d.status);
                  return (
                    <div
                      key={d.id}
                      role="button"
                      tabIndex={0}
                      onClick={() => setOpenDoc(d)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault();
                          setOpenDoc(d);
                        }
                      }}
                      className="card card-hover group relative flex flex-col justify-between p-3 text-left transition-all hover:border-ink/20"
                    >
                      <div className="absolute right-2 top-2">
                        <DocActionsMenu
                          doc={d}
                          kbId={kbId}
                          canMutate={isAdminOrOwner}
                          onChanged={refreshAfterDocChange}
                          onDeleted={markDocDeleting}
                          onReparse={openReparse}
                        />
                      </div>
                      <div>
                        <div className="flex items-start gap-2">
                          <span
                            className="w-[26px] shrink-0 mt-0.5"
                            dangerouslySetInnerHTML={{
                              __html: renderFileIconSvg(
                                d.file_name ?? d.title ?? "",
                                d.file_type,
                                d.profile?.doc_type,
                              ),
                            }}
                          />
                          <div className="min-w-0 flex-1">
                            <div className="truncate text-[13px] font-medium text-ink" title={docName(d)}>
                              {docName(d)}
                            </div>
                            <div className="caption mt-0.5 flex flex-wrap items-center gap-1 text-muted">
                              {d.profile?.doc_type && (
                                <span
                                  className="badge-pill min-w-0 max-w-full text-[10px] py-0 px-1"
                                  title={d.profile.doc_type}
                                >
                                  <span className="truncate">{d.profile.doc_type}</span>
                                </span>
                              )}
                              <span className="text-[11px]">{docExt(d)}</span>
                              {d.file_size ? (
                                <span className="text-muted-soft text-[11px]">· {fmtBytes(d.file_size)}</span>
                              ) : null}
                            </div>
                          </div>
                        </div>

                        {/* Shortened summary / gist: 1 line */}
                        {(d.description || d.profile?.gist) && (
                          <p
                            className="mt-1.5 text-[11.5px] line-clamp-1 text-muted leading-normal"
                            title={d.description || d.profile?.gist}
                          >
                            {d.description || d.profile?.gist}
                          </p>
                        )}

                        {/* Topics */}
                        {d.profile?.topics && d.profile.topics.length > 0 && (
                          <div className="mt-2 flex flex-wrap gap-1">
                            {d.profile.topics.slice(0, 2).map((topic) => (
                              <span
                                key={topic}
                                className="inline-block max-w-[85px] truncate rounded-full bg-surface-strong px-1.5 py-0.2 text-[9.5px] text-muted"
                                title={topic}
                              >
                                {topic}
                              </span>
                            ))}
                            {d.profile.topics.length > 2 && (
                              <span className="text-[9.5px] text-muted-soft self-center">
                                +{d.profile.topics.length - 2}
                              </span>
                            )}
                          </div>
                        )}
                      </div>

                      {/* Bottom Status & Date */}
                      <div className="mt-2.5 flex items-center justify-between border-t border-hairline pt-2 text-[11px] text-muted">
                        <span className={`flex items-center gap-1.5 font-medium ${st.cls}`}>
                          <span
                            className="inline-block h-1.5 w-1.5 rounded-full"
                            style={{ background: st.dot }}
                          />
                          {t(st.labelKey) || st.fallback}
                        </span>
                        <span className="whitespace-nowrap text-muted-soft text-[10.5px]">
                          {fmtShortDate(d.updated_at)}
                        </span>
                      </div>
                    </div>
                  );
                })}
              </div>

              {docs !== null && docs.length === 0 && (
                <div className="mt-8 flex flex-col items-center justify-center rounded-xl border border-dashed border-hairline p-8 text-center">
                  <IconDoc className="mb-2 h-8 w-8 text-muted-soft" />
                  <p className="body-sm font-medium text-ink">Chưa có tài liệu nào</p>
                  <p className="caption mt-1 text-muted">Tải lên tài liệu để hệ thống bắt đầu xử lý và xây dựng wiki.</p>
                  {isOwner && (
                    <button onClick={() => setUploadOpen(true)} className="btn btn-primary btn-sm mt-4">
                      <IconPlus className="h-3.5 w-3.5" /> Tải lên tài liệu
                    </button>
                  )}
                </div>
              )}

              {docs !== null && docs.length > 0 && filtered.length === 0 && (
                <div className="mt-8 flex flex-col items-center justify-center text-center">
                  <p className="body-md text-muted">Không tìm thấy tài liệu phù hợp.</p>
                  <p className="caption mt-1 text-muted-soft">Thử tìm kiếm với từ khóa khác.</p>
                </div>
              )}
            </section>

          </div>
        ) : (
          /* TAB 2: Full Knowledge Graph */
          <section className="card flex min-h-0 flex-1 flex-col overflow-hidden">
            <KnowledgeGraph
              kbId={kbId}
              onSelectSlug={(slug) => setSelectedWikiSlug(slug)}
            />
          </section>
        )}
      </div>
      )}

      {/* Slide-in Panels & Modals */}
      <DocPanel doc={openDoc} onClose={() => setOpenDoc(null)} />

      {selectedWikiSlug && (
        <WikiPageView
          kbId={kbId}
          slug={selectedWikiSlug}
          title={selectedWikiSlug}
          onClose={() => setSelectedWikiSlug(null)}
          onNavigate={(nextSlug) => setSelectedWikiSlug(nextSlug)}
        />
      )}

      <KbSettingsModal
        kbId={kbId}
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
        onSaved={() => {
          getKnowledgeBase(kbId).then((row) => setKb(row ?? null)).catch(() => {});
        }}
      />
      <UploadModal
        kbId={kbId}
        kbName={kb?.name ?? ""}
        open={uploadOpen}
        onClose={() => {
          setUploadOpen(false);
          setDroppedFiles([]);
        }}
        initialFiles={droppedFiles}
        onProceed={(files) => {
          setUploadOpen(false);
          /* Locked mode: enqueue immediately with the system defaults —
           * the parse-settings dialog is bypassed entirely. */
          if (defaultsLocked) {
            enqueue({
              kbId,
              kbName: kb?.name ?? "",
              processConfig: lockedProcessConfig,
              uploads: files.map((f) => ({
                file: f,
                fileName: buildUploadFileName(f, ""),
              })),
            });
            return;
          }
          setConfirmFiles(files);
        }}
      />

      {/* Parse-settings confirmation — Vue UploadConfirmDialog equivalent.
       * file mode: picked files; reparse mode: one document's rebuild. */}
      <ParseSettingsDialog
        open={confirmFiles !== null}
        mode="file"
        kb={kb}
        files={confirmFiles ?? []}
        onCancel={() => {
          /* Back to the picker with the batch restored. */
          setDroppedFiles(confirmFiles ?? []);
          setConfirmFiles(null);
          setUploadOpen(true);
        }}
        onConfirm={(result) => {
          enqueue({
            kbId,
            kbName: kb?.name ?? "",
            tagIds: result.tagIds.length ? result.tagIds : undefined,
            processConfig: result.processConfig,
            uploads: result.files.map((f) => ({
              file: f,
              fileName: buildUploadFileName(f, ""),
            })),
          });
          setConfirmFiles(null);
        }}
      />
      <ParseSettingsDialog
        open={reparseTarget !== null}
        mode="reparse"
        kb={kb}
        reparse={
          reparseTarget
            ? {
                fileName: docName(reparseTarget.doc),
                fileType: reparseTarget.doc.file_type,
                overrides: reparseTarget.overrides,
              }
            : null
        }
        onCancel={() => setReparseTarget(null)}
        onConfirm={(result) => {
          const doc = reparseTarget?.doc;
          setReparseTarget(null);
          if (!doc) return;
          reparseKnowledge(doc.id, { process_config: result.processConfig })
            .then(reloadDocs)
            .catch(() => setError(t("doc.actionFailed")));
        }}
      />
    </div>
  );
}

/* Compact header timestamp — RFC3339 is too long for the stats row. */
function fmtShortDate(v?: string): string {
  if (!v) return "";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return v;
  return d.toLocaleDateString(undefined, { year: "2-digit", month: "short", day: "numeric" });
}

/* Human file size for the metadata row. */
function fmtBytes(bytes: number): string {
  if (!bytes) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1);
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`;
}
