/* One container width for the console/settings pages.
 *
 * Every page guessed its own max-width (1400 / 1200 / 1100 / 1080 / 860 /
 * 760 px). The console pages all use the wide size; the in-modal
 * settings sections use the form size (the modal itself is ~1120px wide,
 * so a narrower column keeps the reading measure sane).
 */
"use client";

import type { ReactNode } from "react";

const WIDTHS = {
  wide: "max-w-[1400px]",
  form: "max-w-[860px]",
} as const;

export type ConsolePageSize = keyof typeof WIDTHS;

export function ConsolePage({
  children,
  size = "wide",
  className = "",
}: {
  children: ReactNode;
  size?: ConsolePageSize;
  className?: string;
}) {
  return <div className={`mx-auto w-full ${WIDTHS[size]} ${className}`}>{children}</div>;
}

export default ConsolePage;
