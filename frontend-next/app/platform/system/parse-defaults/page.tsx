/* Moved to /platform/system/engines/parse-defaults — kept so old links
 * keep working. */
"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

export default function ParseDefaultsRedirect() {
  const router = useRouter();
  useEffect(() => {
    router.replace("/platform/system/engines/parse-defaults");
  }, [router]);
  return null;
}
