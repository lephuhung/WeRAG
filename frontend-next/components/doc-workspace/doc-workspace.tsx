"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useT } from "@/lib/i18n";
import { useToast } from "@/components/toast";
import { BUILTIN_DOCUMENT_ASSISTANT_ID } from "@/lib/chat-context";
import {
  listTemporaryAttachments,
  uploadTemporaryAttachment,
  type TemporaryAttachment,
} from "@/lib/api/attachments";
import {
  DocumentWorkspaceError,
  createDocumentWorkspace,
  documentServerOrigin,
  downloadDocumentWorkspace,
  forceSaveDocumentWorkspace,
  forceSaveDocumentWorkspaceKeepalive,
  getDocumentWorkspace,
  isWordAttachment,
  parsePluginSelectionMessage,
  shouldRefreshEditor,
  type DocumentSelection,
  type DocumentWorkspaceView,
} from "@/lib/api/document-workspace";
import { IconDoc, IconDownload, IconRefresh } from "@/components/icons";
import { OnlyOfficeEditor, type OnlyOfficeEditorHandle } from "./onlyoffice-editor";

type Phase =
  | { kind: "loading" }
  | { kind: "disabled" }
  | { kind: "error"; message: string }
  | { kind: "empty" }
  | { kind: "editor" };

const SELECTION_DEBOUNCE_MS = 250;
const UPDATED_TOAST_THROTTLE_MS = 3000;
// Fallback polling of the workspace (SSE tool results can be lost when the
// stream is cut while the backend still finishes the edit).
const POLL_BUSY_MS = 20_000;
const POLL_IDLE_MS = 30_000;
// Follow-up re-checks after a chat turn ends: the backend may still be
// committing the edit when a dropped stream surfaces client-side.
const SETTLE_RECHECK_MS = [4_000, 12_000];
// Minimum gap between two refreshFile() calls unless onDocumentReady fires.
const REFRESH_COOLDOWN_MS = 1_500;
const WORD_ACCEPT = ".docx,.doc,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/msword";

function errMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/* Left pane of the document assistant: one ONLYOFFICE-backed .docx per chat
 * session. Loads (or lets the user create) the workspace, keeps the editor
 * in sync with assistant-made revisions, and forwards editor selections. */
export function DocWorkspace({
  sessionId,
  revision,
  recheckToken = 0,
  turnInFlight = false,
  onSelectionChange,
}: {
  sessionId: string;
  /** Latest document revision reported by an editing tool in the chat. */
  revision: number | null;
  /** Bumped by the chat when an assistant turn ends (complete, error, abort,
   * resumed stream done) → re-check the workspace for a new editor_key. */
  recheckToken?: number;
  /** A chat turn is streaming → poll faster. */
  turnInFlight?: boolean;
  onSelectionChange: (sel: DocumentSelection | null) => void;
}) {
  const { t } = useT();
  const toast = useToast();
  const [phase, setPhase] = useState<Phase>({ kind: "loading" });
  // `view` tracks the latest server state (badge, file name); `mounted` is the
  // view whose config the editor was created with — only replaced when the
  // document identity changes, revisions go through refreshFile().
  const [view, setView] = useState<DocumentWorkspaceView | null>(null);
  const [mounted, setMounted] = useState<DocumentWorkspaceView | null>(null);
  const [dirty, setDirty] = useState(false);
  const [editorFailed, setEditorFailed] = useState<string | null>(null);
  const [attachments, setAttachments] = useState<TemporaryAttachment[]>([]);
  const [busyMsg, setBusyMsg] = useState<string | null>(null);
  const [dragOver, setDragOver] = useState(false);
  const [saving, setSaving] = useState(false);
  const editorRef = useRef<OnlyOfficeEditorHandle>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const revisionRef = useRef(0);
  // Edits not yet persisted by a successful forcesave (gates the unmount flush).
  const editedRef = useRef(false);
  // One keepalive forcesave per unload: beforeunload and pagehide both fire.
  const unloadFlushedRef = useRef(false);
  const lastUpdatedToastRef = useRef(0);
  const onSelectionRef = useRef(onSelectionChange);
  onSelectionRef.current = onSelectionChange;
  // editor_key of the version currently shown in the editor (mount config or
  // the last refreshFile()). Only ever advances, so a key is applied once.
  const currentKeyRef = useRef<string | null>(null);
  // onDocumentReady seen for the current editor instance.
  const editorReadyRef = useRef(false);
  // A refreshFile() was just issued (cleared on onDocumentReady or cooldown).
  const refreshInFlightRef = useRef(false);
  const refreshCooldownRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // GET serialization: one recheck at a time, a request during it re-runs once.
  const checkingRef = useRef(false);
  const recheckAgainRef = useRef(false);
  const aliveRef = useRef(true);
  useEffect(() => {
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
      if (refreshCooldownRef.current) clearTimeout(refreshCooldownRef.current);
    };
  }, []);

  const applyView = useCallback((v: DocumentWorkspaceView) => {
    setView(v);
    revisionRef.current = v.revision;
    if (!v.editor) {
      // Workspace exists but the server did not hand out an editor config.
      setPhase({ kind: "disabled" });
      return;
    }
    // Every caller mounts a fresh editor (load() clears `mounted` first).
    currentKeyRef.current = v.editor_key || null;
    editorReadyRef.current = false;
    refreshInFlightRef.current = false;
    setMounted(v);
    setPhase({ kind: "editor" });
  }, []);

  const loadAttachments = useCallback(async () => {
    try {
      const list = await listTemporaryAttachments(sessionId);
      setAttachments(list.filter((a) => isWordAttachment(a.file_name, a.file_type) && a.status !== "failed"));
    } catch {
      setAttachments([]);
    }
  }, [sessionId]);

  const load = useCallback(async () => {
    setPhase({ kind: "loading" });
    setEditorFailed(null);
    setMounted(null);
    editorReadyRef.current = false;
    try {
      const v = await getDocumentWorkspace(sessionId);
      if (v) {
        applyView(v);
      } else {
        setPhase({ kind: "empty" });
        void loadAttachments();
      }
    } catch (e) {
      if (e instanceof DocumentWorkspaceError && e.code === "editor_disabled") setPhase({ kind: "disabled" });
      else setPhase({ kind: "error", message: errMessage(e) });
    }
  }, [sessionId, applyView, loadAttachments]);

  // Fresh state per session.
  useEffect(() => {
    setView(null);
    setMounted(null);
    setDirty(false);
    setAttachments([]);
    setBusyMsg(null);
    revisionRef.current = 0;
    currentKeyRef.current = null;
    editedRef.current = false;
    unloadFlushedRef.current = false;
    void load();
  }, [load]);

  /* ---------- create from an attachment ---------- */
  const openAttachment = async (attachmentId: string) => {
    setBusyMsg(t("docws.opening"));
    try {
      applyView(await createDocumentWorkspace(sessionId, attachmentId));
    } catch (e) {
      if (e instanceof DocumentWorkspaceError && e.code === "already_exists") {
        await load();
      } else if (e instanceof DocumentWorkspaceError && e.code === "editor_disabled") {
        setPhase({ kind: "disabled" });
      } else {
        toast.error(`${t("docws.openFailed")}: ${errMessage(e)}`);
      }
    } finally {
      setBusyMsg(null);
    }
  };

  const uploadAndOpen = async (file: File) => {
    if (!isWordAttachment(file.name)) {
      toast.error(t("docws.onlyWord"));
      return;
    }
    setBusyMsg(t("docws.uploading", { percent: 0 }));
    try {
      const res = await uploadTemporaryAttachment(sessionId, file, BUILTIN_DOCUMENT_ASSISTANT_ID, "auto", (p) =>
        setBusyMsg(t("docws.uploading", { percent: Math.round(p) })),
      );
      if (!res?.data?.id) throw new Error("upload returned no attachment id");
      await openAttachment(res.data.id);
    } catch (e) {
      toast.error(`${t("docws.openFailed")}: ${errMessage(e)}`);
      setBusyMsg(null);
    }
  };

  /* ---------- server revisions → refresh editor ---------- */
  const notifyUpdated = useCallback(() => {
    // A burst of revisions produces one toast.
    const now = Date.now();
    if (now - lastUpdatedToastRef.current >= UPDATED_TOAST_THROTTLE_MS) {
      lastUpdatedToastRef.current = now;
      toast.info(t("docws.updated"));
    }
  }, [toast, t]);

  /* Swap the editor to `v` (already decided). Advances currentKeyRef first so
   * the same key is never applied twice, then holds further refreshes until
   * the editor reports ready again or the cooldown passes. */
  const refreshTo = useCallback((v: DocumentWorkspaceView) => {
    if (!v.editor) return;
    currentKeyRef.current = v.editor_key || currentKeyRef.current;
    refreshInFlightRef.current = true;
    if (refreshCooldownRef.current) clearTimeout(refreshCooldownRef.current);
    refreshCooldownRef.current = setTimeout(() => {
      refreshInFlightRef.current = false;
    }, REFRESH_COOLDOWN_MS);
    editorRef.current?.refreshFile(v.editor.config);
  }, []);

  /* Re-GET the workspace; refresh the editor only when editor_key changed.
   * Always adopts the server view (status pill, revision badge). */
  const recheck = useCallback(async () => {
    if (checkingRef.current) {
      recheckAgainRef.current = true;
      return;
    }
    checkingRef.current = true;
    try {
      const v = await getDocumentWorkspace(sessionId);
      if (!aliveRef.current || !v) return;
      revisionRef.current = v.revision;
      setView(v);
      if (!v.editor) return;
      if (
        shouldRefreshEditor({
          currentKey: currentKeyRef.current,
          nextKey: v.editor_key,
          editorReady: editorReadyRef.current,
          inFlight: refreshInFlightRef.current,
        })
      ) {
        refreshTo(v);
        notifyUpdated();
      }
      // Not ready / in flight: onDocumentReady or the next trigger re-checks.
    } catch {
      /* next trigger retries */
    } finally {
      checkingRef.current = false;
      if (recheckAgainRef.current && aliveRef.current) {
        recheckAgainRef.current = false;
        void recheck();
      }
    }
  }, [sessionId, refreshTo, notifyUpdated]);
  const recheckRef = useRef(recheck);
  recheckRef.current = recheck;

  const isEditor = phase.kind === "editor";

  // Tool result in the chat stream.
  useEffect(() => {
    if (isEditor && revision !== null) void recheckRef.current();
  }, [revision, isEditor]);

  // Chat turn ended (incl. dropped stream / abort): check now and again a
  // little later, since the backend may still be finishing the edit.
  useEffect(() => {
    if (!isEditor || !recheckToken) return;
    void recheckRef.current();
    const timers = SETTLE_RECHECK_MS.map((ms) => setTimeout(() => void recheckRef.current(), ms));
    return () => timers.forEach(clearTimeout);
  }, [recheckToken, isEditor]);

  // Fallback polling + re-check when the tab/window comes back.
  useEffect(() => {
    if (!isEditor) return;
    const tick = () => {
      if (typeof document !== "undefined" && document.visibilityState === "hidden") return;
      void recheckRef.current();
    };
    const timer = setInterval(tick, turnInFlight ? POLL_BUSY_MS : POLL_IDLE_MS);
    const onVisible = () => {
      if (document.visibilityState === "visible") tick();
    };
    document.addEventListener("visibilitychange", onVisible);
    window.addEventListener("focus", tick);
    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
      window.removeEventListener("focus", tick);
    };
  }, [isEditor, turnInFlight]);

  /* ---------- flush pending edits when the page goes away ---------- */
  useEffect(() => {
    if (!isEditor) return;
    const flush = () => {
      if (unloadFlushedRef.current) return;
      unloadFlushedRef.current = true;
      void forceSaveDocumentWorkspaceKeepalive(sessionId).then((ok) => {
        if (ok) editedRef.current = false;
      });
    };
    // A page restored from bfcache is live again: allow the next unload.
    const onPageShow = (e: PageTransitionEvent) => {
      if (e.persisted) unloadFlushedRef.current = false;
    };
    window.addEventListener("beforeunload", flush);
    window.addEventListener("pagehide", flush);
    window.addEventListener("pageshow", onPageShow);
    return () => {
      window.removeEventListener("beforeunload", flush);
      window.removeEventListener("pagehide", flush);
      window.removeEventListener("pageshow", onPageShow);
      // In-app navigation (session switch) unmounts without an unload event.
      if (editedRef.current) flush();
    };
  }, [isEditor, sessionId]);

  /* ---------- editor selection (werag-assistant plugin) → chat ---------- */
  const dsUrl = mounted?.editor?.document_server_url;
  useEffect(() => {
    const origin = documentServerOrigin(dsUrl);
    if (!origin) return;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const onMessage = (event: MessageEvent) => {
      if (event.origin !== origin) return;
      const sel = parsePluginSelectionMessage(event.data);
      if (!sel) return; // not ours, or an empty selection — keep the last one
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => onSelectionRef.current(sel), SELECTION_DEBOUNCE_MS);
    };
    window.addEventListener("message", onMessage);
    return () => {
      window.removeEventListener("message", onMessage);
      if (timer) clearTimeout(timer);
    };
  }, [dsUrl]);

  /* ---------- top-bar actions ---------- */
  const download = async () => {
    try {
      const blob = await downloadDocumentWorkspace(sessionId);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = view?.file_name || "document.docx";
      document.body.appendChild(a);
      a.click();
      a.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (e) {
      toast.error(`${t("docws.downloadFailed")}: ${errMessage(e)}`);
    }
  };

  const saveNow = async () => {
    setSaving(true);
    try {
      await forceSaveDocumentWorkspace(sessionId);
      editedRef.current = false;
      toast.success(t("docws.saveRequested"));
    } catch (e) {
      toast.error(`${t("docws.saveFailed")}: ${errMessage(e)}`);
    } finally {
      setSaving(false);
    }
  };

  /* ---------- render ---------- */
  if (phase.kind === "loading") {
    return <Centered><p className="body-sm text-muted">{t("docws.loading")}</p></Centered>;
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
        <button type="button" className="btn btn-outline btn-sm mt-4" onClick={() => void load()}>
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
          if (file && !busyMsg) void uploadAndOpen(file);
        }}
      >
        <input
          ref={fileInputRef}
          type="file"
          accept={WORD_ACCEPT}
          className="hidden"
          onChange={(e) => {
            const file = e.target.files?.[0];
            e.target.value = "";
            if (file) void uploadAndOpen(file);
          }}
        />
        <div
          className={`flex w-full max-w-[520px] flex-col items-center rounded-xl border-2 border-dashed px-6 py-10 text-center transition-colors ${
            dragOver ? "border-ink/40 bg-surface-strong" : "border-hairline"
          }`}
        >
          <IconDoc className="mb-3 h-9 w-9 text-muted-soft" />
          <p className="text-[15px] font-medium text-ink">{dragOver ? t("docws.dropHere") : t("docws.emptyTitle")}</p>
          <p className="body-sm mt-1 text-muted">{t("docws.emptyDesc")}</p>
          {busyMsg ? (
            <p className="body-sm mt-4 text-ink">{busyMsg}</p>
          ) : (
            <button type="button" className="btn btn-primary btn-sm mt-4" onClick={() => fileInputRef.current?.click()}>
              {t("docws.pickFile")}
            </button>
          )}
        </div>
        {attachments.length > 0 && (
          <div className="mt-6 w-full max-w-[520px]">
            <p className="caption-uppercase mb-2 text-muted-soft">{t("docws.existingTitle")}</p>
            <ul className="flex flex-col gap-1.5">
              {attachments.map((a) => (
                <li key={a.id} className="flex items-center gap-2 rounded-lg border border-hairline px-3 py-2">
                  <IconDoc className="h-4 w-4 shrink-0 text-muted" />
                  <span className="min-w-0 flex-1 truncate text-[13px] text-ink" title={a.file_name}>
                    {a.file_name}
                  </span>
                  <button
                    type="button"
                    className="btn btn-outline btn-sm"
                    disabled={Boolean(busyMsg)}
                    onClick={() => void openAttachment(a.id)}
                  >
                    {t("docws.openThis")}
                  </button>
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    );
  }

  // editor
  const ed = mounted?.editor;
  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="hairline-b flex h-14 shrink-0 items-center gap-2 px-3 sm:px-4">
        <IconDoc className="h-4 w-4 shrink-0 text-muted" />
        <span className="min-w-0 flex-1 truncate text-[14px] font-medium text-ink" title={view?.file_name}>
          {view?.file_name}
        </span>
        {view?.status === "closed" ? (
          <Pill tone="muted">{t("docws.closed")}</Pill>
        ) : (
          <Pill tone={dirty ? "warn" : "ok"}>{dirty ? t("docws.saving") : t("docws.saved")}</Pill>
        )}
        {view && <Pill tone="muted">{t("docws.revision", { n: view.revision })}</Pill>}
        <button type="button" className="btn btn-ghost btn-sm" onClick={() => void download()} title={t("docws.download")}>
          <IconDownload className="h-3.5 w-3.5" />
          <span className="hidden xl:inline">{t("docws.download")}</span>
        </button>
        <button type="button" className="btn btn-outline btn-sm" disabled={saving} onClick={() => void saveNow()}>
          {t("docws.saveNow")}
        </button>
      </div>
      <div className="relative min-h-0 flex-1">
        {editorFailed ? (
          <Centered>
            <p className="text-[15px] font-medium text-ink">{t("docws.editorLoadFailed")}</p>
            <p className="body-sm mt-1 max-w-[420px] text-center text-muted">{editorFailed}</p>
            <button type="button" className="btn btn-outline btn-sm mt-4" onClick={() => void load()}>
              <IconRefresh className="h-3.5 w-3.5" /> {t("docws.retry")}
            </button>
          </Centered>
        ) : ed ? (
          <OnlyOfficeEditor
            ref={editorRef}
            documentServerUrl={ed.document_server_url}
            config={ed.config}
            onLoadError={(e) => setEditorFailed(e.message)}
            events={{
              onDocumentReady: () => {
                editorReadyRef.current = true;
                refreshInFlightRef.current = false;
                // Catch up on a key change that arrived while loading.
                void recheckRef.current();
              },
              onDocumentStateChange: (d) => {
                setDirty(d);
                // "saved" (false) only means the DS has the edits, not WeRAG's
                // stored file — editedRef clears on a successful forcesave only.
                if (d) {
                  editedRef.current = true;
                  unloadFlushedRef.current = false; // new edits re-arm the unload flush
                }
              },
              onRequestRefreshFile: () => {
                // The editor itself asks for a fresh config: apply it even if
                // the key is unchanged (version change / reconnect).
                void getDocumentWorkspace(sessionId)
                  .then((v) => {
                    if (!aliveRef.current || !v?.editor) return;
                    revisionRef.current = v.revision;
                    setView(v);
                    refreshTo(v);
                  })
                  .catch(() => {});
              },
              onError: (code, desc) => console.error("[onlyoffice] error", code, desc),
              onWarning: (code, desc) => console.warn("[onlyoffice] warning", code, desc),
            }}
          />
        ) : null}
      </div>
    </div>
  );
}

function Centered({ children }: { children: React.ReactNode }) {
  return <div className="flex h-full flex-col items-center justify-center p-6">{children}</div>;
}

function Pill({ tone, children }: { tone: "ok" | "warn" | "muted"; children: React.ReactNode }) {
  const cls =
    tone === "ok"
      ? "bg-success/10 text-success"
      : tone === "warn"
        ? "bg-amber-500/15 text-amber-600" // same as processing chips in knowledge/processing-timeline
        : "bg-surface-strong text-muted";
  return <span className={`shrink-0 whitespace-nowrap rounded-full px-2 py-0.5 text-[11.5px] font-medium ${cls}`}>{children}</span>;
}
