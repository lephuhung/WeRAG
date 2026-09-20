/* Shared option chip used by agent IM/embed panels and MCP settings.
 * Matches the .option-chip pattern of the Vue settings drawer. */
"use client";

export function Chip({ active, disabled, onClick, children }: {
  active: boolean;
  disabled?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={`h-8 rounded-full border px-3.5 text-[13px] font-medium transition-colors ${
        active
          ? "border-primary bg-primary text-on-primary"
          : "border-hairline-strong bg-surface-card text-body hover:border-ink hover:text-ink"
      } ${disabled ? "cursor-not-allowed opacity-40" : ""}`}
    >
      {children}
    </button>
  );
}
