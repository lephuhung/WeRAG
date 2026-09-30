"use client";

import { useEffect, useRef } from "react";
import { usePathname, useRouter } from "next/navigation";
import { useToast } from "@/components/toast";
import { useT } from "@/lib/i18n";
import { subscribeSessionCompleted } from "@/lib/session-activity";

/* Toast when a chat the user left mid-generation finishes in the background.
 * The sidebar polls detached sessions every 5s; the completion fires through
 * subscribeSessionCompleted. Clicking the toast reopens the chat. */
export function SessionCompleteToasts({ titles }: { titles: ReadonlyMap<string, string> }) {
  const toast = useToast();
  const { t } = useT();
  const router = useRouter();
  const pathname = usePathname();

  const titlesRef = useRef(titles);
  titlesRef.current = titles;
  const navRef = useRef({ router, pathname, t });
  navRef.current = { router, pathname, t };

  useEffect(
    () =>
      subscribeSessionCompleted((sessionId) => {
        const { router, pathname, t } = navRef.current;
        if (pathname === `/platform/chat/${sessionId}`) return;
        const title = titlesRef.current.get(sessionId);
        const message = title
          ? t("nav.sessionCompletedNamed", { title })
          : t("nav.sessionCompleted");
        toast.info(message, () => router.push(`/platform/chat/${sessionId}`));
      }),
    [toast],
  );

  return null;
}
