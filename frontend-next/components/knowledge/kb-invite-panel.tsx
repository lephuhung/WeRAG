/* Admin panel for recipient-bound KB read invitations.
 * The owning workspace's Admin invites ONE specific user in another
 * workspace (email + their workspace ID); the grant is read-only and
 * single-user. The token is shown once for out-of-band delivery; the
 * recipient can also accept in-app from "My KB invites". */
"use client";

import { useCallback, useEffect, useState } from "react";
import {
  issueKBInvite,
  listKBInvites,
  revokeKBInvite,
  type KBInvite,
} from "@/lib/api/knowledge";
import { copyToClipboard } from "@/lib/clipboard";
import { useT } from "@/lib/i18n";

export function KBInvitePanel({ kbId, kbName }: { kbId: string; kbName?: string }) {
  const { t } = useT();
  const [email, setEmail] = useState("");
  const [tenantId, setTenantId] = useState("");
  const [invites, setInvites] = useState<KBInvite[]>([]);
  const [createdToken, setCreatedToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const load = useCallback(async () => {
    try {
      setInvites(await listKBInvites(kbId));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load invitations");
    }
  }, [kbId]);

  useEffect(() => {
    void load();
  }, [load]);

  const issue = async (e: React.FormEvent) => {
    e.preventDefault();
    const target = Number(tenantId);
    if (!email.trim() || !target) return;
    setBusy(true);
    setError("");
    setNotice("");
    setCreatedToken("");
    try {
      const res = await issueKBInvite(kbId, {
        recipient_email: email.trim(),
        recipient_tenant_id: target,
      });
      if (res.success && res.data) {
        setCreatedToken(res.data.token ?? "");
        setNotice(
          res.data.token
            ? "Invitation created. Share the token with the recipient — it is shown only once."
            : "Invitation created.",
        );
        setEmail("");
        await load();
      } else {
        setError(res.message || "Failed to create invitation");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create invitation");
    } finally {
      setBusy(false);
    }
  };

  const revoke = async (inviteId: string) => {
    setError("");
    try {
      const res = await revokeKBInvite(kbId, inviteId);
      if (res.success) {
        setNotice("Invitation revoked. Access stops immediately.");
        await load();
      } else {
        setError(res.message || "Failed to revoke invitation");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to revoke invitation");
    }
  };

  return (
    <div className="space-y-4">
      <div>
        <h3 className="text-sm font-semibold text-ink">Share read access</h3>
        <p className="caption text-muted mt-0.5">
          Invite one member of another workspace to read{kbName ? ` “${kbName}”` : " this knowledge base"}.
          Read-only, non re-shareable.
        </p>
        {/* Recipient-bound guidance: the invitee must already belong to the
         * entered workspace; this never joins anyone to a workspace and is
         * distinct from tenant invitations. */}
        <p className="caption text-muted mt-1">{t("kbInvite.recipientActiveNote")}</p>
      </div>

      {error && <p className="body-sm text-error">{error}</p>}
      {notice && <p className="body-sm text-emerald-600">{notice}</p>}

      <form onSubmit={issue} className="flex flex-wrap items-end gap-2">
        <label className="block min-w-52 flex-1">
          <span className="caption mb-1 block text-muted">Recipient email</span>
          <input
            type="email"
            required
            className="input"
            placeholder="member@other-workspace.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </label>
        <label className="block w-36">
          <span className="caption mb-1 block text-muted">Their workspace ID</span>
          <input
            type="number"
            required
            min={1}
            className="input"
            placeholder="e.g. 42"
            value={tenantId}
            onChange={(e) => setTenantId(e.target.value)}
          />
        </label>
        <button type="submit" disabled={busy} className="btn btn-primary btn-sm">
          {busy ? "Inviting…" : "Invite"}
        </button>
      </form>

      {createdToken && (
        <div className="rounded-xl border border-hairline bg-surface-strong/50 p-3 space-y-2">
          <span className="caption font-medium text-ink">One-time invitation token</span>
          <div className="flex items-center gap-2">
            <input type="text" readOnly value={createdToken} className="input text-xs font-mono select-all" />
            <button
              type="button"
              className="btn btn-primary shrink-0 text-xs"
              onClick={() => void copyToClipboard(createdToken).then((ok) => ok && setNotice("Token copied."))}
            >
              Copy
            </button>
          </div>
        </div>
      )}

      {invites.length > 0 && (
        <div className="divide-y divide-hairline rounded-xl border border-hairline">
          {invites.map((inv) => (
            <div key={inv.id} className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-sm">
              <div className="min-w-0">
                <span className="font-medium text-ink truncate">{inv.recipient_user_id}</span>
                <span className="caption text-muted ml-2">workspace {inv.recipient_tenant_id}</span>
                <span className="badge-pill uppercase text-[10.5px] ml-2">{inv.status}</span>
              </div>
              {(inv.status === "pending" || inv.status === "accepted") && (
                <button
                  type="button"
                  onClick={() => void revoke(inv.id)}
                  className="btn btn-outline btn-sm text-xs text-rose-600 py-1"
                >
                  Revoke
                </button>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
