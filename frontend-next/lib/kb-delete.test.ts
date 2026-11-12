// Runs with: npm run test:kb-delete (Node >= 22.6, type stripping)
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { kbDeleteWarnings, kbNameMatches, normalizeKbName } from "./kb-delete.ts";

describe("kb name confirmation", () => {
  it("folds Unicode forms and whitespace", () => {
    const nfd = "Kho tài liệu"; // "Kho tài liệu" with combining marks
    assert.equal(normalizeKbName(nfd), "Kho tài liệu");
    assert.equal(kbNameMatches(nfd, "Kho tài liệu"), true);
    assert.equal(kbNameMatches("  Kho   tài\tliệu ", "Kho tài liệu"), true);
    assert.equal(kbNameMatches("Kho tài liệu", "Kho  tài liệu "), true);
  });
  it("rejects other names and empty names", () => {
    assert.equal(kbNameMatches("Kho tai lieu", "Kho tài liệu"), false);
    assert.equal(kbNameMatches("", ""), false);
    assert.equal(kbNameMatches(" ", "  "), false);
  });
});

describe("kb delete warnings", () => {
  it("warns for public/published KBs and grantees", () => {
    assert.deepEqual(kbDeleteWarnings(undefined), []);
    assert.deepEqual(kbDeleteWarnings({ visibility: "tenant", grantees: 0 }), []);
    assert.deepEqual(kbDeleteWarnings({ visibility: "public" }), [{ key: "kbDelete.warnPublic" }]);
    assert.deepEqual(kbDeleteWarnings({ visibility: "published", grantees: 2 }), [
      { key: "kbDelete.warnPublished" },
      { key: "kbDelete.warnGrantees", vars: { count: 2 } },
    ]);
  });
});
