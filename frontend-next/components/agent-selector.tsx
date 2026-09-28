"use client";

import { useEffect, useRef } from "react";
import { useAuth } from "@/lib/auth";
import {
  BUILTIN_QUICK_ANSWER_ID,
  BUILTIN_SMART_REASONING_ID,
  useChatContext,
} from "@/lib/chat-context";
import { IconChevronDown } from "@/components/icons";

/* Ports AgentSelector.vue: builtin quick-answer / smart-reasoning + custom
 * agents. Selecting writes selectedAgentId into the chat context, mirroring
 * settings.selectAgent. (Shared agents were dropped with the organization
 * refactor — the backend no longer exposes them.)
 */

export function AgentModeButton({ onOpen }: { onOpen: () => void }) {
  const { selectedAgent, isAgentStreamMode } = useChatContext();
  const label = selectedAgent?.name ?? (isAgentStreamMode ? "Agent mode" : "Normal mode");
  return (
    <button
      onClick={onOpen}
      className={`flex h-7 max-w-[120px] items-center gap-1.5 rounded-full px-2.5 text-[12.5px] font-medium transition-colors sm:max-w-[240px] ${
        isAgentStreamMode
          ? "bg-[#edf5ff] text-[#0f2d59] dark:bg-[#15273f] dark:text-[#dce9fe]"
          : "text-body hover:bg-surface-strong hover:text-ink"
      }`}
      title={selectedAgent?.description ?? label}
    >
      <span className="min-w-0 truncate">{label}</span>
      <IconChevronDown className="h-3.5 w-3.5 shrink-0" />
    </button>
  );
}

export function AgentSelector({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { agents, settings, selectAgent } = useChatContext();

  if (!open) return null;
  const builtins = agents.filter((a) => a.is_builtin);
  const customs = agents.filter((a) => !a.is_builtin);
  const quick = builtins.find((a) => a.id === BUILTIN_QUICK_ANSWER_ID);
  const smart = builtins.find((a) => a.id === BUILTIN_SMART_REASONING_ID);
  const otherBuiltins = builtins.filter(
    (a) => a.id !== BUILTIN_QUICK_ANSWER_ID && a.id !== BUILTIN_SMART_REASONING_ID,
  );

  const pick = (id: string) => {
    selectAgent(id);
    onClose();
  };
  const current = (id: string) => settings.selectedAgentId === id;

  return (
    <>
      <div className="fixed inset-0 z-40" onClick={onClose} />
      <div className="card absolute bottom-full left-0 z-50 mb-2 flex max-h-[320px] w-[300px] max-w-[calc(100vw-2.5rem)] flex-col overflow-hidden shadow-[0_4px_16px_rgba(0,0,0,0.08)]">
        <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
          {quick && (
            <AgentRow name="Normal mode" desc={quick.description ?? "Fast RAG answers"} active={current(quick.id)} onClick={() => pick(quick.id)} />
          )}
          {smart && (
            <AgentRow name="Agent mode" desc={smart.description ?? "Reasoning with tools"} active={current(smart.id)} onClick={() => pick(smart.id)} />
          )}
          {otherBuiltins.map((a) => (
            <AgentRow key={a.id} name={a.name} desc={a.description} active={current(a.id)} onClick={() => pick(a.id)} />
          ))}
          {customs.length > 0 && (
            <div className="caption-uppercase px-3 pb-1 pt-2 text-muted-soft">My agents</div>
          )}
          {customs.map((a) => (
            <AgentRow key={a.id} name={a.name} desc={a.description} active={current(a.id)} onClick={() => pick(a.id)} />
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
  active,
  onClick,
}: {
  name: string;
  desc?: string;
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
          {active && <span className="caption shrink-0 text-muted">current</span>}
        </span>
        {desc && <span className="caption block truncate text-muted">{desc}</span>}
      </span>
    </button>
  );
}
