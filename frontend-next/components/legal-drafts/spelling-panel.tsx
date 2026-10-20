"use client";

/* Spelling of a legal OCR draft: the document assistant's live spellcheck
 * (useLiveSpelling inside DocumentPane) reviews the paragraphs after each
 * pause and underlines the mistakes in the editor; this panel lists them
 * (a click selects one in the editor, "Không phải lỗi" takes the underline
 * away and records the verdict) and starts a review at once. */

import { useState } from "react";
import { IconRefresh } from "@/components/icons";
import { useT } from "@/lib/i18n";
import { mistakeContext, sameMistake, type GoToResult, type LiveMistake, type LiveSpellingStatus } from "@/lib/live-spelling";

export function LegalDraftSpellingPanel({
  status,
  onRun,
  onGoTo,
  onDismiss,
}: {
  status: LiveSpellingStatus;
  /** Reviews the paragraphs not reviewed yet now. */
  onRun: () => void;
  onGoTo: (m: LiveMistake) => Promise<GoToResult>;
  onDismiss: (m: LiveMistake) => Promise<boolean>;
}) {
  const { t } = useT();
  const [jumping, setJumping] = useState<LiveMistake | null>(null);
  const [lost, setLost] = useState<{ m: LiveMistake; r: Exclude<GoToResult, "ok"> } | null>(null);
  const [dismissing, setDismissing] = useState<LiveMistake | null>(null);
  const [dismissFailed, setDismissFailed] = useState<LiveMistake | null>(null);
  const { phase, total, checked, found } = status;
  const busy = phase === "checking";

  if (phase === "off") return <p className="caption text-muted">{t("ldraft.spelling.off")}</p>;

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

  const goTo = async (m: LiveMistake) => {
    if (jumping || dismissing) return;
    setJumping(m);
    setLost(null);
    const r = await onGoTo(m).catch((): GoToResult => "noAnswer");
    setJumping(null);
    if (r !== "ok") setLost({ m, r });
  };

  const dismiss = async (m: LiveMistake) => {
    if (jumping || dismissing) return;
    setDismissing(m);
    setDismissFailed(null);
    const ok = await onDismiss(m).catch(() => false);
    setDismissing(null);
    if (!ok) setDismissFailed(m);
    setLost(null);
  };

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <p className={`min-w-0 flex-1 text-[13px] font-medium ${phase === "failed" ? "text-error" : "text-ink"}`}>{label}</p>
        <button type="button" className="btn btn-outline btn-sm shrink-0" disabled={busy} onClick={onRun}>
          {busy && <IconRefresh className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none" />}
          {t("ldraft.spelling.run")}
        </button>
      </div>
      <p className="caption text-muted">{t("ldraft.spelling.hint")}</p>
      {status.mistakes.length > 0 && (
        <ul className="-mx-1 flex flex-col">
          {status.mistakes.map((m, i) => (
            <li key={`${i}-${m.paragraph.length}-${m.text}-${m.textOccurrence ?? 1}`} className="flex items-start gap-1">
              <button
                type="button"
                disabled={!!jumping || !!dismissing}
                onClick={() => void goTo(m)}
                className="min-w-0 flex-1 rounded-[6px] px-1.5 py-1.5 text-left hover:bg-surface-strong disabled:cursor-wait"
              >
                <span className="sr-only">{t("docws.spGoTo")}: </span>
                <span className="block text-[13px] leading-snug text-ink">
                  <span className="font-semibold underline decoration-[var(--color-warning)] underline-offset-2">{m.text}</span>
                  {m.suggestion && (
                    <>
                      <span className="mx-1 text-muted-soft">→</span>
                      <span className="font-medium">{m.suggestion}</span>
                    </>
                  )}
                  {jumping && sameMistake(jumping, m) && <span className="ml-2 text-[11.5px] text-muted-soft">…</span>}
                </span>
                <span className="mt-0.5 block text-[12px] leading-snug text-muted">{mistakeContext(m)}</span>
                {lost && sameMistake(lost.m, m) && (
                  <span role="status" className="mt-0.5 block text-[11.5px] leading-snug text-error">
                    {t(
                      lost.r === "outdated" ? "docws.spGoToOutdated" : lost.r === "noAnswer" ? "docws.spGoToNoAnswer" : "docws.spGoToFailed",
                    )}
                  </span>
                )}
                {dismissFailed && sameMistake(dismissFailed, m) && (
                  <span role="status" className="mt-0.5 block text-[11.5px] leading-snug text-error">
                    {t("docws.spDismissFailed")}
                  </span>
                )}
              </button>
              <button
                type="button"
                disabled={!!jumping || !!dismissing}
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
    </div>
  );
}
