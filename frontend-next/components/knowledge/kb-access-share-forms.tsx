/* Share actions of the "Access" tab (managers only): invite one person
 * (read-only, recipient-bound; members of the owner unit allowed) and grant
 * read access to a whole unit. Both are view-only and never allow original
 * download. */
"use client";

import { useEffect, useState } from "react";
import { grantKBToTenant, issueKBInvite } from "@/lib/api/knowledge";
import { copyToClipboard } from "@/lib/clipboard";
import { defaultInviteTenant, type KBAccessOverview } from "@/lib/kb-access";
import { useT } from "@/lib/i18n";
import { TenantPicker } from "@/components/knowledge/tenant-picker";

function errText(e: unknown, fallback: string) {
  return e instanceof Error && e.message ? e.message : fallback;
}

export function KbInviteForm({ overview, onDone }: { overview: KBAccessOverview; onDone: () => void }) {
  const { t } = useT();
  const [email, setEmail] = useState("");
  const [unit, setUnit] = useState(() => defaultInviteTenant(overview));
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    setUnit(defaultInviteTenant(overview));
  }, [overview.kb_id, overview.visibility, overview.owner_tenant_id]); // eslint-disable-line react-hooks/exhaustive-deps

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const target = Number(unit);
    if (!email.trim() || !target) return;
    setBusy(true);
    setError("");
    setNotice("");
    setToken("");
    try {
      const res = await issueKBInvite(overview.kb_id, { recipient_email: email.trim(), recipient_tenant_id: target });
      if (!res.success) throw new Error(res.message || t("kbAccess.failed"));
      setToken(res.data?.token ?? "");
      setNotice(t("kbAccess.inviteCreated"));
      setEmail("");
      onDone();
    } catch (err) {
      setError(errText(err, t("kbAccess.failed")));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-2">
      <h4 className="text-[13px] font-semibold text-ink">{t("kbAccess.inviteTitle")}</h4>
      <p className="caption text-muted">{t("kbAccess.inviteDesc")}</p>
      <form onSubmit={submit} className="flex flex-wrap items-end gap-2">
        <label className="block min-w-52 flex-1">
          <span className="caption mb-1 block text-muted">{t("kbAccess.inviteEmail")}</span>
          <input type="email" required className="input" value={email} onChange={(e) => setEmail(e.target.value)} />
        </label>
        <label className="block w-56">
          <span className="caption mb-1 block text-muted">{t("kbAccess.inviteUnit")}</span>
          <TenantPicker value={unit} onChange={setUnit} required />
        </label>
        <button type="submit" disabled={busy || !unit} className="btn btn-primary btn-sm">
          {t("kbAccess.invite")}
        </button>
      </form>
      {error && <p className="body-sm text-error">{error}</p>}
      {notice && <p className="body-sm text-emerald-600">{notice}</p>}
      {token && (
        <div className="space-y-1.5 rounded-xl border border-hairline bg-surface-strong/50 p-3">
          <span className="caption font-medium text-ink">{t("kbAccess.inviteToken")}</span>
          <div className="flex items-center gap-2">
            <input type="text" readOnly value={token} className="input select-all font-mono text-xs" />
            <button
              type="button"
              className="btn btn-primary shrink-0 text-xs"
              onClick={() => void copyToClipboard(token).then((ok) => ok && setNotice(t("kbAccess.copied")))}
            >
              {t("kbAccess.copy")}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

export function KbGrantForm({ overview, onDone }: { overview: KBAccessOverview; onDone: () => void }) {
  const { t } = useT();
  const [unit, setUnit] = useState("");
  const [expires, setExpires] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const grantee = Number(unit);
    if (!grantee) return;
    setBusy(true);
    setError("");
    try {
      await grantKBToTenant(overview.kb_id, {
        grantee_tenant_id: grantee,
        ...(expires ? { expires_at: `${expires}T23:59:59+07:00` } : {}),
      });
      setUnit("");
      setExpires("");
      onDone();
    } catch (err) {
      setError(errText(err, t("kbAccess.failed")));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-2">
      <h4 className="text-[13px] font-semibold text-ink">{t("kbAccess.grantTitle")}</h4>
      <p className="caption text-muted">{t("kbGrant.intro")}</p>
      <form onSubmit={submit} className="flex flex-wrap items-end gap-2">
        <label className="block min-w-52 flex-1">
          <span className="caption mb-1 block text-muted">{t("kbGrant.unit")}</span>
          <TenantPicker value={unit} onChange={setUnit} excludeId={overview.owner_tenant_id} required />
        </label>
        <label className="block w-44">
          <span className="caption mb-1 block text-muted">{t("kbGrant.expires")}</span>
          <input type="date" className="input" value={expires} onChange={(e) => setExpires(e.target.value)} />
        </label>
        <button type="submit" disabled={busy || !unit} className="btn btn-primary btn-sm">
          {t("kbGrant.grant")}
        </button>
      </form>
      {error && <p className="body-sm text-error">{error}</p>}
    </div>
  );
}
