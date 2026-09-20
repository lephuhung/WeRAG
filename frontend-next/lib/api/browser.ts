/* Local browser extension connection — ported from
 * frontend/src/stores/browserConnection.ts +
 * views/settings/BrowserConnectionSettings.vue. */
import { apiDownload, apiGet, apiPost } from "@/lib/api-client";

export interface BrowserDevice {
  id: string;
  label: string;
  last_seen_at: string;
}

export interface BrowserAccountStatus {
  enabled: boolean;
  connected: boolean;
  device?: BrowserDevice;
  extension_available: boolean;
}

const ENDPOINT = "/api/v1/me/browser";

export function getBrowserConnection(opts?: { signal?: AbortSignal }) {
  return apiGet<{ data: BrowserAccountStatus }>(ENDPOINT, opts);
}

/* Returns a short-lived pairing link the user opens in the browser extension.
 * The link expires (~5 min); callers should treat it as single-use. */
export function pairBrowserConnection(origin: string, opts?: { signal?: AbortSignal }) {
  return apiPost<{ data: { pairing_link: string } }>(ENDPOINT, {
    action: "pair",
    origin,
  }, { timeoutMs: 15_000, ...opts });
}

export function revokeBrowserConnection(opts?: { signal?: AbortSignal }) {
  return apiPost<{ data: BrowserAccountStatus }>(ENDPOINT, { action: "revoke" }, opts);
}

/** Browser extension bundle (browser-skill-weknora.zip). */
export function downloadBrowserExtension(): Promise<Blob> {
  return apiDownload(`${ENDPOINT}/extension`);
}
