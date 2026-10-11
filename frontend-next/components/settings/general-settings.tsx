/* Ported from frontend/src/views/settings/GeneralSettings.vue.
 * Subset for the Next app: language (en/vi only, per CLAUDE.md rule), theme
 * and font size live in localStorage exactly like the Vue composables
 * (useTheme/useFont) that the backend preference sync reads back.
 */
"use client";

import { useEffect, useState } from "react";
import { useT } from "@/lib/i18n";
import { Select } from "@/components/select";
import { useAuth } from "@/lib/auth";
import { getSystemSetting, updateSystemSetting, SYSTEM_UI_THEME_KEY } from "@/lib/api/system";
import {
  applyUiTheme,
  dispatchUiThemeChanged,
  normalizeUiTheme,
  type UiTheme,
} from "@/lib/ui-theme";

export type ThemeMode = "light" | "dark" | "system";
export type FontSizeKey = "small" | "normal" | "large";

const THEME_KEY = "weknora_theme";
const FONT_SIZE_KEY = "weknora_font_size";

function applyTheme(mode: ThemeMode) {
  const root = document.documentElement;
  const dark =
    mode === "dark" ||
    (mode === "system" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches);
  root.classList.toggle("dark", dark);
}

export function GeneralSettings() {
  const { t, locale, setLocale } = useT();
  const [theme, setThemeState] = useState<ThemeMode>("system");
  const [fontSize, setFontSizeState] = useState<FontSizeKey>("normal");

  // Global theme — SuperAdmin only. Loaded once and applied for everyone by
  // GlobalUiTheme; this control is the only writer.
  const auth = useAuth();
  const isSystemAdmin = auth.user?.is_system_admin === true;
  const [systemTheme, setSystemTheme] = useState<UiTheme>("elevenlabs");
  const [systemThemeError, setSystemThemeError] = useState(false);

  useEffect(() => {
    if (!isSystemAdmin) return;
    getSystemSetting(SYSTEM_UI_THEME_KEY)
      .then((row) => setSystemTheme(normalizeUiTheme(row.value)))
      .catch(() => {
        /* not persisted yet → keep the default */
      });
  }, [isSystemAdmin]);

  const changeSystemTheme = (value: string) => {
    const next = normalizeUiTheme(value);
    setSystemTheme(next);
    setSystemThemeError(false);
    applyUiTheme(next);
    dispatchUiThemeChanged(next);
    updateSystemSetting(SYSTEM_UI_THEME_KEY, next).catch(() => setSystemThemeError(true));
  };

  useEffect(() => {
    try {
      const savedTheme = localStorage.getItem(THEME_KEY) as ThemeMode | null;
      const savedSize = localStorage.getItem(FONT_SIZE_KEY) as FontSizeKey | null;
      if (savedTheme) setThemeState(savedTheme);
      if (savedSize) setFontSizeState(savedSize);
    } catch {
      /* ignore */
    }
  }, []);

  const setTheme = (mode: ThemeMode) => {
    setThemeState(mode);
    try {
      localStorage.setItem(THEME_KEY, mode);
    } catch {
      /* ignore */
    }
    applyTheme(mode);
  };

  const fontSizeOptions: { value: FontSizeKey; labelKey: "fontSmall" | "fontNormal" | "fontLarge"; scale: string }[] = [
    { value: "small", labelKey: "fontSmall", scale: "90%" },
    { value: "normal", labelKey: "fontNormal", scale: "100%" },
    { value: "large", labelKey: "fontLarge", scale: "112%" },
  ];

  const setFontSize = (size: FontSizeKey, scale: string) => {
    setFontSizeState(size);
    try {
      localStorage.setItem(FONT_SIZE_KEY, size);
    } catch {
      /* ignore */
    }
    document.documentElement.style.setProperty("--app-font-scale", scale);
  };

  return (
    <div className="flex flex-col divide-y divide-hairline">
      {/* language */}
      <div className="flex flex-col items-start gap-3 py-4 sm:flex-row sm:items-center sm:justify-between sm:gap-8">
        <div className="min-w-0">
          <label className="title-sm block">{t("settings.langTitle")}</label>
          <p className="caption mt-1 text-muted">{t("settings.langDesc")}</p>
        </div>
        <Select
          className="w-full sm:w-[280px] sm:shrink-0"
          value={locale}
          onChange={(v) => {
            if (v === "en" || v === "vi") setLocale(v);
          }}
          options={[
            { value: "en", label: t("settings.langEn") },
            { value: "vi", label: t("settings.langVi") },
          ]}
        />
      </div>

      {/* theme */}
      <div className="flex flex-col items-start gap-3 py-4 sm:flex-row sm:items-center sm:justify-between sm:gap-8">
        <div className="min-w-0">
          <label className="title-sm block">{t("settings.themeTitle")}</label>
          <p className="caption mt-1 text-muted">{t("settings.themeDesc")}</p>
        </div>
        <Select
          className="w-full sm:w-[280px] sm:shrink-0"
          value={theme}
          onChange={(v) => setTheme(v as ThemeMode)}
          options={[
            { value: "light", label: t("settings.themeLight") },
            { value: "dark", label: t("settings.themeDark") },
            { value: "system", label: t("settings.themeSystem") },
          ]}
        />
      </div>

      {/* system theme (SuperAdmin only, global) */}
      {isSystemAdmin && (
        <div className="flex flex-col items-start gap-3 py-4 sm:flex-row sm:items-center sm:justify-between sm:gap-8">
          <div className="min-w-0">
            <label className="title-sm block">{t("settings.systemThemeTitle")}</label>
            <p className="caption mt-1 text-muted">{t("settings.systemThemeDesc")}</p>
            {systemThemeError && (
              <p className="caption mt-1 text-error">{t("settings.systemThemeError")}</p>
            )}
          </div>
          <Select
            className="w-full sm:w-[280px] sm:shrink-0"
            value={systemTheme}
            onChange={changeSystemTheme}
            options={[
              { value: "elevenlabs", label: t("settings.systemThemeElevenlabs") },
              { value: "anthropic", label: t("settings.systemThemeAnthropic") },
            ]}
          />
        </div>
      )}

      {/* font size */}
      <div className="flex flex-col items-start gap-3 py-4 sm:flex-row sm:items-center sm:justify-between sm:gap-8">
        <div className="min-w-0">
          <label className="title-sm block">{t("settings.fontSizeTitle")}</label>
          <p className="caption mt-1 text-muted">{t("settings.fontSizeDesc")}</p>
        </div>
        <div className="flex items-center gap-1">
          {fontSizeOptions.map((opt) => (
            <button
              key={opt.value}
              onClick={() => setFontSize(opt.value, opt.scale)}
              className={`btn-sm px-4 py-2 font-medium transition-colors first:rounded-l-full last:rounded-r-full ${
                fontSize === opt.value
                  ? "bg-primary text-on-primary"
                  : "bg-surface-strong text-body hover:text-ink"
              }`}
            >
              {t(opt.labelKey)}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
