import type { Metadata, Viewport } from "next";
import { Inter, Newsreader, IBM_Plex_Sans, IBM_Plex_Mono } from "next/font/google";
import "./globals.css";
import { UI_PREFERENCES_BOOT_SCRIPT } from "@/lib/ui-preferences";
import { UI_THEME_ATTR } from "@/lib/ui-theme";
import { serverUiTheme } from "@/lib/server-ui-theme";

import { GlobalUiTheme } from "@/components/global-ui-theme";
import { I18nProvider } from "@/lib/i18n";

const inter = Inter({
  subsets: ["latin", "vietnamese"],
  weight: ["400", "500", "600"],
  variable: "--font-inter",
  display: "swap",
  /* Preload only the face the platform theme (Anthropic) shows first:
   * preloading all four families sent ~10 font files up front, 8 of them
   * unused (Chrome's "preloaded but not used"). Inter and Newsreader serve
   * the built-in theme, Plex Mono only code. */
  preload: false,
});

/* Waldenburg Light is licensed; spec suggests EB Garamond 300, but Google
   Fonts ships EB Garamond from 400 up — Newsreader 300 is the closest
   light-weight editorial serif available there. */
const displaySerif = Newsreader({
  subsets: ["latin", "vietnamese"],
  weight: ["300", "400"],
  variable: "--font-display-serif",
  display: "swap",
  preload: false,
});

/* Anthropic theme typeface. Anthropic Sans/Mono are proprietary; IBM Plex is
   the substitute with Vietnamese coverage, consumed via --ui-font-sans/mono in
   styles/theme-anthropic.css. */
const plexSans = IBM_Plex_Sans({
  subsets: ["latin", "vietnamese"],
  weight: ["400", "500", "600", "700"],
  variable: "--ui-font-sans",
  display: "swap",
});

const plexMono = IBM_Plex_Mono({
  subsets: ["latin", "vietnamese"],
  weight: ["400", "500"],
  variable: "--ui-font-mono",
  display: "swap",
  preload: false,
});

export const metadata: Metadata = {
  title: "weRAG — Trợ lý pháp luật",
  description: "weRAG — Trợ lý pháp luật: tra cứu, hỏi đáp văn bản pháp luật và đối chiếu nguồn trích dẫn.",
  icons: {
    /* Both files are in public/ (copied from the app icon files) — the
     * metadata points at assets that really exist, so the favicon never
     * 404s on any page. */
    icon: "/icon.svg",
    apple: "/icon.png",
  },
};

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
};

/* Runs before paint so a returning user never sees the default theme flash.
   Keep in sync with UI_THEME_STORAGE_KEY / UI_THEME_ATTR in lib/ui-theme.ts. */
const UI_THEME_BOOT_SCRIPT =
  "try{var t=localStorage.getItem('weknora_ui_theme');" +
  "if(t==='anthropic'){document.documentElement.setAttribute('data-theme','anthropic')}}catch(e){}";

export default async function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  // the platform theme in the first HTML: no re-style once the bundle loads
  const theme = await serverUiTheme();
  return (
    /* the boot script sets data-theme before hydration */
    <html
      lang="vi"
      suppressHydrationWarning
      {...(theme === "anthropic" ? { [UI_THEME_ATTR]: theme } : {})}
      className={`${inter.variable} ${displaySerif.variable} ${plexSans.variable} ${plexMono.variable}`}
    >
      <body>
        <script dangerouslySetInnerHTML={{ __html: UI_THEME_BOOT_SCRIPT + UI_PREFERENCES_BOOT_SCRIPT }} />
        <GlobalUiTheme />
        {/* One provider for the whole app: every route (landing, auth,
            onboarding) reads its strings from the same dictionary. */}
        <I18nProvider>{children}</I18nProvider>
      </body>
    </html>
  );
}
