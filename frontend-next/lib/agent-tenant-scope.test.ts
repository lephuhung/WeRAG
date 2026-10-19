// Runs with: node --experimental-strip-types --test lib/agent-tenant-scope.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  availableAgentId,
  defaultDistribution,
  normalizeDistribution,
  scopeForSave,
  switchOffNeedsConfirm,
  agentEnabled,
  scopeSummary,
  scopeTenantOptions,
  toggleScopeTenant,
} from "./agent-tenant-scope.ts";

describe("scopeTenantOptions", () => {
  it("keeps workspaces on a configuration profile, sorted by name", () => {
    const got = scopeTenantOptions([
      { id: 10009, name: "Cấu hình chung", kind: "config_profile" },
      { id: 10003, name: "Phòng Thuế", kind: "workspace", config_profile_id: 10009 },
      { id: 10001, name: "Ban Giám đốc", config_profile_id: 10009 },
      { id: 20000, name: "Độc lập", kind: "workspace" },
    ]);
    assert.deepEqual(
      got.map((t) => t.id),
      [10001, 10003],
    );
  });
});

describe("toggleScopeTenant", () => {
  it("adds sorted and removes", () => {
    assert.deepEqual(toggleScopeTenant([10005], 10003), [10003, 10005]);
    assert.deepEqual(toggleScopeTenant([10003, 10005], 10003), [10005]);
    assert.deepEqual(toggleScopeTenant([], 1), [1]);
  });
});

describe("scopeSummary", () => {
  const names: Record<number, string> = { 1: "Phòng A", 2: "Phòng B", 3: "Phòng C" };
  it("is null for every Tổ chức", () => {
    assert.equal(scopeSummary([], (id) => names[id]), null);
    assert.equal(scopeSummary(undefined, (id) => names[id]), null);
  });
  it("names the first ones and counts the rest", () => {
    assert.deepEqual(scopeSummary([1], (id) => names[id]), { names: ["Phòng A"], more: 0 });
    assert.deepEqual(scopeSummary([1, 2, 3], (id) => names[id]), { names: ["Phòng A", "Phòng B"], more: 1 });
    assert.deepEqual(scopeSummary([7], (id) => names[id]), { names: ["#7"], more: 0 });
  });
});

describe("availableAgentId", () => {
  const listed = ["builtin-smart-reasoning", "builtin-document-assistant", "custom-1"];
  it("keeps a listed choice", () => {
    assert.equal(availableAgentId(listed, "custom-1", "builtin-smart-reasoning"), "custom-1");
  });
  it("replaces an unlisted custom agent (lost Tổ chức áp dụng) and an unlisted built-in", () => {
    assert.equal(availableAgentId(listed, "custom-gone", "builtin-smart-reasoning"), "builtin-smart-reasoning");
    assert.equal(availableAgentId(listed, "builtin-quick-answer", "builtin-smart-reasoning"), "builtin-smart-reasoning");
  });
  it("falls back to the first listed agent, or keeps the choice when nothing is listed", () => {
    assert.equal(availableAgentId(["custom-1"], "custom-gone", "builtin-smart-reasoning"), "custom-1");
    assert.equal(availableAgentId([], "custom-gone", "builtin-smart-reasoning"), null);
  });
});

describe("distribution", () => {
  it("normalizes unknown values to basic", () => {
    assert.equal(normalizeDistribution("addon"), "addon");
    assert.equal(normalizeDistribution("internal"), "internal");
    assert.equal(normalizeDistribution(undefined), "basic");
    assert.equal(normalizeDistribution("everyone"), "basic");
  });
  it("defaults like the server: list → internal, skill set → addon, else basic", () => {
    assert.equal(defaultDistribution([], []), "basic");
    assert.equal(defaultDistribution(["opc-vn-business-law"], []), "addon");
    assert.equal(defaultDistribution(["opc-vn-business-law"], [10003]), "internal");
  });
  it("keeps the list only for internal", () => {
    assert.deepEqual(scopeForSave("internal", [10003]), [10003]);
    assert.deepEqual(scopeForSave("basic", [10003]), []);
    assert.deepEqual(scopeForSave("addon", [10003]), []);
  });
});

describe("agent switch", () => {
  it("asks before switching off a basic agent only", () => {
    assert.equal(switchOffNeedsConfirm("builtin-document-assistant", false), true);
    assert.equal(switchOffNeedsConfirm("builtin-smart-reasoning", false), true);
    assert.equal(switchOffNeedsConfirm("builtin-smart-reasoning", true), false);
    assert.equal(switchOffNeedsConfirm("builtin-quick-answer", false), false);
  });
  it("reads an absent flag as on", () => {
    assert.equal(agentEnabled({}), true);
    assert.equal(agentEnabled({ enabled: false }), false);
  });
});
