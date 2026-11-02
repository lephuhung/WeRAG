"use client";

import { useSyncExternalStore } from "react";

/* One place for the viewport breakpoints of the app shell.
 *
 * The shell switches between the off-canvas drawer and the docked rail at
 * Tailwind's `lg` (1024px) — the CSS classes and the JS listeners have to
 * agree, otherwise the rail can end up off-screen (or focusable) while the
 * layout thinks it is docked. Every shell component reads the number from
 * here instead of hard-coding a pixel value.
 */
export const SHELL_BREAKPOINT_PX = 1024;

/** The rail is docked from this width up (matches Tailwind's `lg:`). */
export const SHELL_DESKTOP_QUERY = `(min-width: ${SHELL_BREAKPOINT_PX}px)`;

/** The rail is an off-canvas drawer below this width. */
export const SHELL_MOBILE_QUERY = `(max-width: ${SHELL_BREAKPOINT_PX - 1}px)`;

/* The chat surface has its own, narrower switch: the add-in and the
 * composer collapse into a sheet well before the shell does. It lives here
 * so the whole app reads its breakpoints from one module. */
export const CHAT_BREAKPOINT_PX = 768;
export const CHAT_MOBILE_QUERY = `(max-width: ${CHAT_BREAKPOINT_PX - 1}px)`;

/* The rail's width. The docked rail and the offset that hides it are the
 * same number — keep them in sync through this constant. */
export const SIDEBAR_WIDTH_PX = 264;

/* The height of the shell's header row. The sidebar's brand row and the
 * header bar share it, so the logo sits on the header's baseline. */
export const SHELL_HEADER_HEIGHT_PX = 56;

function subscribeToDesktop(onChange: () => void) {
  const media = window.matchMedia(SHELL_DESKTOP_QUERY);
  media.addEventListener("change", onChange);
  return () => media.removeEventListener("change", onChange);
}

/** True when the shell shows the docked rail (desktop). */
export function useIsDesktopShell(): boolean {
  return useSyncExternalStore(
    subscribeToDesktop,
    () => window.matchMedia(SHELL_DESKTOP_QUERY).matches,
    () => true,
  );
}
