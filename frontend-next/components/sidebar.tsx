"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useRef, useState, type CSSProperties } from "react";
import { deleteSession, listSessions, type SessionRow } from "@/lib/api/chat";
import {
  clearSessionActivity,
  refreshSessionActivity,
  updateSessionActivity,
  useSessionActivityEntries,
} from "@/lib/session-activity";
import { useT, type LocaleKey } from "@/lib/i18n";
import { useTenantRole } from "@/lib/auth";
import { useCommandPalette } from "@/components/command-palette/command-palette-context";
import { AccountMenu } from "@/components/account-menu";
import { BrandLockup } from "@/components/brand-logo";
import { Modal } from "@/components/modal";
import { SessionCompleteToasts } from "@/components/session-complete-toasts";
import {
  IconAgent,
  IconArtifact,
  IconBook,
  IconChat,
  IconChevronRight,
  IconPlus,
  IconSearch,
  IconTrash,
} from "@/components/icons";
import { setSidebarHidden, useSidebarHidden } from "@/lib/sidebar-state";
import { dayBucket, type DayBucket } from "@/lib/message-time";
import { useDialogFocus } from "@/components/use-dialog-focus";
import {
  SHELL_DESKTOP_QUERY,
  SHELL_HEADER_HEIGHT_PX,
  SIDEBAR_WIDTH_PX,
  useIsDesktopShell,
} from "@/components/shell-breakpoints";

const NAV = [
  { href: "/platform/creatChat", labelKey: "nav.newChat", icon: IconChat, match: ["/platform/creatChat", "/platform/chat"] },
  { href: "/platform/knowledge-bases", labelKey: "nav.knowledgeBases", icon: IconBook, match: ["/platform/knowledge-bases"] },
  { href: "/platform/artifacts", labelKey: "nav.artifacts", icon: IconArtifact, match: ["/platform/artifacts"], orgOnly: true },
] as const;

function isActive(pathname: string | null, match: readonly string[]) {
  if (!pathname) return false;
  return match.some((m) => pathname === m || pathname.startsWith(m + "/"));
}

/* The day buckets of the session list. The day maths is shared with the
 * chat (lib/message-time.ts); only the labels are translated here. */
const BUCKET_LABELS: Record<DayBucket, LocaleKey> = {
  today: "sidebar.today",
  yesterday: "sidebar.yesterday",
  week: "sidebar.last7Days",
  older: "sidebar.older",
};

const BUCKET_ORDER: DayBucket[] = ["today", "yesterday", "week", "older"];

export function Sidebar() {
  const pathname = usePathname();
  const router = useRouter();
  const { t } = useT();
  const palette = useCommandPalette();
  // a guest (no organization) has chat + public KBs; the artifacts library
  // needs an organization
  const { isGuest } = useTenantRole();
  const [live, setLive] = useState<SessionRow[] | null>(null);
  // Sessions with an in-flight assistant turn (this tab marked them, or they
  // were left mid-generation). Rows show a spinner; detached entries are
  // polled below until the backend finishes the turn.
  const activity = useSessionActivityEntries();
  const [removing, setRemoving] = useState<{ id: string; title: string } | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  const [mobileOpen, setMobileOpen] = useState(false);
  // hidden on desktop by a full-width page (the legal draft review); the
  // tab at the left edge brings it back from any page
  const savedSidebarHidden = useSidebarHidden();
  const [adminMenuOpen, setAdminMenuOpen] = useState(false);
  const inAdmin = pathname === "/platform/admin" || pathname.startsWith("/platform/admin/");
  const desktopHidden = inAdmin ? !adminMenuOpen : savedSidebarHidden;
  const isDesktop = useIsDesktopShell();
  const railRef = useRef<HTMLElement | null>(null);
  /* The rail is off-screen below the shell breakpoint while the drawer is
   * closed, and on desktop while a full-width page hides it. In both cases
   * it must not be reachable by keyboard or by a screen reader. */
  const railHidden = (!isDesktop && !mobileOpen) || (isDesktop && desktopHidden);

  useEffect(() => {
    setAdminMenuOpen(false);
  }, [inAdmin]);

  const confirmDelete = async () => {
    if (!removing || deleting) return;
    setDeleting(true);
    setDeleteError("");
    try {
      await deleteSession(removing.id);
      updateSessionActivity(removing.id, false);
      setLive((prev) => prev?.filter((s) => s.id !== removing.id) ?? prev);
      if (pathname === `/platform/chat/${removing.id}`) {
        router.push("/platform/creatChat");
      }
      setRemoving(null);
    } catch (e) {
      setDeleteError(e instanceof Error ? e.message : t("sidebar.deleteFailed"));
    } finally {
      setDeleting(false);
    }
  };

  // Below the shell breakpoint the sidebar is an off-canvas drawer: the
  // header's menu button dispatches this event; navigation and the
  // backdrop close it.
  useEffect(() => {
    const toggle = () => {
      if (inAdmin && window.matchMedia(SHELL_DESKTOP_QUERY).matches) {
        setAdminMenuOpen((open) => !open);
      } else {
        setMobileOpen((v) => !v);
      }
    };
    window.addEventListener("weknora:toggle-sidebar", toggle);
    return () => window.removeEventListener("weknora:toggle-sidebar", toggle);
  }, [inAdmin]);

  // Rotating the phone or resizing the window must not leave the drawer
  // open (and off-screen) — close it as soon as the shell turns desktop.
  useEffect(() => {
    const close = () => {
      if (window.matchMedia(SHELL_DESKTOP_QUERY).matches) setMobileOpen(false);
    };
    window.addEventListener("resize", close);
    window.addEventListener("orientationchange", close);
    return () => {
      window.removeEventListener("resize", close);
      window.removeEventListener("orientationchange", close);
    };
  }, []);

  /* The open drawer is a modal surface: Escape closes it and the focus
   * stays inside it while it is open. */
  useDialogFocus({
    open: mobileOpen,
    containerRef: railRef,
    onClose: () => setMobileOpen(false),
  });

  useEffect(() => setMobileOpen(false), [pathname]);

  // Poll sessions left mid-generation (detached markers) so the sidebar's
  // "reply in progress" indicator clears once the backend finishes the turn —
  // mirrors menu.vue's 5s sessionActivity.refresh() interval.
  useEffect(() => {
    const timer = window.setInterval(() => {
      void refreshSessionActivity();
    }, 5000);
    return () => {
      window.clearInterval(timer);
      clearSessionActivity();
    };
  }, []);

  // reloaded when the account kind changes: a guest's sessions live in the
  // public portal, a member's in the organization
  useEffect(() => {
    let alive = true;
    listSessions(1, 30)
      .then((res) => {
        if (alive) setLive(res.data ?? []);
      })
      .catch(() => {
        if (alive) setLive([]);
      });
    return () => {
      alive = false;
    };
  }, [isGuest]);

  useEffect(() => {
    const handleTitleUpdated = (e: Event) => {
      const customEvent = e as CustomEvent<{ sessionId?: string; id?: string; title: string }>;
      const targetId = customEvent.detail?.sessionId || customEvent.detail?.id;
      if (!targetId || !customEvent.detail?.title) return;
      setLive((prev) => {
        if (!prev) return prev;
        const exists = prev.some((item) => item.id === targetId);
        if (exists) {
          return prev.map((item) =>
            item.id === targetId ? { ...item, title: customEvent.detail.title } : item
          );
        }
        const now = new Date().toISOString();
        return [
          {
            id: targetId,
            title: customEvent.detail.title,
            created_at: now,
            updated_at: now,
          },
          ...prev,
        ];
      });
    };

    const handleSessionCreated = (e: Event) => {
      const customEvent = e as CustomEvent<{ sessionId?: string; id?: string; title?: string }>;
      const targetId = customEvent.detail?.sessionId || customEvent.detail?.id;
      if (!targetId) return;
      setLive((prev) => {
        if (!prev || prev.some((item) => item.id === targetId)) return prev;
        const now = new Date().toISOString();
        return [
          {
            id: targetId,
            title: customEvent.detail.title || t("nav.newChat"),
            created_at: now,
            updated_at: now,
          },
          ...prev,
        ];
      });
    };

    // A session made for a send whose uploads were then refused.
    const handleSessionRemoved = (e: Event) => {
      const targetId = (e as CustomEvent<{ sessionId?: string }>).detail?.sessionId;
      if (!targetId) return;
      setLive((prev) => (prev ? prev.filter((item) => item.id !== targetId) : prev));
    };

    window.addEventListener("weknora:session-title-updated", handleTitleUpdated);
    window.addEventListener("weknora:session-created", handleSessionCreated);
    window.addEventListener("weknora:session-removed", handleSessionRemoved);
    return () => {
      window.removeEventListener("weknora:session-title-updated", handleTitleUpdated);
      window.removeEventListener("weknora:session-created", handleSessionCreated);
      window.removeEventListener("weknora:session-removed", handleSessionRemoved);
    };
  }, []);

  const groups: Array<{ bucket: DayBucket; items: Array<{ id: string; title: string }> }> =
    (() => {
      const buckets: Record<DayBucket, Array<{ id: string; title: string }>> = {
        today: [],
        yesterday: [],
        week: [],
        older: [],
      };
      for (const r of live ?? []) {
        buckets[dayBucket(r.updated_at ?? r.created_at ?? "")].push({
          id: r.id,
          title: r.title || t("nav.newChat"),
        });
      }
      return BUCKET_ORDER.filter((bucket) => buckets[bucket].length > 0).map((bucket) => ({
        bucket,
        items: buckets[bucket],
      }));
    })();

  return (
    <>
      {mobileOpen && (
        <div
          /* Click-to-close layer that also dims + blurs the page behind the
           * mobile drawer, keeping focus on the sidebar. */
          className="fixed inset-0 z-40 bg-scrim/40 backdrop-blur-sm lg:hidden"
          onClick={() => setMobileOpen(false)}
          aria-hidden
        />
      )}
      {/* Desktop: the translate makes the rail its own stacking layer, so it
       * needs a z-index above the page (the ONLYOFFICE pane is z-10) or the
       * account menu, wider than the rail, opens under the editor. */}
      {desktopHidden && !inAdmin && (
        <button
          type="button"
          onClick={() => setSidebarHidden(false)}
          title={t("sidebar.show")}
          aria-label={t("sidebar.show")}
          className="fixed left-0 top-1/2 z-40 hidden h-12 w-5 -translate-y-1/2 items-center justify-center rounded-r-md border border-l-0 border-hairline bg-canvas text-muted-soft shadow-sm transition-colors hover:text-ink lg:flex"
        >
          <IconChevronRight className="h-3.5 w-3.5" />
        </button>
      )}
      <aside
        ref={railRef}
        /* Off-screen means unreachable: no keyboard focus, no screen
         * reader, no pointer events. */
        inert={railHidden}
        aria-hidden={railHidden || undefined}
        style={{ "--sidebar-width": `${SIDEBAR_WIDTH_PX}px` } as CSSProperties}
        className={`fixed inset-y-0 left-0 z-50 flex h-dvh w-[var(--sidebar-width)] shrink-0 flex-col border-r border-hairline bg-canvas transition-[margin,transform,opacity,visibility] duration-250 ease-in-out motion-reduce:transition-none lg:relative lg:z-30 ${
          mobileOpen ? "translate-x-0" : "-translate-x-full"
        } ${desktopHidden ? "lg:invisible lg:-ml-[var(--sidebar-width)] lg:-translate-x-full lg:opacity-0 lg:pointer-events-none" : "lg:visible lg:ml-0 lg:translate-x-0 lg:opacity-100"}`}
      >
      <div className="flex shrink-0 items-center px-5" style={{ height: SHELL_HEADER_HEIGHT_PX }}>
        <Link
          href="/platform/knowledge-bases"
          className="flex items-center transition-opacity hover:opacity-85"
        >
          <BrandLockup size="sm" priority />
        </Link>
      </div>

      <div className="px-4 pb-3">
        <button
          onClick={() => palette.open()}
          className="card flex w-full items-center gap-2 px-3 py-2 text-[14px] text-muted-soft transition-colors hover:border-ink hover:text-ink"
        >
          <IconSearch className="h-4 w-4" />
          <span className="flex-1 text-left">{t("nav.search")}</span>
          <kbd className="caption rounded border border-hairline px-1.5 py-0.5 text-[11px] text-muted">
            ⌘K
          </kbd>
        </button>
      </div>

      <nav className="flex flex-col gap-0.5 px-3">
        {NAV.filter((item) => !(isGuest && "orgOnly" in item && item.orgOnly)).map((item) => (
          <Link
            key={item.href}
            href={item.href}
            className={`nav-item ${isActive(pathname, item.match) ? "active" : ""}`}
          >
            <item.icon className="h-[18px] w-[18px]" />
            {t(item.labelKey)}
            {item.labelKey === "nav.newChat" && <IconPlus className="ml-auto h-4 w-4 text-muted-soft" />}
          </Link>
        ))}
      </nav>

      <div className="mt-5 flex-1 overflow-y-auto px-3">
        {groups.map((g) => (
          <div key={g.bucket} className="mb-4">
            <div className="caption-uppercase px-3 pb-1.5 text-muted-soft">
              {t(BUCKET_LABELS[g.bucket])}
            </div>
            {g.items.map((s) => (
              <Link
                key={s.id}
                href={`/platform/chat/${s.id}`}
                className={`nav-item group font-normal ${
                  pathname === `/platform/chat/${s.id}` ? "active" : ""
                }`}
              >
                <span className="truncate">{s.title}</span>
                {activity.has(s.id) && (
                  <span
                    role="status"
                    aria-label={t("nav.sessionInProgress")}
                    title={t("nav.sessionInProgress")}
                    className="ml-auto h-3 w-3 shrink-0 animate-spin rounded-full border-[1.5px] border-muted-soft border-t-ink motion-reduce:animate-none"
                  />
                )}
                <button
                  type="button"
                  title={t("sidebar.deleteChat")}
                  aria-label={t("sidebar.deleteChat")}
                  className={`${activity.has(s.id) ? "" : "ml-auto "}hidden h-5 w-5 shrink-0 items-center justify-center rounded text-muted-soft transition-colors hover:text-error group-hover:flex`}
                  onClick={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    setDeleteError("");
                    setRemoving({ id: s.id, title: s.title });
                  }}
                >
                  <IconTrash className="h-3.5 w-3.5" />
                </button>
              </Link>
            ))}
          </div>
        ))}
      </div>

      <div className="border-t border-hairline px-3 py-3">
        <AccountMenu />
      </div>

      </aside>

      <SessionCompleteToasts
        titles={new Map<string, string>((live ?? []).map((s) => [s.id, s.title ?? ""]))}
      />

      {/* Rendered outside <aside>: the drawer's transform would make the modal's
          fixed positioning resolve against the sidebar instead of the viewport. */}
      <Modal
        open={removing !== null}
        title={t("sidebar.deleteChat")}
        onClose={() => setRemoving(null)}
        width="w-[420px]"
      >
        <p className="body-sm text-body">
          {t("sidebar.deleteChatConfirm", { title: removing?.title ?? "" })}
        </p>
        {deleteError && <p className="caption mt-3 text-error">{deleteError}</p>}
        <div className="mt-4 flex justify-end gap-2">
          <button className="btn btn-outline btn-sm" onClick={() => setRemoving(null)}>
            {t("common.cancel")}
          </button>
          <button
            className="btn btn-danger btn-sm"
            disabled={deleting}
            onClick={() => void confirmDelete()}
          >
            {deleting ? t("sidebar.deleting") : t("common.delete")}
          </button>
        </div>
      </Modal>
    </>
  );
}
