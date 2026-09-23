"use client";

import { RequireSystemAccess } from "@/components/require-system-access";
import { TenantMembers } from "@/components/settings/tenant-members";

export default function MembersPage() {
  return (
    <RequireSystemAccess minRole="admin">
      <div className="card p-4 sm:p-6 lg:p-8">
        <TenantMembers />
      </div>
    </RequireSystemAccess>
  );
}
