/* Options under a question (document assistant): which legal document,
 * which meaning of an abbreviation. A radio group: ticking one sends the
 * option's message at once and the assistant continues; nothing to type,
 * nothing to confirm.
 */
"use client";

import { useId, useState } from "react";
import { useT } from "@/lib/i18n";
import type { ChoiceList, ChoiceOption } from "@/lib/choice-list";

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

  const pick = async (i: number) => {
    const o = card.options[i];
    if (!o || working) return;
    setSelected(i);
    setWorking(true);
    setError(null);
    try {
      await onPick(o);
      setPicked(o.label);
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
