"use client";

/* Pill-shaped segmented control for 2-state settings — both options are
 * labelled states (e.g. auto/force), so a switch would hide one meaning. */
export function SegmentedPills<T extends string>({
  value,
  onChange,
  options,
  disabled,
  className = "",
}: {
  value: T;
  onChange: (v: T) => void;
  options: { value: T; label: React.ReactNode }[];
  disabled?: boolean;
  className?: string;
}) {
  return (
    <div
      role="radiogroup"
      className={`inline-flex items-center rounded-full border border-hairline bg-surface-strong p-0.5 ${
        disabled ? "opacity-50" : ""
      } ${className}`}
    >
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={o.value === value}
          disabled={disabled}
          onClick={() => onChange(o.value)}
          className={`whitespace-nowrap rounded-full px-3 py-1 text-[12px] font-medium transition-colors disabled:cursor-not-allowed ${
            o.value === value
              ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.10)]"
              : "text-muted hover:text-ink"
          }`}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}
