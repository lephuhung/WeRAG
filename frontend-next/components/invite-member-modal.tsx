/* Invite-member modal for the workspace members settings page: invite a
 * specific email, or mint a shareable join link (POST
 * /tenants/:id/invite-links → /register?token=). */
"use client";

import { useState } from "react";
import { Modal } from "@/components/modal";
import { Select } from "@/components/select";
import { addMember, createInviteLink, type TenantRole } from "@/lib/api/tenants";
import { copyToClipboard } from "@/lib/clipboard";
import type { LocaleKey } from "@/lib/i18n";
import { useT } from "@/lib/i18n";

/* Assignable workspace roles: Admin and Member. The legacy owner role is
 * retired and never offered. */
const ROLES: { id: TenantRole; labelKey: LocaleKey; descKey: LocaleKey }[] = [
  { id: "admin", labelKey: "acct.roleAdmin", descKey: "mem.roleAdminDesc" },
  { id: "member", labelKey: "acct.roleMember", descKey: "mem.roleMemberDesc" },
];

export function InviteMemberModal({
  tenantId,
  open,
  onClose,
  onInvited,
}: {
  tenantId: number;
  open: boolean;
  onClose: () => void;
  /* Called after a successful email invite or link creation so the caller
   * can refresh its member/invitation lists. */
  onInvited?: () => void;
}) {
  const { t } = useT();
  const [tab, setTab] = useState<"email" | "link">("email");
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<TenantRole>("member");
  const [msg, setMsg] = useState("");
  const [generatedLink, setGeneratedLink] = useState("");
  const [linkCopied, setLinkCopied] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const roleOptions = ROLES.map((r) => ({ value: r.id, label: t(r.labelKey) }));
  const roleEntry = ROLES.find((r) => r.id === role);
  const roleDesc = roleEntry ? t(roleEntry.descKey) : "";
  const roleLabel = roleEntry ? t(roleEntry.labelKey) : role;

  const inviteByEmail = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!tenantId || !email.trim()) return;
    setBusy(true);
    setError("");
    try {
      const res = await addMember(tenantId, { email: email.trim(), role });
      if (res.success) {
        setEmail("");
        onClose();
        onInvited?.();
      } else {
        setError(res.message || t("ui.invm.sendFailed"));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("ui.invm.sendFailed"));
    } finally {
      setBusy(false);
    }
  };

  const createLink = async () => {
    if (!tenantId) return;
    setBusy(true);
    setError("");
    try {
      const res = await createInviteLink(tenantId, {
        role,
        message: msg.trim() || undefined,
      });
      if (res.success && res.data) {
        setGeneratedLink(
          res.data.invite_url
            ? new URL(res.data.invite_url, window.location.origin).toString()
            : `${window.location.origin}/invite/${res.data.id}`,
        );
        onInvited?.();
      } else {
        setError(res.message || t("ui.invm.linkFailed"));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("ui.invm.linkFailed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal open={open} title={t("ui.invm.title")} onClose={onClose}>
      <div className="space-y-5">
        {/* Tabs */}
        <div className="flex border-b border-hairline pb-2 gap-4">
          {(["email", "link"] as const).map((k) => (
            <button
              key={k}
              type="button"
              onClick={() => setTab(k)}
              className={`text-sm font-medium pb-2 transition-colors border-b-2 -mb-2.5 ${
                tab === k
                  ? "border-primary text-primary font-semibold"
                  : "border-transparent text-muted hover:text-ink"
              }`}
            >
              {k === "email" ? t("ui.invm.byEmail") : t("ui.invm.byLink")}
            </button>
          ))}
        </div>

        {error && <p className="body-sm text-error">{error}</p>}

        {tab === "email" ? (
          <form onSubmit={inviteByEmail} className="space-y-4">
            <label className="block">
              <span className="caption mb-1.5 block text-muted">{t("ui.invm.email")}</span>
              <input
                type="email"
                required
                placeholder="member@company.com"
                className="input"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </label>

            <label className="block">
              <span className="caption mb-1.5 block text-muted">{t("ui.invm.initialRole")}</span>
              <Select
                value={role}
                onChange={(v) => setRole(v as TenantRole)}
                options={roleOptions}
              />
              <span className="caption mt-1.5 block text-muted-soft">{roleDesc}</span>
            </label>

            <div className="flex justify-end gap-3 pt-3">
              <button type="button" className="btn btn-outline" onClick={onClose}>
                {t("common.cancel")}
              </button>
              <button type="submit" disabled={busy || !email.trim()} className="btn btn-primary">
                {busy ? t("ui.invm.sending") : t("ui.invm.send")}
              </button>
            </div>
          </form>
        ) : (
          <div className="space-y-4">
            <label className="block">
              <span className="caption mb-1.5 block text-muted">{t("ui.invm.joinRole")}</span>
              <Select
                value={role}
                onChange={(v) => setRole(v as TenantRole)}
                options={roleOptions}
              />
              <span className="caption mt-1.5 block text-muted-soft">{roleDesc}</span>
            </label>

            <label className="block">
              <span className="caption mb-1.5 block text-muted">{t("ui.invm.note")}</span>
              <input
                type="text"
                placeholder={t("ui.invm.notePh")}
                className="input"
                value={msg}
                onChange={(e) => setMsg(e.target.value)}
              />
            </label>

            {generatedLink ? (
              <div className="rounded-xl border border-hairline bg-surface-strong/50 p-4 space-y-2">
                <span className="caption font-medium text-ink">{t("ui.invm.activeLink")}</span>
                <div className="flex items-center gap-2">
                  <input
                    type="text"
                    readOnly
                    value={generatedLink}
                    className="input text-xs font-mono select-all"
                  />
                  <button
                    type="button"
                    onClick={async () => {
                      const ok = await copyToClipboard(generatedLink);
                      if (ok) {
                        setLinkCopied(true);
                        setTimeout(() => setLinkCopied(false), 2000);
                      }
                    }}
                    className="btn btn-primary shrink-0 text-xs"
                  >
                    {linkCopied ? t("ui.invm.copied") : t("mem.copyLink")}
                  </button>
                </div>
                <p className="caption text-muted text-[11px]">
                  {t("ui.invm.linkHint", { role: roleLabel })}
                </p>
              </div>
            ) : (
              <button
                type="button"
                onClick={() => void createLink()}
                disabled={busy}
                className="btn btn-primary w-full"
              >
                {busy ? t("ui.invm.generating") : t("ui.invm.generate")}
              </button>
            )}

            <div className="flex justify-end pt-2">
              <button type="button" className="btn btn-outline" onClick={onClose}>
                {t("common.close")}
              </button>
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}
