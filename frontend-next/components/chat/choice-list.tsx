/* Options under a question (document assistant): which legal document,
 * which meaning of an abbreviation, what an unclear word is. A radio
 * group: ticking one sends the option's message at once and the assistant
 * continues; an option with an input opens a text box sent with Enter or
 * the send button.
 */
"use client";

import { useId, useState } from "react";
import { useT } from "@/lib/i18n";
import { optionMessage, type ChoiceList, type ChoiceOption } from "@/lib/choice-list";

export function ChoiceListView({
  card,
  disabled = false,
  onPick,
}: {
  card: ChoiceList;
  /** A turn is running: the list can be read but not confirmed. */
  disabled?: boolean;
  /** Sends the option's message; a rejection keeps the list with an error. */
  onPick: (option: ChoiceOption) => Promise<void> | void;
}) {
  const { t } = useT();
  const group = useId();
  const [selected, setSelected] = useState<number | null>(null);
  const [working, setWorking] = useState(false);
  const [picked, setPicked] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [text, setText] = useState("");

  const pick = async (i: number) => {
    const o = card.options[i];
    if (!o || working) return;
    setError(null);
    // an option with an input waits for its text
    if (o.input && (selected !== i || !text.trim())) {
      if (selected !== i) setText("");
      setSelected(i);
      return;
    }
    setSelected(i);
    setWorking(true);
    try {
      await onPick({ ...o, resend: optionMessage(o, text) });
      setPicked(o.input ? `${o.label}: ${text.trim()}` : o.label);
    } catch (e) {
      setError(`${t("docws.choice.failed")}: ${e instanceof Error ? e.message : String(e)}`);
    } finally {
      setWorking(false);
    }
  };

  if (picked) {
    return <p className="caption mt-1 text-muted-soft">{t("docws.choice.done", { label: picked })}</p>;
  }

  const more = Math.max(0, card.total - card.options.length);
  return (
    <div className="mt-2 w-full min-w-0">
      <ul className="flex flex-col gap-0.5" role="radiogroup" aria-label={t("docws.choice.listLabel")}>
        {card.options.map((o, i) => (
          <li key={`${i}-${o.label}`}>
            <label className="flex cursor-pointer items-start gap-2 rounded-[6px] px-1 py-1 text-[13px] text-body hover:bg-surface-strong">
              <input
                type="radio"
                name={group}
                className="mt-1"
                checked={selected === i}
                disabled={disabled || working}
                onChange={() => void pick(i)}
              />
              <span className="min-w-0 flex-1">
                <span className="font-semibold text-ink">{o.label}</span>
                {o.badge && (
                  <span className={`ml-2 text-[11.5px] ${o.badgeOk ? "text-ink" : "text-muted-soft"}`}>{o.badge}</span>
                )}
                {o.meta && <span className="ml-2 text-[11.5px] text-muted-soft">{o.meta}</span>}
                {o.detail && <span className="block truncate text-[12.5px] text-body">{o.detail}</span>}
                {o.input && selected === i && (
                  <span className="mt-1 flex items-center gap-1.5">
                    <input
                      type="text"
                      autoFocus
                      value={text}
                      placeholder={o.input}
                      disabled={disabled || working}
                      onChange={(e) => setText(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                          e.preventDefault();
                          void pick(i);
                        }
                      }}
                      className="min-w-0 flex-1 rounded-[6px] border border-hairline-strong bg-surface px-2 py-1 text-[13px] text-ink outline-none focus:border-[#3b82f6]"
                    />
                    <button
                      type="button"
                      disabled={disabled || working || !text.trim()}
                      onClick={() => void pick(i)}
                      className="rounded-[6px] bg-[#1f6feb] px-2.5 py-1 text-[12.5px] font-semibold text-white disabled:opacity-50"
                    >
                      {t("docws.choice.send")}
                    </button>
                  </span>
                )}
              </span>
            </label>
          </li>
        ))}
      </ul>
      {more > 0 && <p className="caption mt-1 text-muted-soft">{t("docws.choice.more", { n: more })}</p>}
      {error && <p className="caption mt-2 text-error">{error}</p>}
    </div>
  );
}
