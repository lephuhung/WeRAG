/* One tab strip for the settings/admin surfaces.
 *
 * The hub pages (data storage, memory) had byte-for-byte duplicate tab
 * strips, the billing and legal pages had two more variants and two of
 * them had no tab semantics at all. This component owns the strip: a
 * real `role="tablist"` with `role="tab"` buttons, `aria-selected` and a
 * consistent focus ring.
 */
"use client";

import type { ReactNode } from "react";

export interface TabItem<T extends string> {
  id: T;
  label: ReactNode;
  /* Optional count/badge rendered after the label. */
  badge?: ReactNode;
  disabled?: boolean;
}

export function TabStrip<T extends string>({
  tabs,
  value,
  onChange,
  ariaLabel,
  className = "",
}: {
  tabs: TabItem<T>[];
  value: T;
  onChange: (id: T) => void;
  ariaLabel: string;
  className?: string;
}) {
  return (
    <div
      className={`flex items-center gap-6 overflow-x-auto border-b border-hairline ${className}`}
      role="tablist"
      aria-label={ariaLabel}
    >
      {tabs.map((tab) => {
        const active = tab.id === value;
        return (
          <button
            key={tab.id}
            type="button"
            role="tab"
            id={`tab-${tab.id}`}
            aria-selected={active}
            aria-controls={`tabpanel-${tab.id}`}
            disabled={tab.disabled}
            onClick={() => onChange(tab.id)}
            className={`-mb-px shrink-0 whitespace-nowrap border-b-2 px-1 pb-3 pt-1 text-sm transition-colors focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-ink disabled:opacity-50 ${
              active
                ? "border-ink font-semibold text-ink"
                : "border-transparent font-medium text-muted hover:border-hairline-strong hover:text-ink"
            }`}
          >
            {tab.label}
            {tab.badge ? <span className="ml-2">{tab.badge}</span> : null}
          </button>
        );
      })}
    </div>
  );
}

export default TabStrip;
