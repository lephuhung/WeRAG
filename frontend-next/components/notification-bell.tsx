"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { ClickAwayLayer } from "@/components/click-away-layer";
import { useToast } from "@/components/toast";
import { useAuth, useTenantRole } from "@/lib/auth";
import { formatDateTime } from "@/lib/datetime";
import { useT, type LocaleKey } from "@/lib/i18n";
import {
  getUnreadNotificationCount,
  listNotifications,
  markAllNotificationsRead,
  markNotificationRead,
  markNotificationsSeen,
  type UserNotification,
} from "@/lib/api/notifications";
import {
  NOTIFICATION_OPENED_EVENT,
  NOTIFICATION_POLL_MS,
  badgeLabel,
  mergeFresh,
  notificationText,
  resolveTarget,
  shouldPoll,
  toastPlan,
  toastKey,
  toastable,
  unseenIds,
  type NotificationTexts,
} from "@/lib/notifications";

/* The header bell: unread badge, a dropdown of the person's notifications
 * (newest first) and a toast once for each new one while the app is open
 * (at most three, the rest summed up). Polls the unread count every minute
 * and when the window comes back into view (not within 10 s of the last
 * poll); a Guest in the public portal shares the same shell and sees it too.
 * Known kinds are worded from their payload in the reader's language. */
export function NotificationBell() {
  const { t, locale } = useT();
  const { isLoggedIn, tenant, selectedTenantId, memberships } = useAuth();
  const { isGuest } = useTenantRole();
  const toast = useToast();
  const router = useRouter();
  const [unread, setUnread] = useState(0);
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<UserNotification[] | null>(null);
  const [loading, setLoading] = useState(false);
  /** toastKey of every version toasted in this tab */
  const toasted = useRef<Set<string>>(new Set());
  const polling = useRef(false);
  const lastPoll = useRef(0);

  const texts = useMemo<NotificationTexts>(
    () => ({
      legalAddedTitle: (doc) => t("notif.legalAdded.title", { doc }),
      legalAddedChat: t("notif.legalAdded.chat"),
      legalAddedWord: t("notif.legalAdded.word"),
      legalAddedWeb: t("notif.legalAdded.web"),
      legalAddedLookup: t("notif.legalAdded.lookup"),
      legalAddedOtherOrg: (org) => (org ? t("notif.legalAdded.otherOrg", { org }) : t("notif.legalAdded.otherOrgUnknown")),
      abbreviationPendingTitle: (n) => t("notif.abbrevPending.title", { n }),
      abbreviationProperName: (short) => t("notif.abbrevPending.properName", { short }),
      abbreviationPendingMore: (n) => t("notif.abbrevPending.more", { n }),
      queueTitle: (kind, n) => t(`notif.queue.${kind}.title` as LocaleKey, { n }),
      queueMore: (kind, n) => t(`notif.queue.${kind}.more` as LocaleKey, { n }),
      queueRequestCount: (n) => t("notif.queue.requestCount", { n }),
      queueLinkCount: (n) => t("notif.queue.linkCount", { n }),
      money: (vnd) => (locale === "vi" ? `${vnd.toLocaleString("vi-VN")} đ` : `${vnd.toLocaleString("en-US")} VND`),
      queueUntitled: t("notif.queue.untitled"),
      backupAlertTitle: (n) => t("notif.backup.title", { n }),
      backupProblem: (problem, at) =>
        problem === "failed" && at ? t("notif.backup.failedAt", { at }) : t(`notif.backup.${problem}` as LocaleKey),
      backupLastOk: (at) => t("notif.backup.lastOk", { at }),
      dateTime: (iso) => formatDateTime(iso, locale),
    }),
    [t, locale],
  );

  // where a click on n goes: never into another organization (the web app
  // has no organization switcher), the legal lookup instead
  const targetOf = useCallback(
    (n: UserNotification) =>
      resolveTarget(n, {
        origin: window.location.origin,
        currentTenantId: Number(selectedTenantId ?? tenant?.id ?? 0) || null,
        memberships,
        isGuest,
      }),
    [isGuest, memberships, selectedTenantId, tenant?.id],
  );

  const go = useCallback(
    (n: UserNotification) => {
      if (!n.read_at) {
        void markNotificationRead(n.id).catch(() => {});
        setItems((prev) => prev?.map((x) => (x.id === n.id ? { ...x, read_at: new Date().toISOString() } : x)) ?? prev);
        setUnread((u) => Math.max(0, u - 1));
      }
      setOpen(false);
      const target = targetOf(n);
      if (target.path) {
        router.push(target.path);
        // a page already showing this link re-applies its query
        window.dispatchEvent(new CustomEvent(NOTIFICATION_OPENED_EVENT, { detail: target.path }));
      }
    },
    [router, targetOf],
  );

  const openList = useCallback(async () => {
    setOpen(true);
    setLoading(true);
    try {
      const res = await listNotifications({ limit: 30 });
      const list = res.data?.items ?? [];
      setItems(list);
      const unseen = unseenIds(list);
      if (unseen.length > 0) {
        list.filter((n) => !n.seen_at).forEach((n) => toasted.current.add(toastKey(n)));
        void markNotificationsSeen(unseen).catch(() => {});
      }
    } catch {
      setItems((prev) => prev ?? []);
    } finally {
      setLoading(false);
    }
  }, []);

  const showToasts = useCallback(async () => {
    const res = await listNotifications({ unread: true, limit: 20 });
    const list = res.data?.items ?? [];
    // an updated digest shows its new count in a list already fetched
    setItems((prev) => mergeFresh(prev, list));
    const { show, more } = toastPlan(toastable(list, toasted.current));
    for (const n of show) {
      toasted.current.add(toastKey(n));
      const { title, body } = notificationText(n, texts, targetOf(n));
      toast.info(body ? `${title} — ${body}` : title, () => go(n), 8000);
    }
    if (more > 0) {
      toastable(list, toasted.current).forEach((n) => toasted.current.add(toastKey(n)));
      toast.info(t("notif.more", { n: more }), () => void openList(), 8000);
    }
    // every unseen one fetched, so a seen call that failed before is retried
    const unseen = unseenIds(list);
    if (unseen.length > 0) await markNotificationsSeen(unseen);
  }, [go, openList, t, targetOf, texts, toast]);

  const poll = useCallback(
    async (fromTimer: boolean) => {
      if (polling.current || document.visibilityState === "hidden") return;
      if (!shouldPoll(lastPoll.current, Date.now(), fromTimer)) return;
      polling.current = true;
      lastPoll.current = Date.now();
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
    },
    [showToasts],
  );

  useEffect(() => {
    if (!isLoggedIn) return;
    void poll(true);
    const timer = window.setInterval(() => void poll(true), NOTIFICATION_POLL_MS);
    const onVisible = () => {
      if (document.visibilityState === "visible") void poll(false);
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
    if (open) {
      setOpen(false);
      return;
    }
    await openList();
  }, [open, openList]);

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
                  {items.map((n) => {
                    const text = notificationText(n, texts, targetOf(n));
                    return (
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
                            {text.title}
                          </span>
                          {text.body && <span className="caption mt-0.5 block line-clamp-3 text-muted">{text.body}</span>}
                          <span className="caption mt-1 block text-muted-soft">{formatDateTime(n.created_at, locale)}</span>
                        </span>
                      </button>
                    </li>
                    );
                  })}
                </ul>
              )}
            </div>
          </div>
        </>
      )}
    </div>
  );
}
