/* Whether the app's left rail is hidden on desktop. A page that needs the
 * whole width (the legal draft review: PDF + editor side by side) offers a
 * toggle; the rail itself shows a small tab to bring it back.
 *
 * The stored value is scoped to the page that hid the rail. A full page load
 * (typing another URL, reopening the browser) never inherits it: a reviewer
 * who left the full-width review page by typing a URL used to come back to a
 * rail-less app, because React's unmount cleanup — the only thing that put the
 * rail back — does not run when the document is replaced. */
import { useSyncExternalStore } from "react";

const STORAGE_KEY = "werag_sidebar_hidden";
let hidden = false;
let loaded = false;
const listeners = new Set<() => void>();

/** page + query of the view that hid the rail; the review page keeps its
 * PDF/editor context in the query, so paths alone are too coarse. */
function scopeKey(): string {
  if (typeof window === "undefined") return "";
  return `${window.location.pathname}${window.location.search}`;
}

function load(): void {
  if (loaded) return;
  loaded = true;
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY) ?? "";
    // "1" is the pre-scope value: it names no page, so it must not keep the
    // rail hidden on a fresh load. Older tabs are also the ones most likely
    // to be stuck this way.
    const [flag, scope] = raw.split("|");
    hidden = flag === "1" && scope === scopeKey();
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
    window.localStorage.setItem(STORAGE_KEY, next ? `1|${scopeKey()}` : "0");
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

/* A page that needs the room (an open document on a small laptop) can fold
 * the rail away for as long as it is shown. Unlike `setSidebarHidden` this
 * is never stored. Holders are counted, so two panes swapping on navigation
 * cannot cancel each other. Once the user brings the rail back with the edge
 * tab, folding stays off for this page load (a session click remounts the
 * pane and must not fold it again) until a wide window resets it. */
let foldHolders = 0;
let foldDismissed = false;
const foldListeners = new Set<() => void>();

function foldedNow(): boolean {
  return foldHolders > 0 && !foldDismissed;
}

function changeFold(fn: () => void): void {
  const before = foldedNow();
  fn();
  if (foldedNow() !== before) foldListeners.forEach((l) => l());
}

/** Fold the rail while the caller holds it; returns the release. */
export function holdSidebarFold(): () => void {
  let released = false;
  changeFold(() => {
    foldHolders += 1;
  });
  return () => {
    if (released) return;
    released = true;
    changeFold(() => {
      foldHolders -= 1;
    });
  };
}

/** The user asked for the rail back (edge tab): stop folding. */
export function dismissSidebarFold(): void {
  changeFold(() => {
    foldDismissed = true;
  });
}

/** The window is wide again: a later small window may fold once more. */
export function resetSidebarFoldDismissal(): void {
  changeFold(() => {
    foldDismissed = false;
  });
}

function subscribeFolded(fn: () => void): () => void {
  foldListeners.add(fn);
  return () => foldListeners.delete(fn);
}

/** True while a page keeps the desktop rail folded away. */
export function useSidebarFolded(): boolean {
  return useSyncExternalStore(subscribeFolded, foldedNow, () => false);
}

/** The fold state outside React (tests). */
export function sidebarFoldedNow(): boolean {
  return foldedNow();
}
