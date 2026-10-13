/* Sections of the SystemAdmin console at /platform/admin: the dashboard and
 * the administration pages, which used to live in the settings modal's
 * "Administration" group and had too little room for their tables. System
 * configuration (models, engines, queues) stays in the modal. Every section
 * is SystemAdmin-only (server guards stay authoritative). `route` is the
 * old /platform/system path — the catch-all redirect maps it to ?section=. */
"use client";

import { lazy, type ComponentType, type ReactNode } from "react";
import type { LocaleKey } from "@/lib/i18n";
import {
  IconCreditCard,
  IconDocReader,
  IconFileSearch,
  IconIdCard,
  IconOrg,
  IconPulse,
  IconUser,
} from "@/components/icons";

const AdminDashboardPage = lazy(() => import("@/components/settings/pages/admin-dashboard"));
const AdminUsersPage = lazy(() => import("@/components/settings/pages/admin-users"));
const AdminTenantsPage = lazy(() => import("@/components/settings/pages/admin-tenants"));
const AdminLogsPage = lazy(() => import("@/components/settings/pages/admin-logs"));
const AdminLegalDocumentsPage = lazy(() => import("@/components/settings/pages/admin-legal-documents"));
const AdminUsagePage = lazy(() => import("@/components/settings/pages/admin-usage"));
const AdminBillingPage = lazy(() => import("@/components/settings/pages/admin-billing"));

export interface AdminSection {
  key: string;
  labelKey?: LocaleKey;
  fallback: string;
  icon: ComponentType<{ className?: string }>;
  render: () => ReactNode;
  route?: string;
}

export interface AdminSectionGroup {
  key: string;
  labelKey: LocaleKey;
  items: AdminSection[];
}

const SYS = "/platform/system";

export const ADMIN_GROUPS: AdminSectionGroup[] = [
  {
    key: "overview",
    labelKey: "adminPage.overview",
    items: [
      { key: "dashboard", labelKey: "adminPage.dashboard", fallback: "Dashboard", icon: IconPulse, render: () => <AdminDashboardPage />, route: `${SYS}/admin` },
    ],
  },
  {
    key: "admin",
    labelKey: "systemNav.admin",
    items: [
      { key: "tenants", labelKey: "systemNav.tenants", fallback: "Units", icon: IconOrg, render: () => <AdminTenantsPage />, route: `${SYS}/admin/tenants` },
      { key: "users", labelKey: "systemNav.users", fallback: "Users", icon: IconUser, render: () => <AdminUsersPage />, route: `${SYS}/admin/users` },
      { key: "logs", labelKey: "systemNav.logs", fallback: "Audit logs", icon: IconDocReader, render: () => <AdminLogsPage />, route: `${SYS}/admin/logs` },
      { key: "legal-documents", labelKey: "systemNav.legalDocuments", fallback: "Legal documents", icon: IconFileSearch, render: () => <AdminLegalDocumentsPage />, route: `${SYS}/admin/legal-documents` },
      { key: "usage", labelKey: "systemNav.usage", fallback: "Usage packages", icon: IconIdCard, render: () => <AdminUsagePage />, route: `${SYS}/admin/usage` },
      { key: "billing", labelKey: "systemNav.billing", fallback: "Billing & promo codes", icon: IconCreditCard, render: () => <AdminBillingPage />, route: `${SYS}/admin/billing` },
    ],
  },
];

/* Old /platform/system route → console section key. */
export const ADMIN_SECTION_ROUTES: Record<string, string> = Object.fromEntries(
  ADMIN_GROUPS.flatMap((g) => g.items)
    .filter((it) => it.route)
    .map((it) => [it.route as string, it.key]),
);
