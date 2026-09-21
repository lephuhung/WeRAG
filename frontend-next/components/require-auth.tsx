"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { useAuth } from "@/lib/auth";
import { BrandLogo } from "@/components/brand-logo";

export function RequireAuth({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const { isLoggedIn, ready } = useAuth();

  useEffect(() => {
    if (ready && !isLoggedIn) router.replace("/login");
  }, [ready, isLoggedIn, router]);

  if (!ready) {
    return (
      <div className="flex h-screen items-center justify-center">
        <div className="flex flex-col items-center gap-5">
          <div className="relative flex h-[52px] w-[52px] items-center justify-center">
            <span className="absolute inset-0 animate-spin rounded-full border-2 border-sky-500/20 border-t-sky-400" />
            <BrandLogo size={40} priority />
          </div>
          <span className="caption text-muted">Loading workspace…</span>
        </div>
      </div>
    );
  }
  if (!isLoggedIn) return null;
  return <>{children}</>;
}
