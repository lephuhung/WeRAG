"use client";

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import {
  IconCheck,
  IconDownload,
  IconFolder,
  IconMinusCircle,
  IconMoreHorizontal,
  IconRefresh,
  IconTrash,
} from "@/components/icons";
import {
  cancelKnowledgeParse,
  deleteKnowledge,
  downloadKnowledge,
  listKnowledgeFolders,
  moveKnowledgeToFolder,
  type KnowledgeDoc,
  type KnowledgeFolderNode,
} from "@/lib/api/knowledge";
import { useT } from "@/lib/i18n";

/* Ported from frontend/src/views/knowledge/components/DocumentActionMenu.vue,
 * reduced to the actions whose backing APIs/UI exist here: download, rebuild,
 * stop parsing, move to folder, delete. The Vue menu additionally offers
 * edit (manual docs), view-trace, move-to-another-KB and batch manage —
 * those flows are not ported yet. */

const IN_FLIGHT = new Set(["pending", "processing", "finalizing"]);

type Mode = "main" | "confirm-cancel" | "confirm-delete" | "folder";

type FolderOption = { path: string; label: string; depth: number };

function flattenFolders(nodes: KnowledgeFolderNode[] | undefined, depth = 0): FolderOption[] {
  const out: FolderOption[] = [];
  for (const n of nodes ?? []) {
    out.push({ path: n.path, label: n.name, depth });
    out.push(...flattenFolders(n.children, depth + 1));
  }
  return out;
}

export function DocActionsMenu({
  doc,
  kbId,
  canMutate,
  onChanged,
  onReparse,
}: {
  doc: KnowledgeDoc;
  kbId: string;
  canMutate: boolean;
  /** Called after an action that changed the document (delete/move/cancel…). */
  onChanged: () => void;
  /** Rebuild opens the parse-settings dialog (Vue UploadConfirmDialog
   * reparse mode); the owner submits reparseKnowledge from there. */
  onReparse?: (doc: KnowledgeDoc) => void;
}) {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<Mode>("main");
  const [folders, setFolders] = useState<FolderOption[] | null>(null);
  const [newFolder, setNewFolder] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const btnRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null);

  const parseStatus = doc.parse_status ?? doc.status ?? "";
  const inFlight = IN_FLIGHT.has(parseStatus);
  const fileName = doc.title || doc.file_name || doc.id;
  const canDownload = doc.type === "file" || doc.type === "manual" || !doc.type;

  const close = () => {
    setOpen(false);
    setMode("main");
    setNewFolder("");
    setError("");
  };

  const toggle = () => {
    if (open) {
      close();
      return;
    }
    const rect = btnRef.current?.getBoundingClientRect();
    if (rect) {
      const menuWidth = 220;
      const menuHeight = 300;
      let top = rect.bottom + 4;
      if (top + menuHeight > window.innerHeight - 8) top = Math.max(8, rect.top - menuHeight - 4);
      const left = Math.min(Math.max(8, rect.right - menuWidth), window.innerWidth - menuWidth - 8);
      setPos({ top, left });
    }
    setOpen(true);
  };

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (e: PointerEvent) => {
      if (menuRef.current?.contains(e.target as Node) || btnRef.current?.contains(e.target as Node))
        return;
      close();
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && close();
    const onScroll = () => close();
    document.addEventListener("pointerdown", onPointerDown, true);
    document.addEventListener("keydown", onKey);
    document.addEventListener("scroll", onScroll, true);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown, true);
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("scroll", onScroll, true);
    };
  }, [open]);

  const run = async (fn: () => Promise<unknown>, refresh = true) => {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await fn();
      close();
      if (refresh) onChanged();
    } catch {
      setError(t("doc.actionFailed"));
    } finally {
      setBusy(false);
    }
  };

  const download = async () => {
    close();
    try {
      const blob = await downloadKnowledge(doc.id);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = doc.file_name || fileName;
      a.click();
      URL.revokeObjectURL(url);
    } catch {
      /* surfaced nowhere — the DocPanel download path is the same */
    }
  };

  const openFolderPicker = () => {
    setMode("folder");
    if (folders !== null) return;
    listKnowledgeFolders(kbId)
      .then((res) => setFolders(flattenFolders(res?.data?.folders)))
      .catch(() => setFolders([]));
  };

  const moveTo = (path: string) =>
    run(() => moveKnowledgeToFolder(kbId, [doc.id], path));

  const itemCls =
    "flex w-full items-center gap-2.5 rounded-[8px] px-3 py-2 text-left text-[13px] leading-5 text-ink transition-colors hover:bg-surface-strong";
  const dangerCls = `${itemCls} text-error hover:bg-surface-strong`;

  const confirmView = (message: string, okLabel: string, action: () => Promise<unknown>) => (
    <div className="p-3">
      <p className="text-[12.5px] leading-[18px] text-body">{message}</p>
      {error && <p className="caption mt-1.5 text-error">{error}</p>}
      <div className="mt-3 flex justify-end gap-2">
        <button type="button" className="btn btn-outline btn-sm h-7" onClick={() => setMode("main")} disabled={busy}>
          {t("common.cancel")}
        </button>
        <button
          type="button"
          className="btn btn-primary btn-sm h-7"
          disabled={busy}
          onClick={() => void run(action)}
        >
          {okLabel}
        </button>
      </div>
    </div>
  );

  const menu = (
    <div
      ref={menuRef}
      className="fixed z-50 w-[220px] rounded-[12px] border border-hairline bg-surface-card p-1 shadow-[0_8px_30px_rgba(0,0,0,0.12)]"
      style={pos ?? undefined}
      role="menu"
      onClick={(e) => e.stopPropagation()}
    >
      {mode === "confirm-cancel" &&
        confirmView(t("doc.cancelParseConfirm", { fileName }), t("doc.cancelParse"), () => cancelKnowledgeParse(doc.id))}
      {mode === "confirm-delete" &&
        confirmView(t("doc.deleteConfirm", { fileName }), t("doc.delete"), () => deleteKnowledge(doc.id))}

      {mode === "folder" && (
        <div>
          <div className="px-3 pb-1 pt-2 text-[11px] font-medium uppercase tracking-wide text-muted-soft">
            {t("doc.moveToFolder")}
          </div>
          <div className="max-h-[220px] overflow-y-auto">
            <button type="button" className={itemCls} disabled={busy} onClick={() => void moveTo("")}>
              <IconFolder className="h-4 w-4 shrink-0 text-muted" />
              {t("doc.folderRoot")}
              {(doc.folder_path ?? "") === "" && <IconCheck className="ml-auto h-3.5 w-3.5 text-muted" />}
            </button>
            {(folders ?? []).map((f) => (
              <button
                key={f.path}
                type="button"
                className={itemCls}
                disabled={busy}
                style={{ paddingLeft: `${12 + f.depth * 14}px` }}
                onClick={() => void moveTo(f.path)}
              >
                <IconFolder className="h-4 w-4 shrink-0 text-muted" />
                <span className="truncate">{f.label}</span>
                {doc.folder_path === f.path && <IconCheck className="ml-auto h-3.5 w-3.5 shrink-0 text-muted" />}
              </button>
            ))}
            {folders === null && <p className="caption px-3 py-2 text-muted">Loading…</p>}
          </div>
          <div className="border-t border-hairline p-2">
            <input
              className="input h-8 w-full text-[12.5px]"
              placeholder={t("doc.newFolderPlaceholder")}
              value={newFolder}
              disabled={busy}
              onChange={(e) => setNewFolder(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && newFolder.trim()) {
                  const path = newFolder.trim().replace(/^\/+|\/+$/g, "");
                  if (path) void moveTo(path);
                }
              }}
            />
          </div>
          {error && <p className="caption px-3 pb-2 text-error">{error}</p>}
          <button
            type="button"
            className={`${itemCls} border-t border-hairline-soft`}
            onClick={() => setMode("main")}
          >
            ← {t("common.cancel")}
          </button>
        </div>
      )}

      {mode === "main" && (
        <>
          {canDownload && (
            <button type="button" className={itemCls} onClick={() => void download()}>
              <IconDownload className="h-4 w-4 text-muted" />
              {t("doc.download")}
            </button>
          )}
          {canMutate && (
            <>
              {/* Vue blocks a rebuild while parsing is in flight (it shows an
               * "already parsing" toast); mirror that with an inline note. */}
              <button
                type="button"
                className={itemCls}
                disabled={busy}
                onClick={() => {
                  if (inFlight) {
                    setError(t("doc.rebuildInProgress"));
                    return;
                  }
                  close();
                  onReparse?.(doc);
                }}
              >
                <IconRefresh className="h-4 w-4 text-muted" />
                {t("doc.rebuild")}
              </button>
              {inFlight && (
                <button type="button" className={itemCls} onClick={() => setMode("confirm-cancel")}>
                  <IconMinusCircle className="h-4 w-4 text-muted" />
                  {t("doc.cancelParse")}
                </button>
              )}
              <button type="button" className={itemCls} onClick={openFolderPicker}>
                <IconFolder className="h-4 w-4 text-muted" />
                {t("doc.moveToFolder")}
              </button>
              <div className="mx-2 my-1 border-t border-hairline" />
              <button type="button" className={dangerCls} onClick={() => setMode("confirm-delete")}>
                <IconTrash className="h-4 w-4" />
                {t("doc.delete")}
              </button>
            </>
          )}
          {error && <p className="caption px-3 pb-2 pt-1 text-error">{error}</p>}
        </>
      )}
    </div>
  );

  return (
    <>
      <button
        ref={btnRef}
        type="button"
        aria-label="Document actions"
        aria-expanded={open}
        className="grid h-6 w-6 place-items-center rounded-[6px] text-muted-soft opacity-0 transition-opacity hover:bg-surface-strong hover:text-ink focus-visible:opacity-100 group-hover:opacity-100 group-focus-within:opacity-100 data-[open=true]:opacity-100"
        data-open={open}
        onClick={(e) => {
          e.stopPropagation();
          toggle();
        }}
      >
        <IconMoreHorizontal className="h-4 w-4" />
      </button>
      {open && pos && typeof document !== "undefined" && createPortal(menu, document.body)}
    </>
  );
}
