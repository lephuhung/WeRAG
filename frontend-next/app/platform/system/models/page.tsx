"use client";

import { useEffect, useState } from "react";
import {
  createModel,
  deleteModel,
  listModels,
  ModelInUseError,
  updateModel,
  type ModelConfig,
} from "@/lib/api/models";
import { Modal } from "@/components/modal";
import { IconPlus } from "@/components/icons";

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

const TYPE_LABEL: Record<string, string> = {
  KnowledgeQA: "Chat",
  VLLM: "VLM",
  Embedding: "Embedding",
  Rerank: "Rerank",
  ASR: "ASR",
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

export default function SystemModels() {
  const [models, setModels] = useState<UiModel[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [actionError, setActionError] = useState("");
  const [editing, setEditing] = useState<UiModel | null>(null);
  const [modalOpen, setModalOpen] = useState(false);
  const [deleting, setDeleting] = useState<UiModel | null>(null);
  const [form, setForm] = useState(EMPTY_FORM);
  const [saving, setSaving] = useState(false);

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
    setModalOpen(true);
  };
  const openEdit = (m: UiModel) => {
    setEditing(m);
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

  return (
    <div className="mx-auto w-full max-w-[1100px]">
      {loadError && <p className="caption mb-4 text-error">{loadError}</p>}
      {actionError && <p className="caption mb-4 text-error">{actionError}</p>}
      <div className="mb-5 flex items-center justify-between">
        <div className="caption text-muted">
          {loaded ? `${models.length} models configured` : "Loading…"}
        </div>
        <button className="btn btn-primary btn-sm" onClick={openAdd}>
          <IconPlus className="h-3.5 w-3.5" /> Add model
        </button>
      </div>

      <div className="card overflow-hidden">
        {models.map((m, i) => (
          <div
            key={m.id}
            className={`flex items-center gap-4 px-5 py-4 ${i > 0 ? "border-t border-hairline" : ""}`}
          >
            <div className="min-w-0 flex-1">
              <div className="text-[15px] font-medium text-ink">{m.name}</div>
              <div className="caption truncate text-muted">
                {m.provider} · {TYPE_LABEL[m.type] ?? m.type}
              </div>
            </div>
            {m.isDefault && <span className="badge-pill">Default</span>}
            <button className="btn btn-outline btn-sm" onClick={() => openEdit(m)}>
              Edit
            </button>
            <button className="btn btn-outline btn-sm" onClick={() => void toggle(m)}>
              {m.status === "active" ? "Disable" : "Enable"}
            </button>
            <button className="btn btn-outline btn-sm" onClick={() => setDeleting(m)}>
              Delete
            </button>
          </div>
        ))}
        {loaded && models.length === 0 && !loadError && (
          <div className="px-5 py-12 text-center text-[14px] text-muted">
            No models configured yet.
          </div>
        )}
      </div>

      <Modal
        open={modalOpen}
        title={editing ? "Edit model" : "Add model"}
        onClose={() => setModalOpen(false)}
      >
        <label className="mb-4 block">
          <span className="caption mb-1.5 block text-muted">Name</span>
          <input
            className="input"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
        </label>
        <label className="mb-4 block">
          <span className="caption mb-1.5 block text-muted">Provider</span>
          <input
            className="input"
            value={form.provider}
            onChange={(e) => setForm({ ...form, provider: e.target.value })}
          />
        </label>
        <label className="mb-4 block">
          <span className="caption mb-1.5 block text-muted">Base URL</span>
          <input
            className="input"
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
          <button className="btn btn-outline" onClick={() => setModalOpen(false)}>
            Cancel
          </button>
        </div>
      </Modal>

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
