/* Moved to /platform/system/extensions/browserconnection — kept so old
 * links work. */
"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

export default function BrowserConnectionRedirect() {
  const router = useRouter();
  useEffect(() => {
    router.replace("/platform/system/extensions/browserconnection");
  }, [router]);
  return null;
}
