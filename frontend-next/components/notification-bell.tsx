"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { ClickAwayLayer } from "@/components/click-away-layer";
import { useToast } from "@/components/toast";
import { useAuth } from "@/lib/auth";
import { formatDateTime } from "@/lib/datetime";
import { useT } from "@/lib/i18n";
import {
  getUnreadNotificationCount,
  listNotifications,
  markAllNotificationsRead,
  markNotificationRead,
  markNotificationsSeen,
  type UserNotification,
} from "@/lib/api/notifications";
import { NOTIFICATION_POLL_MS, badgeLabel, safeAppLink, toastable } from "@/lib/notifications";

/* The header bell: unread badge, a dropdown of the person's notifications
 * (newest first) and a toast once for each new one while the app is open.
 * Polls the unread count every minute and when the window comes back into
 * view; a Guest in the public portal shares the same shell and sees it too. */
export function NotificationBell() {
  const { t, locale } = useT();
  const { isLoggedIn } = useAuth();
  const toast = useToast();
  const router = useRouter();
  const [unread, setUnread] = useState(0);
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<UserNotification[] | null>(null);
  const [loading, setLoading] = useState(false);
  const toasted = useRef<Set<string>>(new Set());
  const polling = useRef(false);

  const go = useCallback(
    (n: UserNotification) => {
      if (!n.read_at) {
        void markNotificationRead(n.id).catch(() => {});
        setItems((prev) => prev?.map((x) => (x.id === n.id ? { ...x, read_at: new Date().toISOString() } : x)) ?? prev);
        setUnread((u) => Math.max(0, u - 1));
      }
      setOpen(false);
      const link = safeAppLink(n.link);
      if (link) router.push(link);
    },
    [router],
  );

  const showToasts = useCallback(async () => {
    const res = await listNotifications({ unread: true, limit: 20 });
    const fresh = toastable(res.data?.items ?? [], toasted.current);
    if (fresh.length === 0) return;
    for (const n of fresh) {
      toasted.current.add(n.id);
      toast.info(n.title, () => go(n), 8000);
    }
    await markNotificationsSeen(fresh.map((n) => n.id));
  }, [go, toast]);

  const poll = useCallback(async () => {
    if (polling.current || document.visibilityState === "hidden") return;
    polling.current = true;
    try {
      const res = await getUnreadNotificationCount();
      const data = res.data;
      if (!data) return;
      setUnread(data.unread);
      if (data.unseen > 0) await showToasts();
    } catch {
      /* offline or signed out: the next poll tries again */
    } finally {
      polling.current = false;
    }
  }, [showToasts]);

  useEffect(() => {
    if (!isLoggedIn) return;
    void poll();
    const timer = window.setInterval(() => void poll(), NOTIFICATION_POLL_MS);
    const onVisible = () => {
      if (document.visibilityState === "visible") void poll();
    };
    window.addEventListener("focus", onVisible);
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("focus", onVisible);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [isLoggedIn, poll]);

  const toggle = useCallback(async () => {
    const next = !open;
    setOpen(next);
    if (!next) return;
    setLoading(true);
    try {
      const res = await listNotifications({ limit: 30 });
      const list = res.data?.items ?? [];
      setItems(list);
      const unseen = list.filter((n) => !n.seen_at).map((n) => n.id);
      if (unseen.length > 0) {
        unseen.forEach((id) => toasted.current.add(id));
        void markNotificationsSeen(unseen).catch(() => {});
      }
    } catch {
      setItems((prev) => prev ?? []);
    } finally {
      setLoading(false);
    }
  }, [open]);

  const readAll = useCallback(async () => {
    try {
      await markAllNotificationsRead();
      const now = new Date().toISOString();
      setItems((prev) => prev?.map((x) => (x.read_at ? x : { ...x, read_at: now })) ?? prev);
      setUnread(0);
    } catch {
      toast.error(t("notif.readAllFailed"));
    }
  }, [t, toast]);

  if (!isLoggedIn) return null;
  const badge = badgeLabel(unread);

  return (
    <div className="relative shrink-0">
      <button
        type="button"
        onClick={() => void toggle()}
        aria-label={unread > 0 ? t("notif.bellUnread", { n: unread }) : t("notif.bell")}
        title={t("notif.bell")}
        aria-expanded={open}
        className="relative flex h-9 w-9 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-strong hover:text-ink"
      >
        <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
          <path d="M6 8a6 6 0 1 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
          <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
        </svg>
        {badge && (
          <span className="absolute right-1 top-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-red-500 px-1 text-[10px] font-semibold leading-none text-white">
            {badge}
          </span>
        )}
      </button>
      {open && (
        <>
          <ClickAwayLayer onClose={() => setOpen(false)} />
          <div className="card absolute right-0 top-11 z-50 flex max-h-[70vh] w-[min(380px,calc(100vw-2rem))] flex-col overflow-hidden p-0 shadow-lg">
            <div className="flex items-center justify-between gap-2 border-b border-hairline px-4 py-2.5">
              <span className="text-[13px] font-semibold text-ink">{t("notif.title")}</span>
              {unread > 0 && (
                <button type="button" onClick={() => void readAll()} className="caption text-accent hover:underline">
                  {t("notif.readAll")}
                </button>
              )}
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto">
              {loading && !items ? (
                <p className="caption px-4 py-6 text-center text-muted">{t("notif.loading")}</p>
              ) : !items || items.length === 0 ? (
                <p className="caption px-4 py-6 text-center text-muted">{t("notif.empty")}</p>
              ) : (
                <ul>
                  {items.map((n) => (
                    <li key={n.id}>
                      <button
                        type="button"
                        onClick={() => go(n)}
                        className={`flex w-full items-start gap-2.5 border-b border-hairline px-4 py-3 text-left transition-colors last:border-b-0 hover:bg-surface-strong ${
                          n.read_at ? "" : "bg-surface"
                        }`}
                      >
                        <span
                          className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${n.read_at ? "bg-transparent" : "bg-sky-500"}`}
                          aria-label={n.read_at ? undefined : t("notif.unread")}
                        />
                        <span className="min-w-0 flex-1">
                          <span className={`block text-[13px] leading-snug ${n.read_at ? "text-body" : "font-semibold text-ink"}`}>
                            {n.title}
                          </span>
                          {n.body && <span className="caption mt-0.5 block line-clamp-3 text-muted">{n.body}</span>}
                          <span className="caption mt-1 block text-muted-soft">{formatDateTime(n.created_at, locale)}</span>
                        </span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </div>
        </>
      )}
    </div>
  );
}
