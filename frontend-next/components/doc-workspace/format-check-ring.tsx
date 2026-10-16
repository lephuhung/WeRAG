"use client";

import { useEffect, useRef, useState } from "react";
import { useContainedPopup } from "@/components/hooks/use-contained-popup";
import { useT } from "@/lib/i18n";
import { IconEye, IconRefresh } from "@/components/icons";
import { IconAction, IconUnderlineMark, IconWand } from "./icon-action";
import type { DocumentFormatCheck } from "@/lib/api/document-workspace";
import { PROFILE_EXPECTED_MS, formatCheckInProgress, formatCheckProgress } from "@/lib/api/document-workspace";

const SIZE = 22;
const STROKE = 2;
const RADIUS = (SIZE - STROKE) / 2;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;

/* Background work on the open document, as a small progress ring in the
 * composer toolbar. Reading (its profile is being made, before the format
 * check starts): the ring fills over the profile's expected time. Then the
 * NĐ30 format check: queued (other documents are being checked first) is
 * an empty ring, running fills over the expected run time. A hover
 * explains each. Finished: a click opens one row of icon actions (named on
 * hover) — read the evaluation (onView: a modal, not a chat turn), have the
 * assistant fix it (onAsk), underline the findings (onMark, nothing
 * changed), or check again. Same look as the Word add-in's ring
 * (werag-word-addin src/components/word/format-check-ring.tsx). */
export function FormatCheckRing({
  check,
  placement = "below",
  reading = null,
  unseen,
  disabled,
  onAsk,
  onView,
  onOpen,
  onRecheck,
  onMark,
  onFix,
}: {
  placement?: "above" | "below";
  /** null while only the profile runs (no format check yet). */
  check: DocumentFormatCheck | null;
  /** The document's profile is being made: start time of that run. */
  reading?: { startedAt?: string } | null;
  /** The finished result was not opened yet → a small dot. */
  unseen: boolean;
  /** A chat turn is running: the actions wait. */
  disabled: boolean;
  onAsk: (question: string) => void;
  /** Shows the finished evaluation. */
  onView: () => void;
  onOpen: () => void;
  /** Runs the check again in the background (instead of asking the chat). */
  onRecheck?: () => void;
  /** Underlines the findings in the document in red (nothing changed). */
  onMark?: () => void;
  /** Offer "fix the format errors" (asks the assistant in the chat). */
  onFix?: boolean;
}) {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const rootRef = useRef<HTMLDivElement>(null);
  const popupRef = useContainedPopup(rootRef, placement);
  const isReading = Boolean(reading);
  const running = isReading || check?.status === "running";
  // reading, queued or running: nothing to open yet
  const busy = isReading || !check || formatCheckInProgress(check);

  useEffect(() => {
    if (!running) return;
    const timer = setInterval(() => setNow(Date.now()), 500);
    return () => clearInterval(timer);
  }, [running]);

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

  const progress = isReading
    ? formatCheckProgress(reading?.startedAt ?? "", now, PROFILE_EXPECTED_MS)
    : !check || check.status === "queued"
      ? 0
      : running
        ? formatCheckProgress(check.started_at, now)
        : 1;
  const status = isReading || !check ? "reading" : check.status;
  const label = isReading || !check
    ? t("docws.fcReading")
    : check.status === "queued"
      ? t("docws.fcQueued")
      : running
        ? t("docws.fcRunning")
        : check.status === "failed"
          ? t("docws.fcFailed")
          : check.document_type_label
            ? t("docws.fcReadyType", { type: check.document_type_label })
            : t("docws.fcReady");

  const ask = (question: string) => {
    setOpen(false);
    onAsk(question);
  };

  return (
    <div ref={rootRef} className="group relative shrink-0">
      <button
        type="button"
        aria-label={label}
        aria-haspopup={busy ? undefined : "menu"}
        aria-expanded={busy ? undefined : open}
        onClick={() => {
          if (busy) return;
          setOpen((v) => !v);
          onOpen();
        }}
        className={`relative flex h-7 w-7 items-center justify-center rounded-full transition-colors ${
          busy ? "cursor-default" : "cursor-pointer hover:bg-surface-strong"
        }`}
      >
        <svg width={SIZE} height={SIZE} viewBox={`0 0 ${SIZE} ${SIZE}`} aria-hidden="true" className="-rotate-90">
          <circle
            cx={SIZE / 2}
            cy={SIZE / 2}
            r={RADIUS}
            fill="none"
            strokeWidth={STROKE}
            style={{ stroke: "var(--color-hairline-strong)" }}
          />
          <circle
            cx={SIZE / 2}
            cy={SIZE / 2}
            r={RADIUS}
            fill="none"
            strokeWidth={STROKE}
            strokeLinecap="round"
            strokeDasharray={CIRCUMFERENCE}
            strokeDashoffset={CIRCUMFERENCE * (1 - progress)}
            className="transition-[stroke-dashoffset] duration-500 ease-out motion-reduce:transition-none"
            style={{ stroke: status === "failed" ? "var(--color-error)" : "var(--color-primary)" }}
          />
        </svg>
        <span className="absolute inset-0 flex items-center justify-center text-[9px] font-semibold text-ink" aria-hidden="true">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
            <path d="M13 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V10l-7-7ZM13 3v7h7M8 15l2 2 5-5" />
          </svg>
        </span>
        {status === "failed" && <span className="absolute -right-0.5 -top-0.5 rounded-full bg-error px-1 text-[8px] leading-3 text-white" aria-hidden="true">!</span>}
        {unseen && status === "ready" && (
          <span className="absolute right-1 top-1 h-1.5 w-1.5 rounded-full bg-error" aria-hidden="true" />
        )}
      </button>

      {!open && (
        <div
          ref={popupRef}
          role="tooltip"
          className={`pointer-events-none absolute right-0 ${placement === "above" ? "bottom-full mb-1" : "top-full mt-1"} z-50 w-max max-w-[260px] rounded-[8px] bg-ink px-2.5 py-1.5 text-[12px] leading-snug text-canvas opacity-0 shadow-[0_4px_16px_rgba(0,0,0,0.12)] transition-opacity duration-150 group-hover:opacity-100 group-focus-within:opacity-100`}
        >
          {label}
        </div>
      )}

      {open && (
        <div
          ref={popupRef}
          role="menu"
          aria-label={label}
          className={`card absolute right-0 ${placement === "above" ? "bottom-full mb-1" : "top-full mt-1"} z-50 flex w-max max-w-[calc(100vw-1.5rem)] items-center gap-2 py-1.5 pl-3 pr-1.5 shadow-[0_4px_16px_rgba(0,0,0,0.08)]`}
        >
          <p className={`min-w-0 truncate text-[13px] font-medium ${status === "failed" ? "text-error" : "text-ink"}`}>
            {status === "failed"
              ? t("docws.fcFailed")
              : check?.document_type_label
                ? t("docws.fcReadyType", { type: check.document_type_label })
                : t("docws.fcReady")}
          </p>
          <div className="flex shrink-0 items-center">
            {status === "ready" && (
              <>
                <IconAction role="menuitem" label={t("docws.fcView")} onClick={() => { setOpen(false); onView(); }}>
                  <IconEye className="h-4 w-4" />
                </IconAction>
                {onFix && (
                  <IconAction role="menuitem" label={t("docws.fcFix")} disabled={disabled} onClick={() => ask(t("docws.fcAskFix"))}>
                    <IconWand />
                  </IconAction>
                )}
                {onMark && (
                  <IconAction role="menuitem" label={t("docws.fcMark")} disabled={disabled} tipAlign="end" onClick={() => { setOpen(false); onMark(); }}>
                    <IconUnderlineMark />
                  </IconAction>
                )}
              </>
            )}
            {(status === "failed" || onRecheck) && (
              <IconAction
                role="menuitem"
                label={onRecheck ? t("docws.fcRecheck") : t("docws.fcRetry")}
                disabled={status === "failed" && !onRecheck && disabled}
                tipAlign="end"
                onClick={() => {
                  setOpen(false);
                  if (onRecheck) onRecheck();
                  else ask(t("docws.fcAskView"));
                }}
              >
                <IconRefresh className="h-4 w-4" />
              </IconAction>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
