/* The /platform/system page tree was folded into the settings modal and the
 * SystemAdmin console — its sections are not standalone routes. This
 * catch-all keeps old deep links working: platform configuration and
 * administration paths go to /platform/admin?section=…, everything else
 * bounces to /platform and opens the modal at the matching section
 * (SECTION_ROUTES maps a path → section key). */
"use client";

import { useEffect } from "react";
import { usePathname, useRouter } from "next/navigation";
import { SECTION_ROUTES } from "@/components/settings-modal";
import { ADMIN_SECTION_ROUTES } from "@/components/admin/admin-sections";
import { adminSectionHref } from "@/components/admin/admin-routes";

export default function SystemRedirect() {
  const router = useRouter();
  const pathname = usePathname();

  useEffect(() => {
    const adminKey = ADMIN_SECTION_ROUTES[pathname];
    if (adminKey) {
      router.replace(adminSectionHref(adminKey));
      return;
    }
    router.replace("/platform");
    /* account-menu (always mounted) listens for this event and opens the
     * modal at the given section. Dispatch after the navigation settles so
     * the new page's listener is attached. */
    const timer = window.setTimeout(() => {
      window.dispatchEvent(
        new CustomEvent("weknora:open-settings", {
          detail: SECTION_ROUTES[pathname] ?? "general",
        }),
      );
    }, 150);
    return () => window.clearTimeout(timer);
  }, [pathname, router]);

  return null;
}
