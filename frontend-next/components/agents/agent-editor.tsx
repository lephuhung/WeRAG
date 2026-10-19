/* Ported from frontend/src/views/agent/AgentList.vue + AgentEditorModal.vue
 * (subsurface: create/edit agents with KB, Tools & MCP binding).
 */
"use client";

import { useEffect, useMemo, useState } from "react";
import { Modal } from "@/components/modal";
import { Select } from "@/components/select";
import { useT, type LocaleKey } from "@/lib/i18n";
import {
  createAgent,
  updateAgent,
  isBuiltinAgent,
  isDocumentAssistantAgent,
  clampOpenDocumentMaxRunes,
  OPEN_DOCUMENT_MAX_RUNES_LIMIT,
  listDomainPacks,
  missingDomainPackTools,
  shippedDomainPacks,
  setAgentEnabled,
  type CustomAgent,
  type DomainPackInfo,
} from "@/lib/api/agents";
import { listKnowledgeBases, type KnowledgeBaseRow } from "@/lib/api/knowledge";
import { listMCPServices, type MCPService } from "@/lib/api/mcp";
import {
  AGENT_TOOLS,
  DEFAULT_ALLOWED_TOOLS,
  TOOL_GROUPS,
  normalizeAllowedTools,
  toggleTool,
  toolInactiveReason,
  unlistedTools,
} from "@/lib/agent-tools";
import { listModels, type ModelConfig } from "@/lib/api/models";
import { buildAgentConfigForSave } from "@/lib/api/agent-config";
import { getStorageEngineStatus } from "@/lib/api/system";
import { formatDate } from "@/lib/datetime";
import { listAllTenants } from "@/lib/api/tenants";
import {
  createAgentAddon,
  formatPriceVND,
  listAgentAddons,
  listUsagePlans,
  updateAgentAddon,
  type UsageAgentAddon,
  type UsagePlan,
} from "@/lib/api/usage";
import {
  addonSaleDraft,
  addonSaleInput,
  shouldSaveAddonSale,
  togglePlanId,
  type AddonSaleDraft,
} from "@/lib/agent-addon-catalogue";
import {
  AGENT_DISTRIBUTIONS,
  agentEnabled,
  switchOffNeedsConfirm,
  defaultDistribution,
  normalizeDistribution,
  scopeForSave,
  scopeTenantOptions,
  toggleScopeTenant,
  type AgentDistribution,
  type ScopeTenantOption,
} from "@/lib/agent-tenant-scope";

type TabKey = "basic" | "knowledge" | "tools" | "multimodal";

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
  allowed_tools: string[];
  // Domain packs (smart-reasoning only)
  domain_packs: string[];
  // Multimodal / attachments
  image_upload_enabled: boolean;
  vlm_model_id: string;
  attachment_image_understanding: boolean;
  attachment_ocr_max_pages: number;
  attachment_parse_wait_timeout_sec: number;
  image_storage_provider: string;
  audio_upload_enabled: boolean;
  asr_model_id: string;
  // Document assistant (sent only when that section is shown)
  format_check_model_id: string;
  spellcheck_model_id: string;
  illustration_mcp_service_id: string;
  format_check_on_open: boolean;
  ask_scope_for_long_documents: boolean;
  web_search_default_on: boolean;
  open_document_max_runes: number;
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
    // an agent stored without a list runs with the server defaults
    allowed_tools: a?.config?.allowed_tools?.length
      ? normalizeAllowedTools(a.config.allowed_tools)
      : [...DEFAULT_ALLOWED_TOOLS],
    domain_packs: a?.config?.domain_packs ?? [],
    image_upload_enabled: a?.config?.image_upload_enabled ?? false,
    vlm_model_id: a?.config?.vlm_model_id ?? "",
    attachment_image_understanding: a?.config?.attachment_image_understanding ?? false,
    attachment_ocr_max_pages: a?.config?.attachment_ocr_max_pages ?? 0,
    attachment_parse_wait_timeout_sec: a?.config?.attachment_parse_wait_timeout_sec ?? 0,
    image_storage_provider: a?.config?.image_storage_provider ?? "",
    audio_upload_enabled: a?.config?.audio_upload_enabled ?? false,
    asr_model_id: a?.config?.asr_model_id ?? "",
    format_check_model_id: a?.config?.format_check_model_id ?? "",
    spellcheck_model_id: a?.config?.spellcheck_model_id ?? "",
    illustration_mcp_service_id: a?.config?.illustration_mcp_service_id ?? "",
    // null / unset = on
    format_check_on_open: a?.config?.format_check_on_open !== false,
    ask_scope_for_long_documents: a?.config?.ask_scope_for_long_documents !== false,
    web_search_default_on: a?.config?.web_search_default_on ?? false,
    open_document_max_runes: clampOpenDocumentMaxRunes(a?.config?.open_document_max_runes ?? 0),
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
  // "Document assistant" settings: the built-in one or any agent allowed to
  // run check_document_format. Its fields are saved only when shown.
  const showDocSection = agent ? isDocumentAssistantAgent(agent.id, agent.config) : false;
  const [tab, setTab] = useState<TabKey>("basic");
  const [draft, setDraft] = useState<Draft>(() => draftFrom(agent));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const [modelList, setModelList] = useState<ModelConfig[]>(models ?? []);
  const [kbs, setKbs] = useState<KnowledgeBaseRow[]>([]);
  const [mcpList, setMcpList] = useState<MCPService[]>([]);
  const [packList, setPackList] = useState<DomainPackInfo[]>([]);
  // only a loaded list may decide that a stored pack no longer ships
  const [packsLoaded, setPacksLoaded] = useState(false);
  // "Tổ chức áp dụng" ([] = every Tổ chức); built-in agents are never scoped
  const [scopeIds, setScopeIds] = useState<number[]>(() => agent?.applies_to_tenant_ids ?? []);
  const [scopeTenants, setScopeTenants] = useState<ScopeTenantOption[]>([]);
  const [scopeLoadFailed, setScopeLoadFailed] = useState(false);
  // "Cách phân phối". A new agent follows the server default (skill set →
  // addon) until the SuperAdmin picks one.
  const [distribution, setDistribution] = useState<AgentDistribution>(() =>
    normalizeDistribution(agent?.distribution),
  );
  const [distributionTouched, setDistributionTouched] = useState(false);
  // the SuperAdmin's on/off switch, saved through its own endpoint
  const [enabled, setEnabled] = useState<boolean>(() => (agent ? agentEnabled(agent) : true));
  // "Bán kèm gói dịch vụ": the agent's row in the add-on catalogue, saved
  // through the catalogue API after the agent itself
  const [addonRow, setAddonRow] = useState<UsageAgentAddon | null>(null);
  const [addonDraft, setAddonDraft] = useState<AddonSaleDraft>(() => addonSaleDraft(null));
  const [addonTouched, setAddonTouched] = useState(false);
  const [addonLoadFailed, setAddonLoadFailed] = useState(false);
  const [salePlans, setSalePlans] = useState<UsagePlan[]>([]);

  useEffect(() => {
    setDraft(draftFrom(agent));
    setScopeIds(agent?.applies_to_tenant_ids ?? []);
    setDistribution(normalizeDistribution(agent?.distribution));
    setDistributionTouched(false);
    setEnabled(agent ? agentEnabled(agent) : true);
    setAddonRow(null);
    setAddonDraft(addonSaleDraft(null));
    setAddonTouched(false);
    setTab("basic");
    setError("");
  }, [agent, open]);

  useEffect(() => {
    if (!open || isBuiltin || readOnly) return;
    let alive = true;
    setAddonLoadFailed(false);
    listUsagePlans(true)
      .then((plans) => alive && setSalePlans(plans.filter((p) => p.is_active)))
      .catch(() => alive && setSalePlans([]));
    if (agent) {
      listAgentAddons(true)
        .then((catalogue) => {
          if (!alive) return;
          const row = catalogue.addons.find((r) => r.agent_id === agent.id) ?? null;
          setAddonRow(row);
          setAddonDraft(addonSaleDraft(row));
        })
        .catch(() => alive && setAddonLoadFailed(true));
    }
    return () => {
      alive = false;
    };
  }, [agent, open, isBuiltin, readOnly]);

  useEffect(() => {
    if (!open || isBuiltin) return;
    setScopeLoadFailed(false);
    listAllTenants()
      .then((res) => {
        if (!res.success) {
          setScopeLoadFailed(true);
          return;
        }
        setScopeTenants(scopeTenantOptions(res.data?.items ?? []));
      })
      .catch(() => setScopeLoadFailed(true));
  }, [open, isBuiltin]);

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

  // KnowledgeQA model options with an "agent's chat model" empty choice and
  // the usual "(Current)" fallback for an id that is not listed.
  const qaModelOptions = (current: string, emptyLabel: string) => [
    { value: "", label: emptyLabel },
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
    ...(current && !chatModels.some((m) => m.id === current) ? [{ value: current, label: `${current} (Current)` }] : []),
  ];

  const rerankModels = useMemo(() => {
    return modelList.filter((m) => m.type === "Rerank");
  }, [modelList]);

  const vlmModels = useMemo(() => {
    return modelList.filter((m) => m.type === "VLLM");
  }, [modelList]);

  const asrModels = useMemo(() => {
    return modelList.filter((m) => m.type === "ASR");
  }, [modelList]);

  const [storageStatus, setStorageStatus] = useState<Record<string, boolean>>({});

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
    setPacksLoaded(false);
    listDomainPacks()
      .then((res) => {
        setPackList(res.data ?? []);
        setPacksLoaded(true);
      })
      .catch(() => setPackList([]));
    getStorageEngineStatus()
      .then((res) => {
        const map: Record<string, boolean> = {};
        for (const e of res.data?.engines ?? []) map[e.name] = e.available;
        setStorageStatus(map);
      })
      .catch(() => setStorageStatus({}));
  }, [open]);

  const patch = <K extends keyof Draft>(k: K, v: Draft[K]) =>
    setDraft((d) => ({ ...d, [k]: v }));

  // Required tools of the chosen packs the agent would lack; the server
  // refuses the save in that case, so it is shown and checked here first.
  const missingPackTools = useMemo(
    () =>
      missingDomainPackTools(packList, draft.domain_packs, {
        allowedTools: draft.allowed_tools,
        kbSelectionMode: draft.kb_selection_mode,
        knowledgeBases: draft.knowledge_bases,
        webSearchEnabled: draft.web_search_enabled,
      }),
    [packList, draft.domain_packs, draft.allowed_tools, draft.kb_selection_mode, draft.knowledge_bases, draft.web_search_enabled],
  );
  // stored names this build no longer ships: removed on save
  const savedPacks = packsLoaded ? shippedDomainPacks(draft.domain_packs, packList) : draft.domain_packs;
  const droppedPacks = draft.domain_packs.filter((name) => !savedPacks.includes(name));

  const toggleDomainPack = (name: string) => {
    const next = draft.domain_packs.includes(name)
      ? draft.domain_packs.filter((n) => n !== name)
      : [...draft.domain_packs, name];
    setDraft((prev) => ({ ...prev, domain_packs: next }));
    if (!agent && !distributionTouched) setDistribution(defaultDistribution(next, scopeIds));
  };

  const pickDistribution = (value: AgentDistribution) => {
    setDistribution(value);
    setDistributionTouched(true);
  };

  const patchAddon = <K extends keyof AddonSaleDraft>(k: K, v: AddonSaleDraft[K]) => {
    setAddonDraft((d) => ({ ...d, [k]: v }));
    setAddonTouched(true);
  };

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

    if (draft.image_upload_enabled && !draft.vlm_model_id) {
      setError(t("agent.vlmRequired"));
      return;
    }

    if (draft.agent_mode === "smart-reasoning" && missingPackTools.length > 0) {
      setError(t("agent.domainPacks.missingTools", { tools: missingPackTools.join(", ") }));
      return;
    }

    const wasEnabled = agent ? agentEnabled(agent) : true;
    if (enabled !== wasEnabled && switchOffNeedsConfirm(agent?.id ?? "", enabled)) {
      if (!window.confirm(t("agentp.switchOffBasicBody", { name: agent?.name ?? "" }))) return;
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
        allowed_tools: draft.allowed_tools,
        // packs only work on smart-reasoning agents; undefined clears them
        domain_packs: draft.agent_mode === "smart-reasoning" && savedPacks.length > 0 ? savedPacks : undefined,
        mcp_services: draft.mcp_selection_mode === "selected" ? draft.mcp_services : undefined,
        image_upload_enabled: draft.image_upload_enabled,
        vlm_model_id: draft.vlm_model_id || undefined,
        attachment_image_understanding: draft.attachment_image_understanding,
        attachment_ocr_max_pages: draft.attachment_ocr_max_pages > 0 ? draft.attachment_ocr_max_pages : undefined,
        attachment_parse_wait_timeout_sec: draft.attachment_parse_wait_timeout_sec > 0 ? draft.attachment_parse_wait_timeout_sec : undefined,
        image_storage_provider: draft.image_storage_provider || undefined,
        audio_upload_enabled: draft.audio_upload_enabled,
        asr_model_id: draft.asr_model_id || undefined,
        // Absent keys keep the stored config on edit (buildAgentConfigForSave),
        // so other agents never send or clear these.
        ...(showDocSection
          ? {
              format_check_model_id: draft.format_check_model_id || undefined,
              spellcheck_model_id: draft.spellcheck_model_id || undefined,
              // "" is sent: it switches back to picking the service automatically
              illustration_mcp_service_id: draft.illustration_mcp_service_id,
              format_check_on_open: draft.format_check_on_open,
              ask_scope_for_long_documents: draft.ask_scope_for_long_documents,
              web_search_default_on: draft.web_search_default_on,
              open_document_max_runes:
                draft.open_document_max_runes > 0 ? clampOpenDocumentMaxRunes(draft.open_document_max_runes) : undefined,
            }
          : {}),
      };

      // The backend PUT replaces the whole config: on EDIT merge the edited
      // fields over the stored config so fields this editor has no UI for
      // (allowed_tools, selected_skills, sandbox_config_id,
      // chat_parser_engine_rules, …) survive, while an explicitly cleared
      // edited value (undefined) still clears the stored key. On CREATE the
      // current defaults go out untouched.
      const config = buildAgentConfigForSave(agent?.config, configPayload, agent ? "edit" : "create");

      // built-in agents never carry a list or a distribution (always basic);
      // only an internal agent keeps its "Tổ chức áp dụng" list
      const appliesTo = isBuiltin ? undefined : scopeForSave(distribution, scopeIds);
      const dist = isBuiltin ? undefined : distribution;
      const res = agent
        ? await updateAgent(agent.id, {
            name: trimmedName,
            description: draft.description,
            config,
            applies_to_tenant_ids: appliesTo,
            distribution: dist,
          })
        : await createAgent({
            name: trimmedName,
            description: draft.description,
            config,
            applies_to_tenant_ids: appliesTo,
            distribution: dist,
          });
      if (enabled !== wasEnabled) {
        const switched = await setAgentEnabled(res.data.id, enabled);
        res.data.enabled = switched.data?.enabled ?? enabled;
      }
      onSaved(res.data);
      // the catalogue row needs the agent saved as a specialist first
      if (!isBuiltin && !addonLoadFailed && shouldSaveAddonSale(addonRow != null, addonTouched, distribution)) {
        const input = addonSaleInput(res.data.id, addonDraft);
        try {
          const saved = addonRow ? await updateAgentAddon(input) : await createAgentAddon(input);
          setAddonRow(saved);
          setAddonTouched(false);
        } catch (e) {
          setError(
            t("agent.addonSale.saveFailed", { reason: e instanceof Error ? e.message : String(e) }),
          );
          return;
        }
      }
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
          <button
            type="button"
            onClick={() => setTab("multimodal")}
            className={`rounded-full px-3.5 py-1.5 font-medium transition-colors ${
              tab === "multimodal" ? "bg-surface-card text-ink shadow-sm" : "text-muted hover:text-ink"
            }`}
          >
            {t("agent.tabMultimodal")}
          </button>
        </div>

        {/* Tab: Basic */}
        {tab === "basic" && (
          <div className="space-y-3.5">
            {!readOnly && (
              <label className="flex cursor-pointer items-start gap-2.5 rounded-[12px] border border-hairline p-3 select-none">
                <input
                  type="checkbox"
                  className="mt-0.5"
                  checked={enabled}
                  onChange={(e) => setEnabled(e.target.checked)}
                />
                <span className="min-w-0">
                  <span className="block text-[13px] font-medium text-ink">{t("agent.enabled.title")}</span>
                  <span className="caption block text-muted">
                    {enabled ? t("agent.enabled.onDesc") : isBuiltin ? t("agent.enabled.offBuiltin") : t("agent.enabled.offCustom")}
                  </span>
                </span>
              </label>
            )}
            {/* "Bộ kỹ năng chuyên môn" first: it decides what the agent is for
                (and, for a new agent, its default distribution) */}
            {(draft.agent_mode === "smart-reasoning" || !isBuiltin) && (
              <div className="rounded-[12px] border border-hairline p-3">
                <span className="mb-1 block text-[13px] font-medium text-ink">{t("agent.domainPacks.title")}</span>
                <p className="caption mb-2 text-muted-soft">{t("agent.domainPacks.desc")}</p>
                {draft.agent_mode !== "smart-reasoning" ? (
                  <p className="caption text-muted">{t("agent.domainPacks.needsSmart")}</p>
                ) : packList.length === 0 ? (
                  <p className="caption text-muted">{t("agent.domainPacks.none")}</p>
                ) : (
                  <div className="space-y-2">
                    {packList.map((pack) => {
                      const checked = draft.domain_packs.includes(pack.name);
                      return (
                        <label
                          key={pack.name}
                          className={`flex items-start gap-2.5 rounded-md border px-2.5 py-2 select-none ${
                            checked ? "border-ink" : "border-hairline"
                          } ${readOnly ? "opacity-75 cursor-default" : "cursor-pointer"}`}
                        >
                          <input
                            type="checkbox"
                            className="mt-0.5"
                            checked={checked}
                            disabled={readOnly}
                            onChange={() => toggleDomainPack(pack.name)}
                          />
                          <span className="min-w-0">
                            <span className="flex flex-wrap items-center gap-1.5">
                              <span className="font-mono text-[13px] text-ink">{pack.name}</span>
                              <span className="caption text-muted-soft">
                                {t("agent.domainPacks.version", { version: pack.version })}
                              </span>
                              <span className="caption text-muted-soft">
                                {t("agent.domainPacks.asOf", { date: formatDate(pack.as_of) })}
                              </span>
                              {pack.overdue && (
                                <span className="caption rounded bg-error/10 px-1 text-error">
                                  {t("agent.domainPacks.overdue")}
                                </span>
                              )}
                            </span>
                            <span className="caption block text-muted">{pack.description}</span>
                            {pack.required_tools?.length > 0 && (
                              <span className="caption block text-muted-soft">
                                {t("agent.domainPacks.requires", { tools: pack.required_tools.join(", ") })}
                              </span>
                            )}
                          </span>
                        </label>
                      );
                    })}
                  </div>
                )}
                {/* a stored pack this build does not ship */}
                {droppedPacks.length > 0 && (
                  <p className="caption mt-2 text-error">
                    {t("agent.domainPacks.unknown", { names: droppedPacks.join(", ") })}
                  </p>
                )}
                {missingPackTools.length > 0 && (
                  <p className="caption mt-2 text-error">
                    {t("agent.domainPacks.missingTools", { tools: missingPackTools.join(", ") })}
                  </p>
                )}
              </div>
            )}
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

            {showDocSection && (
              <div className="rounded-[12px] border border-hairline bg-surface-strong/30 p-4 space-y-3">
                <div>
                  <span className="font-medium text-ink block text-[13px]">{t("agent.docAssist.title")}</span>
                  <span className="caption text-muted">{t("agent.docAssist.desc")}</span>
                </div>
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <label className="block">
                    <span className="caption mb-1.5 block text-muted">{t("agent.docAssist.formatModel")}</span>
                    <Select
                      className="w-full"
                      value={draft.format_check_model_id}
                      disabled={readOnly}
                      onChange={(v) => patch("format_check_model_id", v)}
                      placeholder={t("agent.docAssist.useChatModel")}
                      options={qaModelOptions(draft.format_check_model_id, t("agent.docAssist.useChatModel"))}
                    />
                  </label>
                  <label className="block">
                    <span className="caption mb-1.5 block text-muted">{t("agent.docAssist.spellModel")}</span>
                    <Select
                      className="w-full"
                      value={draft.spellcheck_model_id}
                      disabled={readOnly}
                      onChange={(v) => patch("spellcheck_model_id", v)}
                      placeholder={t("agent.docAssist.useChatModel")}
                      options={qaModelOptions(draft.spellcheck_model_id, t("agent.docAssist.useChatModel"))}
                    />
                  </label>
                  <label className="block">
                    <span className="caption mb-1.5 block text-muted">{t("agent.docAssist.illustrationService")}</span>
                    <Select
                      className="w-full"
                      value={draft.illustration_mcp_service_id}
                      disabled={readOnly}
                      onChange={(v) => patch("illustration_mcp_service_id", v)}
                      placeholder={t("agent.docAssist.illustrationAuto")}
                      options={[
                        { value: "", label: t("agent.docAssist.illustrationAuto") },
                        ...mcpList
                          .filter((m) => m.enabled || m.id === draft.illustration_mcp_service_id)
                          .map((m) => ({ value: m.id, label: m.name })),
                      ]}
                    />
                    <span className="caption mt-1 block text-muted-soft">{t("agent.docAssist.illustrationHint")}</span>
                  </label>
                </div>
                <label className="block sm:w-1/2">
                  <span className="caption mb-1.5 block text-muted">{t("agent.docAssist.maxRunes")}</span>
                  <input
                    className="input w-full"
                    type="number"
                    min={0}
                    max={OPEN_DOCUMENT_MAX_RUNES_LIMIT}
                    step={1000}
                    value={draft.open_document_max_runes}
                    disabled={readOnly}
                    onChange={(e) => patch("open_document_max_runes", clampOpenDocumentMaxRunes(e.target.value))}
                  />
                  <span className="caption mt-1 block text-muted">{t("agent.docAssist.maxRunesHint")}</span>
                </label>
                <div className="flex flex-col gap-2">
                  <label className={`flex items-center gap-2 text-ink select-none ${readOnly ? "cursor-default" : "cursor-pointer"}`}>
                    <input
                      type="checkbox"
                      checked={draft.ask_scope_for_long_documents}
                      disabled={readOnly}
                      onChange={(e) => patch("ask_scope_for_long_documents", e.target.checked)}
                    />
                    <span>{t("agent.docAssist.askScope")}</span>
                  </label>
                  <label
                    className={`flex items-start gap-2 select-none ${
                      !draft.web_search_enabled ? "opacity-50" : ""
                    } ${readOnly || !draft.web_search_enabled ? "cursor-default" : "cursor-pointer"} text-ink`}
                  >
                    <input
                      type="checkbox"
                      className="mt-0.5"
                      checked={draft.web_search_default_on && draft.web_search_enabled}
                      disabled={readOnly || !draft.web_search_enabled}
                      onChange={(e) => patch("web_search_default_on", e.target.checked)}
                    />
                    <span>
                      {t("agent.docAssist.webDefault")}
                      {!draft.web_search_enabled && (
                        <span className="caption block text-muted">{t("agent.docAssist.webDefaultHint")}</span>
                      )}
                    </span>
                  </label>
                </div>
              </div>
            )}
            {!isBuiltin && (
              <div className="rounded-[12px] border border-hairline p-3">
                <span className="mb-1 block text-[13px] font-medium text-ink">{t("agent.distribution.title")}</span>
                <p className="caption mb-2 text-muted-soft">{t("agent.distribution.desc")}</p>
                <div className="space-y-1.5" role="radiogroup" aria-label={t("agent.distribution.title")}>
                  {AGENT_DISTRIBUTIONS.map((value) => (
                    <label
                      key={value}
                      className={`flex items-start gap-2.5 rounded-md border px-2.5 py-2 select-none ${
                        distribution === value ? "border-ink" : "border-hairline"
                      } ${readOnly ? "cursor-default opacity-75" : "cursor-pointer"}`}
                    >
                      <input
                        type="radio"
                        name="agent-distribution"
                        className="mt-0.5"
                        checked={distribution === value}
                        disabled={readOnly}
                        onChange={() => pickDistribution(value)}
                      />
                      <span className="min-w-0">
                        <span className="block text-[13px] text-ink">{t(`agent.distribution.${value}` as LocaleKey)}</span>
                        <span className="caption block text-muted">{t(`agent.distribution.${value}Desc` as LocaleKey)}</span>
                      </span>
                    </label>
                  ))}
                </div>
                {distribution === "addon" && (
                  <p className="caption mt-2 text-muted-soft">{t("agent.distribution.addonNote")}</p>
                )}
                {distribution === "addon" && !readOnly && (
                  <div className="mt-3 rounded-md border border-hairline p-3">
                    <div className="mb-1 flex flex-wrap items-center justify-between gap-2">
                      <span className="text-[13px] font-medium text-ink">{t("agent.addonSale.title")}</span>
                      {addonRow && (
                        <span className="caption text-muted">
                          {t("agent.addonSale.holders", { n: addonRow.holders ?? 0 })}
                        </span>
                      )}
                    </div>
                    <p className="caption mb-2 text-muted-soft">
                      {addonRow ? t("agent.addonSale.descListed") : t("agent.addonSale.descNew")}
                    </p>
                    {addonLoadFailed ? (
                      <p className="caption text-error">{t("agent.addonSale.loadFailed")}</p>
                    ) : (
                      <div className="space-y-3">
                        <label className="block">
                          <span className="caption mb-1 block text-muted">{t("agent.addonSale.price")}</span>
                          <div className="relative">
                            <input
                              type="number"
                              min={0}
                              step={1000}
                              className="input w-full pr-14"
                              value={addonDraft.price_vnd}
                              onChange={(e) => patchAddon("price_vnd", Math.max(0, Number(e.target.value) || 0))}
                            />
                            <span className="caption pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-muted-soft">
                              VNĐ
                            </span>
                          </div>
                          <span className="caption mt-1 block text-muted-soft">
                            {addonDraft.price_vnd > 0
                              ? t("agent.addonSale.priceHint", { price: formatPriceVND(addonDraft.price_vnd) })
                              : t("agent.addonSale.priceFree")}
                          </span>
                        </label>
                        <div>
                          <span className="caption mb-1 block text-muted">{t("agent.addonSale.plans")}</span>
                          {salePlans.length === 0 ? (
                            <p className="caption text-muted">{t("agent.addonSale.noPlans")}</p>
                          ) : (
                            <div className="space-y-1">
                              {salePlans.map((plan) => (
                                <label key={plan.id} className="flex cursor-pointer items-center gap-2 px-1 py-0.5 select-none">
                                  <input
                                    type="checkbox"
                                    checked={addonDraft.plan_ids.includes(plan.id)}
                                    onChange={() => patchAddon("plan_ids", togglePlanId(addonDraft.plan_ids, plan.id))}
                                  />
                                  <span className="text-[13px] text-ink">{plan.name}</span>
                                  {plan.mode === "seat" && (
                                    <span className="caption text-muted-soft">{t("agent.addonSale.perSeat")}</span>
                                  )}
                                </label>
                              ))}
                            </div>
                          )}
                          <span className="caption mt-1 block text-muted-soft">
                            {addonDraft.plan_ids.length === 0
                              ? t("agent.addonSale.allPlans")
                              : t("agent.addonSale.somePlans", { n: addonDraft.plan_ids.length })}
                          </span>
                        </div>
                        <label className="block">
                          <span className="caption mb-1 block text-muted">{t("agent.addonSale.buyerDesc")}</span>
                          <textarea
                            className="input w-full h-auto min-h-[56px] resize-y"
                            value={addonDraft.description}
                            placeholder={t("agent.addonSale.buyerDescPlaceholder")}
                            onChange={(e) => patchAddon("description", e.target.value)}
                          />
                        </label>
                        <label className="flex cursor-pointer items-center gap-2 select-none">
                          <input
                            type="checkbox"
                            checked={addonDraft.is_active}
                            onChange={(e) => patchAddon("is_active", e.target.checked)}
                          />
                          <span className="text-[13px] text-ink">{t("agent.addonSale.active")}</span>
                        </label>
                        {addonRow && addonRow.agent_available === false && (
                          <p className="caption text-amber-600">{t("agent.addonSale.wasOff")}</p>
                        )}
                      </div>
                    )}
                  </div>
                )}
                {distribution === "internal" && (
                  <details className="mt-3" open={(agent?.applies_to_tenant_ids?.length ?? 0) > 0}>
                    <summary className="cursor-pointer select-none text-[13px] text-ink">
                      {t("agent.distribution.advanced")}
                    </summary>
                    <div className="mt-2">
                      <div className="rounded-[12px] border border-hairline p-3">
                        <div className="mb-1 flex items-center justify-between gap-2">
                          <span className="text-[13px] font-medium text-ink">{t("agent.scope.title")}</span>
                          <span className="caption text-muted">
                            {scopeIds.length === 0 ? t("agent.scope.all") : t("agent.scope.count", { count: scopeIds.length })}
                          </span>
                        </div>
                        <p className="caption mb-2 text-muted-soft">{t("agent.scope.desc")}</p>
                        {scopeLoadFailed ? (
                          <p className="caption text-error">{t("agent.scope.loadFailed")}</p>
                        ) : scopeTenants.length === 0 ? (
                          <p className="caption text-muted">{t("agent.scope.none")}</p>
                        ) : (
                          <div className="max-h-[180px] space-y-1 overflow-y-auto">
                            {scopeTenants.map((tenant) => (
                              <label
                                key={tenant.id}
                                className={`flex items-center gap-2 rounded-md px-1.5 py-1 select-none ${
                                  readOnly ? "cursor-default opacity-75" : "cursor-pointer hover:bg-surface-strong/60"
                                }`}
                              >
                                <input
                                  type="checkbox"
                                  checked={scopeIds.includes(tenant.id)}
                                  disabled={readOnly}
                                  onChange={() => setScopeIds((prev) => toggleScopeTenant(prev, tenant.id))}
                                />
                                <span className="min-w-0 truncate text-[13px] text-ink">{tenant.name}</span>
                                <span className="caption tabular-nums text-muted-soft">#{tenant.id}</span>
                              </label>
                            ))}
                          </div>
                        )}
                        {/* stored ids no longer offered (deleted or moved Tổ chức) stay until unticked */}
                        {scopeIds
                          .filter((id) => scopeTenants.length > 0 && !scopeTenants.some((tenant) => tenant.id === id))
                          .map((id) => (
                            <label key={id} className="mt-1 flex items-center gap-2 px-1.5 py-1 select-none">
                              <input
                                type="checkbox"
                                checked
                                disabled={readOnly}
                                onChange={() => setScopeIds((prev) => toggleScopeTenant(prev, id))}
                              />
                              <span className="caption text-error">{t("agent.scope.missing", { id })}</span>
                            </label>
                          ))}
                        {scopeIds.length > 0 && !readOnly && (
                          <button
                            type="button"
                            className="caption mt-2 text-primary hover:underline"
                            onClick={() => setScopeIds([])}
                          >
                            {t("agent.scope.clear")}
                          </button>
                        )}
                      </div>
                    </div>
                  </details>
                )}
              </div>
            )}
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
                    <p className="caption text-muted p-2">No knowledge bases found in this organization.</p>
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

        {/* Tab: Multimodal */}
        {tab === "multimodal" && (
          <div className="space-y-3.5">
            <div className="rounded-[12px] border border-hairline p-3">
              <label className={`flex items-center gap-2.5 select-none ${readOnly ? "opacity-75 cursor-default" : "cursor-pointer"}`}>
                <input
                  type="checkbox"
                  checked={draft.image_upload_enabled}
                  disabled={readOnly}
                  onChange={(e) => patch("image_upload_enabled", e.target.checked)}
                />
                <div>
                  <div className="font-medium text-ink">{t("agent.imageUpload")}</div>
                  <div className="caption text-muted">{t("agent.imageUploadDesc")}</div>
                </div>
              </label>
            </div>

            {draft.image_upload_enabled && (
              <>
                <label className="block">
                  <span className="caption mb-1.5 block text-muted">
                    {t("agent.vlmModel")} <span className="text-error">*</span>
                  </span>
                  <Select
                    className="w-full"
                    value={draft.vlm_model_id}
                    disabled={readOnly}
                    onChange={(v) => patch("vlm_model_id", v)}
                    placeholder={t("model.selectPlaceholder")}
                    options={[
                      { value: "", label: t("model.selectPlaceholder") },
                      ...vlmModels
                        .filter((m) => m.id)
                        .map((m) => {
                          const label = m.display_name?.trim() || m.name;
                          const meta = [
                            m.parameters?.parameter_size,
                            m.parameters?.provider || (m.source === "local" ? "local" : ""),
                            m.is_default ? "default" : "",
                          ]
                            .filter(Boolean)
                            .join(", ");
                          return { value: m.id!, label: meta ? `${label} (${meta})` : label };
                        }),
                      ...(draft.vlm_model_id && !vlmModels.some((m) => m.id === draft.vlm_model_id)
                        ? [{ value: draft.vlm_model_id, label: `${draft.vlm_model_id} (Current)` }]
                        : []),
                    ]}
                  />
                  <p className="caption text-muted mt-1">{t("agent.vlmModelDesc")}</p>
                </label>

                <div className="rounded-[12px] border border-hairline p-3">
                  <label className={`flex items-center gap-2.5 select-none ${readOnly ? "opacity-75 cursor-default" : "cursor-pointer"}`}>
                    <input
                      type="checkbox"
                      checked={draft.attachment_image_understanding}
                      disabled={readOnly}
                      onChange={(e) => patch("attachment_image_understanding", e.target.checked)}
                    />
                    <div>
                      <div className="font-medium text-ink">{t("agent.imageUnderstanding")}</div>
                      <div className="caption text-muted">{t("agent.imageUnderstandingDesc")}</div>
                    </div>
                  </label>
                </div>

                {draft.attachment_image_understanding && (
                  <label className="block w-40">
                    <span className="caption mb-1.5 block text-muted">{t("agent.ocrMaxPages")}</span>
                    <input
                      className="input w-full"
                      type="number"
                      min={0}
                      max={64}
                      step={1}
                      value={draft.attachment_ocr_max_pages}
                      disabled={readOnly}
                      onChange={(e) => patch("attachment_ocr_max_pages", Number(e.target.value))}
                    />
                    <p className="caption text-muted mt-1">{t("agent.ocrMaxPagesDesc")}</p>
                  </label>
                )}

                <label className="block">
                  <span className="caption mb-1.5 block text-muted">{t("agent.storageProvider")}</span>
                  <Select
                    className="w-full"
                    value={draft.image_storage_provider}
                    disabled={readOnly}
                    onChange={(v) => patch("image_storage_provider", v)}
                    placeholder={t("agent.storageDefault")}
                    options={[
                      { value: "", label: t("agent.storageDefault") },
                      { value: "local", label: "Local" },
                      { value: "minio", label: `MinIO${storageStatus.minio === false ? ` (${t("agent.storageNotConfigured")})` : ""}` },
                    ]}
                  />
                  <p className="caption text-muted mt-1">{t("agent.storageProviderDesc")}</p>
                </label>
              </>
            )}

            <div className="rounded-[12px] border border-hairline p-3">
              <label className={`flex items-center gap-2.5 select-none ${readOnly ? "opacity-75 cursor-default" : "cursor-pointer"}`}>
                <input
                  type="checkbox"
                  checked={draft.audio_upload_enabled}
                  disabled={readOnly}
                  onChange={(e) => patch("audio_upload_enabled", e.target.checked)}
                />
                <div>
                  <div className="font-medium text-ink">{t("agent.audioUpload")}</div>
                  <div className="caption text-muted">{t("agent.audioUploadDesc")}</div>
                </div>
              </label>
            </div>

            {draft.audio_upload_enabled && (
              <label className="block">
                <span className="caption mb-1.5 block text-muted">{t("agent.asrModel")}</span>
                <Select
                  className="w-full"
                  value={draft.asr_model_id}
                  disabled={readOnly}
                  onChange={(v) => patch("asr_model_id", v)}
                  placeholder={t("model.selectPlaceholder")}
                  options={[
                    { value: "", label: t("model.selectPlaceholder") },
                    ...asrModels
                      .filter((m) => m.id)
                      .map((m) => {
                        const label = m.display_name?.trim() || m.name;
                        const meta = [
                          m.parameters?.provider || (m.source === "local" ? "local" : ""),
                        ]
                          .filter(Boolean)
                          .join(", ");
                        return { value: m.id!, label: meta ? `${label} (${meta})` : label };
                      }),
                    ...(draft.asr_model_id && !asrModels.some((m) => m.id === draft.asr_model_id)
                      ? [{ value: draft.asr_model_id, label: `${draft.asr_model_id} (Current)` }]
                      : []),
                  ]}
                />
                <p className="caption text-muted mt-1">{t("agent.asrModelDesc")}</p>
              </label>
            )}

            <label className="block w-40">
              <span className="caption mb-1.5 block text-muted">{t("agent.parseWaitTimeout")}</span>
              <input
                className="input w-full"
                type="number"
                min={0}
                max={600}
                step={10}
                value={draft.attachment_parse_wait_timeout_sec}
                disabled={readOnly}
                onChange={(e) => patch("attachment_parse_wait_timeout_sec", Number(e.target.value))}
              />
              <p className="caption text-muted mt-1">{t("agent.parseWaitTimeoutDesc")}</p>
            </label>

            <div className="rounded-[10px] border border-hairline bg-surface-strong/40 px-3 py-2 text-[11.5px] text-muted flex items-center gap-2">
              <span className="text-primary font-bold">ℹ</span>
              <span>{t("agent.multimodalReuploadHint")}</span>
            </div>
          </div>
        )}

        {/* Tab: Tools & MCP */}
        {tab === "tools" && (
          <div className="space-y-4">
            <div>
              <span className="caption mb-1 block text-muted">{t("agent.tools.allowed")}</span>
              <p className="caption mb-2 text-muted-soft">
                {draft.agent_mode === "smart-reasoning" ? t("agent.tools.allowedDesc") : t("agent.tools.needsSmartMode")}
              </p>
              <div className="space-y-3">
                {TOOL_GROUPS.map((group) => {
                  const tools = AGENT_TOOLS.filter((tool) => tool.group === group);
                  const picked = tools.filter((tool) => draft.allowed_tools.includes(tool.name)).length;
                  return (
                    <section key={group} className="rounded-[12px] border border-hairline p-3">
                      <header className="mb-2 flex items-center gap-2">
                        <span className="text-[13px] font-medium text-ink">{t(`agent.tools.group.${group}`)}</span>
                        <span className="caption text-muted-soft">
                          {picked}/{tools.length}
                        </span>
                        {group === "wiki_edit" && (
                          <span className="caption text-error">{t("agent.tools.writeWarning")}</span>
                        )}
                      </header>
                      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                        {tools.map((tool) => {
                          const checked = draft.allowed_tools.includes(tool.name);
                          const reason = checked
                            ? toolInactiveReason(tool, {
                                hasKb: draft.kb_selection_mode !== "none",
                                agentMode: draft.agent_mode,
                              })
                            : null;
                          const hint =
                            reason === "kb"
                              ? t("agent.tools.needsKb")
                              : tool.needs === "document"
                                ? t("agent.tools.needsDocument")
                                : tool.needs === "imageService"
                                  ? t("agent.tools.needsImageService")
                                  : "";
                          return (
                            <label
                              key={tool.name}
                              className={`flex items-start gap-2.5 rounded-md border px-2.5 py-2 select-none ${
                                checked ? "border-ink" : "border-hairline"
                              } ${readOnly ? "opacity-75 cursor-default" : "cursor-pointer"}`}
                            >
                              <input
                                type="checkbox"
                                className="mt-0.5"
                                checked={checked}
                                disabled={readOnly}
                                onChange={() => patch("allowed_tools", toggleTool(draft.allowed_tools, tool.name))}
                              />
                              <span className="min-w-0">
                                <span className="flex items-center gap-1.5">
                                  <span className="text-[13px] text-ink">{t(`agent.tool.${tool.name}` as LocaleKey)}</span>
                                  {tool.danger && (
                                    <span className="caption rounded bg-error/10 px-1 text-error">{t("agent.tools.danger")}</span>
                                  )}
                                </span>
                                <span className="caption block text-muted">{t(`agent.tool.${tool.name}.desc` as LocaleKey)}</span>
                                <span className="caption block font-mono text-muted-soft">{tool.name}</span>
                                {hint && (
                                  <span className={`caption block ${reason === "kb" ? "text-error" : "text-muted-soft"}`}>
                                    {hint}
                                  </span>
                                )}
                              </span>
                            </label>
                          );
                        })}
                      </div>
                    </section>
                  );
                })}
              </div>
              {unlistedTools(draft.allowed_tools).length > 0 && (
                <p className="caption mt-2 text-muted-soft">
                  {t("agent.tools.unlisted")} {unlistedTools(draft.allowed_tools).join(", ")}
                </p>
              )}
              <p className="caption mt-2 text-muted-soft">{t("agent.tools.autoNote")}</p>
            </div>


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
