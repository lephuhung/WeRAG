/* Whether the app's left rail is hidden on desktop. A page that needs the
 * whole width (the legal draft review: PDF + editor side by side) offers a
 * toggle; the rail itself shows a small tab to bring it back, so the choice
 * is safe to remember across pages. Stored per browser. */
import { useSyncExternalStore } from "react";

const STORAGE_KEY = "werag_sidebar_hidden";
let hidden = false;
let loaded = false;
const listeners = new Set<() => void>();

function load(): void {
  if (loaded) return;
  loaded = true;
  try {
    hidden = typeof window !== "undefined" && window.localStorage.getItem(STORAGE_KEY) === "1";
  } catch {
    hidden = false;
  }
}

function subscribe(fn: () => void): () => void {
  load();
  listeners.add(fn);
  return () => listeners.delete(fn);
}

function get(): boolean {
  load();
  return hidden;
}

export function setSidebarHidden(next: boolean): void {
  load();
  if (hidden === next) return;
  hidden = next;
  try {
    window.localStorage.setItem(STORAGE_KEY, next ? "1" : "0");
  } catch {
    /* private window: the choice lasts for this page load only */
  }
  listeners.forEach((fn) => fn());
}

/** The current value outside React (a page restoring it when it closes). */
export function sidebarHiddenNow(): boolean {
  return get();
}

/** True when the desktop rail is hidden; false during server rendering. */
export function useSidebarHidden(): boolean {
  return useSyncExternalStore(subscribe, get, () => false);
}
