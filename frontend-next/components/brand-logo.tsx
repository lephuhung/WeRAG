"use client";

import { useT } from "@/lib/i18n";

interface BrandLogoProps {
  size?: number;
  className?: string;
  priority?: boolean;
  /** Accessible name of the mark; empty when the wordmark beside it names it. */
  alt?: string;
}

/**
 * Shared open-book mark for weRAG — Trợ lý pháp luật.
 *
 * Drawn inline (not as an <img>) so the mark takes the theme's brand
 * colour: a fixed teal file stayed teal on every surface and clashed with
 * the warm palette and with the dark theme. The three tones are mixed
 * from that one colour, so the mark keeps its shape in every theme.
 */
export function BrandLogo({ size = 36, className = "", priority = false, alt }: BrandLogoProps) {
  const { t } = useT();
  const label = alt ?? t("brand.alt");
  return (
    <div
      className={`relative inline-flex shrink-0 items-center justify-center ${className}`}
      style={{ width: size, height: size, color: "var(--color-brand)" }}
    >
      <svg
        viewBox="0 0 128 128"
        fill="none"
        role={label ? "img" : undefined}
        aria-label={label || undefined}
        aria-hidden={label ? undefined : true}
        focusable="false"
        className="h-full w-full"
      >
        <path fill="currentColor" d="M12 10C7 8 4 11 4 16v64c0 7 3 11 9 14l45 24V44c0-11-5-19-15-24L12 10Z" />
        <path
          fill="color-mix(in srgb, currentColor 40%, white)"
          d="M116 10c5-2 8 1 8 6v64c0 7-3 11-9 14l-45 24V44c0-11 5-19 15-24l31-10Z"
        />
        <path
          fill="color-mix(in srgb, currentColor 78%, black)"
          d="M4 53v27c0 7 3 11 9 14l45 24V99c0-11-5-18-15-23L16 63C9 60 6 57 4 53Z"
        />
        <path fill="currentColor" d="M124 53v27c0 7-3 11-9 14l-45 24V99c0-11 5-18 15-23l27-13c7-3 10-6 12-10Z" />
      </svg>
    </div>
  );
}

const LOCKUP_SIZES = {
  sm: { logo: 28, word: "text-[20px]", tagline: "text-[10px]", gap: "gap-2" },
  md: { logo: 34, word: "text-[23px]", tagline: "text-[11px]", gap: "gap-2.5" },
  lg: { logo: 40, word: "text-[25px]", tagline: "text-[11px]", gap: "gap-3" },
} as const;

export type BrandLockupSize = keyof typeof LOCKUP_SIZES;

interface BrandLockupProps {
  size?: BrandLockupSize;
  className?: string;
  priority?: boolean;
}

/**
 * The weRAG lockup: the open-book mark, the wordmark and the tagline.
 * One component for every surface (sidebar, landing, auth shell, Word
 * sign-in) so the brand never drifts between screens.
 */
export function BrandLockup({ size = "md", className = "", priority = false }: BrandLockupProps) {
  const { t } = useT();
  const spec = LOCKUP_SIZES[size];
  return (
    <span className={`flex items-center ${spec.gap} ${className}`}>
      <BrandLogo size={spec.logo} priority={priority} alt="" />
      <span className="flex flex-col">
        <span className={`font-semibold leading-none tracking-tight text-ink ${spec.word}`}>weRAG</span>
        <span className={`mt-1 leading-tight text-muted ${spec.tagline}`}>{t("auth.tagline")}</span>
      </span>
    </span>
  );
}

export default BrandLogo;
