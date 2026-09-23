"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth";

export default function System() {
  const router = useRouter();
  const auth = useAuth();

  useEffect(() => {
    if (!auth.ready) return;
    router.replace(
      auth.user?.is_system_admin
        ? "/platform/system/admin"
        : "/platform/system/workspace",
    );
  }, [auth.ready, auth.user, router]);

  return null;
}
