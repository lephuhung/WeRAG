"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";
import { listSessions, type SessionRow } from "@/lib/api/chat";
import { getCurrentUser } from "@/lib/api/auth";
import { useT } from "@/lib/i18n";
import {
  IconAgent,
  IconArtifact,
  IconBook,
  IconChat,
  IconOrg,
  IconPlus,
  IconPulse,
  IconSearch,
  IconSettings,
} from "@/components/icons";

const NAV = [
  { href: "/platform/creatChat", labelKey: "nav.newChat", icon: IconChat, match: ["/platform/creatChat", "/platform/chat"] },
  { href: "/platform/knowledge-bases", labelKey: "nav.knowledgeBases", icon: IconBook, match: ["/platform/knowledge-bases"] },
  { href: "/platform/artifacts", labelKey: "nav.artifacts", icon: IconArtifact, match: ["/platform/artifacts"] },
  { href: "/platform/agents", labelKey: "nav.agents", icon: IconAgent, match: ["/platform/agents"] },
  { href: "/platform/organizations", labelKey: "nav.organizations", icon: IconOrg, match: ["/platform/organizations"] },
] as const;

function isActive(pathname: string, match: readonly string[]) {
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
  const { t } = useT();
  const [live, setLive] = useState<SessionRow[] | null>(null);
  const [isSystemAdmin, setIsSystemAdmin] = useState(false);

  useEffect(() => {
    let alive = true;
    getCurrentUser()
      .then((res) => {
        if (alive) setIsSystemAdmin(res.data?.user?.is_system_admin === true);
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);

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
  }, [pathname]);

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
    <aside className="flex h-screen w-[264px] shrink-0 flex-col border-r border-hairline bg-canvas">
      <div className="flex h-16 items-center px-5">
        <Link href="/platform/knowledge-bases" className="display-sm tracking-tight">
          WeRAG
        </Link>
      </div>

      <div className="px-4 pb-3">
        <button className="card flex w-full items-center gap-2 px-3 py-2 text-[14px] text-muted-soft">
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
                className={`nav-item font-normal ${
                  pathname === `/platform/chat/${s.id}` ? "active" : ""
                }`}
              >
                <span className="truncate">{s.title}</span>
              </Link>
            ))}
          </div>
        ))}
      </div>

      <div className="border-t border-hairline px-3 py-3">
        {isSystemAdmin && (
          <Link
            href="/platform/system"
            className={`nav-item ${pathname.startsWith("/platform/system") ? "active" : ""}`}
          >
            <IconPulse className="h-[18px] w-[18px]" />
            {t("nav.system")}
            <span className="ml-auto rounded-full border border-hairline-strong px-1.5 py-px text-[10px] font-semibold uppercase tracking-[0.6px] text-muted">
              {t("nav.admin")}
            </span>
          </Link>
        )}
        <Link
          href="/platform/settings"
          className={`nav-item ${pathname.startsWith("/platform/settings") ? "active" : ""}`}
        >
          <IconSettings className="h-[18px] w-[18px]" />
          {t("nav.settings")}
        </Link>
      </div>
    </aside>
  );
}
