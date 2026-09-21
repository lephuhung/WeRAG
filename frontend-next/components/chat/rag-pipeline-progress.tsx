"use client";

import { useMemo, useState } from "react";
import { IconDoc, IconSearch } from "@/components/icons";
import type { ToolEventItem } from "./tool-result-card";

export type RagStepItem = {
  id: string;
  toolName: string;
  title: string;
  pending: boolean;
  success?: boolean;
  summary?: string;
  durationMs?: number;
  output?: unknown;
};

export type KnowledgeReferenceItem = {
  knowledge_title?: string;
  knowledge_id?: string;
  chunk_id?: string;
  chunk_type?: string;
};

function formatSummary(event: ToolEventItem): string | undefined {
  let data = event.output;
  if (typeof data === "string") {
    const trimmed = data.trim();
    if (trimmed.startsWith("{") && trimmed.endsWith("}")) {
      try {
        data = JSON.parse(trimmed);
      } catch {
        return trimmed;
      }
    } else {
      return trimmed;
    }
  }
  if (!data || typeof data !== "object") {
    data = (event as unknown as { tool_data?: Record<string, unknown> }).tool_data;
  }
  if (!data || typeof data !== "object") return undefined;
  const rec = data as Record<string, unknown>;

  const count = Number(rec.count) || (Array.isArray(rec.results) ? rec.results.length : 0);
  const kbCounts = rec.kb_counts;
  const kbCount = kbCounts && typeof kbCounts === "object" ? Object.keys(kbCounts).length : 0;
  const docCount = Number(rec.doc_count) || 0;
  const webCount = Number(rec.web_count) || 0;

  if (count > 0) {
    if (kbCount > 0) {
      return `Tìm thấy ${count} kết quả từ ${kbCount} tài liệu`;
    }
    if (webCount > 0 && docCount > 0) {
      return `Tìm thấy ${count} kết quả (${docCount} tài liệu, ${webCount} web)`;
    }
    if (webCount > 0) {
      return `Tìm thấy ${webCount} kết quả từ web`;
    }
    return `Tìm thấy ${count} tài liệu tham khảo`;
  }
  return undefined;
}

function getStepTitle(event: ToolEventItem): string {
  const name = (event.tool_name || "").toLowerCase();
  const pending = event.status === "pending";

  if (name === "query_understand") {
    return pending ? "Đang phân tích câu hỏi..." : "Đã phân tích câu hỏi";
  }
  if (name === "knowledge_search" || name === "search_knowledge") {
    return pending ? "Đang tìm kiếm tài liệu..." : "Đã tìm kiếm tài liệu";
  }
  if (name === "attachment_parsing") {
    return pending ? "Đang xử lý tệp đính kèm..." : "Đã xử lý tệp đính kèm";
  }
  if (name === "image_analysis") {
    return pending ? "Đang phân tích hình ảnh..." : "Đã phân tích hình ảnh";
  }
  if (event.title) return event.title;
  if (name) return pending ? `Đang thực hiện ${name}...` : `Đã hoàn thành ${name}`;
  return pending ? "Đang xử lý..." : "Hoàn thành bước xử lý";
}

export function RagPipelineProgress({
  events = [],
  references = [],
  isStreaming = false,
  hasAnswer = false,
  isCompleted = false,
  onViewReferences,
}: {
  events?: ToolEventItem[];
  references?: KnowledgeReferenceItem[];
  isStreaming?: boolean;
  hasAnswer?: boolean;
  isCompleted?: boolean;
  onViewReferences?: () => void;
}) {
  const [userExpanded, setUserExpanded] = useState(false);

  // Compute total found documents count
  const docCount = useMemo(() => {
    if (references && references.length > 0) return references.length;
    for (const event of events) {
      if (!event.output) continue;
      let data = event.output;
      if (typeof data === "string") {
        try {
          data = JSON.parse(data.trim());
        } catch {
          // ignore
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

  // Normalize steps from toolEvents
  const steps: RagStepItem[] = useMemo(() => {
    if (events.length > 0) {
      return events.map((e) => ({
        id: e.id,
        toolName: e.tool_name || "",
        title: getStepTitle(e),
        pending: e.status === "pending",
        success: e.status !== "error",
        summary: formatSummary(e),
        output: e.output,
      }));
    }

    // Historical synthesis fallback: if no event recorded but citations exist
    if (references.length > 0) {
      return [
        {
          id: "synth-query",
          toolName: "query_understand",
          title: "Đã phân tích câu hỏi",
          pending: false,
          success: true,
        },
        {
          id: "synth-search",
          toolName: "knowledge_search",
          title: "Đã tìm kiếm tài liệu",
          pending: false,
          success: true,
          summary: `Tìm thấy ${references.length} tài liệu tham khảo`,
        },
      ];
    }

    return [];
  }, [events, references]);

  const hasSteps = steps.length > 0;

  // Pre-answer wait indicator (e.g. while starting up or calling pipeline before any event)
  const showPreWait = isStreaming && !hasAnswer && !hasSteps;

  // When answer has begun or reply is complete with answer, show collapsed summary by default
  const showCollapsed = hasAnswer && hasSteps;

  if (!hasSteps && !showPreWait) {
    return null;
  }

  const collapsedTitle =
    docCount > 0
      ? `Đã tìm kiếm và trả lời ${docCount} tài liệu tham khảo`
      : "Đã tìm kiếm và hoàn tất câu trả lời";

  return (
    <div className="mb-3 select-none">
      {/* 1. Pre-pipeline wait indicator */}
      {showPreWait && (
        <div className="flex items-center gap-2 py-1 text-xs">
          <span className="relative flex h-2 w-2 shrink-0">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-primary/40 opacity-75" />
            <span className="relative inline-flex h-2 w-2 rounded-full bg-primary" />
          </span>
          <span className="animate-shimmer-text font-medium text-xs">
            Đang chuẩn bị câu trả lời và tra cứu tri thức...
          </span>
        </div>
      )}

      {/* 2. Finished search bar (clean text row without white card background) */}
      {showCollapsed && (
        <div className="py-0.5">
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => setUserExpanded((v) => !v)}
              className="group inline-flex items-center gap-2 text-xs text-muted hover:text-ink transition-colors cursor-pointer py-1"
            >
              <span className="flex h-4 w-4 items-center justify-center rounded-full bg-emerald-500/15 text-[10px] font-bold text-emerald-600 dark:text-emerald-400 shrink-0">
                ✓
              </span>
              <span className="font-medium text-ink text-[13px]">
                {collapsedTitle}
              </span>
              <span className="flex items-center gap-1 text-[11px] text-muted-soft group-hover:text-muted transition-colors ml-1">
                <span>{userExpanded ? "Thu gọn" : "Chi tiết"}</span>
                <svg
                  viewBox="0 0 20 20"
                  fill="currentColor"
                  className={`h-3 w-3 transition-transform duration-200 ${userExpanded ? "rotate-180" : ""}`}
                >
                  <path
                    fillRule="evenodd"
                    d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z"
                    clipRule="evenodd"
                  />
                </svg>
              </span>
            </button>
            {docCount > 0 && onViewReferences && (
              <button
                type="button"
                onClick={onViewReferences}
                className="rounded-full bg-surface-strong/70 px-2 py-0.5 text-[11px] text-muted hover:text-ink hover:bg-surface-strong transition-colors cursor-pointer"
                title="Bấm để mở danh sách tài liệu tham khảo"
              >
                {docCount} tài liệu
              </button>
            )}
          </div>

          {userExpanded && (
            <div className="mt-2 pl-3 ml-2 border-l border-hairline/80 space-y-1.5 py-1">
              <StepList steps={steps} />
            </div>
          )}
        </div>
      )}

      {/* 3. Live active search timeline (while searching, without white card) */}
      {!showCollapsed && !showPreWait && hasSteps && (
        <div className="space-y-2 py-0.5">
          <div className="flex items-center gap-2 text-xs font-medium text-muted">
            <IconSearch className="h-3.5 w-3.5 text-muted shrink-0" />
            <span className={isStreaming ? "animate-shimmer-text font-medium" : "text-muted font-medium"}>
              {isStreaming ? "Đang tìm kiếm tài liệu..." : "Hoàn tất tìm kiếm"}
            </span>
          </div>
          <div className="pl-3 ml-1.5 border-l border-hairline/70 space-y-1.5 py-0.5">
            <StepList steps={steps} />
          </div>
        </div>
      )}
    </div>
  );
}

function StepList({ steps }: { steps: RagStepItem[] }) {
  return (
    <div className="space-y-1.5 text-xs">
      {steps.map((step) => {
        const isSearch =
          step.toolName.includes("search") ||
          step.toolName.includes("retrieval") ||
          step.toolName.includes("knowledge");

        return (
          <div
            key={step.id}
            className="flex items-start gap-2 py-0.5"
          >
            <div className="mt-0.5 shrink-0">
              {step.pending ? (
                <span className="relative flex h-3.5 w-3.5 items-center justify-center">
                  <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-primary/40 opacity-75" />
                  <span className="relative inline-flex h-2 w-2 rounded-full bg-primary" />
                </span>
              ) : step.success ? (
                <span className="flex h-3.5 w-3.5 items-center justify-center rounded-full bg-emerald-500/15 text-[10px] font-bold text-emerald-600 dark:text-emerald-400">
                  ✓
                </span>
              ) : (
                <span className="flex h-3.5 w-3.5 items-center justify-center rounded-full bg-red-500/15 text-[10px] font-bold text-red-600">
                  ✕
                </span>
              )}
            </div>

            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span
                  className={
                    step.pending
                      ? "animate-shimmer-text font-medium text-xs"
                      : "text-ink font-normal text-xs"
                  }
                >
                  {step.title}
                </span>
                {isSearch && !step.pending && (
                  <IconDoc className="h-3 w-3 text-muted-soft" />
                )}
              </div>
              {step.summary && (
                <div className="mt-0.5 text-[11px] text-muted leading-relaxed">
                  {step.summary}
                </div>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
