/* Quick-share modal for a knowledge base, reachable from the list card
 * and the detail header: copy the KB link and — for the owning unit's
 * admin — publish the KB to every unit or withdraw it. Sharing with chosen
 * units or people lives in KBAccessPanel.
 */
"use client";

import { useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { KBAccessPanel } from "@/components/knowledge/kb-access-panel";
import { copyToClipboard } from "@/lib/clipboard";
import { useT } from "@/lib/i18n";
import { setKnowledgeBasePublished, type KnowledgeBaseRow } from "@/lib/api/knowledge";

export function KbShareModal({
  kb,
  open,
  onClose,
  /* True when the caller administers the workspace that owns this KB
   * (or is system admin). Backend stays authoritative. Retained for
   * the invite-hint footer; sharing itself grants nothing. */
  canManage,
  onChanged,
}: {
  kb: KnowledgeBaseRow | null;
  open: boolean;
  onClose: () => void;
  canManage: boolean;
  /* Called after the publish state changed so the caller can reload. */
  onChanged?: () => void;
}) {
  const { t } = useT();
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState("");
  const [published, setPublished] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (open && kb) {
      setCopied(false);
      setError("");
      setPublished(kb.visibility === "published");
    }
  }, [open, kb]);

  const togglePublished = async () => {
    if (!kb || busy) return;
    setBusy(true);
    setError("");
    try {
      await setKnowledgeBasePublished(kb.id, !published);
      setPublished(!published);
      onChanged?.();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("kbPub.failed"));
    } finally {
      setBusy(false);
    }
  };

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

        {/* Publishing: a unit's admin opens a tenant KB to every unit (the
         * unit keeps all writes); platform-public KBs are not tenant KBs. */}
        {kb.visibility !== "public" && (
          <div className="space-y-2 border-t border-hairline pt-4">
            <div className="flex items-center justify-between gap-3">
              <div>
                <span className="caption font-medium text-ink">{t("kbPub.label")}</span>
                <p className="caption text-muted text-[11px]">{t("kbPub.desc")}</p>
              </div>
              {canManage ? (
                <button
                  type="button"
                  className={`btn btn-sm shrink-0 ${published ? "btn-outline" : "btn-primary"}`}
                  disabled={busy}
                  onClick={() => void togglePublished()}
                >
                  {published ? t("kbPub.withdraw") : t("kbPub.publish")}
                </button>
              ) : (
                <span className="badge-pill shrink-0 text-[11px]">{published ? t("kbPub.on") : t("kbPub.off")}</span>
              )}
            </div>
          </div>
        )}

        {error && <p className="body-sm text-error">{error}</p>}

        {canManage && kb.visibility !== "public" && (
          <div className="border-t border-hairline pt-4">
            <span className="caption mb-3 block font-medium text-ink">{t("kbList.inviteModalTitle")}</span>
            <KBAccessPanel kbId={kb.id} kbName={kb.name} ownerTenantId={kb.owner_tenant_id} />
          </div>
        )}
      </div>
    </Modal>
  );
}
