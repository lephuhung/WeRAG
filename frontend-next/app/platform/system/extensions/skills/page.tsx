"use client";

import { SkillsSettings } from "@/components/settings/skills-settings";
import { RequireSystemAccess } from "@/components/require-system-access";

export default function SkillsPage() {
  return (
    <RequireSystemAccess minRole="system">
      <div className="card p-6">
        <SkillsSettings />
      </div>
    </RequireSystemAccess>
  );
}
