/* Invite-member modal shared by the workspace members settings page and
 * the organizations workspace panel: invite a specific email, or mint a
 * shareable join link (POST /tenants/:id/invite-links → /register?token=). */
"use client";

import { useState } from "react";
import { Modal } from "@/components/modal";
import { Select } from "@/components/select";
import { addMember, createInviteLink, type TenantRole } from "@/lib/api/tenants";
import { copyToClipboard } from "@/lib/clipboard";

/* Assignable workspace roles: Admin and Member. The legacy owner role is
 * retired and never offered. */
const ROLES: { id: TenantRole; label: string; desc: string }[] = [
  { id: "admin", label: "Admin", desc: "Manage members, knowledge bases, models, integrations, skills, and storage" },
  { id: "member", label: "Member", desc: "Read knowledge bases, upload documents, and chat with agents" },
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
  const [tab, setTab] = useState<"email" | "link">("email");
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<TenantRole>("member");
  const [msg, setMsg] = useState("");
  const [generatedLink, setGeneratedLink] = useState("");
  const [linkCopied, setLinkCopied] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const roleOptions = ROLES.map((r) => ({ value: r.id, label: r.label }));
  const roleDesc = ROLES.find((r) => r.id === role)?.desc ?? "";

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
        setError(res.message || "Failed to send invitation");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to send invitation");
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
        setError(res.message || "Failed to generate invite link");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to generate invite link");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal open={open} title="Invite to Workspace" onClose={onClose}>
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
                  ? "border-brand text-brand font-semibold"
                  : "border-transparent text-muted hover:text-ink"
              }`}
            >
              {k === "email" ? "Invite by Email" : "Shareable Link"}
            </button>
          ))}
        </div>

        {error && <p className="body-sm text-error">{error}</p>}

        {tab === "email" ? (
          <form onSubmit={inviteByEmail} className="space-y-4">
            <label className="block">
              <span className="caption mb-1.5 block text-muted">Email address</span>
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
              <span className="caption mb-1.5 block text-muted">Initial role</span>
              <Select
                value={role}
                onChange={(v) => setRole(v as TenantRole)}
                options={roleOptions}
              />
              <span className="caption mt-1.5 block text-muted-soft">{roleDesc}</span>
            </label>

            <div className="flex justify-end gap-3 pt-3">
              <button type="button" className="btn btn-outline" onClick={onClose}>
                Cancel
              </button>
              <button type="submit" disabled={busy || !email.trim()} className="btn btn-primary">
                {busy ? "Sending…" : "Send Invitation"}
              </button>
            </div>
          </form>
        ) : (
          <div className="space-y-4">
            <label className="block">
              <span className="caption mb-1.5 block text-muted">Assign role for joiners</span>
              <Select
                value={role}
                onChange={(v) => setRole(v as TenantRole)}
                options={roleOptions}
              />
              <span className="caption mt-1.5 block text-muted-soft">{roleDesc}</span>
            </label>

            <label className="block">
              <span className="caption mb-1.5 block text-muted">Note / Message (optional)</span>
              <input
                type="text"
                placeholder="e.g. Engineering team onboarding"
                className="input"
                value={msg}
                onChange={(e) => setMsg(e.target.value)}
              />
            </label>

            {generatedLink ? (
              <div className="rounded-xl border border-hairline bg-surface-strong/50 p-4 space-y-2">
                <span className="caption font-medium text-ink">Active invite link</span>
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
                    {linkCopied ? "Copied!" : "Copy link"}
                  </button>
                </div>
                <p className="caption text-muted text-[11px]">
                  Anyone with this link can join this workspace as {role}.
                </p>
              </div>
            ) : (
              <button
                type="button"
                onClick={() => void createLink()}
                disabled={busy}
                className="btn btn-primary w-full"
              >
                {busy ? "Generating…" : "Generate Invite Link"}
              </button>
            )}

            <div className="flex justify-end pt-2">
              <button type="button" className="btn btn-outline" onClick={onClose}>
                Close
              </button>
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}
