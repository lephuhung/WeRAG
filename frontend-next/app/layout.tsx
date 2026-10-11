import type { Metadata, Viewport } from "next";
import { Inter, Newsreader, IBM_Plex_Sans, IBM_Plex_Mono } from "next/font/google";
import "./globals.css";

const inter = Inter({
  subsets: ["latin", "vietnamese"],
  weight: ["400", "500", "600"],
  variable: "--font-inter",
  display: "swap",
});

/* Waldenburg Light is licensed; spec suggests EB Garamond 300, but Google
   Fonts ships EB Garamond from 400 up — Newsreader 300 is the closest
   light-weight editorial serif available there. */
const displaySerif = Newsreader({
  subsets: ["latin", "vietnamese"],
  weight: ["300", "400"],
  variable: "--font-display-serif",
  display: "swap",
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
});

export const metadata: Metadata = {
  title: "WeRAG - Tra cứu tài liệu AI",
  description: "WeRAG — Tra cứu tài liệu AI & Hệ thống khai thác tri thức thông minh",
  icons: {
    icon: "/icon.png",
    apple: "/icon.png",
  },
};

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html
      lang="en"
      className={`${inter.variable} ${displaySerif.variable} ${plexSans.variable} ${plexMono.variable}`}
    >
      <body>{children}</body>
    </html>
  );
}
