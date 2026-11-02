/* Bottom-of-sidebar account button (replaces the old top-right header menu):
 * avatar + name + role, opening an upward popover with the pending-
 * abbreviations count (approvers only — so suggestions can't go unnoticed
 * inside deep settings), Settings (opens SettingsModal), the SystemAdmin
 * console link (system admins only), the EN/VI pill switch and Sign out. */
"use client";

import { usePathname, useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";
import { listAbbreviations } from "@/lib/api/abbreviations";
import { formatTokens, getMyUsage, type UsageSummary } from "@/lib/api/usage";
import { useAuth, useTenantRole } from "@/lib/auth";
import { useAvatarUrl } from "@/lib/avatar";
import { useT, type Locale } from "@/lib/i18n";
import { consumeSettingsRedirect } from "@/lib/settings-deep-link";
import { useToast } from "@/components/toast";
import { SettingsModal } from "@/components/settings-modal";
import { UsagePlanPill } from "@/components/usage-plan-pill";
import { ADMIN_PATH } from "@/components/admin/admin-routes";
import {
  IconBook,
  IconChevronRight,
  IconChevronUp,
  IconLogout,
  IconPulse,
  IconSettings,
} from "@/components/icons";

const LOCALES: { id: Locale; label: string }[] = [
  { id: "en", label: "EN" },
  { id: "vi", label: "VI" },
];

const PENDING_POLL_MS = 20_000;
/* Pending ids are fetched newest first; a page this size covers any
 * realistic backlog between two polls. */
const PENDING_PAGE_SIZE = 100;
/* Kept per browser session, so a reload or a second tab does not repeat the
 * toast for the same backlog. */
const NOTIFIED_KEY = "werag_abbrev_notified_at";

/* Newest created_at already announced. Pending rows are listed newest first,
 * so only a row created after this mark is a new suggestion; approving one
 * just slides an older row into the polled window and must stay quiet. */
function loadNotified(): string {
  try {
    return sessionStorage.getItem(NOTIFIED_KEY) ?? "";
  } catch {
    return "";
  }
}

function saveNotified(mark: string) {
  try {
    sessionStorage.setItem(NOTIFIED_KEY, mark);
  } catch {
    /* storage disabled — at worst the toast repeats after reload */
  }
}

export function AccountMenu() {
  const router = useRouter();
  const pathname = usePathname();
  const { t, locale, setLocale } = useT();
  const auth = useAuth();
  const { role, isSystemAdmin, isGuest } = useTenantRole();
  const [open, setOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsSection, setSettingsSection] = useState("general");
  const [pending, setPending] = useState(0);
  const [usage, setUsage] = useState<UsageSummary | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const toast = useToast();
  const toastRef = useRef(toast);
  toastRef.current = toast;
  const tLatest = useRef(t);
  tLatest.current = t;

  /* Suggestions are mostly created server-side (the agent's abbreviation
   * tools), so the browser only learns about them by polling. A pending row
   * newer than the last announced one raises one toast — including a backlog
   * already waiting at sign-in — and an approval landing in the same
   * interval cannot mask it. */
  const refreshPending = useCallback(async () => {
    try {
      const res = await listAbbreviations({ isActive: false, page: 1, pageSize: PENDING_PAGE_SIZE });
      const total = res.total ?? 0;
      setPending(total);
      const times = (res.data ?? []).map((a) => new Date(a.created_at).getTime()).filter((n) => !Number.isNaN(n));
      const newest = times.length ? Math.max(...times) : 0;
      const mark = Number(loadNotified()) || 0;
      if (newest > mark) saveNotified(String(newest));
      if (newest > mark) {
        // Longer dwell (20s): admin may be away from the screen when a new
        // suggestion lands, and 5s was easy to miss.
        toastRef.current.info(
          tLatest.current("acct.abbrevPendingToast", { count: total }),
          () => window.dispatchEvent(new CustomEvent("weknora:open-settings", { detail: "abbreviations" })),
          20_000,
        );
      }
    } catch {
      /* non-fatal — badge just stays stale */
    }
  }, []);

  /* Pending count: on mount, every 20s, on returning to the tab, and whenever the abbreviations
   * manager mutates (it dispatches weknora:abbreviations-changed). */
  useEffect(() => {
    if (!isSystemAdmin) return;
    void refreshPending();
    const timer = window.setInterval(() => void refreshPending(), PENDING_POLL_MS);
    const onChanged = () => void refreshPending();
    const onVisible = () => document.visibilityState === "visible" && void refreshPending();
    window.addEventListener("weknora:abbreviations-changed", onChanged);
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("weknora:abbreviations-changed", onChanged);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [isSystemAdmin, refreshPending]);

  useEffect(() => setOpen(false), [pathname]);

  /* Legacy settings URLs redirect into the platform shell. Consume the
   * requested section after navigation so the modal opens reliably. */
  useEffect(() => {
    const section = consumeSettingsRedirect(window.sessionStorage);
    if (!section) return;
    setSettingsSection(section);
    setSettingsOpen(true);
  }, [pathname]);

  /* Own usage counters, refreshed each time the menu opens: cheap (two
   * indexed rows) and always current for the person looking at it. */
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    getMyUsage()
      .then((res) => {
        if (!cancelled) setUsage(res.data ?? null);
      })
      .catch(() => {
        /* non-fatal — the block just stays hidden */
      });
    return () => {
      cancelled = true;
    };
  }, [open]);

  /* Toast notifications (e.g. new pending abbreviation) open the settings
   * modal by dispatching weknora:open-settings with the section as detail. */
  useEffect(() => {
    const onOpen = (e: Event) => {
      setSettingsSection((e as CustomEvent<string>).detail || "general");
      setSettingsOpen(true);
    };
    window.addEventListener("weknora:open-settings", onOpen);
    return () => window.removeEventListener("weknora:open-settings", onOpen);
  }, []);
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
      : isGuest
        ? t("acct.roleGuest")
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
        <span className="flex h-8 w-8 shrink-0 items-center justify-center overflow-hidden rounded-full bg-gradient-to-br from-sky-500 to-indigo-500 text-[11px] font-semibold text-white">
          {avatarUrl ? (
            // eslint-disable-next-line @next/next/no-img-element -- authenticated blob URL, not a Next asset
            <img src={avatarUrl} alt="" className="h-8 w-8 object-cover" />
          ) : (
            initials
          )}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[14.5px] font-semibold leading-tight text-ink">
            {name || "—"}
          </span>
          <span className="block truncate text-[12.5px] leading-tight text-muted">
            {roleLabel}
          </span>
        </span>
        {isSystemAdmin && pending > 0 && (
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

          {usage?.user && (
            <div className="px-3 py-2">
              <div className="caption mb-1 text-muted">{t("acct.usageTitle")}</div>
              <div className="flex items-center justify-between text-[12.5px]">
                <span className="text-muted">{t("acct.usageToday")}</span>
                <span className="text-ink">
                  {t("acct.usageLine", {
                    questions: usage.user.today.questions.toLocaleString(),
                    tokens: formatTokens(usage.user.today.tokens),
                  })}
                </span>
              </div>
              <div className="flex items-center justify-between text-[12.5px]">
                <span className="text-muted">{t("acct.usageMonth")}</span>
                <span className="text-ink">
                  {t("acct.usageLine", {
                    questions: usage.user.month.questions.toLocaleString(),
                    tokens: formatTokens(usage.user.month.tokens),
                  })}
                </span>
              </div>
              {usage.entitlement ? (
                <div className="mt-1.5 border-t border-hairline pt-1.5">
                  <div className="flex items-center justify-between text-[12.5px]">
                    <span className="text-muted">{t("acct.plan")}</span>
                    <UsagePlanPill entitlement={usage.entitlement} />
                  </div>
                  {usage.entitlement.limits.map((l, i) => (
                    <div key={i} className="flex items-center justify-between text-[12.5px]">
                      <span className="text-muted">
                        {l.metric === "tokens" ? t("acct.planTokens") : t("acct.planQuestions")}/
                        {l.period === "day" ? t("acct.planDay") : t("acct.planMonth")}
                      </span>
                      <span className={l.exhausted ? "text-error" : "text-ink"}>
                        {l.limit > 0
                          ? t("acct.planRemaining", {
                              remaining: l.metric === "tokens" ? formatTokens(l.remaining) : l.remaining.toLocaleString(),
                              limit: l.metric === "tokens" ? formatTokens(l.limit) : l.limit.toLocaleString(),
                            })
                          : t("acct.planUnlimited")}
                      </span>
                    </div>
                  ))}
                </div>
              ) : usage.enforced && !isSystemAdmin ? (
                <div className="caption mt-1.5 border-t border-hairline pt-1.5 text-error">{t("acct.planNone")}</div>
              ) : null}
              {!isSystemAdmin && (
                <button type="button" className="caption mt-1.5 block w-full text-left text-muted underline hover:text-ink" onClick={() => openSettings("billing")}>
                  {t("acct.buyPlan")}
                </button>
              )}
            </div>
          )}
          {usage?.user && <div className="my-1 border-t border-hairline" />}

          {isSystemAdmin && (
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
          {isSystemAdmin && (
            <button
              type="button"
              className="nav-item"
              onClick={() => {
                setOpen(false);
                router.push(ADMIN_PATH);
              }}
            >
              <IconPulse className="h-[18px] w-[18px] shrink-0" />
              <span className="flex-1 text-left">{t("adminPage.title")}</span>
            </button>
          )}

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
