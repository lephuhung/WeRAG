/* Platform-wide parse defaults — the editor body is shared with the
 * superadmin-only "Parse defaults" modal on /platform/knowledge-bases. This
 * route stays for deep-linking and legacy redirects. */
"use client";

import { RequireSystemAccess } from "@/components/require-system-access";
import { ParseDefaultsEditor } from "@/components/knowledge/parse-defaults-editor";

export default function ParseDefaultsPage() {
  return (
    <RequireSystemAccess>
      <div className="mx-auto w-full max-w-[1200px]">
        <ParseDefaultsEditor />
      </div>
    </RequireSystemAccess>
  );
}
