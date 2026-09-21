"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth";

export default function AgentsRedirect() {
  const { user, ready } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (!ready) return;
    if (user?.is_system_admin) {
      router.replace("/platform/system/agents");
    } else {
      router.replace("/platform/knowledge-bases");
    }
  }, [user, ready, router]);

  return null;
}
