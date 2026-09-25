"use client";

import { RequireSystemAccess } from "@/components/require-system-access";
import { ApiKeysSection } from "@/components/settings/api-keys";

export default function ApiKeysPage() {
  return (
    <RequireSystemAccess minRole="admin">
      <div className="card p-4 sm:p-6 lg:p-8">
        <ApiKeysSection />
      </div>
    </RequireSystemAccess>
  );
}
