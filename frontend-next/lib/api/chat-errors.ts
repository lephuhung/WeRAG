/* Chat errors shown to the user: one translated sentence per kind of
 * failure instead of the raw "HTTP 409: {json}" a failed stream request
 * used to surface. */
import { ApiError } from "../api-client.ts";

/** Backend error code for "another turn is already running" (HTTP 409). */
export const ERR_TURN_RUNNING = 1005;

export type ChatErrorKey =
  | "chat.err.turnRunning"
  | "chat.err.usage.noSubscription"
  | "chat.err.usage.expired"
  | "chat.err.usage.noSeat"
  | "chat.err.usage.limit"
  | "chat.err.unauthorized"
  | "chat.err.forbidden"
  | "chat.err.notFound"
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
  constructor(status: number, code?: number, serverMessage?: string, reason?: string) {
    super(serverMessage || `HTTP ${status}`);
    this.name = "ChatStreamError";
    this.status = status;
    this.code = code;
    this.serverMessage = serverMessage;
    this.reason = reason;
  }
}

/** Reads `{"error":{"code","message","details":{"reason"}}}` (or
 * `{"message"}`) from a body. */
export function parseErrorEnvelope(body: string): { code?: number; message?: string; reason?: string } {
  try {
    const p = JSON.parse(body) as { error?: unknown; message?: unknown };
    if (p && typeof p.error === "object" && p.error) {
      const e = p.error as { code?: unknown; message?: unknown; details?: unknown };
      const details = e.details && typeof e.details === "object" ? (e.details as { reason?: unknown }) : null;
      const out: { code?: number; message?: string; reason?: string } = {
        code: typeof e.code === "number" ? e.code : undefined,
        message: typeof e.message === "string" ? e.message : undefined,
      };
      if (details && typeof details.reason === "string") out.reason = details.reason;
      return out;
    }
    if (typeof p?.error === "string") return { message: p.error };
    if (typeof p?.message === "string") return { message: p.message };
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
};

function byStatus(status: number, code?: number, reason?: string): ChatErrorKey {
  if (status === 409 && (code === undefined || code === ERR_TURN_RUNNING)) return "chat.err.turnRunning";
  if (status === 429 && reason && USAGE_REASON_KEYS[reason]) return USAGE_REASON_KEYS[reason];
  if (status === 401) return "chat.err.unauthorized";
  if (status === 403) return "chat.err.forbidden";
  if (status === 404) return "chat.err.notFound";
  if (status === 413) return "chat.err.tooLarge";
  if (status === 429) return "chat.err.tooMany";
  if (status === 502 || status === 503 || status === 504) return "chat.err.unavailable";
  if (status >= 500) return "chat.err.server";
  return "chat.err.generic";
}

/** The message key for a failed chat request, or null when the error
 * carries its own readable text (kept as is). */
export function chatErrorKey(e: unknown): ChatErrorKey | null {
  if (e instanceof ChatStreamError || e instanceof ApiError) {
    const env = e instanceof ChatStreamError ? e : parseErrorEnvelope(JSON.stringify(e.payload ?? {}));
    const key = byStatus(e.status, env.code, env.reason);
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
