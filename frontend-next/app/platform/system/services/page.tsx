/* Moved to /platform/system/engines — kept so old links keep working.
 * ?category=<key> mapped to the matching sub-route. */
"use client";

import { Suspense, useEffect } from "react";
import { useRouter, useSearchParams } from "next/navigation";

const CATEGORY_ROUTES: Record<string, string> = {
  vector: "/platform/system/engines/vector",
  storage: "/platform/system/engines/storage",
  search: "/platform/system/engines/search",
  sandbox: "/platform/system/engines/sandbox",
};

export default function ServicesRedirect() {
  return (
    <Suspense fallback={null}>
      <Body />
    </Suspense>
  );
}

function Body() {
  const router = useRouter();
  const params = useSearchParams();

  useEffect(() => {
    const cat = params.get("category") ?? "";
    router.replace(CATEGORY_ROUTES[cat] ?? "/platform/system/engines");
  }, [params, router]);

  return null;
}
