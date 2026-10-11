// Agent tool catalogue of the agent editor.
// Runs with: node --experimental-strip-types --test lib/agent-tools.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  AGENT_TOOLS,
  TOOL_GROUPS,
  normalizeAllowedTools,
  toggleTool,
  toolInactiveReason,
  unlistedTools,
} from "./agent-tools.ts";

describe("agent tools", () => {
  it("lists every tool once, in a known group", () => {
    const names = AGENT_TOOLS.map((t) => t.name);
    assert.equal(new Set(names).size, names.length);
    for (const t of AGENT_TOOLS) assert.ok(TOOL_GROUPS.includes(t.group), t.name);
    for (const n of ["draw_diagram", "generate_illustration", "check_spelling", "check_document_format"]) {
      assert.ok(names.includes(n), n);
    }
  });

  it("maps retired names and drops duplicates", () => {
    assert.deepEqual(normalizeAllowedTools(["grep_chunks", "search_knowledge", "get_document_info", "x"]), [
      "search_knowledge",
      "read_document",
      "x",
    ]);
    assert.deepEqual(normalizeAllowedTools(undefined), []);
  });

  it("keeps unlisted entries and the order when toggling", () => {
    const list = ["resolve_abbreviation", "search_knowledge"];
    assert.deepEqual(unlistedTools(list), ["resolve_abbreviation"]);
    assert.deepEqual(toggleTool(list, "draw_diagram"), [...list, "draw_diagram"]);
    assert.deepEqual(toggleTool(list, "search_knowledge"), ["resolve_abbreviation"]);
  });

  it("explains why a checked tool will not run", () => {
    const search = AGENT_TOOLS.find((t) => t.name === "search_knowledge")!;
    const diagram = AGENT_TOOLS.find((t) => t.name === "draw_diagram")!;
    assert.equal(toolInactiveReason(search, { hasKb: false, agentMode: "smart-reasoning" }), "kb");
    assert.equal(toolInactiveReason(search, { hasKb: true, agentMode: "smart-reasoning" }), null);
    assert.equal(toolInactiveReason(diagram, { hasKb: false, agentMode: "quick-answer" }), "mode");
  });
});
