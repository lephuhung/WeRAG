"use client";

import { RequireSystemAccess } from "@/components/require-system-access";
import { MemoryWorkspaceSettings } from "@/components/settings/memory-workspace-settings";

export default function MemoryPage() {
  return (
    <RequireSystemAccess minRole="admin">
      <div className="card p-4 sm:p-6 lg:p-8">
        <MemoryWorkspaceSettings />
      </div>
    </RequireSystemAccess>
  );
}
