/* One source of an answer. The chip under the answer and the count pill in
 * the thinking header are the same thing, so they share this one component
 * and this one look (the info token — the citation family). */
"use client";

import type { ReactNode } from "react";

export function ReferenceChip({
  label,
  index,
  icon,
  onClick,
  title,
  className = "",
}: {
  /** The source name (or the count of sources, for the summary chip). */
  label: string;
  /** Position of the source in the answer (1-based); hidden when absent. */
  index?: number;
  icon?: ReactNode;
  onClick?: () => void;
  title?: string;
  className?: string;
}) {
  const skin =
    "inline-flex max-w-[180px] items-center gap-1 rounded-full border border-info/30 bg-info-surface px-2 py-0.5 text-[11.5px] font-medium text-info transition-colors";
  const inner = (
    <>
      {index !== undefined && (
        <span className="flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-full bg-info text-[9px] font-semibold text-surface-card">
          {index}
        </span>
      )}
      {icon && <span className="inline-flex shrink-0 items-center text-info">{icon}</span>}
      <span className="truncate">{label}</span>
    </>
  );
  if (!onClick) {
    return (
      <span title={title} className={`${skin} ${className}`}>
        {inner}
      </span>
    );
  }
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      className={`${skin} cursor-pointer select-none text-left hover:border-info/60 hover:bg-info/20 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink ${className}`}
    >
      {inner}
    </button>
  );
}
