// Runs with: node --experimental-strip-types --test lib/api/agent-doc-defaults.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { clampOpenDocumentMaxRunes, defaultWebSearchFor, isDocumentAssistantAgent } from "./agent-doc-defaults.ts";

describe("isDocumentAssistantAgent", () => {
  it("matches the built-in id or the format-check tool", () => {
    assert.equal(isDocumentAssistantAgent("builtin-document-assistant"), true);
    assert.equal(isDocumentAssistantAgent("a1", { allowed_tools: ["knowledge_search", "check_document_format"] }), true);
    assert.equal(isDocumentAssistantAgent("a1", { allowed_tools: ["knowledge_search"] }), false);
    assert.equal(isDocumentAssistantAgent(undefined, null), false);
  });
});

describe("defaultWebSearchFor", () => {
  it("is on only when the agent asks for it and has web search", () => {
    assert.equal(defaultWebSearchFor({ web_search_default_on: true, web_search_enabled: true }), true);
    assert.equal(defaultWebSearchFor({ web_search_default_on: true }), true);
    assert.equal(defaultWebSearchFor({ web_search_default_on: true, web_search_enabled: false }), false);
    assert.equal(defaultWebSearchFor({ web_search_default_on: false, web_search_enabled: true }), false);
    assert.equal(defaultWebSearchFor({ web_search_enabled: true }), false);
    assert.equal(defaultWebSearchFor(undefined), false);
  });
});

describe("clampOpenDocumentMaxRunes", () => {
  it("clamps to 0..60000, 0 for junk", () => {
    assert.equal(clampOpenDocumentMaxRunes(12000), 12000);
    assert.equal(clampOpenDocumentMaxRunes(90000), 60000);
    assert.equal(clampOpenDocumentMaxRunes(-5), 0);
    assert.equal(clampOpenDocumentMaxRunes("abc"), 0);
    assert.equal(clampOpenDocumentMaxRunes(1500.6), 1501);
  });
});
