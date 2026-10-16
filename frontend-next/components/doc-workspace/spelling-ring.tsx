"use client";

import { useEffect, useRef, useState } from "react";
import { useContainedPopup } from "@/components/hooks/use-contained-popup";
import { useT } from "@/lib/i18n";
import { mistakeContext, sameMistake, type GoToResult, type LiveMistake, type LiveSpellingStatus } from "@/lib/live-spelling";

const SIZE = 22;
const STROKE = 2;
const RADIUS = (SIZE - STROKE) / 2;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;

/* The live spellcheck of the open document, as a small progress ring in the
 * composer toolbar (an "ABC ✓" mark inside, the number of mistakes as a
 * badge; the same look as the Word add-in's ring): it fills while the new paragraphs are reviewed and shows a
 * tick (no mistake) or the number of underlined mistakes when done. A hover
 * explains it; once done a click lists the underlined mistakes (a click on
 * one selects it in the editor, which scrolls there; "not a mistake"
 * takes its underline away and records the verdict as training data) and
 * offers the NĐ30 format check (which runs only on request). */
export function SpellingRing({
  status,
  onGoTo,
  onDismiss,
  onFormatCheck,
  formatCheckLabel,
  placement = "below",
}: {
  status: LiveSpellingStatus;
  /** Where the popups open: "above" in the composer. */
  placement?: "above" | "below";
  /** The format check button's label ("check again" once there is one). */
  formatCheckLabel?: string;
  /** Selects the mistake in the editor. */
  onGoTo: (m: LiveMistake) => Promise<GoToResult>;
  /** The user says the mistake is none; false when that was not recorded. */
  onDismiss: (m: LiveMistake) => Promise<boolean>;
  /** Starts the NĐ30 format check in the background. */
  onFormatCheck: () => void;
}) {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  // the listed mistake being selected, and why the others could not be
  const [jumping, setJumping] = useState<number | null>(null);
  const [lost, setLost] = useState<ReadonlyMap<number, Exclude<GoToResult, "ok">>>(new Map());
  // the mistake whose "not a mistake" is being recorded, and one that failed
  const [dismissing, setDismissing] = useState<LiveMistake | null>(null);
  const [dismissFailed, setDismissFailed] = useState<LiveMistake | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const popupRef = useContainedPopup(rootRef, placement);
  const side = placement === "above" ? "bottom-full mb-1" : "top-full mt-1";
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

  const mistakes = phase === "done" ? status.mistakes : [];
  const toggle = () => {
    if (busy) return;
    setLost(new Map());
    setDismissFailed(null);
    setOpen((v) => !v);
  };
  const goTo = async (i: number) => {
    const m = mistakes[i];
    if (!m || jumping !== null) return;
    setJumping(i);
    const r = await onGoTo(m).catch((): GoToResult => "noAnswer");
    setJumping(null);
    setLost((prev) => {
      const next = new Map(prev);
      if (r === "ok") next.delete(i);
      else next.set(i, r);
      return next;
    });
  };

  const dismiss = async (m: LiveMistake) => {
    if (dismissing) return;
    setDismissing(m);
    setDismissFailed(null);
    const ok = await onDismiss(m).catch(() => false);
    setDismissing(null);
    // the list shrinks: positions of the jump results no longer hold
    if (ok) setLost(new Map());
    else setDismissFailed(m);
  };

  return (
    <div ref={rootRef} className="group relative shrink-0">
      <button
        type="button"
        aria-label={label}
        aria-haspopup={busy ? undefined : "menu"}
        aria-expanded={busy ? undefined : open}
        onClick={toggle}
        className={`relative flex h-7 w-7 items-center justify-center rounded-full transition-colors ${
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
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
            <text x="1" y="11" fill="currentColor" stroke="none" fontSize="9" fontWeight="700">ABC</text>
            <path d="m8 17 3 3 7-7" />
          </svg>
        </span>
        {((phase === "done" && found > 0) || phase === "failed") && (
          <span className="absolute -right-1 -top-0.5 rounded-full bg-error px-1 text-[8px] leading-3 text-white" aria-hidden="true">
            {phase === "failed" ? "!" : found > 99 ? "99+" : found}
          </span>
        )}
      </button>

      {!open && (
        <div
          ref={popupRef}
          role="tooltip"
          className={`pointer-events-none absolute right-0 ${side} z-50 w-max max-w-[260px] rounded-[8px] bg-ink px-2.5 py-1.5 text-[12px] leading-snug text-canvas opacity-0 shadow-[0_4px_16px_rgba(0,0,0,0.12)] transition-opacity duration-150 group-hover:opacity-100 group-focus-within:opacity-100`}
        >
          {label}
        </div>
      )}

      {open && (
        <div
          ref={popupRef}
          role="menu"
          className={`card absolute right-0 ${side} z-50 ${
            mistakes.length > 0 ? "w-[340px]" : "w-[260px]"
          } max-w-[calc(100vw-2.5rem)] p-3 shadow-[0_4px_16px_rgba(0,0,0,0.08)]`}
        >
          <p className="text-[13px] font-medium leading-snug text-ink">{label}</p>
          <p className="mt-1 text-[12px] leading-snug text-muted">
            {mistakes.length > 0 ? t("docws.spListHint") : t("docws.spHint")}
          </p>
          {mistakes.length > 0 && (
            <ul className="-mx-1 mt-2">
              {mistakes.map((m, i) => (
                <li key={`${i}-${m.paragraph.length}-${m.text}-${m.textOccurrence ?? 1}`} className="flex items-start gap-1">
                  <button
                    type="button"
                    role="menuitem"
                    disabled={jumping !== null || dismissing !== null}
                    onClick={() => void goTo(i)}
                    className="min-w-0 flex-1 rounded-[6px] px-1.5 py-1.5 text-left hover:bg-surface-strong disabled:cursor-wait"
                  >
                    <span className="block text-[13px] leading-snug text-ink">
                      <span className="font-semibold underline decoration-[var(--color-warning,#a15c00)] underline-offset-2">
                        {m.text}
                      </span>
                      {m.suggestion && (
                        <>
                          <span className="mx-1 text-muted-soft">→</span>
                          <span className="font-medium">{m.suggestion}</span>
                        </>
                      )}
                      {jumping === i && <span className="ml-2 text-[11.5px] text-muted-soft">…</span>}
                    </span>
                    <span className="mt-0.5 block text-[12px] leading-snug text-muted">{mistakeContext(m)}</span>
                    {lost.has(i) && (
                      <span className="mt-0.5 block text-[11.5px] leading-snug text-error">
                        {t(
                          lost.get(i) === "outdated"
                            ? "docws.spGoToOutdated"
                            : lost.get(i) === "noAnswer"
                              ? "docws.spGoToNoAnswer"
                              : "docws.spGoToFailed",
                        )}
                      </span>
                    )}
                    {dismissFailed && sameMistake(dismissFailed, m) && (
                      <span className="mt-0.5 block text-[11.5px] leading-snug text-error">{t("docws.spDismissFailed")}</span>
                    )}
                  </button>
                  <button
                    type="button"
                    disabled={jumping !== null || dismissing !== null}
                    onClick={() => void dismiss(m)}
                    title={t("docws.spDismissHint")}
                    className="mt-1 shrink-0 rounded-[6px] px-1.5 py-1 text-[11.5px] leading-snug text-muted hover:bg-surface-strong hover:text-ink disabled:cursor-wait"
                  >
                    {dismissing && sameMistake(dismissing, m) ? "…" : t("docws.spDismiss")}
                  </button>
                </li>
              ))}
            </ul>
          )}
          <div className="mt-2.5 flex flex-col gap-1.5">
            <button
              type="button"
              role="menuitem"
              onClick={() => {
                setOpen(false);
                onFormatCheck();
              }}
              className={`btn ${mistakes.length > 0 ? "btn-outline" : "btn-primary"} btn-sm w-full disabled:opacity-50`}
            >
              {formatCheckLabel ?? t("docws.spFormat")}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

/* Starts the NĐ30 format check in the background, next to the spelling
 * ring once the spelling is checked; the result comes as a notification. */
export function FormatCheckButton({ busy, onClick, iconOnly = false }: { busy: boolean; onClick: () => void; iconOnly?: boolean }) {
  const { t } = useT();
  return (
    <button
      type="button"
      disabled={busy}
      onClick={onClick}
      title={t("docws.fcStartHint")}
      aria-label={t("docws.fcStart")}
      data-tip={iconOnly ? t("docws.fcStartHint") : undefined}
      data-tip-side="top"
      data-tip-align="start"
      className={iconOnly ? "word-tip flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-muted hover:bg-surface-strong hover:text-ink disabled:opacity-50" : "btn btn-outline btn-sm shrink-0 whitespace-nowrap disabled:opacity-50"}
    >
      {iconOnly ? (
        <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M13 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V10l-7-7Z" />
          <path d="M13 3v7h7M8 15l2 2 5-5" />
        </svg>
      ) : t("docws.fcStart")}
    </button>
  );
}
