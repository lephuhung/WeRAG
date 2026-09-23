"use client";

import { EnvVarsSettings } from "@/components/settings/env-vars-settings";

export default function EnvVarsPage() {
  return (
    <div className="card p-4 sm:p-6 lg:p-8">
      <EnvVarsSettings />
    </div>
  );
}
