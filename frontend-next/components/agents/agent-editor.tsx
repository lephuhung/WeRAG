/* Ported from frontend/src/views/agent/AgentList.vue + AgentEditorModal.vue
 * (subsurface: create/edit agents with KB, Tools & MCP binding).
 */
"use client";

import { useEffect, useMemo, useState } from "react";
import { Modal } from "@/components/modal";
import { Select } from "@/components/select";
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
  rerank_model_id: string;
  system_prompt: string;
  temperature: number;
  max_iterations: number;
  max_completion_tokens: number;
  thinking: boolean;
  citation_enabled: boolean;
  // KB binding & retrieval
  kb_selection_mode: "all" | "selected" | "none";
  knowledge_bases: string[];
  retrieve_kb_only_when_mentioned: boolean;
  embedding_top_k: number;
  vector_threshold: number;
  keyword_threshold: number;
  rerank_top_k: number;
  rerank_threshold: number;
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
    rerank_model_id: a?.config?.rerank_model_id ?? "",
    system_prompt: a?.config?.system_prompt ?? "",
    temperature: a?.config?.temperature ?? 0.7,
    max_iterations: a?.config?.max_iterations ?? 10,
    max_completion_tokens: a?.config?.max_completion_tokens ?? 0,
    thinking: a?.config?.thinking ?? false,
    citation_enabled: a?.config?.citation_enabled ?? true,
    kb_selection_mode: a?.config?.kb_selection_mode ?? "all",
    knowledge_bases: a?.config?.knowledge_bases ?? [],
    retrieve_kb_only_when_mentioned: a?.config?.retrieve_kb_only_when_mentioned ?? false,
    embedding_top_k: a?.config?.embedding_top_k ?? 10,
    vector_threshold: a?.config?.vector_threshold ?? 0.5,
    keyword_threshold: a?.config?.keyword_threshold ?? 0.3,
    rerank_top_k: a?.config?.rerank_top_k ?? 5,
    rerank_threshold: a?.config?.rerank_threshold ?? 0.2,
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

  const rerankModels = useMemo(() => {
    return modelList.filter((m) => m.type === "Rerank");
  }, [modelList]);

  // Auto-select default models when creating a new agent
  useEffect(() => {
    if (!agent && open) {
      setDraft((prev) => {
        let updated = false;
        let newModelId = prev.model_id;
        let newRerankModelId = prev.rerank_model_id;

        if (!newModelId && chatModels.length > 0) {
          const defaultChat = chatModels.find((m) => m.is_default) ?? chatModels[0];
          if (defaultChat?.id) {
            newModelId = defaultChat.id;
            updated = true;
          }
        }

        if (!newRerankModelId && rerankModels.length > 0) {
          const defaultRerank = rerankModels.find((m) => m.is_default) ?? rerankModels[0];
          if (defaultRerank?.id) {
            newRerankModelId = defaultRerank.id;
            updated = true;
          }
        }

        return updated
          ? { ...prev, model_id: newModelId, rerank_model_id: newRerankModelId }
          : prev;
      });
    }
  }, [agent, open, chatModels, rerankModels]);

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

    // Validate rerank model requirement when KB is active
    if (
      draft.agent_mode === "smart-reasoning" &&
      draft.kb_selection_mode !== "none" &&
      !draft.rerank_model_id
    ) {
      setError(t("agent.rerankRequired"));
      return;
    }

    setSaving(true);
    setError("");
    try {
      const configPayload = {
        agent_mode: draft.agent_mode,
        model_id: draft.model_id || undefined,
        rerank_model_id: draft.rerank_model_id || undefined,
        system_prompt: draft.system_prompt || undefined,
        temperature: draft.temperature,
        max_iterations: draft.max_iterations,
        max_completion_tokens: draft.max_completion_tokens > 0 ? draft.max_completion_tokens : undefined,
        thinking: draft.thinking,
        citation_enabled: draft.citation_enabled,
        kb_selection_mode: draft.kb_selection_mode,
        knowledge_bases: draft.kb_selection_mode === "selected" ? draft.knowledge_bases : undefined,
        retrieve_kb_only_when_mentioned: draft.retrieve_kb_only_when_mentioned,
        embedding_top_k: draft.embedding_top_k,
        vector_threshold: draft.vector_threshold,
        keyword_threshold: draft.keyword_threshold,
        rerank_top_k: draft.rerank_top_k,
        rerank_threshold: draft.rerank_threshold,
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
            <div>
              <label className="block">
                <span className="caption mb-1.5 block text-muted">{t("agent.mode")}</span>
                <Select
                  className="w-full"
                  value={draft.agent_mode}
                  disabled={readOnly || isBuiltin}
                  onChange={(v) =>
                    patch("agent_mode", v as "quick-answer" | "smart-reasoning")
                  }
                  options={[
                    { value: "quick-answer", label: t("agent.modeQuick") },
                    { value: "smart-reasoning", label: t("agent.modeSmart") },
                  ]}
                />
              </label>
            </div>

            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <label className="block">
                <span className="caption mb-1.5 block text-muted">{t("agent.modelId")} *</span>
                <Select
                  className="w-full"
                  value={draft.model_id}
                  disabled={readOnly}
                  onChange={(v) => patch("model_id", v)}
                  placeholder={t("model.selectPlaceholder")}
                  options={[
                    { value: "", label: t("model.selectPlaceholder") },
                    ...chatModels
                      .filter((m) => m.id)
                      .map((m) => {
                        const label = m.display_name?.trim() || m.name;
                        const meta = [
                          m.parameters?.parameter_size,
                          m.parameters?.provider || (m.source === "local" ? "local" : ""),
                          m.is_default ? "default" : "",
                          m.is_builtin ? "built-in" : "",
                        ]
                          .filter(Boolean)
                          .join(", ");
                        return { value: m.id!, label: meta ? `${label} (${meta})` : label };
                      }),
                    ...(draft.model_id && !chatModels.some((m) => m.id === draft.model_id)
                      ? [{ value: draft.model_id, label: `${draft.model_id} (Current)` }]
                      : []),
                  ]}
                />
              </label>

              <label className="block">
                <div className="flex items-center justify-between mb-1.5">
                  <span className="caption text-muted">
                    {t("agent.rerankModel")}{" "}
                    <span className={draft.kb_selection_mode !== "none" ? "text-error" : ""}>
                      {draft.kb_selection_mode !== "none" ? "*" : ""}
                    </span>
                  </span>
                  {rerankModels.length === 0 && (
                    <span className="caption text-amber-600 text-[10px]">No rerank models</span>
                  )}
                </div>
                <Select
                  className="w-full"
                  value={draft.rerank_model_id}
                  disabled={readOnly}
                  onChange={(v) => patch("rerank_model_id", v)}
                  placeholder={t("agent.rerankPlaceholder")}
                  options={[
                    { value: "", label: t("agent.rerankPlaceholder") },
                    ...rerankModels
                      .filter((m) => m.id)
                      .map((m) => {
                        const label = m.display_name?.trim() || m.name;
                        const meta = [
                          m.parameters?.provider || (m.source === "local" ? "local" : ""),
                          m.is_default ? "default" : "",
                        ]
                          .filter(Boolean)
                          .join(", ");
                        return { value: m.id!, label: meta ? `${label} (${meta})` : label };
                      }),
                    ...(draft.rerank_model_id && !rerankModels.some((m) => m.id === draft.rerank_model_id)
                      ? [{ value: draft.rerank_model_id, label: `${draft.rerank_model_id} (Current)` }]
                      : []),
                  ]}
                />
              </label>
            </div>

            <div className="rounded-[10px] border border-hairline bg-surface-strong/40 px-3 py-2 text-[11.5px] text-muted flex items-center gap-2">
              <span className="text-primary font-bold">ℹ</span>
              <span>
                <strong>Embedding &amp; ReRank:</strong> Model Embedding được gắn cố định theo từng Cơ sở tri thức (KB). Model ReRank được cấu hình tại đây để xếp hạng lại tài liệu tìm kiếm.
              </span>
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
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                  <label className="block">
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
                  <label className="block">
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
                  <label className="block">
                    <span className="caption mb-1.5 block text-muted">{t("agent.maxTokens")}</span>
                    <input
                      className="input w-full"
                      type="number"
                      min={0}
                      step={256}
                      value={draft.max_completion_tokens}
                      disabled={readOnly}
                      onChange={(e) => patch("max_completion_tokens", Number(e.target.value))}
                    />
                  </label>
                </div>
              </>
            )}

            <div className="flex flex-col gap-2 pt-1">
              <label className="flex items-center gap-2 text-ink cursor-pointer select-none">
                <input
                  type="checkbox"
                  id="citation_en"
                  checked={draft.citation_enabled}
                  disabled={readOnly}
                  onChange={(e) => patch("citation_enabled", e.target.checked)}
                />
                <span>Enable citations and source references in answers</span>
              </label>

              <label className="flex items-center gap-2 text-ink cursor-pointer select-none">
                <input
                  type="checkbox"
                  id="thinking_en"
                  checked={draft.thinking}
                  disabled={readOnly}
                  onChange={(e) => patch("thinking", e.target.checked)}
                />
                <span>{t("agent.thinking")}</span>
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

            {draft.kb_selection_mode !== "none" && (
              <>
                <div className="rounded-[10px] border border-hairline p-3">
                  <label className={`flex items-center gap-2.5 text-ink select-none ${readOnly ? "opacity-75 cursor-default" : "cursor-pointer"}`}>
                    <input
                      type="checkbox"
                      checked={draft.retrieve_kb_only_when_mentioned}
                      disabled={readOnly}
                      onChange={(e) => patch("retrieve_kb_only_when_mentioned", e.target.checked)}
                    />
                    <div>
                      <div className="font-medium text-ink">{t("agent.retrieveOnlyWhenMentioned")}</div>
                      <div className="caption text-muted">
                        Khi bật, agent chỉ tìm trong tri thức nếu người dùng có gõ @tên_tri_thức hoặc @tên_tệp.
                      </div>
                    </div>
                  </label>
                </div>

                <div className="rounded-[12px] border border-hairline bg-surface-strong/30 p-4 space-y-3">
                  <span className="font-medium text-ink block text-[13px]">{t("agent.retrievalStrategy")}</span>
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <label className="block">
                      <span className="caption mb-1 block text-muted">{t("agent.embeddingTopK")} (1-50)</span>
                      <input
                        type="number"
                        min={1}
                        max={50}
                        className="input w-full"
                        value={draft.embedding_top_k}
                        disabled={readOnly}
                        onChange={(e) => patch("embedding_top_k", Number(e.target.value))}
                      />
                    </label>
                    <label className="block">
                      <span className="caption mb-1 block text-muted">{t("agent.rerankTopK")} (1-20)</span>
                      <input
                        type="number"
                        min={1}
                        max={20}
                        className="input w-full"
                        value={draft.rerank_top_k}
                        disabled={readOnly}
                        onChange={(e) => patch("rerank_top_k", Number(e.target.value))}
                      />
                    </label>
                    <label className="block">
                      <span className="caption mb-1 block text-muted">{t("agent.rerankThreshold")} (-10..10)</span>
                      <input
                        type="number"
                        step="0.05"
                        min={-10}
                        max={10}
                        className="input w-full"
                        value={draft.rerank_threshold}
                        disabled={readOnly}
                        onChange={(e) => patch("rerank_threshold", Number(e.target.value))}
                      />
                    </label>
                    <label className="block">
                      <span className="caption mb-1 block text-muted">{t("agent.vectorThreshold")} (0..1)</span>
                      <input
                        type="number"
                        step="0.05"
                        min={0}
                        max={1}
                        className="input w-full"
                        value={draft.vector_threshold}
                        disabled={readOnly}
                        onChange={(e) => patch("vector_threshold", Number(e.target.value))}
                      />
                    </label>
                  </div>
                </div>
              </>
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
