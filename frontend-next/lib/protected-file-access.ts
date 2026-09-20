/* Ported from frontend/src/utils/protectedFileAccess.ts.
 *
 * Access context for protected files (provider:// / resource:// etc.).
 * The backend splits the file proxy per principal — the auth model differs:
 *   - `/files`                                     → Bearer + X-Tenant-ID
 *   - `/api/v1/knowledge-bases/:id/files`          → KB access (cross-tenant shared KBs)
 *   - `/api/v1/sessions/:id/messages/:mid/files`   → session/message ownership + shared agents
 *   - `/api/v1/embed/:channel_id/files`            → embed visitor's Embed token
 *
 * Which proxy to use depends on the auth plane of the current request, not on
 * which component renders the image. This module is the single source of that
 * decision — renderers declare a scope instead of building URLs themselves.
 */

export const PROVIDER_SCHEME_PATTERN = "resource|local|minio|cos|tos|s3|oss|ks3|obs";

const PROVIDER_FILE_SCHEME_RE = new RegExp(`^(${PROVIDER_SCHEME_PATTERN}):\\/\\/\\S+$`, "i");
const STORAGE_BACKEND_FILE_SCHEME_RE = new RegExp(
  `^storage:\\/\\/[0-9A-Za-z_-]+\\/(${PROVIDER_SCHEME_PATTERN}):\\/\\/\\S+$`,
  "i",
);

const KB_FILE_PROXY_PATH_RE = /^\/api\/v1\/knowledge-bases\/[^/]+\/files$/;
const EMBED_FILE_PROXY_PATH_RE = /^\/api\/v1\/embed\/[^/]+\/files$/;
const MESSAGE_FILE_PROXY_PATH_RE = /^\/api\/v1\/sessions\/[^/]+\/messages\/[^/]+\/files$/;

export type ProtectedFileAccessContext =
  /** Signed-in user: Bearer + selected tenant. */
  | { mode: "tenant" }
  /** Embed visitor: only an Embed token — no Bearer, no tenant context. */
  | { mode: "embed"; channelId: string; token: string }
  /** KB scope: a signed-in user reading objects in a shared KB owned by
   * another tenant. */
  | { mode: "knowledgeBase"; kbId: string }
  /** Message scope: a signed-in user reading source-workspace resources
   * inside a shared agent's reply. */
  | { mode: "message"; sessionId: string; messageId: string };

export interface ProtectedFileRequest {
  url: string;
  headers: Record<string, string>;
}

const TENANT_ACCESS: ProtectedFileAccessContext = { mode: "tenant" };

interface ProtectedFileAccessState {
  current: ProtectedFileAccessContext;
}

/* Kept on window like the blob cache: dev hot-reload swaps the module but not
 * the document, so module-level state would lose the context the embed app
 * registered at bootstrap. */
const accessState: ProtectedFileAccessState = (() => {
  const fresh = (): ProtectedFileAccessState => ({ current: TENANT_ACCESS });
  if (typeof window === "undefined") return fresh();
  const scope = window as typeof window & {
    __weknoraProtectedFileAccessV1__?: ProtectedFileAccessState;
  };
  scope.__weknoraProtectedFileAccessV1__ ||= fresh();
  return scope.__weknoraProtectedFileAccessV1__;
})();

/* Register the document's default access context once (the embed app does it
 * after obtaining channelId/token); afterwards every protected-file request
 * automatically uses the matching proxy. */
export function setDefaultProtectedFileAccess(
  access: ProtectedFileAccessContext | null,
): void {
  accessState.current = access ?? TENANT_ACCESS;
}

export function getDefaultProtectedFileAccess(): ProtectedFileAccessContext {
  return accessState.current;
}

/* Merge the default context with a component-supplied scope. The default
 * context carries the auth plane (embed visitor vs signed-in user); a
 * component override may only refine scope within the same plane — an embed
 * visitor has no Bearer, so letting a `knowledgeBase` override replace the
 * embed plane would land on a login-required proxy and 401. */
export function resolveProtectedFileAccess(
  override?: ProtectedFileAccessContext | null,
): ProtectedFileAccessContext {
  const fallback = accessState.current;
  if (fallback.mode === "embed") return fallback;
  if (!override) return fallback;
  if (override.mode === "knowledgeBase" && !override.kbId.trim()) return fallback;
  if (override.mode === "message" && (!override.sessionId.trim() || !override.messageId.trim())) {
    return fallback;
  }
  return override;
}

/** Whether `url` is a storage path needing the proxy (provider:// or
 * storage://<backend>/provider://). */
export function isProviderFileURL(url: string): boolean {
  const trimmed = url.trim();
  return PROVIDER_FILE_SCHEME_RE.test(trimmed) || STORAGE_BACKEND_FILE_SCHEME_RE.test(trimmed);
}

/** Whether `pathname` is one of the protected file proxies. */
export function isProtectedFileProxyPath(pathname: string): boolean {
  return (
    pathname === "/files" ||
    KB_FILE_PROXY_PATH_RE.test(pathname) ||
    MESSAGE_FILE_PROXY_PATH_RE.test(pathname) ||
    EMBED_FILE_PROXY_PATH_RE.test(pathname)
  );
}

function tenantRequestHeaders(): Record<string, string> {
  const headers: Record<string, string> = {};
  try {
    const token = (localStorage.getItem("weknora_token") || "").trim();
    if (token) {
      headers["Authorization"] = `Bearer ${token}`;
    }

    const selectedTenantId = (
      localStorage.getItem("weknora_selected_tenant_id") || ""
    ).trim();
    if (selectedTenantId && /^\d+$/.test(selectedTenantId)) {
      // Always attach when a selected tenant is set. Same rationale as
      // api-client.ts / stream.ts: the
      // "selectedTenantId === defaultTenantId → skip" short-circuit silently
      // drops the header whenever any code path writes the active tenant
      // into weknora_selected_tenant_id, leaving file fetches landing on the
      // home tenant.
      headers["X-Tenant-ID"] = selectedTenantId;
    }
  } catch {
    // ignore localStorage read errors
  }
  return headers;
}

/* Build a proxy request for a storage path. Returns null when the current
 * context cannot make the request (non-storage path, or the embed context
 * has no token yet) — callers should skip and retry after hydration. */
export function buildProtectedFileRequest(
  sourceURL: string,
  access: ProtectedFileAccessContext,
): ProtectedFileRequest | null {
  const filePath = sourceURL.trim();
  if (!isProviderFileURL(filePath)) return null;

  const query = new URLSearchParams({ file_path: filePath }).toString();

  if (access.mode === "embed") {
    const channelId = access.channelId.trim();
    const token = access.token.trim();
    // An embed visitor's only credential is the Embed token; falling back to
    // /files without it would just 401 — better to skip until bootstrap lands.
    if (!channelId || !token) return null;
    return {
      url: `/api/v1/embed/${encodeURIComponent(channelId)}/files?${query}`,
      headers: { Authorization: `Embed ${token}` },
    };
  }

  if (access.mode === "knowledgeBase") {
    return {
      url: `/api/v1/knowledge-bases/${encodeURIComponent(access.kbId.trim())}/files?${query}`,
      headers: tenantRequestHeaders(),
    };
  }

  if (access.mode === "message") {
    return {
      url: `/api/v1/sessions/${encodeURIComponent(access.sessionId.trim())}/messages/${encodeURIComponent(access.messageId.trim())}/files?${query}`,
      headers: tenantRequestHeaders(),
    };
  }

  return { url: `/files?${query}`, headers: tenantRequestHeaders() };
}
