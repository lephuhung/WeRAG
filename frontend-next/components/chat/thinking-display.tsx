"use client";

import { memo, useEffect, useMemo, useRef, useState } from "react";
import { Markdown } from "@/components/markdown";
import type { ToolEventItem } from "./tool-result-card";
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

/* Unified reasoning block: the RAG/tool pipeline and the thought process are
 * ONE collapsible section — the header carries the live status + duration +
 * document count, the expanded body shows the thinking stream (and a compact
 * tool-status line). Replaces the old separate RagPipelineProgress bar. */
export const ThinkingDisplay = memo(function ThinkingDisplay({
  content,
  streaming,
  events = [],
  references = [],
  onViewReferences,
}: {
  content: string;
  streaming?: boolean;
  events?: ToolEventItem[];
  references?: KnowledgeReferenceItem[];
  onViewReferences?: () => void;
}) {
  // Open while streaming; finished thinking (history or completed turns)
  // mounts collapsed — mirrors "khi think xong tự động đóng".
  const [expanded, setExpanded] = useState(() => Boolean(streaming));
  const rollRef = useRef<HTMLDivElement>(null);
  const feedRef = useRef<HTMLDivElement>(null);
  const [overflowing, setOverflowing] = useState(false);
  const wasStreaming = useRef(false);
  const startedAt = useRef<number | null>(null);
  const [elapsed, setElapsed] = useState<number | null>(null);

  const toolCount = events.length;
  const hasTools = toolCount > 0;
  const toolsPending = hasTools && events.some((e) => e.status === "pending");

  // Total found documents count (references list wins; otherwise peek at tool
  // outputs for a count only — never a tool name or payload).
  const docCount = useMemo(() => {
    if (references && references.length > 0) return references.length;
    for (const event of events) {
      if (!event.output) continue;
      let data = event.output;
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
  }, [references, events]);

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

  if (!content && !streaming && !hasTools) return null;

  const label = streaming
    ? content || !hasTools
      ? "Đang suy nghĩ…"
      : "Đang tra cứu & suy nghĩ…"
    : content
      ? "Đã suy nghĩ"
      : "Đã xử lý";

  return (
    <div className="mb-4 text-left">
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          aria-expanded={expanded}
          className="flex min-w-0 flex-1 items-center gap-2 py-1.5 text-xs font-medium text-muted transition-colors select-none hover:text-ink"
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
            {label}
            {!streaming && elapsed != null && (
              <span className="font-normal text-muted-soft"> · {fmtDuration(elapsed)}</span>
            )}
            {hasTools && (
              <span className="font-normal text-muted-soft"> · {toolCount} công cụ</span>
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
              d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25-4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z"
              clipRule="evenodd"
            />
          </svg>
        </button>
        {docCount > 0 && onViewReferences && (
          <button
            type="button"
            onClick={onViewReferences}
            className="shrink-0 cursor-pointer rounded-full bg-surface-strong/70 px-2 py-0.5 text-[11px] text-muted transition-colors hover:bg-surface-strong hover:text-ink"
            title="Bấm để mở danh sách tài liệu tham khảo"
          >
            {docCount} tài liệu
          </button>
        )}
      </div>

      {expanded && (
        <div className="ml-1 border-l-2 border-hairline pl-3.5 pb-1 pt-0.5 text-[13px] leading-relaxed text-muted [&_.chat-markdown]:text-muted">
          {hasTools && (
            <div className="mb-1.5 flex items-center gap-1.5 text-[11.5px] text-muted-soft">
              {toolsPending ? (
                <span className="animate-shimmer-text">Đang sử dụng công cụ…</span>
              ) : (
                <span>
                  Đã sử dụng {toolCount} công cụ
                  {docCount > 0 ? ` · ${docCount} tài liệu` : ""}
                </span>
              )}
            </div>
          )}
          {truncated && (
            <div className="mb-1.5 text-[10px] italic text-muted-soft">
              … showing last {MAX_VISIBLE_CHARS.toLocaleString()} characters
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
                  {hasTools ? "Đang tra cứu dữ liệu…" : "Contemplating…"}
                </span>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
});
