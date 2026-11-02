/* Shown in a guest's chat (signed in, no organization): the assistant only
 * looks up public legal documents, and an organization unlocks the full
 * assistant — with a link to create one where this deployment allows it,
 * otherwise a pointer to ask for an invitation. */
"use client";

import Link from "next/link";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";

export function GuestChatNote({ className = "" }: { className?: string }) {
  const { t } = useT();
  const { canCreateTenant } = useAuth();
  return (
    <p role="note" className={`caption text-center leading-relaxed text-muted ${className}`}>
      {t("guest.note")}{" "}
      {canCreateTenant ? (
        <>
          {t("guest.unlock")}{" "}
          <Link href="/onboarding/workspace" className="font-medium text-ink underline underline-offset-2">
            {t("guest.createOrg")}
          </Link>
        </>
      ) : (
        t("guest.askInvite")
      )}
    </p>
  );
}
