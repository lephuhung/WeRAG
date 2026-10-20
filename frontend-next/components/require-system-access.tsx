"use client";

import { useAuth } from "@/lib/auth";
import { roleAtLeast, type SettingsRoleKey } from "@/components/settings/nav-config";
import { DeniedView } from "@/components/settings/system-section";

/* Client-side gate for the platform-wide surfaces, mirroring the inline
 * guards on the agents/users pages. minRole "system" (default) requires the
 * platform system-admin flag; a tenant role also lets tenant owners/admins
 * through.
 * Server-side authorization remains authoritative — this only keeps the UI
 * consistent with the hidden tabs.
 *
 * A user without the role gets the same "access denied" block as the admin
 * pages (see components/settings/system-section.tsx) instead of a silent
 * redirect that told them nothing. */
export function RequireSystemAccess({
  children,
  minRole = "system",
}: {
  children: React.ReactNode;
  minRole?: SettingsRoleKey;
}) {
  const auth = useAuth();
  const isSystemAdmin = auth.user?.is_system_admin === true;
  const currentRole =
    auth.memberships.find(
      (m) => String(m.tenant_id) === String(auth.selectedTenantId ?? auth.tenant?.id ?? ""),
    )?.role ?? "";
  const allowed =
    isSystemAdmin ||
    (minRole !== "system" && roleAtLeast(currentRole, minRole));

  if (auth.ready && auth.user && !allowed) {
    return <DeniedView />;
  }
  return <>{children}</>;
}
