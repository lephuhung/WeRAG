"use client";

/* SuperAdmin email: the SMTP account the platform sends its notifications
 * with (new orders to the SuperAdmins, payment confirmations to the buyer,
 * package expiry reminders 14/7/1 days ahead). The values are the email.*
 * system settings; the password is a secret the server never returns. */

import { useCallback, useEffect, useState } from "react";
import { SystemSection } from "@/components/settings/system-section";
import { Notice } from "@/components/settings/notice";
import { FieldLabel } from "@/components/settings/field";
import { Toggle } from "@/components/settings/toggle";
import { useToast } from "@/components/toast";
import { useT } from "@/lib/i18n";
import { getCurrentUser } from "@/lib/api/auth";
import {
  EMAIL_SETTING_KEYS as K,
  getSystemSetting,
  sendTestEmail,
  updateSystemSetting,
  type SMTPSecurity,
} from "@/lib/api/system";

type Form = {
  enabled: boolean;
  host: string;
  port: string;
  username: string;
  fromAddress: string;
  fromName: string;
  security: SMTPSecurity;
};
const emptyForm: Form = { enabled: false, host: "", port: "587", username: "", fromAddress: "", fromName: "", security: "starttls" };

/* The usual port of each security mode, filled in when the mode changes
 * while the port is still another mode's default. */
const DEFAULT_PORT: Record<SMTPSecurity, string> = { none: "25", starttls: "587", tls: "465" };

export default function AdminEmail() {
  const { t } = useT();
  const toast = useToast();
  const [allowed, setAllowed] = useState<boolean | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [form, setForm] = useState<Form>(emptyForm);
  const [base, setBase] = useState<Form>(emptyForm);
  /* masked current password ("" = none saved); a new one typed in `password` */
  const [passwordMasked, setPasswordMasked] = useState("");
  const [password, setPassword] = useState("");
  const [testResult, setTestResult] = useState<{ ok: boolean; text: string } | null>(null);

  const load = useCallback(async () => {
    try {
      const [enabled, host, port, username, pw, fromAddress, fromName, security] = await Promise.all([
        getSystemSetting(K.enabled), getSystemSetting(K.host), getSystemSetting(K.port), getSystemSetting(K.username),
        getSystemSetting(K.password), getSystemSetting(K.fromAddress), getSystemSetting(K.fromName), getSystemSetting(K.security),
      ]);
      const f: Form = {
        enabled: enabled.value === true,
        host: String(host.value ?? ""),
        port: String(port.value ?? "587"),
        username: String(username.value ?? ""),
        fromAddress: String(fromAddress.value ?? ""),
        fromName: String(fromName.value ?? ""),
        security: (["none", "starttls", "tls"].includes(String(security.value)) ? String(security.value) : "starttls") as SMTPSecurity,
      };
      setForm(f);
      setBase(f);
      setPasswordMasked(String(pw.value ?? ""));
      setPassword("");
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : t("email.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    let alive = true;
    getCurrentUser()
      .then((me) => {
        if (!alive) return;
        const ok = me.data?.user?.is_system_admin === true;
        setAllowed(ok);
        if (ok) void load();
      })
      .catch(() => alive && setAllowed(false));
    return () => {
      alive = false;
    };
  }, [load]);

  const dirty = JSON.stringify(form) !== JSON.stringify(base) || password !== "";

  const save = async () => {
    setBusy(true);
    setError("");
    try {
      const port = Math.min(65535, Math.max(1, Math.round(Number(form.port) || 587)));
      // only what changed, so a partial failure leaves the rest as it was
      if (form.host !== base.host) await updateSystemSetting(K.host, form.host.trim());
      if (form.port !== base.port) await updateSystemSetting(K.port, port);
      if (form.security !== base.security) await updateSystemSetting(K.security, form.security);
      if (form.username !== base.username) await updateSystemSetting(K.username, form.username.trim());
      if (form.fromAddress !== base.fromAddress) await updateSystemSetting(K.fromAddress, form.fromAddress.trim());
      if (form.fromName !== base.fromName) await updateSystemSetting(K.fromName, form.fromName.trim());
      if (password !== "") {
        const row = await updateSystemSetting(K.password, password);
        setPasswordMasked(String(row.value ?? ""));
        setPassword("");
      }
      if (form.enabled !== base.enabled) await updateSystemSetting(K.enabled, form.enabled);
      const saved = { ...form, port: String(port) };
      setForm(saved);
      setBase(saved);
      toast.success(t("common.saved"));
    } catch (e) {
      setError(e instanceof Error ? e.message : t("email.saveFailed"));
    } finally {
      setBusy(false);
    }
  };

  const clearPassword = async () => {
    setBusy(true);
    setError("");
    try {
      await updateSystemSetting(K.password, "");
      setPasswordMasked("");
      setPassword("");
      toast.success(t("common.saved"));
    } catch (e) {
      setError(e instanceof Error ? e.message : t("email.saveFailed"));
    } finally {
      setBusy(false);
    }
  };

  const sendTest = async () => {
    setBusy(true);
    setTestResult(null);
    try {
      const r = await sendTestEmail();
      setTestResult(r.sent ? { ok: true, text: t("email.testSent", { to: r.to }) } : { ok: false, text: r.error || t("email.testFailed") });
    } catch (e) {
      setTestResult({ ok: false, text: e instanceof Error ? e.message : t("email.testFailed") });
    } finally {
      setBusy(false);
    }
  };

  const changeSecurity = (security: SMTPSecurity) => {
    const port = Object.values(DEFAULT_PORT).includes(form.port) ? DEFAULT_PORT[security] : form.port;
    setForm({ ...form, security, port });
  };

  return (
    <SystemSection
      allowed={allowed}
      title={t("systemNav.email")}
      deniedMessage={t("email.denied")}
      loading={loading}
      loadingLabel={t("common.loading")}
    >
      <div className="max-w-[720px] space-y-5 py-6">
        <p className="caption text-muted">{t("email.desc")}</p>
        {error && (
          <Notice
            tone="error"
            action={
              <button type="button" className="btn btn-outline btn-sm" onClick={() => void load()}>
                {t("common.retry")}
              </button>
            }
          >
            {error}
          </Notice>
        )}

        <div className="flex items-center justify-between gap-4 rounded-lg border border-hairline px-4 py-3">
          <div className="min-w-0">
            <div className="text-[14px] font-medium text-ink">{t("email.enabled")}</div>
            <div className="caption text-muted">{t("email.enabledDesc")}</div>
          </div>
          <Toggle checked={form.enabled} label={t("email.enabled")} onChange={(v) => setForm({ ...form, enabled: v })} disabled={busy} />
        </div>

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div className="min-w-0">
            <FieldLabel htmlFor="smtp-host">{t("email.host")}</FieldLabel>
            <input id="smtp-host" className="input w-full" value={form.host} placeholder="smtp.gmail.com" autoComplete="off"
              onChange={(e) => setForm({ ...form, host: e.target.value })} />
          </div>
          <div className="grid min-w-0 grid-cols-2 gap-3">
            <div className="min-w-0">
              <FieldLabel htmlFor="smtp-security">{t("email.security")}</FieldLabel>
              <select id="smtp-security" className="input w-full" value={form.security} onChange={(e) => changeSecurity(e.target.value as SMTPSecurity)}>
                <option value="starttls">STARTTLS</option>
                <option value="tls">{t("email.securityTLS")}</option>
                <option value="none">{t("email.securityNone")}</option>
              </select>
            </div>
            <div className="min-w-0">
              <FieldLabel htmlFor="smtp-port">{t("email.port")}</FieldLabel>
              <input id="smtp-port" className="input w-full" type="number" min={1} max={65535} value={form.port}
                onChange={(e) => setForm({ ...form, port: e.target.value })} />
            </div>
          </div>
          <div className="min-w-0">
            <FieldLabel htmlFor="smtp-username">{t("email.username")}</FieldLabel>
            <input id="smtp-username" className="input w-full" value={form.username} autoComplete="off"
              onChange={(e) => setForm({ ...form, username: e.target.value })} />
          </div>
          <div className="min-w-0">
            <FieldLabel htmlFor="smtp-password">{t("email.password")}</FieldLabel>
            <input id="smtp-password" className="input w-full" type="password" autoComplete="new-password" value={password}
              placeholder={passwordMasked ? t("email.passwordKeep") : ""}
              onChange={(e) => setPassword(e.target.value)} />
            {passwordMasked && (
              <div className="caption mt-1 flex items-center gap-2 text-muted">
                <span>{t("email.passwordSet")}</span>
                <button type="button" className="underline hover:text-ink" disabled={busy} onClick={() => void clearPassword()}>
                  {t("email.passwordClear")}
                </button>
              </div>
            )}
          </div>
          <div className="min-w-0">
            <FieldLabel htmlFor="smtp-from-address">{t("email.fromAddress")}</FieldLabel>
            <input id="smtp-from-address" className="input w-full" type="email" value={form.fromAddress} placeholder="no-reply@example.com"
              onChange={(e) => setForm({ ...form, fromAddress: e.target.value })} />
          </div>
          <div className="min-w-0">
            <FieldLabel htmlFor="smtp-from-name">{t("email.fromName")}</FieldLabel>
            <input id="smtp-from-name" className="input w-full" value={form.fromName}
              onChange={(e) => setForm({ ...form, fromName: e.target.value })} />
          </div>
        </div>

        {form.security === "none" && form.username.trim() !== "" && (
          <Notice tone="warning">{t("email.noneWithUser")}</Notice>
        )}

        <div className="flex flex-wrap items-center justify-end gap-3">
          <button type="button" className="btn btn-outline" disabled={busy || dirty || !base.enabled} title={dirty ? t("email.saveFirst") : undefined}
            onClick={() => void sendTest()}>
            {t("email.test")}
          </button>
          <button type="button" className="btn btn-primary" disabled={busy || !dirty} onClick={() => void save()}>
            {t("common.save")}
          </button>
        </div>
        {testResult && (
          <Notice tone={testResult.ok ? "success" : "error"}>
            <span className="break-words">{testResult.text}</span>
          </Notice>
        )}

        <div className="rounded-lg border border-hairline px-4 py-3">
          <div className="mb-1 text-[14px] font-medium text-ink">{t("email.whatTitle")}</div>
          <ul className="caption list-disc space-y-0.5 pl-5 text-muted">
            <li>{t("email.whatOrder")}</li>
            <li>{t("email.whatPaid")}</li>
            <li>{t("email.whatExpiry")}</li>
          </ul>
        </div>
      </div>
    </SystemSection>
  );
}
