"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { IconLogout, IconSettings } from "@/components/icons";
import { UserSettingsModal } from "@/components/user-settings-modal";
import { clearTokens } from "@/lib/api-client";
import { useT, type Locale, type LocaleKey } from "@/lib/i18n";
const SECTION_KEYS: [RegExp, LocaleKey][] = [
  [/^\/platform\/chat/, "nav.chat"],
  [/^\/platform\/creatChat/, "nav.newChat"],
  [/^\/platform\/knowledge-bases/, "nav.knowledgeBases"],
  [/^\/platform\/artifacts/, "nav.artifacts"],
  [/^\/platform\/agents/, "nav.agents"],
  [/^\/platform\/organizations/, "nav.organizations"],
  [/^\/platform\/system/, "nav.system"],
  [/^\/platform\/settings/, "nav.settings"],
];

const LOCALES: { id: Locale; label: string }[] = [
  { id: "en", label: "EN" },
  { id: "vi", label: "VI" },
  { id: "zh", label: "ZH" },
];

export function Header() {
  const pathname = usePathname();
  const router = useRouter();
  const { t, locale, setLocale } = useT();
  const [open, setOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  const sectionLabel =
    SECTION_KEYS.find(([re]) => re.test(pathname))?.[1] ?? "hdr.workspace";

  useEffect(() => setOpen(false), [pathname]);
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);

  const signOut = async () => {
    try {
      const token = localStorage.getItem("weknora_token");
      if (token) {
        await fetch("/api/v1/auth/logout", {
          method: "POST",
          headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
          body: "{}",
        });
      }
    } catch {
      /* best effort */
    }
    clearTokens();
    router.push("/login");
  };

  return (
    <header className="relative flex h-14 shrink-0 items-center justify-between border-b border-hairline bg-canvas px-8">
      <span className="caption text-muted">{t(sectionLabel)}</span>

      <button
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="dialog"
        aria-expanded={open}
        className="flex h-9 w-9 items-center justify-center rounded-full bg-surface-strong text-[13px] font-medium text-ink transition-shadow hover:shadow-[0_0_0_2px_var(--color-hairline-strong)]"
      >
        WR
      </button>

      {open && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
          <div
            ref={menuRef}
            role="dialog"
            aria-label="Account"
            className="card absolute right-6 top-[50px] z-50 w-[280px] p-2 shadow-[0_4px_16px_rgba(0,0,0,0.04)]"
          >
            <button
              className="flex w-full items-center gap-3 rounded-[8px] px-3 py-3 text-left transition-colors hover:bg-surface-strong"
              onClick={() => {
                setOpen(false);
                setSettingsOpen(true);
              }}
            >
              <div className="flex h-10 w-10 items-center justify-center rounded-full bg-surface-strong text-[14px] font-medium text-ink">
                WR
              </div>
              <div className="min-w-0">
                <div className="truncate text-[15px] font-medium text-ink">werag admin</div>
                <div className="caption truncate text-muted">admin@werag.local</div>
              </div>
            </button>
            <div className="my-1 border-t border-hairline" />
            <button
              className="nav-item"
              onClick={() => {
                setOpen(false);
                setSettingsOpen(true);
              }}
            >
              <IconSettings className="h-[18px] w-[18px]" />
              {t("user.settings")}
            </button>
            <Link href="/design" className="nav-item">
              <span className="display-sm flex h-[18px] w-[18px] items-center justify-center text-[13px]">
                Aa
              </span>
              {t("user.designSystem")}
            </Link>
            <div className="my-1 border-t border-hairline" />

            <div className="flex items-center justify-between px-3 py-2">
              <span className="text-[13px] text-muted">{t("user.language")}</span>
              <div className="flex items-center gap-1 rounded-full bg-surface-strong p-0.5">
                {LOCALES.map((l) => (
                  <button
                    key={l.id}
                    onClick={() => setLocale(l.id)}
                    className={`rounded-full px-2.5 py-1 text-[12px] font-medium transition-colors ${
                      locale === l.id
                        ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.06)]"
                        : "text-muted hover:text-ink"
                    }`}
                  >
                    {l.label}
                  </button>
                ))}
              </div>
            </div>

            <div className="my-1 border-t border-hairline" />
            <button className="nav-item w-full text-left" onClick={() => void signOut()}>
              <IconLogout className="h-[18px] w-[18px]" />
              {t("user.signOut")}
            </button>
          </div>
        </>
      )}

      <UserSettingsModal open={settingsOpen} onClose={() => setSettingsOpen(false)} />
    </header>
  );
}
