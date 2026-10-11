/* Platform-wide UI theme (SuperAdmin-selected, global). Shared by the web app
 * and the Word add-in via scripts/shared-files.json.
 *
 * The value lives in the system setting `ui.theme`, read from the public
 * endpoint GET /api/v1/system/ui-theme. `data-theme` on <html> selects the
 * Anthropic CSS preset; absence means the built-in ElevenLabs theme.
 * Independent from the per-user light/dark `html.dark` class.
 *
 * No imports: the module must stay loadable by `node --test` (see the test). */

export type UiTheme = "elevenlabs" | "anthropic";

export const UI_THEME_ATTR = "data-theme";
export const UI_THEME_STORAGE_KEY = "weknora_ui_theme";
export const UI_THEME_CHANGED_EVENT = "weknora:ui-theme-changed";
export const UI_THEME_ENDPOINT = "/api/v1/system/ui-theme";
export const DEFAULT_UI_THEME: UiTheme = "elevenlabs";

/** Coerce an arbitrary stored/API value to a known theme. */
export function normalizeUiTheme(value: unknown): UiTheme {
  return value === "anthropic" ? "anthropic" : DEFAULT_UI_THEME;
}

/** Apply a theme to <html> and cache it for the next pre-paint boot. */
export function applyUiTheme(theme: UiTheme): void {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  if (theme === DEFAULT_UI_THEME) {
    root.removeAttribute(UI_THEME_ATTR);
  } else {
    root.setAttribute(UI_THEME_ATTR, theme);
  }
  try {
    localStorage.setItem(UI_THEME_STORAGE_KEY, theme);
  } catch {
    /* private mode / storage disabled */
  }
}

/** Read the cached theme (used by the pre-paint inline script). */
export function readCachedUiTheme(): UiTheme {
  if (typeof localStorage === "undefined") return DEFAULT_UI_THEME;
  try {
    return normalizeUiTheme(localStorage.getItem(UI_THEME_STORAGE_KEY));
  } catch {
    return DEFAULT_UI_THEME;
  }
}

/** Fetch the authoritative global theme. Never throws. */
export async function fetchUiTheme(timeoutMs = 0): Promise<UiTheme> {
  try {
    const controller = timeoutMs > 0 ? new AbortController() : undefined;
    const timer = controller ? setTimeout(() => controller.abort(), timeoutMs) : undefined;
    const res = await fetch(UI_THEME_ENDPOINT, { cache: "no-store", signal: controller?.signal });
    if (timer) clearTimeout(timer);
    if (!res.ok) return DEFAULT_UI_THEME;
    const body = (await res.json()) as { data?: { theme?: unknown } } | null;
    return normalizeUiTheme(body?.data?.theme);
  } catch {
    return DEFAULT_UI_THEME;
  }
}

/** Notify listeners that the theme changed (admin selector). */
export function dispatchUiThemeChanged(theme: UiTheme): void {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent(UI_THEME_CHANGED_EVENT, { detail: theme }));
}

/** Subscribe to in-page theme changes. Returns an unsubscribe fn. */
export function subscribeUiTheme(listener: (theme: UiTheme) => void): () => void {
  if (typeof window === "undefined") return () => {};
  const handler = (e: Event) => listener(normalizeUiTheme((e as CustomEvent).detail));
  window.addEventListener(UI_THEME_CHANGED_EVENT, handler);
  return () => window.removeEventListener(UI_THEME_CHANGED_EVENT, handler);
}

/** Boot: apply the cached theme now, then reconcile with the server value. */
export async function bootUiTheme(): Promise<UiTheme> {
  applyUiTheme(readCachedUiTheme());
  const theme = await fetchUiTheme();
  applyUiTheme(theme);
  return theme;
}
