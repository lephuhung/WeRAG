/* Moved to /platform/system/extensions/abbreviations — kept so old links
 * work. */
"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

export default function AbbreviationsRedirect() {
  const router = useRouter();
  useEffect(() => {
    router.replace("/platform/system/extensions/abbreviations");
  }, [router]);
  return null;
}
