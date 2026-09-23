/* Moved to /platform/system/extensions/envvars — kept so old links work. */
"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

export default function EnvVarsRedirect() {
  const router = useRouter();
  useEffect(() => {
    router.replace("/platform/system/extensions/envvars");
  }, [router]);
  return null;
}
