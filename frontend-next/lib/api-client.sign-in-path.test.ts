// Runs with: node --experimental-strip-types --test lib/api-client.sign-in-path.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { signInPath } from "./api-client.ts";

function at(pathname: string, search = "") {
  (globalThis as unknown as Record<string, unknown>).window = { location: { pathname, search, href: "" } };
}

describe("signInPath", () => {
  it("keeps a console deep link", () => {
    at("/platform/system/workspace/billing", "?x=1");
    assert.equal(signInPath(), `/login?next=${encodeURIComponent("/platform/system/workspace/billing?x=1")}`);
  });
  it("keeps a receipt link opened from the paid email", () => {
    at("/billing/receipt/WR7K2M");
    assert.equal(signInPath(), `/login?next=${encodeURIComponent("/billing/receipt/WR7K2M")}`);
  });
  it("sends the Word add-in to its own sign-in", () => {
    at("/word/billing/receipt/x");
    assert.equal(signInPath(), "/word");
  });
  it("drops other pages", () => {
    at("/gioi-thieu");
    assert.equal(signInPath(), "/login");
  });
});
