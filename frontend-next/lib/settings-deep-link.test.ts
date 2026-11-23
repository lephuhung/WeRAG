import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  consumeSettingsParam,
  consumeSettingsRedirect,
  consumeSettingsSearch,
  queueSettingsRedirect,
  markDeepLinkArrival,
  arrivedByDeepLink,
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

  it("hands a section several parameters together, once", () => {
    const storage = new MemoryStorage();

    queueSettingsRedirect(storage, "billing", "?scope=user&plan=p1");
    assert.equal(consumeSettingsSearch(storage, "usage"), null);
    const q = consumeSettingsSearch(storage, "billing");
    assert.equal(q?.get("scope"), "user");
    assert.equal(q?.get("plan"), "p1");
    assert.equal(consumeSettingsSearch(storage, "billing"), null);
  });

  it("keeps a deep-link visit quiet for a minute", () => {
    const storage = new MemoryStorage();
    assert.equal(arrivedByDeepLink(storage, 1_000), false);
    markDeepLinkArrival(storage, 1_000);
    assert.equal(arrivedByDeepLink(storage, 1_000), true);
    assert.equal(arrivedByDeepLink(storage, 61_000), true);
    assert.equal(arrivedByDeepLink(storage, 61_001), false);
  });

  it("drops a link's query left unread for over a minute", () => {
    const storage = new MemoryStorage();
    queueSettingsRedirect(storage, "billing", "?scope=user&plan=p1");
    assert.equal(consumeSettingsSearch(storage, "billing", Date.now() + 61_000), null);
    assert.equal(consumeSettingsSearch(storage, "billing"), null);
  });
});
