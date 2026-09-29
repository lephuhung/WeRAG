"use client";

import "highlight.js/styles/github.css";
import { memo, useCallback, useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { renderChatMarkdown, setActiveArtifacts } from "@/lib/markdown";
import { downloadArtifact, type ArtifactMeta } from "@/lib/api/chat";
import { hydrateArtifactImages, loadArtifactFullURL } from "@/lib/artifact-images";

type HoveredArtifact = {
  /** Fixed-viewport position of the image's top-right corner. */
  top: number;
  left: number;
  /** Artifact index for authenticated downloads; null for plain markdown images. */
  artifactIndex: number | null;
  src: string;
  alt: string;
};
export const Markdown = memo(function Markdown({
  text,
  streaming,
  artifacts,
  imageContext,
}: {
  text: string;
  streaming?: boolean;
  /** This message's artifacts — artifact image destinations resolve here. */
  artifacts?: ArtifactMeta[] | null;
  /** Session/message identity for authenticated artifact downloads. */
  imageContext?: { sessionId: string; messageId: string } | null;
}) {
  // Defer the source so rapid stream chunks coalesce into a single parse
  // instead of re-running marked + DOMPurify + highlight.js per token.
  // memo() additionally skips reconciling past messages entirely — their
  // text/streaming props don't change between chunks.
  const deferred = useDeferredValue(text);
  const rootRef = useRef<HTMLDivElement>(null);
  const [lightbox, setLightbox] = useState<{ src: string; alt: string } | null>(null);
  const [hovered, setHovered] = useState<HoveredArtifact | null>(null);
  const hoverHideTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  // The image element currently under the hover overlay — the reposition
  // effect re-reads its live rect on scroll/resize.
  const hoverImageRef = useRef<HTMLImageElement | null>(null);
  const html = useMemo(() => {
    setActiveArtifacts(artifacts ?? null);
    try {
      return renderChatMarkdown(deferred);
    } finally {
      setActiveArtifacts(null);
    }
  }, [deferred, artifacts]);
  // Swap artifact-image placeholders for authenticated blob URLs after each
  // render — same hydrate-after-paint contract as the Vue panel.
  useEffect(() => {
    if (!imageContext) return;
    return hydrateArtifactImages(rootRef.current, imageContext, downloadArtifact);
  }, [html, imageContext]);
  // Shared lightbox opener for both the image click and the hover "open"
  // button. Artifact images render from the downscaled thumbnail; the modal
  // upgrades to the original bytes in the background.
  const openLightbox = useCallback(
    (src: string, alt: string, indexRaw: string | undefined) => {
      setLightbox({ src, alt });
      if (indexRaw === undefined || !imageContext) return;
      const index = Number(indexRaw);
      if (!Number.isInteger(index) || index < 0) return;
      let cancelled = false;
      loadArtifactFullURL(imageContext, index, downloadArtifact).then((full) => {
        if (!cancelled && full) setLightbox((prev) => (prev && prev.src === src ? { src: full, alt } : prev));
      });
      lightboxCleanup.current = () => {
        cancelled = true;
      };
    },
    [imageContext],
  );
  const onRootClick = useCallback(
    (event: React.MouseEvent<HTMLDivElement>) => {
      const img = (event.target as HTMLElement).closest?.("img.markdown-image") as HTMLImageElement | null;
      if (!img || img.dataset.imgLoading === "1" || !img.getAttribute("src")) return;
      event.preventDefault();
      openLightbox(img.getAttribute("src") ?? "", img.alt, img.dataset.artifactIndex);
    },
    [openLightbox],
  );
  const lightboxCleanup = useRef<(() => void) | null>(null);
  // Hover overlay for the download button — delegated so the raw markdown
  // markup stays untouched. The button parks itself at the image's top-right
  // corner; a small hide delay keeps it reachable as the pointer crosses the
  // gap between image and button.
  const scheduleHoverHide = useCallback(() => {
    if (hoverHideTimer.current) clearTimeout(hoverHideTimer.current);
    hoverHideTimer.current = setTimeout(() => {
      hoverImageRef.current = null;
      setHovered(null);
    }, 150);
  }, []);
  const onRootMouseOver = useCallback(
    (event: React.MouseEvent<HTMLDivElement>) => {
      const img = (event.target as HTMLElement).closest?.("img.markdown-image") as HTMLImageElement | null;
      if (!img || img.dataset.imgLoading === "1" || !img.getAttribute("src")) return;
      if (hoverHideTimer.current) clearTimeout(hoverHideTimer.current);
      const rect = img.getBoundingClientRect();
      const indexRaw = img.dataset.artifactIndex;
      const index = indexRaw !== undefined ? Number(indexRaw) : null;
      hoverImageRef.current = img;
      setHovered({
        top: rect.bottom,
        left: rect.right,
        artifactIndex: index !== null && Number.isInteger(index) && index >= 0 ? index : null,
        src: img.getAttribute("src") ?? "",
        alt: img.alt,
      });
    },
    [],
  );
  // The overlay uses fixed viewport coords, so it must follow the image while
  // the chat scrolls. Keep re-reading the live rect of the hovered image;
  // hide the buttons once the image itself leaves the viewport.
  useEffect(() => {
    if (!hovered) return;
    const reposition = () => {
      const img = hoverImageRef.current;
      if (!img || !img.isConnected) {
        setHovered(null);
        return;
      }
      const rect = img.getBoundingClientRect();
      if (rect.bottom < 0 || rect.top > window.innerHeight) {
        setHovered(null);
        return;
      }
      setHovered((prev) => (prev ? { ...prev, top: rect.bottom, left: rect.right } : prev));
    };
    window.addEventListener("scroll", reposition, true);
    window.addEventListener("resize", reposition);
    return () => {
      window.removeEventListener("scroll", reposition, true);
      window.removeEventListener("resize", reposition);
    };
  }, [hovered?.src, hovered?.artifactIndex]);
  const downloadHoveredImage = useCallback(async () => {
    if (!hovered) return;
    const fallbackName =
      (hovered.alt || "image").trim().replace(/[\\/:*?"<>|]+/g, "_").slice(0, 60) || "image";
    let blob: Blob | null = null;
    let fileName = `${fallbackName}.png`;
    if (hovered.artifactIndex !== null && imageContext) {
      // Original bytes through the authenticated endpoint; prefer the
      // artifact's own file name for the saved file.
      const name = artifacts?.[hovered.artifactIndex]?.file_name?.trim();
      if (name) fileName = name;
      blob = await downloadArtifact(imageContext.sessionId, imageContext.messageId, hovered.artifactIndex);
    } else {
      blob = await fetch(hovered.src).then((res) => res.blob());
    }
    if (!blob) return;
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = fileName;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 10_000);
  }, [hovered, imageContext, artifacts]);
  // Close on Escape while the lightbox is open.
  useEffect(() => {
    if (!lightbox) {
      lightboxCleanup.current?.();
      lightboxCleanup.current = null;
      return;
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setLightbox(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [lightbox]);
  return (
    <>
      <div
        ref={rootRef}
        className="chat-markdown"
        dangerouslySetInnerHTML={{ __html: html }}
        data-streaming={streaming ? "1" : undefined}
        onClick={onRootClick}
        onMouseOver={onRootMouseOver}
        onMouseLeave={scheduleHoverHide}
      />
      {hovered && !lightbox
        ? createPortal(
            <div
              className="fixed z-[9998] flex items-center gap-1"
              style={{ top: hovered.top - 8, left: hovered.left - 8, transform: "translate(-100%, -100%)" }}
              onMouseEnter={() => {
                if (hoverHideTimer.current) clearTimeout(hoverHideTimer.current);
              }}
              onMouseLeave={scheduleHoverHide}
            >
              <button
                type="button"
                className="flex items-center gap-1 rounded-full bg-black/60 px-2.5 py-1 text-xs text-white shadow-lg backdrop-blur-sm transition-colors hover:bg-black/80"
                aria-label="Mở ảnh lớn"
                title="Xem ảnh lớn"
                onClick={(event) => {
                  event.stopPropagation();
                  setHovered(null);
                  openLightbox(hovered.src, hovered.alt, hovered.artifactIndex !== null ? String(hovered.artifactIndex) : undefined);
                }}
              >
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
                  <circle cx="11" cy="11" r="8" />
                  <line x1="21" y1="21" x2="16.65" y2="16.65" />
                  <line x1="11" y1="8" x2="11" y2="14" />
                  <line x1="8" y1="11" x2="14" y2="11" />
                </svg>
                Mở
              </button>
              <button
                type="button"
                className="flex items-center gap-1 rounded-full bg-black/60 px-2.5 py-1 text-xs text-white shadow-lg backdrop-blur-sm transition-colors hover:bg-black/80"
                aria-label="Tải xuống ảnh"
                title="Tải ảnh gốc"
                onClick={(event) => {
                  event.stopPropagation();
                  void downloadHoveredImage();
                }}
              >
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
                  <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                  <polyline points="7 10 12 15 17 10" />
                  <line x1="12" y1="15" x2="12" y2="3" />
                </svg>
                Tải xuống
              </button>
            </div>,
            document.body,
          )
        : null}
      {lightbox
        ? createPortal(
            <div
              className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/70 p-4 backdrop-blur-sm"
              onClick={() => setLightbox(null)}
              role="dialog"
              aria-modal="true"
              aria-label={lightbox.alt || "image preview"}
            >
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img
                src={lightbox.src}
                alt={lightbox.alt}
                className="max-h-full max-w-full rounded-lg object-contain shadow-2xl"
                onClick={(event) => event.stopPropagation()}
              />
              <button
                type="button"
                aria-label="Close"
                className="absolute top-4 right-4 rounded-full bg-white/10 px-3 py-1.5 text-sm text-white hover:bg-white/20"
                onClick={() => setLightbox(null)}
              >
                ✕
              </button>
            </div>,
            document.body,
          )
        : null}
    </>
  );
});
