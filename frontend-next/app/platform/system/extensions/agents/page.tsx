/* Agents management — moved from the extensions index to its own route. */
"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
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
  type CustomAgent,
} from "@/lib/api/agents";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { AgentIMChannels } from "@/components/agents/im-channels";
import { AgentEmbedChannels } from "@/components/agents/embed-channels";
import { AgentEditorModal } from "@/components/agents/agent-editor";
import { Modal } from "@/components/modal";
import { listModels, type ModelConfig } from "@/lib/api/models";
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
  const user = auth.user;
  const router = useRouter();
  const { t } = useT();

  // Superadmin guard
  useEffect(() => {
    if (auth.ready && auth.user && !auth.user.is_system_admin) {
      router.replace("/platform/system/workspace");
    }
  }, [auth.ready, auth.user, router]);

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

  if (auth.ready && !auth.user?.is_system_admin) {
    return null;
  }

  return (
    <>
      <div className="mb-6 flex items-center justify-between gap-4">
        <p className="text-[14px] text-muted">
          Purpose-built assistants bound to knowledge bases and tools.
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

      {error && <p className="caption mb-6 text-error">{error}</p>}
      {agents !== null && agents.length === 0 && !error && (
        <p className="caption mb-6 text-muted-soft">No agents in this workspace yet.</p>
      )}

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        {(agents ?? []).map((a) => {
          const isBuiltin = isBuiltinAgent(a.id);
          const modelName = getModelDisplayName(a.config?.model_id);

          return (
            <div
              key={a.id}
              className="card card-hover group flex min-w-0 flex-col p-5 cursor-pointer transition-all duration-150 hover:border-hairline-strong"
              onClick={() => {
                setEditing(a);
                setEditorOpen(true);
              }}
            >
              {/* Header: Avatar + Built-in badge (left) & Action icons (right) */}
              <div className="flex items-center justify-between gap-3 mb-3.5">
                <div className="flex items-center gap-2.5 min-w-0">
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-surface-strong text-ink">
                    <IconAgent className="h-5 w-5" />
                  </div>
                  {isBuiltin && (
                    <span className="rounded-full bg-surface-strong px-2 py-0.5 text-[11px] font-medium text-muted shrink-0">
                      built-in
                    </span>
                  )}
                </div>

                <div
                  className="flex items-center gap-1 text-muted shrink-0"
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

              {/* Content: Title and Description */}
              <div className="min-w-0 flex-1">
                <h2 className="title-md truncate text-ink group-hover:text-primary transition-colors">
                  {a.name}
                </h2>
                <p className="body-sm mt-1 text-muted line-clamp-2 min-h-[40px]">
                  {a.description || "—"}
                </p>
              </div>

              {/* Footer: Compact metadata */}
              <div className="mt-4 flex items-center justify-between gap-2 border-t border-hairline pt-3 min-h-[38px] text-xs">
                <div className="flex items-center gap-1.5 min-w-0">
                  {modelName ? (
                    <span
                      className="badge-pill truncate max-w-[150px]"
                      title={`Chat Model: ${modelName} (ID: ${a.config?.model_id})`}
                    >
                      {modelName}
                    </span>
                  ) : (
                    <span className="text-[11.5px] text-muted-soft">Default</span>
                  )}
                  {a.config?.rerank_model_id && (
                    <span
                      className="badge-pill truncate max-w-[140px] bg-emerald-500/10 text-emerald-600 border border-emerald-500/20"
                      title={`ReRank: ${getModelDisplayName(a.config?.rerank_model_id)}`}
                    >
                      RR: {getModelDisplayName(a.config?.rerank_model_id)}
                    </span>
                  )}
                </div>
                {a.config?.agent_mode === "smart-reasoning" && (
                  <span className="caption text-[11.5px] text-muted-soft shrink-0">
                    {t("agent.modeSmart")}
                  </span>
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
