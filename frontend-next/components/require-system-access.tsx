"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { useAuth } from "@/lib/auth";
import { roleAtLeast, type SettingsRoleKey } from "@/components/settings/nav-config";

/* Client-side gate for /platform/system tabs, mirroring the inline guards on
 * the agents/users pages. minRole "system" (default) requires the platform
 * system-admin flag; a tenant role also lets tenant owners/admins through.
 * Server-side authorization remains authoritative — this only keeps the UI
 * consistent with the hidden tabs. */
export function RequireSystemAccess({
  children,
  minRole = "system",
}: {
  children: React.ReactNode;
  minRole?: SettingsRoleKey;
}) {
  const router = useRouter();
  const auth = useAuth();
  const isSystemAdmin = auth.user?.is_system_admin === true;
  const currentRole =
    auth.memberships.find(
      (m) => String(m.tenant_id) === String(auth.selectedTenantId ?? auth.tenant?.id ?? ""),
    )?.role ?? "";
  const allowed =
    isSystemAdmin ||
    (minRole !== "system" && roleAtLeast(currentRole, minRole));

  useEffect(() => {
    if (auth.ready && auth.user && !allowed) {
      router.replace("/platform/system/workspace");
    }
  }, [auth.ready, auth.user, allowed, router]);

  if (auth.ready && !allowed) return null;
  return <>{children}</>;
}
