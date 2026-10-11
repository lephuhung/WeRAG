"use client";

import { useEffect } from "react";

import { applyUiTheme, bootUiTheme, subscribeUiTheme } from "@/lib/ui-theme";

/* Applies the SuperAdmin-selected global theme at runtime. The pre-paint
 * inline script in app/layout.tsx already set data-theme from cache; this
 * reconciles with the server value and follows in-page changes. */
export function GlobalUiTheme() {
  useEffect(() => {
    void bootUiTheme();
    return subscribeUiTheme(applyUiTheme);
  }, []);
  return null;
}
