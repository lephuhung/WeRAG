/* Grantees of the "Access" tab: personal invitations and unit grants with
 * status, expiry and revoke. Every revoke asks first; when the person would
 * still read the KB another way (published, or open to the whole owner
 * organization) the question says so. */
"use client";

import { useState } from "react";
import { revokeKBGrant, revokeKBInvite } from "@/lib/api/knowledge";
import {
  granteeStatusKey,
  isLiveGrantee,
  revokedReasonKey,
  revokeKeepsAccess,
  type KBAccessOverview,
  type KBGrantee,
} from "@/lib/kb-access";
import { useT, type LocaleKey } from "@/lib/i18n";
import { formatDate } from "@/lib/datetime";
import { userErrorMessage } from "@/lib/user-error";

export function KbAccessGrantees({ overview, onChanged }: { overview: KBAccessOverview; onChanged: () => void }) {
  const { t } = useT();
  const [showAll, setShowAll] = useState(false);
  const [confirming, setConfirming] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState("");
  const all = overview.grantees ?? [];
  const rows = showAll ? all : all.filter(isLiveGrantee);
  const rowKey = (g: KBGrantee) => `${g.kind}:${g.share_id || g.id}`;

  const revoke = async (g: KBGrantee) => {
    setBusy(rowKey(g));
    setError("");
    try {
      if (g.kind === "user") {
        const res = await revokeKBInvite(overview.kb_id, g.share_id);
        if (!res.success) throw new Error(res.message || t("kbAccess.failed"));
      } else {
        await revokeKBGrant(overview.kb_id, g.share_id);
      }
      setConfirming(null);
      onChanged();
    } catch (e) {
      setError(userErrorMessage(e, t, "kbAccess.failed"));
    } finally {
      setBusy(null);
    }
  };

  const askRevoke = (g: KBGrantee) => {
    setError("");
    setConfirming(rowKey(g));
  };

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h4 className="text-[13px] font-semibold text-ink">{t("kbAccess.grantees")}</h4>
        {all.length > 0 && (
          <label className="caption flex items-center gap-1.5 text-muted">
            <input type="checkbox" checked={showAll} onChange={(e) => setShowAll(e.target.checked)} />
            {t("kbAccess.showInactive")}
          </label>
        )}
      </div>
      {error && <p className="body-sm text-error">{error}</p>}
      {rows.length === 0 ? (
        <p className="caption text-muted-soft">{t("kbAccess.granteesEmpty")}</p>
      ) : (
        <div className="divide-y divide-hairline rounded-xl border border-hairline">
          {rows.map((g) => {
            const key = rowKey(g);
            const keeps = revokeKeepsAccess(overview, g);
            const reason = revokedReasonKey(g.revoked_reason);
            return (
              <div key={key} className="space-y-1.5 px-3 py-2 text-sm">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="min-w-0">
                    <span className="badge-pill mr-2 text-[10.5px]">
                      {g.kind === "user" ? t("kbAccess.kindUser") : t("kbAccess.kindTenant")}
                    </span>
                    <span className="font-medium text-ink">{g.name || g.id}</span>
                    {g.kind === "user" && g.email && g.email !== g.name && (
                      <span className="caption ml-2 text-muted">{g.email}</span>
                    )}
                    {g.kind === "user" && g.tenant_name && (
                      <span className="caption ml-2 text-muted">· {g.tenant_name}</span>
                    )}
                    <span className="caption ml-2 text-muted">· {t("kbAccess.permView")}</span>
                    <span className="badge-pill ml-2 text-[10.5px]">{t(granteeStatusKey(g.status) as LocaleKey)}</span>
                    {reason && <span className="caption ml-2 text-muted">({t(reason as LocaleKey)})</span>}
                    {g.expires_at && (
                      <span className="caption ml-2 text-muted">
                        {t("kbAccess.until", { date: formatDate(g.expires_at) })}
                      </span>
                    )}
                  </div>
                  {isLiveGrantee(g) && g.share_id && confirming !== key && (
                    <button
                      type="button"
                      disabled={busy !== null}
                      onClick={() => askRevoke(g)}
                      className="btn btn-outline btn-sm py-1 text-xs text-error"
                    >
                      {t("kbAccess.revoke")}
                    </button>
                  )}
                </div>
                {confirming === key && (
                  <div
                    role="alertdialog"
                    aria-label={t("kbAccess.revoke")}
                    className={`flex flex-wrap items-center gap-2 rounded-lg px-2.5 py-1.5 ${
                      keeps ? "bg-warning-surface text-warning" : "bg-surface-strong text-ink"
                    }`}
                  >
                    <span className="caption">
                      {keeps
                        ? keeps === "published"
                          ? t("kbAccess.revokeKeepsPublished")
                          : t("kbAccess.revokeKeepsTenant")
                        : g.kind === "user"
                          ? t("kbAccess.revokeConfirmUser", { name: g.name || g.email || g.id })
                          : t("kbAccess.revokeConfirmTenant", { name: g.name || g.id })}
                    </span>
                    <button
                      type="button"
                      className="btn btn-outline btn-sm py-0.5 text-xs text-error"
                      disabled={busy !== null}
                      aria-busy={busy === key}
                      onClick={() => void revoke(g)}
                    >
                      {busy === key ? "…" : keeps ? t("kbAccess.revokeAnyway") : t("kbAccess.revoke")}
                    </button>
                    <button type="button" className="btn btn-outline btn-sm py-0.5 text-xs" disabled={busy === key} onClick={() => setConfirming(null)}>
                      {t("common.cancel")}
                    </button>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
