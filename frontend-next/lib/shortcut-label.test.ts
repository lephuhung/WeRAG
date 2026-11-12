// Runs with: npm run test:shortcut-label (Node >= 22.6, type stripping)
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { paletteShortcutLabel } from "./shortcut-label.ts";

describe("palette shortcut label", () => {
  it("shows ⌘K on Apple systems", () => {
    assert.equal(paletteShortcutLabel("MacIntel"), "⌘K");
    assert.equal(paletteShortcutLabel("Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5)"), "⌘K");
    assert.equal(paletteShortcutLabel("iPad"), "⌘K");
  });
  it("shows Ctrl K everywhere else", () => {
    assert.equal(paletteShortcutLabel("Win32"), "Ctrl K");
    assert.equal(paletteShortcutLabel("Linux x86_64"), "Ctrl K");
    assert.equal(paletteShortcutLabel(""), "Ctrl K");
  });
});
