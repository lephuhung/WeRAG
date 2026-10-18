"use client";

import { useEffect, useRef, useState, type MutableRefObject } from "react";
import { IconMic } from "@/components/icons";
import { useT } from "@/lib/i18n";
import { appendDictated, startDictation, type Dictation, type DictationError } from "@/lib/speech/dictation";

const DICTATION_ERRORS = {
  insecure: "dictation.error.insecure",
  unsupported: "dictation.error.unsupported",
  denied: "dictation.error.denied",
  no_mic: "dictation.error.no_mic",
  unavailable: "dictation.error.unavailable",
  unauthorized: "dictation.error.unauthorized",
  too_long: "dictation.error.too_long",
} as const satisfies Record<DictationError, string>;

/* Microphone toggle for the composer: the session's punctuated transcript
 * is appended to the draft as it was when listening started, and replaced
 * in place as the service revises it. */
export function DictationButton({
  value,
  onChange,
  onListeningChange,
  onError,
  stopRef,
  disabled,
}: {
  value: string;
  onChange: (v: string) => void;
  onListeningChange: (listening: boolean) => void;
  onError: (message: string) => void;
  /** Filled while listening: sending cancels dictation so late text does not
   *  land in the emptied composer. */
  stopRef: MutableRefObject<(() => void) | null>;
  disabled?: boolean;
}) {
  const { t } = useT();
  const [state, setState] = useState<"idle" | "starting" | "listening">("idle");
  const session = useRef<Dictation | null>(null);
  // The draft when listening started; the transcript goes after it.
  const base = useRef("");
  const cancelled = useRef(false);
  const latest = useRef({ value, onChange, onListeningChange, onError });
  latest.current = { value, onChange, onListeningChange, onError };

  useEffect(() => () => session.current?.cancel(), []);

  const errorText = (e: DictationError) => t(DICTATION_ERRORS[e]);

  const start = async () => {
    setState("starting");
    onError("");
    cancelled.current = false;
    const show = (transcript: string) => {
      if (!cancelled.current) latest.current.onChange(appendDictated(base.current, transcript));
    };
    const d = await startDictation({
      context: () => base.current,
      onPartial: show,
      onFinal: show,
      onError: (e) => latest.current.onError(errorText(e)),
      onEnd: () => {
        // The draft keeps whatever was last shown, even a partial the
        // service could not confirm before ending.
        session.current = null;
        stopRef.current = null;
        setState("idle");
        latest.current.onListeningChange(false);
      },
    });
    if (!d) {
      setState("idle");
      return;
    }
    // Snapshot the draft only now: the textarea stays editable until
    // listening starts (permission prompt, sign-in check).
    base.current = latest.current.value;
    session.current = d;
    stopRef.current = () => {
      cancelled.current = true;
      d.cancel();
    };
    setState("listening");
    latest.current.onListeningChange(true);
  };

  const listening = state === "listening";
  return (
    <button
      type="button"
      onClick={() => (listening ? session.current?.stop() : state === "idle" && void start())}
      disabled={disabled || state === "starting"}
      title={listening ? t("dictation.stop") : t("dictation.start")}
      aria-pressed={listening}
      className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-full transition-colors disabled:opacity-50 ${
        listening ? "animate-pulse bg-error/15 text-error" : "text-muted hover:bg-surface-strong hover:text-ink"
      }`}
    >
      <IconMic className="h-3.5 w-3.5" />
    </button>
  );
}
