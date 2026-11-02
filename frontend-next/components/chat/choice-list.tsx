/* Options under a question (document assistant): which legal document,
 * which meaning of an abbreviation, what an unclear word is. A radio
 * group: ticking one sends the option's message at once and the assistant
 * continues; an option with an input opens a text box sent with Enter or
 * the send button.
 */
"use client";

import { useId, useState } from "react";
import { useT } from "@/lib/i18n";
import { AGENT_HANDOFF_TYPE, optionMessage, type ChoiceList, type ChoiceOption } from "@/lib/choice-list";
import { availableChatChoices } from "@/lib/mobile-chat-policy";
import { useMobileChat } from "@/components/use-mobile-chat";

export function ChoiceListView({
  card: sourceCard,
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
  const mobile = useMobileChat();
  const card = { ...sourceCard, options: availableChatChoices(sourceCard.options, mobile) };
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

  if (mobile && sourceCard.kind === AGENT_HANDOFF_TYPE) {
    return <p className="mt-2 text-[13px] leading-relaxed text-muted">{t("docws.mobileWordUnavailable")}</p>;
  }

  if (picked) {
    return <p className="caption mt-1 text-muted-soft">{t("docws.choice.done", { label: picked })}</p>;
  }

  // the agent hand-off: the guessed type as the main button (switch and
  // draft it), the other types as small buttons to correct the guess
  if (card.kind === AGENT_HANDOFF_TYPE) {
    const main = card.options.findIndex((o) => o.primary);
    const small = "rounded-md2 border border-hairline-strong px-2 py-0.5 text-[12px] text-body hover:bg-surface-strong disabled:opacity-50";
    return (
      <div className="mt-2 w-full min-w-0">
        {main >= 0 && (
          <button
            type="button"
            disabled={disabled || working}
            onClick={() => void pick(main)}
            className="rounded-md2 bg-accent px-2.5 py-1 text-[12.5px] font-semibold text-on-primary disabled:opacity-50"
          >
            {t("docws.handoff.switch", { type: card.options[main].label.toLocaleLowerCase("vi-VN") })}
          </button>
        )}
        <div className="mt-1.5 flex flex-wrap items-center gap-1.5" role="group" aria-label={t("docws.handoff.label")}>
          <span className="caption text-muted-soft">{t(main >= 0 ? "docws.handoff.other" : "docws.handoff.pick")}</span>
          {card.options.map((o, i) =>
            i === main ? null : (
              <button key={`${i}-${o.label}`} type="button" disabled={disabled || working} onClick={() => void pick(i)} title={o.detail} className={small}>
                {o.label}
              </button>
            ),
          )}
        </div>
        {working && <p className="caption mt-1 text-muted-soft">{t("docws.handoff.opening")}</p>}
        {error && <p className="caption mt-2 text-error">{error}</p>}
      </div>
    );
  }

  const more = Math.max(0, card.total - card.options.length);
  return (
    <div className="mt-2 w-full min-w-0">
      <ul className="flex flex-col gap-0.5" role="radiogroup" aria-label={t("docws.choice.listLabel")}>
        {card.options.map((o, i) => (
          <li key={`${i}-${o.label}`}>
            <label className="flex cursor-pointer items-start gap-2 rounded-md2 px-1 py-1 text-[13px] text-body hover:bg-surface-strong">
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
                      className="min-w-0 flex-1 rounded-md2 border border-hairline-strong bg-surface-card px-2 py-1 text-[13px] text-ink outline-none focus:border-accent"
                    />
                    <button
                      type="button"
                      disabled={disabled || working || !text.trim()}
                      onClick={() => void pick(i)}
                      className="rounded-md2 bg-accent px-2.5 py-1 text-[12.5px] font-semibold text-on-primary disabled:opacity-50"
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
