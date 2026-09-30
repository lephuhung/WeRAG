/* Pure state machine for session-activity markers — ported from
 * frontend/src/stores/sessionActivityState.ts. Kept free of React and the
 * "@/"-aliased API layer so it runs under `node --test` and unit tests can
 * inject their own fetcher. session-activity.ts wires the singleton. */

export type SessionActivityEntry = {
  messageId: string;
  detached: boolean;
};

export type SessionActivityRecord = SessionActivityEntry & { failures: number };

export type ActivityMessage = {
  id?: string;
  role: string;
  is_completed?: boolean;
};

export type SessionActivityState = {
  update: (sessionId: string, running: boolean, messageId?: string) => void;
  detach: (sessionId: string) => void;
  refresh: () => Promise<void>;
  clear: () => void;
};

// Entries are replaced on every local update so stale polling responses cannot
// clear a newer turn or a stream that has since reattached.
export function createSessionActivityState(
  entries: Map<string, SessionActivityRecord>,
  fetchMessages: (sessionId: string) => Promise<ActivityMessage[]>,
  onChange: () => void = () => {},
  // Fired when a detached turn completes server-side — the UI layer uses this
  // to notify the user ("chat finished while you were elsewhere").
  onDetachedComplete: (sessionId: string) => void = () => {},
): SessionActivityState {
  const pending = new Set<string>();

  function update(sessionId: string, running: boolean, messageId = "") {
    if (!sessionId || sessionId === "new") return;
    const prev = entries.get(sessionId);
    if (running) {
      if (!prev || prev.messageId !== messageId || prev.detached) {
        entries.set(sessionId, { messageId, detached: false, failures: 0 });
        onChange();
      }
    } else if (prev) {
      entries.delete(sessionId);
      onChange();
    }
  }

  /* Leaving a session whose turn is still generating: keep the marker but flag
   * it detached so refresh() polls it. Reactivating (update true) clears the
   * flag when the user returns to the session. */
  function detach(sessionId: string) {
    const entry = entries.get(sessionId);
    if (!entry || entry.detached) return;
    entries.set(sessionId, { ...entry, detached: true });
    onChange();
  }

  /* Poll detached sessions once: the entry clears when its turn completes, when
   * the session is gone (403/404), or after 3 consecutive empty reads (a turn
   * abandoned before its assistant row ever persisted). */
  async function refresh() {
    await Promise.all(
      [...entries].map(async ([sessionId, entry]) => {
        if (!entry.detached || pending.has(sessionId)) return;
        pending.add(sessionId);
        try {
          const messages = await fetchMessages(sessionId);
          if (entries.get(sessionId) !== entry) return;
          const message = entry.messageId
            ? messages.find((m) => m.id === entry.messageId)
            : [...messages].reverse().find((m) => m.role === "assistant" && !m.is_completed);
          if (message?.is_completed || (!message && entry.messageId)) {
            entries.delete(sessionId);
            onChange();
            onDetachedComplete(sessionId);
          } else if (message) {
            const messageId = message.id ?? entry.messageId;
            if (messageId !== entry.messageId || entry.failures > 0) {
              entries.set(sessionId, { messageId, detached: true, failures: 0 });
              onChange();
            } else {
              entry.failures = 0;
            }
          } else if (++entry.failures >= 3) {
            entries.delete(sessionId);
            onChange();
          }
        } catch (error) {
          if (entries.get(sessionId) !== entry) return;
          const status = (error as { status?: number })?.status;
          if (status === 403 || status === 404 || ++entry.failures >= 3) {
            entries.delete(sessionId);
            onChange();
          }
        } finally {
          pending.delete(sessionId);
        }
      }),
    );
  }

  function clear() {
    if (entries.size === 0) return;
    entries.clear();
    onChange();
  }

  return { update, detach, refresh, clear };
}
