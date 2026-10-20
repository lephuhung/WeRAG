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

import { readPreferences, savePreferences, type ThemeMode, type FontSizeKey } from "@/lib/ui-preferences";
export type { ThemeMode, FontSizeKey } from "@/lib/ui-preferences";

import { ConsolePage } from "@/components/settings/console-page";
import { FieldLabel } from "@/components/settings/field";
import { Pill, PillGroup } from "@/components/settings/pill";
import { Notice } from "@/components/settings/notice";

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
    const saved = readPreferences();
    setThemeState(saved.theme);
    setFontSizeState(saved.fontSize);
  }, []);

  const setTheme = (mode: ThemeMode) => {
    setThemeState(mode);
    savePreferences(mode, fontSize);
  };

  const fontSizeOptions: { value: FontSizeKey; labelKey: "fontSmall" | "fontNormal" | "fontLarge"; scale: string }[] = [
    { value: "small", labelKey: "fontSmall", scale: "90%" },
    { value: "normal", labelKey: "fontNormal", scale: "100%" },
    { value: "large", labelKey: "fontLarge", scale: "112%" },
  ];

  const setFontSize = (size: FontSizeKey, _scale: string) => {
    setFontSizeState(size);
    savePreferences(theme, size);
  };

  return (
    <ConsolePage size="form">
      <div className="flex flex-col divide-y divide-hairline">
        {/* language */}
        <div className="flex flex-col items-start gap-3 py-4 sm:flex-row sm:items-center sm:justify-between sm:gap-8">
          <div className="min-w-0">
            <FieldLabel htmlFor="settings-language" className="text-[15px] font-medium text-ink">
              {t("settings.langTitle")}
            </FieldLabel>
            <p className="caption mt-1 text-muted">{t("settings.langDesc")}</p>
          </div>
          <Select
            id="settings-language"
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
            <FieldLabel htmlFor="settings-theme" className="text-[15px] font-medium text-ink">
              {t("settings.themeTitle")}
            </FieldLabel>
            <p className="caption mt-1 text-muted">{t("settings.themeDesc")}</p>
          </div>
          <Select
            id="settings-theme"
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
              <FieldLabel htmlFor="settings-system-theme" className="text-[15px] font-medium text-ink">
                {t("settings.systemThemeTitle")}
              </FieldLabel>
              <p className="caption mt-1 text-muted">{t("settings.systemThemeDesc")}</p>
              {systemThemeError && (
                <Notice tone="error" className="mt-1">{t("settings.systemThemeError")}</Notice>
              )}
            </div>
            <Select
              id="settings-system-theme"
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
            <div className="text-[15px] font-medium text-ink">{t("settings.fontSizeTitle")}</div>
            <p className="caption mt-1 text-muted">{t("settings.fontSizeDesc")}</p>
          </div>
          <div className="flex items-center gap-1">
            <PillGroup ariaLabel={t("settings.fontSizeTitle")}>
              {fontSizeOptions.map((opt) => (
                <Pill
                  key={opt.value}
                  active={fontSize === opt.value}
                  onClick={() => setFontSize(opt.value, opt.scale)}
                  title={`${t(opt.labelKey)} (${opt.scale})`}
                >
                  {t(opt.labelKey)}
                </Pill>
              ))}
            </PillGroup>
          </div>
        </div>
      </div>
    </ConsolePage>
  );
}
