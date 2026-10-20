"use client";

import { useSyncExternalStore } from "react";
import { CHAT_MOBILE_QUERY } from "@/components/shell-breakpoints";

/* The chat surface collapses into a sheet on narrow screens — the
 * breakpoint lives with the shell's breakpoints (components/shell-breakpoints.ts). */
export function isMobileChatViewport() {
  return typeof window !== "undefined" && window.matchMedia(CHAT_MOBILE_QUERY).matches;
}
function subscribe(onChange: () => void) {
  const media = window.matchMedia(CHAT_MOBILE_QUERY);
  media.addEventListener("change", onChange);
  return () => media.removeEventListener("change", onChange);
}
export function useMobileChat() {
  return useSyncExternalStore(subscribe, isMobileChatViewport, () => false);
}
