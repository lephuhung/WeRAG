"use client";

/* Review page of a legal OCR draft (SuperAdmin): vbpl.vn has the document
 * only as a scanned PDF, which was OCRed into a .docx. Left: the original
 * scan; right: the draft in the editor (the same DocumentPane as the chat's
 * document assistant: forcesave, revisions, live spelling, plugin ops), on
 * the hidden session the server opened in the platform organization (its
 * document routes are sent with X-Tenant-ID = tenant_id, see
 * setDocumentSessionTenant). A side panel shows the structure check
 * (Phần/Chương/Mục/Điều, khoản/điểm, warnings) and the spelling findings.
 * "Duyệt và nạp" is enabled only once the last structure check is ready. */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Modal } from "@/components/modal";
import { useConfirm } from "@/components/confirm-dialog";
import { useToast } from "@/components/toast";
import { IconChevronLeft, IconExternal, IconRefresh } from "@/components/icons";
import { DocumentPane } from "@/components/doc-workspace/document-pane";
import { SplitPane } from "@/components/doc-workspace/split-pane";
import type { EditorActions } from "@/components/doc-workspace/use-live-spelling";
import { useT, type LocaleKey } from "@/lib/i18n";
import { ApiError } from "@/lib/api-client";
import { setDocumentSessionTenant } from "@/lib/api/document-workspace";
import { listLegalRequestKnowledgeBases } from "@/lib/api/legal-requests";
import {
  approveLegalDraft,
  discardLegalDraft,
  getLegalDraft,
  legalDraftBusy,
  legalDraftStructure,
  locateParagraph,
  openLegalDraft,
  pickDraftKnowledgeBase,
  type LegalDraft,
  type LegalDraftOpenResult,
  type LegalDraftStructure,
} from "@/lib/api/legal-drafts";
import { IDLE_SPELLING, type LiveSpellingStatus, type SpellingActions } from "@/lib/live-spelling";
import { adminSectionHref } from "@/components/admin/admin-routes";
import { draftStatusTone } from "@/components/settings/pages/admin-legal-drafts";
import { LegalDraftStructurePanel } from "./structure-panel";
import { LegalDraftSpellingPanel } from "./spelling-panel";

const POLL_MS = 5_000;
const SPLIT_BOUNDS = { min: 25, max: 65, def: 42 };
const SPLIT_STORAGE_KEY = "werag.legalDraft.leftPct";

type Phase =
  | { kind: "loading" }
  | { kind: "waiting"; draft: LegalDraft | null }
  | { kind: "error"; message: string; draft: LegalDraft | null }
  | { kind: "ready" };

type PanelTab = "structure" | "spelling";

function errorText(e: unknown, fallback: string): string {
  return e instanceof Error && e.message ? e.message : fallback;
}

export const queueHref = (view: "requests" | "drafts" = "drafts") =>
  `${adminSectionHref("legal-documents")}&view=${view}`;

export function LegalDraftReview({ id }: { id: string }) {
  const { t, locale } = useT();
  const router = useRouter();
  const searchParams = useSearchParams();
  const preferredKb = searchParams.get("kb");
  const toast = useToast();
  const confirm = useConfirm();
  const [phase, setPhase] = useState<Phase>({ kind: "loading" });
  const [opened, setOpened] = useState<LegalDraftOpenResult | null>(null);
  const [draft, setDraft] = useState<LegalDraft | null>(null);
  const [structure, setStructure] = useState<LegalDraftStructure | null>(null);
  const [checking, setChecking] = useState(false);
  const [checkError, setCheckError] = useState("");
  const [panelOpen, setPanelOpen] = useState(true);
  const [panelTab, setPanelTab] = useState<PanelTab>("structure");
  const [approving, setApproving] = useState(false);
  const [discarding, setDiscarding] = useState(false);
  const [pdfKey, setPdfKey] = useState(0);
  const [toolbarSlot, setToolbarSlot] = useState<HTMLDivElement | null>(null);
  const [spelling, setSpelling] = useState<LiveSpellingStatus>(IDLE_SPELLING);
  const [spellingRunToken, setSpellingRunToken] = useState(0);
  const spellingActionsRef = useRef<SpellingActions | null>(null);
  const editorActionsRef = useRef<EditorActions | null>(null);
  const aliveRef = useRef(true);
  useEffect(() => {
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
    };
  }, []);

  /* Opens the draft in the editor; while it is still fetched/OCRed (409)
   * the draft is polled and opened once it is ready. */
  const open = useCallback(async (): Promise<LegalDraftOpenResult | null> => {
    try {
      const res = await openLegalDraft(id);
      const data = res.data;
      // synchronously, before the pane renders: every request on the hidden
      // session (any module, raw fetches too) goes to the platform organization
      if (data) setDocumentSessionTenant(data.session_id, data.tenant_id);
      if (!aliveRef.current || !data) return null;
      setOpened(data);
      setDraft(data.draft);
      if (data.draft.structure) setStructure(data.draft.structure);
      setPhase({ kind: "ready" });
      return data;
    } catch (e) {
      if (!aliveRef.current) return null;
      const cur = await getLegalDraft(id)
        .then((r) => r.data ?? null)
        .catch(() => null);
      if (!aliveRef.current) return null;
      if (cur) setDraft(cur);
      if (e instanceof ApiError && e.status === 409 && cur && legalDraftBusy(cur.status)) {
        setPhase({ kind: "waiting", draft: cur });
      } else {
        setPhase({
          kind: "error",
          draft: cur,
          message:
            cur?.status === "failed"
              ? cur.error || t("ldraft.review.failed")
              : cur?.status === "approved"
                ? t("ldraft.review.alreadyApproved")
                : errorText(e, t("ldraft.review.openFailed")),
        });
      }
      return null;
    }
  }, [id, t]);

  useEffect(() => {
    void open();
  }, [open]);

  // still OCRed: poll, then open
  const waiting = phase.kind === "waiting";
  useEffect(() => {
    if (!waiting) return;
    const timer = setInterval(async () => {
      if (document.visibilityState === "hidden") return;
      const cur = await getLegalDraft(id)
        .then((r) => r.data ?? null)
        .catch(() => null);
      if (!aliveRef.current || !cur) return;
      setDraft(cur);
      if (legalDraftBusy(cur.status)) setPhase({ kind: "waiting", draft: cur });
      else void open();
    }, POLL_MS);
    return () => clearInterval(timer);
  }, [waiting, id, open]);

  const checkStructure = async () => {
    setChecking(true);
    setCheckError("");
    setPanelOpen(true);
    setPanelTab("structure");
    try {
      const res = await legalDraftStructure(id);
      if (aliveRef.current && res.data) setStructure(res.data);
    } catch (e) {
      if (aliveRef.current) setCheckError(errorText(e, t("ldraft.structure.failed")));
    } finally {
      if (aliveRef.current) setChecking(false);
    }
  };

  const reloadPdf = async () => {
    // a fresh 2-hour link (the open call returns the same session)
    try {
      const res = await openLegalDraft(id);
      if (res.data) setDocumentSessionTenant(res.data.session_id, res.data.tenant_id);
      if (aliveRef.current && res.data) setOpened((cur) => (cur ? { ...cur, pdf_url: res.data.pdf_url } : res.data));
    } catch {
      /* keep the old link */
    }
    setPdfKey((k) => k + 1);
  };

  const discard = async () => {
    const ok = await confirm({
      title: t("ldraft.discardTitle"),
      message: t("ldraft.discardMessage", { number: draftLabel(draft) || id }),
      confirmLabel: t("ldraft.discard"),
      danger: true,
    });
    if (!ok) return;
    setDiscarding(true);
    try {
      await discardLegalDraft(id);
      toast.success(t("ldraft.discarded"));
      router.push(queueHref("drafts"));
    } catch (e) {
      toast.error(errorText(e, t("ldraft.discardFailed")));
      setDiscarding(false);
    }
  };

  const onSpellingStatus = useCallback(
    (status: LiveSpellingStatus, actions: SpellingActions, editor: EditorActions) => {
      setSpelling(status);
      spellingActionsRef.current = actions;
      editorActionsRef.current = editor;
    },
    [],
  );

  /* Selects a paragraph of the structure report in the editor (it scrolls
   * there); when it cannot be found, its lead text is copied instead. */
  const goToParagraph = useCallback(
    async (index: number, lead?: string, copyText?: string) => {
      const editor = editorActionsRef.current;
      const texts = editor ? await editor.readParagraphs() : null;
      const at = texts ? locateParagraph(texts, index, lead) : null;
      if (editor && at) {
        const res = await editor.runOps([{ op: "select", anchor: { text: at.text, occurrence: at.occurrence } }]);
        if (res && res.applied > 0) return;
      }
      const copy = copyText || lead;
      if (copy) {
        try {
          await navigator.clipboard.writeText(copy);
          toast.info(t("ldraft.structure.notFoundCopied", { text: copy }));
          return;
        } catch {
          /* fall through */
        }
      }
      toast.error(t("ldraft.structure.notFound"));
    },
    [toast, t],
  );

  const status = draft?.status ?? "";
  const ready = !!structure?.ready_to_approve;
  const editable = status === "ready" || status === "reviewing";
  const approveTip = !structure
    ? t("ldraft.approve.needCheck")
    : !ready
      ? t("ldraft.approve.notReady", { n: structure.blocking })
      : t("ldraft.approve.hint");

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* header */}
      <div className="hairline-b flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-2.5">
        <Link href={queueHref("drafts")} className="btn btn-ghost btn-sm shrink-0" title={t("ldraft.review.back")}>
          <IconChevronLeft className="h-3.5 w-3.5" />
          <span className="hidden sm:inline">{t("ldraft.review.back")}</span>
        </Link>
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <h1 className="truncate text-[15px] font-semibold text-ink">
              {t("ldraft.review.title")}
              {draftLabel(draft) ? ` · ${draftLabel(draft)}` : ""}
            </h1>
            {status && (
              <span className={`badge-pill shrink-0 text-[10px] ${draftStatusTone(status)}`}>
                {t(`ldraft.status.${status}` as LocaleKey)}
              </span>
            )}
          </div>
          {draft?.title && (
            <p className="caption truncate text-muted" title={draft.title}>
              {draft.title}
            </p>
          )}
        </div>
        <div ref={setToolbarSlot} className="flex items-center gap-1.5" />
        {phase.kind === "ready" && (
          <div className="flex flex-wrap items-center gap-1.5">
            <button
              type="button"
              className="btn btn-outline btn-sm"
              disabled={checking || !editable}
              onClick={() => void checkStructure()}
              title={t("ldraft.structure.checkHint")}
            >
              {checking && <IconRefresh className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none" />}
              {checking ? t("ldraft.structure.checking") : t("ldraft.structure.check")}
            </button>
            <span title={approveTip} className="inline-flex">
              <button
                type="button"
                className="btn btn-primary btn-sm"
                disabled={!ready || !editable || checking}
                onClick={() => setApproving(true)}
              >
                {t("ldraft.approve.button")}
              </button>
            </span>
            <button
              type="button"
              className="btn btn-ghost btn-sm text-error"
              disabled={discarding || status === "approved"}
              onClick={() => void discard()}
            >
              {t("ldraft.review.discard")}
            </button>
            <button
              type="button"
              className="btn btn-ghost btn-sm"
              onClick={() => setPanelOpen((v) => !v)}
              aria-pressed={panelOpen}
            >
              {panelOpen ? t("ldraft.panel.hide") : t("ldraft.panel.show")}
            </button>
          </div>
        )}
      </div>

      {phase.kind === "loading" ? (
        <Centered>
          <p className="body-sm text-muted">{t("ldraft.review.opening")}</p>
        </Centered>
      ) : phase.kind === "waiting" ? (
        <Centered>
          <IconRefresh className="mb-3 h-5 w-5 animate-spin text-muted motion-reduce:animate-none" />
          <p className="text-[15px] font-medium text-ink">
            {phase.draft?.ocr_pages
              ? t("lreq.draft.ocrPages", { n: phase.draft.ocr_pages })
              : t("lreq.draft.ocr")}
          </p>
          <p className="body-sm mt-1 max-w-[420px] text-center text-muted">{t("ldraft.review.waiting")}</p>
        </Centered>
      ) : phase.kind === "error" ? (
        <Centered>
          <p className="text-[15px] font-medium text-ink">{t("ldraft.review.openFailed")}</p>
          <p className="body-sm mt-1 max-w-[480px] text-center text-muted">{phase.message}</p>
          <div className="mt-4 flex gap-2">
            <button type="button" className="btn btn-outline btn-sm" onClick={() => void open()}>
              <IconRefresh className="h-3.5 w-3.5" /> {t("docws.retry")}
            </button>
            <Link href={queueHref("drafts")} className="btn btn-ghost btn-sm">
              {t("ldraft.review.back")}
            </Link>
          </div>
        </Centered>
      ) : opened ? (
        <div className="flex min-h-0 flex-1 overflow-hidden">
          <div className="flex min-h-0 min-w-0 flex-1 flex-col">
            <SplitPane
              bounds={SPLIT_BOUNDS}
              storageKey={SPLIT_STORAGE_KEY}
              left={
                <PdfPane
                  url={opened.pdf_url}
                  reloadKey={pdfKey}
                  pages={opened.draft.ocr_pages}
                  fileName={opened.draft.pdf_file_name}
                  onReload={() => void reloadPdf()}
                />
              }
              right={
                opened.editor ? (
                  <div className="relative min-h-0 flex-1">
                    <DocumentPane
                      sessionId={opened.session_id}
                      documentId={opened.document_id}
                      active
                      toolbarSlot={toolbarSlot}
                      opsBatches={NO_BATCHES}
                      onSelectionChange={noop}
                      onViewChange={noop}
                      onEditorDisabled={noop}
                      onSpellingStatus={onSpellingStatus}
                      spellingRunToken={spellingRunToken}
                    />
                  </div>
                ) : (
                  <Centered>
                    <p className="text-[15px] font-medium text-ink">{t("docws.disabledTitle")}</p>
                    <p className="body-sm mt-1 max-w-[420px] text-center text-muted">{t("docws.disabledDesc")}</p>
                  </Centered>
                )
              }
            />
          </div>
          {panelOpen && (
            <aside className="flex w-[340px] max-w-[45vw] shrink-0 flex-col border-l border-hairline bg-canvas">
              <div className="flex items-center gap-1 border-b border-hairline px-3">
                {(["structure", "spelling"] as const).map((k) => (
                  <button
                    key={k}
                    type="button"
                    onClick={() => setPanelTab(k)}
                    className={`-mb-px flex items-center gap-1.5 border-b-2 px-2.5 py-2 text-[13px] font-medium transition-colors ${
                      panelTab === k ? "border-ink text-ink" : "border-transparent text-muted hover:text-ink"
                    }`}
                  >
                    {t(k === "structure" ? "ldraft.panel.structure" : "ldraft.panel.spelling")}
                    {k === "structure" && structure && structure.blocking > 0 && (
                      <span className="badge-pill bg-red-500/10 text-[10px] tabular-nums text-red-700 dark:text-red-400">
                        {structure.blocking}
                      </span>
                    )}
                    {k === "spelling" && spelling.phase === "done" && spelling.found > 0 && (
                      <span className="badge-pill bg-amber-500/15 text-[10px] tabular-nums text-amber-700 dark:text-amber-400">
                        {spelling.found}
                      </span>
                    )}
                  </button>
                ))}
              </div>
              <div className="min-h-0 flex-1 overflow-y-auto p-3">
                {panelTab === "structure" ? (
                  <LegalDraftStructurePanel
                    structure={structure}
                    checking={checking}
                    error={checkError}
                    locale={locale}
                    onCheck={() => void checkStructure()}
                    onGoTo={goToParagraph}
                  />
                ) : (
                  <LegalDraftSpellingPanel
                    status={spelling}
                    onRun={() => setSpellingRunToken((n) => n + 1)}
                    onGoTo={(m) => spellingActionsRef.current?.goTo(m) ?? Promise.resolve("noAnswer")}
                    onDismiss={(m) => spellingActionsRef.current?.dismiss(m) ?? Promise.resolve(false)}
                  />
                )}
              </div>
            </aside>
          )}
        </div>
      ) : null}

      {approving && draft && (
        <ApproveDialog
          draft={draft}
          preferredKb={preferredKb}
          onClose={() => setApproving(false)}
          onApproved={(articles) => {
            setApproving(false);
            toast.success(t("ldraft.approve.done", { number: draftLabel(draft) || draft.title, n: articles }));
            router.push(queueHref("requests"));
          }}
          onRefused={(message) => {
            setApproving(false);
            setCheckError(message);
            void checkStructure();
          }}
        />
      )}
    </div>
  );
}

const NO_BATCHES: never[] = [];
const noop = () => {};

function draftLabel(d: { doc_type: string; document_number: string } | null): string {
  return d ? [d.doc_type, d.document_number].filter(Boolean).join(" ") : "";
}

function Centered({ children }: { children: React.ReactNode }) {
  return <div className="flex min-h-0 flex-1 flex-col items-center justify-center p-6">{children}</div>;
}

/* The original scan. The /r/ link carries its own grant (no auth header),
 * valid for 2 hours: "reload" asks for a fresh one. */
function PdfPane({
  url,
  reloadKey,
  pages,
  fileName,
  onReload,
}: {
  url: string;
  reloadKey: number;
  pages: number;
  fileName: string;
  onReload: () => void;
}) {
  const { t } = useT();
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="hairline-b flex items-center gap-2 px-3 py-1.5">
        <span className="caption min-w-0 flex-1 truncate text-muted" title={fileName || undefined}>
          {t("ldraft.pdf.title")}
          {pages > 0 ? ` · ${t("lreq.draft.pages", { n: pages })}` : ""}
        </span>
        <button
          type="button"
          className="btn btn-ghost btn-sm h-7 w-7 p-0"
          onClick={onReload}
          title={t("ldraft.pdf.reload")}
          aria-label={t("ldraft.pdf.reload")}
        >
          <IconRefresh className="h-3.5 w-3.5" />
        </button>
        {url && (
          <a
            href={url}
            target="_blank"
            rel="noopener noreferrer"
            className="btn btn-ghost btn-sm h-7 w-7 p-0"
            title={t("ldraft.pdf.newTab")}
            aria-label={t("ldraft.pdf.newTab")}
          >
            <IconExternal className="h-3.5 w-3.5" />
          </a>
        )}
      </div>
      {url ? (
        // the browser's PDF viewer: no thumbnail sidebar (it took half the
        // pane), no toolbar, page fitted to the width
        <iframe
          key={reloadKey}
          src={`${url}#navpanes=0&toolbar=0&view=FitH`}
          title={t("ldraft.pdf.title")}
          className="min-h-0 w-full flex-1 border-0 bg-surface-strong"
        />
      ) : (
        <Centered>
          <p className="body-sm text-muted">{t("ldraft.pdf.missing")}</p>
        </Centered>
      )}
    </div>
  );
}

function ApproveDialog({
  draft,
  preferredKb,
  onClose,
  onApproved,
  onRefused,
}: {
  draft: LegalDraft;
  preferredKb: string | null;
  onClose: () => void;
  onApproved: (articles: number) => void;
  /** The server found the draft not ready (400): the reasons, the
   * structure is checked again. */
  onRefused: (message: string) => void;
}) {
  const { t } = useT();
  const [kbs, setKbs] = useState<{ id: string; name: string }[] | null>(null);
  const [kbId, setKbId] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const res = await listLegalRequestKnowledgeBases();
        if (!alive) return;
        const list = res.data ?? [];
        setKbs(list);
        setKbId(pickDraftKnowledgeBase(list, draft.doc_type, preferredKb));
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
  }, [draft.doc_type, preferredKb, t]);

  const suggested = useMemo(() => (kbs ? pickDraftKnowledgeBase(kbs, draft.doc_type, preferredKb) : ""), [kbs, draft.doc_type, preferredKb]);

  const submit = async () => {
    if (!kbId || saving) return;
    setSaving(true);
    setError("");
    try {
      const res = await approveLegalDraft(draft.id, kbId);
      onApproved(res.data?.articles ?? 0);
    } catch (e) {
      const msg = errorText(e, t("ldraft.approve.failed"));
      if (e instanceof ApiError && e.status === 400 && /not ready/i.test(msg)) {
        onRefused(msg);
        return;
      }
      setError(msg);
      setSaving(false);
    }
  };

  return (
    <Modal open title={t("ldraft.approve.title")} onClose={onClose} width="w-[520px]">
      <div className="flex flex-col gap-4">
        <div className="rounded-[12px] border border-hairline bg-canvas-soft p-3">
          <div className="text-[13px] font-semibold text-ink">{draftLabel(draft) || "—"}</div>
          {draft.title && <div className="caption mt-0.5 text-body">{draft.title}</div>}
        </div>
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
                  {k.id === suggested ? ` (${t("lreq.suggested")})` : ""}
                </option>
              ))}
            </select>
          )}
          <span className="caption text-muted-soft">{t("ldraft.approve.desc")}</span>
        </label>
        {error && <p className="caption text-error">{error}</p>}
        <div className="flex justify-end gap-2 border-t border-hairline pt-4">
          <button className="btn btn-ghost btn-sm" onClick={onClose} disabled={saving}>
            {t("common.cancel")}
          </button>
          <button className="btn btn-primary btn-sm" onClick={() => void submit()} disabled={!kbId || saving}>
            {saving ? t("ldraft.approve.saving") : t("ldraft.approve.confirm")}
          </button>
        </div>
      </div>
    </Modal>
  );
}
