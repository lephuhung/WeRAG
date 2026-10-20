/* The one error surface of the chat. Before there were two: a red bubble
 * around the failed answer and a red line under the message list, both
 * showing the same text, in two different styles. Every error of the chat
 * surface (the composer, a failed turn, a page-level failure) now goes
 * through this one component: the error token for the text, the error
 * surface tint for the box. */
"use client";

import type { ReactNode } from "react";

export function ErrorNote({
  children,
  className = "",
  compact = false,
}: {
  children: ReactNode;
  className?: string;
  /** A short note (the composer's blocking messages) — smaller type. */
  compact?: boolean;
}) {
  return (
    <div
      role="alert"
      className={`rounded-md2 border border-error/30 bg-error-surface px-3 py-2 leading-relaxed text-error ${
        compact ? "text-[12.5px]" : "text-[13px]"
      } ${className}`}
    >
      {children}
    </div>
  );
}
