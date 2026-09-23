/* Moved to /platform/system/workspace/abbreviations — kept so old links
 * keep working. */
"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

export default function AbbreviationsRedirect() {
  const router = useRouter();
  useEffect(() => {
    router.replace("/platform/system/workspace/abbreviations");
  }, [router]);
  return null;
}
