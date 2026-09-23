"use client";

import { RequireSystemAccess } from "@/components/require-system-access";
import { TenantOrgs } from "@/components/settings/tenant-orgs";

export default function OrgsPage() {
  return (
    <RequireSystemAccess minRole="admin">
      <div className="card p-4 sm:p-6 lg:p-8">
        <TenantOrgs />
      </div>
    </RequireSystemAccess>
  );
}
