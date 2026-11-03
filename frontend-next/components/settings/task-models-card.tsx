/* SuperAdmin control for the models of the side tasks around a chat turn
 * (system settings llm.*_model_id): the small auxiliary model (titles,
 * follow-ups, memory sentence classifier), the intent model (labels a turn
 * before the agent runs) and the compaction model. Each falls back to the
 * answering model when blank or failing. */
"use client";

import { useEffect, useState } from "react";
import { getSystemSetting, updateSystemSetting } from "@/lib/api/system";
import type { ModelConfig } from "@/lib/api/models";
import { Select } from "@/components/select";
import { FieldLabel } from "@/components/settings/field";
import { Notice } from "@/components/settings/notice";
import { useT } from "@/lib/i18n";
import { useToast } from "@/components/toast";

const TASK_MODEL_KEYS = [
  { key: "llm.auxiliary_model_id", label: "tmod.auxiliary", desc: "tmod.auxiliaryDesc", blank: "tmod.blankOff" },
  { key: "llm.intent_model_id", label: "tmod.intent", desc: "tmod.intentDesc", blank: "tmod.blankAnswering" },
  { key: "llm.compaction_model_id", label: "tmod.compaction", desc: "tmod.compactionDesc", blank: "tmod.blankAnswering" },
] as const;

type Values = Record<string, string>;

export function TaskModelsCard({ models }: { models: ModelConfig[] }) {
  const { t } = useT();
  const toast = useToast();
  const [saved, setSaved] = useState<Values | null>(null);
  const [draft, setDraft] = useState<Values>({});
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const next: Values = {};
      let denied = false;
      await Promise.all(
        TASK_MODEL_KEYS.map(async (k) => {
          try {
            const row = await getSystemSetting(k.key);
            next[k.key] = typeof row.value === "string" ? row.value : "";
          } catch (e) {
            if ((e as { status?: number }).status === 403) denied = true;
            next[k.key] = "";
          }
        }),
      );
      if (!cancelled && !denied) {
        setSaved(next);
        setDraft(next);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  if (!saved) return null;

  const chatModels = models.filter((m) => m.type === "KnowledgeQA");
  const dirty = TASK_MODEL_KEYS.some((k) => draft[k.key] !== saved[k.key]);

  const save = async () => {
    setSaving(true);
    setError("");
    try {
      for (const k of TASK_MODEL_KEYS) {
        if (draft[k.key] !== saved[k.key]) await updateSystemSetting(k.key, draft[k.key]);
      }
      setSaved({ ...draft });
      toast.success(t("tmod.saved"));
    } catch (e) {
      setError(t("tmod.saveFailed", { error: (e as Error).message }));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="card mt-6 p-5">
      <span className="text-[14px] font-medium text-ink">{t("tmod.title")}</span>
      <p className="caption mb-4 mt-1 text-muted">{t("tmod.desc")}</p>
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        {TASK_MODEL_KEYS.map((k) => {
          const value = draft[k.key] ?? "";
          const known = value === "" || chatModels.some((m) => m.id === value);
          return (
            <div key={k.key} className="min-w-0 space-y-2">
              <FieldLabel htmlFor={`tmod-${k.key}`} className="text-sm font-medium text-ink">
                {t(k.label)}
              </FieldLabel>
              <span className="caption block text-muted">{t(k.desc)}</span>
              <Select
                id={`tmod-${k.key}`}
                className="text-sm"
                value={value}
                onChange={(v) => setDraft({ ...draft, [k.key]: v })}
                options={[
                  { value: "", label: t(k.blank) },
                  ...(known ? [] : [{ value, label: t("tmod.missing", { id: value }) }]),
                  ...chatModels.map((m) => ({
                    value: m.id ?? "",
                    label: m.display_name || m.name,
                  })),
                ]}
              />
            </div>
          );
        })}
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
