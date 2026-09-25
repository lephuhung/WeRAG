/* Same-origin `next` validation for post-login redirects.
 *
 * The login page accepts `?next=<path>` so deep links survive the sign-in
 * round-trip. That value is untrusted input: pushing it blindly into
 * router.push() turns /login?next=//evil.example into an open redirect
 * (protocol-relative URLs navigate away from the app), and encoded variants
 * (%2F%2Fevil, %6aavascript:...) bypass naive prefix checks.
 *
 * resolveSafeNextPath() accepts only same-origin local pathnames and maps
 * everything else to DEFAULT_POST_LOGIN_ROUTE. Pure (no router/DOM) so it
 * is unit-testable.
 */

export const DEFAULT_POST_LOGIN_ROUTE = "/platform/knowledge-bases";

const MAX_NEXT_LENGTH = 2048;

export function resolveSafeNextPath(raw: unknown): string {
  if (typeof raw !== "string") return DEFAULT_POST_LOGIN_ROUTE;
  const trimmed = raw.trim();
  if (!trimmed || trimmed.length > MAX_NEXT_LENGTH) return DEFAULT_POST_LOGIN_ROUTE;
  /* Decode percent-escapes (repeat: %252F double-encodes to %2F then /) so
   * encoded evasions are judged by where they actually navigate. A
   * malformed escape is itself untrusted — fall back. */
  let decoded = trimmed;
  try {
    for (let i = 0; i < 3; i++) {
      const next = decodeURIComponent(decoded);
      if (next === decoded) break;
      decoded = next;
    }
  } catch {
    return DEFAULT_POST_LOGIN_ROUTE;
  }
  /* Only a same-origin local pathname: single leading slash, no scheme,
   * no protocol-relative //, no backslash (browsers normalize /\ and \/
   * toward authority-relative URLs), no whitespace/control characters. */
  if (!decoded.startsWith("/")) return DEFAULT_POST_LOGIN_ROUTE;
  if (decoded.startsWith("//") || decoded.startsWith("/\\")) {
    return DEFAULT_POST_LOGIN_ROUTE;
  }
  // eslint-disable-next-line no-control-regex
  if (/[\s\x00-\x1f\x7f\\]/.test(decoded)) return DEFAULT_POST_LOGIN_ROUTE;
  return decoded;
}
