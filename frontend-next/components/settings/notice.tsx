/* One banner for every action result in the settings/admin surfaces.
 *
 * Before this component the app had two styling families for the same
 * thing: raw rose/emerald Tailwind classes on one side, token-based
 * classes on the other. Both are replaced by <Notice>, which derives its
 * colours from the semantic tokens (--color-error/--color-success/
 * --color-warning/--color-info) and their translucent -surface tints, so
 * the banner follows the theme (light, dark and the Anthropic preset).
 *
 * Convention used across settings/admin:
 *   - server/action error  → <Notice tone="error">
 *   - server/action success → toast (see components/toast.tsx)
 *   - validation error     → inline text next to the field
 */
"use client";

import type { ReactNode } from "react";

export type NoticeTone = "error" | "success" | "warning" | "info";

const TONE_CLASS: Record<NoticeTone, string> = {
  error: "border-error/30 bg-error-surface text-error",
  success: "border-success/30 bg-success-surface text-success",
  warning: "border-warning/30 bg-warning-surface text-warning",
  info: "border-info/30 bg-info-surface text-info",
};

export function Notice({
  tone = "info",
  title,
  children,
  action,
  className = "",
}: {
  tone?: NoticeTone;
  /* Optional bold lead-in; the body text follows below it. */
  title?: ReactNode;
  children?: ReactNode;
  /* Trailing element (a retry button, a link …). */
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div
      role={tone === "error" ? "alert" : "status"}
      aria-live={tone === "error" ? "assertive" : "polite"}
      className={`flex flex-wrap items-center gap-2 rounded-[12px] border px-3.5 py-2.5 text-[13px] leading-relaxed ${TONE_CLASS[tone]} ${className}`}
    >
      {title ? <span className="font-medium">{title}</span> : null}
      {children ? <span className="min-w-0 flex-1">{children}</span> : null}
      {action ? <span className="ml-auto shrink-0">{action}</span> : null}
    </div>
  );
}

export default Notice;
