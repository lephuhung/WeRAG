"use client";

import { useEffect, useRef, useState } from "react";
import { useT } from "@/lib/i18n";
import type { LiveSpellingStatus } from "@/lib/live-spelling";

const SIZE = 22;
const STROKE = 2;
const RADIUS = (SIZE - STROKE) / 2;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;

/* The live spellcheck of the open document, as a small progress ring in the
 * chat header: it fills while the new paragraphs are reviewed and shows a
 * tick (no mistake) or the number of underlined mistakes when done. A hover
 * explains it; once done a click offers the follow-ups — list the mistakes,
 * or check the NĐ30 format (which runs only on request). */
export function SpellingRing({
  status,
  disabled,
  onAsk,
  onFormatCheck,
}: {
  status: LiveSpellingStatus;
  /** A chat turn is running: the actions wait. */
  disabled: boolean;
  onAsk: (question: string) => void;
  /** Starts the NĐ30 format check in the background. */
  onFormatCheck: () => void;
}) {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const { phase, total, checked, found } = status;
  const busy = phase === "idle" || phase === "checking";

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    const onDown = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false);
    };
    window.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onDown);
    return () => {
      window.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onDown);
    };
  }, [open]);

  if (phase === "off") return null;

  const progress = phase === "checking" ? (total > 0 ? Math.max(0.05, checked / total) : 0.05) : phase === "idle" ? 0 : 1;
  const label =
    phase === "idle"
      ? t("docws.spWaiting")
      : phase === "checking"
        ? t("docws.spChecking", { checked, total })
        : phase === "failed"
          ? t("docws.spFailed")
          : found > 0
            ? t("docws.spFound", { n: found })
            : t("docws.spClean");
  const tone =
    phase === "failed" ? "var(--color-error)" : phase === "done" && found > 0 ? "var(--color-warning, #a15c00)" : "var(--color-primary)";

  const ask = (question: string) => {
    setOpen(false);
    onAsk(question);
  };

  return (
    <div ref={rootRef} className="group relative ml-auto shrink-0">
      <button
        type="button"
        aria-label={label}
        aria-haspopup={busy ? undefined : "menu"}
        aria-expanded={busy ? undefined : open}
        onClick={() => !busy && setOpen((v) => !v)}
        className={`relative flex h-8 w-8 items-center justify-center rounded-full transition-colors ${
          busy ? "cursor-default" : "cursor-pointer hover:bg-surface-strong"
        }`}
      >
        <svg
          width={SIZE}
          height={SIZE}
          viewBox={`0 0 ${SIZE} ${SIZE}`}
          aria-hidden="true"
          className={`-rotate-90 ${phase === "idle" ? "animate-spin motion-reduce:animate-none" : ""}`}
        >
          <circle cx={SIZE / 2} cy={SIZE / 2} r={RADIUS} fill="none" strokeWidth={STROKE} style={{ stroke: "var(--color-hairline-strong)" }} />
          <circle
            cx={SIZE / 2}
            cy={SIZE / 2}
            r={RADIUS}
            fill="none"
            strokeWidth={STROKE}
            strokeLinecap="round"
            strokeDasharray={CIRCUMFERENCE}
            strokeDashoffset={CIRCUMFERENCE * (1 - (phase === "idle" ? 0.25 : progress))}
            className="transition-[stroke-dashoffset] duration-500 ease-out motion-reduce:transition-none"
            style={{ stroke: tone }}
          />
        </svg>
        <span className="absolute inset-0 flex items-center justify-center text-[9px] font-semibold text-ink" aria-hidden="true">
          {phase === "done" && found === 0 ? (
            <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
              <path d="M5 12.5l4.5 4.5L19 7.5" />
            </svg>
          ) : phase === "done" ? (
            found > 99 ? "99+" : found
          ) : phase === "failed" ? (
            <span className="text-error">!</span>
          ) : null}
        </span>
      </button>

      {!open && (
        <div
          role="tooltip"
          className="pointer-events-none absolute right-0 top-full z-50 mt-1 w-max max-w-[260px] rounded-[8px] bg-ink px-2.5 py-1.5 text-[12px] leading-snug text-canvas opacity-0 shadow-[0_4px_16px_rgba(0,0,0,0.12)] transition-opacity duration-150 group-hover:opacity-100 group-focus-within:opacity-100"
        >
          {label}
        </div>
      )}

      {open && (
        <div
          role="menu"
          className="card absolute right-0 top-full z-50 mt-1 w-[260px] max-w-[calc(100vw-2.5rem)] p-3 shadow-[0_4px_16px_rgba(0,0,0,0.08)]"
        >
          <p className="text-[13px] font-medium leading-snug text-ink">{label}</p>
          <p className="mt-1 text-[12px] leading-snug text-muted">{t("docws.spHint")}</p>
          <div className="mt-2.5 flex flex-col gap-1.5">
            {phase === "done" && found > 0 && (
              <button
                type="button"
                role="menuitem"
                disabled={disabled}
                onClick={() => ask(t("docws.spAskList"))}
                className="btn btn-primary btn-sm w-full disabled:opacity-50"
              >
                {t("docws.spList")}
              </button>
            )}
            <button
              type="button"
              role="menuitem"
              onClick={() => {
                setOpen(false);
                onFormatCheck();
              }}
              className={`btn ${phase === "done" && found > 0 ? "btn-outline" : "btn-primary"} btn-sm w-full disabled:opacity-50`}
            >
              {t("docws.spFormat")}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

/* Starts the NĐ30 format check in the background, next to the spelling
 * ring once the spelling is checked; the result comes as a notification. */
export function FormatCheckButton({ busy, onClick }: { busy: boolean; onClick: () => void }) {
  const { t } = useT();
  return (
    <button
      type="button"
      disabled={busy}
      onClick={onClick}
      title={t("docws.fcStartHint")}
      className="btn btn-outline btn-sm shrink-0 whitespace-nowrap disabled:opacity-50"
    >
      {t("docws.fcStart")}
    </button>
  );
}
