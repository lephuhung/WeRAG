/* Account profile section of the settings modal: avatar, display name,
 * email, role and workspace — read-only; the backend has no profile-update
 * endpoint yet. Ported from the old user-settings-modal profile tab. */
"use client";

import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";

export function ProfileSettings() {
  const { t } = useT();
  const auth = useAuth();

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
      {/* avatar — initials only; no avatar endpoint on the backend */}
      <div className="flex items-center gap-4">
        <div className="flex h-16 w-16 shrink-0 items-center justify-center overflow-hidden rounded-full bg-surface-strong text-[18px] font-medium text-ink">
          {(name || "?").slice(0, 2).toUpperCase()}
        </div>
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
        <input className="input" value={name} disabled />
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
    </div>
  );
}
