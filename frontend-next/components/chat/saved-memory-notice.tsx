/* "I just saved …" / "I think …" notice. The assistant stores what it learns
 * about the user on its own and tells them on the next turn. Something the
 * user said is saved at once and can be taken back with one click; something
 * the system merely inferred arrives as a pending guess that takes effect
 * only when confirmed here (or under Settings → Memory). Only the caller's
 * own memory is touched — the backend derives the owner from the session.
 */
"use client";

import { useState } from "react";
import { useT } from "@/lib/i18n";
import { confirmMemoryItem, rejectMemoryItem } from "@/lib/api/memory";

export type SavedMemory = { id: string; content: string; pending?: boolean };

/** Reads the memories announced by a `memory_saved` stream event. */
export function parseSavedMemories(data: unknown): SavedMemory[] {
  const raw = (data as { memories?: unknown } | undefined)?.memories;
  if (!Array.isArray(raw)) return [];
  return raw
    .map((m) => m as { id?: unknown; content?: unknown; status?: unknown })
    .filter((m) => typeof m.id === "string" && typeof m.content === "string" && m.content)
    .map((m) => ({ id: m.id as string, content: m.content as string, pending: m.status === "pending" }));
}

type Outcome = "removed" | "confirmed";

export function SavedMemoryNotice({ memories, compact = false }: { memories: SavedMemory[]; compact?: boolean }) {
  const { t } = useT();
  const [done, setDone] = useState<Record<string, Outcome>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  if (memories.length === 0) return null;

  const act = async (id: string, outcome: Outcome) => {
    setBusy(id);
    setFailed(false);
    try {
      if (outcome === "confirmed") await confirmMemoryItem(id);
      else await rejectMemoryItem(id);
      setDone((r) => ({ ...r, [id]: outcome }));
    } catch {
      setFailed(true);
    } finally {
      setBusy(null);
    }
  };

  const saved = memories.filter((m) => !m.pending);
  const proposed = memories.filter((m) => m.pending);
  const btn =
    "shrink-0 rounded-md px-2 py-0.5 text-muted hover:bg-surface-strong hover:text-ink disabled:opacity-50";

  const renderRow = (m: SavedMemory) => (
    <li key={m.id} className="flex items-start justify-between gap-3">
      <span
        className={
          done[m.id] === "removed" ? "text-muted-soft line-through" : m.pending && !done[m.id] ? "text-body" : "text-ink"
        }
      >
        {m.content}
      </span>
      {done[m.id] === "removed" ? (
        <span className="shrink-0 text-muted">{t("memSaved.removed")}</span>
      ) : done[m.id] === "confirmed" ? (
        <span className="shrink-0 text-muted">{t("memSaved.confirmed")}</span>
      ) : (
        <span className="flex shrink-0 items-center gap-1">
          {m.pending && (
            <button type="button" className={btn} disabled={busy !== null} onClick={() => act(m.id, "confirmed")}>
              {t("memSaved.confirm")}
            </button>
          )}
          <button type="button" className={btn} disabled={busy !== null} onClick={() => act(m.id, "removed")}>
            {t("memSaved.wrong")}
          </button>
        </span>
      )}
    </li>
  );

  return (
    <div
      className={`${compact ? "mt-2 text-[12.5px]" : "mt-3 text-[13px]"} rounded-xl border border-hairline bg-surface-card px-3 py-2.5 text-body`}
    >
      {saved.length > 0 && (
        <>
          <p className="mb-1.5 text-muted">{t("memSaved.intro")}</p>
          <ul className="flex flex-col gap-1.5">{saved.map(renderRow)}</ul>
        </>
      )}
      {proposed.length > 0 && (
        <>
          <p className={`${saved.length > 0 ? "mt-2.5" : ""} mb-1.5 text-muted`}>{t("memSaved.proposedIntro")}</p>
          <ul className="flex flex-col gap-1.5">{proposed.map(renderRow)}</ul>
        </>
      )}
      <p className="mt-1.5 text-muted-soft">
        {failed ? t("memSaved.failed") : proposed.length > 0 ? t("memSaved.proposedHint") : t("memSaved.hint")}
      </p>
    </div>
  );
}
