"use client";

import { useEffect, useRef } from "react";
import { useAuth } from "@/lib/auth";
import {
  BUILTIN_DOCUMENT_ASSISTANT_ID,
  BUILTIN_QUICK_ANSWER_ID,
  BUILTIN_SMART_REASONING_ID,
  useChatContext,
} from "@/lib/chat-context";
import { useT } from "@/lib/i18n";
import { IconChevronDown, IconLock } from "@/components/icons";
import Link from "next/link";

/* Ports AgentSelector.vue: builtin quick-answer / smart-reasoning + custom
 * agents. Selecting writes selectedAgentId into the chat context, mirroring
 * settings.selectAgent. (Shared agents were dropped with the organization
 * refactor — the backend no longer exposes them.)
 */

export function AgentModeButton({ onOpen, locked = false }: { onOpen: () => void; locked?: boolean }) {
  const { selectedAgent, isAgentStreamMode } = useChatContext();
  const { t } = useT();
  const label = selectedAgent?.name ?? (isAgentStreamMode ? "Agent mode" : "Normal mode");
  return (
    <button
      onClick={onOpen}
      aria-haspopup="dialog"
      className={`flex h-7 max-w-[120px] items-center gap-1.5 rounded-full px-2.5 text-[12.5px] font-medium transition-colors sm:max-w-[240px] ${
        isAgentStreamMode
          ? "bg-[#edf5ff] text-[#0f2d59] dark:bg-[#15273f] dark:text-[#dce9fe]"
          : "text-body hover:bg-surface-strong hover:text-ink"
      }`}
      title={locked ? t("docws.modeLockedShort") : (selectedAgent?.description ?? label)}
    >
      <span className="min-w-0 truncate">{label}</span>
      {locked ? (
        <IconLock className="h-3.5 w-3.5 shrink-0" />
      ) : (
        <IconChevronDown className="h-3.5 w-3.5 shrink-0" />
      )}
    </button>
  );
}

/* In place of the agent picker when the conversation holds an open
 * document: the mode is fixed to the document assistant (another mode would
 * drop the editor), and a new conversation is the way to another mode. */
export function AgentLockNotice({
  open,
  fileName,
  onClose,
}: {
  open: boolean;
  fileName: string;
  onClose: () => void;
}) {
  const { t } = useT();
  if (!open) return null;
  return (
    <>
      <div className="fixed inset-0 z-40" onClick={onClose} />
      <div
        role="dialog"
        aria-label={t("docws.modeLockedShort")}
        className="card absolute bottom-full left-0 z-50 mb-2 w-[300px] max-w-[calc(100vw-2.5rem)] p-3.5 shadow-[0_4px_16px_rgba(0,0,0,0.08)]"
      >
        <p className="flex items-center gap-1.5 text-[13px] font-medium text-ink">
          <IconLock className="h-3.5 w-3.5 shrink-0" />
          {t("docws.modeLockedShort")}
        </p>
        <p className="mt-1.5 text-[12.5px] leading-snug text-muted">
          {t("docws.modeLockedDesc", { name: fileName })}
        </p>
        <Link href="/platform/creatChat" onClick={onClose} className="btn btn-outline btn-sm mt-3 w-full">
          {t("docws.modeLockedNewChat")}
        </Link>
      </div>
    </>
  );
}

export function AgentSelector({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { agents, settings, selectAgent } = useChatContext();
  const { t } = useT();

  if (!open) return null;
  const builtins = agents.filter((a) => a.is_builtin);
  const customs = agents.filter((a) => !a.is_builtin);
  const quick = builtins.find((a) => a.id === BUILTIN_QUICK_ANSWER_ID);
  const smart = builtins.find((a) => a.id === BUILTIN_SMART_REASONING_ID);
  // Backend order is kept (wiki/data builtins first); the document
  // assistant is pinned last among the builtins.
  const otherBuiltins = builtins
    .filter((a) => a.id !== BUILTIN_QUICK_ANSWER_ID && a.id !== BUILTIN_SMART_REASONING_ID)
    .sort(
      (a, b) =>
        Number(a.id === BUILTIN_DOCUMENT_ASSISTANT_ID) - Number(b.id === BUILTIN_DOCUMENT_ASSISTANT_ID),
    );
  const builtinName = (a: { id: string; name: string }) =>
    a.name || (a.id === BUILTIN_DOCUMENT_ASSISTANT_ID ? t("docws.agentName") : a.id);
  const builtinDesc = (a: { id: string; description?: string }) =>
    a.description || (a.id === BUILTIN_DOCUMENT_ASSISTANT_ID ? t("docws.agentDesc") : undefined);

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
            <AgentRow key={a.id} name={builtinName(a)} desc={builtinDesc(a)} active={current(a.id)} onClick={() => pick(a.id)} />
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
