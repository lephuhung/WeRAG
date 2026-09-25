"use client";

import { memo, useEffect, useMemo, useRef, useState } from "react";
import { Markdown } from "@/components/markdown";
import { useT } from "@/lib/i18n";
import { IconBulb, IconCheckCircleFilled, IconMinusCircle } from "@/components/icons";
import {
  stepsSummaryNodes,
  thinkingSummaryText,
  toolStepIcon,
  toolStepSummary,
  toolStepTitle,
  type AgentStepItem,
  type ThinkingStep,
  type ToolStep,
} from "./agent-steps";
import type { KnowledgeReferenceItem } from "./references-drawer";

// Only the tail of the reasoning stream is rendered: bounds the DOM size and
// powers the "rolling" window where the newest text stays pinned to the bottom.
const MAX_VISIBLE_CHARS = 2000;

function tailSlice(text: string, max: number): { visible: string; truncated: boolean } {
  if (text.length <= max) return { visible: text, truncated: false };
  const tail = text.slice(-max);
  // Cut on a line boundary so the visible tail doesn't start mid-word.
  const nl = tail.indexOf("\n");
  return { visible: nl > 0 ? tail.slice(nl + 1) : tail, truncated: true };
}

function fmtDuration(seconds: number): string {
  if (seconds < 3) return "vài giây";
  if (seconds < 60) return `${seconds} giây`;
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return s ? `${m} phút ${s} giây` : `${m} phút`;
}

const isThinkingStep = (
  s: AgentStepItem,
): s is ThinkingStep | (ToolStep & { tool_name: "thinking" }) =>
  s.type === "thinking" || (s.type === "tool" && s.tool_name === "thinking");

const thinkingBody = (s: ThinkingStep | ToolStep): string =>
  s.type === "thinking"
    ? s.content
    : String(s.tool_data?.thought || (typeof s.output === "string" ? s.output : "") || "");

const thinkingTitle = (s: ThinkingStep | ToolStep, t: (k: "step.think") => string): string =>
  s.type === "thinking" ? (s.title ?? "") : t("step.think");

const stepPending = (s: AgentStepItem): boolean =>
  s.type === "tool" ? s.status === "pending" : s.type === "thinking" ? !s.done : false;

/* Unified reasoning block: the RAG/tool pipeline and the thought process are
 * ONE collapsible section — the header carries the live status + duration +
 * document count, the expanded body shows the agent step timeline (thinking
 * rounds, tool calls, compaction markers — ported from the Vue smart-agent
 * tree) or, when no structured steps exist, the raw thinking stream. */
export const ThinkingDisplay = memo(function ThinkingDisplay({
  content,
  streaming,
  steps = [],
  durationMs,
  references = [],
  onViewReferences,
}: {
  content: string;
  streaming?: boolean;
  steps?: AgentStepItem[];
  durationMs?: number;
  references?: KnowledgeReferenceItem[];
  onViewReferences?: () => void;
}) {
  const { t } = useT();
  // Open while streaming; finished thinking (history or completed turns)
  // mounts collapsed — mirrors "khi think xong tự động đóng".
  const [expanded, setExpanded] = useState(() => Boolean(streaming));
  const rollRef = useRef<HTMLDivElement>(null);
  const feedRef = useRef<HTMLDivElement>(null);
  const [overflowing, setOverflowing] = useState(false);
  const wasStreaming = useRef(false);
  const startedAt = useRef<number | null>(null);
  const [elapsed, setElapsed] = useState<number | null>(null);
  const [expandedSteps, setExpandedSteps] = useState<Set<string>>(() => new Set());
  const activeThinking = useRef<Set<string>>(new Set());

  const hasSteps = steps.length > 0;
  const toolSteps = useMemo(
    () => steps.filter((s): s is ToolStep => s.type === "tool" && s.tool_name !== "thinking"),
    [steps],
  );
  const toolCount = toolSteps.length;
  const hasTools = toolCount > 0;
  const toolsPending = hasTools && toolSteps.some((e) => e.status === "pending");

  // Total found documents count (references list wins; otherwise peek at tool
  // outputs for a count only — never a tool name or payload).
  const docCount = useMemo(() => {
    if (references && references.length > 0) return references.length;
    for (const step of toolSteps) {
      let data: unknown = step.tool_data ?? step.output;
      if (typeof data === "string") {
        try {
          data = JSON.parse(data.trim());
        } catch {
          continue;
        }
      }
      if (data && typeof data === "object") {
        const rec = data as Record<string, unknown>;
        const count =
          Number(rec.doc_count) ||
          Number(rec.count) ||
          (Array.isArray(rec.results) ? rec.results.length : 0);
        if (count > 0) return count;
      }
    }
    return 0;
  }, [references, toolSteps]);

  // Auto-collapse once the stream finishes; the header still lets the user
  // re-open it manually afterwards. A thinking phase that (re)starts mid-turn —
  // e.g. the model reasons again after a tool result — re-opens it.
  useEffect(() => {
    if (streaming) {
      if (startedAt.current == null) startedAt.current = Date.now();
      if (!wasStreaming.current) setExpanded(true);
      wasStreaming.current = true;
    } else if (wasStreaming.current) {
      wasStreaming.current = false;
      setExpanded(false);
      if (startedAt.current != null) {
        setElapsed(Math.max(1, Math.round((Date.now() - startedAt.current) / 1000)));
      }
    }
  }, [streaming]);

  // Vue auto-expands the trailing run of thinking cards while the agent works
  // and folds them back once a tool call follows — the live "reading its mind"
  // view without leaving every round open afterwards.
  useEffect(() => {
    const trailing = new Set<string>();
    for (let i = steps.length - 1; i >= 0; i--) {
      const s = steps[i];
      if (!isThinkingStep(s)) break;
      trailing.add(s.id);
    }
    const prev = activeThinking.current;
    if (prev.size === trailing.size && [...trailing].every((id) => prev.has(id))) return;
    activeThinking.current = trailing;
    setExpandedSteps((old) => {
      const next = new Set(old);
      for (const id of prev) if (!trailing.has(id)) next.delete(id);
      for (const id of trailing) next.add(id);
      return next;
    });
  }, [steps]);

  const { visible, truncated } = tailSlice(content, MAX_VISIBLE_CHARS);

  // The top fade only makes sense once text is actually clipped above the
  // rolling window — measure instead of guessing so short notes stay crisp.
  useEffect(() => {
    const roll = rollRef.current;
    const feed = feedRef.current;
    if (!expanded || !roll || !feed) return;
    const check = () => setOverflowing(feed.offsetHeight > roll.clientHeight + 4);
    check();
    const ro = new ResizeObserver(check);
    ro.observe(feed);
    ro.observe(roll);
    return () => ro.disconnect();
  }, [expanded]);

  if (!content && !streaming && !hasTools && !hasSteps) return null;

  const toggleStep = (id: string) =>
    setExpandedSteps((old) => {
      const next = new Set(old);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const label = streaming
    ? content || !hasTools
      ? t("think.thinking")
      : t("think.thinkingTools")
    : content
      ? t("think.done")
      : t("think.processed");

  const summaryMs = durationMs && durationMs > 0 ? durationMs : (elapsed ?? 0) * 1000;
  // While streaming, a settled tail means the agent is between steps — show
  // the "thinking" placeholder row (Vue's trailing activity node).
  const showPendingTail =
    Boolean(streaming) &&
    hasSteps &&
    !stepPending(steps[steps.length - 1]) &&
    steps[steps.length - 1].type !== "compacted";

  return (
    <div className="mb-4 text-left">
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          aria-expanded={expanded}
          className="flex min-w-0 items-center gap-2 py-1.5 text-xs font-medium text-muted transition-colors select-none hover:text-ink"
        >
          {streaming ? (
            <span className="relative flex h-2 w-2 shrink-0">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-primary opacity-75" />
              <span className="relative inline-flex h-2 w-2 rounded-full bg-primary" />
            </span>
          ) : (
            <span className="inline-block h-2 w-2 shrink-0 rounded-full bg-muted-soft" />
          )}
          <span className={`truncate ${streaming ? "animate-shimmer-text" : ""}`}>
            {!streaming && hasSteps ? (
              stepsSummaryNodes(t, steps, summaryMs)
            ) : (
              <>
                {label}
                {!streaming && elapsed != null && (
                  <span className="font-normal text-muted-soft"> · {fmtDuration(elapsed)}</span>
                )}
                {hasTools && (
                  <span className="font-normal text-muted-soft">
                    {" "}
                    · {t("think.toolCalls", { tools: toolCount })}
                  </span>
                )}
              </>
            )}
          </span>
          <svg
            viewBox="0 0 20 20"
            fill="currentColor"
            className={`h-3.5 w-3.5 shrink-0 text-muted-soft transition-transform duration-200 ${
              expanded ? "rotate-180" : ""
            }`}
          >
            <path
              fillRule="evenodd"
              d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z"
              clipRule="evenodd"
            />
          </svg>
        </button>
        {docCount > 0 && onViewReferences && (
          <button
            type="button"
            onClick={onViewReferences}
            className="shrink-0 cursor-pointer rounded-full border border-emerald-200 bg-emerald-50/80 px-2 py-0.5 text-[11px] font-medium text-emerald-700 transition-colors hover:border-emerald-300 hover:bg-emerald-100 hover:text-emerald-800 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-300 dark:hover:border-emerald-500/50 dark:hover:bg-emerald-500/20"
            title={t("think.openDocs")}
          >
            {t("think.docs", { count: docCount })}
          </button>
        )}
      </div>

      {expanded &&
        (hasSteps ? (
          /* Step timeline: thinking rounds, tool calls and compaction markers
             on a single rail, like the Vue smart-agent tree. */
          <ol className="relative ml-1 flex flex-col gap-0.5 border-l-0 pb-1 pt-0.5 text-[13px] leading-relaxed text-muted">
            <span aria-hidden className="absolute bottom-2 left-[8px] top-2 w-px bg-hairline" />
            {steps.map((step) => {
              if (isThinkingStep(step)) {
                const body = thinkingBody(step);
                const title = thinkingTitle(step, t);
                const open = expandedSteps.has(step.id);
                const pending = stepPending(step) && streaming;
                const line = title || thinkingSummaryText(body) || t("step.think");
                const badge =
                  step.type === "tool" && step.tool_data?.thought_number
                    ? `${step.tool_data.thought_number}/${step.tool_data.total_thoughts ?? "?"}`
                    : null;
                return (
                  <li key={step.id} className="relative flex items-start gap-2.5 py-0.5">
                    <span className="relative z-10 mt-0.5 flex h-[17px] w-[17px] shrink-0 items-center justify-center rounded-full bg-canvas text-muted-soft">
                      <IconBulb className="h-3.5 w-3.5" />
                    </span>
                    <div className="min-w-0 flex-1">
                      <button
                        type="button"
                        onClick={() => body && toggleStep(step.id)}
                        className={`block max-w-full truncate text-left ${pending ? "animate-shimmer-text" : ""}`}
                      >
                        {line}
                        {badge && <span className="ml-1.5 text-[11px] text-muted-soft">{badge}</span>}
                      </button>
                      {open && body && (
                        <div className="mt-0.5 max-h-[240px] overflow-y-auto pr-1 text-[12.5px] [&_.chat-markdown]:text-muted">
                          {/* While the turn is still streaming, render the body
                              as cheap plain text — a Markdown re-parse of every
                              expanded card per delta starves useDeferredValue
                              and the answer stops streaming. */}
                          {streaming ? (
                            <div className="whitespace-pre-wrap">
                              {tailSlice(body, MAX_VISIBLE_CHARS).visible}
                            </div>
                          ) : (
                            <Markdown text={body} />
                          )}
                        </div>
                      )}
                    </div>
                  </li>
                );
              }
              if (step.type === "compacted") {
                const detail = [
                  step.tokens_before && step.tokens_after
                    ? t("think.compactedSummary", {
                        before: step.tokens_before.toLocaleString(),
                        after: step.tokens_after.toLocaleString(),
                      })
                    : "",
                  step.degraded ? t("think.compactedDegraded") : "",
                ]
                  .filter(Boolean)
                  .join(" · ");
                return (
                  <li key={step.id} className="relative flex items-start gap-2.5 py-0.5">
                    <span className="relative z-10 mt-0.5 flex h-[17px] w-[17px] shrink-0 items-center justify-center rounded-full bg-canvas text-muted-soft">
                      <IconMinusCircle className="h-3.5 w-3.5" />
                    </span>
                    <div className="min-w-0 flex-1">
                      <span>{t("think.compacted")}</span>
                      {detail && <div className="text-[12px] text-muted-soft">{detail}</div>}
                    </div>
                  </li>
                );
              }
              const pending = step.status === "pending";
              const failed = step.status === "error";
              const summary = toolStepSummary(t, step);
              return (
                <li key={step.id} className="relative flex items-start gap-2.5 py-0.5">
                  <span
                    className={`relative z-10 mt-0.5 flex h-[17px] w-[17px] shrink-0 items-center justify-center rounded-full bg-canvas ${
                      failed ? "text-error" : "text-muted-soft"
                    }`}
                  >
                    {toolStepIcon(step)}
                  </span>
                  <div className="min-w-0 flex-1">
                    <div
                      className={`truncate ${pending ? "animate-shimmer-text" : ""} ${failed ? "text-error" : ""}`}
                    >
                      {toolStepTitle(t, step)}
                    </div>
                    {summary && <div className="text-[12px] text-muted-soft">{summary}</div>}
                    {failed && step.error && (
                      <div className="truncate text-[12px] text-error/80">{step.error}</div>
                    )}
                  </div>
                </li>
              );
            })}
            {showPendingTail && (
              <li className="relative flex items-start gap-2.5 py-0.5">
                <span className="relative z-10 mt-0.5 flex h-[17px] w-[17px] shrink-0 items-center justify-center rounded-full bg-canvas text-muted-soft">
                  <IconBulb className="h-3.5 w-3.5" />
                </span>
                <span className="animate-shimmer-text">{t("think.thinking")}</span>
              </li>
            )}
            {!streaming && (
              <li className="relative flex items-start gap-2.5 py-0.5">
                <span className="relative z-10 mt-0.5 flex h-[17px] w-[17px] shrink-0 items-center justify-center rounded-full bg-canvas text-muted-soft">
                  <IconCheckCircleFilled className="h-3.5 w-3.5" />
                </span>
                <span>{t("think.finish")}</span>
              </li>
            )}
          </ol>
        ) : (
          <div className="ml-1 border-l-2 border-hairline pl-3.5 pb-1 pt-0.5 text-[13px] leading-relaxed text-muted [&_.chat-markdown]:text-muted">
            {hasTools && (
              <div className="mb-1.5 flex items-center gap-1.5 text-[11.5px] text-muted-soft">
                {toolsPending ? (
                  <span className="animate-shimmer-text">{t("think.usingTools")}</span>
                ) : (
                  <span>
                    {t("think.toolsUsed", { count: toolCount })}
                    {docCount > 0 ? ` · ${t("think.docs", { count: docCount })}` : ""}
                  </span>
                )}
              </div>
            )}
            {truncated && (
              <div className="mb-1.5 text-[10px] italic text-muted-soft">
                {t("think.tailOnly", { chars: MAX_VISIBLE_CHARS.toLocaleString() })}
              </div>
            )}
            {/* Rolling window: overflow-hidden (no scrollbar) + justify-end pins the
                newest text to the bottom so older lines drift up and clip away —
                the "cuộn xuống" effect. The mask fades both edges: clipped lines
                dissolve at the top, and fresh text pours in faded at the bottom
                like a waterfall. */}
            <div
              ref={rollRef}
              className={`flex max-h-[240px] flex-col justify-end overflow-hidden${
                overflowing
                  ? " [-webkit-mask-image:linear-gradient(to_bottom,transparent,black_2.25rem,black_calc(100%_-_1.1rem),transparent)] [mask-image:linear-gradient(to_bottom,transparent,black_2.25rem,black_calc(100%_-_1.1rem),transparent)]"
                  : ""
              }`}
            >
              <div ref={feedRef}>
                {content ? (
                  <Markdown text={visible} streaming={streaming} />
                ) : (
                  <span className="animate-shimmer-text italic">
                    {hasTools ? t("think.lookup") : t("think.contemplating")}
                  </span>
                )}
              </div>
            </div>
          </div>
        ))}
    </div>
  );
});
