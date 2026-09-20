import type { Metadata } from "next";
import { Inter, Newsreader } from "next/font/google";
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

export const metadata: Metadata = {
  title: "WeRAG",
  description: "WeRAG — knowledge base & RAG platform",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" className={`${inter.variable} ${displaySerif.variable}`}>
      <body>{children}</body>
    </html>
  );
}
