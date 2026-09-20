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
import { AgentEditorModal } from "@/components/agents/agent-editor";
import { Modal } from "@/components/modal";

export default function Agents() {
  const { user } = useAuth();
  const { t } = useT();
  // Agent authoring is platform configuration — SystemAdmin only.
  const canManageAgents = user?.is_system_admin === true;
  const [agents, setAgents] = useState<CustomAgent[] | null>(null);
  const [error, setError] = useState("");
  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<CustomAgent | null>(null);
  const [removing, setRemoving] = useState<CustomAgent | null>(null);

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
    return () => {
      alive = false;
    };
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
          {canManageAgents && (
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
            <div key={a.id} className="card card-hover flex flex-col p-6">
              <div className="mb-4 flex h-10 w-10 items-center justify-center rounded-full bg-surface-strong text-ink">
                <IconAgent className="h-5 w-5" />
              </div>
              <h2 className="title-md">{a.name}</h2>
              <p className="body-sm mt-1.5 flex-1 text-body">{a.description}</p>
              <div className="caption mt-5 flex items-center justify-between text-muted">
                <div className="flex items-center gap-3">
                  {a.config?.model_id && <span className="badge-pill">{a.config.model_id}</span>}
                  {a.config?.agent_mode === "smart-reasoning" && (
                    <span className="caption text-muted-soft">{t("agent.modeSmart")}</span>
                  )}
                  {isBuiltinAgent(a.id) && <span className="caption text-muted-soft">built-in</span>}
                </div>
                {canManageAgents && !isBuiltinAgent(a.id) && (
                  <div className="flex items-center gap-3 text-[13px]">
                    <button
                      className="transition-colors hover:text-ink"
                      onClick={() => {
                        setEditing(a);
                        setEditorOpen(true);
                      }}
                    >
                      {t("common.edit")}
                    </button>
                    <button
                      className="transition-colors hover:text-ink"
                      onClick={() => void duplicate(a)}
                    >
                      Copy
                    </button>
                    <button
                      className="transition-colors hover:text-error"
                      onClick={() => setRemoving(a)}
                    >
                      {t("common.delete")}
                    </button>
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
      </div>

      <AgentEditorModal
        open={editorOpen}
        agent={editing}
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
    </div>
  );
}
