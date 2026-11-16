/* The administration console's own top bar.
 *
 * The console is a page of its own, so it cannot borrow the app's <Header />
 * (that one belongs to the frame with the chat rail). It carries what an
 * admin still needs from the shell: the way back to the app, notifications
 * and the account menu — the account popover is the only route to
 * settings/sign-out, and it used to live in the chat rail.
 *
 * The account menu comes last so the notification panel, which anchors to
 * its own right edge, stays within the viewport on a phone; and it opens
 * downward (`placement`) because this bar is at the top of the window.
 * The page title is a plain span: each console page owns its own h1.
 *
 * It also carries the finished-reply toasts: the rail's copy of them is not
 * mounted here, so a chat that finishes while an admin is in the console
 * would otherwise go unannounced. Titles live in the rail's session list, so
 * these toasts name no chat — the generic wording covers that.
 */
"use client";

import Link from "next/link";
import { AccountMenu } from "@/components/account-menu";
import { BrandLockup } from "@/components/brand-logo";
import { NotificationBell } from "@/components/notification-bell";
import { SessionCompleteToasts } from "@/components/session-complete-toasts";
import { IconChevronLeft } from "@/components/icons";
import { SHELL_HEADER_HEIGHT_PX } from "@/components/shell-breakpoints";
import { useT } from "@/lib/i18n";

const NO_TITLES: ReadonlyMap<string, string> = new Map();

export function ConsoleTopBar() {
  const { t } = useT();
  return (
    <header
      className="flex shrink-0 items-center gap-3 border-b border-hairline bg-canvas px-4 sm:px-6"
      style={{ height: SHELL_HEADER_HEIGHT_PX }}
    >
      {/* the lockup names itself ("weRAG"); no aria-label, or it would
          shadow its own visible text */}
      <Link href="/platform/creatChat" className="hidden shrink-0 items-center transition-opacity hover:opacity-85 sm:flex">
        <BrandLockup size="sm" priority />
      </Link>
      <span className="hidden h-5 w-px shrink-0 bg-hairline sm:block" aria-hidden />
      <span className="min-w-0 truncate text-[15px] font-semibold text-ink">{t("adminPage.title")}</span>

      <Link
        href="/platform/creatChat"
        className="btn btn-row btn-soft ml-auto shrink-0"
        title={t("adminPage.backToApp")}
      >
        <IconChevronLeft className="h-3.5 w-3.5" />
        <span className="hidden sm:inline">{t("adminPage.backToApp")}</span>
      </Link>
      <NotificationBell />
      {/* avatar only: a bar has no room for the name, and the popover
          repeats it anyway */}
      <div className="shrink-0">
        <AccountMenu placement="down" compact />
      </div>
      <SessionCompleteToasts titles={NO_TITLES} />
    </header>
  );
}

export default ConsoleTopBar;
