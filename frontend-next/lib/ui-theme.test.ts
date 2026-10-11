// Runs with: node --experimental-strip-types --test lib/ui-theme.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { DEFAULT_UI_THEME, normalizeUiTheme } from "./ui-theme.ts";

describe("normalizeUiTheme", () => {
  it("accepts anthropic", () => assert.equal(normalizeUiTheme("anthropic"), "anthropic"));
  it("accepts elevenlabs", () => assert.equal(normalizeUiTheme("elevenlabs"), "elevenlabs"));
  it("falls back for unknown / null / undefined / non-string", () => {
    assert.equal(normalizeUiTheme("notion"), DEFAULT_UI_THEME);
    assert.equal(normalizeUiTheme(null), DEFAULT_UI_THEME);
    assert.equal(normalizeUiTheme(undefined), DEFAULT_UI_THEME);
    assert.equal(normalizeUiTheme(7), DEFAULT_UI_THEME);
  });
});
