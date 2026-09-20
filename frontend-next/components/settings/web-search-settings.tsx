"use client";

import { useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { IconPlus, IconSearch } from "@/components/icons";
import {
  listWebSearchProviders,
  createWebSearchProvider,
  deleteWebSearchProvider,
  testWebSearchProvider,
  type WebSearchProviderEntity,
} from "@/lib/api/web-search";

const SUPPORTED_PROVIDERS = [
  { id: "brave", name: "Brave Search" },
  { id: "tavily", name: "Tavily AI Search" },
  { id: "google", name: "Google Custom Search" },
  { id: "bing", name: "Bing Search" },
  { id: "duckduckgo", name: "DuckDuckGo" },
  { id: "searxng", name: "SearXNG (Self-hosted)" },
] as const;

export function WebSearchSettings() {
  const [providers, setProviders] = useState<WebSearchProviderEntity[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [modalOpen, setModalOpen] = useState(false);
  const [testResult, setTestResult] = useState<string | null>(null);

  const [form, setForm] = useState<{
    name: string;
    provider: WebSearchProviderEntity["provider"];
    api_key: string;
    base_url: string;
    is_default: boolean;
  }>({
    name: "",
    provider: "tavily",
    api_key: "",
    base_url: "",
    is_default: false,
  });

  const load = async () => {
    setBusy(true);
    setError("");
    try {
      const res = await listWebSearchProviders();
      setProviders(res.data ?? []);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load search providers");
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
      await createWebSearchProvider({
        name: form.name.trim(),
        provider: form.provider,
        parameters: {
          api_key: form.api_key || undefined,
          base_url: form.base_url || undefined,
        },
        is_default: form.is_default,
      });
      setModalOpen(false);
      setForm({
        name: "",
        provider: "tavily",
        api_key: "",
        base_url: "",
        is_default: false,
      });
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to add search provider");
    } finally {
      setBusy(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm("Remove this search provider?")) return;
    try {
      await deleteWebSearchProvider(id);
      setProviders((prev) => prev.filter((p) => p.id !== id));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to remove provider");
    }
  };

  const handleTest = async (id: string) => {
    setTestResult(null);
    try {
      await testWebSearchProvider(id);
      setTestResult("Provider connected and responded successfully!");
    } catch (e) {
      setTestResult(e instanceof Error ? e.message : "Search test failed");
    }
  };

  return (
    <div>
      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div>
          <h3 className="title-md text-ink">Web Search Providers</h3>
          <p className="body-sm mt-1 text-muted">
            Configure external search engines for real-time web retrieval during chats and agent execution.
          </p>
        </div>
        <button
          type="button"
          className="btn btn-primary btn-sm"
          onClick={() => setModalOpen(true)}
        >
          <IconPlus className="h-4 w-4" /> Add provider
        </button>
      </div>

      {error && <div className="card mb-4 p-4 text-error text-xs">{error}</div>}
      {testResult && (
        <div className="card mb-4 p-4 text-xs font-mono bg-surface-strong">
          <div className="font-semibold text-ink mb-1">Test Status:</div>
          <div className="text-muted">{testResult}</div>
        </div>
      )}

      {/* List */}
      {providers.length === 0 && !busy ? (
        <div className="card p-8 text-center text-muted text-xs">
          No web search providers configured yet. Click &quot;Add provider&quot; to configure Tavily, Brave, Google, etc.
        </div>
      ) : (
        <div className="space-y-3">
          {providers.map((p) => (
            <div key={p.id} className="card p-4 flex items-center justify-between">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <IconSearch className="h-4 w-4 text-primary" />
                  <span className="font-medium text-sm text-ink">{p.name}</span>
                  <span className="rounded bg-surface-strong px-2 py-0.5 text-[11px] font-mono text-muted uppercase">
                    {p.provider}
                  </span>
                  {p.is_default && (
                    <span className="rounded bg-primary/10 px-1.5 py-0.5 text-[10px] font-semibold text-primary">
                      Default
                    </span>
                  )}
                </div>
                {p.parameters?.base_url && (
                  <p className="caption mt-1 text-muted font-mono">{p.parameters.base_url}</p>
                )}
              </div>
              <div className="flex items-center gap-2 shrink-0">
                {p.id && (
                  <button
                    type="button"
                    className="btn btn-outline btn-sm"
                    onClick={() => handleTest(p.id!)}
                  >
                    Test
                  </button>
                )}
                {p.id && (
                  <button
                    type="button"
                    className="btn btn-outline btn-sm text-error hover:bg-error/10 px-2"
                    onClick={() => handleDelete(p.id!)}
                  >
                    ✕
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Add Modal */}
      {modalOpen && (
        <Modal
          open={modalOpen}
          title="Add Search Provider"
          onClose={() => setModalOpen(false)}
          width="w-[480px]"
        >
          <div className="space-y-4 text-xs">
            <div>
              <label className="block text-muted font-medium mb-1">Display Name *</label>
              <input
                className="input w-full"
                placeholder="e.g. My Tavily Search"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
              />
            </div>

            <div>
              <label className="block text-muted font-medium mb-1">Provider Engine</label>
              <select
                className="input w-full"
                value={form.provider}
                onChange={(e) =>
                  setForm({ ...form, provider: e.target.value as WebSearchProviderEntity["provider"] })
                }
              >
                {SUPPORTED_PROVIDERS.map((sp) => (
                  <option key={sp.id} value={sp.id}>
                    {sp.name}
                  </option>
                ))}
              </select>
            </div>

            <div>
              <label className="block text-muted font-medium mb-1">API Key / Token</label>
              <input
                type="password"
                className="input w-full font-mono text-[11px]"
                placeholder="tvly-... or key"
                value={form.api_key}
                onChange={(e) => setForm({ ...form, api_key: e.target.value })}
              />
            </div>

            {form.provider === "searxng" && (
              <div>
                <label className="block text-muted font-medium mb-1">Base URL (SearXNG)</label>
                <input
                  className="input w-full font-mono text-[11px]"
                  placeholder="http://localhost:8080"
                  value={form.base_url}
                  onChange={(e) => setForm({ ...form, base_url: e.target.value })}
                />
              </div>
            )}

            <div className="flex items-center gap-2 pt-1">
              <input
                type="checkbox"
                id="default_search"
                checked={form.is_default}
                onChange={(e) => setForm({ ...form, is_default: e.target.checked })}
              />
              <label htmlFor="default_search" className="text-ink cursor-pointer select-none">
                Set as default search provider for workspace
              </label>
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
                {busy ? "Saving…" : "Add Provider"}
              </button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
}
