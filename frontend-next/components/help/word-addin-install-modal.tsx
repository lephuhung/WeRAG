"use client";

import { Modal } from "@/components/modal";
import { WordAddinInstall } from "@/components/auth/word-addin-install";
import { useT } from "@/lib/i18n";

/* "Install the Word add-in" for signed-in people: the same per-system guide
 * the sign-in page shows, opened from the account menu or the Ctrl+K / ⌘K
 * palette. */
export function WordAddinInstallModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useT();
  return (
    <Modal open={open} onClose={onClose} title={t("auth.installTitle")} width="w-[620px]">
      <WordAddinInstall inDialog />
    </Modal>
  );
}

/** The public help page; opened in a new tab so the chat stays where it was. */
export const HELP_PATH = "/ho-tro";
