/* The platform theme read on the server, so the first HTML already carries
 * data-theme. Without it a first-time visitor (nothing cached in
 * localStorage) saw the built-in theme — other fonts and colours — until
 * the client bundle loaded and asked /api/v1/system/ui-theme, then the
 * page re-styled and shifted (14 s in on a slow phone). Server only. */

import { normalizeUiTheme, UI_THEME_ENDPOINT, DEFAULT_UI_THEME, type UiTheme } from "@/lib/ui-theme";

/* The Go backend as the server sees it: the dev compose sets
 * WERAG_BACKEND_URL; the production image's entrypoint gets APP_HOST /
 * APP_PORT / APP_SCHEME for nginx. */
function backendBase(): string {
  const explicit = process.env.WERAG_BACKEND_URL?.trim();
  if (explicit) return explicit.replace(/\/+$/, "");
  const host = process.env.APP_HOST?.trim();
  if (host) return `${process.env.APP_SCHEME?.trim() || "http"}://${host}:${process.env.APP_PORT?.trim() || "8080"}`;
  return "http://127.0.0.1:8080";
}

/** The SuperAdmin-selected theme; the built-in one when the backend does
 * not answer within a second (the client still reconciles after load).
 * Cached for a minute, so pages stay statically rendered. */
export async function serverUiTheme(): Promise<UiTheme> {
  try {
    const res = await fetch(`${backendBase()}${UI_THEME_ENDPOINT}`, {
      next: { revalidate: 60 },
      signal: AbortSignal.timeout(1000),
    });
    if (!res.ok) return DEFAULT_UI_THEME;
    const body = (await res.json()) as { data?: { theme?: unknown } } | null;
    return normalizeUiTheme(body?.data?.theme);
  } catch {
    return DEFAULT_UI_THEME;
  }
}
