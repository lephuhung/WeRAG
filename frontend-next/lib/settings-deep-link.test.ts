import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  consumeSettingsParam,
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

  it("hands the link's query to its section once, not to another one", () => {
    const storage = new MemoryStorage();

    queueSettingsRedirect(storage, "abbreviations", "?status=pending");
    assert.equal(consumeSettingsRedirect(storage), "abbreviations");
    assert.equal(consumeSettingsParam(storage, "storage", "status"), "");
    assert.equal(consumeSettingsParam(storage, "abbreviations", "status"), "pending");
    assert.equal(consumeSettingsParam(storage, "abbreviations", "status"), "");

    // a later link without a query clears an unused one
    queueSettingsRedirect(storage, "abbreviations", "?status=pending");
    queueSettingsRedirect(storage, "abbreviations");
    assert.equal(consumeSettingsParam(storage, "abbreviations", "status"), "");
  });
});
