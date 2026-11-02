/* Account security section of the settings modal: the password-change form
 * that used to live in user-settings-modal. Profile info lives in
 * profile-settings.tsx. */
"use client";

import { useState } from "react";
import { clearTokens, signInPath } from "@/lib/api-client";
import { changePassword } from "@/lib/api/auth";
import { TwoFactorSettings } from "@/components/settings/two-factor-settings";
import { useT } from "@/lib/i18n";
import { useAuth } from "@/lib/auth";
import { ConsolePage } from "@/components/settings/console-page";
import { FieldLabel } from "@/components/settings/field";
import { Notice } from "@/components/settings/notice";
import { useToast } from "@/components/toast";

export function SecuritySettings() {
  const { t } = useT();
  const { user } = useAuth();
  const toast = useToast();

  const [pw, setPw] = useState({ current: "", next: "", confirm: "" });
  const [pwError, setPwError] = useState<string | null>(null);
  const [pwBusy, setPwBusy] = useState(false);
  const changePw = async () => {
    if (!pw.current || !pw.next || pw.next !== pw.confirm) return;
    setPwBusy(true);
    setPwError(null);
    // changePassword resolves { success:false, message } on failure instead of
    // throwing — check the envelope, not exceptions.
    const res = await changePassword({ old_password: pw.current, new_password: pw.next });
    setPwBusy(false);
    if (!res.success) {
      setPwError(res.message || t("userSettings.updateFailed"));
      return;
    }
    toast.success(t("userSettings.pwUpdated"));
    setPw({ current: "", next: "", confirm: "" });
    // Backend revokes all sessions on password change — force re-login.
    setTimeout(() => {
      clearTokens();
      window.location.href = signInPath();
    }, 1200);
  };

  return (
    <ConsolePage size="form">
      <div className="flex flex-col gap-7">
        {/* change password */}
        <form onSubmit={(event) => event.preventDefault()}>
          <h3 className="title-sm mb-3">{t("userSettings.changePassword")}</h3>
          <div className="flex flex-col gap-3">
            <div>
              <FieldLabel htmlFor="security-email">{t("userSettings.email")}</FieldLabel>
              <input
                id="security-email"
                className="input"
                type="email"
                name="username"
                autoComplete="username"
                value={user?.email ?? ""}
                readOnly
              />
            </div>
            <div>
              <FieldLabel htmlFor="security-current-password">{t("userSettings.currentPassword")}</FieldLabel>
              <input
                id="security-current-password"
                className="input"
                type="password"
                name="current-password"
                autoComplete="current-password"
                placeholder={t("userSettings.currentPassword")}
                value={pw.current}
                onChange={(e) => setPw({ ...pw, current: e.target.value })}
              />
            </div>
            <div>
              <FieldLabel htmlFor="security-new-password">{t("userSettings.newPassword")}</FieldLabel>
              <input
                id="security-new-password"
                className="input"
                type="password"
                name="new-password"
                autoComplete="new-password"
                placeholder={t("userSettings.newPassword")}
                value={pw.next}
                onChange={(e) => setPw({ ...pw, next: e.target.value })}
              />
            </div>
            <div>
              <FieldLabel htmlFor="security-confirm-password">{t("userSettings.confirmPassword")}</FieldLabel>
              <input
                id="security-confirm-password"
                className="input"
                type="password"
                name="confirm-password"
                autoComplete="new-password"
                placeholder={t("userSettings.confirmPassword")}
                value={pw.confirm}
                onChange={(e) => setPw({ ...pw, confirm: e.target.value })}
              />
            </div>
          </div>
          {pwError && (
            <div className="mt-3">
              <Notice tone="error">{pwError}</Notice>
            </div>
          )}
          <div className="mt-3 flex items-center gap-3">
            <button
              type="button"
              className="btn btn-primary btn-sm"
              disabled={!pw.current || !pw.next || pw.next !== pw.confirm || pwBusy}
              onClick={() => void changePw()}
            >
              {pwBusy ? t("common.loading") : t("userSettings.updatePassword")}
            </button>
            {pw.next && pw.confirm && pw.next !== pw.confirm && (
              <span className="caption text-error">{t("userSettings.pwMismatch")}</span>
            )}
          </div>
        </form>

        <div className="border-t border-hairline pt-7">
          <TwoFactorSettings />
        </div>
      </div>
    </ConsolePage>
  );
}
