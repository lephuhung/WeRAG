/* Errors shown to the user: one localized sentence per kind of failure
 * instead of the backend's raw (usually English) `e.message`.
 *
 * Order: a backend code that names its failure → its own text; a session
 * that ended (401), no network, a timeout, an aborted request → their text
 * (aborted: no text at all); a backend message already in Vietnamese →
 * shown as is (it explains the specific case better than a generic line);
 * the HTTP status class (403, 404, 409, 413, 429, 5xx) → its text; else the
 * caller's own fallback ("Không tải được …").
 *
 * Pure: no React, no i18n import at runtime (type only), so it runs under
 * `node --experimental-strip-types --test lib/user-error.test.ts`. */
import { ApiError } from "./api-client.ts";
import { ChatStreamError, parseErrorEnvelope, type ErrorEnvelope } from "./api/chat-errors.ts";
import type { LocaleKey } from "./i18n.tsx";

export type Translate = (k: LocaleKey, vars?: Record<string, string | number>) => string;

/** An error whose message was written for the user (already translated):
 * always shown as is. Throw it from a try block whose catch uses
 * userErrorMessage when the text comes from t(…). */
export class UserFacingError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "UserFacingError";
  }
}

/** Backend number codes (internal/errors/errors.go) with their own text. */
const CODE_KEYS: Record<number, LocaleKey> = {
  1009: "err.timeout",
  2000: "err.tenantNotFound",
  2001: "err.tenantExists",
  2002: "err.tenantInactive",
  2003: "err.tenantNameRequired",
  2005: "err.tenantCreationDisabled",
  2006: "err.tenantNameTooLong",
  2104: "chat.err.agentUnavailable",
  2200: "err.vectorStoreInvalid",
  2201: "err.vectorStoreUnavailable",
  2300: "err.modelInUse",
};

/** Top-level string codes (auth middleware, fork handler). */
const STRING_CODE_KEYS: Record<string, LocaleKey> = {
  TENANT_REQUIRED: "chat.err.tenantRequired",
  TENANT_MEMBERSHIP_REQUIRED: "chat.err.tenantRequired",
  ACCOUNT_DISABLED: "auth.accountDisabled",
  FORK_SOURCE_BUSY: "err.forkSourceBusy",
};

/** Usage-package refusals (HTTP 429, details.reason). */
const USAGE_REASON_KEYS: Record<string, LocaleKey> = {
  no_subscription: "chat.err.usage.noSubscription",
  expired: "chat.err.usage.expired",
  not_started: "chat.err.usage.notStarted",
  no_seat: "chat.err.usage.noSeat",
  limit_exhausted: "chat.err.usage.limit",
  session_limit: "chat.err.usage.sessionLimit",
};

/** Letters only Vietnamese uses among the Latin scripts the backend writes
 * in (tone marks and ă â đ ê ô ơ ư). */
const VIETNAMESE_RE =
  /[àáảãạăắằẳẵặâấầẩẫậèéẻẽẹêếềểễệìíỉĩịòóỏõọôốồổỗộơớờởỡợùúủũụưứừửữựỳýỷỹỵđ]/i;

export function isVietnameseText(s: string): boolean {
  return VIETNAMESE_RE.test(s);
}

/** What a failure turns into: nothing (aborted), a translation key, a text
 * to show as is, or the caller's fallback. */
export type UserErrorResult =
  | { kind: "aborted" }
  | { kind: "key"; key: LocaleKey }
  | { kind: "text"; text: string }
  | { kind: "fallback" };

type Parts = { status?: number; env: ErrorEnvelope; message: string };

function partsOf(e: unknown): Parts | null {
  if (e instanceof ChatStreamError) {
    return { status: e.status, env: e, message: e.serverMessage ?? "" };
  }
  if (e instanceof ApiError) {
    const env = e.payload !== undefined ? parseErrorEnvelope(JSON.stringify(e.payload ?? {})) : {};
    return { status: e.status, env, message: env.message ?? e.message ?? "" };
  }
  if (e && typeof e === "object" && "status" in e && typeof (e as { status: unknown }).status === "number" && e instanceof Error) {
    // DocumentWorkspaceError and similar wrappers: status (+ payload) kept
    const payload = (e as { payload?: unknown }).payload;
    const env = payload !== undefined ? parseErrorEnvelope(JSON.stringify(payload ?? {})) : {};
    return { status: (e as { status: number }).status, env, message: env.message ?? e.message };
  }
  return null;
}

function isAbort(e: unknown): boolean {
  if (e && typeof e === "object" && (e as { name?: unknown }).name === "AbortError") return true;
  if (e instanceof ApiError && e.status === 0 && /abort/i.test(e.message)) return true;
  return false;
}

/** Transport failures with status 0 (xhr uploads, refresh) and fetch()'s
 * TypeError when offline. */
function transportKey(e: unknown, status: number | undefined, message: string): LocaleKey | null {
  if (e && typeof e === "object" && (e as { name?: unknown }).name === "TimeoutError") return "err.timeout";
  if (status === 0 || status === undefined) {
    if (/timed? ?out|timeout/i.test(message)) return "err.timeout";
    if (status === 0 || /failed to fetch|network|load failed|fetch failed/i.test(message)) return "err.network";
  }
  return null;
}

function statusKey(status: number): LocaleKey | null {
  if (status === 401) return "err.signInAgain";
  if (status === 403) return "err.forbidden";
  if (status === 404) return "err.notFound";
  if (status === 408) return "err.timeout";
  if (status === 409) return "err.conflict";
  if (status === 413) return "err.tooLarge";
  if (status === 429) return "err.tooMany";
  if (status === 502 || status === 503 || status === 504) return "err.unavailable";
  if (status >= 500) return "err.server";
  return null;
}

/** The pure core: how a failure reads to the user. */
export function classifyUserError(e: unknown): UserErrorResult {
  if (isAbort(e)) return { kind: "aborted" };
  if (e instanceof UserFacingError) return e.message ? { kind: "text", text: e.message } : { kind: "fallback" };
  if (e instanceof Error && e.name === "ModelInUseError") return { kind: "key", key: "err.modelInUse" };
  if (typeof e === "string") return isVietnameseText(e) ? { kind: "text", text: e } : { kind: "fallback" };

  const parts = partsOf(e);
  const message = parts?.message ?? (e instanceof Error ? e.message : "");
  let status = parts?.status;
  const env = parts?.env ?? {};

  // "HTTP 404" / "HTTP 500: {…}" thrown by raw fetch callers
  if (status === undefined && /^HTTP \d{3}\b/.test(message)) status = Number(message.slice(5, 8));
  // "unauthorized" thrown by the stream client when the refresh failed
  if (status === undefined && message === "unauthorized") status = 401;

  // a code that names its failure, whatever status carries it
  if (env.errorCode && STRING_CODE_KEYS[env.errorCode]) return { kind: "key", key: STRING_CODE_KEYS[env.errorCode] };
  if (env.code === 2105) {
    return { kind: "key", key: env.reason === "public_lookup_no_knowledge" ? "chat.err.publicLookup.noKnowledge" : "chat.err.publicLookup.off" };
  }
  if (env.code !== undefined && CODE_KEYS[env.code]) return { kind: "key", key: CODE_KEYS[env.code] };
  if (status === 429 && env.reason && USAGE_REASON_KEYS[env.reason]) return { kind: "key", key: USAGE_REASON_KEYS[env.reason] };

  // the session ended: signing in again is the only way on
  if (status === 401) return { kind: "key", key: "err.signInAgain" };
  const transport = transportKey(e, status, message);
  if (transport) return { kind: "key", key: transport };

  if (message && isVietnameseText(message)) return { kind: "text", text: message };

  if (status !== undefined) {
    const k = statusKey(status);
    if (k) return { kind: "key", key: k };
  }
  return { kind: "fallback" };
}

export type UserErrorOptions = {
  /** Put the caller's fallback in front of a specific text ("Không mở
   * được tài liệu: Bạn không có quyền …"), for toasts that used to read
   * "<what failed>: <raw message>". */
  prefix?: boolean;
  vars?: Record<string, string | number>;
};

/** Localized text for a failure. "" for an aborted request (nothing to
 * show; toast.error ignores empty text). */
export function userErrorMessage(e: unknown, t: Translate, fallbackKey: LocaleKey, opts: UserErrorOptions = {}): string {
  const r = classifyUserError(e);
  if (r.kind === "aborted") return "";
  const fallback = t(fallbackKey, opts.vars);
  if (r.kind === "fallback") return fallback;
  const text = r.kind === "key" ? t(r.key) : r.text;
  return opts.prefix ? `${fallback.replace(/[.:]\s*$/, "")}: ${text}` : text;
}

/** Sign-up failures (POST /auth/register, /auth/register-by-invite): the
 * backend answers in English (password policy sentinels, duplicate
 * account, gin binding errors in error.details), so each known case gets
 * its own sentence. null → not a sign-up specific failure; use
 * userErrorMessage. */
export function registerErrorKey(e: unknown): LocaleKey | null {
  let message = "";
  let details = "";
  if (e instanceof ApiError) {
    message = e.message ?? "";
    const p = e.payload as { error?: unknown; message?: unknown } | undefined;
    if (p && typeof p === "object") {
      if (p.error && typeof p.error === "object") {
        const err = p.error as { message?: unknown; details?: unknown };
        if (typeof err.message === "string") message = err.message;
        if (typeof err.details === "string") details = err.details;
      } else if (typeof p.error === "string") {
        message = p.error;
      } else if (typeof p.message === "string") {
        message = p.message;
      }
    }
  } else if (e instanceof Error) {
    message = e.message;
  } else {
    return null;
  }
  const m = message.toLowerCase();
  if (m.includes("password must be")) {
    return m.includes("uppercase") ? "auth.err.passwordRuleComplex" : "auth.err.passwordRule";
  }
  if (m.includes("email already") || m.includes("with this email")) return "auth.err.emailExists";
  if (m.includes("with this username")) return "auth.err.usernameExists";
  if (m.includes("are required")) return "auth.err.fieldsRequired";
  if (m.includes("invalid registration parameters")) {
    // gin: "Key: 'RegisterRequest.Username' Error:Field validation for 'Username' failed on the 'min' tag"
    if (/'Username'/.test(details)) return "auth.err.usernameLength";
    if (/'Email'/.test(details)) return "auth.err.emailInvalid";
    if (/'Password'/.test(details)) return "auth.err.passwordRule";
    return "auth.err.invalidFields";
  }
  return null;
}
