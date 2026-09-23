/* Moved to /platform/system/extensions/agents — kept so old links keep
 * working. */
"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

export default function AgentsRedirect() {
  const router = useRouter();
  useEffect(() => {
    router.replace("/platform/system/extensions/agents");
  }, [router]);
  return null;
}
