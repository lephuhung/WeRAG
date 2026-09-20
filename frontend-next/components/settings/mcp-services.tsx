/* Ported from frontend/src/views/settings/McpSettings.vue +
 * integrations/McpServerIntegrationSettings.vue (core list + editor).
 * Covers: service list with tool counts, create/edit form (transport,
 * URL, headers, auth_type + api_key/token or stdio command), enable
 * toggle, tools panel with per-tool enable/approval, test connect,
 * delete. OAuth flow lives client-side only — unported (needs popup +
 * callback route) and surfaced as read-only status badge.
 */
"use client";

import { useCallback, useEffect, useState } from "react";
import { SlidePanel, SlidePanelHeader } from "@/components/slide-panel";
import { Modal } from "@/components/modal";
import { useT } from "@/lib/i18n";
import {
  listMCPServices,
  createMCPService,
  updateMCPService,
  deleteMCPService,
  testMCPService,
  getMCPServiceTools,
  setMCPToolEnabled,
  setMCPToolApproval,
  type MCPService,
  type MCPTool,
  type MCPTestResult,
} from "@/lib/api/mcp";
import { Chip } from "@/components/settings/chips";

export function McpServicesPanel({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useT();
  const [services, setServices] = useState<MCPService[]>([]);
  const [loading, setLoading] = useState(open);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<MCPService | null>(null);
  const [creating, setCreating] = useState(false);
  const [removing, setRemoving] = useState<MCPService | null>(null);
  const [toolsFor, setToolsFor] = useState<MCPService | null>(null);

  const load = useCallback(() => {
    setLoading(true);
    listMCPServices()
      .then(setServices)
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load services"))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    if (open) load();
  }, [open, load]);

  const toggle = async (s: MCPService) => {
    const next = await updateMCPService(s.id, { enabled: !s.enabled });
    setServices((prev) => prev.map((x) => (x.id === next.id ? next : x)));
  };

  return (
    <SlidePanel open={open} onClose={onClose} label={t("mcp.title")} width="w-[600px]">
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="mb-4 flex items-center justify-between px-5 pt-5">
          <h2 className="title-md">{t("mcp.title")}</h2>
          <button className="btn btn-primary btn-sm" onClick={() => setCreating(true)}>
            {t("mcp.addService")}
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 pb-5">
          {error && <p className="caption mb-3 text-error">{error}</p>}
          {loading && <p className="caption text-muted">…</p>}
          {(services ?? []).map((s) => (
            <div key={s.id} className="border-b border-hairline py-3.5">
              <div className="flex items-center gap-3">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-[14px] font-medium text-ink">{s.name}</span>
                    <span className="badge-pill">{s.transport_type}</span>
                    {s.is_builtin && (
                      <span className="caption text-muted-soft">built-in</span>
                    )}
                  </div>
                  <div className="caption mt-0.5 truncate text-muted">
                    {s.description}
                    {s.catalog && (
                      <span className="ml-2">
                        · {s.catalog.tool_count} tools{s.catalog.stale ? ` · ${t("mcp.stale")}` : ""}
                      </span>
                    )}
                  </div>
                  {s.url && !s.is_builtin && (
                    <div className="caption mt-0.5 truncate text-muted-soft">{s.url}</div>
                  )}
                </div>
                <button role="switch" aria-checked={s.enabled} onClick={() => void toggle(s)}
                  className={`relative h-6 w-11 shrink-0 rounded-full transition-colors ${s.enabled ? "bg-primary" : "bg-hairline-strong"}`}
                  aria-label={t("common.on")}
                >
                  <span className={`absolute top-0.5 h-5 w-5 rounded-full bg-surface-card transition-transform ${
                    s.enabled ? "translate-x-[22px]" : "translate-x-0.5"
                  }`} />
                </button>
                <button className="btn btn-tertiary btn-sm text-[13px]" onClick={() => setToolsFor(s)}>
                  {t("mcp.tools")}
                </button>
                <button className="btn btn-tertiary btn-sm text-[13px]" onClick={() => setEditing(s)}>
                  {t("common.edit")}
                </button>
                {!s.is_builtin && (
                  <button className="btn btn-tertiary btn-sm text-[13px] text-error" onClick={() => setRemoving(s)}>
                    {t("common.delete")}
                  </button>
                )}
              </div>
            </div>
          ))}
          {services !== null && services.length === 0 && !loading && !error && (
            <p className="caption text-muted-soft">{t("mcp.empty")}</p>
          )}
        </div>
      </div>

      {(creating || editing) && (
        <McpServiceForm
          service={creating ? null : editing}
          onClose={() => {
            setCreating(false);
            setEditing(null);
          }}
          onSaved={() => {
            setCreating(false);
            setEditing(null);
            load();
          }}
        />
      )}

      {toolsFor && (
        <McpToolsPanel
          service={toolsFor}
          onClose={() => setToolsFor(null)}
        />
      )}

      <Modal open={removing !== null} title={t("mcp.deleteTitle")} onClose={() => setRemoving(null)} width="w-[420px]">
        <p className="body-sm text-body">
          {t("mcp.deleteBody").replace("{name}", removing?.name ?? "")}
        </p>
        <div className="mt-4 flex justify-end gap-2">
          <button className="btn btn-outline btn-sm" onClick={() => setRemoving(null)}>
            {t("common.cancel")}
          </button>
          <button
            className="btn btn-sm bg-[var(--color-error)] text-white"
            onClick={() => {
              void deleteMCPService(removing!.id).then(() => {
                setRemoving(null);
                load();
              });
            }}
          >
            {t("common.delete")}
          </button>
        </div>
      </Modal>
    </SlidePanel>
  );
}

function McpServiceForm({ service, onClose, onSaved }: {
  service: MCPService | null;
  onClose: () => void;
  onSaved: (saved?: MCPService) => void;
}) {
  const { t } = useT();
  const [name, setName] = useState(service?.name ?? "");
  const [description, setDescription] = useState(service?.description ?? "");
  const [transport, setTransport] = useState(service?.transport_type ?? "http-streamable");
  const [url, setUrl] = useState(service?.url ?? "");
  const [enabled, setEnabled] = useState(service?.enabled ?? true);
  const [authType, setAuthType] = useState<"" | "api_key" | "bearer">(service?.auth_config?.auth_type as "" | "api_key" | "bearer" ?? "");
  const [apiKey, setApiKey] = useState("");
  const [apiKeyHeader, setApiKeyHeader] = useState(service?.auth_config?.api_key_header ?? "");
  const [token, setToken] = useState("");
  const [cmd, setCmd] = useState(service?.stdio_config?.command ?? "npx");
  const [args, setArgs] = useState((service?.stdio_config?.args ?? []).join(" "));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<MCPTestResult | null>(null);

  const submit = async () => {
    if (!name.trim()) return;
    setSaving(true);
    setError("");
    try {
      const payload: Partial<MCPService> = {
        name: name.trim(),
        description,
        enabled: service?.enabled ?? true,
        transport_type: transport,
        ...(transport === "stdio"
          ? {
              stdio_config: {
                command: cmd as "uvx" | "npx",
                args: args.split(/\s+/).filter(Boolean),
              },
            }
          : {
              url,
              ...(authType
                ? {
                    auth_config: {
                      auth_type: authType,
                      ...(authType === "api_key"
                        ? { api_key: apiKey || undefined, api_key_header: apiKeyHeader || undefined }
                        : { token: token || undefined }),
                    },
                  }
                : {}),
            }),
      };
      const saved = service
        ? await updateMCPService(service.id, payload)
        : await createMCPService(payload);
      onSaved(saved);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSaving(false);
    }
  };

  const runTest = async () => {
    if (!service) return;
    setTesting(true);
    try {
      const res = await testMCPService(service.id);
      setTestResult(res);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Test failed");
    } finally {
      setTesting(false);
    }
  };

  return (
    <Modal open title={service ? t("mcp.editService") : t("mcp.addService")} onClose={onClose} width="w-[620px]">
      <div className="flex max-h-[70vh] flex-col gap-4 overflow-y-auto pr-1">
        {error && <p className="caption text-error">{error}</p>}
        <div className="flex gap-4">
          <label className="block flex-1">
            <span className="caption mb-1.5 block text-muted">{t("mcp.name")}</span>
            <input className="input" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
          </label>
        </div>
        <label className="block">
          <span className="caption mb-1.5 block text-muted">{t("mcp.description")}</span>
          <textarea
            className="input h-auto min-h-[56px] resize-y"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </label>
        <div>
          <span className="caption mb-1.5 block text-muted">{t("mcp.transport")}</span>
          <div className="flex gap-2">
            {(["http-streamable", "sse", "stdio"] as const).map((tr) => (
              <Chip key={tr} active={transport === tr} onClick={() => setTransport(tr)}>
                {tr}
              </Chip>
            ))}
          </div>
        </div>
        {transport !== "stdio" ? (
          <>
            <label className="block">
              <span className="caption mb-1.5 block text-muted">{t("mcp.url")}</span>
              <input
                className="input font-mono text-[13px]"
                value={url}
                placeholder="https://server.example.com/mcp"
                onChange={(e) => setUrl(e.target.value)}
              />
            </label>
            <div className="border-t border-hairline pt-4">
              <div className="caption-uppercase mb-2 text-muted">{t("mcp.sectionAuth")}</div>
              <div className="flex gap-1.5">
                <Chip active={authType === ""} onClick={() => setAuthType("")}>{t("mcp.authNone")}</Chip>
                <Chip active={authType === "api_key"} onClick={() => setAuthType("api_key")}>API key</Chip>
                <Chip active={authType === "bearer"} onClick={() => setAuthType("bearer")}>Bearer</Chip>
              </div>
              {authType === "api_key" && (
                <>
                  <label className="mt-3 block">
                    <span className="caption mb-1.5 block text-muted">API key</span>
                    <input className="input" type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} />
                  </label>
                  <label className="mt-3 block">
                    <span className="caption mb-1.5 block text-muted">{t("mcp.apiKeyHeader")}</span>
                    <input
                      className="input"
                      value={apiKeyHeader}
                      placeholder="X-API-Key"
                      onChange={(e) => setApiKeyHeader(e.target.value)}
                    />
                  </label>
                </>
              )}
              {authType === "bearer" && (
                <label className="mt-3 block">
                  <span className="caption mb-1.5 block text-muted">Token</span>
                  <input className="input" type="password" value={token} onChange={(e) => setToken(e.target.value)} />
                </label>
              )}
              {authType === "" && service?.auth_config?.auth_type === "oauth" && (
                <p className="caption mt-2 text-muted">OAuth2 authorized — {t("mcp.oauthBadge")}</p>
              )}
            </div>
          </>
        ) : (
          <div className="flex gap-4">
            <label className="block w-[140px]">
              <span className="caption mb-1.5 block text-muted">Command</span>
              <select className="input" value={cmd} onChange={(e) => setCmd(e.target.value as "uvx" | "npx")}>
                <option value="npx">npx</option>
                <option value="uvx">uvx</option>
              </select>
            </label>
            <label className="block flex-1">
              <span className="caption mb-1.5 block text-muted">{t("mcp.args")}</span>
              <input
                className="input font-mono text-[13px]"
                value={args}
                onChange={(e) => setArgs(e.target.value)}
              />
            </label>
          </div>
        )}

        {service && (
          <div className="border-t border-hairline pt-4">
            <div className="flex items-center gap-3">
              <button className="btn btn-outline btn-sm" onClick={() => void runTest()} disabled={testing}>
                {testing ? "…" : t("mcp.test")}
              </button>
              {testResult && (
                <span className={`caption ${testResult.success ? "text-success" : "text-error"}`}>
                  {testResult.success
                    ? `${t("mcp.testOk")} · ${(testResult.tools ?? []).length} tools`
                    : testResult.message || t("mcp.testFail")}
                </span>
              )}
            </div>
          </div>
        )}
      </div>

      <div className="mt-5 flex items-center justify-end gap-2 border-t border-hairline pt-4">
        <button className="btn btn-outline btn-sm" onClick={onClose} disabled={saving}>
          {t("common.cancel")}
        </button>
        <button className="btn btn-primary btn-sm" disabled={saving || !name.trim()} onClick={() => void submit()}>
          {saving ? "…" : t("common.save")}
        </button>
      </div>
    </Modal>
  );
}

/* Per-tool enable + approval toggles (port of the tools tab of the Vue drawer). */
function McpToolsPanel({ service, onClose }: { service: MCPService; onClose: () => void }) {
  const { t } = useT();
  const [tools, setTools] = useState<MCPTool[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    getMCPServiceTools(service.id)
      .then((list) => alive && setTools(list))
      .catch((e) => alive && setError(e instanceof Error ? e.message : "Failed to load tools"))
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [service.id]);

  return (
    <SlidePanel open onClose={onClose} label={service.name} width="w-[520px]">
      <SlidePanelHeader title={`${service.name} — ${t("mcp.tools")}`} onClose={onClose} />
      <div className="min-h-0 flex-1 overflow-y-auto px-5 pb-5">
        {error && <p className="caption mb-3 text-error">{error}</p>}
        {loading && <p className="caption text-muted">…</p>}
        {(tools ?? []).map((tool) => (
          <div key={tool.name} className="flex items-center gap-3 border-b border-hairline py-3">
            <div className="min-w-0 flex-1">
              <div className="truncate text-[14px] font-medium text-ink">{tool.name}</div>
              <div className="caption mt-0.5 line-clamp-2 text-muted">{tool.description}</div>
            </div>
            <button
              className={`badge-pill shrink-0 ${tool.require_approval ? "text-error" : ""}`}
              onClick={() => {
                void setMCPToolApproval(service.id, tool.name, !tool.require_approval).then(() => {
                  setTools((prev) =>
                    prev.map((x) => (x.name === tool.name ? { ...x, require_approval: !tool.require_approval } : x)),
                  );
                });
              }}
            >
              {tool.require_approval ? t("mcp.approvalRequired") : t("mcp.noApproval")}
            </button>
            <button role="switch"
              aria-checked={tool.enabled !== false}
              onClick={() => {
                void setMCPToolEnabled(service.id, tool.name, tool.enabled === false).then(() => {
                  setTools((prev) =>
                    prev.map((x) => (x.name === tool.name ? { ...x, enabled: tool.enabled === false } : x)),
                  );
                });
              }}
              className={`relative h-6 w-11 shrink-0 rounded-full transition-colors ${
                tool.enabled !== false ? "bg-primary" : "bg-hairline-strong"
              }`}
              aria-label={t("mcp.tools")}
            >
              <span className={`absolute top-0.5 h-5 w-5 rounded-full bg-surface-card transition-transform ${
                tool.enabled !== false ? "translate-x-[22px]" : "translate-x-0.5"
              }`} />
            </button>
          </div>
        ))}
        {tools !== null && tools.length === 0 && !loading && !error && (
          <p className="caption text-muted-soft">{t("mcp.noTools")}</p>
        )}
      </div>
    </SlidePanel>
  );
}

