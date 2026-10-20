/* Account profile section of the settings modal: avatar (uploadable), display
 * name, email, role and workspace. Ported from the old user-settings-modal
 * profile tab; avatar upload uses POST /auth/me/avatar. */
"use client";

import { useEffect, useRef, useState } from "react";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { updateMyProfile, uploadMyAvatar } from "@/lib/api/auth";
import { useAvatarUrl } from "@/lib/avatar";
import { ApiError } from "@/lib/api-client";
import { WorkContextForm } from "@/components/settings/work-context-form";
import { useToast } from "@/components/toast";
import { Notice } from "@/components/settings/notice";

const AVATAR_MAX_BYTES = 5 * 1024 * 1024;

export function ProfileSettings() {
  const { t } = useT();
  const auth = useAuth();
  const toast = useToast();
  const [nameDraft, setNameDraft] = useState(auth.user?.username ?? "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  /* Avatar upload state lives beside the name form: uploading shows a
   * spinner on the disc, failures surface under it like name errors. */
  const [uploadingAvatar, setUploadingAvatar] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const avatarUrl = useAvatarUrl(auth.user?.avatar);

  /* Re-pull the profile when the section opens so a username/email changed
   * elsewhere (admin reset, another device) shows up without re-login. */
  useEffect(() => {
    void auth.refreshMe();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  /* Follow external refreshes while nothing is being edited. */
  useEffect(() => {
    if (!saving) setNameDraft(auth.user?.username ?? "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [auth.user?.username]);

  const saveName = async () => {
    const next = nameDraft.trim();
    if (!next || next === (auth.user?.username ?? "")) return;
    setSaving(true);
    setError("");
    try {
      const res = await updateMyProfile(next);
      if (!res.success) throw new Error(res.message || t("common.error"));
      await auth.refreshMe();
      toast.success(t("common.saved"));
    } catch (e) {
      setError(e instanceof ApiError ? e.message : e instanceof Error ? e.message : t("common.error"));
    } finally {
      setSaving(false);
    }
  };

  const pickAvatar = () => fileInputRef.current?.click();

  const onAvatarPicked = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    if (file.size > AVATAR_MAX_BYTES) {
      setError(t("userSettings.avatarTooLarge"));
      return;
    }
    setUploadingAvatar(true);
    setError("");
    try {
      const res = await uploadMyAvatar(file);
      if (!res.success) throw new Error(res.message || t("common.error"));
      await auth.refreshMe();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("common.error"));
    } finally {
      setUploadingAvatar(false);
    }
  };

  const name = auth.user?.username ?? "";
  const email = auth.user?.email ?? "";
  const isSystemAdmin = auth.user?.is_system_admin === true;
  const role = auth.memberships.find(
    (m) => String(m.tenant_id) === String(auth.selectedTenantId ?? auth.tenant?.id ?? ""),
  )?.role;
  const roleLabel = isSystemAdmin
    ? t("acct.roleSystem")
    : role === "admin" || role === "owner"
      ? t("acct.roleAdmin")
      : t("acct.roleMember");

  return (
    <div className="flex flex-col gap-5">
      {/* avatar — click to upload; initials on a tinted disc while no image
       * has been set or while the blob download is in flight */}
      <div className="flex items-center gap-4">
        <button
          type="button"
          onClick={pickAvatar}
          disabled={uploadingAvatar}
          title={t("userSettings.changePhoto")}
          className="group relative h-16 w-16 shrink-0 overflow-hidden rounded-full outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink"
        >
          {avatarUrl ? (
            // eslint-disable-next-line @next/next/no-img-element -- authenticated blob URL, not a Next asset
            <img src={avatarUrl} alt={name} className="h-16 w-16 object-cover" />
          ) : (
            <span className="flex h-16 w-16 items-center justify-center bg-gradient-to-br from-info to-accent text-[18px] font-semibold text-white">
              {(name || "?").slice(0, 2).toUpperCase()}
            </span>
          )}
          <span
            className={`absolute inset-0 flex items-center justify-center bg-black/40 text-[11px] font-medium text-white transition-opacity ${
              uploadingAvatar ? "opacity-100" : "opacity-0 group-hover:opacity-100"
            }`}
          >
            {uploadingAvatar ? "…" : t("userSettings.changePhoto")}
          </span>
        </button>
        <input
          ref={fileInputRef}
          type="file"
          accept="image/png,image/jpeg,image/webp,image/gif"
          className="hidden"
          onChange={(e) => void onAvatarPicked(e)}
        />
        <div className="min-w-0">
          <div className="truncate text-[15px] font-medium text-ink">{name}</div>
          <div className="mt-1 flex flex-wrap items-center gap-1.5">
            <span className="badge-pill border border-hairline bg-surface-strong text-muted">
              {roleLabel}
            </span>
            {auth.tenant?.name && (
              <span className="badge-pill border border-hairline bg-surface-strong text-muted">
                {auth.tenant.name}
              </span>
            )}
          </div>
        </div>
      </div>

      <label className="block">
        <span className="caption mb-1.5 block text-muted">
          {t("userSettings.displayName")}
        </span>
        <div className="flex gap-2">
          <input
            className="input"
            value={nameDraft}
            maxLength={50}
            onChange={(e) => {
              setNameDraft(e.target.value);
              setError("");
            }}
          />
          <button
            className="btn btn-primary shrink-0"
            onClick={() => void saveName()}
            disabled={saving || !nameDraft.trim() || nameDraft.trim() === (auth.user?.username ?? "")}
          >
            {saving ? "…" : t("common.save")}
          </button>
        </div>
        {error && <Notice tone="error" className="mt-1.5">{error}</Notice>}
      </label>
      <label className="block">
        <span className="caption mb-1.5 block text-muted">
          {t("userSettings.email")}
        </span>
        <input className="input" type="email" value={email} disabled />
      </label>
      <p className="caption text-muted-soft">
        {t("userSettings.profileReadonly")}
      </p>
      <div className="border-t border-hairline pt-4">
        <h3 className="body-md mb-2 font-semibold text-ink">{t("workCtx.title")}</h3>
        <WorkContextForm />
      </div>
    </div>
  );
}
