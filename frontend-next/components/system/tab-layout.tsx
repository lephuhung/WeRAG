"use client";

import { RequireSystemAccess } from "@/components/require-system-access";
import type { SettingsRoleKey } from "@/components/settings/nav-config";

/* Shared body for /platform/system/<tab> layouts: centered content column,
 * optional role gate for the whole tab (undefined = any signed-in user).
 * Navigation between sections happens on the tab's card hub, not a left
 * sub-nav. */
export function SystemTabLayout({
  minRole,
  children,
}: {
  minRole?: SettingsRoleKey;
  children: React.ReactNode;
}) {
  const body = <div className="mx-auto w-full max-w-[1200px]">{children}</div>;
  if (!minRole) return body;
  return <RequireSystemAccess minRole={minRole}>{body}</RequireSystemAccess>;
}
