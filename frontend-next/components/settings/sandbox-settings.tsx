"use client";

import { useCallback, useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { IconEdit, IconPlus, IconRefresh, IconTrash } from "@/components/icons";
import { Toggle } from "@/components/settings/toggle";
import { Select } from "@/components/select";
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
import { ApiError } from "@/lib/api-client";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { formatDate } from "@/lib/datetime";
import { DataTable } from "@/components/settings/data-table";
import { Notice } from "@/components/settings/notice";
import { SectionHeader } from "@/components/settings/section-header";
import { Loading } from "@/components/settings/loading";
import { DeniedView } from "@/components/settings/system-section";
import { FieldLabel } from "@/components/settings/field";
import { Pill, PillGroup } from "@/components/settings/pill";
import { ConsolePage } from "@/components/settings/console-page";
import { useToast } from "@/components/toast";

/* Same default the Vue editor pre-fills (SandboxConfigEditorDrawer.vue). */
const DEFAULT_DOCKER_IMAGE = "wechatopenai/weknora-sandbox:main";

const FILTER_TYPES = ["all", "docker", "cube", "e2b"] as const;

export function SandboxSettings() {
  const { t } = useT();
  const toast = useToast();
  const auth = useAuth();
  const activeTenantId = Number(auth.selectedTenantId ?? auth.tenant?.id ?? 0);
  const currentRole =
    auth.memberships.find((m) => String(m.tenant_id) === String(activeTenantId))?.role ?? "";
  const isSystemAdmin = auth.user?.is_system_admin === true;
  /* Sandbox configs (all /sandbox-configs routes incl. workspace-policy
   * and per-config skills) are Admin+ on the backend, so all Tenant
   * Admins may manage — not just legacy owners. */
  const canManage = currentRole === "owner" || currentRole === "admin" || isSystemAdmin;

  const [configs, setConfigs] = useState<SandboxConfigRecord[]>([]);
  const [scriptsDisabled, setScriptsDisabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [denied, setDenied] = useState(false);

  const [filterType, setFilterType] = useState<string>("all");

  // Create / Edit Modal
  const [modalOpen, setModalOpen] = useState(false);
  const [editingConfig, setEditingConfig] = useState<SandboxConfigRecord | null>(null);
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [sandboxType, setSandboxType] = useState<"docker" | "cube" | "e2b">("docker");
  const [endpoint, setEndpoint] = useState("");
  const [apiKey, setApiKey] = useState("");
  // Fields the Vue editor (SandboxConfigEditorDrawer) collects per backend.
  const [dockerImage, setDockerImage] = useState("");
  const [tlsCertPath, setTlsCertPath] = useState("");
  const [allowPrivate, setAllowPrivate] = useState(false);
  const [apiUrl, setApiUrl] = useState("");
  const [proxyUrl, setProxyUrl] = useState("");
  const [sandboxDomain, setSandboxDomain] = useState("");
  const [dnsServers, setDnsServers] = useState("");
  const [saving, setSaving] = useState(false);
  const [nameError, setNameError] = useState("");
  const [formError, setFormError] = useState("");
  const [deleteError, setDeleteError] = useState("");

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
      setDenied(false);
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        setConfigs([]);
      } else {
        setError(e instanceof Error ? e.message : t("sbox.loadFailed"));
      }
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
      toast.success(t(disabled ? "sbox.policyDisabled" : "sbox.policyEnabled"));
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        return;
      }
      setError(e instanceof Error ? e.message : t("sbox.policyFailed"));
    }
  };

  // Open Create
  const openCreate = () => {
    setEditingConfig(null);
    setName("");
    setDesc("");
    setSandboxType("docker");
    setEndpoint("unix:///var/run/docker.sock");
    setDockerImage(DEFAULT_DOCKER_IMAGE);
    setTlsCertPath("");
    setAllowPrivate(false);
    setApiUrl("");
    setProxyUrl("");
    setSandboxDomain("");
    setDnsServers("");
    setApiKey("");
    setNameError("");
    setModalOpen(true);
  };

  // Open Edit
  const openEdit = (cfg: SandboxConfigRecord) => {
    setEditingConfig(cfg);
    setName(cfg.name);
    setDesc(cfg.description || "");
    const st = (cfg.sandbox_type as "docker" | "cube" | "e2b") || "docker";
    const c = cfg.config || {};
    setSandboxType(st);
    setAllowPrivate(c.allow_private_endpoints === true);
    setApiKey("");
    setProxyUrl("");
    setSandboxDomain("");
    setDnsServers("");
    setApiUrl("");
    setNameError("");
    if (st === "docker") {
      setEndpoint(c.docker?.host || c.docker?.endpoint || "unix:///var/run/docker.sock");
      setDockerImage(c.docker?.image || DEFAULT_DOCKER_IMAGE);
      setTlsCertPath(c.docker?.tls_cert_path || "");
    } else if (st === "cube") {
      setEndpoint("");
      setDockerImage("");
      setTlsCertPath("");
      setApiUrl(c.cube?.api_url || c.cube?.endpoint || "");
      setProxyUrl(c.cube?.proxy_url || "");
      setSandboxDomain(c.cube?.sandbox_domain || "");
      setDnsServers((c.cube?.dns_servers ?? []).join(", "));
    } else {
      setEndpoint("");
      setDockerImage("");
      setTlsCertPath("");
      setApiUrl(c.e2b?.api_url || "");
      setSandboxDomain(c.e2b?.sandbox_domain || "");
      setProxyUrl(c.e2b?.proxy_url || "");
    }
    setModalOpen(true);
  };

  // Save Config
  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) {
      setNameError(t("common.required"));
      return;
    }
    setNameError("");
    setFormError("");
    setSaving(true);
    try {
      const configData: SandboxConfigData = {
        // The backend validates "sandbox_type" (Vue's collectPayload sends it
        // at the config top level); "type" kept for legacy readers.
        type: sandboxType,
        sandbox_type: sandboxType,
      };
      if (allowPrivate) configData.allow_private_endpoints = true;
      if (sandboxType === "docker") {
        const host = endpoint.trim() || undefined;
        configData.docker = {
          // Legacy records read "endpoint"; the Vue editor persists "host".
          endpoint: host,
          host,
          image: dockerImage.trim() || DEFAULT_DOCKER_IMAGE,
          ...(tlsCertPath.trim() ? { tls_cert_path: tlsCertPath.trim() } : {}),
        };
      } else if (sandboxType === "cube") {
        const url = apiUrl.trim() || undefined;
        configData.cube = {
          endpoint: url,
          api_url: url,
          ...(proxyUrl.trim() ? { proxy_url: proxyUrl.trim() } : {}),
          ...(sandboxDomain.trim() ? { sandbox_domain: sandboxDomain.trim() } : {}),
          ...(apiKey.trim() ? { api_key: apiKey.trim() } : {}),
          ...(dnsServers.split(",").map((s) => s.trim()).filter(Boolean).length
            ? { dns_servers: dnsServers.split(",").map((s) => s.trim()).filter(Boolean) }
            : {}),
        };
      } else if (sandboxType === "e2b") {
        configData.e2b = {
          ...(apiKey.trim() ? { api_key: apiKey.trim() } : {}),
          ...(apiUrl.trim() ? { api_url: apiUrl.trim() } : {}),
          ...(sandboxDomain.trim() ? { sandbox_domain: sandboxDomain.trim() } : {}),
          ...(proxyUrl.trim() ? { proxy_url: proxyUrl.trim() } : {}),
        };
      }

      if (editingConfig) {
        await updateSandboxConfigById(editingConfig.id, {
          name: name.trim(),
          description: desc.trim() || undefined,
          config: configData,
        });
        toast.success(t("sbox.updated", { name }));
      } else {
        await createSandboxConfig({
          name: name.trim(),
          description: desc.trim() || undefined,
          config: configData,
        });
        toast.success(t("sbox.created", { name }));
      }
      setModalOpen(false);
      await loadData();
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        setModalOpen(false);
        return;
      }
      setFormError(e instanceof Error ? e.message : t("sbox.saveFailed"));
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
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        return;
      }
      setError(e instanceof Error ? e.message : t("sbox.loadActiveFailed"));
    } finally {
      setLoadingInventory(false);
    }
  };

  // Delete Config
  const handleDelete = async () => {
    if (!deletingConfig) return;
    setDeletingBusy(true);
    setDeleteError("");
    try {
      await deleteSandboxConfig(deletingConfig.id);
      toast.success(t("sbox.deleted", { name: deletingConfig.name }));
      setDeletingConfig(null);
      await loadData();
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        return;
      }
      setDeleteError(e instanceof Error ? e.message : t("sbox.deleteFailed"));
    } finally {
      setDeletingBusy(false);
    }
  };

  const filteredConfigs = configs.filter(
    (c) => filterType === "all" || c.sandbox_type === filterType,
  );

  if (denied) {
    return <DeniedView />;
  }

  return (
    <ConsolePage size="form">
      <div className="space-y-8">
        {/* Header */}
        <SectionHeader
          title={t("sbox.title")}
          subtitle={t("sbox.subtitle")}
          actions={
            <>
              {canManage && (
                <button
                  type="button"
                  onClick={openCreate}
                  className="btn btn-primary btn-sm flex items-center gap-1.5"
                >
                  <IconPlus className="h-3.5 w-3.5" />
                  <span>{t("sbox.add")}</span>
                </button>
              )}
              <button
                type="button"
                onClick={() => void loadData()}
                className="btn btn-outline btn-sm p-1.5"
                title={t("common.refresh")}
                aria-label={t("common.refresh")}
              >
                <IconRefresh className="h-3.5 w-3.5" />
              </button>
            </>
          }
        />

        {error && (
          <Notice
            tone="error"
            action={
              <button type="button" className="btn btn-outline btn-sm" onClick={() => void loadData()}>
                {t("common.retry")}
              </button>
            }
          >
            {error}
          </Notice>
        )}

        {/* Kill Switch policy */}
        <div className="flex items-center justify-between rounded-xl border border-hairline bg-surface-card p-5">
          <div>
            <span className="text-sm font-semibold text-ink block">
              {t("sbox.policyTitle")}
            </span>
            <span className="caption text-muted block mt-0.5">
              {t("sbox.policyDesc")}
            </span>
          </div>
          <Toggle
            checked={!scriptsDisabled}
            disabled={!canManage}
            onChange={(checked) => void handleTogglePolicy(!checked)}
            label={t("sbox.policyTitle")}
          />
        </div>

        {/* Filters */}
        <PillGroup ariaLabel={t("common.filters")}>
          {FILTER_TYPES.map((ft) => (
            <Pill
              key={ft}
              active={filterType === ft}
              onClick={() => setFilterType(ft)}
              ariaLabel={ft === "all" ? t("common.all") : ft}
            >
              {ft === "all" ? t("common.all") : ft}
            </Pill>
          ))}
        </PillGroup>

        {/* Configs Table */}
        <DataTable<SandboxConfigRecord>
          columns={[
            { key: "name", header: t("sbox.colName") },
            { key: "type", header: t("sbox.colType") },
            { key: "endpoint", header: t("sbox.colEndpoint") },
            { key: "created", header: t("sbox.colCreated") },
            { key: "actions", header: t("mem.colActions"), align: "right" },
          ]}
          rows={filteredConfigs}
          rowKey={(cfg) => cfg.id}
          loading={loading}
          loadingLabel={t("sbox.loading")}
          emptyLabel={t("sbox.empty")}
          minWidth="min-w-[640px]"
          renderRow={(cfg) => {
            const ep =
              cfg.config?.docker?.endpoint ||
              cfg.config?.cube?.endpoint ||
              cfg.config?.e2b?.template ||
              t("stor.default");
            return (
              <>
                <td className="px-5 py-3.5 align-middle">
                  <span className="font-medium text-ink block">{cfg.name}</span>
                  {cfg.description && (
                    <span className="caption text-muted truncate block text-xs">
                      {cfg.description}
                    </span>
                  )}
                </td>
                <td className="px-5 py-3.5 align-middle">
                  <span className="badge-pill uppercase text-[10.5px] font-semibold">
                    {cfg.sandbox_type}
                  </span>
                </td>
                <td className="px-5 py-3.5 align-middle text-xs font-mono text-muted max-w-xs truncate">
                  {ep}
                </td>
                <td className="px-5 py-3.5 align-middle text-xs text-muted whitespace-nowrap">
                  {cfg.created_at ? formatDate(cfg.created_at) : "—"}
                </td>
                <td className="px-5 py-3.5 align-middle text-right space-x-1 whitespace-nowrap">
                  <button
                    type="button"
                    onClick={() => void openInventory(cfg)}
                    className="btn btn-outline btn-sm text-xs py-1"
                  >
                    {t("sbox.status")}
                  </button>
                  {canManage && (
                    <>
                      <button
                        type="button"
                        onClick={() => openEdit(cfg)}
                        className="btn btn-ghost btn-sm p-1.5 text-muted hover:text-ink"
                        title={t("common.edit")}
                        aria-label={t("common.edit")}
                      >
                        <IconEdit className="h-3.5 w-3.5" />
                      </button>
                      <button
                        type="button"
                        onClick={() => setDeletingConfig(cfg)}
                        className="btn btn-danger btn-sm p-1.5"
                        title={t("common.delete")}
                        aria-label={t("common.delete")}
                      >
                        <IconTrash className="h-3.5 w-3.5" />
                      </button>
                    </>
                  )}
                </td>
              </>
            );
          }}
        />

        {/* Create / Edit Modal */}
        <Modal
          open={modalOpen}
          title={editingConfig ? t("sbox.editTitle") : t("sbox.addTitle")}
          onClose={() => setModalOpen(false)}
        >
          <form onSubmit={handleSave} className="space-y-4">
            {formError && (
              <Notice tone="error" className="mb-4">
                {formError}
              </Notice>
            )}
            <div>
              <FieldLabel htmlFor="sbox-name" required>
                {t("sbox.name")}
              </FieldLabel>
              <input
                id="sbox-name"
                type="text"
                required
                aria-required="true"
                aria-invalid={nameError ? true : undefined}
                aria-describedby={nameError ? "sbox-name-error" : undefined}
                placeholder={t("sbox.namePh")}
                className="input"
                value={name}
                onChange={(e) => {
                  setName(e.target.value);
                  if (nameError) setNameError("");
                }}
              />
              {nameError && (
                <p id="sbox-name-error" className="caption mt-1 text-error">
                  {nameError}
                </p>
              )}
            </div>

            <div>
              <FieldLabel htmlFor="sbox-desc">{t("sbox.desc")}</FieldLabel>
              <input
                id="sbox-desc"
                type="text"
                placeholder={t("sbox.descPh")}
                className="input"
                value={desc}
                onChange={(e) => setDesc(e.target.value)}
              />
            </div>

            <div>
              <FieldLabel htmlFor="sbox-provider">{t("sbox.provider")}</FieldLabel>
              <Select
                id="sbox-provider"
                disabled={!!editingConfig}
                value={sandboxType}
                onChange={(v) => setSandboxType(v as "docker" | "cube" | "e2b")}
                options={[
                  { value: "docker", label: "Docker (Local or remote daemon)" },
                  { value: "cube", label: "Cube (Managed Kubernetes)" },
                  { value: "e2b", label: "E2B (Cloud microVMs)" },
                ]}
              />
            </div>

            {sandboxType === "docker" && (
              <>
                <div>
                  <FieldLabel htmlFor="sbox-docker-image" required>
                    {t("sbox.dockerImage")}
                  </FieldLabel>
                  <input
                    id="sbox-docker-image"
                    type="text"
                    required
                    aria-required="true"
                    placeholder={DEFAULT_DOCKER_IMAGE}
                    className="input font-mono text-xs"
                    value={dockerImage}
                    onChange={(e) => setDockerImage(e.target.value)}
                  />
                </div>
                <div>
                  <FieldLabel htmlFor="sbox-docker-endpoint">{t("sbox.dockerEndpoint")}</FieldLabel>
                  <input
                    id="sbox-docker-endpoint"
                    type="text"
                    placeholder="unix:///var/run/docker.sock or tcp://127.0.0.1:2375"
                    className="input font-mono text-xs"
                    value={endpoint}
                    onChange={(e) => setEndpoint(e.target.value)}
                  />
                </div>
                <div>
                  <FieldLabel htmlFor="sbox-tls-cert">{t("sbox.tlsCertPath")}</FieldLabel>
                  <input
                    id="sbox-tls-cert"
                    type="text"
                    placeholder="/etc/weknora/docker-certs"
                    className="input font-mono text-xs"
                    value={tlsCertPath}
                    onChange={(e) => setTlsCertPath(e.target.value)}
                  />
                </div>
              </>
            )}

            {sandboxType === "cube" && (
              <>
                <div>
                  <FieldLabel htmlFor="sbox-cube-endpoint" required>
                    {t("sbox.cubeEndpoint")}
                  </FieldLabel>
                  <input
                    id="sbox-cube-endpoint"
                    type="text"
                    required
                    aria-required="true"
                    placeholder="http://cube.example.com:33000"
                    className="input font-mono text-xs"
                    value={apiUrl}
                    onChange={(e) => setApiUrl(e.target.value)}
                  />
                </div>
                <div>
                  <FieldLabel htmlFor="sbox-cube-proxy">{t("sbox.proxyUrl")}</FieldLabel>
                  <input
                    id="sbox-cube-proxy"
                    type="text"
                    placeholder="http://cube.example.com:80"
                    className="input font-mono text-xs"
                    value={proxyUrl}
                    onChange={(e) => setProxyUrl(e.target.value)}
                  />
                </div>
                <div>
                  <FieldLabel htmlFor="sbox-cube-domain">{t("sbox.sandboxDomain")}</FieldLabel>
                  <input
                    id="sbox-cube-domain"
                    type="text"
                    placeholder="cube.app"
                    className="input font-mono text-xs"
                    value={sandboxDomain}
                    onChange={(e) => setSandboxDomain(e.target.value)}
                  />
                </div>
                <div>
                  <FieldLabel htmlFor="sbox-cube-key">
                    {t("sbox.apiKey")}
                    {editingConfig ? ` ${t("sbox.keepCurrent")}` : ""}
                  </FieldLabel>
                  <input
                    id="sbox-cube-key"
                    type="password"
                    className="input font-mono text-xs"
                    value={apiKey}
                    onChange={(e) => setApiKey(e.target.value)}
                  />
                </div>
                <div>
                  <FieldLabel htmlFor="sbox-dns">{t("sbox.dnsServers")}</FieldLabel>
                  <input
                    id="sbox-dns"
                    type="text"
                    placeholder="8.8.8.8, 1.1.1.1"
                    className="input font-mono text-xs"
                    value={dnsServers}
                    onChange={(e) => setDnsServers(e.target.value)}
                  />
                </div>
              </>
            )}

            {sandboxType === "e2b" && (
              <>
                <div>
                  <FieldLabel htmlFor="sbox-e2b-key" required={!editingConfig}>
                    {t("sbox.e2bKey")}
                    {editingConfig ? ` ${t("sbox.keepCurrent")}` : ""}
                  </FieldLabel>
                  <input
                    id="sbox-e2b-key"
                    type="password"
                    required={!editingConfig}
                    aria-required={!editingConfig ? "true" : undefined}
                    className="input font-mono text-xs"
                    value={apiKey}
                    onChange={(e) => setApiKey(e.target.value)}
                  />
                </div>
                <div>
                  <FieldLabel htmlFor="sbox-e2b-url">{t("sbox.apiUrl")}</FieldLabel>
                  <input
                    id="sbox-e2b-url"
                    type="text"
                    placeholder="https://api.e2b.app"
                    className="input font-mono text-xs"
                    value={apiUrl}
                    onChange={(e) => setApiUrl(e.target.value)}
                  />
                </div>
                <div>
                  <FieldLabel htmlFor="sbox-e2b-domain">{t("sbox.sandboxDomain")}</FieldLabel>
                  <input
                    id="sbox-e2b-domain"
                    type="text"
                    placeholder="e2b.app"
                    className="input font-mono text-xs"
                    value={sandboxDomain}
                    onChange={(e) => setSandboxDomain(e.target.value)}
                  />
                </div>
                <div>
                  <FieldLabel htmlFor="sbox-e2b-proxy">{t("sbox.proxyUrl")}</FieldLabel>
                  <input
                    id="sbox-e2b-proxy"
                    type="text"
                    placeholder="http://sandbox-gateway.example.com"
                    className="input font-mono text-xs"
                    value={proxyUrl}
                    onChange={(e) => setProxyUrl(e.target.value)}
                  />
                </div>
              </>
            )}

            <div className="flex items-start justify-between gap-4 rounded-[10px] border border-hairline px-3 py-2.5">
              <span className="min-w-0">
                <span className="block text-[13px] font-medium text-ink">{t("sbox.allowPrivate")}</span>
                <span className="caption mt-0.5 block text-muted">{t("sbox.allowPrivateHint")}</span>
              </span>
              <Toggle checked={allowPrivate} onChange={setAllowPrivate} label={t("sbox.allowPrivate")} />
            </div>

            <div className="flex justify-end gap-3 pt-3">
              <button
                type="button"
                className="btn btn-outline"
                onClick={() => setModalOpen(false)}
              >
                {t("common.cancel")}
              </button>
              <button
                type="submit"
                disabled={saving || !name.trim()}
                className="btn btn-primary"
              >
                {saving ? t("memp.saving") : editingConfig ? t("memp.saveChanges") : t("sbox.createCta")}
              </button>
            </div>
          </form>
        </Modal>

        {/* Inventory Status Modal */}
        <Modal
          open={inventoryConfig !== null}
          title={t("sbox.statusTitle", { name: inventoryConfig?.name || "Sandbox" })}
          onClose={() => setInventoryConfig(null)}
        >
          <div className="space-y-4">
            {loadingInventory ? (
              <Loading label={t("sbox.checking")} className="py-6" />
            ) : (
              <>
                <div className="rounded-xl border border-hairline p-4 space-y-2 bg-surface-strong/30">
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-muted">{t("sbox.activeContainers")}:</span>
                    <span className="font-semibold text-ink text-base">
                      {inventoryCount !== null ? inventoryCount : t("sbox.unknown")}
                    </span>
                  </div>
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-muted">{t("sbox.provider")}:</span>
                    <span className="badge-pill uppercase text-xs">
                      {inventoryConfig?.sandbox_type}
                    </span>
                  </div>
                </div>

                {inventorySessions.length > 0 && (
                  <div className="space-y-1">
                    <span className="caption text-muted font-medium">{t("sbox.sessions")}:</span>
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
                {t("common.close")}
              </button>
            </div>
          </div>
        </Modal>

        {/* Delete Modal */}
        <Modal
          open={deletingConfig !== null}
          title={t("sbox.deleteTitle")}
          onClose={() => setDeletingConfig(null)}
        >
          <div className="space-y-4">
            <p className="body-sm text-body">
              {t("sbox.deleteBody1")} <strong>{deletingConfig?.name}</strong>?
              {" "}{t("sbox.deleteBody2")}
            </p>

            {deleteError && (
              <Notice tone="error" className="mb-4">
                {deleteError}
              </Notice>
            )}
            <div className="flex justify-end gap-3 pt-2">
              <button
                type="button"
                className="btn btn-outline"
                onClick={() => setDeletingConfig(null)}
              >
                {t("common.cancel")}
              </button>
              <button
                type="button"
                disabled={deletingBusy}
                onClick={() => void handleDelete()}
                className="btn btn-danger"
              >
                {deletingBusy ? t("skillc.deleting") : t("sbox.deleteCta")}
              </button>
            </div>
          </div>
        </Modal>
      </div>
    </ConsolePage>
  );
}
