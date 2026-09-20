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

function selectedTenantHeader(skip: boolean): Record<string, string> {
  if (skip) return {};
  try {
    const id = localStorage.getItem(TENANT_KEY);
    if (!isValidTenantId(id)) {
      if (id === "undefined" || id === "null") {
        localStorage.removeItem(TENANT_KEY);
      }
      return {};
    }
    return { "X-Tenant-ID": id!.trim() };
  } catch {
    return {};
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
  /** Abort the request after N ms (fetch has no built-in timeout). */
  timeoutMs?: number;
  signal?: AbortSignal;
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

async function refreshAccessToken(): Promise<string> {
  if (refreshPromise) return refreshPromise;
  refreshPromise = (async () => {
    const { refreshToken } = getTokens();
    if (!refreshToken) throw new ApiError(401, "Please sign in again");
    const res = await fetch("/api/v1/auth/refresh", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refreshToken }),
    });
    const data = (await res.json().catch(() => null)) as {
      success?: boolean;
      access_token?: string;
      refresh_token?: string;
      data?: { token?: string; refreshToken?: string };
      message?: string;
    } | null;
    const token = data?.access_token ?? data?.data?.token;
    const nextRefresh = data?.refresh_token ?? data?.data?.refreshToken;
    if (!res.ok || !data?.success || !token) {
      clearTokens();
      window.location.href = "/login";
      throw new ApiError(res.status || 401, data?.message ?? "Session expired");
    }
    setTokens(token, nextRefresh ?? refreshToken);
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
  const isEmbed =
    path.includes("/api/v1/embed/") ||
    (typeof (init.headers as Record<string, string> | undefined)?.Authorization === "string" &&
      (init.headers as Record<string, string>).Authorization.startsWith("Embed "));
  const controller = new AbortController();
  const res = await fetch(path, {
    ...init,
    signal: wireSignal(controller, opts),
    headers: {
      "Content-Type": "application/json",
      "X-Request-ID": `${Math.random().toString(36).slice(2, 14)}`,
      ...(!isEmbed && token ? { Authorization: `Bearer ${token}` } : {}),
      ...selectedTenantHeader(isEmbed),
      ...(init.headers ?? {}),
    },
  });
  if (
    res.status === 401 &&
    retry &&
    !isEmbed &&
    !isPublicAuthPath(path) &&
    !path.includes("/auth/refresh")
  ) {
    const next = await refreshAccessToken();
    return request<T>(
      path,
      { ...init, headers: { ...(init.headers ?? {}), Authorization: `Bearer ${next}` } },
      opts,
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

async function downloadRequest(path: string, init: RequestInit, opts?: ApiRequestOptions): Promise<Blob> {
  const { token } = getTokens();
  const isEmbed = path.includes("/api/v1/embed/");
  const controller = new AbortController();
  const res = await fetch(path, {
    ...init,
    signal: wireSignal(controller, opts),
    headers: {
      ...(init.body ? { "Content-Type": "application/json" } : {}),
      ...(!isEmbed && token ? { Authorization: `Bearer ${token}` } : {}),
      ...selectedTenantHeader(isEmbed),
      ...(opts?.headers ?? {}),
    },
  });
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
  const isEmbed = path.includes("/api/v1/embed/");
  return new Promise<T>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", path);
    if (opts?.timeoutMs) xhr.timeout = opts.timeoutMs;
    if (!isEmbed && token) xhr.setRequestHeader("Authorization", `Bearer ${token}`);
    if (!isEmbed && tenantId) xhr.setRequestHeader("X-Tenant-ID", tenantId);
    xhr.setRequestHeader("X-Request-ID", Math.random().toString(36).slice(2, 14));
    for (const [k, v] of Object.entries(opts?.headers ?? {})) xhr.setRequestHeader(k, v);
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress?.(Math.round((e.loaded * 100) / e.total));
    };
    xhr.onload = () => {
      try {
        const payload = JSON.parse(xhr.responseText) as T;
        if (xhr.status >= 200 && xhr.status < 300) resolve(payload);
        else reject(new ApiError(xhr.status, envelopeMessage(payload), payload));
      } catch (err) {
        reject(err instanceof Error ? err : new ApiError(xhr.status, "Upload failed"));
      }
    };
    xhr.onerror = () => reject(new ApiError(0, "Network error during upload"));
    xhr.ontimeout = () => reject(new ApiError(0, "Upload timed out"));
    xhr.send(form);
  });
}
