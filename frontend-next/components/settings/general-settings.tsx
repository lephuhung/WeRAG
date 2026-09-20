/* Ported from frontend/src/views/settings/GeneralSettings.vue.
 * Subset for the Next app: language (en/vi only, per CLAUDE.md rule), theme
 * and font size live in localStorage exactly like the Vue composables
 * (useTheme/useFont) that the backend preference sync reads back.
 */
"use client";

import { useEffect, useState } from "react";
import { useT } from "@/lib/i18n";

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
      <div className="flex items-center justify-between gap-8 py-4">
        <div className="min-w-0">
          <label className="title-sm block">{t("settings.langTitle")}</label>
          <p className="caption mt-1 text-muted">{t("settings.langDesc")}</p>
        </div>
        <select
          className="input w-[280px] shrink-0"
          value={locale}
          onChange={(e) => {
            const next = e.target.value as "en" | "vi";
            if (next === "en" || next === "vi") setLocale(next);
          }}
        >
          <option value="en">{t("settings.langEn")}</option>
          <option value="vi">{t("settings.langVi")}</option>
        </select>
      </div>

      {/* theme */}
      <div className="flex items-center justify-between gap-8 py-4">
        <div className="min-w-0">
          <label className="title-sm block">{t("settings.themeTitle")}</label>
          <p className="caption mt-1 text-muted">{t("settings.themeDesc")}</p>
        </div>
        <select
          className="input w-[280px] shrink-0"
          value={theme}
          onChange={(e) => setTheme(e.target.value as ThemeMode)}
        >
          <option value="light">{t("settings.themeLight")}</option>
          <option value="dark">{t("settings.themeDark")}</option>
          <option value="system">{t("settings.themeSystem")}</option>
        </select>
      </div>

      {/* font size */}
      <div className="flex items-center justify-between gap-8 py-4">
        <div className="min-w-0">
          <label className="title-sm block">{t("settings.fontSizeTitle")}</label>
          <p className="caption mt-1 text-muted">{t("settings.fontSizeDesc")}</p>
        </div>
        <div className="flex shrink-0 items-center gap-1">
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
