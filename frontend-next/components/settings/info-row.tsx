/* One label/value row for the info lists (tenant info, tenant usage).
 * The two lists each had their own copy of the helper. */
"use client";

import type { ReactNode } from "react";

export function InfoRow({
  label,
  desc,
  value,
  children,
  className = "",
}: {
  label: string;
  desc?: string;
  value?: ReactNode;
  children?: ReactNode;
  className?: string;
}) {
  return (
    <div className={`flex items-center justify-between gap-8 py-4 ${className}`}>
      <div className="min-w-0">
        <div className="title-sm block">{label}</div>
        {desc ? <p className="caption mt-1 text-muted">{desc}</p> : null}
      </div>
      <div className="shrink-0 text-right">
        {children ?? <span className="caption text-body">{value}</span>}
      </div>
    </div>
  );
}

export default InfoRow;
