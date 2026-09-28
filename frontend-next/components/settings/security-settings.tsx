/* Account security section of the settings modal: the password-change form
 * that used to live in user-settings-modal. Profile info lives in
 * profile-settings.tsx. */
"use client";

import { useState } from "react";
import { clearTokens } from "@/lib/api-client";
import { changePassword } from "@/lib/api/auth";
import { useT } from "@/lib/i18n";

export function SecuritySettings() {
  const { t } = useT();

  const [pw, setPw] = useState({ current: "", next: "", confirm: "" });
  const [pwSaved, setPwSaved] = useState(false);
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
    setPwSaved(true);
    setPw({ current: "", next: "", confirm: "" });
    // Backend revokes all sessions on password change — force re-login.
    setTimeout(() => {
      clearTokens();
      window.location.href = "/login";
    }, 1200);
  };

  return (
    <div className="flex flex-col gap-7">
      {/* change password */}
      <div>
        <h3 className="title-sm mb-3">{t("userSettings.changePassword")}</h3>
        <div className="flex flex-col gap-3">
          <input
            className="input"
            type="password"
            placeholder={t("userSettings.currentPassword")}
            value={pw.current}
            onChange={(e) => setPw({ ...pw, current: e.target.value })}
          />
          <input
            className="input"
            type="password"
            placeholder={t("userSettings.newPassword")}
            value={pw.next}
            onChange={(e) => setPw({ ...pw, next: e.target.value })}
          />
          <input
            className="input"
            type="password"
            placeholder={t("userSettings.confirmPassword")}
            value={pw.confirm}
            onChange={(e) => setPw({ ...pw, confirm: e.target.value })}
          />
        </div>
        <div className="mt-3 flex items-center gap-3">
          <button
            className="btn btn-primary btn-sm"
            disabled={!pw.current || !pw.next || pw.next !== pw.confirm || pwBusy}
            onClick={() => void changePw()}
          >
            {pwBusy ? "…" : t("userSettings.updatePassword")}
          </button>
          {pw.next && pw.confirm && pw.next !== pw.confirm && (
            <span className="caption text-error">{t("userSettings.pwMismatch")}</span>
          )}
          {pwSaved && (
            <span className="caption text-success">{t("userSettings.pwUpdated")}</span>
          )}
          {pwError && <span className="caption text-error">{pwError}</span>}
        </div>
      </div>
    </div>
  );
}
