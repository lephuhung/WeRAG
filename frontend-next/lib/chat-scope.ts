/* The quoted scope of a chat: knowledge bases, files, tags, MCP services and
 * skills.
 *
 * Two scopes exist and used to be one:
 *
 *  · the STICKY scope — what the user picked and what a new chat starts
 *    with. It is persisted, so the selection survives a reload and applies
 *    to the next chat;
 *  · the SESSION scope — what an existing conversation was created with.
 *    Opening that conversation shows its own sources.
 *
 * They used to be the same field, so opening an old chat overwrote the
 * sticky scope and a removed knowledge base came back on the next chat.
 * The removal is now recorded in `removedScopeItems` (a tombstone): the
 * session scope is filtered through it, so something the user deleted is
 * never restored behind their back. Picking the item again clears the
 * tombstone.
 *
 * Pure functions only — lib/chat-context.tsx applies them, and
 * components/create-chat.tsx writes the same shape into localStorage.
 */

export const SCOPE_FIELDS = [
  "selectedKnowledgeBases",
  "selectedFiles",
  "selectedFileKbMap",
  "selectedTags",
  "selectedMCPServices",
  "selectedSkills",
] as const;

export type ScopeField = (typeof SCOPE_FIELDS)[number];

export type Scope = {
  selectedKnowledgeBases: string[];
  selectedFiles: string[];
  selectedFileKbMap: Record<string, string>;
  selectedTags: Array<{ id: string; name: string; kbId: string; kbName?: string }>;
  selectedMCPServices: string[];
  selectedSkills: string[];
};

/** A stable key per scope item — the tombstone list is keyed by it. */
export function scopeKey(kind: "kb" | "file" | "tag" | "mcp" | "skill", id: string, kbId?: string): string {
  if (kind === "tag") return `tag:${kbId ?? ""}:${id}`;
  return `${kind}:${id}`;
}

/** The scope part of the chat settings. */
export function scopeOf(settings: {
  selectedKnowledgeBases: string[];
  selectedFiles: string[];
  selectedFileKbMap: Record<string, string>;
  selectedTags: Array<{ id: string; name: string; kbId: string; kbName?: string }>;
  selectedMCPServices: string[];
  selectedSkills: string[];
}): Scope {
  return {
    selectedKnowledgeBases: settings.selectedKnowledgeBases,
    selectedFiles: settings.selectedFiles,
    selectedFileKbMap: settings.selectedFileKbMap,
    selectedTags: settings.selectedTags,
    selectedMCPServices: settings.selectedMCPServices,
    selectedSkills: settings.selectedSkills,
  };
}

/** Drops every item the user removed from the scope. */
export function filterRemoved(scope: Scope, removedScopeItems: string[]): Scope {
  const removed = new Set(removedScopeItems);
  const keptFiles = scope.selectedFiles.filter((id) => !removed.has(scopeKey("file", id)));
  const keptMap: Record<string, string> = {};
  for (const id of keptFiles) {
    const kb = scope.selectedFileKbMap[id];
    if (kb) keptMap[id] = kb;
  }
  return {
    selectedKnowledgeBases: scope.selectedKnowledgeBases.filter((id) => !removed.has(scopeKey("kb", id))),
    selectedFiles: keptFiles,
    selectedFileKbMap: keptMap,
    selectedTags: scope.selectedTags.filter((t) => !removed.has(scopeKey("tag", t.id, t.kbId))),
    selectedMCPServices: scope.selectedMCPServices.filter((id) => !removed.has(scopeKey("mcp", id))),
    selectedSkills: scope.selectedSkills.filter((name) => !removed.has(scopeKey("skill", name))),
  };
}

/** Adds the item and clears its tombstone — an explicit pick wins. */
export function withAdded(
  scope: Scope,
  removedScopeItems: string[],
  kind: "kb" | "file" | "tag" | "mcp" | "skill",
  id: string,
  extra?: { kbId?: string; kbName?: string; name?: string },
): { scope: Scope; removedScopeItems: string[] } {
  const key = scopeKey(kind, id, extra?.kbId);
  const next: Scope = { ...scope };
  if (kind === "kb" && !next.selectedKnowledgeBases.includes(id)) {
    next.selectedKnowledgeBases = [...next.selectedKnowledgeBases, id];
  } else if (kind === "file" && !next.selectedFiles.includes(id)) {
    next.selectedFiles = [...next.selectedFiles, id];
    if (extra?.kbId) next.selectedFileKbMap = { ...next.selectedFileKbMap, [id]: extra.kbId };
  } else if (kind === "tag" && !next.selectedTags.some((t) => t.id === id && t.kbId === extra?.kbId)) {
    next.selectedTags = [...next.selectedTags, { id, name: extra?.name ?? id, kbId: extra?.kbId ?? "", kbName: extra?.kbName }];
  } else if (kind === "mcp" && !next.selectedMCPServices.includes(id)) {
    next.selectedMCPServices = [...next.selectedMCPServices, id];
  } else if (kind === "skill" && !next.selectedSkills.includes(id)) {
    next.selectedSkills = [...next.selectedSkills, id];
  }
  return { scope: next, removedScopeItems: removedScopeItems.filter((k) => k !== key) };
}

/** Removes the item and records the tombstone, so it cannot come back. */
export function withRemoved(
  scope: Scope,
  removedScopeItems: string[],
  kind: "kb" | "file" | "tag" | "mcp" | "skill",
  id: string,
  kbId?: string,
): { scope: Scope; removedScopeItems: string[] } {
  const key = scopeKey(kind, id, kbId);
  const next: Scope = { ...scope };
  if (kind === "kb") {
    next.selectedKnowledgeBases = scope.selectedKnowledgeBases.filter((x) => x !== id);
  } else if (kind === "file") {
    next.selectedFiles = scope.selectedFiles.filter((x) => x !== id);
    const map = { ...scope.selectedFileKbMap };
    delete map[id];
    next.selectedFileKbMap = map;
  } else if (kind === "tag") {
    next.selectedTags = scope.selectedTags.filter((t) => !(t.id === id && (!kbId || t.kbId === kbId)));
  } else if (kind === "mcp") {
    next.selectedMCPServices = scope.selectedMCPServices.filter((x) => x !== id);
  } else if (kind === "skill") {
    next.selectedSkills = scope.selectedSkills.filter((x) => x !== id);
  }
  return {
    scope: next,
    removedScopeItems: removedScopeItems.includes(key) ? removedScopeItems : [...removedScopeItems, key],
  };
}
