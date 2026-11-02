interface SettingsRedirectStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

const SETTINGS_REDIRECT_KEY = "weknora:settings-redirect";

export function queueSettingsRedirect(storage: SettingsRedirectStorage, section: string) {
  storage.setItem(SETTINGS_REDIRECT_KEY, section);
}

export function consumeSettingsRedirect(storage: SettingsRedirectStorage): string | null {
  const section = storage.getItem(SETTINGS_REDIRECT_KEY);
  storage.removeItem(SETTINGS_REDIRECT_KEY);
  return section;
}
