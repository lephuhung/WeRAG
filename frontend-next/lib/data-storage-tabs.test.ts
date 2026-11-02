import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { resolveDataStorageTab } from "./data-storage-tabs.ts";

describe("resolveDataStorageTab", () => {
  it("opens the storage tab for the legacy storage section", () => {
    assert.equal(resolveDataStorageTab("storage"), "storage");
  });

  it("defaults the merged menu and vector section to the vector tab", () => {
    assert.equal(resolveDataStorageTab("data-storage"), "vector");
    assert.equal(resolveDataStorageTab("vector"), "vector");
    assert.equal(resolveDataStorageTab(undefined), "vector");
  });
});
