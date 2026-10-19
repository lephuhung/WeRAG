/* Guest chat: a signed-in account with no active organization membership
 * (GET /auth/me membership_state "guest") may chat with one assistant only,
 * the public-lookup assistant, which answers from the platform's public
 * legal knowledge bases. The server forces the agent and strips every other
 * scope for a guest; the UI offers none of it either, and keeps a guest's
 * calls inside the tenantless chat routes (sessions, agent-chat, messages,
 * GET /agents, GET /me/usage).
 *
 * Shared with the Word add-in (scripts/shared-files.json there). */

import { TENANT_MEMBERSHIP_REQUIRED, TENANT_REQUIRED } from "./auth-recovery.ts";
import { errorStatusAndCode } from "./api/chat-errors.ts";

/** Built-in agent a guest chats with ("Tra cứu văn bản công khai"). */
export const BUILTIN_PUBLIC_LOOKUP_ID = "builtin-public-lookup";

/** Whether the account is a Guest. Only meaningful once /auth/me answered:
 * before that the membership state defaults to "guest". A SuperAdmin is
 * never a guest, with or without a selected organization. */
export function isGuestAccount(input: {
  loggedIn: boolean;
  membershipState?: string | null;
  isSystemAdmin?: boolean | null;
}): boolean {
  return input.loggedIn && input.membershipState === "guest" && input.isSystemAdmin !== true;
}

/** The chat-input fields a guest's turn may carry. */
type ScopedChatSettings = {
  selectedKnowledgeBases: string[];
  selectedFiles: string[];
  selectedFileKbMap: Record<string, string>;
  selectedTags: unknown[];
  selectedMCPServices: string[];
  selectedSkills: string[];
  isAgentEnabled: boolean;
  selectedAgentId: string;
  webSearchEnabled: boolean;
  localBrowserEnabled: boolean;
  selectedChatModelId: string;
};

/** A guest's effective chat settings: the public-lookup agent on the agent
 * pipeline and nothing else — no knowledge bases, files, tags, MCP, skills,
 * web or browser search, and no model override. The stored settings (a
 * member's choices left in this browser) are kept untouched. */
export function guestChatSettings<T extends ScopedChatSettings>(stored: T): T {
  return {
    ...stored,
    selectedKnowledgeBases: [],
    selectedFiles: [],
    selectedFileKbMap: {},
    selectedTags: [],
    selectedMCPServices: [],
    selectedSkills: [],
    isAgentEnabled: true,
    selectedAgentId: BUILTIN_PUBLIC_LOOKUP_ID,
    webSearchEnabled: false,
    localBrowserEnabled: false,
    selectedChatModelId: "",
  };
}

/** Whether a failed call means the account kind may have changed while the
 * chat was open (a guest joined or created an organization, a member lost
 * the last membership), so /auth/me should be read again and the chat reset:
 * - TENANT_REQUIRED / TENANT_MEMBERSHIP_REQUIRED on any call;
 * - for a guest, a 404 on a session call: a guest's sessions live in the
 *   public portal, which a member is no longer served from.
 * Pass `sessionCall: false` for a call that does not name a session. */
export function shouldRefreshAccount(e: unknown, opts: { isGuest: boolean; sessionCall?: boolean }): boolean {
  const { status, errorCode } = errorStatusAndCode(e);
  if (errorCode === TENANT_REQUIRED || errorCode === TENANT_MEMBERSHIP_REQUIRED) return true;
  return opts.isGuest && opts.sessionCall !== false && status === 404;
}

/** Whether a stored guest conversation is still usable after the account
 * kind moved from `before` to `after` (membership states of /auth/me). Only
 * a guest that stays a guest keeps it: a guest that became a member (or a
 * SuperAdmin) can no longer open its portal session. */
export function guestSessionSurvives(before: string | null | undefined, after: string | null | undefined): boolean {
  return before === "guest" && after === "guest";
}
