"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { deleteSession, listSessions, type SessionRow } from "@/lib/api/chat";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { useCommandPalette } from "@/components/command-palette/command-palette-context";
import { BrandLogo } from "@/components/brand-logo";
import { Modal } from "@/components/modal";
import {
  IconAgent,
  IconArtifact,
  IconBook,
  IconChat,
  IconOrg,
  IconPlus,
  IconSearch,
  IconSettings,
  IconTrash,
} from "@/components/icons";

const NAV = [
  { href: "/platform/creatChat", labelKey: "nav.newChat", icon: IconChat, match: ["/platform/creatChat", "/platform/chat"] },
  { href: "/platform/knowledge-bases", labelKey: "nav.knowledgeBases", icon: IconBook, match: ["/platform/knowledge-bases"] },
  { href: "/platform/artifacts", labelKey: "nav.artifacts", icon: IconArtifact, match: ["/platform/artifacts"] },
  { href: "/platform/organizations", labelKey: "nav.organizations", icon: IconOrg, match: ["/platform/organizations"] },
] as const;

function isActive(pathname: string | null, match: readonly string[]) {
  if (!pathname) return false;
  return match.some((m) => pathname === m || pathname.startsWith(m + "/"));
}

function bucketOf(row: SessionRow): string {
  const ts = row.updated_at ?? row.created_at ?? "";
  const d = ts ? new Date(ts) : null;
  if (!d || Number.isNaN(d.getTime())) return "Older";
  const now = new Date();
  const day = (a: Date) => a.getFullYear() * 1000 + Math.floor(a.getMonth() * 30 + a.getDate());
  const diff = day(now) - day(d);
  if (diff <= 0) return "Today";
  if (diff <= 1) return "Yesterday";
  if (diff <= 7) return "Last 7 days";
  return "Older";
}

export function Sidebar() {
  const pathname = usePathname();
  const router = useRouter();
  const { t } = useT();
  const auth = useAuth();
  const palette = useCommandPalette();
  const [live, setLive] = useState<SessionRow[] | null>(null);
  const [removing, setRemoving] = useState<{ id: string; title: string } | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  const [mobileOpen, setMobileOpen] = useState(false);

  const confirmDelete = async () => {
    if (!removing || deleting) return;
    setDeleting(true);
    setDeleteError("");
    try {
      await deleteSession(removing.id);
      setLive((prev) => prev?.filter((s) => s.id !== removing.id) ?? prev);
      if (pathname === `/platform/chat/${removing.id}`) {
        router.push("/platform/creatChat");
      }
      setRemoving(null);
    } catch (e) {
      setDeleteError(e instanceof Error ? e.message : "Delete failed");
    } finally {
      setDeleting(false);
    }
  };

  // Below lg the sidebar is an off-canvas drawer: the header's menu button
  // dispatches this event; navigation and the backdrop close it.
  useEffect(() => {
    const toggle = () => setMobileOpen((v) => !v);
    window.addEventListener("weknora:toggle-sidebar", toggle);
    return () => window.removeEventListener("weknora:toggle-sidebar", toggle);
  }, []);

  useEffect(() => setMobileOpen(false), [pathname]);

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
  }, []);

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
            title: customEvent.detail?.title || "New chat",
            created_at: now,
            updated_at: now,
          },
          ...prev,
        ];
      });
    };

    window.addEventListener("weknora:session-title-updated", handleTitleUpdated);
    window.addEventListener("weknora:session-created", handleSessionCreated);
    return () => {
      window.removeEventListener("weknora:session-title-updated", handleTitleUpdated);
      window.removeEventListener("weknora:session-created", handleSessionCreated);
    };
  }, []);

  const groups: Array<{ bucket: string; items: Array<{ id: string; title: string }> }> =
    (() => {
      const buckets: Record<string, Array<{ id: string; title: string }>> = {};
      const order: string[] = [];
      for (const r of live ?? []) {
        const b = bucketOf(r);
        if (!buckets[b]) {
          buckets[b] = [];
          order.push(b);
        }
        buckets[b].push({ id: r.id, title: r.title || "New chat" });
      }
      return order.map((bucket) => ({ bucket, items: buckets[bucket] }));
    })();

  return (
    <>
      {mobileOpen && (
        <div
          className="fixed inset-0 z-40 bg-ink/20 lg:hidden"
          onClick={() => setMobileOpen(false)}
          aria-hidden
        />
      )}
      <aside
        className={`fixed inset-y-0 left-0 z-50 flex h-dvh w-[264px] shrink-0 flex-col border-r border-hairline bg-canvas transition-transform duration-200 ease-out lg:static lg:z-auto lg:translate-x-0 lg:transition-none ${
          mobileOpen ? "translate-x-0" : "-translate-x-full"
        }`}
      >
      <div className="flex h-16 items-center px-5">
        <Link
          href="/platform/knowledge-bases"
          className="flex items-center gap-2.5 transition-opacity hover:opacity-85"
        >
          <BrandLogo size={32} priority />
          <span className="display-sm tracking-tight text-ink">WeRAG</span>
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
        {NAV.map((item) => (
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
            <div className="caption-uppercase px-3 pb-1.5 text-muted-soft">{g.bucket}</div>
            {g.items.map((s) => (
              <Link
                key={s.id}
                href={`/platform/chat/${s.id}`}
                className={`nav-item group font-normal ${
                  pathname === `/platform/chat/${s.id}` ? "active" : ""
                }`}
              >
                <span className="truncate">{s.title}</span>
                <button
                  type="button"
                  title="Delete chat"
                  aria-label="Delete chat"
                  className="ml-auto hidden h-5 w-5 shrink-0 items-center justify-center rounded text-muted-soft transition-colors hover:text-error group-hover:flex"
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
        <Link
          href={
            auth.ready
              ? auth.user?.is_system_admin
                ? "/platform/system/admin"
                : "/platform/system/workspace"
              : "/platform/system"
          }
          className={`nav-item ${pathname?.startsWith("/platform/system") || pathname?.startsWith("/platform/settings") ? "active" : ""}`}
        >
          <IconSettings className="h-[18px] w-[18px]" />
          {t("nav.settings")}
        </Link>
      </div>

      </aside>

      {/* Rendered outside <aside>: the drawer's transform would make the modal's
          fixed positioning resolve against the sidebar instead of the viewport. */}
      <Modal
        open={removing !== null}
        title="Delete chat"
        onClose={() => setRemoving(null)}
        width="w-[420px]"
      >
        <p className="body-sm text-body">
          Delete &quot;{removing?.title}&quot;? This conversation will be permanently removed.
        </p>
        {deleteError && <p className="caption mt-3 text-error">{deleteError}</p>}
        <div className="mt-4 flex justify-end gap-2">
          <button className="btn btn-outline btn-sm" onClick={() => setRemoving(null)}>
            Cancel
          </button>
          <button
            className="btn btn-sm bg-[var(--color-error)] text-white"
            disabled={deleting}
            onClick={() => void confirmDelete()}
          >
            {deleting ? "Deleting…" : "Delete"}
          </button>
        </div>
      </Modal>
    </>
  );
}
