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

// Required tools of the selected packs the agent would not have: not in its
// tool list, or search_knowledge without a knowledge base. Mirrors the
// backend save check (CustomAgentConfig.ValidateDomainPacks).
export function missingDomainPackTools(
  packs: DomainPackInfo[],
  selected: string[],
  allowedTools: string[],
  kbSelectionMode: string,
): string[] {
  const missing = new Set<string>();
  for (const pack of packs) {
    if (!selected.includes(pack.name)) continue;
    for (const tool of pack.required_tools ?? []) {
      if (!allowedTools.includes(tool) || (tool === "search_knowledge" && kbSelectionMode === "none")) {
        missing.add(tool);
      }
    }
  }
  return [...missing];
}
