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
 * with a 2s interval and gives up quietly. */
function pollSuggestions(sessionId: string, messageId: string): Promise<MessageSuggestionSet | null> {
  const deadline = Date.now() + 10_000;
  const interval = (ms: number) => {
    const { promise, resolve } = Promise.withResolvers<boolean>();
    setTimeout(() => resolve(true), ms);
    return promise;
  };
  const attempt = async (): Promise<MessageSuggestionSet | null> => {
    const res = await getMessageSuggestions(sessionId, messageId);
    const set = res.data;
    if (set && set.status !== "generating") return set;
    if (Date.now() > deadline) return set ?? null;
    await interval(1500);
    return attempt();
  };
  return attempt();
}

export function useFollowUpSuggestions(sessionId: string, opts: {
  assistantMessageId: string | null;
  enabled: boolean;
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
    setLoading(true);
    ensureMessageSuggestions(sessionId, opts.assistantMessageId)
      .catch(() => null)
      .then(() => pollSuggestions(sessionId, opts.assistantMessageId!))
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
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [opts.assistantMessageId, opts.enabled, sessionId]);

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

export function FollowUpSuggestions({ sessionId, messageId, enabled, onAsk }: {
  sessionId: string;
  messageId: string | null;
  enabled: boolean;
  onAsk: (text: string, attribution: { setId: string; questionId: string }, knowledgeBaseIds: string[]) => void;
}) {
  const { t } = useT();
  const { set, loading, dismissed, dismiss, pick } = useFollowUpSuggestions(sessionId, {
    assistantMessageId: messageId,
    enabled,
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
          className="flex items-center gap-1.5 rounded-full border border-hairline bg-surface-card px-3.5 py-1.5 text-[13px] text-body transition-colors hover:border-ink hover:text-ink"
          title={q.category}
        >
          <span className="text-muted-soft">›</span>
          <span className="max-w-[420px] truncate">{q.text}</span>
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
