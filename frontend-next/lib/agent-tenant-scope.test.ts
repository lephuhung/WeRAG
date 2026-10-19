// Runs with: node --experimental-strip-types --test lib/agent-tenant-scope.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { scopeSummary, scopeTenantOptions, toggleScopeTenant } from "./agent-tenant-scope.ts";

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
