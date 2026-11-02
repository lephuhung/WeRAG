/* A locked account (users.is_active = false in WeRAG). The backend answers
 * the device-code poll, /auth/refresh and the auth middleware with 401 and
 * the login form's text "Account is disabled" (service.ErrUserDisabled);
 * the middleware also adds code "ACCOUNT_DISABLED". Retrying cannot help,
 * so the sign-in screen says so instead of "code expired". */

export const ACCOUNT_DISABLED_MESSAGE = "Account is disabled";
export const ACCOUNT_DISABLED_CODE = "ACCOUNT_DISABLED";

const SIGNED_OUT_KEY = "werag_word_signed_out_disabled";

/* Matches the flat middleware body {error, code}, the AppError envelope
 * {error: {code: 1001, message}} and a plain {message}. */
export function isAccountDisabledReply(status: number, payload: unknown): boolean {
  if (status !== 401 || !payload || typeof payload !== "object") return false;
  const record = payload as { code?: unknown; error?: unknown; message?: unknown };
  if (record.code === ACCOUNT_DISABLED_CODE) return true;
  const texts: unknown[] = [record.message];
  if (typeof record.error === "string") texts.push(record.error);
  else if (record.error && typeof record.error === "object") {
    const nested = record.error as { code?: unknown; message?: unknown };
    if (nested.code === ACCOUNT_DISABLED_CODE) return true;
    texts.push(nested.message);
  }
  return texts.some((t) => typeof t === "string" && t.trim() === ACCOUNT_DISABLED_MESSAGE);
}

/* The refresh path signs the user out with a page navigation; this note
 * survives it so the sign-in screen can name the reason once. */
export function noteSignedOutDisabled(): void {
  try {
    window.sessionStorage.setItem(SIGNED_OUT_KEY, "1");
  } catch {
    /* no storage */
  }
}

export function takeSignedOutDisabled(): boolean {
  try {
    const set = window.sessionStorage.getItem(SIGNED_OUT_KEY) === "1";
    if (set) window.sessionStorage.removeItem(SIGNED_OUT_KEY);
    return set;
  } catch {
    return false;
  }
}
