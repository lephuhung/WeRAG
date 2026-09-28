"use client";

import { createContext, useContext } from "react";

/* When a settings page is embedded inside the settings modal, SectionCardGrid
 * cards must open as modal sub-pages instead of routing away or opening a
 * nested modal. The modal provides this context; it receives a card href or
 * key and returns true when it handled the navigation (sub-page pushed), so
 * the grid can fall back to router.push / its content modal outside. */
export const InModalNavContext = createContext<((target: string) => boolean) | null>(
  null,
);

export function useInModalNav() {
  return useContext(InModalNavContext);
}

/* True while rendering inside the settings modal — embedded pages use it to
 * switch viewport-based multi-column grids to narrower compact layouts. */
export function useInSettingsModal() {
  return useContext(InModalNavContext) != null;
}
