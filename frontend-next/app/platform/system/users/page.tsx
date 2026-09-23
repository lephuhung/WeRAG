/* Moved to /platform/system/admin/users — kept so old links keep working. */
"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

export default function UsersRedirect() {
  const router = useRouter();
  useEffect(() => {
    router.replace("/platform/system/admin/users");
  }, [router]);
  return null;
}
