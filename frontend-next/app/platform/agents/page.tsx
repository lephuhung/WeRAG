"use client";

import { useEffect, useState } from "react";
import { Orb } from "@/components/orb";
import { IconAgent, IconPlus } from "@/components/icons";
import { listAgents, type CustomAgent } from "@/lib/api/agents";
import { useAuth } from "@/lib/auth";

export default function Agents() {
  const { user } = useAuth();
  // Agent authoring is platform configuration — SystemAdmin only.
  const canManageAgents = user?.is_system_admin === true;
  const [agents, setAgents] = useState<CustomAgent[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
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
  }, []);

  const rows = (agents ?? []).map((a) => ({
    id: a.id,
    name: a.name,
    description: a.description ?? "",
    model: a.config?.model_id ?? "",
  }));

  return (
    <div className="relative flex-1 overflow-y-auto">
      <Orb color="peach" size={480} className="-top-32 right-[-100px]" />
      <div className="relative mx-auto w-full max-w-[1200px] px-12 py-12">
        <div className="mb-10 flex items-end justify-between gap-6">
          <div>
            <div className="caption-uppercase mb-3 text-muted">Workspace</div>
            <h1 className="display-xl">Agents</h1>
            <p className="mt-3 max-w-[520px] text-body">
              Purpose-built assistants bound to knowledge bases and tools.
            </p>
          </div>
          {canManageAgents && (
            <button className="btn btn-primary">
              <IconPlus className="h-4 w-4" /> New agent
            </button>
          )}
        </div>

        {error && <p className="caption mb-6 text-error">{error}</p>}
        {agents !== null && agents.length === 0 && !error && (
          <p className="caption mb-6 text-muted-soft">No agents in this workspace yet.</p>
        )}

        <div className="grid grid-cols-1 gap-5 md:grid-cols-2 xl:grid-cols-3">
          {rows.map((a) => (
            <div key={a.id} className="card card-hover p-6">
              <div className="mb-4 flex h-10 w-10 items-center justify-center rounded-full bg-surface-strong text-ink">
                <IconAgent className="h-5 w-5" />
              </div>
              <h2 className="title-md">{a.name}</h2>
              <p className="body-sm mt-1.5 text-body">{a.description}</p>
              <div className="caption mt-5 flex items-center gap-3 text-muted">
                {a.model && <span className="badge-pill">{a.model}</span>}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
