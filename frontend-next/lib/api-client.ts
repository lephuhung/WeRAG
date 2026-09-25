/* Fetch wrapper ported from frontend/src/utils/request.ts + authRefresh.ts.
 * - Bearer JWT from weknora_token, X-Tenant-ID from weknora_selected_tenant_id
 * - 401 → single shared refresh (POST /api/v1/auth/refresh) then replay once
 * - Backend envelope: { success, data?, error? } → error.message extraction
 */

const TOKEN_KEY = "weknora_token";
const REFRESH_KEY = "weknora_refresh_token";
const TENANT_KEY = "weknora_selected_tenant_id";

export type ApiEnvelope<T> = {
  success: boolean;
  data?: T;
  message?: string;
  error?: string | { code?: string; message?: string };
};

export function getTokens(): { token: string | null; refreshToken: string | null } {
  try {
    return {
      token: localStorage.getItem(TOKEN_KEY),
      refreshToken: localStorage.getItem(REFRESH_KEY),
    };
  } catch {
    return { token: null, refreshToken: null };
  }
}

export function setTokens(token: string, refreshToken: string) {
  localStorage.setItem(TOKEN_KEY, token);
  if (refreshToken) localStorage.setItem(REFRESH_KEY, refreshToken);
}

export function clearTokens() {
  for (const k of [
    "weknora_user",
    "weknora_tenant",
    TOKEN_KEY,
    REFRESH_KEY,
    "weknora_knowledge_bases",
    "weknora_current_kb",
    "weknora_selected_tenant_id",
    "weknora_selected_tenant_name",
    "weknora_memberships",
    "weknora_lite_mode",
  ]) {
    try {
      localStorage.removeItem(k);
    } catch {
      /* ignore */
    }
  }
}

function isValidTenantId(id: string | null | undefined): boolean {
  if (!id) return false;
  const trimmed = id.trim();
  if (!trimmed || trimmed === "undefined" || trimmed === "null") return false;
  const n = Number(trimmed);
  return !Number.isNaN(n) && n > 0;
}

// Backend Language() middleware reads the first Accept-Language tag
// (frontend/src/utils/request.ts sent the UI locale the same way; "zh-CN"
// default). Locales here are short ("en"|"vi"|"zh") — expand to the BCP-47
// tags the backend localizes (en-US, vi-VN, zh-CN).
const LOCALE_TO_BCP47: Record<string, string> = {
  en: "en-US",
  vi: "vi-VN",
  zh: "zh-CN",
};

function acceptLanguageHeader(): Record<string, string> {
  try {
    const raw = localStorage.getItem("werag_locale")?.trim() || localStorage.getItem("locale")?.trim() || "";
    const base = raw.split(/[-_]/)[0]?.toLowerCase() || "";
    return { "Accept-Language": LOCALE_TO_BCP47[base] ?? "zh-CN" };
  } catch {
    return { "Accept-Language": "zh-CN" };
  }
}

/* Case-insensitive lookup for an explicit custom header value. Upload and
 * download wrappers accept shorthand header bags, so `authorization` must
 * match `Authorization`. */
function lookupCustomHeader(
  headers: Record<string, string> | undefined,
  name: string,
): string | undefined {
  if (!headers) return undefined;
  const want = name.toLowerCase();
  for (const [key, value] of Object.entries(headers)) {
    if (key.toLowerCase() === want) return value;
  }
  return undefined;
}

function storedTenantId(): string | null {
  try {
    const id = localStorage.getItem(TENANT_KEY);
    if (!isValidTenantId(id)) {
      if (id === "undefined" || id === "null") {
        localStorage.removeItem(TENANT_KEY);
      }
      return null;
    }
    return id!.trim();
  } catch {
    return null;
  }
}

export class ApiError extends Error {
  status: number;
  /** Raw response body, for callers that inspect error.code / error.details. */
  payload?: unknown;
  constructor(status: number, message: string, payload?: unknown) {
    super(message);
    this.status = status;
    this.payload = payload;
  }
}

/* Mirrors the `$httpStatus` convention of frontend/src/utils/request.ts:
 * attached non-enumerably to JSON object payloads so callers can distinguish
 * outcomes sharing a success shape (e.g. createSystemUser 201 vs 200). */
export type WithStatus<T> = T & { readonly $httpStatus?: number };

function withHttpStatus<T>(data: T, status: number): T {
  if (data !== null && typeof data === "object") {
    try {
      Object.defineProperty(data, "$httpStatus", {
        value: status,
        enumerable: false,
        configurable: true,
        writable: false,
      });
    } catch {
      /* frozen payload */
    }
  }
  return data;
}

export type ApiRequestOptions = {
  /** Extra headers; an explicit Authorization (e.g. `Embed <token>`) wins over JWT. */
  headers?: Record<string, string>;
  /** Omit X-Tenant-ID even when one is stored (stale-tenant recovery retry). */
  skipTenant?: boolean;
  /** Abort the request after N ms (fetch has no built-in timeout). */
  timeoutMs?: number;
  signal?: AbortSignal;
};

/* Extra opts.headers that are not managed above ride the request verbatim
 * (X-WeKnora-Desktop-Token, X-Embed-Session, ...). Managed names are
 * skipped so computed values are not concatenated into duplicates. */
function passthroughHeaders(headers: Record<string, string>): Record<string, string> {
  const extra: Record<string, string> = {};
  for (const [name, value] of Object.entries(headers)) {
    switch (name.toLowerCase()) {
      case "authorization":
      case "x-tenant-id":
      case "x-request-id":
      case "accept-language":
        continue;
    }
    extra[name] = value;
  }
  return extra;
}

/* Strip a stale caller-copy Bearer so a refresh replay picks up the freshly
 * stored token instead of re-sending the expired one (same rule as
 * apiUpload's replay). */
function withoutCustomAuth(
  headers: Record<string, string> | undefined,
): Record<string, string> | undefined {
  if (!headers) return headers;
  const replay: Record<string, string> = {};
  for (const [name, value] of Object.entries(headers)) {
    if (name.toLowerCase() !== "authorization") replay[name] = value;
  }
  return replay;
};

/* Share-link / embed endpoints are reachable anonymously — a 401 there must
 * surface to the caller, not trigger refresh-then-redirect (issue #1617). */
const PUBLIC_AUTH_PATHS = [
  "/auth/auto-setup",
  "/auth/login",
  "/auth/register",
  "/auth/oidc/",
  "/auth/invitations/lookup",
  "/api/v1/embed/",
];

function isPublicAuthPath(path: string): boolean {
  return PUBLIC_AUTH_PATHS.some((p) => path.includes(p));
}

let refreshPromise: Promise<string> | null = null;

/* Only a confirmed-invalid refresh credential wipes the session: an
 * explicit 401/403 from /auth/refresh, or an equivalent explicit
 * invalid-auth verdict in the body (e.g. success:false carrying an
 * invalid/expired/revoked refresh-token message). Anything else — 5xx,
 * network failure, unparseable body — is transient: tokens are preserved
 * and no navigation happens, so the next request retries with the same
 * credentials instead of signing the user out for a blip. */
function isInvalidRefreshCredential(status: number, payload: unknown): boolean {
  // 5xx / network (status 0): always transient, even when a proxy error
  // page carries invalid-looking text such as "session expired".
  if (status === 0 || status >= 500) return false;
  if (status === 401 || status === 403) return true;
  if (payload && typeof payload === "object") {
    const record = payload as { error?: unknown; message?: unknown };
    const texts: unknown[] = [record.message];
    if (typeof record.error === "string") texts.push(record.error);
    else if (record.error && typeof record.error === "object") {
      texts.push((record.error as { message?: unknown }).message);
    }
    if (
      texts.some(
        (text) =>
          typeof text === "string" &&
          /invalid[^a-z0-9]*refresh|refresh[^a-z0-9]*(invalid|expired|revoked)|unauthori|session expired|please sign in|sign.?in again/i.test(
            text,
          ),
      )
    ) {
      return true;
    }
  }
  return false;
}

/* Shared refresh single-flight: JSON (request/downloadRequest/apiUpload) and
 * the SSE stream (lib/api/stream.ts) import this same function, so
 * concurrent 401s reuse one promise and one token rotation. The backend
 * rotates BOTH tokens on every call — both are persisted. */
export async function refreshAccessToken(): Promise<string> {
  if (refreshPromise) return refreshPromise;
  refreshPromise = (async () => {
    /* Snapshot the credentials this refresh runs for. The response is
     * async: a login, logout, or account switch may land mid-flight, and
     * blindly persisting afterwards would overwrite the newer session
     * (or resurrect a logged-out one). Every store/clear below first
     * re-checks that these are still the current credentials. */
    const { token: requestAccess, refreshToken: requestRefresh } = getTokens();
    if (!requestRefresh) throw new ApiError(401, "Please sign in again");
    const superseded = () => {
      const current = getTokens();
      return (
        current.refreshToken !== requestRefresh || current.token !== requestAccess
      );
    };
    const staleFlight = () =>
      new ApiError(409, "Session changed during refresh");
    let res: Response;
    try {
      res = await fetch("/api/v1/auth/refresh", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refreshToken: requestRefresh }),
      });
    } catch (err) {
      // Network/abort before any verdict: transient, keep the session.
      throw new ApiError(
        0,
        err instanceof Error ? err.message : "Network error during refresh",
      );
    }
    const data = (await res.json().catch(() => null)) as {
      success?: boolean;
      access_token?: string;
      refresh_token?: string;
      data?: { token?: string; refreshToken?: string };
      message?: string;
      error?: string | { code?: string; message?: string };
    } | null;
    const token = data?.access_token ?? data?.data?.token;
    const nextRefresh = data?.refresh_token ?? data?.data?.refreshToken;
    if (!res.ok || !data?.success || !token) {
      // A verdict for the OLD session must never touch the NEW one: no
      // clearing, no redirect, no replay under the wrong account.
      if (superseded()) throw staleFlight();
      if (!isInvalidRefreshCredential(res.status, data)) {
        throw new ApiError(res.status || 0, envelopeMessage(data), data ?? undefined);
      }
      clearTokens();
      window.location.href = "/login";
      throw new ApiError(res.status || 401, data?.message ?? "Session expired");
    }
    if (superseded()) throw staleFlight();
    setTokens(token, nextRefresh ?? requestRefresh);
    return token;
  })();
  try {
    return await refreshPromise;
  } finally {
    refreshPromise = null;
  }
}

function envelopeMessage(payload: unknown): string {
  if (payload && typeof payload === "object") {
    const p = payload as { error?: unknown; message?: unknown };
    if (typeof p.error === "string") return p.error;
    if (p.error && typeof p.error === "object") {
      const m = (p.error as { message?: unknown }).message;
      if (typeof m === "string") return m;
    }
    if (typeof p.message === "string") return p.message;
  }
  return "Request failed";
}

function wireSignal(controller: AbortController, opts?: ApiRequestOptions): AbortSignal | undefined {
  if (!opts?.signal && !opts?.timeoutMs) return opts?.signal;
  if (opts?.timeoutMs) setTimeout(() => controller.abort(), opts.timeoutMs);
  opts?.signal?.addEventListener("abort", () => controller.abort(), { once: true });
  return controller.signal;
}

async function request<T>(path: string, init: RequestInit, opts?: ApiRequestOptions, retry = true): Promise<T> {
  const { token } = getTokens();
  /* opts.headers carry caller credentials (Embed tokens, desktop token,
   * tenant/session overrides) — an explicit Authorization wins over the
   * stored JWT, case-insensitively, with the same override rule as
   * apiUpload(): an explicit `Bearer` is a stale-capable caller copy and
   * stays refresh-eligible; any other scheme is caller-managed. */
  const customHeaders = opts?.headers ?? {};
  const customAuth = lookupCustomHeader(customHeaders, "Authorization");
  const initAuth = (init.headers as Record<string, string> | undefined)?.Authorization;
  const isEmbedAuth =
    (typeof customAuth === "string" && customAuth.startsWith("Embed ")) ||
    (typeof initAuth === "string" && initAuth.startsWith("Embed "));
  const isBearerCustomAuth =
    typeof customAuth === "string" && customAuth.startsWith("Bearer ");
  const isEmbed = path.includes("/api/v1/embed/") || isEmbedAuth;
  const customTenant = lookupCustomHeader(customHeaders, "X-Tenant-ID");
  const authHeader = customAuth ?? (!isEmbed && token ? `Bearer ${token}` : null);
  const tenantHeader =
    customTenant ?? (!isEmbed && !opts?.skipTenant ? storedTenantId() : null);
  const controller = new AbortController();
  const res = await fetch(path, {
    ...init,
    signal: wireSignal(controller, opts),
    headers: {
      "Content-Type": "application/json",
      "X-Request-ID":
        lookupCustomHeader(customHeaders, "X-Request-ID") ??
        `${Math.random().toString(36).slice(2, 14)}`,
      "Accept-Language":
        lookupCustomHeader(customHeaders, "Accept-Language") ??
        acceptLanguageHeader()["Accept-Language"],
      ...(authHeader ? { Authorization: authHeader } : {}),
      ...(tenantHeader ? { "X-Tenant-ID": tenantHeader } : {}),
      ...(init.headers ?? {}),
      ...passthroughHeaders(customHeaders),
    },
  });
  if (
    res.status === 401 &&
    retry &&
    !isEmbed &&
    !isPublicAuthPath(path) &&
    !path.includes("/auth/refresh") &&
    (customAuth === undefined || isBearerCustomAuth)
  ) {
    const next = await refreshAccessToken();
    return request<T>(
      path,
      { ...init, headers: { ...(init.headers ?? {}), Authorization: `Bearer ${next}` } },
      isBearerCustomAuth ? { ...opts, headers: withoutCustomAuth(opts?.headers) } : opts,
      false,
    );
  }
  const payload = (await res.json().catch(() => null)) as T;
  if (!res.ok) throw new ApiError(res.status, envelopeMessage(payload), payload);
  return withHttpStatus(payload, res.status);
}

export function apiGet<T>(path: string, opts?: ApiRequestOptions): Promise<T> {
  return request<T>(path, { method: "GET" }, opts);
}

export function apiPost<T>(path: string, body: unknown, opts?: ApiRequestOptions): Promise<T> {
  return request<T>(path, { method: "POST", body: JSON.stringify(body ?? {}) }, opts);
}

export function apiPut<T>(path: string, body: unknown, opts?: ApiRequestOptions): Promise<T> {
  return request<T>(path, { method: "PUT", body: JSON.stringify(body ?? {}) }, opts);
}

export function apiPatch<T>(path: string, body: unknown, opts?: ApiRequestOptions): Promise<T> {
  return request<T>(path, { method: "PATCH", body: JSON.stringify(body ?? {}) }, opts);
}

export function apiDel<T>(path: string, body?: unknown, opts?: ApiRequestOptions): Promise<T> {
  return request<T>(
    path,
    { method: "DELETE", body: body === undefined ? undefined : JSON.stringify(body) },
    opts,
  );
}

async function downloadRequest(path: string, init: RequestInit, opts?: ApiRequestOptions, retry = true): Promise<Blob> {
  const { token } = getTokens();
  /* Same opts.headers override rule as request(): explicit Authorization
   * wins (case-insensitive); Embed or other caller-managed schemes never
   * refresh; a stale caller-copy Bearer replays with the fresh token. */
  const customHeaders = opts?.headers ?? {};
  const customAuth = lookupCustomHeader(customHeaders, "Authorization");
  const isEmbedAuth = typeof customAuth === "string" && customAuth.startsWith("Embed ");
  const isBearerCustomAuth =
    typeof customAuth === "string" && customAuth.startsWith("Bearer ");
  const isEmbed = path.includes("/api/v1/embed/") || isEmbedAuth;
  const customTenant = lookupCustomHeader(customHeaders, "X-Tenant-ID");
  const authHeader = customAuth ?? (!isEmbed && token ? `Bearer ${token}` : null);
  const tenantHeader =
    customTenant ?? (!isEmbed && !opts?.skipTenant ? storedTenantId() : null);
  const controller = new AbortController();
  const res = await fetch(path, {
    ...init,
    signal: wireSignal(controller, opts),
    headers: {
      ...(init.body ? { "Content-Type": "application/json" } : {}),
      "Accept-Language":
        lookupCustomHeader(customHeaders, "Accept-Language") ??
        acceptLanguageHeader()["Accept-Language"],
      ...(authHeader ? { Authorization: authHeader } : {}),
      ...(tenantHeader ? { "X-Tenant-ID": tenantHeader } : {}),
      ...(init.headers ?? {}),
      ...passthroughHeaders(customHeaders),
    },
  });
  if (
    res.status === 401 &&
    retry &&
    !isEmbed &&
    !isPublicAuthPath(path) &&
    (customAuth === undefined || isBearerCustomAuth)
  ) {
    /* Same refresh-then-replay contract as request(): Vue's axios
     * interceptor covers downloads too — without this an expired access
     * token fails downloads/uploads while every JSON call self-heals. */
    const next = await refreshAccessToken();
    return downloadRequest(
      path,
      {
        ...init,
        headers: {
          ...(init.headers ?? {}),
          Authorization: `Bearer ${next}`,
        },
      },
      isBearerCustomAuth ? { ...opts, headers: withoutCustomAuth(opts?.headers) } : opts,
      false,
    );
  }
  if (!res.ok) {
    // A failed download still returns a JSON error body — unwrap it.
    const text = await res.text().catch(() => "");
    let message = `Download failed: HTTP ${res.status}`;
    try {
      const payload = JSON.parse(text);
      message = envelopeMessage(payload);
    } catch {
      /* non-JSON body */
    }
    throw new ApiError(res.status, message);
  }
  return res.blob();
}

export function apiDownload(path: string, opts?: ApiRequestOptions): Promise<Blob> {
  return downloadRequest(path, { method: "GET" }, opts);
}

/* Some download endpoints are POST (e.g. batch-download carries an id list
 * in the body). Same auth/blob semantics as apiDownload. */
export function apiPostDownload(path: string, body: unknown, opts?: ApiRequestOptions): Promise<Blob> {
  return downloadRequest(path, { method: "POST", body: JSON.stringify(body ?? {}) }, opts);
}

/* Multipart upload with progress, ported from postUpload() in
 * frontend/src/utils/request.ts. fetch() cannot report upload progress, so
 * this stays on XMLHttpRequest. */
export function apiUpload<T>(
  path: string,
  form: FormData,
  onProgress?: (percent: number) => void,
  opts?: ApiRequestOptions,
  retry = true,
): Promise<T> {
  const token = (() => {
    try {
      return localStorage.getItem(TOKEN_KEY);
    } catch {
      return null;
    }
  })();
  const tenantId = (() => {
    try {
      return localStorage.getItem(TENANT_KEY);
    } catch {
      return null;
    }
  })();
  /* Custom upload headers (opts.headers) win over the stored defaults, with
   * the same override semantics as request(): an explicit `Embed <token>`
   * Authorization marks the call as embed and suppresses the stored JWT and
   * tenant, matching the axios postUpload interceptor this was ported from.
   * Content-Type is never applied: XHR must derive the multipart boundary
   * from the FormData, and a manual value would corrupt the upload. */
  const customHeaders = opts?.headers ?? {};
  const customAuth = lookupCustomHeader(customHeaders, "Authorization");
  const isEmbedAuth =
    typeof customAuth === "string" && customAuth.startsWith("Embed ");
  /* An explicit `Bearer <token>` in opts.headers is a stale-capable caller
   * copy of the JWT (not caller-managed auth): it stays refresh-eligible so
   * a 401 still self-heals. Any other explicit Authorization scheme (Embed
   * above, or e.g. `Token ...`) is caller-managed and must never trigger a
   * refresh — replaying with the stored JWT would send the wrong identity. */
  const isBearerCustomAuth =
    typeof customAuth === "string" && customAuth.startsWith("Bearer ");
  const isEmbed = path.includes("/api/v1/embed/") || isEmbedAuth;
  const customTenant = lookupCustomHeader(customHeaders, "X-Tenant-ID");
  const storedTenant = tenantId !== null && isValidTenantId(tenantId) ? tenantId.trim() : null;
  const authHeader = customAuth ?? (!isEmbed && token ? `Bearer ${token}` : null);
  const tenantHeader = customTenant ?? (!isEmbed ? storedTenant : null);
  const requestId =
    lookupCustomHeader(customHeaders, "X-Request-ID") ??
    Math.random().toString(36).slice(2, 14);
  let acceptLanguage: string | null = null;
  try {
    acceptLanguage =
      lookupCustomHeader(customHeaders, "Accept-Language") ??
      acceptLanguageHeader()["Accept-Language"];
  } catch {
    /* locale is best-effort */
  }
  return new Promise<T>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", path);
    if (opts?.timeoutMs) xhr.timeout = opts.timeoutMs;
    if (authHeader) xhr.setRequestHeader("Authorization", authHeader);
    if (tenantHeader) xhr.setRequestHeader("X-Tenant-ID", tenantHeader);
    xhr.setRequestHeader("X-Request-ID", requestId);
    if (acceptLanguage) xhr.setRequestHeader("Accept-Language", acceptLanguage);
    for (const [name, value] of Object.entries(customHeaders)) {
      /* Managed above (or forbidden for multipart): skip so XHR does not
       * concatenate duplicate values onto the same header. Everything else
       * — e.g. X-WeKnora-Desktop-Token — passes through verbatim. */
      switch (name.toLowerCase()) {
        case "authorization":
        case "x-tenant-id":
        case "x-request-id":
        case "accept-language":
        case "content-type":
          continue;
      }
      try {
        xhr.setRequestHeader(name, value);
      } catch {
        /* invalid header name/value */
      }
    }
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress?.(Math.round((e.loaded * 100) / e.total));
    };
    if (opts?.signal) {
      if (opts.signal.aborted) {
        reject(new ApiError(0, "Upload aborted"));
        return;
      }
      opts.signal.addEventListener("abort", () => xhr.abort(), { once: true });
    }
    xhr.onabort = () => reject(new ApiError(0, "Upload aborted"));
    xhr.onload = () => {
      if (
        xhr.status === 401 &&
        retry &&
        !isEmbed &&
        !isPublicAuthPath(path) &&
        (customAuth === undefined || isBearerCustomAuth)
      ) {
        /* The upload ran with a stale access token: refresh once and
         * resend the same FormData, like request()'s replay. A stale
         * custom `Bearer` in opts.headers must not survive into the replay
         * — strip it so the recursive call picks up the freshly stored
         * token instead of re-sending the expired one. */
        refreshAccessToken()
          .then(() => {
            if (!isBearerCustomAuth) {
              resolve(apiUpload(path, form, onProgress, opts, false));
              return;
            }
            const replayHeaders: Record<string, string> = {};
            for (const [name, value] of Object.entries(customHeaders)) {
              if (name.toLowerCase() !== "authorization") {
                replayHeaders[name] = value;
              }
            }
            resolve(
              apiUpload(
                path,
                form,
                onProgress,
                { ...opts, headers: replayHeaders },
                false,
              ),
            );
          })
          .catch(reject);
        return;
      }
      /* Error bodies are not always the JSON envelope (nginx 413/504
       * pages, proxies). Don't leak the JSON.parse SyntaxError as the
       * user-facing message. */
      let payload: T | null = null;
      try {
        payload = xhr.responseText ? (JSON.parse(xhr.responseText) as T) : null;
      } catch {
        payload = null;
      }
      if (xhr.status >= 200 && xhr.status < 300) {
        if (payload !== null) resolve(payload);
        else reject(new ApiError(xhr.status, "Upload failed: empty response"));
      } else {
        reject(
          new ApiError(
            xhr.status,
            payload ? envelopeMessage(payload) : `Upload failed: HTTP ${xhr.status}`,
            payload ?? undefined,
          ),
        );
      }
    };
    xhr.onerror = () => reject(new ApiError(0, "Network error during upload"));
    xhr.ontimeout = () => reject(new ApiError(0, "Upload timed out"));
    xhr.send(form);
  });
}
