"use client";

/* Agent step timeline — port of the Vue smart-agent "thinking" tree
 * (frontend/src/views/chat/components/AgentStreamDisplay.vue +
 * utils/agent-tool-display.ts, utils/knowledgeChunksDisplay.ts,
 * utils/attachmentParsingDisplay.ts, utils/skillToolDisplay.ts,
 * utils/grepResultsGroup.ts, utils/agent-tool-icons.ts).
 *
 * The model is a single ordered list per assistant turn, mirroring the Vue
 * `agentEventStream`: thinking rounds, tool calls and context-compaction
 * markers in arrival order. */
import type { ReactNode } from "react";
import type { LocaleKey } from "@/lib/i18n";
import type { StreamChunk } from "@/lib/api/stream";
import type { ChatMessage } from "@/lib/api/chat";
import {
  IconBulb,
  IconSearch,
  IconGlobe,
  IconFileSearch,
  IconDoc,
  IconFolder,
  IconEdit,
  IconCode,
  IconArtifact,
} from "@/components/icons";

type TFn = (k: LocaleKey, vars?: Record<string, string | number>) => string;

export type ThinkingStep = {
  type: "thinking";
  id: string;
  /** Reasoning body (reasoning_content); streams incrementally while !done. */
  content: string;
  /** Folded tool-round preamble (the retracted answer text) — card title. */
  title?: string;
  done?: boolean;
};

export type ToolStep = {
  type: "tool";
  id: string;
  tool_name?: string;
  /** Call arguments (data.arguments of the tool_call event / step args). */
  args?: unknown;
  status: "pending" | "success" | "error";
  output?: unknown;
  error?: string;
  /** Structured result payload — the `data` object of tool_result events
   * (results, count, kb_counts, fetched_chunks, knowledge_title, ...). */
  tool_data?: Record<string, unknown>;
  /** Live stdout for a running shell_exec call (command_output events). */
  command_output?: Record<string, unknown>;
};

export type CompactedStep = {
  type: "compacted";
  id: string;
  tokens_before?: number;
  tokens_after?: number;
  degraded?: boolean;
  summary?: string;
};

export type AgentStepItem = ThinkingStep | ToolStep | CompactedStep;

// ---- stream → steps reducer --------------------------------------------------

/* Shared by the send path and the continue-stream replay path. Mutates `steps`;
 * callers snapshot with [...steps] before writing to state. */
export function applyChunkToSteps(steps: AgentStepItem[], c: StreamChunk): void {
  const kind = c.response_type ?? c.type;
  const d = c.data as Record<string, unknown> | undefined;

  if (kind === "thinking") {
    const id = typeof d?.event_id === "string" && d.event_id ? d.event_id : "thinking";
    let step = steps.find((s): s is ThinkingStep => s.type === "thinking" && s.id === id);
    let merged = false;
    if (!step) {
      const last = steps[steps.length - 1];
      if (last && last.type === "thinking") {
        // Vue buildFullEventList merges a contiguous run of thinking events
        // into one card — same here.
        step = last;
        merged = step.id !== id;
      } else {
        step = { type: "thinking", id, content: "" };
        steps.push(step);
      }
    }
    const chunk = typeof c.content === "string" ? c.content : "";
    if (chunk) {
      step.content += merged && step.content ? `\n\n${chunk}` : chunk;
      step.done = false;
    }
    if (c.done) step.done = true;
    return;
  }

  if (kind === "context_compacted") {
    steps.push({
      type: "compacted",
      id: c.id || `compact-${steps.length}`,
      tokens_before: Number(d?.tokens_before) || 0,
      tokens_after: Number(d?.tokens_after) || 0,
      degraded: Boolean(d?.degraded),
      summary: typeof d?.summary === "string" ? d.summary : undefined,
    });
    return;
  }

  if (kind === "tool_call") {
    const toolName = (typeof d?.tool_name === "string" && d.tool_name) || c.tool_name || "";
    if (toolName === "final_answer") return;
    const id =
      (d?.tool_call_id as string) ||
      c.tool_call_id ||
      (d?.event_id as string) ||
      `${toolName || "tool"}-${steps.length}`;
    const args = d?.arguments ?? c.tool_input ?? c.tool_data;
    const existing = steps.find((s): s is ToolStep => s.type === "tool" && s.id === id);
    if (existing) {
      if (toolName) existing.tool_name = toolName;
      if (args !== undefined) existing.args = args;
      existing.status = "pending";
    } else {
      steps.push({ type: "tool", id, tool_name: toolName, args, status: "pending" });
    }
    return;
  }

  if (kind === "command_output") {
    // Streaming stdout attaches to its still-pending shell_exec step; a late
    // chunk must not resurrect a finished call or stick to another one with
    // the same tool name.
    const callId = (d?.tool_call_id as string) || c.tool_call_id;
    if (!callId) return;
    const tool = steps.find((s): s is ToolStep => s.type === "tool" && s.id === callId);
    if (tool?.status === "pending" && tool.tool_name === "shell_exec") {
      const out = tool.command_output as { content?: string; done?: boolean } | undefined;
      const content = typeof d?.content === "string" ? d.content : "";
      tool.command_output = {
        ...d,
        content: (out?.content ?? "") + content,
        done: Boolean(d?.done),
      };
    }
    return;
  }

  if (kind === "tool_result" || kind === "error") {
    const toolName = (typeof d?.tool_name === "string" && d.tool_name) || c.tool_name || "";
    if (toolName === "final_answer") return; // never a timeline row (Vue skips it)
    const callId = (d?.tool_call_id as string) || c.tool_call_id || (d?.event_id as string);
    // An error event with no tool identity is a fatal turn error — the caller
    // owns that path, not the timeline.
    if (kind === "error" && !toolName && !callId) return;
    let step = callId
      ? steps.find((s): s is ToolStep => s.type === "tool" && s.id === callId)
      : undefined;
    if (!step && toolName) {
      step = steps.find(
        (s): s is ToolStep => s.type === "tool" && s.tool_name === toolName && s.status === "pending",
      );
    }
    const success = kind !== "error" && c.success !== false && d?.success !== false;
    if (!step) {
      step = {
        type: "tool",
        id: callId || `${toolName || "tool"}-${steps.length}`,
        tool_name: toolName,
        status: "pending",
      };
      steps.push(step);
    }
    step.status = success ? "success" : "error";
    if (toolName) step.tool_name = toolName;
    step.output = d?.output ?? c.tool_output ?? c.content;
    if (d) step.tool_data = d;
    step.error = success ? undefined : String(d?.error ?? c.content ?? "Tool error");
  }
}

/* Turn finished (complete/stop/abort): nothing stays "pending" forever —
 * calls without a result are shown as done rather than shimmering forever. */
export function finalizeSteps(steps: AgentStepItem[]): void {
  for (const s of steps) {
    if (s.type === "tool" && s.status === "pending") s.status = "success";
    if (s.type === "thinking") s.done = true;
  }
}

// ---- history reconstruction (port of reconstructEventStreamFromSteps) --------

export function stepsFromHistory(m: ChatMessage): AgentStepItem[] {
  const out: AgentStepItem[] = [];
  const steps = m.agent_steps;
  if (!Array.isArray(steps)) return out;
  steps.forEach((step, i) => {
    const reasoning = typeof step.reasoning_content === "string" ? step.reasoning_content.trim() : "";
    const thought = typeof step.thought === "string" ? step.thought.trim() : "";
    // A non-intermediate `thought` is the round preamble — Vue folds it into
    // the thinking card as its title with reasoning_content as the body.
    if (reasoning || (thought && !step.intermediate_answer)) {
      out.push({
        type: "thinking",
        id: `h-step-${i}-thought`,
        title: thought && !step.intermediate_answer ? thought : undefined,
        content: reasoning,
        done: true,
      });
    }
    (step.tool_calls ?? []).forEach((call, j) => {
      if (call.name === "final_answer") return;
      const result = call.result;
      const toolName = call.target?.name || call.name;
      const data = result?.data as Record<string, unknown> | undefined;
      if (toolName === "thinking") {
        out.push({
          type: "thinking",
          id: call.id || `h-step-${i}-think-${j}`,
          content: String(data?.thought || result?.output || ""),
          done: true,
        });
        return;
      }
      out.push({
        type: "tool",
        id: call.id || `h-step-${i}-tool-${j}`,
        tool_name: toolName,
        args: call.target?.args || call.args,
        status: result?.success === false ? "error" : "success",
        output: result?.output,
        error: result?.error,
        tool_data: data,
      });
    });
  });
  return out;
}

// ---- value extraction helpers -------------------------------------------------

function asRecord(value: unknown): Record<string, unknown> {
  if (!value) return {};
  if (typeof value === "string") {
    try {
      const parsed = JSON.parse(value);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
        return parsed as Record<string, unknown>;
      }
    } catch {
      return {};
    }
    return {};
  }
  if (typeof value === "object" && !Array.isArray(value)) return value as Record<string, unknown>;
  return {};
}

function collectQueryStrings(value: unknown): string[] {
  if (value == null) return [];
  if (typeof value === "string") {
    const trimmed = value.trim();
    if (!trimmed) return [];
    if (trimmed.startsWith("[")) {
      try {
        const parsed = JSON.parse(trimmed);
        if (Array.isArray(parsed)) {
          return parsed.filter((q): q is string => typeof q === "string" && Boolean(q.trim()));
        }
      } catch {
        /* treat as a single query string */
      }
    }
    return [trimmed];
  }
  if (Array.isArray(value)) {
    return value.filter((q): q is string => typeof q === "string" && Boolean(q.trim()));
  }
  return [];
}

export function getQueryText(args: unknown): string {
  const record = asRecord(args);
  if (Object.keys(record).length === 0) return "";
  const queries = [...collectQueryStrings(record.query), ...collectQueryStrings(record.queries)];
  return Array.from(new Set(queries)).join("，");
}

function getWikiPageText(args: unknown): string {
  const record = asRecord(args);
  const slugs = [...collectQueryStrings(record.slug), ...collectQueryStrings(record.slugs)];
  return Array.from(new Set(slugs)).join("、");
}

type RetrievalSearchSource = "knowledge" | "web" | "mixed";

function getRetrievalSearchSource(args: unknown, toolData?: Record<string, unknown> | null): RetrievalSearchSource {
  const fromArgs = String(asRecord(args).search_source || "");
  const fromData = toolData ? String(toolData.search_source || "") : "";
  const source = (fromData || fromArgs).trim();
  return source === "web" || source === "mixed" ? source : "knowledge";
}

function grepPatterns(step: ToolStep): string[] {
  const src = Object.keys(asRecord(step.args)).length ? asRecord(step.args) : asRecord(step.tool_data);
  if (Array.isArray(src.queries)) return src.queries.map(String);
  if (Array.isArray(src.patterns)) return src.patterns.map(String);
  if (src.query) return [String(src.query)];
  if (src.pattern) return [String(src.pattern)];
  return [];
}

function eventFields(step: ToolStep): Record<string, unknown> {
  return { ...asRecord(step.args), ...asRecord(step.tool_data) };
}

function stringField(record: Record<string, unknown>, key: string): string {
  const v = record[key];
  return typeof v === "string" ? v.trim() : v == null ? "" : String(v).trim();
}

const skillNameOf = (step: ToolStep) => stringField(eventFields(step), "skill_name");
const sandboxPathOf = (step: ToolStep) => stringField(eventFields(step), "path");
const readSkillTargetOf = (step: ToolStep) => {
  const f = eventFields(step);
  const skill = stringField(f, "skill_name");
  const file = stringField(f, "file_path");
  return skill && file ? `${skill}/${file}` : skill || file;
};

/* Port of skillScriptCommandLabel: the command actually run for
 * execute_skill_script / shell_exec — `command`, else skill/script_path + args. */
function scriptCommandLabel(step: ToolStep): string {
  const f = eventFields(step);
  const fromData = stringField(f, "command");
  if (fromData) return fromData;
  const skill = stringField(f, "skill_name");
  const script = stringField(f, "script_path");
  const path = [skill, script].filter(Boolean).join("/");
  const args = f.args;
  if (Array.isArray(args) && args.length) {
    return [path, ...args.map((a) => String(a))].filter(Boolean).join(" ");
  }
  return path;
}

const previewShellCommand = (command: string, max = 72): string => {
  const trimmed = command.replace(/\s+/g, " ").trim();
  return trimmed.length <= max ? trimmed : `${trimmed.slice(0, max - 1)}…`;
};

function countGrepDocuments(td: Record<string, unknown> | undefined): number {
  if (!td) return 0;
  if (typeof td.document_count === "number" && td.document_count >= 0) return td.document_count;
  if (Array.isArray(td.knowledge_results) && td.knowledge_results.length) {
    return td.knowledge_results.length;
  }
  if (Array.isArray(td.chunk_results) && td.chunk_results.length) {
    const ids = new Set(
      (td.chunk_results as Record<string, unknown>[])
        .map((r) => r?.knowledge_id)
        .filter((x): x is string => typeof x === "string" && Boolean(x)),
    );
    return ids.size || td.chunk_results.length;
  }
  return 0;
}

// ---- localized tool names ------------------------------------------------------

const TOOL_NAME_KEYS: Record<string, LocaleKey> = {
  discover_mcp_tools: "step.tool.mcpDiscover",
  call_mcp_tool: "step.tool.mcpCall",
  search_knowledge: "step.tool.searchKnowledge",
  knowledge_search: "step.tool.searchKnowledge",
  read_document: "step.tool.readDocument",
  list_documents: "step.tool.listDocuments",
  grep_chunks: "step.tool.grepChunks",
  web_search: "step.tool.webSearch",
  web_fetch: "step.tool.webFetch",
  get_document_info: "step.tool.getDocumentInfo",
  list_knowledge_chunks: "step.tool.listKnowledgeChunks",
  get_related_documents: "step.tool.getRelatedDocuments",
  get_document_content: "step.tool.getDocumentContent",
  wiki_search: "step.tool.wikiSearch",
  wiki_read_page: "step.tool.wikiReadPage",
  wiki_read_source_doc: "step.tool.wikiReadSourceDoc",
  todo_write: "step.tool.todoWrite",
  knowledge_graph_extract: "step.tool.knowledgeGraphExtract",
  thinking: "step.tool.thinking",
  attachment_parsing: "step.tool.attachmentParsing",
  image_analysis: "step.tool.imageAnalysis",
  query_understand: "step.tool.queryUnderstand",
  query_knowledge_graph: "step.tool.queryKnowledgeGraph",
  read_skill: "step.tool.readSkill",
  read_file: "step.tool.readFile",
  execute_skill_script: "step.tool.executeSkillScript",
  list_sandbox_files: "step.tool.listSandboxFiles",
  read_sandbox_file: "step.tool.readSandboxFile",
  write_sandbox_file: "step.tool.writeSandboxFile",
  edit_sandbox_file: "step.tool.editSandboxFile",
  shell_exec: "step.tool.shellExec",
  data_analysis: "step.tool.dataAnalysis",
  data_schema: "step.tool.dataSchema",
  database_query: "step.tool.databaseQuery",
  local_browser: "step.localBrowser",
};

export function localizedToolName(t: TFn, toolName?: string | null): string {
  if (!toolName) return t("step.toolFallback");
  const key = TOOL_NAME_KEYS[toolName];
  if (key) return t(key);
  // "mcp_my_server_search_docs" → "My Server Search Docs"
  if (toolName.startsWith("mcp_")) {
    return toolName
      .slice(4)
      .split("_")
      .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
      .join(" ");
  }
  return toolName;
}

// ---- titles --------------------------------------------------------------------

const withDetail = (base: string, detail: string) =>
  detail.trim() ? `${base}：「${detail.trim()}」` : base;

const SANDBOX_FILE_TOOLS = new Set([
  "list_sandbox_files",
  "read_file",
  "read_sandbox_file",
  "write_sandbox_file",
  "edit_sandbox_file",
]);

/* Port of getToolTitle / getToolDescription / getRagPipelineStepTitle. */
export function toolStepTitle(t: TFn, step: ToolStep): string {
  const name = step.tool_name || "";
  const pending = step.status === "pending";
  const failed = step.status === "error";
  const query = getQueryText(step.args) || getQueryText(step.tool_data);
  const loc = (n: string) => localizedToolName(t, n);
  const quoted = (base: string) => (query ? `${base}：「${query}」` : base);

  if (pending) {
    if (name === "image_analysis") return t("step.imageAnalyzing");
    if (name === "attachment_parsing") return t("step.attachmentParsing");
    if (name === "query_understand") return t("step.queryUnderstanding");
    if (name === "shell_exec") return t("step.shellExecRunning");
    if (name === "wiki_search" || name === "wiki_read_page") return `${loc(name)}...`;
    if (name === "read_skill") return `${withDetail(loc(name), readSkillTargetOf(step))}...`;
    if (name === "execute_skill_script") return `${withDetail(loc(name), skillNameOf(step))}...`;
    if (SANDBOX_FILE_TOOLS.has(name)) return `${withDetail(loc(name), sandboxPathOf(step))}...`;
    return t("step.calling", { name: loc(name) });
  }

  if (name === "search_knowledge" || name === "knowledge_search") {
    const source = getRetrievalSearchSource(step.args, step.tool_data);
    const key = failed
      ? source === "web"
        ? "step.webSearchFailed"
        : source === "mixed"
          ? "step.searchMixedFailed"
          : "step.searchKbFailed"
      : source === "web"
        ? "step.webSearch"
        : source === "mixed"
          ? "step.searchMixed"
          : "step.searchKb";
    return quoted(t(key as LocaleKey));
  }
  if (name === "wiki_search") {
    return quoted(failed ? t("step.calledFailed", { name: loc(name) }) : loc(name));
  }
  if (name === "web_search") {
    return quoted(t(failed ? "step.webSearchFailed" : "step.webSearch"));
  }
  if (name === "grep_chunks") {
    const base = t(failed ? "step.grepSearchFailed" : "step.grepSearch");
    const pats = grepPatterns(step);
    if (pats.length) {
      const shown = pats.slice(0, 2).join("、");
      const more = pats.length > 2 ? ` +${pats.length - 2}` : "";
      return `${base}：「${shown}${more}」`;
    }
    return base;
  }
  if (name === "wiki_read_page") {
    const page = String(
      step.tool_data?.title || getWikiPageText(step.args) || getWikiPageText(step.tool_data) || "",
    ).trim();
    const base = failed ? t("step.calledFailed", { name: loc(name) }) : loc(name);
    return page ? `${base}：「${page}」` : base;
  }
  if (name === "get_document_info" || name === "list_documents") {
    const title = step.tool_data?.title;
    if (!failed && title) return t("step.res.getDocument", { title: String(title) });
    return t(failed ? "step.getDocInfoFailed" : "step.getDocInfo");
  }
  if (
    name === "read_document" ||
    name === "list_knowledge_chunks" ||
    name === "get_document_content" ||
    name === "wiki_read_source_doc"
  ) {
    const td = step.tool_data;
    if (!failed && td) {
      if (td.faq_question) return t("step.res.listFaq", { question: String(td.faq_question) });
      if (td.fetched_chunks !== undefined || td.knowledge_title || td.knowledge_id) {
        return t("step.res.listChunks", {
          title: String(td.knowledge_title || td.knowledge_id || t("step.res.document")),
        });
      }
    }
    return failed ? t("step.calledFailed", { name: t("step.viewDocument") }) : t("step.viewDocument");
  }
  if (name === "todo_write") {
    if (step.tool_data?.steps) return t("step.updatePlan");
    return t(failed ? "step.updateTodosFailed" : "step.updateTodos");
  }
  if (name === "thinking") return t(failed ? "step.thinkingFailed" : "step.thinkingDone");
  if (name === "image_analysis") {
    return t(failed ? "step.imageAnalysisFailed" : "step.imageAnalysisDone");
  }
  if (name === "attachment_parsing") {
    return t(failed ? "step.attachmentParsingFailed" : "step.attachmentParsingDone");
  }
  if (name === "query_understand") {
    return failed
      ? t("step.calledFailed", { name: loc(name) })
      : t("step.queryUnderstandDone");
  }
  if (name === "read_skill") {
    const base = failed ? t("step.calledFailed", { name: loc(name) }) : loc(name);
    return withDetail(base, readSkillTargetOf(step));
  }
  if (SANDBOX_FILE_TOOLS.has(name)) {
    const base = failed ? t("step.calledFailed", { name: loc(name) }) : loc(name);
    return withDetail(base, sandboxPathOf(step));
  }
  if (name === "execute_skill_script") {
    const skill = skillNameOf(step);
    const base = withDetail(failed ? t("step.calledFailed", { name: loc(name) }) : loc(name), skill);
    const command = previewShellCommand(scriptCommandLabel(step));
    const rest = skill && command.startsWith(`${skill}/`) ? command.slice(skill.length + 1) : command;
    return rest ? `${base}：${rest}` : base;
  }
  if (name === "shell_exec") {
    const base = failed ? t("step.calledFailed", { name: loc(name) }) : loc(name);
    const command = previewShellCommand(scriptCommandLabel(step));
    return command ? `${base}：${command}` : base;
  }
  if (name === "local_browser") {
    return failed ? t("step.calledFailed", { name: t("step.localBrowser") }) : t("step.localBrowser");
  }
  const localized = loc(name);
  return t(failed ? "step.calledFailed" : "step.called", { name: localized });
}

// ---- result summaries ------------------------------------------------------------

/* Vue interpolates <strong>{count}</strong> inside the i18n string; here the
 * template is split on {placeholders} and the listed vars render bold. */
function tparts(
  t: TFn,
  key: LocaleKey,
  vars: Record<string, string | number>,
  bold: string[] = [],
): ReactNode {
  const raw = t(key); // no vars → placeholders kept
  return raw.split(/\{(\w+)\}/g).map((seg, i) => {
    if (i % 2 === 0) return seg;
    const text = vars[seg] === undefined ? `{${seg}}` : String(vars[seg]);
    return bold.includes(seg) ? (
      <strong key={i} className="font-semibold text-body">
        {text}
      </strong>
    ) : (
      <span key={i}>{text}</span>
    );
  });
}

const joinNodes = (nodes: ReactNode[], sep = " · "): ReactNode =>
  nodes.filter(Boolean).map((n, i) => (
    <span key={i}>
      {i > 0 ? sep : ""}
      {n}
    </span>
  ));

function resultsCount(td: Record<string, unknown> | undefined): number {
  if (!td) return 0;
  return (Array.isArray(td.results) ? td.results.length : 0) || Number(td.count) || 0;
}

/* Port of getKnowledgeSearchSummaryHtml. */
function searchSummary(t: TFn, td: Record<string, unknown> | undefined): ReactNode {
  if (!td) return null;
  const count = resultsCount(td);
  if (count === 0) {
    const candidates = Number(td.candidate_count) || 0;
    return candidates > 0
      ? tparts(t, "step.res.belowThreshold", { count: candidates }, ["count"])
      : t("step.res.noResults");
  }
  const source = getRetrievalSearchSource(null, td);
  const webCount = Number(td.web_count) || 0;
  const docCount = Number(td.doc_count) || 0;
  if (source === "web" || (webCount > 0 && docCount === 0)) {
    return tparts(t, "step.res.web", { count }, ["count"]);
  }
  const kbCounts = td.kb_counts;
  const kbCount = kbCounts && typeof kbCounts === "object" ? Object.keys(kbCounts).length : 0;
  if (kbCount > 0) {
    return tparts(t, "step.res.fromFiles", { count, files: kbCount }, ["count", "files"]);
  }
  if (source === "mixed" && docCount > 0 && webCount > 0) {
    return tparts(t, "step.res.mixed", { count, docCount, webCount }, [
      "count",
      "docCount",
      "webCount",
    ]);
  }
  return tparts(t, "step.res.found", { count }, ["count"]);
}

/* Port of getKnowledgeChunksSummaryHtml. */
function chunksSummary(t: TFn, td: Record<string, unknown> | undefined): ReactNode {
  if (!td || td.fetched_chunks === undefined) return null;
  const query = typeof td.query === "string" ? td.query.trim() : "";
  if (query) {
    const count = `${Number(td.match_count ?? 0)}${td.truncated ? "+" : ""}`;
    return Number(td.match_count ?? 0) > 0
      ? tparts(t, "step.res.queryMatches", { query, count }, ["count"])
      : tparts(t, "step.res.queryNoMatch", { query, count }, ["count"]);
  }
  const fetched = Number(td.fetched_chunks ?? 0);
  const total = Number(td.total_chunks ?? 0);
  const parts: ReactNode[] = [
    tparts(
      t,
      "step.res.chunkRange",
      { fetched, total: td.total_chunks == null ? "?" : String(td.total_chunks) },
      ["fetched", "total"],
    ),
  ];
  if (td.offset !== undefined) {
    if (fetched > 0 && fetched < total) {
      const from = Number(td.offset) + 1;
      parts.push(tparts(t, "step.res.offset", { from, to: from + fetched - 1 }));
    }
    return joinNodes(parts);
  }
  const pageSize = Number(td.page_size ?? 0);
  if (total > pageSize && pageSize > 0) {
    parts.push(tparts(t, "step.res.page", { page: Number(td.page ?? 1), pageSize }));
  }
  return joinNodes(parts);
}

function grepSummary(t: TFn, td: Record<string, unknown> | undefined): ReactNode {
  if (!td) return null;
  const totalChunks = Number(td.total_matches ?? 0) || 0;
  if (totalChunks === 0) return t("step.res.noResults");
  return tparts(t, "step.res.grep", { chunks: totalChunks, docs: countGrepDocuments(td) }, [
    "chunks",
    "docs",
  ]);
}

function planSummary(t: TFn, td: Record<string, unknown> | undefined): ReactNode {
  const steps = td?.steps;
  if (!Array.isArray(steps)) return null;
  const count = (s: string) =>
    (steps as Record<string, unknown>[]).filter((x) => x?.status === s).length;
  const parts: ReactNode[] = [];
  if (count("in_progress") > 0)
    parts.push(`${t("step.plan.inProgress")} ${count("in_progress")}`);
  if (count("pending") > 0) parts.push(`${t("step.plan.pending")} ${count("pending")}`);
  if (count("completed") > 0) parts.push(`${t("step.plan.completed")} ${count("completed")}`);
  return parts.length ? joinNodes(parts) : null;
}

/* Port of getAttachmentParsingSummaryHtml. */
function attachSummary(t: TFn, step: ToolStep): ReactNode {
  if (step.status === "error") {
    const err = String(step.error || step.output || "")
      .trim()
      .replace(/^附件解析失败:\s*/i, "")
      .trim();
    return err || null;
  }
  const td = step.tool_data;
  let parsed = Number(td?.parsed_count) || 0;
  let skipped = Number(td?.skipped_count) || 0;
  if (!td || td.parsed_count === undefined) {
    const output = String(step.output || "");
    parsed =
      Number(
        output.match(/已解析\s*(\d+)\s*个附件/)?.[1] ??
          output.match(/Parsed\s*(\d+)\s*attachment/i)?.[1] ??
          0,
      ) || 0;
    skipped =
      Number(
        output.match(/(\d+)\s*个未完成已跳过/)?.[1] ?? output.match(/(\d+)\s*skipped/i)?.[1] ?? 0,
      ) || 0;
  }
  if (parsed === 0 && skipped === 0) return t("step.res.attachNone");
  if (skipped > 0) {
    return tparts(t, "step.res.attachSkipped", { parsed, skipped }, ["parsed", "skipped"]);
  }
  return tparts(t, "step.res.attachParsed", { count: parsed }, ["count"]);
}

/* Second line under a tool step — result summary per tool (Vue's fixed
 * search/plan/chunks/attachment summary rows). */
export function toolStepSummary(t: TFn, step: ToolStep): ReactNode {
  if (step.status === "pending") return null;
  const td = step.tool_data;
  switch (step.tool_name) {
    case "search_knowledge":
    case "knowledge_search":
      return searchSummary(t, td);
    case "web_search": {
      const count = resultsCount(td);
      return count > 0 ? tparts(t, "step.res.webSearch", { count }, ["count"]) : null;
    }
    case "grep_chunks":
      return grepSummary(t, td);
    case "read_document":
    case "list_knowledge_chunks":
      return chunksSummary(t, td);
    case "todo_write":
      return planSummary(t, td);
    case "attachment_parsing":
      return attachSummary(t, step);
    case "thinking":
      return td?.thought ? t("step.deepThinking") : null;
    default:
      return null;
  }
}

// ---- thinking text ----------------------------------------------------------------

const UUID_RE = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/gi;
const ID_LABEL_RE = /\b(knowledge_base_id|knowledge_id|chunk_id|knowledge_base_ids)\s*[:=]\s*/gi;

/* One-line snippet for a collapsed thinking row (Vue getThinkingSummary). */
export function thinkingSummaryText(content: string): string {
  if (!content) return "";
  const cleaned = content
    .replace(ID_LABEL_RE, "")
    .replace(UUID_RE, "")
    .replace(/^#+\s+/gm, "")
    .replace(/\*\*/g, "")
    .replace(/\*/g, "")
    .replace(/`/g, "")
    .replace(/\n+/g, " ")
    .trim();
  return cleaned.length <= 50 ? cleaned : `${cleaned.slice(0, 50)}...`;
}

// ---- icons --------------------------------------------------------------------------

export function toolStepIcon(step: ToolStep): ReactNode {
  const cls = "h-3.5 w-3.5";
  const name = step.tool_name || "";
  if (name === "thinking" || name === "image_analysis" || name === "query_understand") {
    return <IconBulb className={cls} />;
  }
  if (name === "search_knowledge" || name === "knowledge_search") {
    return getRetrievalSearchSource(step.args, step.tool_data) === "knowledge" ? (
      <IconSearch className={cls} />
    ) : (
      <IconGlobe className={cls} />
    );
  }
  if (name === "web_search" || name === "web_fetch" || name === "local_browser") {
    return <IconGlobe className={cls} />;
  }
  if (name === "wiki_search" || name === "grep_chunks" || name === "discover_mcp_tools") {
    return <IconSearch className={cls} />;
  }
  if (
    name === "read_document" ||
    name === "list_documents" ||
    name === "get_document_info" ||
    name === "list_knowledge_chunks" ||
    name === "get_document_content" ||
    name === "wiki_read_page" ||
    name === "wiki_read_source_doc" ||
    name === "get_related_documents"
  ) {
    return <IconFileSearch className={cls} />;
  }
  if (name === "todo_write") return <IconArtifact className={cls} />;
  if (name === "attachment_parsing") return <IconDoc className={cls} />;
  if (name === "call_mcp_tool" || name === "shell_exec" || name === "execute_skill_script" || name.startsWith("mcp_")) {
    return <IconCode className={cls} />;
  }
  if (name === "list_sandbox_files") return <IconFolder className={cls} />;
  if (name === "read_file" || name === "read_sandbox_file" || name === "read_skill") {
    return <IconDoc className={cls} />;
  }
  if (name === "write_sandbox_file" || name === "edit_sandbox_file") {
    return <IconEdit className={cls} />;
  }
  return <IconDoc className={cls} />;
}

// ---- header summary -------------------------------------------------------------------

/* Vue formatDuration: "20s" / "1m 5s" / "320ms". */
export function compactDuration(ms: number): string {
  if (!ms || ms <= 0) return "0s";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
}

/* Port of intermediateStepsSummary:
 * "4 reasoning round(s) · 3 tool call(s) · 20s" with bolded numbers. */
export function stepsSummaryNodes(
  t: TFn,
  steps: AgentStepItem[],
  durationMs: number,
): ReactNode {
  const rounds = steps.filter(
    (s) => s.type === "thinking" || (s.type === "tool" && s.tool_name === "thinking"),
  ).length;
  const tools = steps.filter((s) => s.type === "tool" && s.tool_name !== "thinking").length;
  const parts: ReactNode[] = [];
  if (rounds > 0) parts.push(tparts(t, "think.rounds", { rounds }, ["rounds"]));
  if (tools > 0) parts.push(tparts(t, "think.toolCalls", { tools }, ["tools"]));
  if (parts.length === 0) {
    parts.push(tparts(t, "think.steps", { steps: steps.length }, ["steps"]));
  }
  if (durationMs > 0) {
    parts.push(<strong className="font-semibold text-body">{compactDuration(durationMs)}</strong>);
  }
  return joinNodes(parts);
}
