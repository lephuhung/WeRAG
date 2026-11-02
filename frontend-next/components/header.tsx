"use client";

import { usePathname } from "next/navigation";
import { useT, type LocaleKey } from "@/lib/i18n";
import { SHELL_HEADER_HEIGHT_PX } from "@/components/shell-breakpoints";

const SECTION_KEYS: [RegExp, LocaleKey][] = [
  [/^\/platform\/chat/, "nav.chat"],
  [/^\/platform\/creatChat/, "nav.newChat"],
  [/^\/platform\/knowledge-bases/, "nav.knowledgeBases"],
  [/^\/platform\/artifacts/, "nav.artifacts"],
  [/^\/platform\/agents/, "nav.agents"],
  [/^\/platform\/system/, "nav.settings"],
  [/^\/platform\/settings/, "nav.settings"],
];

/* Account/settings/sign-out live in the sidebar's AccountMenu popover — the
 * header only carries the mobile nav toggle and the section breadcrumb. */
export function Header() {
  const pathname = usePathname();
  const { t } = useT();
  const inAdmin = pathname === "/platform/admin" || pathname.startsWith("/platform/admin/");

  const sectionLabel =
    SECTION_KEYS.find(([re]) => re.test(pathname))?.[1] ?? "hdr.workspace";

  return (
    <header
      className="relative flex shrink-0 items-center gap-3 border-b border-hairline bg-canvas px-4 sm:px-8"
      style={{ height: SHELL_HEADER_HEIGHT_PX }}
    >
      <button
        onClick={() => window.dispatchEvent(new Event("weknora:toggle-sidebar"))}
        aria-label={t("header.toggleNav")}
        title={t("header.toggleNav")}
        className={`-ml-1 flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-strong hover:text-ink ${inAdmin ? "" : "lg:hidden"}`}
      >
        <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
          <path d="M4 7h16M4 12h16M4 17h16" />
        </svg>
      </button>
      <span className="caption truncate text-muted">{t(sectionLabel)}</span>
    </header>
  );
}
