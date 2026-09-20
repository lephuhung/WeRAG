/* Ported from frontend/src/views/agent/AgentList.vue + AgentEditorModal.vue
 * (subsurface: create/edit agents). Model/platform surface knobs mirror the
 * payload the Vue editor assembles for updateAgent — the settings drawer
 * sections most agents never touch stay in the Vue app until ported.
 */
"use client";

import { useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { useT } from "@/lib/i18n";
import {
  createAgent,
  updateAgent,
  type CustomAgent,
} from "@/lib/api/agents";

type Draft = {
  name: string;
  description: string;
  agent_mode: "quick-answer" | "smart-reasoning";
  model_id: string;
  system_prompt: string;
  temperature: number;
  max_iterations: number;
  citation_enabled: boolean;
};

function draftFrom(a: CustomAgent | null): Draft {
  return {
    name: a?.name ?? "",
    description: a?.description ?? "",
    agent_mode: a?.config?.agent_mode ?? "quick-answer",
    model_id: a?.config?.model_id ?? "",
    system_prompt: a?.config?.system_prompt ?? "",
    temperature: a?.config?.temperature ?? 0.7,
    max_iterations: a?.config?.max_iterations ?? 10,
    citation_enabled: a?.config?.citation_enabled ?? true,
  };
}

export function AgentEditorModal({ open, agent, onClose, onSaved }: {
  open: boolean;
  agent: CustomAgent | null;
  onClose: () => void;
  onSaved: (a: CustomAgent) => void;
}) {
  const { t } = useT();
  const [draft, setDraft] = useState<Draft>(() => draftFrom(agent));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    setDraft(draftFrom(agent));
    setError("");
  }, [agent, open]);

  const patch = <K extends keyof Draft>(k: K, v: Draft[K]) =>
    setDraft((d) => ({ ...d, [k]: v }));

  const save = async () => {
    if (!draft.name.trim()) return;
    setSaving(true);
    setError("");
    try {
      const res = agent
        ? await updateAgent(agent.id, {
            name: draft.name.trim(),
            description: draft.description,
            config: {
              agent_mode: draft.agent_mode,
              model_id: draft.model_id || undefined,
              system_prompt: draft.system_prompt || undefined,
              temperature: draft.temperature,
              max_iterations: draft.max_iterations,
              citation_enabled: draft.citation_enabled,
            },
          })
        : await createAgent({
            name: draft.name.trim(),
            description: draft.description,
            config: {
              agent_mode: draft.agent_mode,
              model_id: draft.model_id || undefined,
              system_prompt: draft.system_prompt || undefined,
              temperature: draft.temperature,
              max_iterations: draft.max_iterations,
              citation_enabled: draft.citation_enabled,
            },
          });
      onSaved(res.data);
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal open={open} title={agent ? t("agent.edit") : t("agent.create")} onClose={onClose} width="w-[640px]">
      <div className="flex flex-col gap-4">
        {error && <p className="caption text-error">{error}</p>}
        <label className="block">
          <span className="caption mb-1.5 block text-muted">{t("agent.name")}</span>
          <input className="input" value={draft.name} onChange={(e) => patch("name", e.target.value)} autoFocus />
        </label>
        <label className="block">
          <span className="caption mb-1.5 block text-muted">{t("agent.description")}</span>
          <textarea
            className="input h-auto min-h-[64px] resize-y"
            value={draft.description}
            onChange={(e) => patch("description", e.target.value)}
          />
        </label>
        <label className="block">
          <span className="caption mb-1.5 block text-muted">{t("agent.mode")}</span>
          <select
            className="input w-[280px]"
            value={draft.agent_mode}
            onChange={(e) => patch("agent_mode", e.target.value as "quick-answer" | "smart-reasoning")}
          >
            <option value="quick-answer">{t("agent.modeQuick")}</option>
            <option value="smart-reasoning">{t("agent.modeSmart")}</option>
          </select>
          <p className="caption mt-1 text-muted-soft">{t("agent.modeDesc")}</p>
        </label>
        <label className="block">
          <span className="caption mb-1.5 block text-muted">{t("agent.modelId")}</span>
          <input
            className="input"
            value={draft.model_id}
            placeholder="e.g. gpt-4o-mini"
            onChange={(e) => patch("model_id", e.target.value)}
          />
        </label>
        {draft.agent_mode === "smart-reasoning" && (
          <>
            <label className="block">
              <span className="caption mb-1.5 block text-muted">{t("agent.systemPrompt")}</span>
              <textarea
                className="input h-auto min-h-[96px] font-mono text-[13px] resize-y"
                value={draft.system_prompt}
                onChange={(e) => patch("system_prompt", e.target.value)}
              />
            </label>
            <div className="flex gap-4">
              <label className="block flex-1">
                <span className="caption mb-1.5 block text-muted">{t("agent.temperature")}</span>
                <input
                  className="input"
                  type="number"
                  step="0.1"
                  min={0}
                  max={2}
                  value={draft.temperature}
                  onChange={(e) => patch("temperature", Number(e.target.value))}
                />
              </label>
              <label className="block flex-1">
                <span className="caption mb-1.5 block text-muted">{t("agent.maxIterations")}</span>
                <input
                  className="input"
                  type="number"
                  min={-1}
                  value={draft.max_iterations}
                  onChange={(e) => patch("max_iterations", Number(e.target.value))}
                />
              </label>
            </div>
          </>
        )}
      </div>

      <div className="mt-5 flex items-center justify-end gap-2 border-t border-hairline pt-4">
        <button className="btn btn-outline btn-sm" onClick={onClose} disabled={saving}>
          {t("common.cancel")}
        </button>
        <button className="btn btn-primary btn-sm" disabled={saving || !draft.name.trim()} onClick={() => void save()}>
          {saving ? "…" : t("common.save")}
        </button>
      </div>
    </Modal>
  );
}
