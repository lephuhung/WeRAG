"use client";

import { useCallback, useEffect, useState } from "react";
import { Toggle } from "@/components/settings/toggle";
import { IconRefresh } from "@/components/icons";
import {
  getTenantMemoryConfig,
  updateTenantMemoryConfig,
  type MemoryConfig,
} from "@/lib/api/memory";
import { listModels, type ModelConfig } from "@/lib/api/models";
import { useAuth } from "@/lib/auth";

const DEFAULT_CONFIG: MemoryConfig = {
  enabled: false,
  write_mode: "auto",
  extract_model_id: "",
  max_items: 200,
  extract_delay_seconds: 15,
  extract_min_interval_seconds: 300,
  extract_instructions: "",
  interest_threshold: 3,
  retrieval_conditioning: true,
  embedding_model_id: "",
  vector_recall: false,
};

export function MemoryWorkspaceSettings() {
  const auth = useAuth();
  const activeTenantId = Number(auth.selectedTenantId ?? auth.tenant?.id ?? 0);
  const currentRole =
    auth.memberships.find((m) => String(m.tenant_id) === String(activeTenantId))?.role ?? "";
  const isSystemAdmin = auth.user?.is_system_admin === true;
  const canEdit = currentRole === "owner" || currentRole === "admin" || isSystemAdmin;

  const [config, setConfig] = useState<MemoryConfig>(DEFAULT_CONFIG);
  const [models, setModels] = useState<ModelConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const loadData = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [cfgRes, mdlRes] = await Promise.all([
        getTenantMemoryConfig(),
        listModels().catch(() => []),
      ]);
      if (cfgRes.success && cfgRes.data) {
        setConfig(cfgRes.data);
      }
      setModels(mdlRes);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load workspace memory configuration");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  const handleSave = async (updated?: Partial<MemoryConfig>) => {
    const payload = { ...config, ...(updated || {}) };
    setSaving(true);
    setError("");
    setSuccess("");
    try {
      const res = await updateTenantMemoryConfig(payload);
      if (res.success && res.data) {
        setConfig(res.data);
        setSuccess("Workspace memory settings saved");
      } else {
        setError("Failed to save workspace memory settings");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to save configuration");
    } finally {
      setSaving(false);
    }
  };

  const chatModels = models.filter((m) => m.type === "KnowledgeQA");
  const embeddingModels = models.filter((m) => m.type === "Embedding");

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-hairline pb-5">
        <div>
          <h2 className="title-md font-semibold text-ink">Workspace Memory</h2>
          <p className="caption text-muted mt-1">
            Configure how long-term memory is captured, extracted, and injected for members of this workspace.
          </p>
        </div>

        <div className="flex items-center gap-2">
          {canEdit && (
            <button
              type="button"
              onClick={() => void handleSave()}
              disabled={saving}
              className="btn btn-primary btn-sm"
            >
              {saving ? "Saving…" : "Save changes"}
            </button>
          )}
          <button
            type="button"
            onClick={() => void loadData()}
            className="btn btn-outline btn-sm p-1.5"
            title="Refresh"
          >
            <IconRefresh className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {error && (
        <div className="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/50 dark:bg-rose-950/30 dark:text-rose-400">
          {error}
        </div>
      )}
      {success && (
        <div className="rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700 dark:border-emerald-900/50 dark:bg-emerald-950/30 dark:text-emerald-400">
          {success}
        </div>
      )}

      {/* Main Switch */}
      <div className="flex items-center justify-between rounded-xl border border-hairline bg-surface-card p-5">
        <div>
          <span className="text-sm font-semibold text-ink block">Enable Workspace Memory</span>
          <span className="caption text-muted block mt-0.5">
            When enabled, conversations can extract and retain facts to personalize future interactions.
          </span>
        </div>
        <Toggle
          checked={config.enabled}
          disabled={!canEdit}
          onChange={(checked) => {
            setConfig({ ...config, enabled: checked });
            void handleSave({ enabled: checked });
          }}
        />
      </div>

      {config.enabled && (
        <div className="space-y-6 pt-2">
          {/* Write mode */}
          <div className="rounded-xl border border-hairline p-5 space-y-3">
            <div className="flex items-center justify-between">
              <div>
                <span className="text-sm font-medium text-ink block">Extraction Mode</span>
                <span className="caption text-muted block mt-0.5">
                  Choose whether memories are inferred automatically or only when users explicitly state them.
                </span>
              </div>
              <div className="flex items-center gap-1 rounded-lg border border-hairline p-1 bg-surface-strong/40">
                <button
                  type="button"
                  disabled={!canEdit}
                  onClick={() => setConfig({ ...config, write_mode: "auto" })}
                  className={`px-3 py-1 text-xs font-medium rounded-md transition-colors ${
                    config.write_mode === "auto"
                      ? "bg-white text-ink shadow-sm dark:bg-surface-card"
                      : "text-muted hover:text-ink"
                  }`}
                >
                  Automatic
                </button>
                <button
                  type="button"
                  disabled={!canEdit}
                  onClick={() => setConfig({ ...config, write_mode: "explicit_only" })}
                  className={`px-3 py-1 text-xs font-medium rounded-md transition-colors ${
                    config.write_mode === "explicit_only"
                      ? "bg-white text-ink shadow-sm dark:bg-surface-card"
                      : "text-muted hover:text-ink"
                  }`}
                >
                  Explicit only
                </button>
              </div>
            </div>
          </div>

          {/* Model selection */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="rounded-xl border border-hairline p-5 space-y-2">
              <label className="block">
                <span className="text-sm font-medium text-ink block">Extraction Model</span>
                <span className="caption text-muted block mt-0.5 mb-2">
                  LLM used to summarize and extract facts from conversation turns.
                </span>
                <select
                  disabled={!canEdit}
                  className="input text-sm"
                  value={config.extract_model_id}
                  onChange={(e) => setConfig({ ...config, extract_model_id: e.target.value })}
                >
                  <option value="">Default system model</option>
                  {chatModels.map((m) => (
                    <option key={m.id} value={m.name}>
                      {m.display_name || m.name} ({m.parameters?.provider || "remote"})
                    </option>
                  ))}
                </select>
              </label>
            </div>

            <div className="rounded-xl border border-hairline p-5 space-y-2">
              <label className="block">
                <span className="text-sm font-medium text-ink block">Embedding Model (Vector Recall)</span>
                <span className="caption text-muted block mt-0.5 mb-2">
                  Used when semantic vector matching is enabled for recall.
                </span>
                <select
                  disabled={!canEdit || !config.vector_recall}
                  className="input text-sm"
                  value={config.embedding_model_id}
                  onChange={(e) => setConfig({ ...config, embedding_model_id: e.target.value })}
                >
                  <option value="">None (lexical keyword match only)</option>
                  {embeddingModels.map((m) => (
                    <option key={m.id} value={m.name}>
                      {m.display_name || m.name}
                    </option>
                  ))}
                </select>
              </label>
            </div>
          </div>

          {/* Vector Recall and Retrieval Conditioning */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="flex items-center justify-between rounded-xl border border-hairline p-5">
              <div>
                <span className="text-sm font-medium text-ink block">Vector Recall</span>
                <span className="caption text-muted block mt-0.5">
                  Match memories by semantic meaning, not just exact keywords.
                </span>
              </div>
              <Toggle
                checked={config.vector_recall}
                disabled={!canEdit}
                onChange={(checked) => setConfig({ ...config, vector_recall: checked })}
              />
            </div>

            <div className="flex items-center justify-between rounded-xl border border-hairline p-5">
              <div>
                <span className="text-sm font-medium text-ink block">Retrieval Conditioning</span>
                <span className="caption text-muted block mt-0.5">
                  Allow recalled memory to reshape KB search queries.
                </span>
              </div>
              <Toggle
                checked={config.retrieval_conditioning}
                disabled={!canEdit}
                onChange={(checked) => setConfig({ ...config, retrieval_conditioning: checked })}
              />
            </div>
          </div>

          {/* Timing and Limits */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <div className="rounded-xl border border-hairline p-5 space-y-1.5">
              <label className="block">
                <span className="text-sm font-medium text-ink block">Extraction Delay</span>
                <span className="caption text-muted block text-xs">
                  Seconds to wait after a message before extracting facts.
                </span>
                <input
                  type="number"
                  min={5}
                  max={3600}
                  disabled={!canEdit}
                  className="input mt-2"
                  value={config.extract_delay_seconds}
                  onChange={(e) =>
                    setConfig({ ...config, extract_delay_seconds: Number(e.target.value) })
                  }
                />
              </label>
            </div>

            <div className="rounded-xl border border-hairline p-5 space-y-1.5">
              <label className="block">
                <span className="text-sm font-medium text-ink block">Min Run Interval</span>
                <span className="caption text-muted block text-xs">
                  Floor between two distillation passes for a user (seconds).
                </span>
                <input
                  type="number"
                  min={0}
                  max={86400}
                  disabled={!canEdit}
                  className="input mt-2"
                  value={config.extract_min_interval_seconds}
                  onChange={(e) =>
                    setConfig({ ...config, extract_min_interval_seconds: Number(e.target.value) })
                  }
                />
              </label>
            </div>

            <div className="rounded-xl border border-hairline p-5 space-y-1.5">
              <label className="block">
                <span className="text-sm font-medium text-ink block">Max Items Cap</span>
                <span className="caption text-muted block text-xs">
                  Maximum retained memory items per user.
                </span>
                <input
                  type="number"
                  min={10}
                  max={2000}
                  disabled={!canEdit}
                  className="input mt-2"
                  value={config.max_items}
                  onChange={(e) => setConfig({ ...config, max_items: Number(e.target.value) })}
                />
              </label>
            </div>
          </div>

          {/* Custom instructions */}
          <div className="rounded-xl border border-hairline p-5 space-y-2">
            <label className="block">
              <span className="text-sm font-medium text-ink block">Workspace Extraction Guidelines</span>
              <span className="caption text-muted block text-xs mb-2">
                Custom prompt rules injected into memory distillation (e.g. ignore greeting patterns, focus on tech stacks).
              </span>
              <textarea
                rows={3}
                disabled={!canEdit}
                placeholder="e.g. Always record project identifiers, API version preferences, and tech stacks mentioned."
                className="input resize-none text-sm font-mono"
                value={config.extract_instructions}
                onChange={(e) => setConfig({ ...config, extract_instructions: e.target.value })}
              />
            </label>
          </div>
        </div>
      )}
    </div>
  );
}
