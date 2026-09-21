/* Ported from frontend/src/views/agent/AgentList.vue + AgentEditorModal.vue
 * (subsurface: create/edit agents with KB, Tools & MCP binding).
 */
"use client";

import { useEffect, useMemo, useState } from "react";
import { Modal } from "@/components/modal";
import { useT } from "@/lib/i18n";
import {
  createAgent,
  updateAgent,
  isBuiltinAgent,
  type CustomAgent,
} from "@/lib/api/agents";
import { listKnowledgeBases, type KnowledgeBaseRow } from "@/lib/api/knowledge";
import { listMCPServices, type MCPService } from "@/lib/api/mcp";
import { listModels, type ModelConfig } from "@/lib/api/models";

type TabKey = "basic" | "knowledge" | "tools";

type Draft = {
  name: string;
  description: string;
  agent_mode: "quick-answer" | "smart-reasoning";
  model_id: string;
  system_prompt: string;
  temperature: number;
  max_iterations: number;
  citation_enabled: boolean;
  // KB binding
  kb_selection_mode: "all" | "selected" | "none";
  knowledge_bases: string[];
  // Tools & MCP
  web_search_enabled: boolean;
  mcp_selection_mode: "all" | "selected" | "none";
  mcp_services: string[];
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
    kb_selection_mode: a?.config?.kb_selection_mode ?? "all",
    knowledge_bases: a?.config?.knowledge_bases ?? [],
    web_search_enabled: a?.config?.web_search_enabled ?? true,
    mcp_selection_mode: a?.config?.mcp_selection_mode ?? "all",
    mcp_services: a?.config?.mcp_services ?? [],
  };
}

export function AgentEditorModal({
  open,
  agent,
  models,
  readOnly = false,
  onClose,
  onSaved,
}: {
  open: boolean;
  agent: CustomAgent | null;
  models?: ModelConfig[];
  readOnly?: boolean;
  onClose: () => void;
  onSaved: (a: CustomAgent) => void;
}) {
  const { t } = useT();
  const isBuiltin = agent ? isBuiltinAgent(agent.id) : false;
  const [tab, setTab] = useState<TabKey>("basic");
  const [draft, setDraft] = useState<Draft>(() => draftFrom(agent));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const [modelList, setModelList] = useState<ModelConfig[]>(models ?? []);
  const [kbs, setKbs] = useState<KnowledgeBaseRow[]>([]);
  const [mcpList, setMcpList] = useState<MCPService[]>([]);

  useEffect(() => {
    setDraft(draftFrom(agent));
    setTab("basic");
    setError("");
  }, [agent, open]);

  useEffect(() => {
    if (models && models.length > 0) {
      setModelList(models);
    } else if (open) {
      listModels()
        .then((m) => setModelList(m ?? []))
        .catch(() => setModelList([]));
    }
  }, [open, models]);

  const chatModels = useMemo(() => {
    const qaModels = modelList.filter((m) => m.type === "KnowledgeQA");
    return qaModels.length > 0 ? qaModels : modelList;
  }, [modelList]);

  useEffect(() => {
    if (!open) return;
    listKnowledgeBases()
      .then((rows) => setKbs(rows ?? []))
      .catch(() => setKbs([]));
    listMCPServices()
      .then((res: MCPService[]) => setMcpList(res ?? []))
      .catch(() => setMcpList([]));
  }, [open]);

  const patch = <K extends keyof Draft>(k: K, v: Draft[K]) =>
    setDraft((d) => ({ ...d, [k]: v }));

  const toggleKb = (kbId: string) => {
    setDraft((prev) => {
      const exists = prev.knowledge_bases.includes(kbId);
      return {
        ...prev,
        knowledge_bases: exists
          ? prev.knowledge_bases.filter((id) => id !== kbId)
          : [...prev.knowledge_bases, kbId],
      };
    });
  };

  const toggleMcp = (mcpId: string) => {
    setDraft((prev) => {
      const exists = prev.mcp_services.includes(mcpId);
      return {
        ...prev,
        mcp_services: exists
          ? prev.mcp_services.filter((id) => id !== mcpId)
          : [...prev.mcp_services, mcpId],
      };
    });
  };

  const save = async () => {
    if (readOnly) return;
    const trimmedName = draft.name.trim() || agent?.name || "";
    if (!trimmedName) return;
    setSaving(true);
    setError("");
    try {
      const configPayload = {
        agent_mode: draft.agent_mode,
        model_id: draft.model_id || undefined,
        system_prompt: draft.system_prompt || undefined,
        temperature: draft.temperature,
        max_iterations: draft.max_iterations,
        citation_enabled: draft.citation_enabled,
        kb_selection_mode: draft.kb_selection_mode,
        knowledge_bases: draft.kb_selection_mode === "selected" ? draft.knowledge_bases : undefined,
        web_search_enabled: draft.web_search_enabled,
        mcp_selection_mode: draft.mcp_selection_mode,
        mcp_services: draft.mcp_selection_mode === "selected" ? draft.mcp_services : undefined,
      };

      const res = agent
        ? await updateAgent(agent.id, {
            name: trimmedName,
            description: draft.description,
            config: configPayload,
          })
        : await createAgent({
            name: trimmedName,
            description: draft.description,
            config: configPayload,
          });
      onSaved(res.data);
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSaving(false);
    }
  };

  const modalTitle = readOnly
    ? `${t("common.view")} - ${agent?.name ?? ""}`
    : agent
    ? t("agent.edit")
    : t("agent.create");

  return (
    <Modal
      open={open}
      title={modalTitle}
      onClose={onClose}
      width="w-[680px]"
    >
      <div className="flex flex-col gap-4 text-xs">
        {error && <p className="caption text-error">{error}</p>}

        {isBuiltin && (
          <div className="flex items-start gap-2.5 rounded-[10px] border border-hairline bg-surface-strong/60 p-3 text-[12px] text-muted">
            <span className="badge-pill bg-primary/10 text-primary shrink-0">built-in</span>
            <span>
              {t("agentEditor.builtinHint")}
            </span>
          </div>
        )}

        {/* Tab switcher */}
        <div className="flex items-center gap-1 rounded-full bg-surface-strong p-1 w-fit">
          <button
            type="button"
            onClick={() => setTab("basic")}
            className={`rounded-full px-3.5 py-1.5 font-medium transition-colors ${
              tab === "basic" ? "bg-surface-card text-ink shadow-sm" : "text-muted hover:text-ink"
            }`}
          >
            Basic info
          </button>
          <button
            type="button"
            onClick={() => setTab("knowledge")}
            className={`rounded-full px-3.5 py-1.5 font-medium transition-colors ${
              tab === "knowledge" ? "bg-surface-card text-ink shadow-sm" : "text-muted hover:text-ink"
            }`}
          >
            Knowledge Bases ({draft.kb_selection_mode === "all" ? "All" : draft.knowledge_bases.length})
          </button>
          <button
            type="button"
            onClick={() => setTab("tools")}
            className={`rounded-full px-3.5 py-1.5 font-medium transition-colors ${
              tab === "tools" ? "bg-surface-card text-ink shadow-sm" : "text-muted hover:text-ink"
            }`}
          >
            Tools & MCP
          </button>
        </div>

        {/* Tab: Basic */}
        {tab === "basic" && (
          <div className="space-y-3.5">
            <label className="block">
              <span className="caption mb-1.5 block text-muted">{t("agent.name")} *</span>
              <input
                className="input w-full"
                value={draft.name}
                disabled={readOnly}
                onChange={(e) => patch("name", e.target.value)}
                autoFocus={!readOnly}
              />
            </label>
            <label className="block">
              <span className="caption mb-1.5 block text-muted">{t("agent.description")}</span>
              <textarea
                className="input w-full h-auto min-h-[56px] resize-y"
                value={draft.description}
                disabled={readOnly}
                onChange={(e) => patch("description", e.target.value)}
              />
            </label>
            <div className="grid grid-cols-2 gap-3">
              <label className="block">
                <span className="caption mb-1.5 block text-muted">{t("agent.mode")}</span>
                <select
                  className="input w-full"
                  value={draft.agent_mode}
                  disabled={readOnly || isBuiltin}
                  onChange={(e) =>
                    patch("agent_mode", e.target.value as "quick-answer" | "smart-reasoning")
                  }
                >
                  <option value="quick-answer">{t("agent.modeQuick")}</option>
                  <option value="smart-reasoning">{t("agent.modeSmart")}</option>
                </select>
              </label>
              <label className="block">
                <span className="caption mb-1.5 block text-muted">{t("agent.modelId")}</span>
                <select
                  className="input w-full"
                  value={draft.model_id}
                  disabled={readOnly}
                  onChange={(e) => patch("model_id", e.target.value)}
                >
                  <option value="">{t("model.selectPlaceholder")}</option>
                  {chatModels.map((m) => {
                    const label = m.display_name?.trim() || m.name;
                    const meta = [
                      m.parameters?.parameter_size,
                      m.parameters?.provider || (m.source === "local" ? "local" : ""),
                      m.is_default ? "default" : "",
                      m.is_builtin ? "built-in" : "",
                    ]
                      .filter(Boolean)
                      .join(", ");
                    return (
                      <option key={m.id} value={m.id}>
                        {label} {meta ? `(${meta})` : ""}
                      </option>
                    );
                  })}
                  {draft.model_id && !chatModels.some((m) => m.id === draft.model_id) && (
                    <option value={draft.model_id}>
                      {draft.model_id} (Current)
                    </option>
                  )}
                </select>
              </label>
            </div>

            {draft.agent_mode === "smart-reasoning" && (
              <>
                <label className="block">
                  <span className="caption mb-1.5 block text-muted">{t("agent.systemPrompt")}</span>
                  <textarea
                    className="input w-full h-auto min-h-[84px] font-mono text-[12px] resize-y"
                    placeholder="Instructions for the agent's behavior, tone, and tool usage guidelines…"
                    value={draft.system_prompt}
                    disabled={readOnly}
                    onChange={(e) => patch("system_prompt", e.target.value)}
                  />
                </label>
                <div className="flex gap-4">
                  <label className="block flex-1">
                    <span className="caption mb-1.5 block text-muted">{t("agent.temperature")}</span>
                    <input
                      className="input w-full"
                      type="number"
                      step="0.1"
                      min={0}
                      max={2}
                      value={draft.temperature}
                      disabled={readOnly}
                      onChange={(e) => patch("temperature", Number(e.target.value))}
                    />
                  </label>
                  <label className="block flex-1">
                    <span className="caption mb-1.5 block text-muted">{t("agent.maxIterations")}</span>
                    <input
                      className="input w-full"
                      type="number"
                      min={-1}
                      value={draft.max_iterations}
                      disabled={readOnly}
                      onChange={(e) => patch("max_iterations", Number(e.target.value))}
                    />
                  </label>
                </div>
              </>
            )}

            <div className="flex items-center gap-2 pt-1">
              <input
                type="checkbox"
                id="citation_en"
                checked={draft.citation_enabled}
                disabled={readOnly}
                onChange={(e) => patch("citation_enabled", e.target.checked)}
              />
              <label htmlFor="citation_en" className="text-ink cursor-pointer select-none">
                Enable citations and source references in answers
              </label>
            </div>
          </div>
        )}

        {/* Tab: Knowledge */}
        {tab === "knowledge" && (
          <div className="space-y-4">
            <div>
              <span className="caption mb-1.5 block text-muted">Knowledge Retrieval Scope</span>
              <div className="flex gap-2">
                {(["all", "selected", "none"] as const).map((mode) => (
                  <button
                    key={mode}
                    type="button"
                    disabled={readOnly}
                    onClick={() => patch("kb_selection_mode", mode)}
                    className={`rounded-[10px] border px-3.5 py-1.5 font-medium transition-colors ${
                      draft.kb_selection_mode === mode
                        ? "border-primary bg-primary/10 text-primary"
                        : "border-hairline text-muted hover:border-ink hover:text-ink"
                    } ${readOnly ? "opacity-60 cursor-not-allowed" : ""}`}
                  >
                    {mode === "all" ? "All Knowledge Bases" : mode === "selected" ? "Selected Only" : "Disable KB"}
                  </button>
                ))}
              </div>
            </div>

            {draft.kb_selection_mode === "selected" && (
              <div>
                <span className="caption mb-2 block text-muted">Choose Knowledge Bases</span>
                <div className="max-h-[220px] overflow-y-auto space-y-1.5 rounded-[12px] border border-hairline p-2">
                  {kbs.length === 0 ? (
                    <p className="caption text-muted p-2">No knowledge bases found in workspace.</p>
                  ) : (
                    kbs.map((k) => (
                      <label
                        key={k.id}
                        className={`flex items-center gap-2.5 rounded-[8px] p-2 select-none ${
                          readOnly ? "opacity-75 cursor-default" : "hover:bg-surface-strong/50 cursor-pointer"
                        }`}
                      >
                        <input
                          type="checkbox"
                          checked={draft.knowledge_bases.includes(k.id)}
                          disabled={readOnly}
                          onChange={() => toggleKb(k.id)}
                        />
                        <div className="min-w-0 flex-1">
                          <div className="font-medium text-ink truncate">{k.name}</div>
                          {k.description && (
                            <div className="caption text-muted truncate">{k.description}</div>
                          )}
                        </div>
                      </label>
                    ))
                  )}
                </div>
              </div>
            )}
          </div>
        )}

        {/* Tab: Tools & MCP */}
        {tab === "tools" && (
          <div className="space-y-4">
            <div className="rounded-[12px] border border-hairline p-3">
              <label className={`flex items-center gap-2.5 select-none ${readOnly ? "opacity-75 cursor-default" : "cursor-pointer"}`}>
                <input
                  type="checkbox"
                  checked={draft.web_search_enabled}
                  disabled={readOnly}
                  onChange={(e) => patch("web_search_enabled", e.target.checked)}
                />
                <div>
                  <div className="font-medium text-ink">Web Search Tool</div>
                  <div className="caption text-muted">
                    Allows agent to search external internet resources when question needs fresh facts.
                  </div>
                </div>
              </label>
            </div>

            <div>
              <span className="caption mb-1.5 block text-muted">MCP Tool Services</span>
              <div className="flex gap-2 mb-3">
                {(["all", "selected", "none"] as const).map((mode) => (
                  <button
                    key={mode}
                    type="button"
                    disabled={readOnly}
                    onClick={() => patch("mcp_selection_mode", mode)}
                    className={`rounded-[10px] border px-3.5 py-1.5 font-medium transition-colors ${
                      draft.mcp_selection_mode === mode
                        ? "border-primary bg-primary/10 text-primary"
                        : "border-hairline text-muted hover:border-ink hover:text-ink"
                    } ${readOnly ? "opacity-60 cursor-not-allowed" : ""}`}
                  >
                    {mode === "all" ? "All Enabled MCP" : mode === "selected" ? "Selected Only" : "Disable MCP"}
                  </button>
                ))}
              </div>

              {draft.mcp_selection_mode === "selected" && (
                <div className="max-h-[180px] overflow-y-auto space-y-1.5 rounded-[12px] border border-hairline p-2">
                  {mcpList.length === 0 ? (
                    <p className="caption text-muted p-2">No registered MCP services.</p>
                  ) : (
                    mcpList.map((m) => (
                      <label
                        key={m.id}
                        className={`flex items-center gap-2.5 rounded-[8px] p-2 select-none ${
                          readOnly ? "opacity-75 cursor-default" : "hover:bg-surface-strong/50 cursor-pointer"
                        }`}
                      >
                        <input
                          type="checkbox"
                          checked={draft.mcp_services.includes(m.id)}
                          disabled={readOnly}
                          onChange={() => toggleMcp(m.id)}
                        />
                        <div className="min-w-0 flex-1">
                          <div className="font-medium text-ink truncate">{m.name}</div>
                          <div className="caption text-muted truncate font-mono">
                            {m.stdio_config?.command || m.url || "MCP Service"}
                          </div>
                        </div>
                      </label>
                    ))
                  )}
                </div>
              )}
            </div>
          </div>
        )}
      </div>

      <div className="mt-6 flex items-center justify-end gap-2 border-t border-hairline pt-4">
        <button className="btn btn-outline btn-sm" onClick={onClose} disabled={saving}>
          {readOnly ? t("common.close") : t("common.cancel")}
        </button>
        {!readOnly && (
          <button
            className="btn btn-primary btn-sm"
            disabled={saving || (!isBuiltin && !draft.name.trim())}
            onClick={() => void save()}
          >
            {saving ? "Saving…" : t("common.save")}
          </button>
        )}
      </div>
    </Modal>
  );
}
