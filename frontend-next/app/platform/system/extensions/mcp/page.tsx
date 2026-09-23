/* MCP servers — extracted from the old single-page Extensions tab. */
"use client";

import { useCallback, useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import {
  IconPlus,
  IconRefresh,
  IconTrash,
} from "@/components/icons";
import {
  listMCPServices,
  updateMCPService,
  deleteMCPService,
  testMCPService,
  type MCPService,
  type MCPTestResult,
} from "@/lib/api/mcp";
import { McpServiceForm, McpToolsPanel } from "@/components/settings/mcp-services";
import { Toggle } from "@/components/settings/toggle";
import { RequireSystemAccess } from "@/components/require-system-access";

export default function McpServersPage() {
  return (
    <RequireSystemAccess minRole="system">
      <McpServersPanel />
    </RequireSystemAccess>
  );
}

function McpServersPanel() {
  const [mcpServices, setMcpServices] = useState<MCPService[]>([]);
  const [mcpLoading, setMcpLoading] = useState(true);
  const [mcpError, setMcpError] = useState("");
  const [editingMcp, setEditingMcp] = useState<MCPService | null>(null);
  const [creatingMcp, setCreatingMcp] = useState(false);
  const [removingMcp, setRemovingMcp] = useState<MCPService | null>(null);
  const [toolsForMcp, setToolsForMcp] = useState<MCPService | null>(null);
  const [testingId, setTestingId] = useState<string | null>(null);
  const [testResults, setTestResults] = useState<Record<string, MCPTestResult>>({});

  const loadMcp = useCallback(() => {
    setMcpLoading(true);
    setMcpError("");
    listMCPServices()
      .then((data) => setMcpServices(data ?? []))
      .catch((e) => setMcpError(e instanceof Error ? e.message : "Failed to load MCP services"))
      .finally(() => setMcpLoading(false));
  }, []);

  useEffect(() => {
    loadMcp();
  }, [loadMcp]);

  const toggleMcp = async (s: MCPService) => {
    try {
      const next = await updateMCPService(s.id, { enabled: !s.enabled });
      setMcpServices((prev) => prev.map((x) => (x.id === next.id ? next : x)));
    } catch (e) {
      setMcpError(e instanceof Error ? e.message : "Failed to toggle service");
    }
  };

  const handleTestMcp = async (s: MCPService) => {
    setTestingId(s.id);
    try {
      const res = await testMCPService(s.id);
      setTestResults((prev) => ({ ...prev, [s.id]: res }));
    } catch (e) {
      setTestResults((prev) => ({
        ...prev,
        [s.id]: {
          success: false,
          message: e instanceof Error ? e.message : "Test failed",
          tools: [],
        },
      }));
    } finally {
      setTestingId(null);
    }
  };

  return (
    <div>
      {/* Header bar */}
      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 className="title-md font-semibold text-ink">MCP Servers</h2>
          <p className="caption text-muted">
            External tools and service endpoints available for dynamic tool calls in agents.
          </p>
        </div>

        <div className="flex items-center gap-2">
          <button
            className="btn btn-outline btn-sm"
            onClick={loadMcp}
            disabled={mcpLoading}
            title="Refresh MCP servers"
          >
            <IconRefresh className={`h-3.5 w-3.5 ${mcpLoading ? "animate-spin" : ""}`} />
            Refresh
          </button>
          <button className="btn btn-primary btn-sm" onClick={() => setCreatingMcp(true)}>
            <IconPlus className="h-3.5 w-3.5" /> Add MCP Server
          </button>
        </div>
      </div>

      {mcpError && <p className="caption mb-4 text-error">{mcpError}</p>}

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
        {mcpServices.map((s) => {
          const testResult = testResults[s.id];
          return (
            <div
              key={s.id}
              className={`flex flex-col justify-between rounded-xl border p-4.5 transition-all shadow-sm ${
                s.enabled
                  ? "border-hairline bg-surface-card hover:border-hairline-strong hover:shadow-md"
                  : "border-hairline/60 bg-surface/60 opacity-80"
              }`}
            >
              <div>
                {/* Top row: Name & Enabled Switch */}
                <div className="mb-2 flex items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-[15px] font-semibold text-ink" title={s.name}>
                        {s.name}
                      </span>
                      {s.is_builtin && (
                        <span className="rounded bg-surface-strong px-1.5 py-0.5 text-[10px] font-medium text-muted">
                          builtin
                        </span>
                      )}
                    </div>
                    <div className="mt-1 flex flex-wrap items-center gap-1.5">
                      <span className="badge-pill text-[11px] uppercase tracking-wider">
                        {s.transport_type}
                      </span>
                      {s.catalog && (
                        <span className="rounded bg-primary/10 px-1.5 py-0.5 text-[11px] font-medium text-primary">
                          {s.catalog.tool_count} tools
                        </span>
                      )}
                      {s.catalog?.stale && (
                        <span className="rounded bg-amber-500/10 px-1.5 py-0.5 text-[11px] font-medium text-amber-600">
                          stale
                        </span>
                      )}
                    </div>
                  </div>

                  <Toggle
                    checked={s.enabled}
                    onChange={() => void toggleMcp(s)}
                    title={s.enabled ? "Enabled" : "Disabled"}
                    label={s.enabled ? "Enabled" : "Disabled"}
                  />
                </div>

                {/* Description */}
                <p className="caption line-clamp-2 text-muted mb-3">
                  {s.description || "No description provided."}
                </p>

                {/* Endpoint / Stdio Details */}
                {s.url && (
                  <div className="caption mb-3 truncate font-mono text-[11px] text-muted-soft bg-surface-strong rounded px-2 py-1">
                    {s.url}
                  </div>
                )}
                {s.stdio_config && (
                  <div className="caption mb-3 truncate font-mono text-[11px] text-muted-soft bg-surface-strong rounded px-2 py-1">
                    {s.stdio_config.command} {(s.stdio_config.args ?? []).join(" ")}
                  </div>
                )}

                {/* Test result message if available */}
                {testResult && (
                  <div
                    className={`caption mb-3 rounded p-2 text-[11px] ${
                      testResult.success
                        ? "bg-emerald-500/10 text-emerald-600 border border-emerald-500/20"
                        : "bg-rose-500/10 text-rose-600 border border-rose-500/20"
                    }`}
                  >
                    {testResult.success
                      ? `Connected · ${(testResult.tools ?? []).length} tools verified`
                      : testResult.message || "Connection failed"}
                  </div>
                )}
              </div>

              {/* Action row */}
              <div className="mt-3 flex items-center justify-between border-t border-hairline pt-3">
                <div className="flex items-center gap-1">
                  <button
                    className="btn btn-tertiary btn-sm text-[12px]"
                    onClick={() => void handleTestMcp(s)}
                    disabled={testingId === s.id}
                  >
                    {testingId === s.id ? "Testing…" : "Test"}
                  </button>
                  <button
                    className="btn btn-tertiary btn-sm text-[12px]"
                    onClick={() => setToolsForMcp(s)}
                  >
                    Tools
                  </button>
                  <button
                    className="btn btn-tertiary btn-sm text-[12px]"
                    onClick={() => setEditingMcp(s)}
                  >
                    Edit
                  </button>
                </div>

                {!s.is_builtin && (
                  <button
                    className="btn btn-tertiary btn-sm text-[12px] text-error hover:bg-rose-500/10"
                    onClick={() => setRemovingMcp(s)}
                    title="Delete service"
                  >
                    <IconTrash className="h-3.5 w-3.5" />
                  </button>
                )}
              </div>
            </div>
          );
        })}

        {/* Quick Add Card */}
        <div
          onClick={() => setCreatingMcp(true)}
          className="flex min-h-[160px] cursor-pointer flex-col items-center justify-center rounded-xl border border-dashed border-hairline-strong bg-surface-card/40 p-6 text-center transition-all hover:border-primary hover:bg-surface-card"
        >
          <div className="mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-surface-strong text-muted group-hover:text-primary">
            <IconPlus className="h-5 w-5" />
          </div>
          <span className="text-[14px] font-medium text-ink">Register MCP Server</span>
          <span className="caption mt-1 text-muted">HTTP Streamable, SSE or Stdio</span>
        </div>
      </div>

      {/* Modals & SlidePanels for MCP */}
      {(creatingMcp || editingMcp) && (
        <McpServiceForm
          service={creatingMcp ? null : editingMcp}
          onClose={() => {
            setCreatingMcp(false);
            setEditingMcp(null);
          }}
          onSaved={() => {
            setCreatingMcp(false);
            setEditingMcp(null);
            loadMcp();
          }}
        />
      )}

      {toolsForMcp && (
        <McpToolsPanel
          service={toolsForMcp}
          onClose={() => setToolsForMcp(null)}
        />
      )}

      <Modal
        open={removingMcp !== null}
        title="Delete MCP Service"
        onClose={() => setRemovingMcp(null)}
        width="w-[420px]"
      >
        <p className="body-sm text-body">
          Are you sure you want to delete MCP service &quot;{removingMcp?.name}&quot;? Agents using its tools will lose access.
        </p>
        <div className="mt-4 flex justify-end gap-2">
          <button className="btn btn-outline btn-sm" onClick={() => setRemovingMcp(null)}>
            Cancel
          </button>
          <button
            className="btn btn-sm bg-[var(--color-error)] text-white"
            onClick={() => {
              if (removingMcp) {
                void deleteMCPService(removingMcp.id).then(() => {
                  setRemovingMcp(null);
                  loadMcp();
                });
              }
            }}
          >
            Delete
          </button>
        </div>
      </Modal>
    </div>
  );
}
