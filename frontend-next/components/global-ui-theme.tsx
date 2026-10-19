"use client";

import { useEffect } from "react";

import { applyPreferences, readPreferences, PREFERENCES_EVENT } from "@/lib/ui-preferences";
import { applyUiTheme, bootUiTheme, subscribeUiTheme } from "@/lib/ui-theme";

/* Applies the SuperAdmin-selected global theme at runtime. The pre-paint
 * inline script in app/layout.tsx already set data-theme from cache; this
 * reconciles with the server value and follows in-page changes. */
export function GlobalUiTheme() {
  useEffect(() => {
    let preferences = readPreferences();
    const sync = () => applyPreferences(preferences.theme, preferences.fontSize);
    const changed = (event: Event) => {
      preferences = (event as CustomEvent<ReturnType<typeof readPreferences>>).detail;
      sync();
    };
    const storage = () => { preferences = readPreferences(); sync(); };
    const system = matchMedia("(prefers-color-scheme: dark)");
    const systemChanged = () => { if (preferences.theme === "system") sync(); };
    sync();
    window.addEventListener(PREFERENCES_EVENT, changed);
    window.addEventListener("storage", storage);
    system.addEventListener("change", systemChanged);
    return () => {
      window.removeEventListener(PREFERENCES_EVENT, changed);
      window.removeEventListener("storage", storage);
      system.removeEventListener("change", systemChanged);
    };
  }, []);
  useEffect(() => {
    void bootUiTheme();
    return subscribeUiTheme(applyUiTheme);
  }, []);
  return null;
}
