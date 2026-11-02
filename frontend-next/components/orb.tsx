type OrbColor = "mint" | "peach" | "lavender" | "sky" | "rose";

/* The orb hues live in the theme (--color-orb-*) so a preset can restyle
 * them; the hex used to be duplicated here, which made the Anthropic
 * preset keep the default theme's pastels. */
const ORB_VAR: Record<OrbColor, string> = {
  mint: "var(--color-orb-mint)",
  peach: "var(--color-orb-peach)",
  lavender: "var(--color-orb-lavender)",
  sky: "var(--color-orb-sky)",
  rose: "var(--color-orb-rose)",
};

/**
 * Atmospheric pastel orb — pure decoration per the ElevenLabs spec.
 * Never a card surface, never behind buttons as a fill.
 */
export function Orb({
  color,
  size = 480,
  className = "",
  style,
}: {
  color: OrbColor;
  size?: number;
  className?: string;
  style?: React.CSSProperties;
}) {
  return (
    <div
      aria-hidden
      className={`orb ${className}`}
      style={{
        width: size,
        height: size,
        background: `radial-gradient(circle, ${ORB_VAR[color]} 0%, transparent 70%)`,
        opacity: 0.7,
        ...style,
      }}
    />
  );
}
