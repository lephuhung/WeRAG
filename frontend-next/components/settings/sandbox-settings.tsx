"use client";

import { useCallback, useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { IconEdit, IconPlus, IconRefresh, IconTrash } from "@/components/icons";
import { Toggle } from "@/components/settings/toggle";
import {
  listSandboxConfigs,
  setSandboxWorkspacePolicy,
  createSandboxConfig,
  updateSandboxConfigById,
  deleteSandboxConfig,
  getSandboxConfigInventory,
  type SandboxConfigRecord,
  type SandboxConfigData,
} from "@/lib/api/sandbox";
import { useAuth } from "@/lib/auth";

export function SandboxSettings() {
  const auth = useAuth();
  const activeTenantId = Number(auth.selectedTenantId ?? auth.tenant?.id ?? 0);
  const currentRole =
    auth.memberships.find((m) => String(m.tenant_id) === String(activeTenantId))?.role ?? "";
  const isSystemAdmin = auth.user?.is_system_admin === true;
  const canManage = currentRole === "owner" || currentRole === "admin" || isSystemAdmin;

  const [configs, setConfigs] = useState<SandboxConfigRecord[]>([]);
  const [scriptsDisabled, setScriptsDisabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const [filterType, setFilterType] = useState<string>("all");

  // Create / Edit Modal
  const [modalOpen, setModalOpen] = useState(false);
  const [editingConfig, setEditingConfig] = useState<SandboxConfigRecord | null>(null);
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [sandboxType, setSandboxType] = useState<"docker" | "cube" | "e2b">("docker");
  const [endpoint, setEndpoint] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [saving, setSaving] = useState(false);

  // Inventory Modal
  const [inventoryConfig, setInventoryConfig] = useState<SandboxConfigRecord | null>(null);
  const [inventoryCount, setInventoryCount] = useState<number | null>(null);
  const [inventorySessions, setInventorySessions] = useState<string[]>([]);
  const [loadingInventory, setLoadingInventory] = useState(false);

  // Delete Modal
  const [deletingConfig, setDeletingConfig] = useState<SandboxConfigRecord | null>(null);
  const [deletingBusy, setDeletingBusy] = useState(false);

  const loadData = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await listSandboxConfigs();
      setConfigs(res.data ?? []);
      setScriptsDisabled(res.workspace_scripts_disabled === true);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load sandbox configurations");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  // Toggle script execution policy
  const handleTogglePolicy = async (disabled: boolean) => {
    setError("");
    try {
      const res = await setSandboxWorkspacePolicy(disabled);
      setScriptsDisabled(res.workspace_scripts_disabled);
      setSuccess(`Workspace script execution ${disabled ? "disabled" : "enabled"}`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to update workspace script policy");
    }
  };

  // Open Create
  const openCreate = () => {
    setEditingConfig(null);
    setName("");
    setDesc("");
    setSandboxType("docker");
    setEndpoint("unix:///var/run/docker.sock");
    setApiKey("");
    setModalOpen(true);
  };

  // Open Edit
  const openEdit = (cfg: SandboxConfigRecord) => {
    setEditingConfig(cfg);
    setName(cfg.name);
    setDesc(cfg.description || "");
    const st = (cfg.sandbox_type as "docker" | "cube" | "e2b") || "docker";
    setSandboxType(st);
    if (st === "docker") {
      setEndpoint(cfg.config?.docker?.endpoint || "unix:///var/run/docker.sock");
      setApiKey("");
    } else if (st === "cube") {
      setEndpoint(cfg.config?.cube?.endpoint || "");
      setApiKey("");
    } else if (st === "e2b") {
      setEndpoint("");
      setApiKey("");
    }
    setModalOpen(true);
  };

  // Save Config
  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    setSaving(true);
    setError("");
    try {
      const configData: SandboxConfigData = { type: sandboxType };
      if (sandboxType === "docker") {
        configData.docker = { endpoint: endpoint.trim() || undefined };
      } else if (sandboxType === "cube") {
        configData.cube = {
          endpoint: endpoint.trim() || undefined,
          ...(apiKey.trim() ? { api_key: apiKey.trim() } : {}),
        };
      } else if (sandboxType === "e2b") {
        configData.e2b = {
          ...(apiKey.trim() ? { api_key: apiKey.trim() } : {}),
        };
      }

      if (editingConfig) {
        await updateSandboxConfigById(editingConfig.id, {
          name: name.trim(),
          description: desc.trim() || undefined,
          config: configData,
        });
        setSuccess(`Updated sandbox config ${name}`);
      } else {
        await createSandboxConfig({
          name: name.trim(),
          description: desc.trim() || undefined,
          config: configData,
        });
        setSuccess(`Created sandbox config ${name}`);
      }
      setModalOpen(false);
      await loadData();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to save sandbox configuration");
    } finally {
      setSaving(false);
    }
  };

  // Open Inventory
  const openInventory = async (cfg: SandboxConfigRecord) => {
    setInventoryConfig(cfg);
    setLoadingInventory(true);
    setInventoryCount(null);
    setInventorySessions([]);
    try {
      const res = await getSandboxConfigInventory(cfg.id);
      if (res.data) {
        setInventoryCount(res.data.sandbox_count ?? 0);
        setInventorySessions(res.data.session_ids ?? []);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load active sandboxes");
    } finally {
      setLoadingInventory(false);
    }
  };

  // Delete Config
  const handleDelete = async () => {
    if (!deletingConfig) return;
    setDeletingBusy(true);
    setError("");
    try {
      await deleteSandboxConfig(deletingConfig.id);
      setSuccess(`Deleted sandbox configuration ${deletingConfig.name}`);
      setDeletingConfig(null);
      await loadData();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to delete sandbox configuration");
    } finally {
      setDeletingBusy(false);
    }
  };

  const filteredConfigs = configs.filter(
    (c) => filterType === "all" || c.sandbox_type === filterType,
  );

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-hairline pb-5">
        <div>
          <h2 className="title-md font-semibold text-ink">Sandbox Environments</h2>
          <p className="caption text-muted mt-1">
            Isolated runtime instances where agents safely execute code, tools, and custom skills.
          </p>
        </div>

        <div className="flex items-center gap-2">
          {canManage && (
            <button
              type="button"
              onClick={openCreate}
              className="btn btn-primary btn-sm flex items-center gap-1.5"
            >
              <IconPlus className="h-3.5 w-3.5" />
              <span>Add sandbox</span>
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

      {/* Kill Switch policy */}
      <div className="flex items-center justify-between rounded-xl border border-hairline bg-surface-card p-5">
        <div>
          <span className="text-sm font-semibold text-ink block">
            Workspace Code Execution Policy
          </span>
          <span className="caption text-muted block mt-0.5">
            When disabled, all agent code interpreter and script execution requests will be blocked.
          </span>
        </div>
        <Toggle
          checked={!scriptsDisabled}
          disabled={!canManage}
          onChange={(checked) => void handleTogglePolicy(!checked)}
        />
      </div>

      {/* Filters */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1.5">
          {["all", "docker", "cube", "e2b"].map((t) => (
            <button
              key={t}
              type="button"
              onClick={() => setFilterType(t)}
              className={`rounded-full px-3 py-1 text-xs font-medium capitalize transition-colors ${
                filterType === t
                  ? "bg-ink text-white dark:bg-white dark:text-ink"
                  : "text-muted hover:bg-surface-strong hover:text-ink"
              }`}
            >
              {t}
            </button>
          ))}
        </div>
      </div>

      {/* Configs Table */}
      <div className="overflow-hidden rounded-xl border border-hairline">
        <table className="w-full text-left border-collapse text-sm">
          <thead>
            <tr className="border-b border-hairline bg-surface-strong/50 text-xs font-semibold text-muted">
              <th className="py-3 px-4">Name</th>
              <th className="py-3 px-4">Backend Type</th>
              <th className="py-3 px-4">Endpoint / Target</th>
              <th className="py-3 px-4">Created</th>
              <th className="py-3 px-4 text-right">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-hairline">
            {loading ? (
              <tr>
                <td colSpan={5} className="py-12 text-center text-muted">
                  Loading sandbox configurations…
                </td>
              </tr>
            ) : filteredConfigs.length === 0 ? (
              <tr>
                <td colSpan={5} className="py-12 text-center text-muted">
                  No sandbox configurations found. Add one to enable code execution for agents.
                </td>
              </tr>
            ) : (
              filteredConfigs.map((cfg) => {
                const ep =
                  cfg.config?.docker?.endpoint ||
                  cfg.config?.cube?.endpoint ||
                  cfg.config?.e2b?.template ||
                  "Default";

                return (
                  <tr key={cfg.id} className="hover:bg-surface-strong/30 transition-colors">
                    <td className="py-3.5 px-4">
                      <span className="font-medium text-ink block">{cfg.name}</span>
                      {cfg.description && (
                        <span className="caption text-muted truncate block text-xs">
                          {cfg.description}
                        </span>
                      )}
                    </td>
                    <td className="py-3.5 px-4">
                      <span className="badge-pill uppercase text-[10.5px] font-semibold">
                        {cfg.sandbox_type}
                      </span>
                    </td>
                    <td className="py-3.5 px-4 text-xs font-mono text-muted max-w-xs truncate">
                      {ep}
                    </td>
                    <td className="py-3.5 px-4 text-xs text-muted">
                      {cfg.created_at ? new Date(cfg.created_at).toLocaleDateString() : "—"}
                    </td>
                    <td className="py-3.5 px-4 text-right space-x-1">
                      <button
                        type="button"
                        onClick={() => void openInventory(cfg)}
                        className="btn btn-outline btn-sm text-xs py-1"
                      >
                        Status
                      </button>
                      {canManage && (
                        <>
                          <button
                            type="button"
                            onClick={() => openEdit(cfg)}
                            className="btn btn-ghost btn-sm p-1.5 text-muted hover:text-ink"
                            title="Edit"
                          >
                            <IconEdit className="h-3.5 w-3.5" />
                          </button>
                          <button
                            type="button"
                            onClick={() => setDeletingConfig(cfg)}
                            className="btn btn-ghost btn-sm p-1.5 text-muted hover:text-rose-600"
                            title="Delete"
                          >
                            <IconTrash className="h-3.5 w-3.5" />
                          </button>
                        </>
                      )}
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>

      {/* Create / Edit Modal */}
      <Modal
        open={modalOpen}
        title={editingConfig ? "Edit Sandbox Environment" : "Add Sandbox Environment"}
        onClose={() => setModalOpen(false)}
      >
        <form onSubmit={handleSave} className="space-y-4">
          <label className="block">
            <span className="caption mb-1.5 block text-muted">Name</span>
            <input
              type="text"
              required
              placeholder="e.g. Local Docker Runner"
              className="input"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </label>

          <label className="block">
            <span className="caption mb-1.5 block text-muted">Description (optional)</span>
            <input
              type="text"
              placeholder="e.g. General execution environment"
              className="input"
              value={desc}
              onChange={(e) => setDesc(e.target.value)}
            />
          </label>

          <label className="block">
            <span className="caption mb-1.5 block text-muted">Backend Provider</span>
            <select
              disabled={!!editingConfig}
              className="input"
              value={sandboxType}
              onChange={(e) => setSandboxType(e.target.value as "docker" | "cube" | "e2b")}
            >
              <option value="docker">Docker (Local or remote daemon)</option>
              <option value="cube">Cube (Managed Kubernetes)</option>
              <option value="e2b">E2B (Cloud microVMs)</option>
            </select>
          </label>

          {sandboxType === "docker" && (
            <label className="block">
              <span className="caption mb-1.5 block text-muted">Docker Daemon Endpoint</span>
              <input
                type="text"
                placeholder="unix:///var/run/docker.sock or tcp://127.0.0.1:2375"
                className="input font-mono text-xs"
                value={endpoint}
                onChange={(e) => setEndpoint(e.target.value)}
              />
            </label>
          )}

          {sandboxType === "cube" && (
            <>
              <label className="block">
                <span className="caption mb-1.5 block text-muted">Cube API Endpoint</span>
                <input
                  type="text"
                  required
                  placeholder="https://cube.internal:8443"
                  className="input font-mono text-xs"
                  value={endpoint}
                  onChange={(e) => setEndpoint(e.target.value)}
                />
              </label>
              <label className="block">
                <span className="caption mb-1.5 block text-muted">
                  API Key{editingConfig ? " (leave empty to keep current)" : ""}
                </span>
                <input
                  type="password"
                  className="input font-mono text-xs"
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                />
              </label>
            </>
          )}

          {sandboxType === "e2b" && (
            <label className="block">
              <span className="caption mb-1.5 block text-muted">
                E2B API Key{editingConfig ? " (leave empty to keep current)" : ""}
              </span>
              <input
                type="password"
                required={!editingConfig}
                className="input font-mono text-xs"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
              />
            </label>
          )}

          <div className="flex justify-end gap-3 pt-3">
            <button
              type="button"
              className="btn btn-outline"
              onClick={() => setModalOpen(false)}
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={saving || !name.trim()}
              className="btn btn-primary"
            >
              {saving ? "Saving…" : editingConfig ? "Save changes" : "Create sandbox"}
            </button>
          </div>
        </form>
      </Modal>

      {/* Inventory Status Modal */}
      <Modal
        open={inventoryConfig !== null}
        title={`Status for ${inventoryConfig?.name || "Sandbox"}`}
        onClose={() => setInventoryConfig(null)}
      >
        <div className="space-y-4">
          {loadingInventory ? (
            <p className="caption text-muted text-center py-6">Checking backend status…</p>
          ) : (
            <>
              <div className="rounded-xl border border-hairline p-4 space-y-2 bg-surface-strong/30">
                <div className="flex items-center justify-between text-sm">
                  <span className="text-muted">Active Sandbox Containers:</span>
                  <span className="font-semibold text-ink text-base">
                    {inventoryCount !== null ? inventoryCount : "Unknown"}
                  </span>
                </div>
                <div className="flex items-center justify-between text-sm">
                  <span className="text-muted">Provider:</span>
                  <span className="badge-pill uppercase text-xs">
                    {inventoryConfig?.sandbox_type}
                  </span>
                </div>
              </div>

              {inventorySessions.length > 0 && (
                <div className="space-y-1">
                  <span className="caption text-muted font-medium">Bound Session IDs:</span>
                  <div className="max-h-32 overflow-y-auto space-y-1">
                    {inventorySessions.map((sid) => (
                      <div
                        key={sid}
                        className="text-xs font-mono p-1.5 rounded bg-surface-strong text-muted truncate"
                      >
                        {sid}
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </>
          )}

          <div className="flex justify-end pt-2">
            <button
              type="button"
              className="btn btn-outline"
              onClick={() => setInventoryConfig(null)}
            >
              Close
            </button>
          </div>
        </div>
      </Modal>

      {/* Delete Modal */}
      <Modal
        open={deletingConfig !== null}
        title="Delete Sandbox Configuration"
        onClose={() => setDeletingConfig(null)}
      >
        <div className="space-y-4">
          <p className="body-sm text-body">
            Are you sure you want to delete <strong>{deletingConfig?.name}</strong>?
            Agents configured to run in this sandbox will fail until mapped to another active environment.
          </p>
          <div className="flex justify-end gap-3 pt-2">
            <button
              type="button"
              className="btn btn-outline"
              onClick={() => setDeletingConfig(null)}
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={deletingBusy}
              onClick={() => void handleDelete()}
              className="btn btn-primary bg-rose-600 hover:bg-rose-700 text-white"
            >
              {deletingBusy ? "Deleting…" : "Delete Configuration"}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
