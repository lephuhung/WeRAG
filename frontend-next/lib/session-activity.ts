/* Ported from frontend/src/stores/sessionActivity.ts — module-level singleton
 * wiring createSessionActivityState to listMessages and exposing the entries
 * to React via useSyncExternalStore.
 *
 * - update(sid, true, msgId): the chat view marks its session running while a
 *   turn streams. Leaving mid-turn aborts the local SSE but the backend keeps
 *   generating — the entry survives so the sidebar can report it.
 * - detach(sid): leaving a still-running session flags the entry detached.
 *   Only detached entries get polled by refresh() — the sidebar drives it on
 *   a 5s interval until the turn completes server-side.
 * - update(sid, false) removes the marker (turn finished / history showed
 *   nothing running / session deleted). */
import { useSyncExternalStore } from "react";
import { listMessages } from "@/lib/api/chat";
import {
  createSessionActivityState,
  type SessionActivityEntry,
  type SessionActivityRecord,
} from "./session-activity-state.ts";

export type { SessionActivityEntry };

const entries = new Map<string, SessionActivityRecord>();
const listeners = new Set<() => void>();
let snapshot: ReadonlyMap<string, SessionActivityEntry> = new Map();

function emit() {
  snapshot = new Map(entries);
  for (const l of listeners) l();
}

const activity = createSessionActivityState(
  entries,
  async (sessionId) => (await listMessages(sessionId, 20)).data ?? [],
  emit,
);

export function updateSessionActivity(sessionId: string, running: boolean, messageId = "") {
  activity.update(sessionId, running, messageId);
}

export function detachSessionActivity(sessionId: string) {
  activity.detach(sessionId);
}

export function refreshSessionActivity() {
  return activity.refresh();
}

export function clearSessionActivity() {
  activity.clear();
}

export function subscribeSessionActivity(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function getSessionActivitySnapshot() {
  return snapshot;
}

export function useSessionActivityEntries(): ReadonlyMap<string, SessionActivityEntry> {
  return useSyncExternalStore(
    subscribeSessionActivity,
    getSessionActivitySnapshot,
    getSessionActivitySnapshot,
  );
}
