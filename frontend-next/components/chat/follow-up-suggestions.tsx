/* Ported from frontend/src/views/chat/index.vue follow-up suggestions
 * (suggestionSet lifecycle: ensure → poll generating → render chips →
 * record click/dismiss → attribute next turn) and the assistant message
 * action toolbar (copy + fork from botmsg.vue / forkPoint.ts).
 */
"use client";

import { useEffect, useState } from "react";
import {
  ensureMessageSuggestions,
  getMessageSuggestions,
  recordMessageSuggestionEvent,
  type MessageSuggestionSet,
} from "@/lib/api/chat";
import { useT } from "@/lib/i18n";

/* Poll a generating suggestion set up to ~10s; the Vue view does the same
 * with a 1s interval and gives up quietly. The abort signal short-circuits
 * both the wait and the fetch so an unmounted/StrictMode-killed effect stops
 * hitting the API instead of polling to the deadline. */
function pollSuggestions(
  sessionId: string,
  messageId: string,
  signal: AbortSignal,
): Promise<MessageSuggestionSet | null> {
  const deadline = Date.now() + 10_000;
  const interval = (ms: number) =>
    new Promise<boolean>((resolve) => {
      const timer = setTimeout(() => resolve(true), ms);
      signal.addEventListener(
        "abort",
        () => {
          clearTimeout(timer);
          resolve(false);
        },
        { once: true },
      );
    });
  const attempt = async (): Promise<MessageSuggestionSet | null> => {
    if (signal.aborted) return null;
    const res = await getMessageSuggestions(sessionId, messageId, { signal });
    const set = res.data;
    if (set && set.status !== "generating") return set;
    if (signal.aborted || Date.now() > deadline) return signal.aborted ? null : (set ?? null);
    if (!(await interval(1500))) return null;
    return attempt();
  };
  return attempt();
}

export function useFollowUpSuggestions(sessionId: string, opts: {
  assistantMessageId: string | null;
  enabled: boolean;
  /** The turn completed in this client (Vue: onTurnComplete) — POST ensure to
   * kick off generation. History rows pass false and only GET the existing
   * set, so opening an old session never spawns generation jobs. */
  ensure: boolean;
}) {
  const [set, setSet] = useState<MessageSuggestionSet | null>(null);
  const [loading, setLoading] = useState(false);
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    setDismissed(false);
    if (!opts.enabled || !opts.assistantMessageId || sessionId === "new") {
      setSet(null);
      return;
    }
    let alive = true;
    const controller = new AbortController();
    setLoading(true);
    const messageId = opts.assistantMessageId;
    (async () => {
      // Mirrors Vue loadFollowUpSuggestions: the first response decides —
      // only a "generating" set is polled, a failure ends quietly. History
      // rows GET instead of POST-ensure (opts.ensure === false).
      const initial = await (opts.ensure
        ? ensureMessageSuggestions(sessionId, messageId, false, { signal: controller.signal })
        : getMessageSuggestions(sessionId, messageId, { signal: controller.signal })
      ).then((r) => r.data ?? null);
      return initial && initial.status === "generating"
        ? pollSuggestions(sessionId, messageId, controller.signal)
        : initial;
    })()
      .then((s) => {
        if (alive) setSet(s && s.status === "ready" ? s : null);
      })
      .catch(() => {
        if (alive) setSet(null);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
      controller.abort();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [opts.assistantMessageId, opts.enabled, opts.ensure, sessionId]);

  const dismiss = () => {
    setDismissed(true);
    if (set) void recordMessageSuggestionEvent(sessionId, set.id, "dismiss").catch(() => undefined);
  };

  const pick = (questionId: string) => {
    if (set) void recordMessageSuggestionEvent(sessionId, set.id, "click", questionId).catch(() => undefined);
    return set?.id ?? null;
  };

  return { set, loading, dismissed, dismiss, pick };
}

export function FollowUpSuggestions({ sessionId, messageId, enabled, ensure, onAsk }: {
  sessionId: string;
  messageId: string | null;
  enabled: boolean;
  ensure: boolean;
  onAsk: (text: string, attribution: { setId: string; questionId: string }, knowledgeBaseIds: string[]) => void;
}) {
  const { t } = useT();
  const { set, loading, dismissed, dismiss, pick } = useFollowUpSuggestions(sessionId, {
    assistantMessageId: messageId,
    enabled,
    ensure,
  });

  /* Regenerate: re-runs ensure with regenerate=true. Rare; skip the lure and
   * just surface nothing when suppressed. */
  if (!enabled || dismissed || loading) return null;
  if (!set || set.questions.length === 0) return null;

  return (
    <div className="mt-3 flex flex-wrap items-center gap-2">
      {set.questions.map((q) => (
        <button
          key={q.id}
          onClick={() => {
            const setId = pick(q.id);
            if (!setId) return;
            onAsk(q.text, { setId, questionId: q.id }, q.knowledge_base_ids ?? []);
          }}
          className="flex max-w-full items-center gap-1.5 rounded-full border border-hairline bg-surface-card px-3.5 py-1.5 text-[13px] text-body transition-colors hover:border-ink hover:text-ink"
          title={q.category}
        >
          <span className="text-muted-soft">›</span>
          <span className="min-w-0 max-w-[420px] truncate">{q.text}</span>
        </button>
      ))}
      <button
        onClick={dismiss}
        className="btn btn-tertiary btn-sm text-muted-soft"
        aria-label={t("chat.dismissSuggestions")}
      >
        ✕
      </button>
    </div>
  );
}
