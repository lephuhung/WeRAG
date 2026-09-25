// RED: login `next` must be a validated same-origin local pathname.
// Runs with: node --experimental-strip-types --test lib/safe-next.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { resolveSafeNextPath, DEFAULT_POST_LOGIN_ROUTE } from "./safe-next.ts";

describe("resolveSafeNextPath (issue 2)", () => {
  it("falls back for missing/empty values", () => {
    assert.equal(resolveSafeNextPath(null), DEFAULT_POST_LOGIN_ROUTE);
    assert.equal(resolveSafeNextPath(undefined), DEFAULT_POST_LOGIN_ROUTE);
    assert.equal(resolveSafeNextPath(""), DEFAULT_POST_LOGIN_ROUTE);
    assert.equal(resolveSafeNextPath("   "), DEFAULT_POST_LOGIN_ROUTE);
  });

  it("rejects javascript:, data:, and absolute URLs", () => {
    for (const evil of [
      "javascript:alert(1)",
      "JaVaScRiPt:alert(1)",
      "data:text/html,<script>alert(1)</script>",
      "https://evil.example/phish",
      "http://evil.example",
      "//evil.example/phish",
    ]) {
      assert.equal(resolveSafeNextPath(evil), DEFAULT_POST_LOGIN_ROUTE, evil);
    }
  });

  it("rejects protocol-relative and backslash evasions", () => {
    for (const evil of ["//evil", "/\\evil", "/\\/evil", "\\\\evil"]) {
      assert.equal(resolveSafeNextPath(evil), DEFAULT_POST_LOGIN_ROUTE, evil);
    }
  });

  it("rejects percent-encoded and double-encoded evasions", () => {
    for (const evil of [
      "%2F%2Fevil.example",
      "%2f%2fevil.example",
      "/%2Fevil.example",
      "%252F%252Fevil.example",
      "%6aavascript:alert(1)",
      "java%09script:alert(1)",
    ]) {
      assert.equal(resolveSafeNextPath(evil), DEFAULT_POST_LOGIN_ROUTE, evil);
    }
  });

  it("accepts valid internal routes including query and hash", () => {
    assert.equal(resolveSafeNextPath("/platform/knowledge-bases"), "/platform/knowledge-bases");
    assert.equal(resolveSafeNextPath("/"), "/");
    assert.equal(
      resolveSafeNextPath("/platform/kb/123?tab=docs#sec"),
      "/platform/kb/123?tab=docs#sec",
    );
  });
});
