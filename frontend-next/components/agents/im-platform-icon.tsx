/* Per-platform glyphs for IM channels — simplified monochrome marks in the
 * app's stroke style (24px, 1.6 stroke) so they sit next to the existing
 * icon set. Exact brand logos are intentionally not reproduced. */
"use client";

import { IconChat } from "@/components/icons";

export function IMPlatformIcon({ platform, className }: {
  platform: string;
  className?: string;
}) {
  const c = className ?? "h-5 w-5";
  switch (platform) {
    case "telegram":
      return (
        <svg viewBox="0 0 24 24" className={c} fill="currentColor" aria-hidden>
          <path d="M21.9 4.6 18.9 19c-.2 1-.8 1.2-1.6.8l-4.5-3.3-2.2 2.1c-.24.24-.44.44-.9.44l.32-4.6L18.3 6.5c.36-.32-.08-.5-.56-.18L7.5 12.9l-4.4-1.4c-.96-.3-.98-.96.2-1.42l17.2-6.6c.8-.3 1.5.18 1.4 1.12z" />
        </svg>
      );
    default:
      return <IconChat className={c} />;
  }
}

/* Human label — single source for selects and cards. */
export const IM_PLATFORM_LABELS: Record<string, string> = {
  telegram: "Telegram",
};
