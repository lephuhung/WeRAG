/* Recipient-side KB invitation inbox: list invites addressed to me,
 * accept in-app, or redeem an out-of-band token. Accepted KBs are
 * read-only and appear in the knowledge-base listing. */
"use client";

import { useCallback, useEffect, useState } from "react";
import {
  acceptKBInvite,
  acceptKBInviteByID,
  listMyKBInvites,
  type KBInvite,
} from "@/lib/api/knowledge";
import { useT } from "@/lib/i18n";

export function MyKBInvites({ onAccepted }: { onAccepted?: () => void }) {
  const { t } = useT();
  const [invites, setInvites] = useState<KBInvite[]>([]);
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const load = useCallback(async () => {
    try {
      setInvites(await listMyKBInvites());
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load invitations");
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const acceptByID = async (inviteId: string) => {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const res = await acceptKBInviteByID(inviteId);
      if (res.success) {
        setNotice("Invitation accepted. The knowledge base is now listed as shared with you (read-only).");
        await load();
        onAccepted?.();
      } else {
        setError(res.message || "Failed to accept invitation");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to accept invitation");
    } finally {
      setBusy(false);
    }
  };

  const redeemToken = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token.trim()) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const res = await acceptKBInvite(token.trim());
      if (res.success) {
        setNotice("Token redeemed. The knowledge base is now available read-only.");
        setToken("");
        await load();
        onAccepted?.();
      } else {
        setError(res.message || "Invalid or expired token");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Invalid or expired token");
    } finally {
      setBusy(false);
    }
  };

  const pending = invites.filter((i) => i.status === "pending");
  const decided = invites.filter((i) => i.status !== "pending");

  return (
    <div className="space-y-4">
      <div>
        <h3 className="text-sm font-semibold text-ink">Knowledge bases shared with me</h3>
        <p className="caption text-muted mt-0.5">
          Invitations from other workspaces. Accepted items are read-only.
        </p>
        {/* Tenant-join invites are a separate flow — accepting here never
         * changes workspace membership. */}
        <p className="caption text-muted mt-1">{t("kbInvite.inboxTenantNote")}</p>
      </div>

      {error && <p className="body-sm text-error">{error}</p>}
      {notice && <p className="body-sm text-emerald-600">{notice}</p>}

      <form onSubmit={redeemToken} className="flex flex-wrap items-end gap-2">
        <label className="block min-w-52 flex-1">
          <span className="caption mb-1 block text-muted">Redeem a token</span>
          <input
            type="text"
            className="input font-mono text-xs"
            placeholder="Paste an invitation token…"
            value={token}
            onChange={(e) => setToken(e.target.value)}
          />
        </label>
        <button type="submit" disabled={busy || !token.trim()} className="btn btn-outline btn-sm">
          {busy ? "Redeeming…" : "Redeem"}
        </button>
      </form>

      {pending.length === 0 && decided.length === 0 && (
        <p className="caption text-muted">No invitations.</p>
      )}

      {pending.length > 0 && (
        <div className="divide-y divide-hairline rounded-xl border border-hairline">
          {pending.map((inv) => (
            <div key={inv.id} className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-sm">
              <div className="min-w-0">
                <span className="font-medium text-ink truncate">{inv.kb_name || inv.kb_id}</span>
                <span className="caption text-muted ml-2">from workspace {inv.owner_tenant_id}</span>
              </div>
              <button
                type="button"
                disabled={busy}
                onClick={() => void acceptByID(inv.id)}
                className="btn btn-primary btn-sm text-xs"
              >
                Accept read access
              </button>
            </div>
          ))}
        </div>
      )}

      {decided.length > 0 && (
        <div className="space-y-1">
          {decided.map((inv) => (
            <p key={inv.id} className="caption text-muted">
              {inv.kb_name || inv.kb_id} — {inv.status}
            </p>
          ))}
        </div>
      )}
    </div>
  );
}
