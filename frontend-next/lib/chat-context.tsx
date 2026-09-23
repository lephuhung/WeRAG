"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { apiGet } from "@/lib/api-client";
import type { SessionLastRequestState } from "@/lib/api/chat";

/* Ports the chat-input slice of frontend/src/stores/settings.ts +
 * chatResources.ts + organization.ts into one React context:
 * - WeKnora_settings localStorage blob (selected KBs/files/tags/MCP/skills,
 *   agent id + source tenant, websearch flag, last chat model)
 * - space-level resource lists for the @mention picker and toolbar
 */

export const BUILTIN_QUICK_ANSWER_ID = "builtin-quick-answer";
export const BUILTIN_SMART_REASONING_ID = "builtin-smart-reasoning";

export type MentionType = "kb" | "file" | "tag" | "mcp" | "skill";

export type MentionRequestItem = {
  id: string;
  name: string;
  type: MentionType;
  kb_type?: "document" | "faq";
  kb_id?: string;
  kb_name?: string;
  service_id?: string;
  skill_name?: string;
};

export type SelectedTag = { id: string; name: string; kbId: string; kbName?: string };

export type AgentSummary = {
  id: string;
  name: string;
  description?: string;
  is_builtin: boolean;
  config?: {
    agent_mode?: string;
    model_id?: string;
    knowledge_bases?: string[];
    kb_selection_mode?: string;
    mcp_selection_mode?: string;
    mcp_services?: string[];
    skills_selection_mode?: string;
    selected_skills?: string[];
    allowed_tools?: string[];
    web_search_enabled?: boolean;
    web_search_provider_id?: string;
    image_upload_enabled?: boolean;
    supported_file_types?: string[];
  };
};

export type SharedAgentSummary = {
  agent: { id: string; name: string; description?: string };
  source_tenant_id: number;
  org_name: string;
  web_search_ready?: boolean;
  disabled_by_me?: boolean;
};

export type KbSummary = {
  id: string;
  name: string;
  type?: string;
  knowledge_count?: number;
  chunk_count?: number;
  org_name?: string;
};

export type ModelSummary = {
  id?: string;
  name: string;
  display_name?: string;
  type?: string;
  parameters?: { context_window?: number };
};

export type ChatSettings = {
  selectedKnowledgeBases: string[];
  selectedFiles: string[];
  selectedFileKbMap: Record<string, string>;
  selectedTags: SelectedTag[];
  selectedMCPServices: string[];
  selectedSkills: string[];
  isAgentEnabled: boolean;
  selectedAgentId: string;
  selectedAgentSourceTenantId: string | null;
  webSearchEnabled: boolean;
  localBrowserEnabled: boolean;
  selectedChatModelId: string;
};

const SETTINGS_KEY = "WeKnora_settings";
const LAST_MODEL_KEY = "weknora_last_chat_model_id";

const DEFAULTS: ChatSettings = {
  selectedKnowledgeBases: [],
  selectedFiles: [],
  selectedFileKbMap: {},
  selectedTags: [],
  selectedMCPServices: [],
  selectedSkills: [],
  isAgentEnabled: false,
  selectedAgentId: BUILTIN_QUICK_ANSWER_ID,
  selectedAgentSourceTenantId: null,
  webSearchEnabled: false,
  localBrowserEnabled: false,
  selectedChatModelId: "",
};

function loadSettings(): ChatSettings {
  try {
    const raw = localStorage.getItem(SETTINGS_KEY);
    if (!raw) return { ...DEFAULTS };
    const parsed = JSON.parse(raw) as Partial<ChatSettings>;
    return {
      ...DEFAULTS,
      ...parsed,
      selectedKnowledgeBases: parsed.selectedKnowledgeBases ?? [],
      selectedFiles: parsed.selectedFiles ?? [],
      selectedFileKbMap: parsed.selectedFileKbMap ?? {},
      selectedTags: parsed.selectedTags ?? [],
      selectedMCPServices: parsed.selectedMCPServices ?? [],
      selectedSkills: parsed.selectedSkills ?? [],
      selectedAgentId: parsed.selectedAgentId || BUILTIN_QUICK_ANSWER_ID,
    };
  } catch {
    return { ...DEFAULTS };
  }
}

function persist(s: ChatSettings) {
  try {
    localStorage.setItem(SETTINGS_KEY, JSON.stringify(s));
  } catch {
    /* ignore */
  }
}

type Ctx = {
  settings: ChatSettings;
  // True once settings have been hydrated from localStorage. Anything that
  // fires on mount (the ?q= auto-send) must wait for it — child effects run
  // before this provider's hydration effect, so they otherwise send the
  // DEFAULTS (webSearchEnabled=false, no KB scope) on the first request.
  hydrated: boolean;
  update: (patch: Partial<ChatSettings>) => void;
  addKnowledgeBase: (id: string) => void;
  removeKnowledgeBase: (id: string) => void;
  addFile: (id: string, kbId?: string, name?: string) => void;
  removeFile: (id: string) => void;
  addTag: (t: SelectedTag) => void;
  removeTag: (id: string, kbId?: string) => void;
  addMCPService: (id: string) => void;
  removeMCPService: (id: string) => void;
  addSkill: (name: string) => void;
  removeSkill: (name: string) => void;
  selectAgent: (id: string, sourceTenantId?: string | null) => void;
  toggleWebSearch: (on: boolean) => void;
  toggleLocalBrowser: (on: boolean) => void;
  setModel: (id: string) => void;
  mentionItems: MentionRequestItem[];
  removeMention: (item: MentionRequestItem) => void;
  fileNames: Record<string, string>;
  // space resources
  agents: AgentSummary[];
  sharedAgents: SharedAgentSummary[];
  knowledgeBases: KbSummary[];
  models: ModelSummary[];
  mcpServices: Array<{ id: string; name: string; description?: string; usage_instructions?: string }>;
  skills: Array<{ name: string; description?: string }>;
  webSearchReady: boolean;
  selectedAgent: AgentSummary | null;
  isAgentStreamMode: boolean;
  hydrateSessionState: (state?: SessionLastRequestState | null) => void;
  refresh: () => Promise<void>;
};

const ChatContext = createContext<Ctx | null>(null);

export function ChatProvider({ children }: { children: React.ReactNode }) {
  const [settings, setSettings] = useState<ChatSettings>(DEFAULTS);
  const [agents, setAgents] = useState<AgentSummary[]>([]);
  const [sharedAgents, setSharedAgents] = useState<SharedAgentSummary[]>([]);
  const [knowledgeBases, setKnowledgeBases] = useState<KbSummary[]>([]);
  const [models, setModels] = useState<ModelSummary[]>([]);
  const [mcpServices, setMcpServices] = useState<Ctx["mcpServices"]>([]);
  const [skills, setSkills] = useState<Ctx["skills"]>([]);
  const [webSearchReady, setWebSearchReady] = useState(false);
  const [fileNames, setFileNames] = useState<Record<string, string>>({});
  const [hydrated, setHydrated] = useState(false);
  const loaded = useRef(false);

  useEffect(() => {
    setSettings(loadSettings());
    setHydrated(true);
  }, []);

  const update = useCallback((patch: Partial<ChatSettings>) => {
    setSettings((prev) => {
      const next = { ...prev, ...patch };
      persist(next);
      return next;
    });
  }, []);

  const refresh = useCallback(async () => {
    try {
      const [agentsRes, sharedRes, kbRes, sharedKbRes, modelsRes, mcpRes, skillsRes, providersRes] =
        await Promise.all([
          apiGet<{ success: boolean; data?: AgentSummary[] }>(`/api/v1/agents`).catch(() => null),
          apiGet<{ success: boolean; data?: SharedAgentSummary[] }>(`/api/v1/shared-agents`).catch(() => null),
          apiGet<{ success: boolean; data?: KbSummary[] }>(`/api/v1/knowledge-bases`).catch(() => null),
          apiGet<{ success: boolean; data?: Array<{ knowledge_base?: KbSummary; org_name?: string }> }>(
            `/api/v1/shared-knowledge-bases`,
          ).catch(() => null),
          apiGet<{ success: boolean; data?: ModelSummary[] }>(`/api/v1/models`).catch(() => null),
          apiGet<{ success: boolean; data?: Ctx["mcpServices"] }>(`/api/v1/mcp-services`).catch(() => null),
          apiGet<{ success: boolean; data?: Ctx["skills"] }>(`/api/v1/skills`).catch(() => null),
          apiGet<{ success: boolean; data?: Array<{ is_default?: boolean }> }>(
            `/api/v1/web-search-providers`,
          ).catch(() => null),
        ]);
      const list = (r: { data?: KbSummary[] } | null): KbSummary[] =>
        Array.isArray(r?.data) ? (r?.data as KbSummary[]) : [];
      const own = list(kbRes);
      const ownIds = new Set(own.map((k) => k.id));
      const shared =
        sharedKbRes?.data
          ?.filter((s) => s.knowledge_base && !ownIds.has(s.knowledge_base.id))
          .map((s) => ({ ...(s.knowledge_base as KbSummary), org_name: s.org_name })) ?? [];
      if (agentsRes?.data) setAgents(agentsRes.data);
      if (sharedRes?.data) setSharedAgents(sharedRes.data.filter((s) => s.agent && !s.disabled_by_me));
      setKnowledgeBases([...own, ...shared]);
      if (modelsRes?.data) {
        setModels(modelsRes.data);
        const mList = modelsRes.data;
        setSettings((prev) => {
          if (prev.selectedChatModelId && mList.some((m) => m.id === prev.selectedChatModelId)) {
            return prev;
          }
          let pick = "";
          try {
            pick = localStorage.getItem(LAST_MODEL_KEY) || "";
          } catch {
            /* ignore */
          }
          if (pick && mList.some((m) => m.id === pick)) {
            const next = { ...prev, selectedChatModelId: pick };
            persist(next);
            return next;
          }
          const defaultModel = mList.find((m) => m.type === "KnowledgeQA" || m.type === "knowledgeqa") || mList[0];
          if (defaultModel?.id) {
            const next = { ...prev, selectedChatModelId: defaultModel.id };
            persist(next);
            return next;
          }
          return prev;
        });
      }
      if (mcpRes?.data) setMcpServices(mcpRes.data);
      if (skillsRes?.data) setSkills(skillsRes.data);
      setWebSearchReady(Boolean(providersRes?.data?.some((p) => p.is_default)));
    } catch {
      /* toolbar degrades to unscoped mode */
    }
  }, []);

  useEffect(() => {
    if (loaded.current) return;
    loaded.current = true;
    void refresh();
  }, [refresh]);

  // Resolve display names for @-picked files (settings store only keeps ids).
  useEffect(() => {
    const missing = settings.selectedFiles.filter((id) => !fileNames[id]);
    if (missing.length === 0) return;
    const qs = missing.map((id) => `ids=${encodeURIComponent(id)}`).join("&");
    apiGet<{ success: boolean; data?: Array<{ id: string; title?: string; file_name?: string }> }>(
      `/api/v1/knowledge/batch?${qs}`,
    )
      .then((res) => {
        if (!res.data) return;
        setFileNames((prev) => {
          const next = { ...prev };
          for (const f of res.data ?? []) next[f.id] = f.title || f.file_name || f.id;
          return next;
        });
      })
      .catch(() => {});
  }, [settings.selectedFiles, fileNames]);

  const addKnowledgeBase = useCallback(
    (id: string) => {
      if (settings.selectedKnowledgeBases.includes(id)) return;
      update({ selectedKnowledgeBases: [...settings.selectedKnowledgeBases, id] });
    },
    [settings.selectedKnowledgeBases, update],
  );
  const removeKnowledgeBase = useCallback(
    (id: string) => update({ selectedKnowledgeBases: settings.selectedKnowledgeBases.filter((x) => x !== id) }),
    [settings.selectedKnowledgeBases, update],
  );
  const addFile = useCallback(
    (id: string, kbId?: string, name?: string) => {
      if (settings.selectedFiles.includes(id)) return;
      if (name) setFileNames((prev) => ({ ...prev, [id]: name }));
      update({
        selectedFiles: [...settings.selectedFiles, id],
        selectedFileKbMap: kbId ? { ...settings.selectedFileKbMap, [id]: kbId } : settings.selectedFileKbMap,
      });
    },
    [settings.selectedFiles, settings.selectedFileKbMap, update],
  );
  const removeFile = useCallback(
    (id: string) => {
      const map = { ...settings.selectedFileKbMap };
      delete map[id];
      update({ selectedFiles: settings.selectedFiles.filter((x) => x !== id), selectedFileKbMap: map });
    },
    [settings.selectedFiles, settings.selectedFileKbMap, update],
  );
  const addTag = useCallback(
    (t: SelectedTag) => {
      if (settings.selectedTags.some((x) => x.id === t.id && x.kbId === t.kbId)) return;
      update({ selectedTags: [...settings.selectedTags, t] });
    },
    [settings.selectedTags, update],
  );
  const removeTag = useCallback(
    (id: string, kbId?: string) =>
      update({ selectedTags: settings.selectedTags.filter((x) => !(x.id === id && (!kbId || x.kbId === kbId))) }),
    [settings.selectedTags, update],
  );
  const addMCPService = useCallback(
    (id: string) => {
      if (settings.selectedMCPServices.includes(id)) return;
      update({ selectedMCPServices: [...settings.selectedMCPServices, id] });
    },
    [settings.selectedMCPServices, update],
  );
  const removeMCPService = useCallback(
    (id: string) => update({ selectedMCPServices: settings.selectedMCPServices.filter((x) => x !== id) }),
    [settings.selectedMCPServices, update],
  );
  const addSkill = useCallback(
    (name: string) => {
      if (settings.selectedSkills.includes(name)) return;
      update({ selectedSkills: [...settings.selectedSkills, name] });
    },
    [settings.selectedSkills, update],
  );
  const removeSkill = useCallback(
    (name: string) => update({ selectedSkills: settings.selectedSkills.filter((x) => x !== name) }),
    [settings.selectedSkills, update],
  );
  const selectAgent = useCallback(
    (id: string, sourceTenantId?: string | null) => {
      // Mirrors settings.selectAgent: switching agent resets the per-turn websearch flag.
      const isQuickAnswer = id === BUILTIN_QUICK_ANSWER_ID;
      const targetAgent = agents.find((a) => a.id === id);
      const agentModelId = targetAgent?.config?.model_id;
      update({
        selectedAgentId: id,
        selectedAgentSourceTenantId: sourceTenantId ? String(sourceTenantId) : null,
        webSearchEnabled: false,
        isAgentEnabled: !isQuickAnswer,
        ...(agentModelId ? { selectedChatModelId: agentModelId } : {}),
      });
    },
    [agents, update],
  );
  const toggleWebSearch = useCallback((on: boolean) => update({ webSearchEnabled: on }), [update]);
  const toggleLocalBrowser = useCallback((on: boolean) => update({ localBrowserEnabled: on }), [update]);
  const setModel = useCallback(
    (id: string) => {
      try {
        if (id) localStorage.setItem(LAST_MODEL_KEY, id);
        else localStorage.removeItem(LAST_MODEL_KEY);
      } catch {
        /* ignore */
      }
      update({ selectedChatModelId: id });
    },
    [update],
  );

  const hydrateSessionState = useCallback(
    (state?: SessionLastRequestState | null) => {
      if (!state) return;
      update({
        ...(typeof state.agent_enabled === "boolean" ? { isAgentEnabled: state.agent_enabled } : {}),
        ...(state.agent_id ? { selectedAgentId: state.agent_id } : {}),
        ...(state.model_id ? { selectedChatModelId: state.model_id } : {}),
        ...(Array.isArray(state.knowledge_base_ids) ? { selectedKnowledgeBases: [...state.knowledge_base_ids] } : {}),
        ...(Array.isArray(state.knowledge_ids) ? { selectedFiles: [...state.knowledge_ids] } : {}),
        ...(Array.isArray(state.mcp_service_ids) ? { selectedMCPServices: [...state.mcp_service_ids] } : {}),
        ...(Array.isArray(state.skill_names) ? { selectedSkills: [...state.skill_names] } : {}),
        ...(typeof state.web_search_enabled === "boolean" ? { webSearchEnabled: state.web_search_enabled } : {}),
        ...(typeof state.local_browser_enabled === "boolean" ? { localBrowserEnabled: state.local_browser_enabled } : {}),
      });
    },
    [update],
  );

  const selectedAgent = useMemo<AgentSummary | null>(() => {
    // Shared agents win on id collision (builtin ids exist in every tenant).
    if (settings.selectedAgentSourceTenantId) {
      const shared = sharedAgents.find(
        (s) => s.agent.id === settings.selectedAgentId && String(s.source_tenant_id) === settings.selectedAgentSourceTenantId,
      );
      if (shared) return { id: shared.agent.id, name: shared.agent.name, description: shared.agent.description, is_builtin: false };
    }
    return agents.find((a) => a.id === settings.selectedAgentId) ?? null;
  }, [agents, sharedAgents, settings.selectedAgentId, settings.selectedAgentSourceTenantId]);

  const isAgentStreamMode = useMemo(() => {
    // Mirrors isAgentStreamAgentId: quick-answer → RAG pipeline, everything else → agent pipeline.
    if (!settings.selectedAgentId || settings.selectedAgentId === BUILTIN_QUICK_ANSWER_ID) return false;
    if (settings.selectedAgentId === BUILTIN_SMART_REASONING_ID) return true;
    return settings.isAgentEnabled;
  }, [settings.selectedAgentId, settings.isAgentEnabled]);

  const kbById = useMemo(() => new Map(knowledgeBases.map((k) => [k.id, k])), [knowledgeBases]);
  const mcpById = useMemo(() => new Map(mcpServices.map((m) => [m.id, m])), [mcpServices]);

  const mentionItems = useMemo<MentionRequestItem[]>(() => {
    const kbs: MentionRequestItem[] = settings.selectedKnowledgeBases.map((id) => {
      const kb = kbById.get(id);
      return {
        id,
        name: kb?.name ?? id,
        type: "kb",
        kb_type: kb?.type === "faq" ? "faq" : "document",
      };
    });
    const files: MentionRequestItem[] = settings.selectedFiles.map((id) => ({
      id,
      name: fileNames[id] ?? "Loading…",
      type: "file",
      kb_id: settings.selectedFileKbMap[id],
      kb_name: settings.selectedFileKbMap[id] ? (kbById.get(settings.selectedFileKbMap[id])?.name ?? "") : "",
    }));
    const tags: MentionRequestItem[] = settings.selectedTags.map((t) => ({
      id: t.id,
      name: t.name,
      type: "tag",
      kb_id: t.kbId,
      kb_name: t.kbName ?? kbById.get(t.kbId)?.name ?? "",
    }));
    const mcps: MentionRequestItem[] = settings.selectedMCPServices
      .map((id) => ({ id, name: mcpById.get(id)?.name ?? id, type: "mcp" as const }))
      .filter((m) => mcpById.has(m.id));
    const skillItems: MentionRequestItem[] = settings.selectedSkills.map((name) => ({
      id: name,
      name,
      type: "skill",
      skill_name: name,
    }));
    return [...kbs, ...files, ...tags, ...mcps, ...skillItems];
  }, [settings, kbById, mcpById, fileNames]);

  const removeMention = useCallback(
    (item: MentionRequestItem) => {
      if (item.type === "kb") removeKnowledgeBase(item.id);
      else if (item.type === "file") removeFile(item.id);
      else if (item.type === "tag") removeTag(item.id, item.kb_id);
      else if (item.type === "mcp") removeMCPService(item.id);
      else if (item.type === "skill") removeSkill(item.skill_name ?? item.id);
    },
    [removeKnowledgeBase, removeFile, removeTag, removeMCPService, removeSkill],
  );

  const value = useMemo<Ctx>(
    () => ({
      settings,
      hydrated,
      update,
      addKnowledgeBase,
      removeKnowledgeBase,
      addFile,
      removeFile,
      addTag,
      removeTag,
      addMCPService,
      removeMCPService,
      addSkill,
      removeSkill,
      selectAgent,
      toggleWebSearch,
      toggleLocalBrowser,
      setModel,
      mentionItems,
      removeMention,
      fileNames,
      agents,
      sharedAgents,
      knowledgeBases,
      models,
      mcpServices,
      skills,
      webSearchReady,
      selectedAgent,
      isAgentStreamMode,
      hydrateSessionState,
      refresh,
    }),
    [
      settings,
      hydrated,
      update,
      addKnowledgeBase,
      removeKnowledgeBase,
      addFile,
      removeFile,
      addTag,
      removeTag,
      addMCPService,
      removeMCPService,
      addSkill,
      removeSkill,
      selectAgent,
      toggleWebSearch,
      toggleLocalBrowser,
      setModel,
      hydrateSessionState,
      mentionItems,
      removeMention,
      fileNames,
      agents,
      sharedAgents,
      knowledgeBases,
      models,
      mcpServices,
      skills,
      webSearchReady,
      selectedAgent,
      isAgentStreamMode,
      refresh,
    ],
  );

  return <ChatContext.Provider value={value}>{children}</ChatContext.Provider>;
}

export function useChatContext(): Ctx {
  const ctx = useContext(ChatContext);
  if (!ctx) throw new Error("useChatContext must be used inside <ChatProvider>");
  return ctx;
}
