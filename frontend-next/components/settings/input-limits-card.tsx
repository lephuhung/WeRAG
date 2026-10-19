/* SuperAdmin control for the limits.* system settings: the page caps an
 * upload must pass before it is parsed (chat attachments and document
 * assistant on one side, knowledge-base uploads on the other). */
"use client";

import { useEffect, useState } from "react";
import {
  getSystemSetting,
  updateSystemSetting,
  INPUT_LIMIT_KEYS,
  BROAD_QUESTION_GATE_KEY,
  type InputLimitKey,
} from "@/lib/api/system";
import { Toggle } from "@/components/settings/toggle";
import { useT } from "@/lib/i18n";

type Values = Record<InputLimitKey, number>;

const defaults = (): Values =>
  Object.fromEntries(INPUT_LIMIT_KEYS.map((k) => [k.key, k.def])) as Values;

export function InputLimitsCard() {
  const { t } = useT();
  const [saved, setSaved] = useState<Values>(defaults);
  const [draft, setDraft] = useState<Values>(defaults);
  const [broadSaved, setBroadSaved] = useState(true);
  const [broadDraft, setBroadDraft] = useState(true);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const next = defaults();
      await Promise.all(
        INPUT_LIMIT_KEYS.map(async (k) => {
          try {
            const row = await getSystemSetting(k.key);
            const n = typeof row.value === "number" ? row.value : Number(row.value);
            if (Number.isFinite(n) && n >= 0) next[k.key] = n;
          } catch {
            /* not persisted yet → default */
          }
        }),
      );
      let broad = true;
      try {
        const row = await getSystemSetting(BROAD_QUESTION_GATE_KEY);
        if (typeof row.value === "boolean") broad = row.value;
      } catch {
        /* not persisted yet → default */
      }
      if (!cancelled) {
        setSaved(next);
        setDraft(next);
        setBroadSaved(broad);
        setBroadDraft(broad);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const dirty = INPUT_LIMIT_KEYS.some((k) => draft[k.key] !== saved[k.key]) || broadDraft !== broadSaved;

  const save = async () => {
    setSaving(true);
    setMessage(null);
    try {
      for (const k of INPUT_LIMIT_KEYS) {
        if (draft[k.key] !== saved[k.key]) await updateSystemSetting(k.key, draft[k.key]);
      }
      if (broadDraft !== broadSaved) await updateSystemSetting(BROAD_QUESTION_GATE_KEY, broadDraft);
      setSaved(draft);
      setBroadSaved(broadDraft);
      setMessage({ ok: true, text: t("q.limitsSaved") });
    } catch (e) {
      setMessage({ ok: false, text: t("q.limitsSaveFailed", { error: (e as Error).message }) });
    } finally {
      setSaving(false);
    }
  };

  const group = (prefix: "limits.chat.pdf" | "limits.chat.docx" | "limits.kb." | "limits.chat.question" | "limits.parse.", title: string) => (
    <div className="min-w-0 w-full basis-[260px] flex-1">
      <div className="caption mb-2 font-medium text-ink">{title}</div>
      <div className="flex flex-wrap gap-3">
        {INPUT_LIMIT_KEYS.filter((k) => k.key.startsWith(prefix)).map((k) => (
          <label key={k.key} className="caption flex flex-col gap-1 text-muted">
            {t(k.labelKey)}
            <input
              className="input w-24"
              type="number"
              min={0}
              step={1}
              value={draft[k.key]}
              onChange={(e) => {
                const n = Math.max(0, Math.floor(Number(e.target.value) || 0));
                setDraft({ ...draft, [k.key]: n });
              }}
            />
          </label>
        ))}
      </div>
    </div>
  );

  return (
    <div className="card mb-6 p-5">
      <div className="mb-1 flex flex-wrap items-center justify-between gap-2">
        <span className="text-[14px] font-medium text-ink">{t("q.limits")}</span>
      </div>
      <p className="caption mb-4 text-muted">{t("q.limitsDesc")}</p>
      <div className="flex flex-wrap gap-6">
        <div className="min-w-0 w-full basis-[260px] flex-1">
          <div className="caption mb-2 font-medium text-ink">{t("q.limitsChat")}</div>
          <div className="flex flex-wrap gap-3">
            {INPUT_LIMIT_KEYS.filter((k) => k.key.startsWith("limits.chat.pdf") || k.key.startsWith("limits.chat.docx")).map((k) => (
              <label key={k.key} className="caption flex flex-col gap-1 text-muted">
                {t(k.labelKey)}
                <input
                  className="input w-24"
                  type="number"
                  min={0}
                  step={1}
                  value={draft[k.key]}
                  onChange={(e) => setDraft({ ...draft, [k.key]: Math.max(0, Math.floor(Number(e.target.value) || 0)) })}
                />
              </label>
            ))}
          </div>
        </div>
        {group("limits.kb.", t("q.limitsKb"))}
      </div>
      <div className="mt-4 flex flex-wrap gap-6">
        {group("limits.chat.question", t("q.limitsQuestions"))}
        {group("limits.parse.", t("q.limitsParsing"))}
        <div className="min-w-0 w-full basis-[260px] flex-1">
          <div className="caption mb-2 font-medium text-ink">{t("q.limitsBroad")}</div>
          <div className="flex items-center gap-2">
            <Toggle checked={broadDraft} onChange={setBroadDraft} label={t("q.limitsBroadOn")} />
            <span className="caption text-ink">{t("q.limitsBroadOn")}</span>
          </div>
          <p className="caption mt-1 text-muted">{t("q.limitsBroadDesc")}</p>
        </div>
      </div>
      <div className="mt-4 flex items-center gap-3">
        <button className="btn btn-primary btn-sm" disabled={!dirty || saving} onClick={() => void save()}>
          {saving ? t("q.limitsSaving") : t("common.save")}
        </button>
        {message && (
          <span className={`caption ${message.ok ? "text-muted" : "text-error"}`}>{message.text}</span>
        )}
      </div>
    </div>
  );
}
