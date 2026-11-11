interface SettingsRedirectStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

const SETTINGS_REDIRECT_KEY = "weknora:settings-redirect";
const SETTINGS_PARAMS_KEY = "weknora:settings-redirect-params";

/* section: the settings section to open; search: the deep link's query
 * ("?status=pending"), which the section reads once (consumeSettingsParam). */
export function queueSettingsRedirect(storage: SettingsRedirectStorage, section: string, search = "") {
  storage.setItem(SETTINGS_REDIRECT_KEY, section);
  if (search && search !== "?") storage.setItem(SETTINGS_PARAMS_KEY, JSON.stringify({ section, search }));
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

export function consumeSettingsRedirect(storage: SettingsRedirectStorage): string | null {
  const section = storage.getItem(SETTINGS_REDIRECT_KEY);
  storage.removeItem(SETTINGS_REDIRECT_KEY);
  return section;
}
