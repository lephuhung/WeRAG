import { test } from "node:test";
import assert from "node:assert/strict";
import { normalizeThemeMode, normalizeFontSize, resolveDarkMode, fontScale, UI_PREFERENCES_BOOT_SCRIPT } from "./ui-preferences.ts";

test("invalid persisted preferences use system and normal", () => {
  for (const value of [null, undefined, "", "invalid", 1, {}]) {
    assert.equal(normalizeThemeMode(value), "system");
    assert.equal(normalizeFontSize(value), "normal");
  }
});
test("explicit light/dark override OS and system follows it", () => {
  for (const system of [false, true]) {
    assert.equal(resolveDarkMode("light", system), false);
    assert.equal(resolveDarkMode("dark", system), true);
    assert.equal(resolveDarkMode("system", system), system);
  }
});
test("font preferences use the promised scales", () => {
  assert.equal(fontScale("small"), .9);
  assert.equal(fontScale("normal"), 1);
  assert.equal(fontScale("large"), 1.12);
});
test("pre-paint boot restores preferences and survives blocked storage", () => {
  for (const blocked of [false, true]) {
    const classes = new Set<string>();
    const styles = new Map<string, string>();
    const document = { documentElement: { classList: { toggle: (k: string, v: boolean) => v ? classes.add(k) : classes.delete(k) }, style: { setProperty: (k: string, v: string) => styles.set(k, v) } } };
    const localStorage = { getItem: (key: string) => { if (blocked) throw Error("blocked"); return key === "weknora_theme" ? "dark" : "large"; } };
    new Function("document", "localStorage", "matchMedia", UI_PREFERENCES_BOOT_SCRIPT)(document, localStorage, () => ({ matches: false }));
    assert.equal(classes.has("dark"), !blocked);
    assert.equal(styles.get("--app-font-scale"), blocked ? "1" : "1.12");
  }
});
