/* Avatar rendering helper: user.avatar holds either an external URL or a
 * stored provider:// path. Provider paths can only be fetched through the
 * authenticated /files proxy (Bearer + X-Tenant-ID), so <img src> alone can't
 * display them — this hook fetches the bytes with auth and swaps in a blob
 * URL, same pattern as chat artifact images (lib/artifact-images.ts). */
"use client";

import { useEffect, useState } from "react";

import { buildProtectedFileRequest, isProviderFileURL } from "@/lib/protected-file-access";

type AvatarCache = { byKey: Map<string, string> };

function avatarCache(): AvatarCache {
  const fresh = (): AvatarCache => ({ byKey: new Map() });
  if (typeof window === "undefined") return fresh();
  const scope = window as typeof window & { __weknoraAvatarBlobCacheV1__?: AvatarCache };
  scope.__weknoraAvatarBlobCacheV1__ ||= fresh();
  return scope.__weknoraAvatarBlobCacheV1__;
}

/** Resolve a user.avatar value into a URL an <img> can display, or null while
 * loading / when the value is empty or the fetch failed. External http(s) and
 * data: URLs pass through untouched; storage paths hydrate via the proxy. */
export function useAvatarUrl(avatar?: string | null): string | null {
  const [url, setUrl] = useState<string | null>(() => {
    const value = (avatar || "").trim();
    if (!value || isProviderFileURL(value)) return null;
    return value;
  });

  useEffect(() => {
    const value = (avatar || "").trim();
    if (!value) {
      setUrl(null);
      return;
    }
    if (!isProviderFileURL(value)) {
      setUrl(value);
      return;
    }
    const cache = avatarCache();
    const cached = cache.byKey.get(value);
    if (cached) {
      setUrl(cached);
      return;
    }
    let cancelled = false;
    (async () => {
      const request = buildProtectedFileRequest(value, { mode: "tenant" });
      if (!request) return;
      try {
        const res = await fetch(request.url, { headers: request.headers });
        if (!res.ok) return;
        const blob = await res.blob();
        const blobURL = URL.createObjectURL(blob);
        cache.byKey.set(value, blobURL);
        if (!cancelled) setUrl(blobURL);
      } catch {
        /* leave url null — callers render initials instead */
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [avatar]);

  return url;
}
