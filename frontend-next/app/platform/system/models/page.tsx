"use client";

import { Suspense, useEffect, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  createModel,
  debugModel,
  deleteModel,
  listModels,
  ModelInUseError,
  updateModel,
  type ModelConfig,
} from "@/lib/api/models";
import { Modal } from "@/components/modal";
import {
  IconEdit,
  IconPlus,
  IconPower,
  IconPulse,
  IconStorageEngine,
  IconTrash,
} from "@/components/icons";
import { Select } from "@/components/select";
import { SectionCardGrid, type SectionCard } from "@/components/system/section-cards";
import { OllamaSettings } from "@/components/settings/ollama-settings";
import { WeKnoraCloudSettings } from "@/components/settings/weknora-cloud-settings";

const EMPTY_FORM = { name: "", provider: "", type: "KnowledgeQA", baseUrl: "", apiKey: "" };

type UiModel = {
  id: string;
  name: string;
  provider: string;
  type: string;
  status: string;
  isDefault: boolean;
  raw: ModelConfig;
};

/* Type Logos */
const IconModelChat = ({ className }: { className?: string }) => (
  <svg
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.8}
    strokeLinecap="round"
    strokeLinejoin="round"
    className={className ?? "h-5 w-5"}
  >
    <path d="M14 9a2 2 0 0 1-2 2H6l-4 4V4a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v5Z" />
    <path d="M18 9h2a2 2 0 0 1 2 2v7l-4-4h-4a2 2 0 0 1-2-2v-1" />
  </svg>
);

const IconModelEmbedding = ({ className }: { className?: string }) => (
  <svg
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.8}
    strokeLinecap="round"
    strokeLinejoin="round"
    className={className ?? "h-5 w-5"}
  >
    <path d="m12 3-10 5.5 10 5.5 10-5.5L12 3Z" />
    <path d="m2 14 10 5.5 10-5.5" />
    <path d="m2 9.5 10 5.5 10-5.5" />
  </svg>
);

const IconModelRerank = ({ className }: { className?: string }) => (
  <svg
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.8}
    strokeLinecap="round"
    strokeLinejoin="round"
    className={className ?? "h-5 w-5"}
  >
    <path d="m3 8 4-4 4 4" />
    <path d="M7 4v16" />
    <path d="M15 5h6" />
    <path d="M15 10h4" />
    <path d="M15 15h5" />
    <path d="M15 20h3" />
  </svg>
);

const IconModelVLM = ({ className }: { className?: string }) => (
  <svg
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.8}
    strokeLinecap="round"
    strokeLinejoin="round"
    className={className ?? "h-5 w-5"}
  >
    <rect width="18" height="18" x="3" y="3" rx="4" />
    <circle cx="8.5" cy="8.5" r="1.5" />
    <path d="m21 15-5-5L5 21" />
  </svg>
);

const IconModelASR = ({ className }: { className?: string }) => (
  <svg
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.8}
    strokeLinecap="round"
    strokeLinejoin="round"
    className={className ?? "h-5 w-5"}
  >
    <path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z" />
    <path d="M19 10v2a7 7 0 0 1-14 0v-2" />
    <line x1="12" x2="12" y1="19" y2="22" />
  </svg>
);

const IconModelDefault = ({ className }: { className?: string }) => (
  <svg
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.8}
    strokeLinecap="round"
    strokeLinejoin="round"
    className={className ?? "h-5 w-5"}
  >
    <rect width="16" height="16" x="4" y="4" rx="2" />
    <rect width="6" height="6" x="9" y="9" rx="1" />
    <path d="M9 1v3M15 1v3M9 20v3M15 20v3M20 9h3M20 14h3M1 9h3M1 14h3" />
  </svg>
);

type TypeConfig = {
  name: string;
  sub: string;
  icon: (props: { className?: string }) => React.ReactNode;
  color: string;
  bg: string;
  border: string;
  badge: string;
};

const TYPE_CONFIG: Record<string, TypeConfig> = {
  KnowledgeQA: {
    name: "Chat / LLM",
    sub: "Text generation & QA",
    icon: IconModelChat,
    color: "text-blue-600 dark:text-blue-400",
    bg: "bg-blue-50 dark:bg-blue-950/40",
    border: "border-blue-200 dark:border-blue-900/60",
    badge: "bg-blue-500/10 text-blue-600 dark:text-blue-400 border border-blue-500/20",
  },
  Embedding: {
    name: "Embedding",
    sub: "Vector semantic search",
    icon: IconModelEmbedding,
    color: "text-purple-600 dark:text-purple-400",
    bg: "bg-purple-50 dark:bg-purple-950/40",
    border: "border-purple-200 dark:border-purple-900/60",
    badge: "bg-purple-500/10 text-purple-600 dark:text-purple-400 border border-purple-500/20",
  },
  Rerank: {
    name: "Rerank",
    sub: "Score & order reranking",
    icon: IconModelRerank,
    color: "text-amber-600 dark:text-amber-400",
    bg: "bg-amber-50 dark:bg-amber-950/40",
    border: "border-amber-200 dark:border-amber-900/60",
    badge: "bg-amber-500/10 text-amber-600 dark:text-amber-400 border border-amber-500/20",
  },
  VLLM: {
    name: "VLM",
    sub: "Vision & OCR multimodal",
    icon: IconModelVLM,
    color: "text-emerald-600 dark:text-emerald-400",
    bg: "bg-emerald-50 dark:bg-emerald-950/40",
    border: "border-emerald-200 dark:border-emerald-900/60",
    badge: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border border-emerald-500/20",
  },
  ASR: {
    name: "ASR",
    sub: "Speech recognition",
    icon: IconModelASR,
    color: "text-rose-600 dark:text-rose-400",
    bg: "bg-rose-50 dark:bg-rose-950/40",
    border: "border-rose-200 dark:border-rose-900/60",
    badge: "bg-rose-500/10 text-rose-600 dark:text-rose-400 border border-rose-500/20",
  },
};

const DEFAULT_TYPE_CONFIG: TypeConfig = {
  name: "Model",
  sub: "General AI model",
  icon: IconModelDefault,
  color: "text-stone-600 dark:text-stone-400",
  bg: "bg-surface-strong",
  border: "border-hairline",
  badge: "bg-surface-strong text-muted border border-hairline",
};

function toUi(m: ModelConfig, i: number): UiModel {
  return {
    id: m.id ?? `m${i}`,
    name: m.display_name || m.name,
    provider: m.parameters?.provider ?? m.source ?? "",
    type: m.type,
    status: m.status ?? "active",
    isDefault: m.is_default === true,
    raw: m,
  };
}

export default function SystemModelsPage() {
  return (
    <Suspense fallback={null}>
      <SystemModels />
    </Suspense>
  );
}

/* ?tab=ollama / ?tab=weknoracloud were fake type filters in the old layout —
 * they are real sub-routes now. Other ?tab= values still select a type filter. */
const PROVIDER_ROUTES: Record<string, string> = {
  ollama: "/platform/system/models/ollama",
  weknoracloud: "/platform/system/models/weknoracloud",
};

/* Provider backends — compact cards opening their config in a modal. The
 * same panels are reachable at /platform/system/models/{ollama,
 * weknoracloud} for deep-linking and palette search. */
const providerCards: SectionCard[] = [
  {
    key: "ollama",
    title: "Ollama (local)",
    desc: "Run and manage local models via an Ollama server.",
    icon: <IconStorageEngine className="h-5 w-5" />,
    content: <OllamaSettings />,
  },
  {
    key: "weknoracloud",
    title: "WeRAG Cloud",
    desc: "Hosted model provider — credentials and quotas.",
    icon: <IconPulse className="h-5 w-5" />,
    content: <WeKnoraCloudSettings />,
  },
];

function SystemModels() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const tabParam = searchParams.get("tab");
  const initialTab = tabParam && !PROVIDER_ROUTES[tabParam] ? tabParam : "all";

  const [models, setModels] = useState<UiModel[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [actionError, setActionError] = useState("");
  const [editing, setEditing] = useState<UiModel | null>(null);
  const [modalOpen, setModalOpen] = useState(false);
  const [deleting, setDeleting] = useState<UiModel | null>(null);
  const [form, setForm] = useState(EMPTY_FORM);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testMsg, setTestMsg] = useState<string | null>(null);
  const [filterType, setFilterType] = useState<string>(initialTab);

  useEffect(() => {
    if (!tabParam) return;
    const target = PROVIDER_ROUTES[tabParam];
    if (target) {
      router.replace(target);
    } else {
      setFilterType(tabParam);
    }
  }, [tabParam, router]);

  const load = async () => {
    try {
      const rows = await listModels();
      setModels(rows.map(toUi));
      setLoadError("");
    } catch (e) {
      setLoadError(e instanceof Error ? e.message : "Failed to load models");
    } finally {
      setLoaded(true);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const openAdd = () => {
    setEditing(null);
    setForm(EMPTY_FORM);
    setActionError("");
    setTestMsg(null);
    setModalOpen(true);
  };

  const openEdit = (m: UiModel) => {
    setEditing(m);
    setTestMsg(null);
    setForm({
      name: m.name,
      provider: m.provider,
      type: m.type,
      baseUrl: m.raw.parameters?.base_url ?? "",
      apiKey: "",
    });
    setActionError("");
    setModalOpen(true);
  };

  const save = async () => {
    if (!form.name.trim() || saving) return;
    setSaving(true);
    setActionError("");
    try {
      if (editing) {
        await updateModel(editing.id, {
          display_name: form.name.trim(),
          parameters: {
            ...editing.raw.parameters,
            provider: form.provider.trim() || editing.raw.parameters?.provider,
            base_url: form.baseUrl || editing.raw.parameters?.base_url,
            ...(form.apiKey ? { api_key: form.apiKey } : {}),
          },
        });
      } else {
        await createModel({
          name: form.name.trim(),
          type: form.type as ModelConfig["type"],
          source: "remote",
          parameters: {
            provider: form.provider.trim() || undefined,
            base_url: form.baseUrl || undefined,
            api_key: form.apiKey || undefined,
          },
        });
      }
      setModalOpen(false);
      await load();
    } catch (e) {
      setActionError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSaving(false);
    }
  };

  const test = async () => {
    if (!editing?.id || testing) return;
    setTesting(true);
    setTestMsg(null);
    try {
      const res = await debugModel(editing.id, { input: "Hello! This is a test query." });
      setTestMsg(res.ok ? "Connection successful — model responded." : res.error || "Test failed");
    } catch (e) {
      setTestMsg(e instanceof Error ? e.message : "Test failed");
    } finally {
      setTesting(false);
    }
  };

  const toggle = async (m: UiModel) => {
    const next = m.status === "active" ? "disabled" : "active";
    try {
      await updateModel(m.id, { status: next });
      await load();
    } catch (e) {
      setActionError(e instanceof Error ? e.message : "Update failed");
    }
  };

  const remove = async () => {
    if (!deleting) return;
    setActionError("");
    try {
      await deleteModel(deleting.id);
      setDeleting(null);
      await load();
    } catch (e) {
      setActionError(
        e instanceof ModelInUseError
          ? "Model is still in use — remove its bindings first."
          : e instanceof Error
            ? e.message
            : "Delete failed",
      );
      setDeleting(null);
    }
  };

  // Group counts for filters
  const typeCounts = useMemo(() => {
    const counts: Record<string, number> = { all: models.length };
    for (const m of models) {
      counts[m.type] = (counts[m.type] ?? 0) + 1;
    }
    return counts;
  }, [models]);

  const filteredModels = useMemo(() => {
    if (filterType === "all") return models;
    return models.filter((m) => m.type === filterType);
  }, [models, filterType]);

  const filterOptions = [
    { id: "all", label: "All" },
    { id: "KnowledgeQA", label: "Chat" },
    { id: "Embedding", label: "Embedding" },
    { id: "Rerank", label: "Rerank" },
    { id: "VLLM", label: "VLM" },
    { id: "ASR", label: "ASR" },
  ];

  return (
    <div className="mx-auto w-full max-w-[1200px]">
      {loadError && <p className="caption mb-4 text-error">{loadError}</p>}
      {actionError && <p className="caption mb-4 text-error">{actionError}</p>}

      {/* Provider backends */}
      <div className="caption-uppercase mb-3 text-muted-soft">Providers</div>
      <div className="mb-8">
        <SectionCardGrid cards={providerCards} />
      </div>

      {/* Header bar */}
      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 className="title-md font-semibold text-ink">Models & Providers</h2>
          <p className="caption text-muted">
            {loaded ? `${models.length} models configured` : "Loading models…"}
          </p>
        </div>

        <button className="btn btn-primary btn-sm" onClick={openAdd}>
          <IconPlus className="h-3.5 w-3.5" /> Add model
        </button>
      </div>

      {/* Type filter tabs */}
      <div className="mb-6 flex flex-wrap items-center gap-1.5 border-b border-hairline pb-3">
        {filterOptions.map((opt) => {
          const count = typeCounts[opt.id];
          const isSelected = filterType === opt.id;
          return (
            <button
              key={opt.id}
              onClick={() => setFilterType(opt.id)}
              className={`flex items-center gap-1.5 rounded-full px-3 py-1 text-[12.5px] font-medium transition-colors ${
                isSelected
                  ? "bg-ink text-white dark:bg-white dark:text-ink"
                  : "text-muted hover:bg-surface-strong hover:text-ink"
              }`}
            >
              <span>{opt.label}</span>
              {count !== undefined && (
                <span
                  className={`rounded-full px-1.5 py-0.2 text-[10.5px] ${
                    isSelected
                      ? "bg-white/20 text-white dark:bg-black/20 dark:text-ink"
                      : "bg-surface-strong text-muted"
                  }`}
                >
                  {count}
                </span>
              )}
            </button>
          );
        })}
      </div>

      {/* Card Grid */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {filteredModels.map((m) => {
            const config = TYPE_CONFIG[m.type] ?? DEFAULT_TYPE_CONFIG;
            const Icon = config.icon;
            const isActive = m.status === "active";

            return (
              <div
              key={m.id}
              className={`card card-hover group flex min-w-0 flex-col p-5 transition-all duration-150 ${
                !isActive ? "opacity-75" : ""
              }`}
            >
              {/* Top row: Type Logo + Badge (Left) & Actions (Right) */}
              <div className="flex items-start justify-between gap-3 mb-3.5">
                <div className="flex items-center gap-3 min-w-0">
                  {/* Type Logo */}
                  <div
                    className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-xl border ${config.bg} ${config.border} ${config.color}`}
                    title={`${config.name} (${config.sub})`}
                  >
                    <Icon className="h-5 w-5" />
                  </div>

                  <div className="min-w-0">
                    <div className="flex items-center gap-1.5 flex-wrap">
                      <span
                        className={`rounded-md px-2 py-0.5 text-[11px] font-semibold uppercase tracking-wider ${config.badge}`}
                      >
                        {config.name}
                      </span>
                      {m.isDefault && (
                        <span className="rounded-md bg-ink px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-white dark:bg-white dark:text-ink">
                          DEFAULT
                        </span>
                      )}
                    </div>
                    <div className="text-[11.5px] text-muted-soft truncate mt-0.5">
                      {config.sub}
                    </div>
                  </div>
                </div>

                {/* Actions */}
                <div className="flex items-center gap-1 text-muted shrink-0">
                  <button
                    type="button"
                    className="flex h-7 w-7 items-center justify-center rounded-lg text-muted transition-colors hover:bg-surface-strong hover:text-ink"
                    title="Edit model"
                    aria-label="Edit model"
                    onClick={() => openEdit(m)}
                  >
                    <IconEdit className="h-3.5 w-3.5" />
                  </button>
                  <button
                    type="button"
                    className={`flex h-7 w-7 items-center justify-center rounded-lg transition-colors hover:bg-surface-strong ${
                      isActive
                        ? "text-emerald-600 hover:text-amber-600"
                        : "text-muted hover:text-emerald-600"
                    }`}
                    title={isActive ? "Disable model" : "Enable model"}
                    aria-label={isActive ? "Disable model" : "Enable model"}
                    onClick={() => void toggle(m)}
                  >
                    <IconPower className="h-3.5 w-3.5" />
                  </button>
                  <button
                    type="button"
                    className="flex h-7 w-7 items-center justify-center rounded-lg text-muted transition-colors hover:bg-surface-strong hover:text-error"
                    title="Delete model"
                    aria-label="Delete model"
                    onClick={() => setDeleting(m)}
                  >
                    <IconTrash className="h-3.5 w-3.5" />
                  </button>
                </div>
              </div>

              {/* Model Name & Endpoint */}
              <div className="min-w-0 flex-1">
                <h3
                  className="text-[15px] font-semibold text-ink truncate leading-tight"
                  title={m.name}
                >
                  {m.name}
                </h3>
                {m.raw.parameters?.base_url ? (
                  <p
                    className="caption text-muted-soft truncate mt-1 font-mono text-[11px]"
                    title={m.raw.parameters.base_url}
                  >
                    {m.raw.parameters.base_url}
                  </p>
                ) : (
                  <p className="caption text-muted-soft truncate mt-1 text-[11.5px]">
                    Remote API endpoint
                  </p>
                )}
              </div>

              {/* Footer: Provider and Status */}
              <div className="mt-4 flex items-center justify-between gap-2 border-t border-hairline pt-3 text-xs">
                <div className="flex items-center gap-1.5 min-w-0">
                  <span className="text-muted text-[11.5px]">Provider:</span>
                  <span className="badge-pill truncate max-w-[130px] font-medium">
                    {m.provider || "generic"}
                  </span>
                </div>

                <div className="flex items-center gap-1.5 shrink-0">
                  <span
                    className={`h-2 w-2 rounded-full ${
                      isActive ? "bg-emerald-500" : "bg-stone-300 dark:bg-stone-600"
                    }`}
                  />
                  <span className="text-[11.5px] font-medium text-muted">
                    {isActive ? "Active" : "Disabled"}
                  </span>
                </div>
              </div>
            </div>
          );
        })}
      </div>

      {loaded && filteredModels.length === 0 && !loadError && (
        <div className="card py-16 text-center text-[14px] text-muted">
          No models found matching the selected filter.
        </div>
      )}

      {/* Add / Edit Modal */}
      <Modal
        open={modalOpen}
        title={editing ? "Edit model" : "Add model"}
        onClose={() => setModalOpen(false)}
      >
        <label className="mb-4 block">
          <span className="caption mb-1.5 block text-muted">Name</span>
          <input
            className="input"
            placeholder="e.g. Qwen/Qwen3.6-35B-A3B-FP8"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
        </label>
        <label className="mb-4 block">
          <span className="caption mb-1.5 block text-muted">Model Type</span>
          <Select
            value={form.type}
            disabled={!!editing}
            onChange={(v) => setForm({ ...form, type: v })}
            options={[
              { value: "KnowledgeQA", label: "Chat / LLM (KnowledgeQA)" },
              { value: "Embedding", label: "Embedding (Vector search)" },
              { value: "Rerank", label: "Rerank (Score reordering)" },
              { value: "VLLM", label: "VLM (Vision-Language Model)" },
              { value: "ASR", label: "ASR (Speech recognition)" },
            ]}
          />
        </label>
        <label className="mb-4 block">
          <span className="caption mb-1.5 block text-muted">Provider</span>
          <input
            className="input"
            placeholder="e.g. openai, generic, ollama"
            value={form.provider}
            onChange={(e) => setForm({ ...form, provider: e.target.value })}
          />
        </label>
        <label className="mb-4 block">
          <span className="caption mb-1.5 block text-muted">Base URL</span>
          <input
            className="input"
            placeholder="https://api.openai.com/v1"
            value={form.baseUrl}
            onChange={(e) => setForm({ ...form, baseUrl: e.target.value })}
          />
        </label>
        <label className="mb-4 block">
          <span className="caption mb-1.5 block text-muted">
            API key{editing ? " (leave empty to keep current)" : ""}
          </span>
          <input
            className="input"
            type="password"
            value={form.apiKey}
            onChange={(e) => setForm({ ...form, apiKey: e.target.value })}
          />
        </label>
        <div className="flex gap-3">
          <button className="btn btn-primary" onClick={() => void save()} disabled={saving}>
            {saving ? "Saving…" : "Save"}
          </button>
          {editing && (
            <button className="btn btn-outline" onClick={() => void test()} disabled={testing}>
              {testing ? "Testing…" : "Test connection"}
            </button>
          )}
          <button className="btn btn-outline" onClick={() => setModalOpen(false)}>
            Cancel
          </button>
        </div>
        {testMsg && <p className="caption mt-3 text-muted">{testMsg}</p>}
      </Modal>

      {/* Delete Confirmation Modal */}
      <Modal open={deleting !== null} title="Delete model" onClose={() => setDeleting(null)}>
        <p className="body-sm mb-6 text-body">Delete {deleting?.name}?</p>
        <div className="flex gap-3">
          <button className="btn btn-primary" onClick={() => void remove()}>
            Delete
          </button>
          <button className="btn btn-outline" onClick={() => setDeleting(null)}>
            Cancel
          </button>
        </div>
      </Modal>
    </div>
  );
}
