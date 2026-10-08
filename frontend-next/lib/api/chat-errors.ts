/* Chat errors shown to the user: one translated sentence per kind of
 * failure instead of the raw "HTTP 409: {json}" a failed stream request
 * used to surface. */
import { ApiError } from "../api-client.ts";

/** Backend error code for "another turn is already running" (HTTP 409). */
export const ERR_TURN_RUNNING = 1005;

export type ChatErrorKey =
  | "chat.err.turnRunning"
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
  constructor(status: number, code?: number, serverMessage?: string) {
    super(serverMessage || `HTTP ${status}`);
    this.name = "ChatStreamError";
    this.status = status;
    this.code = code;
    this.serverMessage = serverMessage;
  }
}

/** Reads `{"error":{"code","message"}}` (or `{"message"}`) from a body. */
export function parseErrorEnvelope(body: string): { code?: number; message?: string } {
  try {
    const p = JSON.parse(body) as { error?: unknown; message?: unknown };
    if (p && typeof p.error === "object" && p.error) {
      const e = p.error as { code?: unknown; message?: unknown };
      return {
        code: typeof e.code === "number" ? e.code : undefined,
        message: typeof e.message === "string" ? e.message : undefined,
      };
    }
    if (typeof p?.error === "string") return { message: p.error };
    if (typeof p?.message === "string") return { message: p.message };
  } catch {
    /* not JSON */
  }
  return {};
}

function byStatus(status: number, code?: number): ChatErrorKey {
  if (status === 409 && (code === undefined || code === ERR_TURN_RUNNING)) return "chat.err.turnRunning";
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
    const code = e instanceof ChatStreamError ? e.code : parseErrorEnvelope(JSON.stringify(e.payload ?? {})).code;
    const key = byStatus(e.status, code);
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
