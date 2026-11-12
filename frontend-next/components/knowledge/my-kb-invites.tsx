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
import { userErrorMessage } from "@/lib/user-error";

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
      setError(userErrorMessage(e, t, "ui.inv.loadFailed"));
    }
  }, [t]);

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
        setNotice(t("ui.inv.accepted"));
        await load();
        onAccepted?.();
      } else {
        setError(res.message || t("ui.inv.acceptFailed"));
      }
    } catch (err) {
      setError(userErrorMessage(err, t, "ui.inv.acceptFailed"));
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
        setNotice(t("ui.inv.redeemed"));
        setToken("");
        await load();
        onAccepted?.();
      } else {
        setError(res.message || t("ui.inv.badToken"));
      }
    } catch (err) {
      setError(userErrorMessage(err, t, "ui.inv.badToken"));
    } finally {
      setBusy(false);
    }
  };

  const pending = invites.filter((i) => i.status === "pending");
  const decided = invites.filter((i) => i.status !== "pending");

  return (
    <div className="space-y-4">
      <div>
        <h3 className="text-sm font-semibold text-ink">{t("ui.inv.sharedTitle")}</h3>
        <p className="caption text-muted mt-0.5">
          {t("ui.inv.sharedDesc")}
        </p>
        {/* Tenant-join invites are a separate flow — accepting here never
         * changes workspace membership. */}
        <p className="caption text-muted mt-1">{t("kbInvite.inboxTenantNote")}</p>
      </div>

      {error && <p className="body-sm text-error">{error}</p>}
      {notice && <p className="body-sm text-success">{notice}</p>}

      <form onSubmit={redeemToken} className="flex flex-wrap items-end gap-2">
        <label className="block min-w-52 flex-1">
          <span className="caption mb-1 block text-muted">{t("ui.inv.redeemLabel")}</span>
          <input
            type="text"
            className="input font-mono text-xs"
            placeholder={t("ui.inv.redeemPh")}
            value={token}
            onChange={(e) => setToken(e.target.value)}
          />
        </label>
        <button type="submit" disabled={busy || !token.trim()} className="btn btn-outline btn-sm">
          {busy ? t("ui.inv.redeeming") : t("ui.inv.redeem")}
        </button>
      </form>

      {pending.length === 0 && decided.length === 0 && (
        <p className="caption text-muted">{t("ui.inv.none")}</p>
      )}

      {pending.length > 0 && (
        <div className="divide-y divide-hairline rounded-xl border border-hairline">
          {pending.map((inv) => (
            <div key={inv.id} className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-sm">
              <div className="min-w-0">
                <span className="font-medium text-ink truncate">{inv.kb_name || inv.kb_id}</span>
                <span className="caption text-muted ml-2">from organization {inv.owner_tenant_id}</span>
              </div>
              <button
                type="button"
                disabled={busy}
                onClick={() => void acceptByID(inv.id)}
                className="btn btn-primary btn-sm text-xs"
              >
                {t("ui.inv.accept")}
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
