interface SettingsRedirectStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

const SETTINGS_REDIRECT_KEY = "weknora:settings-redirect";
const SETTINGS_PARAMS_KEY = "weknora:settings-redirect-params";
const DEEP_LINK_ARRIVAL_KEY = "weknora:settings-deep-link-at";
const DEEP_LINK_QUIET_MS = 60_000;

/* Window event dispatched once a redirect's section was opened (detail: the
 * section key): a section already on screen reads its deep-link query then,
 * as its mount-time read does not run again. */
export const SETTINGS_REDIRECTED_EVENT = "weknora:settings-redirected";

/* section: the settings section to open; search: the deep link's query
 * ("?status=pending"), which the section reads once (consumeSettingsParam). */
export function queueSettingsRedirect(storage: SettingsRedirectStorage, section: string, search = "") {
  storage.setItem(SETTINGS_REDIRECT_KEY, section);
  if (search && search !== "?") storage.setItem(SETTINGS_PARAMS_KEY, JSON.stringify({ section, search, at: Date.now() }));
  else storage.removeItem(SETTINGS_PARAMS_KEY);
}

/* consumeSettingsParam: the query parameter `name` of the deep link that
 * opened `section`, once ("" when none). Another section's parameters are
 * left alone. */
export function consumeSettingsParam(storage: SettingsRedirectStorage, section: string, name: string): string {
  const raw = storage.getItem(SETTINGS_PARAMS_KEY);
  if (!raw) return "";
  let parsed: { section?: unknown; search?: unknown };
  try {
    parsed = JSON.parse(raw);
  } catch {
    storage.removeItem(SETTINGS_PARAMS_KEY);
    return "";
  }
  if (parsed.section !== section) return "";
  storage.removeItem(SETTINGS_PARAMS_KEY);
  return typeof parsed.search === "string" ? (new URLSearchParams(parsed.search).get(name) ?? "") : "";
}

/* consumeSettingsSearch: the whole query of the deep link that opened
 * `section`, once (null when none) — for a section that reads several
 * parameters together. */
export function consumeSettingsSearch(storage: SettingsRedirectStorage, section: string, now = Date.now()): URLSearchParams | null {
  const raw = storage.getItem(SETTINGS_PARAMS_KEY);
  if (!raw) return null;
  let parsed: { section?: unknown; search?: unknown; at?: unknown };
  try {
    parsed = JSON.parse(raw);
  } catch {
    storage.removeItem(SETTINGS_PARAMS_KEY);
    return null;
  }
  if (parsed.section !== section) return null;
  storage.removeItem(SETTINGS_PARAMS_KEY);
  // a link whose section was closed before it read it (e.g. the modal shut
  // at once) must not preselect an old plan the next time the shop opens
  if (typeof parsed.at === "number" && now - parsed.at > DEEP_LINK_QUIET_MS) return null;
  return typeof parsed.search === "string" ? new URLSearchParams(parsed.search) : null;
}

export function consumeSettingsRedirect(storage: SettingsRedirectStorage): string | null {
  const section = storage.getItem(SETTINGS_REDIRECT_KEY);
  storage.removeItem(SETTINGS_REDIRECT_KEY);
  return section;
}

/* A visit that arrived through a settings deep link (a plan picked on the
 * pricing table, a notification) came to do that one thing: one-off
 * prompts (the work-context question) keep quiet for a minute instead of
 * covering the section it opened. A timestamp, not a one-shot flag, so
 * every component mounting in that window reads the same answer. */
export function markDeepLinkArrival(storage: SettingsRedirectStorage, now = Date.now()) {
  storage.setItem(DEEP_LINK_ARRIVAL_KEY, String(now));
}

export function arrivedByDeepLink(storage: SettingsRedirectStorage, now = Date.now()): boolean {
  const at = Number(storage.getItem(DEEP_LINK_ARRIVAL_KEY));
  return Number.isFinite(at) && at > 0 && now - at >= 0 && now - at <= DEEP_LINK_QUIET_MS;
}
