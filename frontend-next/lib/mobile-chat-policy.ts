/** A viewport rule, not a persisted change to the desktop agent preference. */
export function resolveChatMode(agentId: string, mobile: boolean, newChat: boolean, sessionAgentId: string | null = null) {
  if (!mobile) return { agentId, readOnly: false };
  if (!newChat && sessionAgentId === "builtin-document-assistant") return { agentId: sessionAgentId, readOnly: true };
  return { agentId: "builtin-smart-reasoning", readOnly: false };
}

export function availableChatChoices<T extends { handoff?: unknown }>(options: T[], mobile: boolean): T[] {
  return mobile ? options.filter((option) => !option.handoff) : options;
}
