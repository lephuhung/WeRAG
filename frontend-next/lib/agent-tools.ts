/* The agent tools a SuperAdmin can allow per agent (config.allowed_tools),
 * grouped for the agent editor. Mirrors internal/agent/tools/definitions.go
 * (plus check_document_format, which the document assistant lists); ported
 * from frontend/src/views/agent/AgentEditorModal.vue (allTools / toolGroups).
 *
 * Tools the server adds on its own are not listed: web_search / web_fetch
 * follow the Web toggle, MCP tools follow the MCP services, search_memory /
 * resolve_abbreviation / check_legal_status / search_document_section are
 * injected when their switches allow. */

export type ToolGroupKey =
  | "base"
  | "rag"
  | "wiki_read"
  | "wiki_edit"
  | "wiki_issue"
  | "data"
  | "document"
  | "illustration";

/** What a tool needs before it actually runs (shown as a hint). */
export type ToolNeed = "kb" | "document" | "imageService";

export type AgentToolDef = {
  name: string;
  group: ToolGroupKey;
  /** writes or deletes content */
  danger?: boolean;
  needs?: ToolNeed;
};

export const TOOL_GROUPS: ToolGroupKey[] = [
  "base",
  "rag",
  "wiki_read",
  "wiki_edit",
  "wiki_issue",
  "data",
  "document",
  "illustration",
];

export const AGENT_TOOLS: AgentToolDef[] = [
  { name: "thinking", group: "base" },
  { name: "todo_write", group: "base" },

  { name: "search_knowledge", group: "rag", needs: "kb" },
  { name: "read_document", group: "rag", needs: "kb" },
  { name: "list_documents", group: "rag", needs: "kb" },
  { name: "query_knowledge_graph", group: "rag", needs: "kb" },
  { name: "search_conversations", group: "rag" },
  { name: "database_query", group: "rag", needs: "kb" },

  { name: "wiki_search", group: "wiki_read", needs: "kb" },
  { name: "wiki_read_page", group: "wiki_read", needs: "kb" },
  { name: "wiki_flag_issue", group: "wiki_read", needs: "kb" },

  { name: "wiki_write_page", group: "wiki_edit", danger: true, needs: "kb" },
  { name: "wiki_replace_text", group: "wiki_edit", danger: true, needs: "kb" },
  { name: "wiki_rename_page", group: "wiki_edit", danger: true, needs: "kb" },
  { name: "wiki_delete_page", group: "wiki_edit", danger: true, needs: "kb" },

  { name: "wiki_read_issue", group: "wiki_issue", needs: "kb" },
  { name: "wiki_update_issue", group: "wiki_issue", needs: "kb" },

  { name: "data_analysis", group: "data", needs: "kb" },
  { name: "data_schema", group: "data", needs: "kb" },

  { name: "check_document_format", group: "document", needs: "document" },
  { name: "read_document_outline", group: "document", needs: "document" },
  { name: "apply_format_fixes", group: "document", needs: "document" },
  { name: "rewrite_paragraphs", group: "document", needs: "document" },
  { name: "insert_paragraphs", group: "document", needs: "document" },
  { name: "mark_passages", group: "document", needs: "document" },
  { name: "check_spelling", group: "document", needs: "document" },

  { name: "draw_diagram", group: "illustration" },
  { name: "generate_illustration", group: "illustration", needs: "imageService" },
];

const KNOWN = new Set(AGENT_TOOLS.map((t) => t.name));

/** What a new agent gets (DefaultAllowedTools on the server). */
export const DEFAULT_ALLOWED_TOOLS = ["search_knowledge", "read_document", "list_documents", "search_conversations"];

/** Retired names → their successors (legacyToolSuccessors on the server). */
const LEGACY_TOOLS: Record<string, string> = {
  knowledge_search: "search_knowledge",
  grep_chunks: "search_knowledge",
  list_knowledge_chunks: "read_document",
  get_document_info: "read_document",
  wiki_read_source_doc: "read_document",
};

/** Retired names mapped to their successors, duplicates dropped, order kept. */
export function normalizeAllowedTools(list: readonly string[] | null | undefined): string[] {
  const out: string[] = [];
  for (const raw of list ?? []) {
    const name = LEGACY_TOOLS[raw] ?? raw;
    if (name && !out.includes(name)) out.push(name);
  }
  return out;
}

/** Allowed entries this editor has no checkbox for (kept as they are). */
export function unlistedTools(list: readonly string[]): string[] {
  return list.filter((n) => !KNOWN.has(n));
}

/** Toggles one tool, keeping the order of the others. */
export function toggleTool(list: readonly string[], name: string): string[] {
  return list.includes(name) ? list.filter((n) => n !== name) : [...list, name];
}

/** Why a checked tool will not run with this configuration, or null. */
export function toolInactiveReason(tool: AgentToolDef, ctx: { hasKb: boolean; agentMode: string }): ToolNeed | "mode" | null {
  if (ctx.agentMode !== "smart-reasoning") return "mode";
  if (tool.needs === "kb" && !ctx.hasKb) return "kb";
  return null;
}
