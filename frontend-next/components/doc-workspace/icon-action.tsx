"use client";

/* An icon-only button for the narrow chat column (as in the Word task
 * pane): its name shows in a tooltip on hover or keyboard focus (styles:
 * .word-tip in globals.css) and is its accessible name. */
export function IconAction({
  label,
  onClick,
  disabled,
  tone = "plain",
  tipAlign = "center",
  tipSide = "bottom",
  role,
  children,
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  /** primary: the filled main action. */
  tone?: "plain" | "primary";
  /** Keeps the tooltip inside the pane for buttons at its edges. */
  tipAlign?: "start" | "center" | "end";
  tipSide?: "top" | "bottom";
  role?: "menuitem";
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      role={role}
      aria-label={label}
      data-tip={label}
      data-tip-align={tipAlign}
      data-tip-side={tipSide}
      disabled={disabled}
      onClick={onClick}
      className={`word-tip flex h-8 w-8 shrink-0 items-center justify-center rounded-full transition-colors disabled:opacity-40 ${
        tone === "primary"
          ? "bg-primary text-on-primary hover:opacity-90"
          : "text-muted hover:bg-surface-strong hover:text-ink"
      }`}
    >
      {children}
    </button>
  );
}

/** Underline with a wave: "mark in the document". */
export function IconUnderlineMark({ className = "h-4 w-4" }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M7 4v6a5 5 0 0 0 10 0V4" />
      <path d="M3 20c1.5-1.5 3-1.5 4.5 0s3 1.5 4.5 0 3-1.5 4.5 0 3 1.5 4.5 0" />
    </svg>
  );
}

/** A magic wand: "fix for me". */
export function IconWand({ className = "h-4 w-4" }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="m4 20 11-11M14 4v2M19 9h2M17.5 6.5 19 5M9 4.5 10 6M18 13l1.5 1" />
      <path d="m13 7 4 4" />
    </svg>
  );
}
