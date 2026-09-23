"use client";

import { RequireSystemAccess } from "@/components/require-system-access";
import { KBAccessGrants } from "@/components/settings/kb-access-grants";

export default function SharingPage() {
  return (
    <RequireSystemAccess minRole="owner">
      <div className="card p-4 sm:p-6 lg:p-8">
        <KBAccessGrants />
      </div>
    </RequireSystemAccess>
  );
}
