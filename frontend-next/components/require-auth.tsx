"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { BrandLogo } from "@/components/brand-logo";
import { loginPathFor } from "@/lib/safe-next";

export function RequireAuth({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const { isLoggedIn, ready } = useAuth();
  const { t } = useT();

  useEffect(() => {
    // keep the page (e.g. a link from an email) for after the sign-in
    if (ready && !isLoggedIn) router.replace(loginPathFor(window.location.pathname, window.location.search));
  }, [ready, isLoggedIn, router]);

  if (!ready) {
    return (
      <div className="flex h-screen items-center justify-center">
        <div className="flex flex-col items-center gap-5">
          {/* The brand logo is square — a spinning ring around it reads as a
              broken circle. A gentle breathing pulse suits it better. */}
          <div className="workspace-loading-pulse">
            <BrandLogo size={48} priority />
          </div>
          <span className="caption animate-pulse text-muted">{t("workspace.loading")}</span>
        </div>
      </div>
    );
  }
  if (!isLoggedIn) return null;
  return <>{children}</>;
}
