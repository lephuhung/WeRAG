// Runs with: node --experimental-strip-types --test lib/api/domain-packs.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  knowledgeScopedTools,
  missingDomainPackTools,
  shippedDomainPacks,
  type DomainPackAgentTools,
  type DomainPackInfo,
} from "./domain-packs.ts";
import { buildAgentConfigForSave } from "./agent-config.ts";

const pack = (name: string, required_tools: string[]): DomainPackInfo => ({
  name,
  description: "",
  version: "1.0.0",
  as_of: "2026-10-17",
  review_by: "2027-04-17",
  overdue: false,
  required_tools,
  domain: "",
  attribution: "",
});

describe("missingDomainPackTools", () => {
  const packs = [pack("opc", ["search_knowledge", "compare_tax_methods"]), pack("reader", ["read_document"])];
  const agent = (over: Partial<DomainPackAgentTools> = {}): DomainPackAgentTools => ({
    allowedTools: ["search_knowledge", "compare_tax_methods", "read_document"],
    kbSelectionMode: "selected",
    knowledgeBases: ["kb-1"],
    webSearchEnabled: false,
    ...over,
  });

  it("lists required tools of selected packs the agent lacks", () => {
    assert.deepEqual(missingDomainPackTools(packs, ["opc"], agent({ allowedTools: ["search_knowledge"] })), [
      "compare_tax_methods",
    ]);
  });

  it("ignores packs that are not selected", () => {
    assert.deepEqual(missingDomainPackTools(packs, [], agent({ allowedTools: [] })), []);
  });

  it("counts knowledge tools as missing without a knowledge base", () => {
    assert.deepEqual(missingDomainPackTools(packs, ["opc"], agent({ kbSelectionMode: "none" })), ["search_knowledge"]);
    assert.deepEqual(missingDomainPackTools(packs, ["reader"], agent({ knowledgeBases: [] })), ["read_document"]);
  });

  it("cannot judge \"all\" and leaves it to the server", () => {
    assert.deepEqual(missingDomainPackTools(packs, ["opc", "reader"], agent({ kbSelectionMode: "all", knowledgeBases: [] })), []);
  });

  it("mirrors todo_write going with web search off", () => {
    assert.equal(knowledgeScopedTools(false).includes("todo_write"), true);
    assert.equal(knowledgeScopedTools(true).includes("todo_write"), false);
  });
});

describe("shippedDomainPacks", () => {
  it("drops names this build no longer ships, keeping order", () => {
    assert.deepEqual(shippedDomainPacks(["gone", "reader", "opc"], [pack("opc", []), pack("reader", [])]), ["reader", "opc"]);
  });
});

describe("domain_packs on save", () => {
  it("keeps stored packs when the edit does not touch them", () => {
    const saved = buildAgentConfigForSave({ domain_packs: ["opc"] } as never, { agent_mode: "smart-reasoning" } as never, "edit");
    assert.deepEqual(saved.domain_packs, ["opc"]);
  });

  it("clears them when the edit sends undefined", () => {
    const saved = buildAgentConfigForSave({ domain_packs: ["opc"] } as never, { domain_packs: undefined } as never, "edit");
    assert.equal("domain_packs" in saved, false);
  });
});
