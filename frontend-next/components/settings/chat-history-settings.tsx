"use client";

import { useCallback, useEffect, useState } from "react";
import { Toggle } from "@/components/settings/toggle";
import { IconRefresh } from "@/components/icons";
import { apiGet, apiPut } from "@/lib/api-client";
import { listModels, type ModelConfig } from "@/lib/api/models";
import { Select } from "@/components/select";
import { useAuth } from "@/lib/auth";

export interface ChatHistoryConfig {
  enabled: boolean;
  embedding_model_id?: string;
  knowledge_base_id?: string;
}

export interface ChatHistoryKBStats {
  enabled: boolean;
  knowledge_base_id?: string;
  indexed_message_count?: number;
  has_indexed_messages?: boolean;
}

export function ChatHistorySettings() {
  const auth = useAuth();
  const activeTenantId = Number(auth.selectedTenantId ?? auth.tenant?.id ?? 0);
  const currentRole =
    auth.memberships.find((m) => String(m.tenant_id) === String(activeTenantId))?.role ?? "";
  const isSystemAdmin = auth.user?.is_system_admin === true;
  /* Tenant KV config (PUT /tenants/kv/:key) is Admin+ on the backend,
   * so all Tenant Admins may edit — not just legacy owners. */
  const canEdit = currentRole === "owner" || currentRole === "admin" || isSystemAdmin;

  const [config, setConfig] = useState<ChatHistoryConfig>({ enabled: false });
  const [stats, setStats] = useState<ChatHistoryKBStats | null>(null);
  const [models, setModels] = useState<ModelConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const loadData = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [cfgRes, statsRes, mdlRes] = await Promise.all([
        apiGet<{ data: ChatHistoryConfig }>("/api/v1/tenants/kv/chat-history-config").catch(() => ({
          data: { enabled: false },
        })),
        apiGet<{ data: ChatHistoryKBStats }>("/api/v1/tenants/chat-history-stats").catch(() => ({
          data: { enabled: false },
        })),
        listModels().catch(() => []),
      ]);

      if (cfgRes?.data) setConfig(cfgRes.data);
      if (statsRes?.data) setStats(statsRes.data);
      setModels(mdlRes);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load chat history configuration");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  const handleSave = async (updated: Partial<ChatHistoryConfig>) => {
    const payload = { ...config, ...updated };
    setSaving(true);
    setError("");
    setSuccess("");
    try {
      const res = await apiPut<{ data: ChatHistoryConfig }>(
        "/api/v1/tenants/kv/chat-history-config",
        payload,
      );
      if (res?.data) {
        setConfig(res.data);
        setSuccess("Chat history settings saved");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to save configuration");
    } finally {
      setSaving(false);
    }
  };

  const embeddingModels = models.filter((m) => m.type === "Embedding");
  const modelLocked = stats?.has_indexed_messages === true;

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-hairline pb-5">
        <div>
          <h2 className="title-md font-semibold text-ink">Chat History Indexing</h2>
          <p className="caption text-muted mt-1">
            Automatically index past chat messages into a dedicated knowledge base for recall and semantic search.
          </p>
        </div>

        <button
          type="button"
          onClick={() => void loadData()}
          className="btn btn-outline btn-sm p-1.5"
          title="Refresh"
        >
          <IconRefresh className="h-3.5 w-3.5" />
        </button>
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

      {/* Switch */}
      <div className="flex items-center justify-between rounded-xl border border-hairline bg-surface-card p-5">
        <div>
          <span className="text-sm font-semibold text-ink block">Enable Message Indexing</span>
          <span className="caption text-muted block mt-0.5">
            Store conversations in vector storage to let agents cite previous chats.
          </span>
        </div>
        <Toggle
          checked={config.enabled}
          disabled={!canEdit || saving}
          onChange={(checked) => {
            setConfig({ ...config, enabled: checked });
            void handleSave({ enabled: checked });
          }}
        />
      </div>

      {/* Embedding Model */}
      {config.enabled && (
        <div className="rounded-xl border border-hairline p-5 space-y-3">
          <label className="block">
            <span className="text-sm font-medium text-ink block">Embedding Model</span>
            <span className="caption text-muted block text-xs mt-0.5 mb-2">
              Model used to generate vector embeddings for chat messages.
              {modelLocked && " (Locked because indexed messages already exist)"}
            </span>
            <Select
              disabled={!canEdit || modelLocked}
              className="text-sm"
              value={config.embedding_model_id || ""}
              placeholder="Select embedding model…"
              onChange={(v) => {
                setConfig({ ...config, embedding_model_id: v });
                void handleSave({ embedding_model_id: v });
              }}
              options={embeddingModels.map((m) => ({
                value: m.id ?? "",
                label: m.display_name || m.name,
              }))}
            />
          </label>
        </div>
      )}

      {/* Stats Card */}
      <div className="rounded-xl border border-hairline bg-surface-strong/30 p-5 space-y-2">
        <span className="caption font-medium text-ink block">Indexing Statistics</span>
        <div className="grid grid-cols-2 gap-4 pt-1">
          <div>
            <span className="caption text-muted block text-xs">Indexed Messages</span>
            <span className="font-semibold text-ink text-xl">
              {stats?.indexed_message_count ?? 0}
            </span>
          </div>
          <div>
            <span className="caption text-muted block text-xs">Target Knowledge Base</span>
            <span className="caption font-mono text-muted text-xs truncate block mt-1">
              {stats?.knowledge_base_id || "Auto-managed"}
            </span>
          </div>
        </div>
      </div>
    </div>
  );
}
