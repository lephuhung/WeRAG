import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  consumeSettingsRedirect,
  queueSettingsRedirect,
} from "./settings-deep-link.ts";

class MemoryStorage {
  private values = new Map<string, string>();

  getItem(key: string) {
    return this.values.get(key) ?? null;
  }

  setItem(key: string, value: string) {
    this.values.set(key, value);
  }

  removeItem(key: string) {
    this.values.delete(key);
  }
}

describe("settings deep-link handoff", () => {
  it("hands the requested section to the destination page exactly once", () => {
    const storage = new MemoryStorage();

    queueSettingsRedirect(storage, "storage");

    assert.equal(consumeSettingsRedirect(storage), "storage");
    assert.equal(consumeSettingsRedirect(storage), null);
  });
});
