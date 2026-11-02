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
import { formatDateTime } from "@/lib/datetime";
import { useToast } from "@/components/toast";
import { Notice } from "@/components/settings/notice";

export function WikiRunWindowCard({
  status,
  onSaved,
}: {
  status: WikiRunWindowStatus;
  onSaved: () => void;
}) {
  const { t, locale } = useT();
  const toast = useToast();
  const [draft, setDraft] = useState<WikiRunWindow>(status);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

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
    setError("");
    try {
      await updateSystemSetting(WIKI_RUN_WINDOW_KEY, draft);
      toast.success(t("q.wikiWindowSaved"));
      onSaved();
    } catch (e) {
      setError(t("q.wikiWindowSaveFailed", { error: (e as Error).message }));
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
            ? formatDateTime(status.next_open, locale)
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
      <div className="mb-4 flex items-center gap-3 border-b border-hairline pb-4">
        <Toggle
          checked={draft.enabled}
          onChange={(enabled) => setDraft({ ...draft, enabled })}
          label={t("q.wikiWindowEnabled")}
        />
        <span className="caption text-ink">{t("q.wikiWindowEnabled")}</span>
      </div>
      <div className="grid grid-cols-[repeat(auto-fit,minmax(min(100%,180px),1fr))] gap-4">
        <label className="caption flex min-w-0 flex-col gap-2 text-muted">
          {t("q.wikiWindowStart")}
          <input
            className="input w-full min-w-0"
            type="time"
            value={draft.start}
            disabled={!draft.enabled}
            onChange={(e) => setDraft({ ...draft, start: e.target.value })}
          />
        </label>
        <label className="caption flex min-w-0 flex-col gap-2 text-muted">
          {t("q.wikiWindowEnd")}
          <input
            className="input w-full min-w-0"
            type="time"
            value={draft.end}
            disabled={!draft.enabled}
            onChange={(e) => setDraft({ ...draft, end: e.target.value })}
          />
        </label>
        <label className="caption flex min-w-0 flex-col gap-2 text-muted">
          {t("q.wikiWindowTz")}
          <input
            className="input w-full min-w-0"
            value={draft.timezone}
            disabled={!draft.enabled}
            onChange={(e) => setDraft({ ...draft, timezone: e.target.value })}
          />
        </label>
      </div>
      <div className="mt-4 flex justify-end">
        <button className="btn btn-primary btn-sm" disabled={!dirty || saving} onClick={() => void save()}>
          {t("common.save")}
        </button>
      </div>
      {error && <Notice tone="error" className="mt-3">{error}</Notice>}
    </div>
  );
}
