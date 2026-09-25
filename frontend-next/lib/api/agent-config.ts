import type { CustomAgentConfig } from "./agents.ts";

/* B-2a(1): the agent editor only models a subset of CustomAgentConfig, but
 * the backend PUT replaces the whole config — sending just the edited subset
 * would silently drop fields the editor has no UI for (allowed_tools,
 * selected_skills, sandbox_config_id, chat_parser_engine_rules, …).
 *
 * On EDIT the payload is merged over the stored config: unknown fields are
 * preserved, edited fields win, and an explicitly `undefined` edited value
 * clears the stored key (so disabling KB/MCP/image/audio actually removes
 * the stale list instead of leaving it behind). On CREATE there is no stored
 * config, so the current defaults go out untouched. */

export function mergeAgentConfigForEdit(
  existing: CustomAgentConfig | null | undefined,
  edited: CustomAgentConfig,
): CustomAgentConfig {
  const merged: Record<string, unknown> = { ...(existing ?? {}) };
  for (const [key, value] of Object.entries(edited)) {
    if (value === undefined) {
      delete merged[key];
    } else {
      merged[key] = value;
    }
  }
  return merged as CustomAgentConfig;
}

export function buildAgentConfigForSave(
  existing: CustomAgentConfig | null | undefined,
  edited: CustomAgentConfig,
  mode: "create" | "edit",
): CustomAgentConfig {
  return mode === "create" ? edited : mergeAgentConfigForEdit(existing, edited);
}
