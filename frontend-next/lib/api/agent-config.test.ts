// B-2a(1) RED: editing an agent must merge unchanged config instead of
// replacing it, so fields the editor has no UI for (allowed_tools,
// selected_skills, sandbox_config_id, chat_parser_engine_rules, …) survive.
// Runs with: node --experimental-strip-types --test lib/api/agent-config.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { buildAgentConfigForSave, mergeAgentConfigForEdit } from "./agent-config.ts";
import type { CustomAgentConfig } from "./agents.ts";

const existing: CustomAgentConfig = {
  agent_mode: "smart-reasoning",
  model_id: "model-old",
  temperature: 0.7,
  allowed_tools: ["web_search", "code_run"],
  skills_selection_mode: "selected",
  selected_skills: ["skill-a"],
  sandbox_config_id: "sbx-1",
  chat_parser_engine_rules: [{ file_types: ["pdf"], engine: "native" }],
  kb_selection_mode: "all",
  knowledge_bases: ["kb-stale"],
  mcp_selection_mode: "all",
  mcp_services: ["mcp-stale"],
  web_search_enabled: true,
};

describe("mergeAgentConfigForEdit", () => {
  it("preserves unknown existing fields the editor has no UI for", () => {
    const edited: CustomAgentConfig = {
      agent_mode: "smart-reasoning",
      model_id: "model-old",
      temperature: 0.7,
      kb_selection_mode: "all",
      web_search_enabled: true,
    };
    const out = mergeAgentConfigForEdit(existing, edited);
    assert.deepEqual(out.allowed_tools, ["web_search", "code_run"]);
    assert.deepEqual(out.selected_skills, ["skill-a"]);
    assert.equal(out.skills_selection_mode, "selected");
    assert.equal(out.sandbox_config_id, "sbx-1");
    assert.deepEqual(out.chat_parser_engine_rules, [{ file_types: ["pdf"], engine: "native" }]);
  });

  it("explicitly overrides edited fields (deliberate KB/MCP selection changes)", () => {
    const edited: CustomAgentConfig = {
      agent_mode: "smart-reasoning",
      model_id: "model-new",
      temperature: 0.5,
      kb_selection_mode: "selected",
      knowledge_bases: ["kb-1", "kb-2"],
      mcp_selection_mode: "selected",
      mcp_services: ["mcp-1"],
      web_search_enabled: false,
    };
    const out = mergeAgentConfigForEdit(existing, edited);
    assert.equal(out.model_id, "model-new");
    assert.equal(out.temperature, 0.5);
    assert.equal(out.kb_selection_mode, "selected");
    assert.deepEqual(out.knowledge_bases, ["kb-1", "kb-2"]);
    assert.deepEqual(out.mcp_services, ["mcp-1"]);
    assert.equal(out.web_search_enabled, false);
    // Unknown fields still survive alongside the deliberate changes.
    assert.deepEqual(out.allowed_tools, ["web_search", "code_run"]);
    assert.equal(out.sandbox_config_id, "sbx-1");
  });

  it("lets intentionally cleared/disabled fields actually clear", () => {
    const edited: CustomAgentConfig = {
      agent_mode: "smart-reasoning",
      model_id: undefined, // cleared model picker
      kb_selection_mode: "all", // stale selected list must not linger
      knowledge_bases: undefined,
      mcp_selection_mode: "none", // stale MCP list must not linger
      mcp_services: undefined,
      vlm_model_id: undefined, // image upload disabled
      asr_model_id: undefined, // audio upload disabled
    };
    const out = mergeAgentConfigForEdit(existing, edited);
    assert.equal("model_id" in out, false);
    assert.equal("knowledge_bases" in out, false);
    assert.equal("mcp_services" in out, false);
    assert.equal("vlm_model_id" in out, false);
    assert.equal("asr_model_id" in out, false);
    assert.equal(out.kb_selection_mode, "all");
    assert.equal(out.mcp_selection_mode, "none");
  });

  it("does not mutate its inputs", () => {
    const edited: CustomAgentConfig = { model_id: "model-new" };
    const before = JSON.stringify(existing);
    mergeAgentConfigForEdit(existing, edited);
    assert.equal(JSON.stringify(existing), before);
  });
});

describe("buildAgentConfigForSave", () => {
  it("merges on EDIT", () => {
    const out = buildAgentConfigForSave(existing, { model_id: "m2" }, "edit");
    assert.equal(out.model_id, "m2");
    assert.deepEqual(out.allowed_tools, ["web_search", "code_run"]);
  });

  it("uses current defaults untouched on CREATE", () => {
    const edited: CustomAgentConfig = { agent_mode: "quick-answer", temperature: 0.7 };
    const out = buildAgentConfigForSave(existing, edited, "create");
    assert.deepEqual(out, edited);
    assert.equal("allowed_tools" in out, false);
  });
});
