"use client";

import { useEffect, useRef, useState } from "react";
import { apiGet } from "@/lib/api-client";
import { useAuth } from "@/lib/auth";
import {
  BUILTIN_QUICK_ANSWER_ID,
  BUILTIN_SMART_REASONING_ID,
  useChatContext,
} from "@/lib/chat-context";
import { IconChevronDown } from "@/components/icons";

/* Ports AgentSelector.vue: builtin quick-answer / smart-reasoning + custom
 * agents + shared-with-me agents. Selecting writes selectedAgentId (+ source
 * tenant for shared) into the chat context, mirroring settings.selectAgent.
 */

export function AgentModeButton({ onOpen }: { onOpen: () => void }) {
  const { selectedAgent, isAgentStreamMode } = useChatContext();
  const label = selectedAgent?.name ?? (isAgentStreamMode ? "Agent mode" : "Normal mode");
  return (
    <button
      onClick={onOpen}
      className={`flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-[13px] font-medium transition-colors ${
        isAgentStreamMode
          ? "border-ink bg-ink text-white"
          : "border-hairline-strong text-body hover:border-ink hover:text-ink"
      }`}
      title={selectedAgent?.description ?? label}
    >
      {label}
      <IconChevronDown className="h-3.5 w-3.5" />
    </button>
  );
}

export function AgentSelector({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { agents, sharedAgents, settings, selectAgent } = useChatContext();
  const [filter, setFilter] = useState("");
  const [sharedFull, setSharedFull] = useState<typeof sharedAgents>([]);

  useEffect(() => {
    if (!open) return;
    setFilter("");
    // Shared list from context may be stale; refresh names for the dropdown.
    apiGet<{ success: boolean; data?: typeof sharedAgents }>(`/api/v1/shared-agents`)
      .then((r) => {
        if (r.data) setSharedFull(r.data.filter((s) => s.agent && !s.disabled_by_me));
      })
      .catch(() => setSharedFull([]));
  }, [open]);

  if (!open) return null;
  const builtins = agents.filter((a) => a.is_builtin);
  const customs = agents.filter((a) => !a.is_builtin);
  const q = filter.trim().toLowerCase();
  const match = (name: string) => !q || name.toLowerCase().includes(q);
  const quick = builtins.find((a) => a.id === BUILTIN_QUICK_ANSWER_ID);
  const smart = builtins.find((a) => a.id === BUILTIN_SMART_REASONING_ID);
  const otherBuiltins = builtins.filter(
    (a) => a.id !== BUILTIN_QUICK_ANSWER_ID && a.id !== BUILTIN_SMART_REASONING_ID,
  );

  const pick = (id: string, sourceTenantId?: string | null) => {
    selectAgent(id, sourceTenantId ?? null);
    onClose();
  };
  const current = (id: string, source?: string | null) =>
    settings.selectedAgentId === id && (settings.selectedAgentSourceTenantId ?? null) === (source ?? null);

  return (
    <>
      <div className="fixed inset-0 z-40" onClick={onClose} />
      <div className="card absolute bottom-full left-0 z-50 mb-2 flex max-h-[320px] w-[300px] flex-col overflow-hidden shadow-[0_4px_16px_rgba(0,0,0,0.08)]">
        <div className="shrink-0 border-b border-hairline p-2">
          <input
            className="input h-9 text-[14px]"
            placeholder="Search agents…"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            autoFocus
          />
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
          {quick && match(quick.name) && (
            <AgentRow name="Normal mode" desc={quick.description ?? "Fast RAG answers"} active={current(quick.id)} onClick={() => pick(quick.id)} />
          )}
          {smart && match(smart.name) && (
            <AgentRow name="Agent mode" desc={smart.description ?? "Reasoning with tools"} active={current(smart.id)} onClick={() => pick(smart.id)} />
          )}
          {otherBuiltins.filter((a) => match(a.name)).map((a) => (
            <AgentRow key={a.id} name={a.name} desc={a.description} active={current(a.id)} onClick={() => pick(a.id)} />
          ))}
          {customs.filter((a) => match(a.name)).length > 0 && (
            <div className="caption-uppercase px-3 pb-1 pt-2 text-muted-soft">My agents</div>
          )}
          {customs.filter((a) => match(a.name)).map((a) => (
            <AgentRow key={a.id} name={a.name} desc={a.description} active={current(a.id)} onClick={() => pick(a.id)} />
          ))}
          {(sharedFull.length > 0 || sharedAgents.length > 0) && (
            <div className="caption-uppercase px-3 pb-1 pt-2 text-muted-soft">Shared with me</div>
          )}
          {(sharedFull.length > 0 ? sharedFull : sharedAgents)
            .filter((s) => match(s.agent.name))
            .map((s) => (
              <AgentRow
                key={`${s.agent.id}-${s.source_tenant_id}`}
                name={s.agent.name}
                desc={s.org_name}
                badge="shared"
                active={current(s.agent.id, String(s.source_tenant_id))}
                onClick={() => pick(s.agent.id, String(s.source_tenant_id))}
              />
            ))}
        </div>
      </div>
    </>
  );
}

export function useAgentModelSync() {
  // Mirrors the Input-field watcher: when the agent carries its own model_id,
  // the composer model follows it; otherwise fall back to the last user pick.
  // Only system admins drive a composer model at all — other users' requests
  // carry no model override and the server resolves the mode's assignment.
  const { user } = useAuth();
  const canPickModel = user?.is_system_admin === true;
  const { selectedAgent, models, settings, setModel } = useChatContext();
  const ref = useRef(selectedAgent?.id);
  useEffect(() => {
    if (!canPickModel) return;
    if (ref.current === selectedAgent?.id) return;
    ref.current = selectedAgent?.id;
    const agentModel = selectedAgent?.config?.model_id;
    if (agentModel) {
      setModel(agentModel);
      return;
    }
    try {
      const last = localStorage.getItem("weknora_last_chat_model_id");
      if (last && models.some((m) => m.id === last)) setModel(last);
    } catch {
      /* ignore */
    }
  }, [canPickModel, selectedAgent?.id, selectedAgent?.config?.model_id, models, setModel]);
}

function AgentRow({
  name,
  desc,
  badge,
  active,
  onClick,
}: {
  name: string;
  desc?: string;
  badge?: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className={`flex w-full items-center gap-3 rounded-[8px] px-3 py-2 text-left transition-colors ${
        active ? "bg-surface-strong" : "hover:bg-surface-strong"
      }`}
    >
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-2 text-[14px] font-medium text-ink">
          <span className="truncate">{name}</span>
          {badge && <span className="badge-pill shrink-0">{badge}</span>}
          {active && <span className="caption shrink-0 text-muted">current</span>}
        </span>
        {desc && <span className="caption block truncate text-muted">{desc}</span>}
      </span>
    </button>
  );
}
