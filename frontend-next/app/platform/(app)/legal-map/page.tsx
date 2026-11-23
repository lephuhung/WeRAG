"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect } from "react";

/* The map lives under the validity lookup (one search box for both);
 * old links land here and move on with their document. */
function Redirect() {
  const router = useRouter();
  const params = useSearchParams();
  useEffect(() => {
    const sp = new URLSearchParams();
    for (const k of ["doc", "depth", "q"]) {
      const v = params.get(k);
      if (v) sp.set(k, v);
    }
    router.replace(`/platform/legal-lookup${sp.toString() ? "?" + sp.toString() : ""}`);
  }, [router, params]);
  return null;
}

export default function LegalMapPage() {
  return (
    <Suspense>
      <Redirect />
    </Suspense>
  );
}
