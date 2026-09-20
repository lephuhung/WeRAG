"use client";

import { useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { clearTokens } from "@/lib/api-client";
import { changePassword, getCurrentUser } from "@/lib/api/auth";
import { useT } from "@/lib/i18n";

type Section = "profile" | "security";

export function UserSettingsModal({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useT();
  const [section, setSection] = useState<Section>("profile");

  const SECTIONS: { id: Section; label: string }[] = [
    { id: "profile", label: t("userSettings.profile") },
    { id: "security", label: t("userSettings.security") },
  ];

  /* profile — read-only; the backend has no profile-update endpoint yet */
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");

  useEffect(() => {
    if (!open) return;
    let alive = true;
    getCurrentUser()
      .then((res) => {
        if (!alive) return;
        setName(res.data?.user?.username ?? "");
        setEmail(res.data?.user?.email ?? "");
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [open]);

  /* security */
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
      setPwError(res.message || "Update failed");
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
    <Modal open={open} title={t("userSettings.title")} onClose={onClose} width="w-[720px]">
      <div className="flex min-h-[420px] gap-6">
        {/* section nav */}
        <div className="w-[160px] shrink-0 border-r border-hairline pr-4">
          {SECTIONS.map((s) => (
            <button
              key={s.id}
              onClick={() => setSection(s.id)}
              className={`nav-item mb-0.5 ${section === s.id ? "active" : ""}`}
            >
              {s.label}
            </button>
          ))}
        </div>

        {/* content */}
        <div className="min-w-0 flex-1">
          {section === "profile" && (
            <div className="flex flex-col gap-5">
              {/* avatar — initials only; no avatar endpoint on the backend */}
              <div className="flex items-center gap-4">
                <div className="flex h-16 w-16 shrink-0 items-center justify-center overflow-hidden rounded-full bg-surface-strong text-[18px] font-medium text-ink">
                  {(name || "?").slice(0, 2).toUpperCase()}
                </div>
              </div>

              <label className="block">
                <span className="caption mb-1.5 block text-muted">
                  {t("userSettings.displayName")}
                </span>
                <input className="input" value={name} disabled />
              </label>
              <label className="block">
                <span className="caption mb-1.5 block text-muted">
                  {t("userSettings.email")}
                </span>
                <input className="input" type="email" value={email} disabled />
              </label>
              <p className="caption text-muted-soft">
                Profile editing is not supported by this deployment yet.
              </p>
            </div>
          )}

          {section === "security" && (
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
          )}
        </div>
      </div>
    </Modal>
  );
}
