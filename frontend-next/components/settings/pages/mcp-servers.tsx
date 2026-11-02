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
import { useInSettingsModal } from "@/components/system/in-modal-nav";
import { useT } from "@/lib/i18n";
import { ConsolePage } from "@/components/settings/console-page";
import { Notice } from "@/components/settings/notice";
import { SectionHeader } from "@/components/settings/section-header";

export default function McpServersPage() {
  return (
    <RequireSystemAccess minRole="system">
      <McpServersPanel />
    </RequireSystemAccess>
  );
}

function McpServersPanel() {
  const inModal = useInSettingsModal();
  const { t } = useT();
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
      .catch((e) => setMcpError(e instanceof Error ? e.message : t("mcpp.loadFailed")))
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
      setMcpError(e instanceof Error ? e.message : t("mcpp.toggleFailed"));
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
          message: e instanceof Error ? e.message : t("mcpp.testFailed"),
          tools: [],
        },
      }));
    } finally {
      setTestingId(null);
    }
  };

  return (
    <ConsolePage size="form">
      <SectionHeader
        title={t("mcpp.title")}
        subtitle={t("mcpp.subtitle")}
        actions={
          <>
            <button
              type="button"
              className="btn btn-outline btn-sm"
              onClick={loadMcp}
              disabled={mcpLoading}
              title={t("mcpp.refreshTip")}
              aria-label={t("mcpp.refreshTip")}
            >
              <IconRefresh className={`h-3.5 w-3.5 ${mcpLoading ? "animate-spin" : ""}`} />
              {t("common.refresh")}
            </button>
            <button type="button" className="btn btn-primary btn-sm" onClick={() => setCreatingMcp(true)}>
              <IconPlus className="h-3.5 w-3.5" /> {t("mcpp.add")}
            </button>
          </>
        }
      />

      {mcpError && (
        <Notice
          tone="error"
          className="mt-4"
          action={
            <button type="button" className="btn btn-outline btn-sm" onClick={loadMcp}>
              {t("common.retry")}
            </button>
          }
        >
          {mcpError}
        </Notice>
      )}

      <div
        className={`grid grid-cols-1 gap-4 md:grid-cols-2 ${inModal ? "" : "lg:grid-cols-3"}`}
      >
        {mcpServices.map((s) => {
          const testResult = testResults[s.id];
          return (
            <div
              key={s.id}
              className={`flex flex-col justify-between rounded-xl border p-4.5 transition-all shadow-sm ${
                s.enabled
                  ? "border-hairline bg-surface-card hover:border-hairline-strong hover:shadow-md"
                  : "border-hairline/60 bg-surface-card/60 opacity-80"
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
                        <span className="badge-neutral rounded px-1.5 py-0.5 text-[10px] font-medium">
                          {t("mcpp.builtin")}
                        </span>
                      )}
                    </div>
                    <div className="mt-1 flex flex-wrap items-center gap-1.5">
                      <span className="badge-neutral badge-pill text-[11px] uppercase tracking-wider">
                        {s.transport_type}
                      </span>
                      {s.catalog && (
                        <span className="badge-accent rounded px-1.5 py-0.5 text-[11px] font-medium">
                          {t("mcpp.toolCount", { n: s.catalog.tool_count })}
                        </span>
                      )}
                      {s.catalog?.stale && (
                        <span className="badge-warning rounded px-1.5 py-0.5 text-[11px] font-medium">
                          {t("mcp.stale")}
                        </span>
                      )}
                    </div>
                  </div>

                  <Toggle
                    checked={s.enabled}
                    onChange={() => void toggleMcp(s)}
                    title={s.enabled ? t("common.on") : t("common.off")}
                    label={s.enabled ? t("common.on") : t("common.off")}
                  />
                </div>

                {/* Description */}
                <p className="caption line-clamp-2 text-muted mb-3">
                  {s.description || t("mcpp.noDesc")}
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
                    className={`caption mb-3 rounded border p-2 text-[11px] ${
                      testResult.success
                        ? "border-success/30 bg-success-surface text-success"
                        : "border-error/30 bg-error-surface text-error"
                    }`}
                  >
                    {testResult.success
                      ? t("mcpp.testOk", { n: (testResult.tools ?? []).length })
                      : testResult.message || t("mcp.testFail")}
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
                    {testingId === s.id ? t("vstore.testing") : t("vstore.test")}
                  </button>
                  <button
                    className="btn btn-tertiary btn-sm text-[12px]"
                    onClick={() => setToolsForMcp(s)}
                  >
                    {t("mcp.tools")}
                  </button>
                  <button
                    className="btn btn-tertiary btn-sm text-[12px]"
                    onClick={() => setEditingMcp(s)}
                  >
                    {t("common.edit")}
                  </button>
                </div>

                {!s.is_builtin && (
                  <button
                    type="button"
                    className="btn btn-tertiary btn-sm text-[12px] text-error"
                    onClick={() => setRemovingMcp(s)}
                    title={t("mcp.deleteTitle")}
                    aria-label={t("mcp.deleteTitle")}
                  >
                    <IconTrash className="h-3.5 w-3.5" />
                  </button>
                )}
              </div>
            </div>
          );
        })}

        {/* Quick Add Card */}
        <button
          type="button"
          onClick={() => setCreatingMcp(true)}
          className="flex min-h-[160px] cursor-pointer flex-col items-center justify-center rounded-xl border border-dashed border-hairline-strong bg-surface-card/40 p-6 text-center transition-all hover:border-primary hover:bg-surface-card focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink"
        >
          <div className="mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-surface-strong text-muted group-hover:text-primary">
            <IconPlus className="h-5 w-5" />
          </div>
          <span className="text-[14px] font-medium text-ink">{t("mcpp.register")}</span>
          <span className="caption mt-1 text-muted">{t("mcpp.registerDesc")}</span>
        </button>
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
        title={t("mcp.deleteTitle")}
        onClose={() => setRemovingMcp(null)}
        width="w-[420px]"
      >
        <p className="body-sm text-body">
          {t("mcp.deleteBody", { name: removingMcp?.name ?? "" })}
        </p>
        <div className="mt-4 flex justify-end gap-2">
          <button className="btn btn-outline btn-sm" onClick={() => setRemovingMcp(null)}>
            {t("common.cancel")}
          </button>
          <button
            type="button"
            className="btn btn-danger btn-sm"
            onClick={() => {
              if (removingMcp) {
                void deleteMCPService(removingMcp.id).then(() => {
                  setRemovingMcp(null);
                  loadMcp();
                });
              }
            }}
          >
            {t("common.delete")}
          </button>
        </div>
      </Modal>
    </ConsolePage>
  );
}
