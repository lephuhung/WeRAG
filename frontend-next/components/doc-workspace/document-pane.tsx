"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useT } from "@/lib/i18n";
import { useToast } from "@/components/toast";
import {
  DocumentWorkspaceError,
  documentServerOrigin,
  downloadDocumentWorkspace,
  forceSaveDocumentWorkspace,
  forceSaveDocumentWorkspaceKeepalive,
  getDocumentWorkspace,
  parsePluginSelectionMessage,
  shouldRefreshEditor,
  type DocumentSelection,
  type DocumentWorkspaceView,
} from "@/lib/api/document-workspace";
import type { OpsBatch, OpsFailure } from "@/lib/api/document-ops";
import { IconClock, IconDownload, IconRefresh } from "@/components/icons";
import { OnlyOfficeEditor, type OnlyOfficeEditorHandle } from "./onlyoffice-editor";
import { RevisionHistoryPanel } from "./revision-history";
import { useEditorOps, type EditorOpsOutcome } from "./use-editor-ops";

type Phase = { kind: "loading" } | { kind: "disabled" } | { kind: "error"; message: string } | { kind: "editor" };

// Fallback polling of the document (SSE tool results can be lost when the
// stream is cut while the backend still finishes the edit).
const POLL_BUSY_MS = 20_000;
const POLL_IDLE_MS = 30_000;
// While the background format check runs, poll faster so its result shows
// up soon after it is ready.
const POLL_FORMAT_CHECK_MS = 5_000;
// Follow-up re-checks after a chat turn ends: the backend may still be
// committing the edit when a dropped stream surfaces client-side.
const SETTLE_RECHECK_MS = [4_000, 12_000];
// Minimum gap between two refreshFile() calls unless onDocumentReady fires.
const REFRESH_COOLDOWN_MS = 1_500;

function errMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/* One document of the document assistant: an ONLYOFFICE editor bound to one
 * workspace (tab). It applies the assistant's edit plans for this document
 * inside the editor (through the werag-assistant plugin, so Ctrl+Z undoes
 * them), reloads on restores (editor_key change) and forwards selections.
 * A pane stays mounted while hidden so its editor keeps its undo history;
 * only the visible (`active`) pane shows its toolbar (portalled into
 * `toolbarSlot`) and forwards selections. */
export function DocumentPane({
  sessionId,
  documentId,
  active,
  toolbarSlot,
  opsBatches,
  recheckToken = 0,
  turnInFlight = false,
  onSelectionChange,
  onViewChange,
  onEditorDisabled,
}: {
  sessionId: string;
  documentId: string;
  active: boolean;
  toolbarSlot: HTMLElement | null;
  /** This document's edit plans (append-only), applied once each. */
  opsBatches: (OpsBatch & { rejected?: OpsFailure[] })[];
  recheckToken?: number;
  turnInFlight?: boolean;
  onSelectionChange: (sel: DocumentSelection | null) => void;
  /** Latest server view of the document (status, revision, format check). */
  onViewChange: (view: DocumentWorkspaceView) => void;
  onEditorDisabled: () => void;
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
  const [saving, setSaving] = useState(false);
  const editorRef = useRef<OnlyOfficeEditorHandle>(null);
  // Edits not yet persisted by a successful forcesave (gates the unmount flush).
  const editedRef = useRef(false);
  // One keepalive forcesave per unload: beforeunload and pagehide both fire.
  const unloadFlushedRef = useRef(false);
  const onSelectionRef = useRef(onSelectionChange);
  onSelectionRef.current = onSelectionChange;
  const onViewRef = useRef(onViewChange);
  onViewRef.current = onViewChange;
  const onDisabledRef = useRef(onEditorDisabled);
  onDisabledRef.current = onEditorDisabled;
  const activeRef = useRef(active);
  activeRef.current = active;
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
  // Set once useEditorOps runs (declared further down): refreshTo() drops
  // pending edit plans when the document under the editor is replaced.
  const resetOpsRef = useRef<() => void>(() => {});
  useEffect(() => {
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
      if (refreshCooldownRef.current) clearTimeout(refreshCooldownRef.current);
    };
  }, []);

  const adopt = useCallback((v: DocumentWorkspaceView) => {
    setView(v);
    onViewRef.current(v);
  }, []);

  const load = useCallback(async () => {
    setPhase({ kind: "loading" });
    setEditorFailed(null);
    setMounted(null);
    editorReadyRef.current = false;
    try {
      const v = await getDocumentWorkspace(sessionId, documentId);
      if (!aliveRef.current) return;
      if (!v) {
        setPhase({ kind: "error", message: t("docws.loadFailed") });
        return;
      }
      adopt(v);
      if (!v.editor) {
        setPhase({ kind: "disabled" });
        onDisabledRef.current();
        return;
      }
      currentKeyRef.current = v.editor_key || null;
      refreshInFlightRef.current = false;
      setMounted(v);
      setPhase({ kind: "editor" });
    } catch (e) {
      if (!aliveRef.current) return;
      if (e instanceof DocumentWorkspaceError && e.code === "editor_disabled") {
        setPhase({ kind: "disabled" });
        onDisabledRef.current();
      } else setPhase({ kind: "error", message: errMessage(e) });
    }
  }, [sessionId, documentId, adopt, t]);

  useEffect(() => {
    void load();
  }, [load]);

  /* ---------- server key changes (restore, close/reopen) → refresh editor ----------
   * AI edits do not change the stored file: they arrive as edit plans (see
   * useEditorOps), so a key change is silent here — the restore UI shows its
   * own toast. */
  /* Swap the editor to `v` (already decided). Advances currentKeyRef first so
   * the same key is never applied twice, then holds further refreshes until
   * the editor reports ready again or the cooldown passes. */
  const refreshTo = useCallback((v: DocumentWorkspaceView) => {
    if (!v.editor) return;
    currentKeyRef.current = v.editor_key || currentKeyRef.current;
    refreshInFlightRef.current = true;
    resetOpsRef.current();
    if (refreshCooldownRef.current) clearTimeout(refreshCooldownRef.current);
    refreshCooldownRef.current = setTimeout(() => {
      refreshInFlightRef.current = false;
    }, REFRESH_COOLDOWN_MS);
    editorRef.current?.refreshFile(v.editor.config);
  }, []);

  /* Re-GET the document; refresh the editor only when editor_key changed.
   * Always adopts the server view (status pill, revision badge). */
  const recheck = useCallback(async () => {
    if (checkingRef.current) {
      recheckAgainRef.current = true;
      return;
    }
    checkingRef.current = true;
    try {
      const v = await getDocumentWorkspace(sessionId, documentId);
      if (!aliveRef.current || !v) return;
      adopt(v);
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
  }, [sessionId, documentId, refreshTo, adopt]);
  const recheckRef = useRef(recheck);
  recheckRef.current = recheck;

  const isEditor = phase.kind === "editor";
  const formatCheckRunning = view?.format_check?.status === "running";

  // Chat turn ended (incl. dropped stream / abort): check now and again a
  // little later, since the backend may still be finishing the edit.
  useEffect(() => {
    if (!isEditor || !recheckToken) return;
    void recheckRef.current();
    const timers = SETTLE_RECHECK_MS.map((ms) => setTimeout(() => void recheckRef.current(), ms));
    return () => timers.forEach(clearTimeout);
  }, [recheckToken, isEditor]);

  // Fallback polling + re-check when the tab/window comes back. A hidden
  // pane polls at the idle rate.
  useEffect(() => {
    if (!isEditor) return;
    const tick = () => {
      if (typeof document !== "undefined" && document.visibilityState === "hidden") return;
      void recheckRef.current();
    };
    const timer = setInterval(
      tick,
      formatCheckRunning ? POLL_FORMAT_CHECK_MS : turnInFlight && active ? POLL_BUSY_MS : POLL_IDLE_MS,
    );
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
  }, [isEditor, turnInFlight, formatCheckRunning, active]);

  // Becoming the visible tab: catch up at once.
  useEffect(() => {
    if (active && isEditor) void recheckRef.current();
  }, [active, isEditor]);

  /* ---------- flush pending edits when the page goes away ---------- */
  useEffect(() => {
    if (!isEditor) return;
    const flush = () => {
      if (unloadFlushedRef.current) return;
      unloadFlushedRef.current = true;
      void forceSaveDocumentWorkspaceKeepalive(sessionId, documentId).then((ok) => {
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
      // In-app navigation (session switch, tab closed) unmounts without an unload event.
      if (editedRef.current) flush();
    };
  }, [isEditor, sessionId, documentId]);

  /* ---------- editor selection (werag-assistant plugin) → chat ---------- */
  const dsUrl = mounted?.editor?.document_server_url;
  useEffect(() => {
    const origin = documentServerOrigin(dsUrl);
    if (!origin) return;
    const onMessage = (event: MessageEvent) => {
      if (event.origin !== origin || !activeRef.current) return;
      // every open editor posts to this window: keep this editor's only
      if (!editorRef.current?.ownsMessageSource(event.source)) return;
      const sel = parsePluginSelectionMessage(event.data);
      if (!sel) return; // not ours, or an empty selection — keep the last one
      onSelectionRef.current({ ...sel, document_id: documentId });
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [dsUrl, documentId]);

  /* ---------- AI edit plans → editor plugin ---------- */
  const fileName = view?.file_name ?? "";
  const onOpsOutcome = useCallback(
    (o: EditorOpsOutcome) => {
      if (!aliveRef.current) return;
      const where = fileName ? `${fileName}: ` : "";
      if (o.kind === "timeout") {
        toast.error(where + t("docws.opsTimeout"));
        return;
      }
      const firstError = o.failed[0]?.error ?? "";
      if (o.failed.length === 0) toast.success(where + t("docws.opsApplied", { n: o.applied }));
      else if (o.applied > 0)
        toast.info(where + t("docws.opsPartial", { n: o.applied, total: o.total, failed: o.failed.length, error: firstError }));
      else toast.error(where + t("docws.opsNone", { total: o.total, error: firstError }));
    },
    [toast, t, fileName],
  );
  const { pump: pumpOps, reset: resetOps } = useEditorOps({
    sessionId,
    origin: documentServerOrigin(dsUrl),
    editorRef,
    isReady: () => editorReadyRef.current && phase.kind === "editor",
    batches: opsBatches,
    onOutcome: onOpsOutcome,
  });
  resetOpsRef.current = resetOps;

  /* ---------- snapshot timeline ---------- */
  const [historyOpen, setHistoryOpen] = useState(false);

  /* ---------- toolbar actions ---------- */
  const download = async () => {
    try {
      const blob = await downloadDocumentWorkspace(sessionId, documentId);
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
      await forceSaveDocumentWorkspace(sessionId, documentId);
      editedRef.current = false;
      toast.success(t("docws.saveRequested"));
    } catch (e) {
      toast.error(`${t("docws.saveFailed")}: ${errMessage(e)}`);
    } finally {
      setSaving(false);
    }
  };

  const toolbar =
    active && toolbarSlot && view
      ? createPortal(
          <>
            {view.status === "closed" ? (
              <Pill tone="muted">{t("docws.closed")}</Pill>
            ) : (
              <Pill tone={dirty ? "warn" : "ok"}>{dirty ? t("docws.saving") : t("docws.saved")}</Pill>
            )}
            <span className="hidden 2xl:inline-flex">
              <Pill tone="muted">{t("docws.revision", { n: view.revision })}</Pill>
            </span>
            <button
              type="button"
              className="btn btn-ghost btn-sm"
              onClick={() => setHistoryOpen(true)}
              title={t("docws.history")}
            >
              <IconClock className="h-3.5 w-3.5" />
              <span className="hidden 2xl:inline">{t("docws.history")}</span>
            </button>
            <button type="button" className="btn btn-ghost btn-sm" onClick={() => void download()} title={t("docws.download")}>
              <IconDownload className="h-3.5 w-3.5" />
              <span className="hidden 2xl:inline">{t("docws.download")}</span>
            </button>
            <button type="button" className="btn btn-outline btn-sm" disabled={saving || !isEditor} onClick={() => void saveNow()}>
              {t("docws.saveNow")}
            </button>
          </>,
          toolbarSlot,
        )
      : null;

  const ed = mounted?.editor;
  return (
    // Hidden panes keep their size (visibility, not display:none) so the
    // editor inside does not relayout when its tab comes back.
    <div className={`absolute inset-0 ${active ? "z-10" : "invisible z-0"}`} aria-hidden={!active}>
      {toolbar}
      {phase.kind === "loading" ? (
        <Centered>
          <p className="body-sm text-muted">{t("docws.loading")}</p>
        </Centered>
      ) : phase.kind === "disabled" ? (
        <Centered>
          <p className="text-[15px] font-medium text-ink">{t("docws.disabledTitle")}</p>
          <p className="body-sm mt-1 max-w-[420px] text-center text-muted">{t("docws.disabledDesc")}</p>
        </Centered>
      ) : phase.kind === "error" ? (
        <Centered>
          <p className="text-[15px] font-medium text-ink">{t("docws.loadFailed")}</p>
          <p className="body-sm mt-1 max-w-[420px] text-center text-muted">{phase.message}</p>
          <button type="button" className="btn btn-outline btn-sm mt-4" onClick={() => void load()}>
            <IconRefresh className="h-3.5 w-3.5" /> {t("docws.retry")}
          </button>
        </Centered>
      ) : editorFailed ? (
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
              // Catch up on a key change that arrived while loading, then
              // send the edit plans queued meanwhile.
              void recheckRef.current();
              pumpOps();
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
              void getDocumentWorkspace(sessionId, documentId)
                .then((v) => {
                  if (!aliveRef.current || !v?.editor) return;
                  adopt(v);
                  refreshTo(v);
                })
                .catch(() => {});
            },
            onError: (code, desc) => console.error("[onlyoffice] error", code, desc),
            onWarning: (code, desc) => console.warn("[onlyoffice] warning", code, desc),
          }}
        />
      ) : null}
      <RevisionHistoryPanel
        sessionId={sessionId}
        documentId={documentId}
        title={view?.file_name}
        open={historyOpen}
        onClose={() => setHistoryOpen(false)}
        onRestored={() => void recheckRef.current()}
      />
    </div>
  );
}

function Centered({ children }: { children: React.ReactNode }) {
  return <div className="flex h-full flex-col items-center justify-center p-6">{children}</div>;
}

export function Pill({ tone, children }: { tone: "ok" | "warn" | "muted"; children: React.ReactNode }) {
  const cls =
    tone === "ok"
      ? "bg-success/10 text-success"
      : tone === "warn"
        ? "bg-amber-500/15 text-amber-600" // same as processing chips in knowledge/processing-timeline
        : "bg-surface-strong text-muted";
  return <span className={`shrink-0 whitespace-nowrap rounded-full px-2 py-0.5 text-[11.5px] font-medium ${cls}`}>{children}</span>;
}
