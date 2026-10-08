"use client";

import { useEffect, useRef, useState } from "react";
import { useT } from "@/lib/i18n";
import type { DocumentFormatCheck } from "@/lib/api/document-workspace";
import { formatCheckInProgress, formatCheckProgress } from "@/lib/api/document-workspace";

const SIZE = 22;
const STROKE = 2;
const RADIUS = (SIZE - STROKE) / 2;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;

/* Background NĐ30 format check of the open document, as a small progress
 * ring in the chat header. Queued (other documents are being checked
 * first): an empty ring. Running: the ring fills over the expected run
 * time. A hover explains either. Finished: a click opens the actions that
 * read or apply the evaluation. */
export function FormatCheckRing({
  check,
  unseen,
  disabled,
  onAsk,
  onOpen,
}: {
  check: DocumentFormatCheck;
  /** The finished result was not opened yet → a small dot. */
  unseen: boolean;
  /** A chat turn is running: the actions wait. */
  disabled: boolean;
  onAsk: (question: string) => void;
  onOpen: () => void;
}) {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const rootRef = useRef<HTMLDivElement>(null);
  const running = check.status === "running";
  // queued or running: nothing to open yet
  const busy = formatCheckInProgress(check);

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

  const progress = check.status === "queued" ? 0 : running ? formatCheckProgress(check.started_at, now) : 1;
  const label =
    check.status === "queued"
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
    <div ref={rootRef} className="group relative ml-auto shrink-0">
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
        className={`relative flex h-8 w-8 items-center justify-center rounded-full transition-colors ${
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
            style={{ stroke: check.status === "failed" ? "var(--color-error)" : "var(--color-primary)" }}
          />
        </svg>
        <span className="absolute inset-0 flex items-center justify-center text-[9px] font-semibold text-ink" aria-hidden="true">
          {check.status === "ready" ? (
            <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
              <path d="M5 12.5l4.5 4.5L19 7.5" />
            </svg>
          ) : check.status === "failed" ? (
            <span className="text-error">!</span>
          ) : null}
        </span>
        {unseen && check.status === "ready" && (
          <span className="absolute right-1 top-1 h-1.5 w-1.5 rounded-full bg-error" aria-hidden="true" />
        )}
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
          {check.status === "ready" && (
            <p className="mt-1 text-[12px] leading-snug text-muted">{t("docws.fcReadyHint")}</p>
          )}
          <div className="mt-2.5 flex flex-col gap-1.5">
            <button
              type="button"
              role="menuitem"
              disabled={disabled}
              onClick={() => ask(t("docws.fcAskView"))}
              className="btn btn-primary btn-sm w-full disabled:opacity-50"
            >
              {check.status === "failed" ? t("docws.fcRetry") : t("docws.fcView")}
            </button>
            {check.status === "ready" && (
              <button
                type="button"
                role="menuitem"
                disabled={disabled}
                onClick={() => ask(t("docws.fcAskFix"))}
                className="btn btn-outline btn-sm w-full disabled:opacity-50"
              >
                {t("docws.fcFix")}
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
