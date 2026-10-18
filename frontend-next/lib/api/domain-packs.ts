/* Domain packs: embedded read-only handbooks an agent opts into
 * (docs/domain-pack-standard.md). Kept free of app imports so the node test
 * runner can load it. */

// One shipped domain pack (GET /agents/domain-packs). Dates are YYYY-MM-DD;
// overdue = past review_by (the pack keeps working, answers say so).
export interface DomainPackInfo {
  name: string;
  description: string;
  version: string;
  as_of: string;
  review_by: string;
  overdue: boolean;
  required_tools: string[];
  domain: string;
  attribution: string;
}

// Tools a turn without any knowledge base does not get. Mirrors the
// backend tools.KnowledgeScopedTools: todo_write goes too when web search is
// also off.
export function knowledgeScopedTools(webSearchEnabled: boolean): string[] {
  const scoped = [
    "search_knowledge",
    "read_document",
    "list_documents",
    "search_document_section",
    "query_knowledge_graph",
    "database_query",
    "data_analysis",
    "data_schema",
    "wiki_read_page",
    "wiki_search",
    "wiki_flag_issue",
    "wiki_write_page",
    "wiki_replace_text",
    "wiki_rename_page",
    "wiki_delete_page",
    "wiki_read_issue",
    "wiki_update_issue",
  ];
  return webSearchEnabled ? scoped : [...scoped, "todo_write"];
}

export interface DomainPackAgentTools {
  allowedTools: string[];
  kbSelectionMode: string;
  knowledgeBases: string[];
  webSearchEnabled: boolean;
}

// Required tools of the selected packs the agent would not have: not in its
// tool list, or a knowledge tool on an agent with no knowledge base ("none",
// or "selected" with none picked; "all" cannot be checked here). Mirrors the
// backend save check (CustomAgentConfig.ValidateDomainPacks).
export function missingDomainPackTools(
  packs: DomainPackInfo[],
  selected: string[],
  agent: DomainPackAgentTools,
): string[] {
  const noKnowledge =
    agent.kbSelectionMode === "none" || (agent.kbSelectionMode === "selected" && agent.knowledgeBases.length === 0);
  const scoped = noKnowledge ? knowledgeScopedTools(agent.webSearchEnabled) : [];
  const missing = new Set<string>();
  for (const pack of packs) {
    if (!selected.includes(pack.name)) continue;
    for (const tool of pack.required_tools ?? []) {
      if (!agent.allowedTools.includes(tool) || scoped.includes(tool)) {
        missing.add(tool);
      }
    }
  }
  return [...missing];
}

// The selected names this build ships, in order; a stored pack that no
// longer ships is removed on save (the server refuses unknown names).
export function shippedDomainPacks(selected: string[], packs: DomainPackInfo[]): string[] {
  return selected.filter((name) => packs.some((p) => p.name === name));
}
