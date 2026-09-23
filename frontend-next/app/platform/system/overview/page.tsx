/* Moved to /platform/system/admin — kept so old links keep working. */
"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

export default function OverviewRedirect() {
  const router = useRouter();
  useEffect(() => {
    router.replace("/platform/system/admin");
  }, [router]);
  return null;
}
