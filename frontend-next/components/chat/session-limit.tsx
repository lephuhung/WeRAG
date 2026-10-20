/* Per-session caps of the usage plan in the chat: the card shown when a
 * session is refused (session_limit), the banner shown from 80% of a cap,
 * and the marker of where earlier conversation was summarized. Both notices
 * offer "open a new session", which carries this session's summary. */
"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useT } from "@/lib/i18n";
import { continueSession } from "@/lib/api/chat";
import { tokenPercent, turnsUsed, type SessionUsage } from "@/lib/session-limits";

function useContinueSession(sessionId: string) {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const open = async () => {
    setBusy(true);
    setFailed(false);
    try {
      const res = await continueSession(sessionId);
      const next = res.data?.id;
      if (!next) throw new Error("no session");
      router.push(`/platform/chat/${next}`);
    } catch {
      setFailed(true);
      setBusy(false);
    }
  };
  return { busy, failed, open };
}

function OpenNewSessionButton({ sessionId }: { sessionId: string }) {
  const { t } = useT();
  const { busy, failed, open } = useContinueSession(sessionId);
  return (
    <span className="inline-flex items-center gap-2">
      <button type="button" className="btn btn-primary btn-sm" disabled={busy} onClick={() => void open()}>
        {busy ? t("session.limit.opening") : t("session.limit.open")}
      </button>
      {failed && <span className="text-error">{t("session.limit.failed")}</span>}
    </span>
  );
}

/** The refusal card: this session used up a per-session cap. */
export function SessionLimitCard({ sessionId, compact = false }: { sessionId: string; compact?: boolean }) {
  const { t } = useT();
  return (
    <div
      className={`${compact ? "text-[12.5px]" : "text-[13px]"} mb-4 rounded-lg2 border border-hairline bg-surface-card p-3 text-body`}
      role="status"
    >
      <p className="mb-1 font-medium text-ink">{t("session.limit.title")}</p>
      <p className="mb-2 text-muted">{t("chat.err.usage.sessionLimit")}</p>
      <OpenNewSessionButton sessionId={sessionId} />
    </div>
  );
}

/** The banner from 80% of a cap (or past it, where the cap only warns). */
export function SessionUsageBanner({ usage, sessionId, compact = false }: {
  usage: SessionUsage;
  sessionId: string;
  compact?: boolean;
}) {
  const { t } = useT();
  const parts: string[] = [];
  if (usage.maxTurns > 0) parts.push(t("session.warn.turns", { used: turnsUsed(usage), max: usage.maxTurns }));
  if (usage.maxTokens > 0) parts.push(t("session.warn.tokens", { pct: tokenPercent(usage) }));
  const hint = usage.reached && usage.soft ? t("session.warn.soft") : t("session.warn.near");
  return (
    <div
      className={`${compact ? "text-[12px]" : "text-[12.5px]"} mb-2 flex flex-wrap items-center justify-between gap-2 rounded-lg2 border border-hairline bg-surface-card p-3 text-body`}
      role="status"
    >
      <span>
        {parts.join(" · ")} {hint}
      </span>
      {!usage.soft && <OpenNewSessionButton sessionId={sessionId} />}
    </div>
  );
}

/** Marks the turn up to which earlier conversation was summarized, from a
 * stored context checkpoint (kept across reloads). */
export function ContextCompactedDivider({ summary, degraded }: { summary: string; degraded?: boolean }) {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  return (
    <div className="my-3 text-[12px] text-muted">
      <button
        type="button"
        className="flex w-full items-center gap-2 hover:text-ink"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="h-px flex-1 bg-hairline" />
        <span>
          {t("think.compactedHere")}
          {degraded ? ` · ${t("think.compactedDegraded")}` : ""}
        </span>
        <span className="h-px flex-1 bg-hairline" />
      </button>
      {open && (
        <pre className="mt-2 max-h-72 overflow-y-auto whitespace-pre-wrap rounded-lg bg-surface-card p-3 font-sans text-body">
          {summary}
        </pre>
      )}
    </div>
  );
}
