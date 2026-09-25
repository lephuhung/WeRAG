"use client";

import { useEffect, useState } from "react";
import { IconClose, IconDoc, IconDownload, IconExternal } from "@/components/icons";
import { Markdown } from "@/components/markdown";

export interface DocPreviewSource {
  title: string;
  fileName?: string;
  fileType?: string;
  sizeBytes?: number;
  fetchBlob: () => Promise<Blob>;
}

export function DocPreviewModal({
  source,
  onClose,
  canDownloadOriginal = true,
}: {
  source: DocPreviewSource | null;
  onClose: () => void;
  /* Resource capability gate (UI affordance only — the backend still
   * authorizes /preview and /download, and nothing here can stop
   * browser-level saving of content already delivered for preview).
   * The /preview endpoint serves the original file, so the modal's
   * explicit Download controls are original-source download affordances
   * and are hidden when false. Defaults true so unrelated callers
   * (e.g. the artifacts page) keep download without code changes. */
  canDownloadOriginal?: boolean;
}) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [blobUrl, setBlobUrl] = useState<string | null>(null);
  const [textContent, setTextContent] = useState<string | null>(null);
  const [previewKind, setPreviewKind] = useState<"image" | "pdf" | "markdown" | "code" | "unsupported">("unsupported");

  useEffect(() => {
    if (!source) {
      setBlobUrl(null);
      setTextContent(null);
      setError("");
      return;
    }

    let active = true;
    let urlToRevoke: string | null = null;

    setLoading(true);
    setError("");
    setTextContent(null);

    source
      .fetchBlob()
      .then(async (blob) => {
        if (!active) return;
        const url = URL.createObjectURL(blob);
        urlToRevoke = url;
        setBlobUrl(url);

        const name = (source.fileName || source.title || "").toLowerCase();
        const mime = (blob.type || source.fileType || "").toLowerCase();

        const isImage = mime.startsWith("image/") || /\.(png|jpe?g|gif|svg|webp|bmp|ico)$/.test(name);
        const isPdf = mime.includes("pdf") || name.endsWith(".pdf");
        const isMd = name.endsWith(".md") || name.endsWith(".markdown");
        const isCodeOrText =
          mime.startsWith("text/") ||
          mime.includes("json") ||
          /\.(txt|json|csv|ts|tsx|js|jsx|py|go|rs|c|cpp|h|java|html|css|yaml|yml|xml|sh|bash|sql|log)$/.test(name);

        if (isImage) {
          setPreviewKind("image");
        } else if (isPdf) {
          setPreviewKind("pdf");
        } else if (isMd) {
          const text = await blob.text();
          if (active) {
            setTextContent(text);
            setPreviewKind("markdown");
          }
        } else if (isCodeOrText) {
          const text = await blob.text();
          if (active) {
            setTextContent(text);
            setPreviewKind("code");
          }
        } else {
          setPreviewKind("unsupported");
        }
      })
      .catch((err) => {
        if (!active) return;
        setError(err instanceof Error ? err.message : "Failed to load document preview");
      })
      .finally(() => {
        if (active) setLoading(false);
      });

    return () => {
      active = false;
      if (urlToRevoke) URL.revokeObjectURL(urlToRevoke);
    };
  }, [source]);

  if (!source) return null;

  const displayName = source.fileName || source.title;

  const handleDownload = () => {
    if (!blobUrl) return;
    const a = document.createElement("a");
    a.href = blobUrl;
    a.download = displayName;
    a.click();
  };

  const handleOpenExternal = () => {
    if (!blobUrl) return;
    window.open(blobUrl, "_blank");
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm animate-in fade-in duration-150">
      <div
        className="fixed inset-0"
        onClick={onClose}
        aria-hidden="true"
      />
      <div
        className="relative flex h-[88vh] w-full max-w-[1020px] flex-col overflow-hidden rounded-[20px] border border-hairline-strong bg-canvas shadow-[0_32px_64px_rgba(0,0,0,0.3)]"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Modal Header */}
        <div className="flex shrink-0 items-center justify-between border-b border-hairline bg-surface-card px-6 py-4">
          <div className="flex min-w-0 items-center gap-3">
            <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-surface text-ink">
              <IconDoc className="h-5 w-5" />
            </div>
            <div className="min-w-0">
              <h3 className="truncate text-[15px] font-semibold text-ink">{displayName}</h3>
              {source.sizeBytes ? (
                <span className="caption text-muted">{fmtBytes(source.sizeBytes)}</span>
              ) : null}
            </div>
          </div>

          <div className="flex items-center gap-2">
            {blobUrl && (
              <>
                <button
                  onClick={handleOpenExternal}
                  className="btn btn-outline btn-sm"
                  title="Open in new window"
                >
                  <IconExternal className="h-3.5 w-3.5" /> Open
                </button>
                {canDownloadOriginal && (
                  <button
                    onClick={handleDownload}
                    className="btn btn-primary btn-sm"
                    title="Download file"
                  >
                    <IconDownload className="h-3.5 w-3.5" /> Download
                  </button>
                )}
              </>
            )}
            <button
              onClick={onClose}
              className="rounded-full p-1.5 text-muted hover:bg-surface-strong hover:text-ink"
            >
              <IconClose className="h-5 w-5" />
            </button>
          </div>
        </div>

        {/* Modal Body */}
        <div className="flex min-h-0 flex-1 flex-col overflow-hidden bg-canvas">
          {loading ? (
            <div className="flex flex-1 flex-col items-center justify-center gap-3 text-muted">
              <span className="h-6 w-6 animate-spin rounded-full border-2 border-hairline-strong border-t-ink" />
              <p className="caption">Loading document preview…</p>
            </div>
          ) : error ? (
            <div className="flex flex-1 flex-col items-center justify-center gap-2 p-8 text-center">
              <p className="text-[14px] font-medium text-error">{error}</p>
              <p className="caption text-muted">Unable to load preview for this file.</p>
            </div>
          ) : previewKind === "image" && blobUrl ? (
            <div className="flex flex-1 items-center justify-center overflow-auto p-6">
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img
                src={blobUrl}
                alt={displayName}
                className="max-h-full max-w-full rounded-lg object-contain shadow-sm"
              />
            </div>
          ) : previewKind === "pdf" && blobUrl ? (
            <div className="flex-1 bg-surface-strong">
              <iframe
                src={blobUrl}
                title={displayName}
                className="h-full w-full border-0"
              />
            </div>
          ) : previewKind === "markdown" && textContent !== null ? (
            <div className="flex-1 overflow-y-auto px-8 py-6">
              <div className="mx-auto max-w-[800px]">
                <Markdown text={textContent} />
              </div>
            </div>
          ) : previewKind === "code" && textContent !== null ? (
            <div className="flex-1 overflow-auto bg-surface-card p-6">
              <pre className="font-mono text-[13px] leading-relaxed text-ink selection:bg-accent/20">
                <code>{textContent}</code>
              </pre>
            </div>
          ) : (
            <div className="flex flex-1 flex-col items-center justify-center gap-4 p-8 text-center text-muted">
              <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-surface-strong text-ink">
                <IconDoc className="h-7 w-7" />
              </div>
              <div>
                <p className="text-[15px] font-medium text-ink">Preview not available</p>
                <p className="caption mt-1 text-muted">
                  {canDownloadOriginal
                    ? "Direct in-browser preview is not supported for this file format. You can download the file to inspect it."
                    : "Direct in-browser preview is not supported for this file format."}
                </p>
              </div>
              {canDownloadOriginal && (
                <button
                  onClick={handleDownload}
                  disabled={!blobUrl}
                  className="btn btn-primary btn-sm"
                >
                  <IconDownload className="h-4 w-4" /> Download original file
                </button>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function fmtBytes(bytes: number): string {
  if (!bytes) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1);
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`;
}
