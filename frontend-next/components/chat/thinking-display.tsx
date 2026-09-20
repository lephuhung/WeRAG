"use client";

import { useState } from "react";
import { Markdown } from "@/components/markdown";

export function ThinkingDisplay({
  content,
  streaming,
}: {
  content: string;
  streaming?: boolean;
}) {
  // During active streaming, keep thinking open by default; close or open as user prefers
  const [expanded, setExpanded] = useState(true);

  if (!content && !streaming) return null;

  return (
    <div className="mb-4 overflow-hidden rounded-[14px] border border-hairline bg-surface-card text-left transition-all">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className="flex w-full items-center justify-between px-4 py-2.5 text-xs font-medium text-muted hover:text-ink transition-colors select-none"
      >
        <span className="flex items-center gap-2">
          {streaming ? (
            <span className="relative flex h-2 w-2">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-primary opacity-75" />
              <span className="relative inline-flex h-2 w-2 rounded-full bg-primary" />
            </span>
          ) : (
            <span className="inline-block h-2 w-2 rounded-full bg-muted-soft" />
          )}
          <span>{streaming ? "Thinking…" : "Thought process"}</span>
        </span>
        <span className="flex items-center gap-1 text-[11px] text-muted-soft">
          {expanded ? "Hide" : "Show"}
          <svg
            viewBox="0 0 20 20"
            fill="currentColor"
            className={`h-3.5 w-3.5 transition-transform duration-200 ${expanded ? "rotate-180" : ""}`}
          >
            <path
              fillRule="evenodd"
              d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z"
              clipRule="evenodd"
            />
          </svg>
        </span>
      </button>

      {expanded && (
        <div className="border-t border-hairline/60 bg-canvas/40 px-4 py-3 text-xs leading-relaxed text-muted font-mono">
          <div className="max-h-[300px] overflow-y-auto pr-1">
            {content ? (
              <Markdown text={content} />
            ) : (
              <span className="italic text-muted-soft">Contemplating…</span>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
