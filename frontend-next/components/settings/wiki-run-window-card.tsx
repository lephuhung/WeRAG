/* SuperAdmin control for the `wiki.run_window` system setting: wiki is built
 * only inside a daily window so interactive chat keeps the LLM. */
"use client";

import { useEffect, useState } from "react";
import {
  updateSystemSetting,
  WIKI_RUN_WINDOW_KEY,
  type WikiRunWindow,
  type WikiRunWindowStatus,
} from "@/lib/api/system";
import { Toggle } from "@/components/settings/toggle";
import { useT } from "@/lib/i18n";

export function WikiRunWindowCard({
  status,
  onSaved,
}: {
  status: WikiRunWindowStatus;
  onSaved: () => void;
}) {
  const { t } = useT();
  const [draft, setDraft] = useState<WikiRunWindow>(status);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);

  useEffect(() => {
    setDraft({ enabled: status.enabled, start: status.start, end: status.end, timezone: status.timezone });
  }, [status.enabled, status.start, status.end, status.timezone]);

  const dirty =
    draft.enabled !== status.enabled ||
    draft.start !== status.start ||
    draft.end !== status.end ||
    draft.timezone !== status.timezone;

  const save = async () => {
    setSaving(true);
    setMessage(null);
    try {
      await updateSystemSetting(WIKI_RUN_WINDOW_KEY, draft);
      setMessage({ ok: true, text: t("q.wikiWindowSaved") });
      onSaved();
    } catch (e) {
      setMessage({ ok: false, text: t("q.wikiWindowSaveFailed", { error: (e as Error).message }) });
    } finally {
      setSaving(false);
    }
  };

  const state = !status.enabled
    ? t("q.wikiWindowAlways")
    : status.open
      ? t("q.wikiWindowOpen")
      : t("q.wikiWindowClosed", {
          time: status.next_open
            ? new Date(status.next_open).toLocaleString(undefined, {
                timeZone: status.timezone,
                hour: "2-digit",
                minute: "2-digit",
                day: "2-digit",
                month: "2-digit",
              })
            : status.start,
        });

  return (
    <div className="card mb-6 p-5">
      <div className="mb-1 flex flex-wrap items-center justify-between gap-2">
        <span className="text-[14px] font-medium text-ink">{t("q.wikiWindow")}</span>
        <span className={`caption font-medium ${status.enabled && !status.open ? "text-muted" : "text-ink"}`}>
          {state}
        </span>
      </div>
      <p className="caption mb-4 text-muted">{t("q.wikiWindowDesc")}</p>
      <div className="flex flex-wrap items-end gap-4">
        <Toggle
          checked={draft.enabled}
          onChange={(enabled) => setDraft({ ...draft, enabled })}
          label={t("q.wikiWindowEnabled")}
        />
        <span className="caption text-ink">{t("q.wikiWindowEnabled")}</span>
        <label className="caption flex flex-col gap-1 text-muted">
          {t("q.wikiWindowStart")}
          <input
            className="input w-28"
            type="time"
            value={draft.start}
            disabled={!draft.enabled}
            onChange={(e) => setDraft({ ...draft, start: e.target.value })}
          />
        </label>
        <label className="caption flex flex-col gap-1 text-muted">
          {t("q.wikiWindowEnd")}
          <input
            className="input w-28"
            type="time"
            value={draft.end}
            disabled={!draft.enabled}
            onChange={(e) => setDraft({ ...draft, end: e.target.value })}
          />
        </label>
        <label className="caption flex flex-col gap-1 text-muted">
          {t("q.wikiWindowTz")}
          <input
            className="input w-48"
            value={draft.timezone}
            disabled={!draft.enabled}
            onChange={(e) => setDraft({ ...draft, timezone: e.target.value })}
          />
        </label>
        <button className="btn btn-primary btn-sm" disabled={!dirty || saving} onClick={() => void save()}>
          {t("common.save")}
        </button>
      </div>
      {message && (
        <p className={`caption mt-3 ${message.ok ? "text-muted" : "text-error"}`}>{message.text}</p>
      )}
    </div>
  );
}
