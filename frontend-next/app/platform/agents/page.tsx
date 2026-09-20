/* Ported from frontend/src/views/agent/AgentList.vue: list cards with
 * copy/delete actions, "New agent" gate (SystemAdmin), and the editor modal.
 */
"use client";

import { useEffect, useState } from "react";
import { Orb } from "@/components/orb";
import { IconAgent, IconPlus } from "@/components/icons";
import {
  listAgents,
  deleteAgent,
  copyAgent,
  isBuiltinAgent,
  type CustomAgent,
} from "@/lib/api/agents";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { AgentIMChannels } from "@/components/agents/im-channels";
import { AgentEmbedChannels } from "@/components/agents/embed-channels";
import { AgentEditorModal } from "@/components/agents/agent-editor";
import { Modal } from "@/components/modal";
import { listModels, type ModelConfig } from "@/lib/api/models";

export default function Agents() {
  const auth = useAuth();
  const user = auth.user;
  const { t } = useT();

  const currentRole = auth.memberships.find(
    (m) => String(m.tenant_id) === String(auth.selectedTenantId ?? auth.tenant?.id ?? ""),
  )?.role;
  const isTenantAdmin = currentRole === "admin" || currentRole === "owner";
  const isContributor = isTenantAdmin || currentRole === "contributor";
  const isSystemAdmin = user?.is_system_admin === true;

  const canCreateAgents = isSystemAdmin || isContributor;
  const canManageAgent = (agent?: CustomAgent | null) => {
    if (isSystemAdmin || isTenantAdmin) return true;
    if (agent?.created_by && user?.id && agent.created_by === user.id) return true;
    return false;
  };

  const [agents, setAgents] = useState<CustomAgent[] | null>(null);
  const [models, setModels] = useState<ModelConfig[]>([]);
  const [error, setError] = useState("");
  const [removing, setRemoving] = useState<CustomAgent | null>(null);
  const [imAgent, setImAgent] = useState<CustomAgent | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<CustomAgent | null>(null);
  const [embedAgent, setEmbedAgent] = useState<CustomAgent | null>(null);

  const load = () => {
    let alive = true;
    listAgents()
      .then((res) => {
        if (alive) setAgents(res.data ?? []);
      })
      .catch((e) => {
        if (alive) {
          setAgents([]);
          setError(e instanceof Error ? e.message : "Failed to load agents");
        }
      });
    listModels()
      .then((rows) => {
        if (alive) setModels(rows ?? []);
      })
      .catch(() => {
        if (alive) setModels([]);
      });
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
      setError(e instanceof Error ? e.message : "Delete failed");
    } finally {
      setRemoving(null);
    }
  };

  const duplicate = async (a: CustomAgent) => {
    try {
      const res = await copyAgent(a.id);
      setAgents((prev) => [...(prev ?? []), res.data]);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Copy failed");
    }
  };

  return (
    <>
    <div className="relative flex-1 overflow-y-auto">
      <Orb color="peach" size={480} className="-top-32 right-[-100px]" />
      <div className="relative mx-auto w-full max-w-[1200px] px-12 py-12">
        <div className="mb-10 flex items-end justify-between gap-6">
          <div>
            <div className="caption-uppercase mb-3 text-muted">Workspace</div>
            <h1 className="display-xl">{t("nav.agents")}</h1>
            <p className="mt-3 max-w-[520px] text-body">
              Purpose-built assistants bound to knowledge bases and tools.
            </p>
          </div>
          {canCreateAgents && (
            <button
              className="btn btn-primary"
              onClick={() => {
                setEditing(null);
                setEditorOpen(true);
              }}
            >
              <IconPlus className="h-4 w-4" /> {t("agent.create")}
            </button>
          )}
        </div>

        {error && <p className="caption mb-6 text-error">{error}</p>}
        {agents !== null && agents.length === 0 && !error && (
          <p className="caption mb-6 text-muted-soft">No agents in this workspace yet.</p>
        )}

        <div className="grid grid-cols-1 gap-5 md:grid-cols-2 xl:grid-cols-3">
          {(agents ?? []).map((a) => (
            <div
              key={a.id}
              className="card card-hover flex min-w-0 flex-col p-6 cursor-pointer"
              onClick={() => {
                setEditing(a);
                setEditorOpen(true);
              }}
            >
              <div className="mb-4 flex h-10 w-10 items-center justify-center rounded-full bg-surface-strong text-ink">
                <IconAgent className="h-5 w-5" />
              </div>
              <h2 className="title-md truncate">{a.name}</h2>
              <p className="body-sm mt-1.5 line-clamp-2 flex-1 text-body">{a.description}</p>
              <div className="caption mt-5 flex min-w-0 flex-col gap-2.5 border-t border-hairline pt-4 text-muted">
                  {a.config?.model_id && (
                    <span className="badge-pill truncate max-w-full" title={`Model: ${getModelDisplayName(a.config.model_id)} (ID: ${a.config.model_id})`}>
                      {getModelDisplayName(a.config.model_id)}
                    </span>
                  )}
                  {a.config?.agent_mode === "smart-reasoning" && (
                    <span className="caption text-muted-soft">{t("agent.modeSmart")}</span>
                  )}
                  {isBuiltinAgent(a.id) && <span className="caption text-muted-soft">built-in</span>}
                </div>
                <div
                  className="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-[12px]"
                  onClick={(e) => e.stopPropagation()}
                >
                  <button
                    className="transition-colors hover:text-ink"
                    onClick={(e) => {
                      e.stopPropagation();
                      setImAgent(a);
                    }}
                  >
                    {t("agentEditor.im.title")}
                  </button>
                  <button
                    className="transition-colors hover:text-ink"
                    onClick={(e) => {
                      e.stopPropagation();
                      setEmbedAgent(a);
                    }}
                  >
                    {t("embedPublish.title")}
                  </button>
                  {canManageAgent(a) ? (
                    <button
                      className="transition-colors hover:text-ink"
                      onClick={(e) => {
                        e.stopPropagation();
                        setEditing(a);
                        setEditorOpen(true);
                      }}
                    >
                      {t("common.edit")}
                    </button>
                  ) : (
                    <button
                      className="transition-colors hover:text-ink"
                      onClick={(e) => {
                        e.stopPropagation();
                        setEditing(a);
                        setEditorOpen(true);
                      }}
                    >
                      {t("common.view")}
                    </button>
                  )}
                  {(canManageAgent(a) || isContributor || isSystemAdmin) && (
                    <button
                      className="transition-colors hover:text-ink"
                      onClick={(e) => {
                        e.stopPropagation();
                        void duplicate(a);
                      }}
                    >
                      {t("common.copy")}
                    </button>
                  )}
                  {canManageAgent(a) && !isBuiltinAgent(a.id) && (
                    <button
                      className="transition-colors hover:text-error"
                      onClick={(e) => {
                        e.stopPropagation();
                        setRemoving(a);
                      }}
                    >
                      {t("common.delete")}
                    </button>
                  )}
                </div>
              </div>
          ))}
        </div>
      </div>
    </div>

      <AgentEditorModal
        open={editorOpen}
        agent={editing}
        models={models}
        readOnly={!canManageAgent(editing)}
        onClose={() => setEditorOpen(false)}
        onSaved={() => load()}
      />
      <Modal
        open={removing !== null}
        title={t("agent.deleteConfirmTitle")}
        onClose={() => setRemoving(null)}
        width="w-[420px]"
      >
        <p className="body-sm text-body">
          {t("agent.deleteConfirmBody").replace("{name}", removing?.name ?? "")}
        </p>
        <div className="mt-4 flex justify-end gap-2">
          <button className="btn btn-outline btn-sm" onClick={() => setRemoving(null)}>
            {t("common.cancel")}
          </button>
          <button
            className="btn btn-sm bg-[var(--color-error)] text-white"
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
