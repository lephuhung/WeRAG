type OrbColor = "mint" | "peach" | "lavender" | "sky" | "rose";

const ORB_HEX: Record<OrbColor, string> = {
  mint: "#a7e5d3",
  peach: "#f4c5a8",
  lavender: "#c8b8e0",
  sky: "#a8c8e8",
  rose: "#e8b8c4",
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
        background: `radial-gradient(circle, ${ORB_HEX[color]} 0%, transparent 70%)`,
        opacity: 0.7,
        ...style,
      }}
    />
  );
}
