/* Bottom-of-sidebar account button (replaces the old top-right header menu):
 * avatar + name + role, opening an upward popover with the pending-
 * abbreviations count (approvers only — so suggestions can't go unnoticed
 * inside deep settings), Settings (opens SettingsModal), the EN/VI pill
 * switch and Sign out. */
"use client";

import { usePathname, useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";
import { listAbbreviations } from "@/lib/api/abbreviations";
import { useAuth, useTenantRole } from "@/lib/auth";
import { useAvatarUrl } from "@/lib/avatar";
import { useT, type Locale } from "@/lib/i18n";
import { SettingsModal } from "@/components/settings-modal";
import {
  IconBook,
  IconChevronRight,
  IconChevronUp,
  IconLogout,
  IconSettings,
} from "@/components/icons";

const LOCALES: { id: Locale; label: string }[] = [
  { id: "en", label: "EN" },
  { id: "vi", label: "VI" },
];

const PENDING_POLL_MS = 60_000;

export function AccountMenu() {
  const router = useRouter();
  const pathname = usePathname();
  const { t, locale, setLocale } = useT();
  const auth = useAuth();
  const { role, isSystemAdmin, isTenantAdmin } = useTenantRole();
  const [open, setOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsSection, setSettingsSection] = useState("general");
  const [pending, setPending] = useState(0);
  const rootRef = useRef<HTMLDivElement>(null);

  const refreshPending = useCallback(async () => {
    try {
      const res = await listAbbreviations({ isActive: false, page: 1, pageSize: 1 });
      setPending(res.total ?? 0);
    } catch {
      /* non-fatal — badge just stays stale */
    }
  }, []);

  /* Pending count: on mount, every minute, and whenever the abbreviations
   * manager mutates (it dispatches weknora:abbreviations-changed). */
  useEffect(() => {
    if (!isTenantAdmin) return;
    void refreshPending();
    const timer = window.setInterval(() => void refreshPending(), PENDING_POLL_MS);
    const onChanged = () => void refreshPending();
    window.addEventListener("weknora:abbreviations-changed", onChanged);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("weknora:abbreviations-changed", onChanged);
    };
  }, [isTenantAdmin, refreshPending]);

  useEffect(() => setOpen(false), [pathname]);
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    /* Outside-click: a fixed backdrop won't work here — the sidebar's
     * transform makes `position: fixed` resolve against the <aside>, not
     * the viewport, so the overlay would only cover the 264px rail. */
    const onDown = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    window.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onDown);
    return () => {
      window.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onDown);
    };
  }, [open]);

  const openSettings = (section: string) => {
    setOpen(false);
    setSettingsSection(section);
    setSettingsOpen(true);
  };

  const signOut = async () => {
    await auth.logout();
    router.push("/login");
  };

  const name = auth.user?.username ?? "";
  const initials = (name || "?").slice(0, 2).toUpperCase();
  const avatarUrl = useAvatarUrl(auth.user?.avatar);
  const roleLabel = isSystemAdmin
    ? t("acct.roleSystem")
    : role === "admin" || role === "owner"
      ? t("acct.roleAdmin")
      : t("acct.roleMember");

  return (
    <div className="relative" ref={rootRef}>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="dialog"
        aria-expanded={open}
        className="flex w-full items-center gap-2 rounded-[10px] px-2 py-1.5 text-left transition-colors hover:bg-surface-strong"
      >
        <span className="flex h-7 w-7 shrink-0 items-center justify-center overflow-hidden rounded-full bg-gradient-to-br from-sky-500 to-indigo-500 text-[11px] font-semibold text-white">
          {avatarUrl ? (
            // eslint-disable-next-line @next/next/no-img-element -- authenticated blob URL, not a Next asset
            <img src={avatarUrl} alt="" className="h-7 w-7 object-cover" />
          ) : (
            initials
          )}
        </span>
        <span className="min-w-0 flex-1 truncate text-[13.5px] font-medium text-ink">
          {name || "—"}
        </span>
        {isTenantAdmin && pending > 0 && (
          <span className="badge-pill shrink-0 border border-amber-500/20 bg-amber-500/10 text-amber-600">
            {pending}
          </span>
        )}
        <IconChevronUp className="h-4 w-4 shrink-0 text-muted-soft" />
      </button>

      {open && (
        <div
          role="dialog"
          aria-label={name || t("user.settings")}
          className="acct-menu card absolute bottom-full left-0 z-50 mb-2 w-[272px] max-w-[calc(100vw-1.5rem)] p-1.5 shadow-[0_4px_16px_rgba(0,0,0,0.04)]"
        >
          {/* account card */}
          <div className="flex items-center gap-3 px-2.5 py-2.5">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center overflow-hidden rounded-full bg-gradient-to-br from-sky-500 to-indigo-500 text-[13px] font-semibold text-white">
              {avatarUrl ? (
                // eslint-disable-next-line @next/next/no-img-element -- authenticated blob URL, not a Next asset
                <img src={avatarUrl} alt="" className="h-9 w-9 object-cover" />
              ) : (
                initials
              )}
            </span>
            <div className="min-w-0">
              <div className="truncate text-[14px] font-medium text-ink">{name}</div>
              <div className="caption truncate text-muted">{roleLabel}</div>
            </div>
          </div>
          <div className="my-1 border-t border-hairline" />

          {isTenantAdmin && (
            <button
              type="button"
              className="nav-item"
              onClick={() => openSettings("abbreviations")}
            >
              <IconBook className="h-[18px] w-[18px] shrink-0" />
              <span className="min-w-0 flex-1 truncate text-left">
                {t("acct.pendingAbbrev")}
              </span>
              {pending > 0 ? (
                <span className="badge-pill shrink-0 border border-amber-500/20 bg-amber-500/10 text-amber-600">
                  {pending}
                </span>
              ) : (
                <IconChevronRight className="h-4 w-4 shrink-0 text-muted-soft" />
              )}
            </button>
          )}
          <button
            type="button"
            className="nav-item"
            onClick={() => openSettings("general")}
          >
            <IconSettings className="h-[18px] w-[18px] shrink-0" />
            <span className="flex-1 text-left">{t("user.settings")}</span>
          </button>

          <div className="my-1 border-t border-hairline" />
          <div className="flex items-center justify-between px-3 py-2">
            <span className="text-[13px] text-muted">{t("user.language")}</span>
            <div className="flex items-center gap-1 rounded-full bg-surface-strong p-0.5">
              {LOCALES.map((l) => (
                <button
                  key={l.id}
                  type="button"
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
          <button
            type="button"
            className="nav-item w-full text-left"
            onClick={() => void signOut()}
          >
            <IconLogout className="h-[18px] w-[18px] shrink-0" />
            {t("user.signOut")}
          </button>
        </div>
      )}

      <SettingsModal
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
        initialSection={settingsSection}
        pendingAbbrev={pending}
      />
    </div>
  );
}
