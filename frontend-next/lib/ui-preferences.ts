export type ThemeMode = "light" | "dark" | "system";
export type FontSizeKey = "small" | "normal" | "large";

export const THEME_KEY = "weknora_theme";
export const FONT_SIZE_KEY = "weknora_font_size";
export const PREFERENCES_EVENT = "weknora:ui-preferences-changed";

export function normalizeThemeMode(value: unknown): ThemeMode {
  return value === "light" || value === "dark" ? value : "system";
}
export function normalizeFontSize(value: unknown): FontSizeKey {
  return value === "small" || value === "large" ? value : "normal";
}
export function resolveDarkMode(mode: ThemeMode, systemDark: boolean): boolean {
  return mode === "dark" || (mode === "system" && systemDark);
}
export function fontScale(size: FontSizeKey): number {
  return size === "small" ? .9 : size === "large" ? 1.12 : 1;
}
export function readPreferences(): { theme: ThemeMode; fontSize: FontSizeKey } {
  try {
    return { theme: normalizeThemeMode(localStorage.getItem(THEME_KEY)), fontSize: normalizeFontSize(localStorage.getItem(FONT_SIZE_KEY)) };
  } catch {
    return { theme: "system", fontSize: "normal" };
  }
}
export function applyPreferences(theme: ThemeMode, fontSize: FontSizeKey): void {
  document.documentElement.classList.toggle("dark", resolveDarkMode(theme, matchMedia("(prefers-color-scheme: dark)").matches));
  document.documentElement.style.setProperty("--app-font-scale", String(fontScale(fontSize)));
}
export function savePreferences(theme: ThemeMode, fontSize: FontSizeKey): void {
  try {
    localStorage.setItem(THEME_KEY, theme);
    localStorage.setItem(FONT_SIZE_KEY, fontSize);
  } catch { /* Preferences still apply for this page when storage is blocked. */ }
  applyPreferences(theme, fontSize);
  window.dispatchEvent(new CustomEvent(PREFERENCES_EVENT, { detail: { theme, fontSize } }));
}

// One pre-paint source, exercised by the same tests as the runtime helpers.
export const UI_PREFERENCES_BOOT_SCRIPT = `(()=>{const normalizeThemeMode=(${normalizeThemeMode.toString()}),normalizeFontSize=(${normalizeFontSize.toString()}),resolveDarkMode=(${resolveDarkMode.toString()}),fontScale=(${fontScale.toString()});let theme="system",size="normal";try{theme=normalizeThemeMode(localStorage.getItem("${THEME_KEY}"));size=normalizeFontSize(localStorage.getItem("${FONT_SIZE_KEY}"))}catch{}document.documentElement.classList.toggle("dark",resolveDarkMode(theme,matchMedia("(prefers-color-scheme: dark)").matches));document.documentElement.style.setProperty("--app-font-scale",String(fontScale(size)))})()`;
