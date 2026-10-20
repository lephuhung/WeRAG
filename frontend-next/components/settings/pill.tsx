/* One filter-pill primitive.
 *
 * The pills came in two skins — a solid `bg-ink text-on-primary` chip and
 * a raised `bg-surface-card` chip — with two different focus treatments.
 * This component owns the container, the active state and the focus ring.
 */
"use client";

import type { ReactNode } from "react";

/* The pill bar that groups a set of mutually exclusive filters. */
export function PillGroup({
  children,
  ariaLabel,
  className = "",
}: {
  children: ReactNode;
  ariaLabel?: string;
  className?: string;
}) {
  return (
    <div
      className={`inline-flex items-center gap-0.5 rounded-full border border-hairline bg-surface-strong/60 p-0.5 ${className}`}
      role="group"
      aria-label={ariaLabel}
    >
      {children}
    </div>
  );
}

export function Pill({
  active,
  onClick,
  children,
  title,
  ariaLabel,
  disabled = false,
  className = "",
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
  title?: string;
  ariaLabel?: string;
  disabled?: boolean;
  className?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      title={title}
      aria-label={ariaLabel}
      aria-pressed={active}
      className={`rounded-full px-3 py-1 text-[12px] font-medium transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink disabled:cursor-not-allowed disabled:opacity-50 ${
        active ? "bg-ink text-on-primary" : "text-muted hover:text-ink"
      } ${className}`}
    >
      {children}
    </button>
  );
}

export default Pill;
