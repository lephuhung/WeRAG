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
      /* overflow-x:auto forces overflow-y to auto too, so any child that
       * pokes past the box (the old -mb-px underline trick) made a stray
       * vertical scrollbar. Clip y, and draw the hairline as an inset
       * shadow so the active tab's 2px underline paints over it without
       * overflowing. */
      className={`flex items-center gap-6 overflow-x-auto overflow-y-hidden shadow-[inset_0_-1px_0_var(--color-hairline)] ${className}`}
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
            className={`shrink-0 whitespace-nowrap border-b-2 px-1 pb-3 pt-1 text-sm transition-colors focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-ink disabled:opacity-50 ${
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
