/* The progress ring — one implementation for every place that draws a
 * circular progress indicator (the live spellcheck, the NĐ30 format
 * check and the upload transfer).
 *
 * The ring starts at the top and fills clockwise; the caller supplies
 * the value (0..1), the colours and the size. */

export function Ring({
  value,
  size = 22,
  stroke = 2,
  tone = "var(--color-primary)",
  track = "var(--color-hairline-strong)",
  transitionMs = 250,
  className,
}: {
  /** 0..1 */
  value: number;
  size?: number;
  stroke?: number;
  /** The colour of the filled arc. */
  tone?: string;
  /** The colour of the unfilled track. */
  track?: string;
  transitionMs?: number;
  className?: string;
}) {
  const radius = (size - stroke) / 2;
  const circumference = 2 * Math.PI * radius;
  const clamped = Math.min(1, Math.max(0, value));
  const offset = circumference * (1 - clamped);
  return (
    <svg
      className={className}
      width={size}
      height={size}
      viewBox={`0 0 ${size} ${size}`}
      style={{ transform: "rotate(-90deg)", display: "block" }}
      aria-hidden="true"
    >
      <circle cx={size / 2} cy={size / 2} r={radius} fill="none" strokeWidth={stroke} style={{ stroke: track }} />
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        strokeWidth={stroke}
        strokeLinecap="round"
        strokeDasharray={circumference}
        strokeDashoffset={offset}
        style={{ stroke: tone, transition: `stroke-dashoffset ${transitionMs}ms ease` }}
      />
    </svg>
  );
}
