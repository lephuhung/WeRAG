"use client";

import { useEffect, useId, useRef, useState } from "react";
import { useT } from "@/lib/i18n";
import { getContextUsage } from "@/lib/api/context-usage";
import {
  contextFill,
  contextPercent,
  contextTone,
  denialKey,
  formatCount,
  formatTokens,
  shouldFetchContext,
  type ContextFetchState,
  type ContextTone,
  type ContextUsage,
} from "@/lib/context-usage";

const SIZE = 20;
const STROKE = 2;
const RADIUS = (SIZE - STROKE) / 2;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;

const TONE_COLOR: Record<ContextTone, string> = {
  neutral: "var(--color-muted)",
  amber: "var(--color-warning, #d97706)",
  red: "var(--color-error)",
};

const DENIAL_TEXT = {
  noPackage: "ctx.denied.noPackage",
  expired: "ctx.denied.expired",
  noSeat: "ctx.denied.noSeat",
  limit: "ctx.denied.limit",
} as const;

/** The session's context usage: fetched when the session opens idle, when
 * a turn ends, and when refreshKey moves mid-turn (a compaction is persisted
 * as it happens) — see shouldFetchContext. null until loaded, and when it
 * cannot be read. */
export function useContextUsage(sessionId: string | null, busy: boolean, refreshKey: string | number) {
  const [usage, setUsage] = useState<ContextUsage | null>(null);
  const last = useRef<ContextFetchState | null>(null);
  const seq = useRef(0);
  useEffect(() => {
    const prev = last.current;
    const next: ContextFetchState = { sessionId, busy, key: refreshKey };
    last.current = next;
    if (prev?.sessionId !== sessionId) setUsage(null);
    if (!sessionId || !shouldFetchContext(prev, next)) return;
    // a newer request, or another session, wins over a slower older one
    const mine = ++seq.current;
    getContextUsage(sessionId)
      .then((u) => mine === seq.current && setUsage(u))
      .catch(() => mine === seq.current && setUsage(null));
    // No cleanup cancels the request, on purpose: under React Strict Mode
    // effects run, clean up and run again on mount, and the rerun sees the
    // same state (shouldFetchContext says no), so cancelling here — or
    // bumping seq in an unmount cleanup — would drop the only request and
    // the ring would never show in dev. seq alone orders the responses; a
    // late setUsage after unmount is a no-op.
  }, [sessionId, busy, refreshKey]);
  return usage;
}

/* How full the history of the next turn is, as a small ring in the chat
 * header next to the document rings. Hovering (desktop), or a click/tap,
 * shows the details: history against the budget, compaction, the session's
 * turns and tokens against the plan's caps, the latest turn and the model
 * window. A tap toggles it; Escape, leaving focus or a click outside closes
 * it. */
export function ContextRing({
  sessionId,
  busy,
  refreshKey,
  className = "",
}: {
  /** null before the session exists ("new"): nothing is shown. */
  sessionId: string | null;
  /** A turn is running: the ring keeps its value until it ends. */
  busy: boolean;
  /** Changes when the ring should reload (e.g. a compaction was streamed). */
  refreshKey: string | number;
  className?: string;
}) {
  const { t, locale } = useT();
  const tipId = useId();
  const rootRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const usage = useContextUsage(sessionId, busy, refreshKey);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    const onDown = (e: Event) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false);
    };
    window.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("touchstart", onDown);
    return () => {
      window.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("touchstart", onDown);
    };
  }, [open]);

  if (!sessionId || !usage) return null;

  const fill = contextFill(usage);
  const pct = contextPercent(usage);
  const tone = contextTone(fill);
  const tok = (n: number) => formatTokens(n, locale);
  const cnt = (n: number) => formatCount(n, locale);
  // with a refusing package "no cap" would be wrong: say why instead
  const denial = usage.denied ? t(DENIAL_TEXT[denialKey(usage.reason)]) : "";
  const noCap = denial || t("ctx.unlimited");
  const budget = usage.history_budget || usage.model_window;
  const s = usage.session;
  const quick = usage.mode === "quick-answer";

  const lines: string[] = [
    t("ctx.history", { used: tok(usage.history_tokens), budget: tok(budget), pct }),
  ];
  if (usage.compacted && usage.compacted_turns > 0) lines.push(t("ctx.compacted", { n: cnt(usage.compacted_turns) }));
  else if (usage.seeded) lines.push(t("ctx.seeded"));
  lines.push(t("ctx.turns", { value: s.max_turns > 0 ? `${cnt(s.turns)}/${cnt(s.max_turns)}` : cnt(s.turns) }));
  lines.push(t("ctx.sessionTokens", { value: s.max_tokens > 0 ? `${tok(s.tokens)} / ${tok(s.max_tokens)}` : tok(s.tokens) }));
  if (usage.last_turn)
    lines.push(t("ctx.lastTurn", { tokens: tok(usage.last_turn.prompt_tokens), rounds: cnt(usage.last_turn.rounds) }));
  lines.push(t("ctx.turnCap", { value: usage.turn_token_cap > 0 ? tok(usage.turn_token_cap) : noCap }));
  lines.push(t("ctx.window", { value: tok(usage.model_window) }));
  if (denial) lines.push(t("ctx.plan", { name: denial }));
  else if (usage.plan_name) lines.push(t("ctx.plan", { name: usage.plan_name }));
  const hint = quick ? t("ctx.hintQuick", { n: cnt(usage.history_rounds) }) : t("ctx.hint");

  return (
    <div
      ref={rootRef}
      className={`group relative shrink-0 ${className}`}
      onBlur={(e) => {
        if (!rootRef.current?.contains(e.relatedTarget as Node | null)) setOpen(false);
      }}
    >
      <button
        type="button"
        aria-label={t("ctx.ringLabel", { pct })}
        aria-describedby={tipId}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="relative flex h-8 w-8 cursor-pointer items-center justify-center rounded-full hover:bg-surface-strong"
      >
        <svg width={SIZE} height={SIZE} viewBox={`0 0 ${SIZE} ${SIZE}`} aria-hidden="true" className="-rotate-90">
          <circle cx={SIZE / 2} cy={SIZE / 2} r={RADIUS} fill="none" strokeWidth={STROKE} style={{ stroke: "var(--color-hairline-strong)" }} />
          <circle
            cx={SIZE / 2}
            cy={SIZE / 2}
            r={RADIUS}
            fill="none"
            strokeWidth={STROKE}
            strokeLinecap="round"
            strokeDasharray={CIRCUMFERENCE}
            strokeDashoffset={CIRCUMFERENCE * (1 - Math.max(fill, 0.02))}
            className="transition-[stroke-dashoffset] duration-500 ease-out motion-reduce:transition-none"
            style={{ stroke: TONE_COLOR[tone] }}
          />
        </svg>
      </button>
      <div
        id={tipId}
        role="tooltip"
        className={`pointer-events-none absolute right-0 top-full z-50 mt-1 w-max max-w-[min(280px,calc(100vw-2rem))] rounded-[8px] bg-ink px-2.5 py-1.5 text-[12px] leading-snug text-canvas shadow-[0_4px_16px_rgba(0,0,0,0.12)] transition-opacity duration-150 ${
          open ? "opacity-100" : "opacity-0 group-hover:opacity-100"
        }`}
      >
        {lines.map((line, i) => (
          <p key={i} className={i === 0 ? "font-medium" : undefined}>
            {line}
          </p>
        ))}
        <p className="mt-1 opacity-75">{hint}</p>
      </div>
    </div>
  );
}
