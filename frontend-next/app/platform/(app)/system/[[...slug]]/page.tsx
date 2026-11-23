/* The /platform/system page tree was folded into the settings modal and the
 * SystemAdmin console — its sections are not standalone routes. This
 * catch-all keeps old deep links working: platform configuration and
 * administration paths go to /platform/admin?section=…, everything else
 * bounces to /platform and opens the modal at the matching section
 * (SECTION_ROUTES maps a path → section key), handing it the link's query
 * (e.g. ?status=pending on the abbreviations section). A Tổ chức plan
 * link for someone without an organization goes through onboarding. */
"use client";

import { useEffect } from "react";
import { usePathname, useRouter } from "next/navigation";
import { SECTION_ROUTES } from "@/components/settings-modal";
import { ADMIN_SECTION_ROUTES } from "@/components/admin/admin-sections";
import { adminSectionHref } from "@/components/admin/admin-routes";
import { markDeepLinkArrival, queueSettingsRedirect } from "@/lib/settings-deep-link";
import { checkoutFromSearch, withNext } from "@/lib/plan-checkout";
import { useAuth } from "@/lib/auth";

export default function SystemRedirect() {
  const router = useRouter();
  const pathname = usePathname();
  const auth = useAuth();

  useEffect(() => {
    const adminKey = ADMIN_SECTION_ROUTES[pathname];
    if (adminKey) {
      router.replace(adminSectionHref(adminKey));
      return;
    }
    const section = SECTION_ROUTES[pathname] ?? "general";
    /* A Tổ chức plan picked on the pricing table by someone in no
     * organization: create one first (onboarding comes back here), on
     * every path — sign-up, sign-in, or already signed in. A member who
     * is not an admin goes on; the shop says why it cannot sell it. */
    if (
      section === "billing" &&
      checkoutFromSearch(window.location.search)?.scope === "tenant" &&
      auth.memberships.length === 0 &&
      !auth.user?.is_system_admin &&
      auth.canCreateTenant
    ) {
      router.replace(withNext("/onboarding/workspace", `${pathname}${window.location.search}`));
      return;
    }
    markDeepLinkArrival(window.sessionStorage);
    queueSettingsRedirect(window.sessionStorage, section, window.location.search);
    router.replace("/platform/creatChat");
    // runs once per visit: the page navigates away at once
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pathname, router]);

  return null;
}
