/* Quick-share modal for a knowledge base, reachable from the list card
 * and the detail header: copy the KB link. Visibility is tenant-only —
 * every KB is private to its workspace; cross-workspace reads use
 * recipient-bound invitations (see KBInvitePanel).
 */
"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { Modal } from "@/components/modal";
import { copyToClipboard } from "@/lib/clipboard";
import { useT } from "@/lib/i18n";
import type { KnowledgeBaseRow } from "@/lib/api/knowledge";

export function KbShareModal({
  kb,
  open,
  onClose,
  /* True when the caller administers the workspace that owns this KB
   * (or is system admin). Backend stays authoritative. Retained for
   * the invite-hint footer; sharing itself grants nothing. */
  canManage,
}: {
  kb: KnowledgeBaseRow | null;
  open: boolean;
  onClose: () => void;
  canManage: boolean;
}) {
  const { t } = useT();
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (open && kb) {
      setCopied(false);
      setError("");
    }
  }, [open, kb]);

  if (!kb) return null;
  const shareUrl =
    typeof window !== "undefined"
      ? `${window.location.origin}/platform/knowledge-bases/${kb.id}`
      : `/platform/knowledge-bases/${kb.id}`;

  return (
    <Modal open={open} title={t("kbShare.title")} onClose={onClose}>
      <div className="space-y-5">
        {/* Copyable link — copying the URL grants nothing. Only workspace
         * members, and the single user named on a KB invitation, can open
         * it; everyone else sees a no-access panel. */}
        <div className="space-y-2">
          <span className="caption font-medium text-muted">{t("kbShare.linkLabel")}</span>
          <div className="flex items-center gap-2">
            <input
              type="text"
              readOnly
              value={shareUrl}
              className="input text-xs font-mono select-all"
              onFocus={(e) => e.target.select()}
            />
            <button
              type="button"
              className="btn btn-primary shrink-0 text-xs"
              onClick={async () => {
                const ok = await copyToClipboard(shareUrl);
                if (ok) {
                  setCopied(true);
                  setTimeout(() => setCopied(false), 2000);
                }
              }}
            >
              {copied ? t("kbShare.copied") : t("kbShare.copyLink")}
            </button>
          </div>
          <p className="caption text-muted text-[11px]">
            {t("kbShare.linkNoteNoGrant")}
          </p>
        </div>

        {/* Visibility is tenant-only (public sharing is retired). */}
        <div className="space-y-2 border-t border-hairline pt-4">
          <span className="caption font-medium text-muted">{t("kbSettings.visibilityNote")}</span>
          <p className="caption text-muted text-[11px]">{t("kbSettings.visTenantTip")}</p>
        </div>

        {error && <p className="body-sm text-error">{error}</p>}

        {canManage && (
          <p className="caption text-muted border-t border-hairline pt-4">
            {t("kbShare.inviteHintPre")}{" "}
            <Link href="/platform/knowledge-bases" className="text-brand hover:underline" onClick={onClose}>
              {t("nav.knowledgeBases")}
            </Link>{" "}
            {t("kbShare.inviteHintPost")}
          </p>
        )}
      </div>
    </Modal>
  );
}
