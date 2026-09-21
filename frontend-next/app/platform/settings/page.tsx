/* /platform/settings was merged into /platform/system — personal/tenant
 * sections live under the Workspace tab, admin sections on the engine/model/
 * extension tabs. Keep the ?section= contract so old links land correctly. */
"use client";

import { Suspense, useEffect } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { legacySectionTarget } from "@/components/settings/nav-config";

export default function SettingsRedirect() {
  return (
    <Suspense fallback={null}>
      <RedirectBody />
    </Suspense>
  );
}

function RedirectBody() {
  const router = useRouter();
  const params = useSearchParams();

  useEffect(() => {
    router.replace(legacySectionTarget(params.get("section")));
  }, [params, router]);

  return null;
}
