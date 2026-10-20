/* One page-header shape for the settings sections.
 *
 * Two variants were in use: `title-md font-semibold` + `border-b
 * border-hairline pb-5` and `title-md` + `pb-4`. This component owns the
 * header block (title, subtitle, trailing actions).
 */
"use client";

import type { ReactNode } from "react";

export function SectionHeader({
  title,
  subtitle,
  actions,
  className = "",
}: {
  title: ReactNode;
  subtitle?: ReactNode;
  /* Trailing area (buttons, filters). */
  actions?: ReactNode;
  className?: string;
}) {
  return (
    <div className={`flex flex-wrap items-end justify-between gap-3 border-b border-hairline pb-5 ${className}`}>
      <div className="min-w-0">
        <h2 className="title-md font-semibold text-ink">{title}</h2>
        {subtitle ? <p className="caption mt-1 text-muted">{subtitle}</p> : null}
      </div>
      {actions ? <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  );
}

export default SectionHeader;
