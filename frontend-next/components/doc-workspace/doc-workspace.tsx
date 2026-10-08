"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useT } from "@/lib/i18n";
import { useToast } from "@/components/toast";
import { useConfirm } from "@/components/confirm-dialog";
import { BUILTIN_DOCUMENT_ASSISTANT_ID } from "@/lib/chat-context";
import {
  listTemporaryAttachments,
  uploadTemporaryAttachment,
  type TemporaryAttachment,
} from "@/lib/api/attachments";
import { createSession, deleteSession } from "@/lib/api/chat";
import {
  DocumentWorkspaceError,
  MAX_DOCUMENTS_PER_SESSION,
  MAX_DOCUMENT_FILE_BYTES,
  MAX_SOURCES_PER_SESSION,
  activateDocumentWorkspace,
  closeDocumentWorkspace,
  createDocumentWorkspace,
  documentFileTooLarge,
  formatCheckIsCurrent,
  isWordAttachment,
  profileInProgress,
  sourceProfileLine,
  listDocumentWorkspaces,
  openDocumentInNewSession,
  setDocumentWorkspaceRole,
  splitDocumentsByRole,
  unopenedWordUploads,
  type DocumentFormatCheck,
  type DocumentProfile,
  type DocumentSelection,
  type DocumentWorkspaceView,
} from "@/lib/api/document-workspace";
import { readAppliedBatches, type OpsBatch, type OpsFailure } from "@/lib/api/document-ops";
import type { EditorOpsOutcome } from "./use-editor-ops";
import { IconBookmark, IconClose, IconDoc, IconPlus, IconRefresh } from "@/components/icons";
import { FileTypeIcon } from "@/components/files/file-type-icon";
import { formatFileSize } from "@/components/use-attachments";
import { DocumentPane } from "./document-pane";

type Phase =
  | { kind: "loading" }
  | { kind: "disabled" }
  | { kind: "error"; message: string }
  | { kind: "empty" }
  | { kind: "tabs" };

/** A document of the session as the chat sees it (mention picker, lock). */
export type SessionDocument = {
  id: string;
  file_name: string;
  handle?: string;
  /** "source": a chat upload, looked up only (no tab). */
  role?: "target" | "source";
  file_type?: string;
  /** The document's card, as far as the chat uses it (suggestion chips). */
  profile?: Pick<DocumentProfile, "status" | "gist" | "document_number" | "typical_questions">;
};

const WORD_ACCEPT = ".docx,.doc,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/msword";

function errMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

function storage(): Storage | null {
  try {
    return typeof window !== "undefined" ? window.localStorage : null;
  } catch {
    return null;
  }
}

/* Left pane of the document assistant: the session's working documents
 * (targets, up to 4), one ONLYOFFICE editor tab each (DocumentPane), and its
 * sources (chat uploads the assistant only looks up, up to 10), which never
 * get a tab. Lists the documents, lets the user open more (upload, or a Word
 * source "Mở để soạn thảo"), switch and close tabs, turn a tab into a source
 * ("Chỉ dùng làm nguồn"), and routes each edit plan of the assistant to the
 * tab of its document — switching to that tab so the user sees the edit.
 *
 * Pre-session mode (`sessionId` undefined, new-chat page): shows the drop
 * zone right away; picking a file creates the session, uploads it, opens the
 * document and hands the new id to `onSessionCreated` (which navigates). */
export function DocWorkspace({
  sessionId,
  onSessionCreated,
  onBusyChange,
  blocked = false,
  opsBatches,
  recheckToken = 0,
  turnInFlight = false,
  onSelectionChange,
  onFormatCheckChange,
  onDocumentChange,
  onDocumentsChange,
  refreshToken,
  onOpsOutcome,
}: {
  /** Undefined → pre-session mode (no chat session exists yet). */
  sessionId: string | undefined;
  /** Pre-session mode: the session that now holds the opened document. */
  onSessionCreated?: (sessionId: string) => void;
  /** Pre-session mode: true while a picked file is being opened (the host
   * must not create another session from its composer meanwhile). */
  onBusyChange?: (busy: boolean) => void;
  /** Host is busy (e.g. a first message is creating the session): the drop
   * zone and file picker are disabled. */
  blocked?: boolean;
  /** Edit plans from the chat's document tools (append-only per session),
   * applied in the editor of their document by the plugin, each batch once. */
  opsBatches?: (OpsBatch & { rejected?: OpsFailure[] })[];
  /** Bumped by the chat when an assistant turn ends (complete, error, abort,
   * resumed stream done) → re-check the documents for a new editor_key. */
  recheckToken?: number;
  /** A chat turn is streaming → poll faster. */
  turnInFlight?: boolean;
  onSelectionChange: (sel: DocumentSelection | null) => void;
  /** Background format check and profile of the visible document (null: none). */
  onFormatCheckChange?: (
    check: DocumentFormatCheck | null,
    doc: SessionDocument | null,
    profile: DocumentProfile | null,
  ) => void;
  /** File name of a document the session holds (null: none) — locks the mode. */
  onDocumentChange?: (fileName: string | null) => void;
  /** The session's documents of both roles in handle order and the visible tab. */
  onDocumentsChange?: (docs: SessionDocument[], activeId: string | null) => void;
  /** Changes when the chat recorded or promoted a source → reload the list. */
  refreshToken?: string;
  /** Outcome of each edit batch in its editor (the chat's proposal cards). */
  onOpsOutcome?: (o: EditorOpsOutcome) => void;
}) {
  const { t } = useT();
  const toast = useToast();
  const confirm = useConfirm();
  const [phase, setPhase] = useState<Phase>(() => (sessionId ? { kind: "loading" } : { kind: "empty" }));
  // docs: the targets (tabs); sources: the lookup-only uploads; allDocs:
  // both in handle order (for the chat's @ picker).
  const [docs, setDocs] = useState<DocumentWorkspaceView[]>([]);
  const [sources, setSources] = useState<DocumentWorkspaceView[]>([]);
  const [allDocs, setAllDocs] = useState<DocumentWorkspaceView[]>([]);
  const [maxSources, setMaxSources] = useState(MAX_SOURCES_PER_SESSION);
  // Session uploads (all of them); the Word ones without a document — e.g.
  // uploaded before roles existed — stay openable.
  const [attachments, setAttachments] = useState<TemporaryAttachment[]>([]);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [maxDocs, setMaxDocs] = useState(MAX_DOCUMENTS_PER_SESSION);
  const [maxFileBytes, setMaxFileBytes] = useState(MAX_DOCUMENT_FILE_BYTES);
  // Tabs opened at least once: their editors stay mounted (hidden) so each
  // keeps its undo history.
  const [visited, setVisited] = useState<Set<string>>(() => new Set());
  // Latest view each pane reported (status, revision, format check).
  const [views, setViews] = useState<Record<string, DocumentWorkspaceView>>({});
  const [busyMsg, setBusyMsg] = useState<string | null>(null);
  const [dragOver, setDragOver] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  // Pre-session mode: last failure and the file kept for a retry.
  const [openError, setOpenError] = useState<string | null>(null);
  const [retryFile, setRetryFile] = useState<File | null>(null);
  const [toolbarSlot, setToolbarSlot] = useState<HTMLElement | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const aliveRef = useRef(true);
  useEffect(() => {
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
    };
  }, []);

  /* Reload the list; `focus` makes that document the visible tab. */
  const loadDocs = useCallback(
    async (focus?: string) => {
      if (!sessionId) {
        setPhase({ kind: "empty" });
        return;
      }
      try {
        const list = await listDocumentWorkspaces(sessionId);
        if (!aliveRef.current) return;
        void listTemporaryAttachments(sessionId)
          .then((a) => aliveRef.current && setAttachments(a))
          .catch(() => aliveRef.current && setAttachments([]));
        const { targets, sources: srcs } = splitDocumentsByRole(list.documents);
        setDocs(targets);
        setSources(srcs);
        setAllDocs(list.documents);
        setMaxDocs(list.max_documents);
        setMaxSources(list.max_sources);
        setMaxFileBytes(list.max_file_bytes);
        if (targets.length === 0) {
          setActiveId(null);
          setPhase({ kind: "empty" });
          return;
        }
        const ids = new Set(targets.map((d) => d.id));
        setActiveId((cur) => {
          if (focus && ids.has(focus)) return focus;
          if (cur && ids.has(cur)) return cur;
          return ids.has(list.active_id) ? list.active_id : targets[targets.length - 1].id;
        });
        setPhase({ kind: "tabs" });
      } catch (e) {
        if (!aliveRef.current) return;
        if (e instanceof DocumentWorkspaceError && e.code === "editor_disabled") setPhase({ kind: "disabled" });
        else setPhase({ kind: "error", message: errMessage(e) });
      }
    },
    [sessionId],
  );

  // Fresh state per session.
  useEffect(() => {
    setDocs([]);
    setSources([]);
    setAllDocs([]);
    setAttachments([]);
    setActiveId(null);
    setVisited(new Set());
    setViews({});
    setBusyMsg(null);
    setPhase(sessionId ? { kind: "loading" } : { kind: "empty" });
    void loadDocs();
  }, [sessionId, loadDocs]);

  useEffect(() => {
    if (!activeId) return;
    setVisited((prev) => (prev.has(activeId) ? prev : new Set(prev).add(activeId)));
  }, [activeId]);

  // the chat recorded or promoted a source
  const firstRefreshRef = useRef(true);
  useEffect(() => {
    if (firstRefreshRef.current) {
      firstRefreshRef.current = false;
      return;
    }
    void loadDocs();
  }, [refreshToken, loadDocs]);

  // a source is being read or profiled: poll until its text and card are in
  const reading = sources.some((d) => d.text_status === "processing" || profileInProgress(d.profile));
  useEffect(() => {
    if (!reading) return;
    const timer = setTimeout(() => void loadDocs(), 3000);
    return () => clearTimeout(timer);
  }, [reading, sources, loadDocs]);

  const switchTo = useCallback(
    (docId: string) => {
      setActiveId(docId);
      setAddOpen(false);
      if (sessionId) void activateDocumentWorkspace(sessionId, docId).catch(() => {});
    },
    [sessionId],
  );

  /* ---------- report to the chat ---------- */
  const onDocumentRef = useRef(onDocumentChange);
  onDocumentRef.current = onDocumentChange;
  const lockName = docs.find((d) => (views[d.id] ?? d).status === "open")?.file_name ?? null;
  useEffect(() => {
    if (lockName) onDocumentRef.current?.(lockName);
  }, [lockName]);

  const onDocumentsRef = useRef(onDocumentsChange);
  onDocumentsRef.current = onDocumentsChange;
  const docsSig =
    allDocs
      .map((d) => `${d.id}:${d.file_name}:${d.role ?? ""}:${d.profile?.status ?? ""}:${d.profile?.typical_questions?.length ?? 0}`)
      .join("|") + `#${activeId ?? ""}`;
  useEffect(() => {
    onDocumentsRef.current?.(
      allDocs.map((d) => ({
        id: d.id,
        file_name: d.file_name,
        handle: d.handle,
        role: d.role === "source" ? "source" : "target",
        file_type: d.file_type,
        profile: d.profile
          ? {
              status: d.profile.status,
              gist: d.profile.gist,
              document_number: d.profile.document_number,
              typical_questions: d.profile.typical_questions,
            }
          : undefined,
      })),
      activeId,
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [docsSig]);

  // Hand the visible document's format check to the chat (progress ring);
  // a finished check of a document edited since no longer describes it.
  const onFormatCheckRef = useRef(onFormatCheckChange);
  onFormatCheckRef.current = onFormatCheckChange;
  const activeView = activeId ? (views[activeId] ?? docs.find((d) => d.id === activeId) ?? null) : null;
  const rawCheck = activeView?.format_check ?? null;
  const formatCheck = rawCheck && activeView && formatCheckIsCurrent(rawCheck, activeView) ? rawCheck : null;
  const activeProfile = activeView?.profile ?? null;
  const formatCheckSig =
    (formatCheck
      ? `${activeView?.id}|${formatCheck.status}|${formatCheck.revision}|${formatCheck.finished_at ?? ""}`
      : `${activeView?.id ?? ""}|none`) + `|${activeProfile?.status ?? ""}|${activeProfile?.started_at ?? ""}`;
  useEffect(() => {
    onFormatCheckRef.current?.(
      formatCheck,
      activeView ? { id: activeView.id, file_name: activeView.file_name, handle: activeView.handle } : null,
      activeProfile,
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [formatCheckSig]);

  const onPaneView = useCallback((v: DocumentWorkspaceView) => {
    setViews((prev) => ({ ...prev, [v.id]: v }));
  }, []);

  /* ---------- edit plans → the tab of their document ----------
   * A batch without a document (older results) goes to the tab visible when
   * it arrived; the routing is fixed on first sight. A new batch for another
   * tab switches to it, so the user sees the edit land. */
  const routeRef = useRef<Map<string, string>>(new Map());
  const appliedRef = useRef<Set<string> | null>(null);
  useEffect(() => {
    routeRef.current = new Map();
    appliedRef.current = null;
  }, [sessionId]);
  const batchesByDoc = useMemo(() => {
    const out: Record<string, (OpsBatch & { rejected?: OpsFailure[] })[]> = {};
    if (!opsBatches || docs.length === 0) return out;
    const ids = new Set(docs.map((d) => d.id));
    for (const b of opsBatches) {
      let target = routeRef.current.get(b.batchId);
      if (!target) {
        target = b.documentId && ids.has(b.documentId) ? b.documentId : (activeId ?? undefined);
        if (!target) continue;
        routeRef.current.set(b.batchId, target);
      }
      (out[target] ??= []).push(b);
    }
    return out;
  }, [opsBatches, docs, activeId]);
  const switchedForRef = useRef<Set<string>>(new Set());
  useEffect(() => {
    if (!sessionId || !opsBatches?.length) return;
    if (!appliedRef.current) appliedRef.current = new Set(readAppliedBatches(storage(), sessionId));
    for (const b of opsBatches) {
      if (switchedForRef.current.has(b.batchId) || appliedRef.current.has(b.batchId)) continue;
      switchedForRef.current.add(b.batchId);
      const target = routeRef.current.get(b.batchId);
      if (target && target !== activeId) switchTo(target);
    }
  }, [opsBatches, sessionId, activeId, switchTo]);

  /* ---------- open documents ---------- */
  const openAttachment = async (attachmentId: string) => {
    if (!sessionId) return;
    setBusyMsg(t("docws.opening"));
    try {
      const v = await createDocumentWorkspace(sessionId, attachmentId);
      await loadDocs(v.id);
    } catch (e) {
      if (e instanceof DocumentWorkspaceError && e.code === "editor_disabled") setPhase({ kind: "disabled" });
      else toast.error(`${t("docws.openFailed")}: ${errMessage(e)}`);
    } finally {
      setBusyMsg(null);
    }
  };

  /* Pre-session: session → upload → document, then hand off (navigation).
   * On failure the fresh session is deleted and the file kept for retry. */
  const openingRef = useRef(false);
  const onBusyChangeRef = useRef(onBusyChange);
  onBusyChangeRef.current = onBusyChange;
  const openInNewSession = async (file: File) => {
    if (openingRef.current || blocked) return;
    openingRef.current = true;
    onBusyChangeRef.current?.(true);
    setOpenError(null);
    setRetryFile(file);
    setBusyMsg(t("docws.opening"));
    try {
      const sid = await openDocumentInNewSession(file, {
        createSession: async () => {
          const res = await createSession({});
          return res?.data?.id ?? "";
        },
        upload: async (sid, f) => {
          const res = await uploadTemporaryAttachment(
            sid,
            f,
            BUILTIN_DOCUMENT_ASSISTANT_ID,
            "auto",
            (p) => setBusyMsg(t("docws.uploading", { percent: Math.round(p) })),
            "target",
          );
          setBusyMsg(t("docws.opening"));
          return res?.data?.id ?? "";
        },
        createWorkspace: (sid, attachmentId) => createDocumentWorkspace(sid, attachmentId),
        deleteSession: (sid) => deleteSession(sid),
      });
      if (!aliveRef.current) {
        // Pane went away (agent switched) before navigating: nobody will
        // open this session — remove it like a failed attempt.
        openingRef.current = false;
        onBusyChangeRef.current?.(false);
        void deleteSession(sid).catch(() => {});
        return;
      }
      setRetryFile(null);
      // Keep the progress message (and the host blocked) until the route changes.
      onSessionCreated?.(sid);
    } catch (e) {
      openingRef.current = false;
      onBusyChangeRef.current?.(false);
      if (!aliveRef.current) return;
      setBusyMsg(null);
      setOpenError(
        e instanceof DocumentWorkspaceError && e.code === "editor_disabled" ? t("docws.disabledTitle") : errMessage(e),
      );
    }
  };

  const atLimit = docs.length >= maxDocs;

  const uploadAndOpen = async (file: File) => {
    setAddOpen(false);
    if (!isWordAttachment(file.name)) {
      toast.error(t("docws.onlyWord"));
      return;
    }
    if (documentFileTooLarge(file, maxFileBytes)) {
      toast.error(t("docws.tooLarge", { name: file.name, max: Math.round(maxFileBytes / (1024 * 1024)) }));
      return;
    }
    if (!sessionId) {
      await openInNewSession(file);
      return;
    }
    if (atLimit) {
      toast.error(t("docws.limitReached", { n: maxDocs }));
      return;
    }
    setBusyMsg(t("docws.uploading", { percent: 0 }));
    try {
      const res = await uploadTemporaryAttachment(
        sessionId,
        file,
        BUILTIN_DOCUMENT_ASSISTANT_ID,
        "auto",
        (p) => setBusyMsg(t("docws.uploading", { percent: Math.round(p) })),
        "target",
      );
      if (!res?.data?.id) throw new Error("upload returned no attachment id");
      await openAttachment(res.data.id);
    } catch (e) {
      toast.error(`${t("docws.openFailed")}: ${errMessage(e)}`);
      setBusyMsg(null);
    }
  };

  const closeTab = async (doc: DocumentWorkspaceView) => {
    if (!sessionId) return;
    const ok = await confirm({
      title: t("docws.closeTabTitle"),
      message: t("docws.closeTabConfirm", { name: doc.file_name }),
      confirmLabel: t("docws.closeTab"),
      danger: true,
    });
    if (!ok) return;
    try {
      await closeDocumentWorkspace(sessionId, doc.id);
      setVisited((prev) => {
        const next = new Set(prev);
        next.delete(doc.id);
        return next;
      });
      if (activeId === doc.id) {
        const rest = docs.filter((d) => d.id !== doc.id);
        const idx = docs.findIndex((d) => d.id === doc.id);
        const next = rest[Math.min(idx, rest.length - 1)];
        if (next) switchTo(next.id);
      }
      await loadDocs();
    } catch (e) {
      toast.error(`${t("docws.closeTabFailed")}: ${errMessage(e)}`);
    }
  };

  /* ---------- roles ---------- */
  const roleError = (e: unknown) => {
    if (e instanceof DocumentWorkspaceError && e.code === "editor_disabled") setPhase({ kind: "disabled" });
    else toast.error(`${t("docws.roleChangeFailed")}: ${errMessage(e)}`);
  };

  /* "Mở để soạn thảo": a Word source becomes a tab, keeping its handle. */
  const openSource = async (src: DocumentWorkspaceView) => {
    if (!sessionId) return;
    setAddOpen(false);
    if (atLimit) {
      toast.error(t("docws.limitReached", { n: maxDocs }));
      return;
    }
    setBusyMsg(t("docws.opening"));
    try {
      const v = await setDocumentWorkspaceRole(sessionId, src.id, "target");
      await loadDocs(v.id);
    } catch (e) {
      roleError(e);
    } finally {
      setBusyMsg(null);
    }
  };

  /* "Chỉ dùng làm nguồn": the tab leaves the editor, its text stays for lookups. */
  const demoteTab = async (doc: DocumentWorkspaceView) => {
    if (!sessionId) return;
    const ok = await confirm({
      title: t("docws.useAsSourceTitle"),
      message: t("docws.useAsSourceConfirm", { name: doc.file_name }),
      confirmLabel: t("docws.useAsSource"),
    });
    if (!ok) return;
    try {
      await setDocumentWorkspaceRole(sessionId, doc.id, "source");
      setVisited((prev) => {
        const next = new Set(prev);
        next.delete(doc.id);
        return next;
      });
      if (activeId === doc.id) {
        const rest = docs.filter((d) => d.id !== doc.id);
        const idx = docs.findIndex((d) => d.id === doc.id);
        const next = rest[Math.min(idx, rest.length - 1)];
        if (next) switchTo(next.id);
      }
      await loadDocs();
    } catch (e) {
      roleError(e);
    }
  };

  const removeSource = async (src: DocumentWorkspaceView) => {
    if (!sessionId) return;
    try {
      await closeDocumentWorkspace(sessionId, src.id);
      await loadDocs();
    } catch (e) {
      toast.error(`${t("docws.closeTabFailed")}: ${errMessage(e)}`);
    }
  };

  const uploads = unopenedWordUploads(attachments, allDocs);
  const sourcesList = (
    <SourcesList
      sources={sources}
      uploads={uploads}
      onOpenUpload={(a) => {
        setAddOpen(false);
        void openAttachment(a.id);
      }}
      max={maxSources}
      busy={Boolean(busyMsg)}
      canOpen={!atLimit}
      onOpen={(d) => void openSource(d)}
      onRemove={(d) => void removeSource(d)}
    />
  );

  const fileInput = (
    <input
      ref={fileInputRef}
      type="file"
      accept={WORD_ACCEPT}
      className="hidden"
      onChange={(e) => {
        const file = e.target.files?.[0];
        e.target.value = "";
        if (file && !busyMsg && !blocked) void uploadAndOpen(file);
      }}
    />
  );

  /* ---------- render ---------- */
  if (phase.kind === "loading") {
    return (
      <Centered>
        <p className="body-sm text-muted">{t("docws.loading")}</p>
      </Centered>
    );
  }
  if (phase.kind === "disabled") {
    return (
      <Centered>
        <IconDoc className="mb-3 h-8 w-8 text-muted-soft" />
        <p className="text-[15px] font-medium text-ink">{t("docws.disabledTitle")}</p>
        <p className="body-sm mt-1 max-w-[420px] text-center text-muted">{t("docws.disabledDesc")}</p>
      </Centered>
    );
  }
  if (phase.kind === "error") {
    return (
      <Centered>
        <p className="text-[15px] font-medium text-ink">{t("docws.loadFailed")}</p>
        <p className="body-sm mt-1 max-w-[420px] text-center text-muted">{phase.message}</p>
        <button type="button" className="btn btn-outline btn-sm mt-4" onClick={() => void loadDocs()}>
          <IconRefresh className="h-3.5 w-3.5" /> {t("docws.retry")}
        </button>
      </Centered>
    );
  }
  if (phase.kind === "empty") {
    return (
      <div
        className="flex h-full flex-col items-center justify-center overflow-y-auto p-6"
        onDragOver={(e) => {
          e.preventDefault();
          setDragOver(true);
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragOver(false);
          const file = e.dataTransfer.files?.[0];
          if (file && !busyMsg && !blocked) void uploadAndOpen(file);
        }}
      >
        {fileInput}
        <div
          className={`flex w-full max-w-[520px] flex-col items-center rounded-xl border-2 border-dashed px-6 py-10 text-center transition-colors ${
            dragOver ? "border-ink/40 bg-surface-strong" : "border-hairline"
          }`}
        >
          <IconDoc className="mb-3 h-9 w-9 text-muted-soft" />
          <p className="text-[15px] font-medium text-ink">{dragOver ? t("docws.dropHere") : t("docws.emptyTitle")}</p>
          <p className="body-sm mt-1 text-muted">{t("docws.emptyDesc")}</p>
          <p className="caption mt-2 text-muted-soft">
            {t("docws.limitsHint", { n: maxDocs, max: Math.round(maxFileBytes / (1024 * 1024)) })}
          </p>
          {busyMsg ? (
            <p className="body-sm mt-4 text-ink" role="status">
              {busyMsg}
            </p>
          ) : (
            <>
              {openError && (
                <p className="body-sm mt-4 max-w-[420px] text-error" role="alert">
                  {t("docws.openFailed")}: {openError}
                </p>
              )}
              <div className="mt-4 flex flex-wrap justify-center gap-2">
                {openError && retryFile && (
                  <button
                    type="button"
                    className="btn btn-outline btn-sm"
                    disabled={blocked}
                    onClick={() => void uploadAndOpen(retryFile)}
                  >
                    <IconRefresh className="h-3.5 w-3.5" /> {t("docws.retryFile", { name: retryFile.name })}
                  </button>
                )}
                <button
                  type="button"
                  className="btn btn-primary btn-sm"
                  disabled={blocked}
                  onClick={() => fileInputRef.current?.click()}
                >
                  {t("docws.pickFile")}
                </button>
              </div>
            </>
          )}
        </div>
        {sources.length + uploads.length > 0 && <div className="mt-6 w-full max-w-[520px]">{sourcesList}</div>}
      </div>
    );
  }

  // tabs
  return (
    <div
      className="flex h-full flex-col overflow-hidden"
      onDragOver={(e) => {
        if (Array.from(e.dataTransfer.types).includes("Files")) e.preventDefault();
      }}
      onDrop={(e) => {
        const file = e.dataTransfer.files?.[0];
        if (!file) return;
        e.preventDefault();
        if (!busyMsg) void uploadAndOpen(file);
      }}
    >
      {fileInput}
      <div className="hairline-b flex h-14 shrink-0 items-center gap-2 px-2 sm:px-3">
        <div role="tablist" className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto">
          {docs.map((d) => {
            const selected = d.id === activeId;
            return (
              <div
                key={d.id}
                role="tab"
                aria-selected={selected}
                tabIndex={0}
                title={d.file_name}
                onClick={() => !selected && switchTo(d.id)}
                onKeyDown={(e) => {
                  if ((e.key === "Enter" || e.key === " ") && !selected) {
                    e.preventDefault();
                    switchTo(d.id);
                  }
                }}
                className={`group relative flex h-9 min-w-[96px] max-w-[200px] shrink cursor-pointer items-center gap-1.5 rounded-lg border px-2.5 text-[13px] transition-colors ${
                  selected
                    ? // the open document: blue like the chat's own turns, with an underline accent
                      "border-[#cfe1fd] bg-[#edf5ff] font-semibold text-[#0f2d59] shadow-2xs after:absolute after:inset-x-2.5 after:-bottom-px after:h-[2px] after:rounded-full after:bg-[#1d5bd8] dark:border-[#223d63] dark:bg-[#15273f] dark:text-[#dce9fe] dark:after:bg-[#8ab4ff]"
                    : "border-transparent text-muted hover:bg-surface-strong hover:text-ink"
                }`}
              >
                <FileTypeIcon name={d.file_name} fileType={d.file_type} className="h-4 w-3.5" />
                <span className="min-w-0 flex-1 truncate">{d.file_name}</span>
                <button
                  type="button"
                  className={`shrink-0 rounded p-0.5 text-muted hover:bg-hairline hover:text-ink ${
                    selected ? "" : "opacity-0 group-hover:opacity-100 focus:opacity-100"
                  }`}
                  aria-label={t("docws.useAsSourceOf", { name: d.file_name })}
                  title={t("docws.useAsSource")}
                  onClick={(e) => {
                    e.stopPropagation();
                    void demoteTab(d);
                  }}
                >
                  <IconBookmark className="h-3 w-3" />
                </button>
                <button
                  type="button"
                  className={`shrink-0 rounded p-0.5 text-muted hover:bg-hairline hover:text-ink ${
                    selected ? "" : "opacity-0 group-hover:opacity-100 focus:opacity-100"
                  }`}
                  aria-label={t("docws.closeTabOf", { name: d.file_name })}
                  title={t("docws.closeTab")}
                  onClick={(e) => {
                    e.stopPropagation();
                    void closeTab(d);
                  }}
                >
                  <IconClose className="h-3 w-3" />
                </button>
              </div>
            );
          })}
        </div>
          <div className="relative shrink-0">
            <button
              type="button"
              className="btn btn-ghost btn-sm h-9 w-9 p-0"
              disabled={Boolean(busyMsg) || (atLimit && sources.length + uploads.length === 0)}
              title={atLimit ? t("docws.limitReached", { n: maxDocs }) : t("docws.addDocument")}
              aria-label={t("docws.addDocument")}
              onClick={() => (sources.length + uploads.length > 0 ? setAddOpen((o) => !o) : fileInputRef.current?.click())}
            >
              <IconPlus className="h-4 w-4" />
            </button>
            {addOpen && (
              <>
                <div className="fixed inset-0 z-40" onClick={() => setAddOpen(false)} />
                <div className="card absolute left-0 top-full z-[60] mt-1 max-h-[70vh] w-[320px] overflow-y-auto p-1.5 shadow-[0_4px_16px_rgba(0,0,0,0.08)]">
                  <button
                    type="button"
                    disabled={atLimit}
                    title={atLimit ? t("docws.limitReached", { n: maxDocs }) : undefined}
                    className="flex w-full items-center gap-2 rounded-[8px] px-3 py-2 text-left text-[13px] text-ink hover:bg-surface-strong disabled:opacity-50 disabled:hover:bg-transparent"
                    onClick={() => {
                      setAddOpen(false);
                      fileInputRef.current?.click();
                    }}
                  >
                    <IconPlus className="h-3.5 w-3.5" /> {t("docws.uploadNew")}
                  </button>
                  <div className="px-1.5 pb-1 pt-2">{sourcesList}</div>
                </div>
              </>
            )}
          </div>
          {busyMsg && (
            <span className="caption shrink-0 truncate text-muted" role="status">
              {busyMsg}
            </span>
          )}
        {/* the visible tab's actions (history, download, save) */}
        <div ref={setToolbarSlot} className="flex shrink-0 items-center gap-1.5" />
      </div>
      <div className="relative min-h-0 flex-1">
        {sessionId &&
          docs
            .filter((d) => visited.has(d.id) || d.id === activeId)
            .map((d) => (
              <DocumentPane
                key={d.id}
                sessionId={sessionId}
                documentId={d.id}
                active={d.id === activeId}
                toolbarSlot={toolbarSlot}
                opsBatches={batchesByDoc[d.id] ?? NO_BATCHES}
                recheckToken={recheckToken}
                turnInFlight={turnInFlight}
                onSelectionChange={onSelectionChange}
                onViewChange={onPaneView}
                onEditorDisabled={() => setPhase({ kind: "disabled" })}
                onOpsOutcome={onOpsOutcome}
              />
            ))}
      </div>
    </div>
  );
}

const NO_BATCHES: (OpsBatch & { rejected?: OpsFailure[] })[] = [];

/* "Tài liệu nguồn": the session's sources with type, size and reading
 * state; a Word source can be opened for editing. */
function SourcesList({
  sources,
  uploads,
  max,
  busy,
  canOpen,
  onOpen,
  onOpenUpload,
  onRemove,
}: {
  sources: DocumentWorkspaceView[];
  /** Word uploads without a document (e.g. from before roles existed). */
  uploads: TemporaryAttachment[];
  max: number;
  busy: boolean;
  canOpen: boolean;
  onOpen: (d: DocumentWorkspaceView) => void;
  onOpenUpload: (a: TemporaryAttachment) => void;
  onRemove: (d: DocumentWorkspaceView) => void;
}) {
  const { t } = useT();
  if (sources.length === 0 && uploads.length === 0) return null;
  return (
    <div>
      {sources.length > 0 && (
        <>
          <div className="mb-1 flex items-baseline justify-between gap-2">
            <p className="caption-uppercase text-muted-soft">{t("docws.sourcesTitle")}</p>
            <span className="caption text-muted-soft">{t("docws.sourcesCount", { n: sources.length, max })}</span>
          </div>
          <p className="caption mb-2 text-muted">{t("docws.sourcesHint")}</p>
          <ul className="flex flex-col gap-1.5">
            {sources.map((d) => (
              <li key={d.id} className="flex items-center gap-2 rounded-lg border border-hairline px-3 py-2">
                <FileTypeIcon name={d.file_name} fileType={d.file_type} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] text-ink" title={d.file_name}>
                    {d.handle ? `${d.handle} · ` : ""}
                    {d.file_name}
                  </span>
                  <span className="caption block truncate text-muted">
                    {(d.file_type || "").toUpperCase()} · {formatFileSize(d.file_size)}
                    {d.text_status === "processing"
                      ? ` · ${t("docws.sourceReading")}`
                      : d.text_status === "failed"
                        ? ` · ${t("docws.sourceFailed")}`
                        : profileInProgress(d.profile)
                          ? ` · ${t("docws.sourceProfiling")}`
                          : ""}
                  </span>
                  {sourceProfileLine(d.profile) && (
                    <span className="caption mt-0.5 line-clamp-2 text-body" title={sourceProfileLine(d.profile)}>
                      {sourceProfileLine(d.profile)}
                    </span>
                  )}
                </span>
                {isWordAttachment(d.file_name, d.file_type) && (
                  <button
                    type="button"
                    className="btn btn-outline btn-sm shrink-0"
                    disabled={busy || !canOpen}
                    onClick={() => onOpen(d)}
                  >
                    {t("docws.openForEditing")}
                  </button>
                )}
                <button
                  type="button"
                  className="shrink-0 rounded p-1 text-muted hover:bg-hairline hover:text-ink"
                  aria-label={t("docws.removeSourceOf", { name: d.file_name })}
                  title={t("docws.removeSource")}
                  disabled={busy}
                  onClick={() => onRemove(d)}
                >
                  <IconClose className="h-3 w-3" />
                </button>
              </li>
            ))}
          </ul>
        </>
      )}
      {uploads.length > 0 && (
        <div className={sources.length > 0 ? "mt-3" : ""}>
          <p className="caption-uppercase mb-2 text-muted-soft">{t("docws.uploadsTitle")}</p>
          <ul className="flex flex-col gap-1.5">
            {uploads.map((a) => (
              <li key={a.id} className="flex items-center gap-2 rounded-lg border border-hairline px-3 py-2">
                <FileTypeIcon name={a.file_name} fileType={a.file_type} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] text-ink" title={a.file_name}>
                    {a.file_name}
                  </span>
                  <span className="caption block truncate text-muted">{formatFileSize(a.file_size)}</span>
                </span>
                <button
                  type="button"
                  className="btn btn-outline btn-sm shrink-0"
                  disabled={busy || !canOpen}
                  onClick={() => onOpenUpload(a)}
                >
                  {t("docws.openForEditing")}
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function Centered({ children }: { children: React.ReactNode }) {
  return <div className="flex h-full flex-col items-center justify-center p-6">{children}</div>;
}
