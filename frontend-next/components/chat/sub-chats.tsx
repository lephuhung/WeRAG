/* Sub-chats ("chat nhỏ") of a document-assistant chat: the switcher in the
 * chat header and the banner that offers a new one once the context fills
 * up. Every sub-chat works on the same documents (the root chat's); each
 * keeps its own messages and context, and a new one starts from a summary
 * of the one it was opened from. */
"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useT } from "@/lib/i18n";
import { createSubChat, listSubChats, type SubChats } from "@/lib/api/chat";
import { contextFill, contextPercent, type ContextUsage } from "@/lib/context-usage";
import { IconCheck, IconChevronDown, IconClose, IconPlus } from "@/components/icons";

/** From this fill on the chat offers a new sub-chat. */
export const SUB_CHAT_FILL = 0.8;

/** The chat is long: its context is SUB_CHAT_FILL full, or earlier turns
 * of this chat were already summarized. Asking on still works (the server
 * keeps summarizing); a new sub-chat answers more precisely. A chat that
 * only starts from the previous chat's summary (seeded, e.g. a fresh
 * sub-chat) is not long yet. */
export function shouldOfferSubChat(usage: ContextUsage | null): boolean {
  return !!usage && (contextFill(usage) >= SUB_CHAT_FILL || (usage.compacted && !usage.seeded));
}

/** The chats of `sessionId`'s document chat; reloads when refreshKey moves. */
export function useSubChats(sessionId: string | null, refreshKey: string | number) {
  const [data, setData] = useState<SubChats | null>(null);
  useEffect(() => {
    if (!sessionId) {
      setData(null);
      return;
    }
    let alive = true;
    listSubChats(sessionId)
      .then((res) => alive && setData(res?.data ?? null))
      .catch(() => alive && setData(null));
    return () => {
      alive = false;
    };
  }, [sessionId, refreshKey]);
  return data;
}

/** Opens a new sub-chat from `sessionId` and goes to it. */
export function useOpenSubChat(sessionId: string) {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const open = useCallback(async () => {
    setBusy(true);
    setFailed(false);
    try {
      const next = (await createSubChat(sessionId))?.data?.id;
      if (!next) throw new Error("no session");
      router.push(`/platform/chat/${next}`);
    } catch {
      setFailed(true);
      setBusy(false);
    }
  }, [router, sessionId]);
  return { busy, failed, open };
}

function chatLabel(t: ReturnType<typeof useT>["t"], title: string | undefined, index: number) {
  return title?.trim() || t("subchat.untitled", { n: index + 1 });
}

/** The header switcher: which sub-chat this is, the others, and "new". */
export function SubChatSwitcher({ sessionId, chats, disabled }: { sessionId: string; chats: SubChats | null; disabled: boolean }) {
  const { t } = useT();
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const { busy, failed, open: openNew } = useOpenSubChat(sessionId);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", onDown);
    window.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      window.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const list = chats?.chats ?? [];
  const index = list.findIndex((c) => c.id === sessionId);
  const label = list.length > 0 && index >= 0 ? t("subchat.position", { n: index + 1, total: list.length }) : t("subchat.label");

  return (
    <div ref={rootRef} className="relative ml-2 shrink-0">
      <button
        type="button"
        className="flex h-7 items-center gap-1 rounded-full border border-hairline px-2.5 text-[12px] text-body hover:border-ink hover:text-ink"
        aria-haspopup="menu"
        aria-expanded={open}
        title={t("subchat.hint")}
        onClick={() => setOpen((o) => !o)}
      >
        {label}
        <IconChevronDown className="h-3 w-3" />
      </button>
      {open && (
        <div role="menu" className="card absolute right-0 top-full z-[60] mt-1 max-h-[60vh] w-[280px] overflow-y-auto p-1.5 shadow-[0_4px_16px_rgba(0,0,0,0.08)]">
          <p className="caption px-3 pb-1 pt-1.5 text-muted">{t("subchat.hint")}</p>
          {list.map((c, i) => (
            <button
              key={c.id}
              type="button"
              role="menuitem"
              className="flex w-full items-center gap-2 rounded-[8px] px-3 py-2 text-left text-[13px] text-ink hover:bg-surface-strong"
              onClick={() => {
                setOpen(false);
                if (c.id !== sessionId) router.push(`/platform/chat/${c.id}`);
              }}
            >
              <span className="w-3.5 shrink-0">{c.id === sessionId && <IconCheck className="h-3.5 w-3.5" />}</span>
              <span className="min-w-0 flex-1 truncate">{chatLabel(t, c.title, i)}</span>
              <span className="caption shrink-0 text-muted-soft">{i + 1}</span>
            </button>
          ))}
          <button
            type="button"
            role="menuitem"
            disabled={disabled || busy}
            className="mt-1 flex w-full items-center gap-2 rounded-[8px] px-3 py-2 text-left text-[13px] text-ink hover:bg-surface-strong disabled:opacity-50"
            onClick={() => void openNew()}
          >
            <IconPlus className="h-3.5 w-3.5" /> {busy ? t("subchat.opening") : t("subchat.new")}
          </button>
          {failed && <p className="caption px-3 pb-1 text-error">{t("subchat.failed")}</p>}
        </div>
      )}
    </div>
  );
}

/** Above the composer once the chat is long: offers a new sub-chat. */
export function SubChatBanner({ sessionId, usage, disabled, onClose, compact = false }: {
  sessionId: string;
  usage: ContextUsage;
  disabled: boolean;
  onClose: () => void;
  compact?: boolean;
}) {
  const { t } = useT();
  const { busy, failed, open } = useOpenSubChat(sessionId);
  const pct = contextPercent(usage);
  return (
    <div
      className={`${compact ? "text-[12px]" : "text-[12.5px]"} mb-2 flex flex-col gap-2 rounded-lg border border-hairline bg-surface-card px-3 py-2 text-body`}
      role="status"
    >
      <div className="flex items-start gap-2">
        <span className="flex-1">{pct >= SUB_CHAT_FILL * 100 ? t("subchat.banner", { pct }) : t("subchat.bannerCompacted")}</span>
        <button type="button" className="text-muted hover:text-ink" aria-label={t("subchat.dismiss")} onClick={onClose}>
          <IconClose className="h-3.5 w-3.5" />
        </button>
      </div>
      <span className="inline-flex items-center gap-2">
        <button type="button" className="btn btn-primary btn-sm" disabled={disabled || busy} onClick={() => void open()}>
          {busy ? t("subchat.opening") : t("subchat.new")}
        </button>
        {failed && <span className="text-error">{t("subchat.failed")}</span>}
      </span>
    </div>
  );
}
