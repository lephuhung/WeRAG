/* Chat errors shown to the user: one translated sentence per kind of
 * failure instead of the raw "HTTP 409: {json}" a failed stream request
 * used to surface. */
import { ApiError } from "../api-client.ts";
import { TENANT_MEMBERSHIP_REQUIRED, TENANT_REQUIRED } from "../auth-recovery.ts";

/** Backend error code for "another turn is already running" (HTTP 409). */
export const ERR_TURN_RUNNING = 1005;

/** Backend error code for "this custom agent does not exist or does not
 * apply to your Tổ chức" (HTTP 404, "Tổ chức áp dụng"). */
export const ERR_AGENT_UNAVAILABLE = 2104;

export type ChatErrorKey =
  | "chat.err.turnRunning"
  | "chat.err.tenantRequired"
  | "chat.err.usage.noSubscription"
  | "chat.err.usage.expired"
  | "chat.err.usage.noSeat"
  | "chat.err.usage.limit"
  | "chat.err.usage.sessionLimit"
  | "chat.err.unauthorized"
  | "chat.err.forbidden"
  | "chat.err.notFound"
  | "chat.err.agentUnavailable"
  | "chat.err.tooMany"
  | "chat.err.tooLarge"
  | "chat.err.server"
  | "chat.err.unavailable"
  | "chat.err.network"
  | "chat.err.generic";

/** Error thrown for a non-2xx chat stream response. */
export class ChatStreamError extends Error {
  status: number;
  code?: number;
  /** Message from the backend error envelope, if any. */
  serverMessage?: string;
  /** `details.reason` of the envelope: why a usage package refused the turn. */
  reason?: string;
  /** Top-level string code of the body (`TENANT_REQUIRED`…), if any. */
  errorCode?: string;
  constructor(status: number, code?: number, serverMessage?: string, reason?: string, errorCode?: string) {
    super(serverMessage || `HTTP ${status}`);
    this.name = "ChatStreamError";
    this.status = status;
    this.code = code;
    this.serverMessage = serverMessage;
    this.reason = reason;
    this.errorCode = errorCode;
  }
}

export type ErrorEnvelope = { code?: number; message?: string; reason?: string; errorCode?: string };

/** Reads `{"error":{"code","message","details":{"reason"}}}`, the auth
 * middleware's flat `{"error":"…","code":"TENANT_REQUIRED"}` (string code
 * → `errorCode`) or `{"message"}` from a body. */
export function parseErrorEnvelope(body: string): ErrorEnvelope {
  try {
    const p = JSON.parse(body) as { error?: unknown; message?: unknown; code?: unknown };
    const flat: ErrorEnvelope = typeof p?.code === "string" && p.code ? { errorCode: p.code } : {};
    if (p && typeof p.error === "object" && p.error) {
      const e = p.error as { code?: unknown; message?: unknown; details?: unknown };
      const details = e.details && typeof e.details === "object" ? (e.details as { reason?: unknown }) : null;
      const out: ErrorEnvelope = {
        code: typeof e.code === "number" ? e.code : undefined,
        message: typeof e.message === "string" ? e.message : undefined,
        ...flat,
      };
      if (details && typeof details.reason === "string") out.reason = details.reason;
      return out;
    }
    if (typeof p?.error === "string") return { message: p.error, ...flat };
    if (typeof p?.message === "string") return { message: p.message, ...flat };
    if (flat.errorCode) return flat;
  } catch {
    /* not JSON */
  }
  return {};
}

/** Usage-package refusals (HTTP 429 with details.reason) by reason. */
const USAGE_REASON_KEYS: Record<string, ChatErrorKey> = {
  no_subscription: "chat.err.usage.noSubscription",
  expired: "chat.err.usage.expired",
  no_seat: "chat.err.usage.noSeat",
  limit_exhausted: "chat.err.usage.limit",
  session_limit: "chat.err.usage.sessionLimit",
};

function byStatus(status: number, code?: number, reason?: string, errorCode?: string): ChatErrorKey {
  // a Guest (or a stale organization) on an organization-only route; checked
  // before the 409 default below, as the flat body carries no number code
  if (errorCode === TENANT_REQUIRED || errorCode === TENANT_MEMBERSHIP_REQUIRED) return "chat.err.tenantRequired";
  if (status === 409 && (code === undefined || code === ERR_TURN_RUNNING)) return "chat.err.turnRunning";
  if (status === 429 && reason && USAGE_REASON_KEYS[reason]) return USAGE_REASON_KEYS[reason];
  if (status === 401) return "chat.err.unauthorized";
  if (status === 403) return "chat.err.forbidden";
  // the conversation is fine; its assistant is gone for this Tổ chức
  if (status === 404 && code === ERR_AGENT_UNAVAILABLE) return "chat.err.agentUnavailable";
  if (status === 404) return "chat.err.notFound";
  if (status === 413) return "chat.err.tooLarge";
  if (status === 429) return "chat.err.tooMany";
  if (status === 502 || status === 503 || status === 504) return "chat.err.unavailable";
  if (status >= 500) return "chat.err.server";
  return "chat.err.generic";
}

/** Whether a failed chat request was refused because the session used up
 * its plan's per-session cap: the UI then offers a new session that
 * carries this one's summary instead of a dead end. */
export function isSessionLimitError(e: unknown): boolean {
  if (e instanceof ChatStreamError) return e.status === 429 && e.reason === "session_limit";
  if (e instanceof ApiError) {
    return e.status === 429 && parseErrorEnvelope(JSON.stringify(e.payload ?? {})).reason === "session_limit";
  }
  return false;
}

/** The message key for a failed chat request, or null when the error
 * carries its own readable text (kept as is). */
export function chatErrorKey(e: unknown): ChatErrorKey | null {
  if (e instanceof ChatStreamError || e instanceof ApiError) {
    const env = e instanceof ChatStreamError ? e : parseErrorEnvelope(JSON.stringify(e.payload ?? {}));
    const key = byStatus(e.status, env.code, env.reason, env.errorCode);
    // a 4xx the backend explained (validation, quota…) reads better as is
    if (key === "chat.err.generic" && (e instanceof ChatStreamError ? e.serverMessage : e.message)) return null;
    return key;
  }
  if (e instanceof Error && e.message === "unauthorized") return "chat.err.unauthorized";
  if (e instanceof TypeError) return "chat.err.network"; // fetch() rejects with TypeError offline
  if (e instanceof Error && /^HTTP \d{3}/.test(e.message)) {
    return byStatus(Number(e.message.slice(5, 8)));
  }
  return null;
}

/** Translation keys a chat error turns into: the error keys plus the guest
 * variants of the usage refusals. */
export type ChatErrorTextKey = ChatErrorKey | "chat.err.guest.usageLimit" | "chat.err.guest.unavailable";

/** The text key for a chat error key as this account reads it. A guest has
 * no organization admin to contact: its usage refusals get their own texts
 * (daily allowance used up; public lookup not available for the account).
 * Members keep the usage texts as they are. */
export function chatErrorTextKey(key: ChatErrorKey, isGuest: boolean): ChatErrorTextKey;
export function chatErrorTextKey(key: ChatErrorKey | null, isGuest: boolean): ChatErrorTextKey | null;
export function chatErrorTextKey(key: ChatErrorKey | null, isGuest: boolean): ChatErrorTextKey | null {
  if (!isGuest || !key) return key;
  if (key === "chat.err.usage.limit") return "chat.err.guest.usageLimit";
  if (key === "chat.err.usage.noSubscription" || key === "chat.err.usage.expired" || key === "chat.err.usage.noSeat") {
    return "chat.err.guest.unavailable";
  }
  return key;
}

/** HTTP status and top-level string code of a failed request, when known. */
export function errorStatusAndCode(e: unknown): { status?: number; errorCode?: string } {
  if (e instanceof ChatStreamError) return { status: e.status, errorCode: e.errorCode };
  if (e instanceof ApiError) {
    return { status: e.status, errorCode: parseErrorEnvelope(JSON.stringify(e.payload ?? {})).errorCode };
  }
  return {};
}
