/* Agents management — moved from the extensions index to its own route. */
"use client";

import { useEffect, useState } from "react";
import {
  IconAgent,
  IconChat,
  IconCode,
  IconCopy,
  IconEdit,
  IconEye,
  IconPlus,
  IconTrash,
} from "@/components/icons";
import {
  listAgents,
  deleteAgent,
  copyAgent,
  isBuiltinAgent,
  setAgentEnabled,
  type CustomAgent,
} from "@/lib/api/agents";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { AgentIMChannels } from "@/components/agents/im-channels";
import { AgentEmbedChannels } from "@/components/agents/embed-channels";
import { AgentEditorModal } from "@/components/agents/agent-editor";
import { Modal } from "@/components/modal";
import { listModels, type ModelConfig } from "@/lib/api/models";
import { DeniedView } from "@/components/settings/system-section";
import { Notice } from "@/components/settings/notice";
import { Loading } from "@/components/settings/loading";
import { listAllTenants } from "@/lib/api/tenants";
import { agentEnabled, normalizeDistribution, scopeSummary, switchOffNeedsConfirm } from "@/lib/agent-tenant-scope";
import { Toggle } from "@/components/settings/toggle";
import { RequireSystemAccess } from "@/components/require-system-access";

export default function AgentsPage() {
  return (
    <RequireSystemAccess minRole="system">
      <AgentsPanel />
    </RequireSystemAccess>
  );
}

function AgentsPanel() {
  const auth = useAuth();
  const { t } = useT();

  const [agents, setAgents] = useState<CustomAgent[] | null>(null);
  const [models, setModels] = useState<ModelConfig[]>([]);
  const [error, setError] = useState("");
  const [removing, setRemoving] = useState<CustomAgent | null>(null);
  const [imAgent, setImAgent] = useState<CustomAgent | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<CustomAgent | null>(null);
  const [embedAgent, setEmbedAgent] = useState<CustomAgent | null>(null);
  // Tổ chức names for the "Tổ chức áp dụng" badge
  const [tenantNames, setTenantNames] = useState<Record<number, string>>({});
  // on/off switch: the agent waiting for the "every plan includes it"
  // confirmation, and the one being saved
  const [switchConfirm, setSwitchConfirm] = useState<CustomAgent | null>(null);
  const [switching, setSwitching] = useState<string | null>(null);

  const applySwitch = async (agent: CustomAgent, next: boolean) => {
    setSwitchConfirm(null);
    setSwitching(agent.id);
    try {
      const res = await setAgentEnabled(agent.id, next);
      setAgents((prev) => (prev ?? []).map((x) => (x.id === agent.id ? { ...x, enabled: res.data?.enabled ?? next } : x)));
    } catch (e) {
      setError(e instanceof Error ? e.message : t("agentp.switchFailed"));
    } finally {
      setSwitching(null);
    }
  };

  const requestSwitch = (agent: CustomAgent, next: boolean) => {
    if (switchOffNeedsConfirm(agent.id, next)) {
      setSwitchConfirm(agent);
      return;
    }
    void applySwitch(agent, next);
  };

  const load = () => {
    let alive = true;
    listAgents({ includeDisabled: true })
      .then((res) => {
        if (alive) setAgents(res.data ?? []);
      })
      .catch((e) => {
        if (alive) {
          setAgents([]);
          setError(e instanceof Error ? e.message : t("agentp.loadFailed"));
        }
      });
    listModels()
      .then((rows) => {
        if (alive) setModels(rows ?? []);
      })
      .catch(() => {
        if (alive) setModels([]);
      });
    listAllTenants()
      .then((res) => {
        if (!alive || !res.success) return;
        const names: Record<number, string> = {};
        for (const tenant of res.data?.items ?? []) names[tenant.id] = tenant.name;
        setTenantNames(names);
      })
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  };

  const getModelDisplayName = (modelId?: string) => {
    if (!modelId) return "";
    const m = models.find((item) => item.id === modelId);
    return m ? (m.display_name?.trim() || m.name) : modelId;
  };

  useEffect(() => {
    const stop = load();
    return stop;
  }, []);

  const remove = async () => {
    if (!removing) return;
    try {
      await deleteAgent(removing.id);
      setAgents((prev) => prev?.filter((a) => a.id !== removing.id) ?? null);
    } catch (e) {
      setError(e instanceof Error ? e.message : t("agentp.deleteFailed"));
    } finally {
      setRemoving(null);
    }
  };

  const duplicate = async (a: CustomAgent) => {
    try {
      const res = await copyAgent(a.id);
      setAgents((prev) => [...(prev ?? []), res.data]);
    } catch (e) {
      setError(e instanceof Error ? e.message : t("agentp.copyFailed"));
    }
  };

  if (auth.ready && !auth.user?.is_system_admin) {
    return <DeniedView />;
  }

  return (
    <>
      <div className="mb-6 flex flex-col items-start gap-4 sm:flex-row sm:items-center sm:justify-between">
        <p className="text-[14px] text-muted">
          {t("agentp.subtitle")}
        </p>
        <button
          className="btn btn-primary"
          onClick={() => {
            setEditing(null);
            setEditorOpen(true);
          }}
        >
          <IconPlus className="h-4 w-4" /> {t("agent.create")}
        </button>
      </div>

      {error && (
        <Notice
          tone="error"
          className="mb-6"
          action={
            <button type="button" className="btn btn-outline btn-sm" onClick={() => void load()}>
              {t("common.retry")}
            </button>
          }
        >
          {error}
        </Notice>
      )}
      {agents === null && !error && <Loading className="py-8" />}
      {agents !== null && agents.length === 0 && !error && (
        <p className="caption mb-6 text-muted-soft">{t("agentp.empty")}</p>
      )}

      <div
        className="grid grid-cols-[repeat(auto-fit,minmax(min(100%,360px),1fr))] gap-5"
      >
        {(agents ?? []).map((a) => {
          const isBuiltin = isBuiltinAgent(a.id);
          const modelName = getModelDisplayName(a.config?.model_id);
          const distribution = isBuiltin ? "basic" : normalizeDistribution(a.distribution);
          const scope = distribution === "internal" ? scopeSummary(a.applies_to_tenant_ids, (id) => tenantNames[id]) : null;
          const scopeAll = (a.applies_to_tenant_ids ?? []).map((id) => tenantNames[id] || `#${id}`).join(", ");

          return (
            <div
              key={a.id}
              className="card card-hover group flex min-w-0 flex-col p-5 sm:p-6 cursor-pointer transition-colors duration-150 hover:border-hairline-strong"
              onClick={() => {
                setEditing(a);
                setEditorOpen(true);
              }}
            >
              {/* Header: Avatar + Built-in badge (left) & Action icons (right) */}
              <div className="flex items-center justify-between gap-3 mb-3.5">
                <div className="flex flex-wrap items-center gap-2.5 min-w-0">
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-surface-strong text-ink">
                    <IconAgent className="h-5 w-5" />
                  </div>
                  {isBuiltin && (
                    <span className="rounded-full bg-surface-strong px-2 py-0.5 text-[11px] font-medium text-muted shrink-0">
                      {t("agentp.builtin")}
                    </span>
                  )}
                  {distribution === "addon" && (
                    <span
                      className="badge-pill badge-warning shrink-0 text-[11px]"
                      title={t("agent.distribution.addonDesc")}
                    >
                      {t("agent.distribution.addon")}
                    </span>
                  )}
                  {distribution === "internal" && !scope && (
                    <span className="shrink-0 rounded-full bg-primary/10 px-2 py-0.5 text-[11px] font-medium text-primary">
                      {t("agent.distribution.internal")}
                    </span>
                  )}
                  {scope && (
                    <span
                      className="min-w-0 truncate rounded-full bg-primary/10 px-2 py-0.5 text-[11px] font-medium text-primary"
                      title={t("agentp.scopeTitle", { names: scopeAll })}
                    >
                      {t("agentp.scope", { names: scope.names.join(", ") + (scope.more > 0 ? ` +${scope.more}` : "") })}
                    </span>
                  )}
                </div>

                <div className="shrink-0" onClick={(e) => e.stopPropagation()}>
                  <Toggle
                    size="sm"
                    checked={agentEnabled(a)}
                    disabled={switching === a.id}
                    title={agentEnabled(a) ? t("agentp.switchOn") : t("agentp.switchOff")}
                    onChange={(v) => requestSwitch(a, v)}
                  />
                </div>
              </div>

              {/* Content: Title and Description */}
              <div className={`min-w-0 flex-1 ${agentEnabled(a) ? "" : "opacity-60"}`}>
                <h2 className="title-md break-words text-ink group-hover:text-primary transition-colors">
                  {a.name}
                </h2>
                {!agentEnabled(a) && (
                  <span className="caption text-muted">{t("agentp.offNote")}</span>
                )}
                <p className="body-sm mt-2 text-muted line-clamp-3 leading-relaxed min-h-[48px]">
                  {a.description || "—"}
                </p>
              </div>

              {/* Footer: Compact metadata */}
              <div className="mt-4 space-y-2 rounded-xl bg-surface-strong/40 p-3 text-xs">
                <div className="flex flex-col items-start gap-2 min-w-0">
                  {modelName ? (
                    <span
                      className="max-w-full break-all text-[12px] font-medium leading-relaxed text-ink"
                      title={t("agentp.chatModel", { name: modelName, id: a.config?.model_id ?? "" })}
                    >
                      {modelName}
                    </span>
                  ) : (
                    <span className="text-[11.5px] text-muted-soft">{t("agentp.defaultModel")}</span>
                  )}
                  {a.config?.rerank_model_id && (
                    <span
                      className="max-w-full break-all text-[12px] leading-relaxed text-success"
                      title={t("agentp.rerank", { name: getModelDisplayName(a.config?.rerank_model_id) })}
                    >
                      RR: {getModelDisplayName(a.config?.rerank_model_id)}
                    </span>
                  )}
                </div>
                {a.config?.agent_mode === "smart-reasoning" && (
                  <span className="caption block text-[11.5px] text-muted-soft">
                    {t("agent.modeSmart")}
                  </span>
                )}
              </div>
                <div
                  className="mt-4 flex flex-wrap items-center justify-end gap-1 border-t border-hairline pt-3 text-muted"
                  onClick={(e) => e.stopPropagation()}
                >
                  <button
                    type="button"
                    className="flex h-8 w-8 items-center justify-center rounded-lg text-muted transition-colors hover:bg-surface-strong hover:text-ink"
                    title={t("agentEditor.im.title")}
                    aria-label={t("agentEditor.im.title")}
                    onClick={(e) => {
                      e.stopPropagation();
                      setImAgent(a);
                    }}
                  >
                    <IconChat className="h-4 w-4" />
                  </button>
                  <button
                    type="button"
                    className="flex h-8 w-8 items-center justify-center rounded-lg text-muted transition-colors hover:bg-surface-strong hover:text-ink"
                    title={t("embedPublish.title")}
                    aria-label={t("embedPublish.title")}
                    onClick={(e) => {
                      e.stopPropagation();
                      setEmbedAgent(a);
                    }}
                  >
                    <IconCode className="h-4 w-4" />
                  </button>
                  <button
                    type="button"
                    className="flex h-8 w-8 items-center justify-center rounded-lg text-muted transition-colors hover:bg-surface-strong hover:text-ink"
                    title={t("common.edit")}
                    aria-label={t("common.edit")}
                    onClick={(e) => {
                      e.stopPropagation();
                      setEditing(a);
                      setEditorOpen(true);
                    }}
                  >
                    <IconEdit className="h-4 w-4" />
                  </button>
                  <button
                    type="button"
                    className="flex h-8 w-8 items-center justify-center rounded-lg text-muted transition-colors hover:bg-surface-strong hover:text-ink"
                    title={t("common.copy")}
                    aria-label={t("common.copy")}
                    onClick={(e) => {
                      e.stopPropagation();
                      void duplicate(a);
                    }}
                  >
                    <IconCopy className="h-4 w-4" />
                  </button>
                  {!isBuiltin && (
                    <button
                      type="button"
                      className="flex h-8 w-8 items-center justify-center rounded-lg text-muted transition-colors hover:bg-surface-strong hover:text-error"
                      title={t("common.delete")}
                      aria-label={t("common.delete")}
                      onClick={(e) => {
                        e.stopPropagation();
                        setRemoving(a);
                      }}
                    >
                      <IconTrash className="h-4 w-4" />
                    </button>
                  )}
                </div>
            </div>
          );
        })}
      </div>

      <AgentEditorModal
        open={editorOpen}
        agent={editing}
        models={models}
        readOnly={false}
        onClose={() => setEditorOpen(false)}
        onSaved={() => load()}
      />
      <Modal
        open={switchConfirm !== null}
        title={t("agentp.switchOffBasicTitle")}
        onClose={() => setSwitchConfirm(null)}
        width="w-[440px]"
      >
        <p className="body-sm text-body">{t("agentp.switchOffBasicBody", { name: switchConfirm?.name ?? "" })}</p>
        <div className="mt-4 flex justify-end gap-2">
          <button className="btn btn-outline btn-sm" onClick={() => setSwitchConfirm(null)}>
            {t("common.cancel")}
          </button>
          <button
            className="btn btn-danger btn-sm"
            onClick={() => switchConfirm && void applySwitch(switchConfirm, false)}
          >
            {t("agentp.switchOffConfirm")}
          </button>
        </div>
      </Modal>
      <Modal
        open={removing !== null}
        title={t("agent.deleteConfirmTitle")}
        onClose={() => setRemoving(null)}
        width="w-[420px]"
      >
        <p className="body-sm text-body">
          {t("agent.deleteConfirmBody", { name: removing?.name ?? "" })}
        </p>
        <div className="mt-4 flex justify-end gap-2">
          <button className="btn btn-outline btn-sm" onClick={() => setRemoving(null)}>
            {t("common.cancel")}
          </button>
          <button
            className="btn btn-danger btn-sm"
            onClick={() => void remove()}
          >
            {t("common.delete")}
          </button>
        </div>
      </Modal>
      {imAgent && (
        <AgentIMChannels
          agentId={imAgent.id}
          agentName={imAgent.name}
          open
          onClose={() => setImAgent(null)}
        />
      )}
      {embedAgent && (
        <AgentEmbedChannels
          agentId={embedAgent.id}
          agentName={embedAgent.name}
          open
          onClose={() => setEmbedAgent(null)}
        />
      )}
    </>
  );
}
