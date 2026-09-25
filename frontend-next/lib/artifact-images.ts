/* Port of frontend/src/utils/sandboxArtifactRefs.ts — the minimal slice the
 * chat panel needs to render tool-generated images (e.g. MCP text-to-image).
 *
 * The backend persists tool-produced images as message artifacts and cites
 * them in the answer body as `![...](resource://<handle>)`. Artifact bytes
 * are fetched with auth through the download endpoint and swapped into a
 * blob URL, so the panel never needs the physical storage path.
 */

import type { ArtifactMeta } from "@/lib/api/chat";

export type ArtifactImageContext = {
  sessionId: string;
  messageId: string;
};

const RESOURCE_HANDLE_RE = /^resource:\/\/([A-Za-z0-9_-]{22})$/;
const IMAGE_EXTENSIONS = new Set(["png", "jpg", "jpeg", "gif", "webp", "bmp", "avif"]);

export const ARTIFACT_TRANSPARENT_PIXEL =
  "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==";

function artifactHandle(a: ArtifactMeta): string {
  return (a.handle || "").trim().match(RESOURCE_HANDLE_RE)?.[1] || "";
}

function fileExtension(fileName: string): string {
  const base = (fileName || "").trim().toLowerCase();
  const dot = base.lastIndexOf(".");
  return dot > 0 ? base.slice(dot + 1) : "";
}

/** Images render inline; every other artifact kind is out of scope here. */
export function artifactRendersAsImage(a: ArtifactMeta): boolean {
  const ext = fileExtension(a.file_name);
  if (ext) return IMAGE_EXTENSIONS.has(ext);
  return (a.file_type || "").toLowerCase().startsWith("image/");
}

/** Resolve a Markdown image destination to this message's artifact, if any. */
export function resolveArtifactImage(
  href: string,
  artifacts: ArtifactMeta[] | undefined | null,
): ArtifactMeta | null {
  const trimmed = (href || "").trim();
  if (!trimmed || !artifacts?.length) return null;
  // The backend writes the authoritative `resource://<handle>` form; accept a
  // bare file name as well so a non-rewritten body still resolves.
  const handleMatch = trimmed.match(RESOURCE_HANDLE_RE);
  if (handleMatch) {
    return artifacts.find((a) => artifactHandle(a) === handleMatch[1]) || null;
  }
  const name = trimmed.split("/").pop()?.trim();
  if (!name) return null;
  return artifacts.find((a) => (a.file_name || "").trim() === name) || null;
}

type BlobCache = { byKey: Map<string, string>; inflight: Map<string, Promise<string | null>> };

function blobCache(): BlobCache {
  const fresh = (): BlobCache => ({ byKey: new Map(), inflight: new Map() });
  if (typeof window === "undefined") return fresh();
  const scope = window as typeof window & { __weknoraArtifactBlobCacheV1__?: BlobCache };
  scope.__weknoraArtifactBlobCacheV1__ ||= fresh();
  return scope.__weknoraArtifactBlobCacheV1__;
}

function blobKey(ctx: ArtifactImageContext, index: number): string {
  return `${ctx.sessionId}\u0000${ctx.messageId}\u0000${index}`;
}

async function loadArtifactBlobURL(
  ctx: ArtifactImageContext,
  index: number,
  download: (sessionId: string, messageId: string, index: number) => Promise<Blob>,
): Promise<string | null> {
  const cache = blobCache();
  const key = blobKey(ctx, index);
  const cached = cache.byKey.get(key);
  if (cached) return cached;
  let task = cache.inflight.get(key);
  if (!task) {
    task = (async () => {
      try {
        const blob = await download(ctx.sessionId, ctx.messageId, index);
        const url = URL.createObjectURL(blob);
        cache.byKey.set(key, url);
        return url;
      } catch {
        return null;
      } finally {
        cache.inflight.delete(key);
      }
    })();
    cache.inflight.set(key, task);
  }
  return task;
}

/** Swap placeholder <img> nodes carrying data-artifact-index for blob URLs. */
export async function hydrateArtifactImages(
  root: ParentNode | null | undefined,
  ctx: ArtifactImageContext | null | undefined,
  download: (sessionId: string, messageId: string, index: number) => Promise<Blob>,
): Promise<void> {
  if (!root || !ctx?.sessionId || !ctx?.messageId) return;
  const images = Array.from(
    root.querySelectorAll<HTMLImageElement>("img.artifact-ref-image[data-artifact-index]"),
  ).filter((img) => img.dataset.authHydrated !== "1");
  if (!images.length) return;
  await Promise.all(
    images.map(async (img) => {
      const index = Number(img.getAttribute("data-artifact-index"));
      if (!Number.isInteger(index) || index < 0) return;
      img.dataset.authHydrated = "1";
      const blobURL = await loadArtifactBlobURL(ctx, index, download);
      if (!blobURL) {
        img.dataset.authHydrated = "0";
        return;
      }
      img.src = blobURL;
      img.removeAttribute("data-img-loading");
    }),
  );
}
