"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { useAuth } from "@/lib/auth";

export function RequireAuth({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const { isLoggedIn, ready } = useAuth();

  useEffect(() => {
    if (ready && !isLoggedIn) router.replace("/login");
  }, [ready, isLoggedIn, router]);

  if (!ready) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <span className="caption text-muted">Loading workspace…</span>
      </div>
    );
  }
  if (!isLoggedIn) return null;
  return <>{children}</>;
}
