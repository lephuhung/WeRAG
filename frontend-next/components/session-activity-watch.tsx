/* Keeps polling the chats the user left mid-generation, for as long as they
 * are signed in.
 *
 * This used to live in <Sidebar />, which also cleared the store when it
 * unmounted. That was fine while the rail was mounted across all of
 * /platform, but the administration console is its own frame now: opening it
 * unmounted the rail, which stopped the poll and dropped the markers, so a
 * question asked just before switching lost both its "in progress" spinner
 * and its finished-reply notice. The store is a module-level singleton, so
 * polling belongs to the signed-in shell, not to one piece of chrome.
 */
"use client";

import { useEffect } from "react";
import { refreshSessionActivity } from "@/lib/session-activity";

/* mirrors menu.vue's 5s sessionActivity.refresh() interval */
const POLL_MS = 5000;

export function SessionActivityWatch() {
  useEffect(() => {
    const timer = window.setInterval(() => {
      void refreshSessionActivity();
    }, POLL_MS);
    return () => window.clearInterval(timer);
  }, []);

  return null;
}

export default SessionActivityWatch;
