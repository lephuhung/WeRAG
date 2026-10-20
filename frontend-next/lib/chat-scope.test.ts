import { test } from "node:test";
import assert from "node:assert/strict";
import { filterRemoved, scopeKey, withAdded, withRemoved, type Scope } from "./chat-scope.ts";

const scope: Scope = {
  selectedKnowledgeBases: ["kb-a", "kb-b"],
  selectedFiles: ["file-1", "file-2"],
  selectedFileKbMap: { "file-1": "kb-a", "file-2": "kb-b" },
  selectedTags: [{ id: "tag-1", name: "tag one", kbId: "kb-a" }],
  selectedMCPServices: ["mcp-1"],
  selectedSkills: ["skill-1"],
};

test("a removal is recorded and never restored", () => {
  const removed = withRemoved(scope, [], "kb", "kb-a");
  assert.deepEqual(removed.scope.selectedKnowledgeBases, ["kb-b"]);
  assert.deepEqual(removed.removedScopeItems, [scopeKey("kb", "kb-a")]);

  // An older conversation that used the removed knowledge base must not
  // bring it back.
  const restored = filterRemoved(scope, removed.removedScopeItems);
  assert.deepEqual(restored.selectedKnowledgeBases, ["kb-b"]);
});

test("picking an item again clears its removal", () => {
  const removed = withRemoved(scope, [], "kb", "kb-a");
  const picked = withAdded(removed.scope, removed.removedScopeItems, "kb", "kb-a");
  assert.deepEqual(picked.scope.selectedKnowledgeBases, ["kb-b", "kb-a"]);
  assert.deepEqual(picked.removedScopeItems, []);
  // …and the item survives a session scope that still lists it.
  assert.deepEqual(filterRemoved(scope, picked.removedScopeItems).selectedKnowledgeBases, ["kb-a", "kb-b"]);
});

test("removals cover every scope kind", () => {
  let removed: string[] = [];
  let current = scope;
  for (const [kind, id] of [
    ["kb", "kb-a"],
    ["file", "file-1"],
    ["tag", "tag-1"],
    ["mcp", "mcp-1"],
    ["skill", "skill-1"],
  ] as const) {
    const next = withRemoved(current, removed, kind, id, kind === "tag" ? "kb-a" : undefined);
    current = next.scope;
    removed = next.removedScopeItems;
  }
  const kept = filterRemoved(scope, removed);
  assert.deepEqual(kept.selectedKnowledgeBases, ["kb-b"]);
  assert.deepEqual(kept.selectedFiles, ["file-2"]);
  assert.deepEqual(kept.selectedTags, []);
  assert.deepEqual(kept.selectedMCPServices, []);
  assert.deepEqual(kept.selectedSkills, []);
});

test("a removed file drops its knowledge base link", () => {
  const removed = withRemoved(scope, [], "file", "file-1");
  const kept = filterRemoved(scope, removed.removedScopeItems);
  assert.deepEqual(kept.selectedFileKbMap, { "file-2": "kb-b" });
});

test("a tag is only removed in its own knowledge base", () => {
  const removed = withRemoved(scope, [], "tag", "tag-1", "kb-b");
  assert.deepEqual(removed.scope.selectedTags, [{ id: "tag-1", name: "tag one", kbId: "kb-a" }]);
});
