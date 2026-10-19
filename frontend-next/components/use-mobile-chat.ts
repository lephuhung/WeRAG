"use client";

import { useSyncExternalStore } from "react";

const QUERY = "(max-width: 767px)";
export function isMobileChatViewport() {
  return typeof window !== "undefined" && window.matchMedia(QUERY).matches;
}
function subscribe(onChange: () => void) {
  const media = window.matchMedia(QUERY);
  media.addEventListener("change", onChange);
  return () => media.removeEventListener("change", onChange);
}
export function useMobileChat() {
  return useSyncExternalStore(subscribe, isMobileChatViewport, () => false);
}
