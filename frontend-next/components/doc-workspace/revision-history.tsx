"use client";

import { useCallback, useEffect, useState } from "react";
import { useT, type LocaleKey } from "@/lib/i18n";
import { useToast } from "@/components/toast";
import { useConfirm } from "@/components/confirm-dialog";
import { SlidePanel, SlidePanelHeader } from "@/components/slide-panel";
import { IconRefresh } from "@/components/icons";
import {
  latestRevisionOf,
  listDocumentRevisions,
  restoreDocumentRevision,
  revisionsNewestFirst,
  type DocumentRevisionEntry,
} from "@/lib/api/document-workspace";

function errMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

const SOURCE_KEYS: Record<string, LocaleKey> = {
  ai: "docws.source.ai",
  manual: "docws.source.manual",
  close: "docws.source.close",
  restore: "docws.source.restore",
};

/* Server-side snapshot timeline of the workspace document. Restoring rotates
 * the editor key; `onRestored` lets the workspace re-check and reload. */
export function RevisionHistoryPanel({
  sessionId,
  open,
  onClose,
  onRestored,
}: {
  sessionId: string;
  open: boolean;
  onClose: () => void;
  onRestored: () => void;
}) {
  const { t, locale } = useT();
  const toast = useToast();
  const confirm = useConfirm();
  const [items, setItems] = useState<DocumentRevisionEntry[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [restoring, setRestoring] = useState<number | null>(null);

  const load = useCallback(async () => {
    setError(null);
    try {
      setItems(revisionsNewestFirst(await listDocumentRevisions(sessionId)));
    } catch (e) {
      setItems([]);
      setError(errMessage(e));
    }
  }, [sessionId]);

  useEffect(() => {
    if (open) void load();
  }, [open, load]);

  const fmt = (iso: string) => {
    const d = new Date(iso);
    return Number.isNaN(d.getTime())
      ? iso
      : d.toLocaleString(locale === "vi" ? "vi-VN" : "en-GB", { dateStyle: "short", timeStyle: "short" });
  };

  const restore = async (rev: DocumentRevisionEntry, message: string) => {
    const ok = await confirm({
      title: t("docws.restoreConfirmTitle"),
      message,
      confirmLabel: t("docws.restore"),
      danger: true,
    });
    if (!ok) return;
    setRestoring(rev.seq);
    try {
      await restoreDocumentRevision(sessionId, rev.seq);
      toast.success(t("docws.restored"));
      onRestored();
      void load();
    } catch (e) {
      toast.error(`${t("docws.restoreFailed")}: ${errMessage(e)}`);
    } finally {
      setRestoring(null);
    }
  };

  const latestAi = items ? latestRevisionOf(items, "ai") : null;

  return (
    <SlidePanel open={open} onClose={onClose} label={t("docws.historyTitle")} width="w-[400px]">
      <SlidePanelHeader
        title={t("docws.historyTitle")}
        onClose={onClose}
        actions={
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => void load()} title={t("docws.retry")}>
            <IconRefresh className="h-3.5 w-3.5" />
          </button>
        }
      />
      <div className="border-b border-hairline px-5 py-3">
        <button
          type="button"
          className="btn btn-outline btn-sm w-full"
          disabled={!latestAi || restoring !== null}
          onClick={() =>
            latestAi && void restore(latestAi, t("docws.undoAiConfirm", { label: latestAi.label || `#${latestAi.seq}` }))
          }
        >
          {t("docws.undoAi")}
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-3 py-2">
        {items === null ? (
          <p className="body-sm px-2 py-4 text-muted">{t("docws.loading")}</p>
        ) : error ? (
          <p className="body-sm px-2 py-4 text-error">
            {t("docws.historyLoadFailed")}: {error}
          </p>
        ) : items.length === 0 ? (
          <p className="body-sm px-2 py-4 text-muted">{t("docws.historyEmpty")}</p>
        ) : (
          <ul className="flex flex-col">
            {items.map((r) => (
              <li key={r.seq} className="flex items-center gap-3 rounded-lg px-2 py-2.5 hover:bg-surface-strong/60">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[13.5px] text-ink" title={r.label}>
                    {r.label || `#${r.seq}`}
                  </div>
                  <div className="caption mt-0.5 flex items-center gap-1.5 text-muted">
                    <span className="rounded-full bg-surface-strong px-1.5 py-px text-[11px] font-medium">
                      {SOURCE_KEYS[r.source] ? t(SOURCE_KEYS[r.source]) : r.source}
                    </span>
                    <span>{fmt(r.created_at)}</span>
                  </div>
                </div>
                <button
                  type="button"
                  className="btn btn-ghost btn-sm shrink-0"
                  disabled={restoring !== null}
                  onClick={() => void restore(r, t("docws.restoreConfirm", { label: r.label || `#${r.seq}` }))}
                >
                  {restoring === r.seq ? "…" : t("docws.restore")}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </SlidePanel>
  );
}
