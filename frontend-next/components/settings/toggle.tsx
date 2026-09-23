"use client";

/* Shared switch (setting-row toggle) — the vue app used t-switch; this is its
 * Tailwind counterpart. Same geometry in every ported panel. */
export function Toggle({
  checked,
  onChange,
  label,
  title,
  disabled,
  size = "md",
  className = "",
}: {
  checked: boolean;
  onChange?: (v: boolean) => void;
  label?: string;
  title?: string;
  disabled?: boolean;
  size?: "sm" | "md";
  className?: string;
}) {
  const isSm = size === "sm";

  return (
    <button
      role="switch"
      type="button"
      disabled={disabled}
      aria-checked={checked}
      aria-label={label}
      title={title || label}
      onClick={() => !disabled && onChange?.(!checked)}
      className={`relative inline-flex shrink-0 cursor-pointer rounded-full transition-colors duration-200 ease-in-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-stone-900 ${
        isSm ? "h-5 w-9" : "h-6 w-11"
      } ${
        disabled ? "opacity-50 cursor-not-allowed " : ""
      } ${
        checked
          ? "bg-emerald-600 hover:bg-emerald-700 dark:bg-emerald-500 dark:hover:bg-emerald-600"
          : "bg-stone-300 hover:bg-stone-400 dark:bg-stone-700 dark:hover:bg-stone-600"
      } ${className}`}
    >
      <span
        className={`pointer-events-none absolute top-0.5 rounded-full bg-white shadow-[0_1px_3px_rgba(0,0,0,0.25)] transition-transform duration-200 ease-in-out ${
          isSm ? "h-4 w-4" : "h-5 w-5"
        } ${
          checked
            ? isSm
              ? "translate-x-[18px]"
              : "translate-x-[22px]"
            : "translate-x-0.5"
        }`}
      />
    </button>
  );
}

