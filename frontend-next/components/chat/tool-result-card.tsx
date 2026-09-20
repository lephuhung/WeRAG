"use client";

import { useState } from "react";
import { IconSearch, IconDoc } from "@/components/icons";

export type ToolEventItem = {
  id: string;
  tool_name?: string;
  title?: string;
  input?: unknown;
  output?: unknown;
  status?: "pending" | "success" | "error";
  error?: string;
};

function getToolIcon(name?: string) {
  if (!name) return <IconDoc className="h-3.5 w-3.5 text-muted" />;
  const n = name.toLowerCase();
  if (n.includes("search") || n.includes("web") || n.includes("fetch")) {
    return <IconSearch className="h-3.5 w-3.5 text-primary" />;
  }
  return <IconDoc className="h-3.5 w-3.5 text-muted" />;
}

function formatJson(val: unknown): string {
  if (!val) return "";
  if (typeof val === "string") return val;
  try {
    return JSON.stringify(val, null, 2);
  } catch {
    return String(val);
  }
}

export function ToolResultCard({ event }: { event: ToolEventItem }) {
  const [expanded, setExpanded] = useState(false);
  const isPending = event.status === "pending";
  const isError = event.status === "error";

  const label = event.title || event.tool_name || "Tool invocation";

  return (
    <div className="mb-2 overflow-hidden rounded-[12px] border border-hairline bg-surface-card text-left text-xs transition-colors">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className="flex w-full items-center justify-between px-3.5 py-2 hover:bg-surface-strong/40 transition-colors select-none"
      >
        <span className="flex items-center gap-2 min-w-0">
          <span className="shrink-0">{getToolIcon(event.tool_name)}</span>
          <span className="truncate font-medium text-ink">
            {label}
          </span>
          {isPending && (
            <span className="flex items-center gap-1 text-[11px] text-primary">
              <span className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-primary" />
              Running
            </span>
          )}
          {isError && (
            <span className="rounded bg-error/10 px-1.5 py-0.5 text-[10px] font-medium text-error">
              Failed
            </span>
          )}
        </span>

        <span className="shrink-0 flex items-center gap-1 text-[11px] text-muted-soft">
          <span>{expanded ? "Less" : "Details"}</span>
          <svg
            viewBox="0 0 20 20"
            fill="currentColor"
            className={`h-3 w-3 transition-transform duration-200 ${expanded ? "rotate-180" : ""}`}
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
        <div className="border-t border-hairline/60 bg-canvas/30 px-3.5 py-2.5 space-y-2 font-mono text-[11px] text-muted">
          {event.input !== undefined && (
            <div>
              <div className="text-[10px] uppercase font-semibold text-muted-soft mb-1">Input</div>
              <pre className="max-h-[150px] overflow-x-auto overflow-y-auto rounded bg-surface-strong/50 p-2 text-ink">
                {formatJson(event.input)}
              </pre>
            </div>
          )}
          {(event.output !== undefined || event.error) && (
            <div>
              <div className="text-[10px] uppercase font-semibold text-muted-soft mb-1">
                {event.error ? "Error" : "Output"}
              </div>
              <pre className={`max-h-[200px] overflow-x-auto overflow-y-auto rounded p-2 ${
                event.error ? "bg-error/10 text-error" : "bg-surface-strong/50 text-ink"
              }`}>
                {event.error ? event.error : formatJson(event.output)}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
