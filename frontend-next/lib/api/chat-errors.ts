/* Chat errors shown to the user: one translated sentence per kind of
 * failure instead of the raw "HTTP 409: {json}" a failed stream request
 * used to surface. */
import { ApiError } from "../api-client.ts";
import { TENANT_MEMBERSHIP_REQUIRED, TENANT_REQUIRED } from "../auth-recovery.ts";
import { formatDate, formatLastDay, vnParts } from "../datetime.ts";

/** Backend error code for "another turn is already running" (HTTP 409). */
export const ERR_TURN_RUNNING = 1005;

/** Backend error code for "this custom agent does not exist or does not
 * apply to your Tổ chức" (HTTP 404, "Tổ chức áp dụng"). */
export const ERR_AGENT_UNAVAILABLE = 2104;

/** Backend error code for "a Guest's public lookup cannot run now" (HTTP
 * 503): switched off, or no public knowledge base to read (details.reason). */
export const ERR_PUBLIC_LOOKUP_UNAVAILABLE = 2105;

export type ChatErrorKey =
  | "chat.err.turnRunning"
  | "chat.err.tenantRequired"
  | "chat.err.usage.noSubscription"
  | "chat.err.usage.expired"
  | "chat.err.usage.notStarted"
  | "chat.err.usage.noSeat"
  | "chat.err.usage.limit"
  | "chat.err.usage.sessionLimit"
  | "chat.err.unauthorized"
  | "chat.err.forbidden"
  | "chat.err.notFound"
  | "chat.err.agentUnavailable"
  | "chat.err.publicLookup.off"
  | "chat.err.publicLookup.noKnowledge"
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
  /** `details.starts_at`: when a not-started package starts (ISO time). */
  startsAt?: string;
  /** The limit a limit_exhausted refusal ran into (details). */
  usage?: UsageLimitDetails;
  /** The specialist assistant whose add-on right ended (reason addon_expired). */
  addon?: AddonEndedDetails;
  constructor(status: number, code?: number, serverMessage?: string, reason?: string, errorCode?: string, startsAt?: string) {
    super(serverMessage || `HTTP ${status}`);
    this.name = "ChatStreamError";
    this.status = status;
    this.code = code;
    this.serverMessage = serverMessage;
    this.reason = reason;
    this.errorCode = errorCode;
    this.startsAt = startsAt;
  }
}

/** details of a limit_exhausted refusal: which limit, and how much of it. */
export type UsageLimitDetails = { metric: string; period: string; limit: number; used: number };
/** details of a 2104 refusal whose add-on right ended. */
export type AddonEndedDetails = { name: string; endsAt: string };

export type ErrorEnvelope = {
  code?: number;
  message?: string;
  reason?: string;
  errorCode?: string;
  startsAt?: string;
  usage?: UsageLimitDetails;
  addon?: AddonEndedDetails;
};

/** Reads `{"error":{"code","message","details":{"reason"}}}`, the auth
 * middleware's flat `{"error":"…","code":"TENANT_REQUIRED"}` (string code
 * → `errorCode`) or `{"message"}` from a body. */
export function parseErrorEnvelope(body: string): ErrorEnvelope {
  try {
    const p = JSON.parse(body) as { error?: unknown; message?: unknown; code?: unknown };
    const flat: ErrorEnvelope = typeof p?.code === "string" && p.code ? { errorCode: p.code } : {};
    if (p && typeof p.error === "object" && p.error) {
      const e = p.error as { code?: unknown; message?: unknown; details?: unknown };
      const details =
        e.details && typeof e.details === "object"
          ? (e.details as { reason?: unknown; starts_at?: unknown; metric?: unknown; period?: unknown; limit?: unknown; used?: unknown; agent_name?: unknown; ends_at?: unknown })
          : null;
      const out: ErrorEnvelope = {
        code: typeof e.code === "number" ? e.code : undefined,
        message: typeof e.message === "string" ? e.message : undefined,
        ...flat,
      };
      if (details && typeof details.reason === "string") out.reason = details.reason;
      // an "expired" refusal with a start date: the package starts later
      if (out.reason === "expired" && typeof details?.starts_at === "string" && details.starts_at) {
        out.reason = "not_started";
        out.startsAt = details.starts_at;
      }
      if (out.reason === "limit_exhausted" && typeof details?.metric === "string" && typeof details.period === "string" && typeof details.limit === "number") {
        out.usage = { metric: details.metric, period: details.period, limit: details.limit, used: typeof details.used === "number" ? details.used : details.limit };
      }
      if (out.reason === "addon_expired" && typeof details?.ends_at === "string") {
        out.addon = { name: typeof details.agent_name === "string" ? details.agent_name : "", endsAt: details.ends_at };
      }
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
  not_started: "chat.err.usage.notStarted",
  no_seat: "chat.err.usage.noSeat",
  limit_exhausted: "chat.err.usage.limit",
  session_limit: "chat.err.usage.sessionLimit",
};

/** The key for a backend error code that names its failure, whatever HTTP
 * status carries it, or null for a generic code. Checked before the status
 * so a specific refusal sent with 5xx (or 4xx) is not read as "server
 * unavailable" or "not found". */
function byCode(code?: number, reason?: string): ChatErrorKey | null {
  if (code === ERR_TURN_RUNNING) return "chat.err.turnRunning";
  // the conversation is fine; its assistant is gone for this Tổ chức
  if (code === ERR_AGENT_UNAVAILABLE) return "chat.err.agentUnavailable";
  // a Guest's lookup: nothing to pick instead, only to wait
  if (code === ERR_PUBLIC_LOOKUP_UNAVAILABLE) {
    return reason === "public_lookup_no_knowledge" ? "chat.err.publicLookup.noKnowledge" : "chat.err.publicLookup.off";
  }
  return null;
}

function byStatus(status: number, code?: number, reason?: string, errorCode?: string): ChatErrorKey {
  // a Guest (or a stale organization) on an organization-only route; checked
  // before the 409 default below, as the flat body carries no number code
  if (errorCode === TENANT_REQUIRED || errorCode === TENANT_MEMBERSHIP_REQUIRED) return "chat.err.tenantRequired";
  const known = byCode(code, reason);
  if (known) return known;
  if (status === 409 && code === undefined) return "chat.err.turnRunning";
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
 * variants of the usage refusals and the role-aware usage texts. */
export type ChatErrorTextKey =
  | ChatErrorKey
  | "chat.err.guest.usageLimit"
  | "chat.err.guest.unavailable"
  | "chat.err.usage.notStartedOn"
  | "chat.err.usage.noSubscriptionAdmin"
  | "chat.err.usage.noSubscriptionSelf"
  | "chat.err.usage.expiredBuy"
  | "chat.err.usage.noSeatAdmin"
  | "chat.err.usage.limitBuy"
  | "chat.err.usage.limitDetail"
  | "chat.err.usage.limitDetailBuy"
  | "chat.err.agentAddonExpired"
  | "chat.err.agentAddonExpiredBuy";

/** The text key for a chat error key as this account reads it. A guest has
 * no organization admin to contact: its usage refusals get their own texts
 * (daily allowance used up; public lookup not available for the account).
 * Members keep the usage texts as they are. */
export function chatErrorTextKey(key: ChatErrorKey, isGuest: boolean): ChatErrorTextKey;
export function chatErrorTextKey(key: ChatErrorKey | null, isGuest: boolean): ChatErrorTextKey | null;
export function chatErrorTextKey(key: ChatErrorKey | null, isGuest: boolean): ChatErrorTextKey | null {
  if (!isGuest || !key) return key;
  if (key === "chat.err.usage.limit") return "chat.err.guest.usageLimit";
  if (
    key === "chat.err.usage.noSubscription" ||
    key === "chat.err.usage.expired" ||
    key === "chat.err.usage.notStarted" ||
    key === "chat.err.usage.noSeat"
  ) {
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

/** Who reads the error: decides between "buy or renew" (a person without a
 * Tổ chức, or its admin) and "ask your Tổ chức admin" (a member). */
export type ChatErrorViewer = { isTenantAdmin?: boolean; hasTenant?: boolean };

/** What the error bubble offers: open the billing page, or the Tổ chức
 * usage page where an admin gives out chỗ. */
export type ChatErrorAction = "billing" | "seats";

export type ChatErrorText = {
  key: ChatErrorTextKey;
  vars?: Record<string, string>;
  /** Variables that are themselves translation keys (unit, period). */
  varKeys?: Record<string, string>;
  action?: ChatErrorAction;
};

/** The VN calendar day a limit of `period` counts again from: tomorrow for
 * a day limit, the 1st of next month for a month limit ("" otherwise). */
export function limitResetDate(period: string, now: Date = new Date()): string {
  const p = vnParts(now);
  if (!p) return "";
  let y = p.year;
  let m = p.month;
  let d = p.day;
  if (period === "day") {
    // noon UTC of the VN day, plus a day: no zone edge to trip on
    const next = new Date(Date.UTC(y, m - 1, d, 12) + 24 * 60 * 60 * 1000);
    y = next.getUTCFullYear();
    m = next.getUTCMonth() + 1;
    d = next.getUTCDate();
  } else if (period === "month") {
    m += 1;
    if (m > 12) {
      m = 1;
      y += 1;
    }
    d = 1;
  } else {
    return "";
  }
  return `${String(d).padStart(2, "0")}/${String(m).padStart(2, "0")}/${y}`;
}

/** The text key, its variables and the bubble's action for a chat error as
 * this account reads it: chatErrorTextKey, plus the start date of a
 * not-started package, the limit that ran out with the day it counts again,
 * the specialist assistant whose right ended, and who can act (buy / renew
 * for a person without a Tổ chức or its admin; ask the admin otherwise). */
export function chatErrorText(
  key: ChatErrorKey,
  isGuest: boolean,
  err: unknown,
  viewer: ChatErrorViewer = {},
  now: Date = new Date(),
): ChatErrorText {
  const textKey = chatErrorTextKey(key, isGuest);
  const stream = err instanceof ChatStreamError ? err : null;
  const env = stream ?? (err instanceof ApiError ? parseErrorEnvelope(JSON.stringify(err.payload ?? {})) : null);
  // a guest's own (personal) package: they buy it themselves, as the banner says
  if (textKey === "chat.err.guest.usageLimit") return { key: textKey, action: "billing" };
  // a guest's other refusals have their own texts; an add-on of their own
  // package that ran out reads as for anyone buying for themselves
  if (isGuest && textKey !== "chat.err.agentUnavailable") return { key: textKey };
  const self = isGuest || !viewer.hasTenant;
  const canBuy = self || !!viewer.isTenantAdmin;
  switch (textKey) {
    case "chat.err.usage.notStarted": {
      const date = stream?.startsAt ? formatDate(stream.startsAt) : "";
      return date ? { key: "chat.err.usage.notStartedOn", vars: { date } } : { key: textKey };
    }
    case "chat.err.usage.noSubscription":
      if (self) return { key: "chat.err.usage.noSubscriptionSelf", action: "billing" };
      if (viewer.isTenantAdmin) return { key: "chat.err.usage.noSubscriptionAdmin", action: "billing" };
      return { key: textKey };
    case "chat.err.usage.expired":
      return canBuy ? { key: "chat.err.usage.expiredBuy", action: "billing" } : { key: textKey };
    case "chat.err.usage.noSeat":
      return viewer.isTenantAdmin ? { key: "chat.err.usage.noSeatAdmin", action: "seats" } : { key: textKey };
    case "chat.err.usage.limit": {
      const u = env?.usage;
      const date = u ? limitResetDate(u.period, now) : "";
      if (u && date && u.limit > 0) {
        return {
          key: canBuy ? "chat.err.usage.limitDetailBuy" : "chat.err.usage.limitDetail",
          vars: { limit: u.limit.toLocaleString("vi-VN"), date },
          varKeys: {
            unit: u.metric === "tokens" ? "pbn.tokens" : "pbn.questions",
            period: u.period === "day" ? "chat.err.usage.perDay" : "chat.err.usage.perMonth",
          },
          ...(canBuy ? { action: "billing" as const } : {}),
        };
      }
      return canBuy ? { key: "chat.err.usage.limitBuy", action: "billing" } : { key: textKey };
    }
    case "chat.err.agentUnavailable": {
      const a = env?.reason === "addon_expired" ? env.addon : undefined;
      const date = a ? formatLastDay(a.endsAt) : "";
      if (!a || !date) return { key: textKey };
      return canBuy
        ? { key: "chat.err.agentAddonExpiredBuy", vars: { name: a.name, date }, action: "billing" }
        : { key: "chat.err.agentAddonExpired", vars: { name: a.name, date } };
    }
  }
  return { key: textKey };
}
