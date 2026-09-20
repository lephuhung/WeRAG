"use client";

import { useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { IconPlus, IconSettings } from "@/components/icons";
import {
  listModels,
  createModel,
  deleteModel,
  debugModel,
  type ModelConfig,
} from "@/lib/api/models";

const MODEL_TYPES = [
  { id: "KnowledgeQA", label: "LLM (Chat / QA)" },
  { id: "Embedding", label: "Embedding" },
  { id: "Rerank", label: "Rerank" },
  { id: "VLLM", label: "VLM (Vision)" },
  { id: "ASR", label: "ASR (Speech)" },
] as const;

export function ModelsSettings() {
  const [models, setModels] = useState<ModelConfig[]>([]);
  const [activeType, setActiveType] = useState<string>("KnowledgeQA");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [modalOpen, setModalOpen] = useState(false);
  const [debugResult, setDebugResult] = useState<string | null>(null);
  const [testingId, setTestingId] = useState<string | null>(null);

  const [form, setForm] = useState<{
    name: string;
    type: "KnowledgeQA" | "Embedding" | "Rerank" | "VLLM" | "ASR";
    provider: string;
    base_url: string;
    api_key: string;
  }>({
    name: "",
    type: "KnowledgeQA",
    provider: "openai",
    base_url: "https://api.openai.com/v1",
    api_key: "",
  });

  const load = async () => {
    setBusy(true);
    setError("");
    try {
      const data = await listModels();
      setModels(data);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load models");
    } finally {
      setBusy(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const handleCreate = async () => {
    if (!form.name.trim()) return;
    setBusy(true);
    setError("");
    try {
      await createModel({
        name: form.name.trim(),
        type: form.type,
        source: "remote",
        parameters: {
          provider: form.provider,
          base_url: form.base_url || undefined,
          api_key: form.api_key || undefined,
        },
      });
      setModalOpen(false);
      setForm({
        name: "",
        type: "KnowledgeQA",
        provider: "openai",
        base_url: "https://api.openai.com/v1",
        api_key: "",
      });
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to create model");
    } finally {
      setBusy(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm("Are you sure you want to delete this model?")) return;
    try {
      await deleteModel(id);
      setModels((prev) => prev.filter((m) => m.id !== id));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to delete model");
    }
  };

  const handleTest = async (model: ModelConfig) => {
    if (!model.id) return;
    setTestingId(model.id);
    setDebugResult(null);
    try {
      const res = await debugModel(model.id, { input: "Hello! This is a test query." });
      setDebugResult(res.ok ? "Connection successful! Model responded." : (res.error || "Model test returned false"));
    } catch (e) {
      setDebugResult(e instanceof Error ? e.message : "Connection failed");
    } finally {
      setTestingId(null);
    }
  };

  const filtered = models.filter((m) => m.type === activeType);

  return (
    <div>
      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div>
          <h3 className="title-md text-ink">Models & Providers</h3>
          <p className="body-sm mt-1 text-muted">
            Configure LLMs, embedding models, and rerankers used by your knowledge bases and agents.
          </p>
        </div>
        <button
          type="button"
          className="btn btn-primary btn-sm"
          onClick={() => setModalOpen(true)}
        >
          <IconPlus className="h-4 w-4" /> Add model
        </button>
      </div>

      {error && <div className="card mb-4 p-4 text-error text-xs">{error}</div>}
      {debugResult && (
        <div className="card mb-4 p-4 text-xs font-mono bg-surface-strong">
          <div className="font-semibold text-ink mb-1">Test Result:</div>
          <div className="text-muted">{debugResult}</div>
        </div>
      )}

      {/* Type tabs */}
      <div className="mb-6 flex items-center gap-1 rounded-full bg-surface-strong p-1 w-fit">
        {MODEL_TYPES.map((t) => (
          <button
            key={t.id}
            onClick={() => setActiveType(t.id)}
            className={`rounded-full px-3.5 py-1.5 text-xs font-medium transition-colors ${
              activeType === t.id
                ? "bg-surface-card text-ink shadow-sm"
                : "text-muted hover:text-ink"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* Model Cards */}
      {filtered.length === 0 && !busy ? (
        <div className="card p-8 text-center text-muted text-xs">
          No {MODEL_TYPES.find((t) => t.id === activeType)?.label} models configured yet.
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          {filtered.map((m) => (
            <div key={m.id} className="card p-4 text-left">
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium text-[14px] text-ink">{m.name}</span>
                    {m.is_default && (
                      <span className="rounded bg-primary/10 px-1.5 py-0.5 text-[10px] font-semibold text-primary">
                        Default
                      </span>
                    )}
                  </div>
                  <p className="caption mt-1 text-muted truncate">
                    {m.parameters?.base_url || m.parameters?.provider || "Remote API"}
                  </p>
                </div>
                <div className="flex items-center gap-1 shrink-0">
                  <button
                    type="button"
                    className="btn btn-outline btn-sm px-2"
                    disabled={testingId === m.id}
                    onClick={() => handleTest(m)}
                    title="Test connection"
                  >
                    {testingId === m.id ? "Testing…" : "Test"}
                  </button>
                  <button
                    type="button"
                    className="btn btn-outline btn-sm px-2 text-error hover:bg-error/10"
                    onClick={() => m.id && handleDelete(m.id)}
                    title="Delete"
                  >
                    ✕
                  </button>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Add Model Modal */}
      {modalOpen && (
        <Modal
          open={modalOpen}
          title="Add Model Configuration"
          onClose={() => setModalOpen(false)}
          width="w-[520px]"
        >
          <div className="space-y-4 text-xs">
            <div>
              <label className="block text-muted font-medium mb-1">Model Name / ID *</label>
              <input
                className="input w-full"
                placeholder="e.g. gpt-4o, claude-3-5-sonnet, text-embedding-3-small"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
              />
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-muted font-medium mb-1">Model Type</label>
                <select
                  className="input w-full"
                  value={form.type}
                  onChange={(e) =>
                    setForm({
                      ...form,
                      type: e.target.value as "KnowledgeQA" | "Embedding" | "Rerank" | "VLLM" | "ASR",
                    })
                  }
                >
                  {MODEL_TYPES.map((t) => (
                    <option key={t.id} value={t.id}>
                      {t.label}
                    </option>
                  ))}
                </select>
              </div>

              <div>
                <label className="block text-muted font-medium mb-1">Provider</label>
                <select
                  className="input w-full"
                  value={form.provider}
                  onChange={(e) => setForm({ ...form, provider: e.target.value })}
                >
                  <option value="openai">OpenAI compatible</option>
                  <option value="anthropic">Anthropic</option>
                  <option value="ollama">Ollama</option>
                  <option value="generic">Generic / Other</option>
                </select>
              </div>
            </div>

            <div>
              <label className="block text-muted font-medium mb-1">API Base URL</label>
              <input
                className="input w-full font-mono text-[11px]"
                placeholder="https://api.openai.com/v1"
                value={form.base_url}
                onChange={(e) => setForm({ ...form, base_url: e.target.value })}
              />
            </div>

            <div>
              <label className="block text-muted font-medium mb-1">API Key / Token</label>
              <input
                type="password"
                className="input w-full font-mono text-[11px]"
                placeholder="sk-..."
                value={form.api_key}
                onChange={(e) => setForm({ ...form, api_key: e.target.value })}
              />
            </div>

            <div className="flex justify-end gap-2 pt-3">
              <button
                type="button"
                className="btn btn-outline"
                onClick={() => setModalOpen(false)}
              >
                Cancel
              </button>
              <button
                type="button"
                className="btn btn-primary"
                disabled={!form.name.trim() || busy}
                onClick={() => void handleCreate()}
              >
                {busy ? "Saving…" : "Save Model"}
              </button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
}
