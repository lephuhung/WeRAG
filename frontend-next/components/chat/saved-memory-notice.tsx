/* "I just saved …" notice. The assistant stores what it learns about the
 * user on its own and tells them on the next turn; a wrong memory is taken
 * back with one click (users have no memory settings page). Only the
 * caller's own memory can be removed — the backend derives the owner from
 * the session.
 */
"use client";

import { useState } from "react";
import { useT } from "@/lib/i18n";
import { rejectMemoryItem } from "@/lib/api/memory";

export type SavedMemory = { id: string; content: string };

/** Reads the memories announced by a `memory_saved` stream event. */
export function parseSavedMemories(data: unknown): SavedMemory[] {
  const raw = (data as { memories?: unknown } | undefined)?.memories;
  if (!Array.isArray(raw)) return [];
  return raw
    .map((m) => m as { id?: unknown; content?: unknown })
    .filter((m) => typeof m.id === "string" && typeof m.content === "string" && m.content)
    .map((m) => ({ id: m.id as string, content: m.content as string }));
}

export function SavedMemoryNotice({ memories, compact = false }: { memories: SavedMemory[]; compact?: boolean }) {
  const { t } = useT();
  const [removed, setRemoved] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  if (memories.length === 0) return null;

  const takeBack = async (id: string) => {
    setBusy(id);
    setFailed(false);
    try {
      await rejectMemoryItem(id);
      setRemoved((r) => ({ ...r, [id]: true }));
    } catch {
      setFailed(true);
    } finally {
      setBusy(null);
    }
  };

  return (
    <div
      className={`${compact ? "mt-2 text-[12.5px]" : "mt-3 text-[13px]"} rounded-xl border border-hairline bg-surface-card px-3 py-2.5 text-body`}
    >
      <p className="mb-1.5 text-muted">{t("memSaved.intro")}</p>
      <ul className="flex flex-col gap-1.5">
        {memories.map((m) => (
          <li key={m.id} className="flex items-start justify-between gap-3">
            <span className={removed[m.id] ? "text-muted-soft line-through" : "text-ink"}>{m.content}</span>
            {removed[m.id] ? (
              <span className="shrink-0 text-muted">{t("memSaved.removed")}</span>
            ) : (
              <button
                type="button"
                className="shrink-0 rounded-md px-2 py-0.5 text-muted hover:bg-surface-strong hover:text-ink disabled:opacity-50"
                disabled={busy !== null}
                onClick={() => takeBack(m.id)}
              >
                {t("memSaved.wrong")}
              </button>
            )}
          </li>
        ))}
      </ul>
      <p className="mt-1.5 text-muted-soft">{failed ? t("memSaved.failed") : t("memSaved.hint")}</p>
    </div>
  );
}
