/* Pure helpers for the document-assistant agent settings (no "@/" imports,
 * so node --test can load them). Re-exported from ./agents. */

export const DOCUMENT_ASSISTANT_AGENT_ID = "builtin-document-assistant";
export const CHECK_DOCUMENT_FORMAT_TOOL = "check_document_format";

/** Agents that get the "Document assistant" settings section: the built-in
 * one, or any agent allowed to run the format checker. */
export function isDocumentAssistantAgent(
  id: string | null | undefined,
  config?: { allowed_tools?: string[] | null } | null,
): boolean {
  if (id === DOCUMENT_ASSISTANT_AGENT_ID) return true;
  return Array.isArray(config?.allowed_tools) && config!.allowed_tools!.includes(CHECK_DOCUMENT_FORMAT_TOOL);
}

/** Initial per-chat web-search toggle when an agent is picked: on only when
 * the agent asks for it (web_search_default_on) AND has web search at all.
 * web_search_enabled follows the agent editor's semantics: unset = enabled. */
export function defaultWebSearchFor(
  config?: { web_search_default_on?: boolean | null; web_search_enabled?: boolean | null } | null,
): boolean {
  return config?.web_search_default_on === true && config?.web_search_enabled !== false;
}

/** open_document_max_runes input: integer, clamped to 0..60000 (0 = server default). */
export const OPEN_DOCUMENT_MAX_RUNES_LIMIT = 60_000;
export function clampOpenDocumentMaxRunes(v: unknown): number {
  const n = typeof v === "number" ? v : Number(v);
  if (!Number.isFinite(n) || n <= 0) return 0;
  return Math.min(OPEN_DOCUMENT_MAX_RUNES_LIMIT, Math.round(n));
}
