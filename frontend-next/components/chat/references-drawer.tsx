"use client";

import { useEffect, useMemo, useState } from "react";
import { SlidePanel, SlidePanelHeader } from "@/components/slide-panel";
import { IconDoc } from "@/components/icons";
import { Markdown } from "@/components/markdown";
import { getChunkByIdOnly } from "@/lib/api/knowledge";
import { copyToClipboard } from "@/lib/clipboard";

export type KnowledgeReferenceItem = {
  id?: string;
  content?: string;
  knowledge_title?: string;
  knowledge_id?: string;
  knowledge_base_id?: string;
  knowledge_filename?: string;
  chunk_id?: string;
  chunk_index?: number;
  chunk_type?: string;
  metadata?: Record<string, string>;
  score?: number;
};

function getRefKey(r: KnowledgeReferenceItem, index: number): string {
  return r.chunk_id || r.id || `${r.knowledge_id || "ref"}-${index}`;
}

export function ReferencesDrawer({
  open,
  onClose,
  references = [],
  activeKey,
}: {
  open: boolean;
  onClose: () => void;
  references?: KnowledgeReferenceItem[];
  activeKey?: string | null;
}) {
  const [copied, setCopied] = useState(false);
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [chunkContentMap, setChunkContentMap] = useState<Record<string, string>>({});
  const [loadingChunkId, setLoadingChunkId] = useState<string | null>(null);

  // Sync selectedKey when drawer opens or activeKey changes
  useEffect(() => {
    if (!open) return;
    if (activeKey) {
      setSelectedKey(activeKey);
    } else if (references.length > 0) {
      setSelectedKey(getRefKey(references[0], 0));
    }
  }, [open, activeKey, references]);

  // Find the active reference item
  const currentRef = useMemo(() => {
    if (!references.length) return null;
    let idx = references.findIndex((r, i) => getRefKey(r, i) === selectedKey);
    if (idx === -1) idx = 0;
    return references[idx];
  }, [references, selectedKey]);

  // Lazy-load ONLY the selected chunk ID if content is not already present
  const currentChunkId = currentRef ? currentRef.chunk_id || currentRef.id : null;
  const hasEmbeddedContent = Boolean(currentRef?.content && currentRef.content.trim());

  useEffect(() => {
    if (!open || !currentChunkId || hasEmbeddedContent) return;

    // Already cached
    if (chunkContentMap[currentChunkId] !== undefined) return;

    setLoadingChunkId(currentChunkId);
    let alive = true;

    getChunkByIdOnly(currentChunkId, { includeImageText: true })
      .then((res) => {
        if (!alive) return;
        const raw = res as {
          data?: { content?: string; chunk?: { content?: string } } | string;
          content?: string;
        };
        let content = "";
        if (typeof raw?.data === "string") {
          content = raw.data;
        } else if (raw?.data?.content) {
          content = raw.data.content;
        } else if (raw?.data?.chunk?.content) {
          content = raw.data.chunk.content;
        } else if (raw?.content) {
          content = raw.content;
        }
        setChunkContentMap((prev) => ({ ...prev, [currentChunkId]: content }));
      })
      .catch((err) => {
        if (!alive) return;
        console.warn("[ReferencesDrawer] Failed to fetch chunk content:", currentChunkId, err);
        setChunkContentMap((prev) => ({ ...prev, [currentChunkId]: "" }));
      })
      .finally(() => {
        if (alive) setLoadingChunkId(null);
      });

    return () => {
      alive = false;
    };
  }, [open, currentChunkId, hasEmbeddedContent, chunkContentMap]);

  const handleCopy = async (text: string) => {
    if (!text) return;
    const ok = await copyToClipboard(text);
    if (ok) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  const title =
    currentRef?.knowledge_title ||
    currentRef?.knowledge_filename ||
    currentRef?.knowledge_id ||
    "Văn bản gốc";

  const isWeb = currentRef?.chunk_type === "web_search" || Boolean(currentRef?.metadata?.url);
  const webUrl = currentRef?.metadata?.url || (currentRef?.id?.startsWith("http") ? currentRef.id : undefined);

  const fetchedText = currentChunkId ? chunkContentMap[currentChunkId] : undefined;
  const isLoading = currentChunkId === loadingChunkId;
  const contentText = (currentRef?.content || fetchedText || "").trim();

  return (
    <SlidePanel
      open={open}
      onClose={onClose}
      label="Văn bản gốc"
      width="w-[540px]"
    >
      <SlidePanelHeader
        title="Văn bản gốc"
        subtitle={title}
        onClose={onClose}
      />

      <div className="flex min-h-0 flex-1 flex-col p-5 overflow-y-auto">
        {!currentRef ? (
          <div className="flex flex-col items-center justify-center py-20 text-center text-muted">
            <IconDoc className="h-8 w-8 stroke-[1.4] opacity-50 mb-2" />
            <p className="text-[13.5px] font-medium text-ink">Không tìm thấy thông tin đoạn trích</p>
          </div>
        ) : (
          <div className="flex flex-col gap-4">
            {/* Meta bar: Tên văn bản + Độ khớp (%) + Nút Copy / Link */}
            <div className="flex items-center justify-between gap-3 rounded-xl border border-hairline bg-surface-strong/35 px-3.5 py-2.5">
              <div className="flex items-center gap-2 min-w-0">
                <IconDoc className="h-4 w-4 shrink-0 text-primary" />
                <span className="truncate text-[13.5px] font-medium text-ink" title={title}>
                  {title}
                </span>
                {currentRef.score !== undefined && (
                  <span className="shrink-0 rounded-md bg-emerald-500/10 px-2 py-0.5 text-[11.5px] font-semibold text-emerald-600 dark:text-emerald-400">
                    {(currentRef.score * 100).toFixed(0)}%
                  </span>
                )}
              </div>

              <div className="flex items-center gap-1.5 shrink-0">
                {contentText && (
                  <button
                    type="button"
                    onClick={() => handleCopy(contentText)}
                    className="flex items-center gap-1 rounded-lg border border-hairline bg-surface-card px-2.5 py-1 text-[12px] text-muted hover:text-ink hover:border-ink/30 transition-colors cursor-pointer"
                    title="Sao chép nội dung"
                  >
                    {copied ? (
                      <>
                        <span className="text-emerald-600 dark:text-emerald-400">✓</span>
                        <span className="text-emerald-600 dark:text-emerald-400 font-medium">Đã chép</span>
                      </>
                    ) : (
                      <>
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" className="h-3.5 w-3.5">
                          <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
                          <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
                        </svg>
                        <span>Copy</span>
                      </>
                    )}
                  </button>
                )}

                {isWeb && webUrl && (
                  <a
                    href={webUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center gap-1 rounded-lg border border-hairline bg-surface-card px-2.5 py-1 text-[12px] text-muted hover:text-ink transition-colors"
                    title="Mở liên kết web"
                  >
                    <span>Mở web</span>
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="h-3 w-3">
                      <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
                      <polyline points="15 3 21 3 21 9" />
                      <line x1="10" y1="14" x2="21" y2="3" />
                    </svg>
                  </a>
                )}

                {!isWeb && currentRef.knowledge_base_id && (
                  <a
                    href={`/platform/knowledge-bases/${currentRef.knowledge_base_id}${
                      currentRef.knowledge_id ? `?knowledge_id=${currentRef.knowledge_id}` : ""
                    }`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center gap-1 rounded-lg border border-hairline bg-surface-card px-2.5 py-1 text-[12px] text-muted hover:text-ink transition-colors"
                    title="Mở trong kho tri thức"
                  >
                    <span>Xem file</span>
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="h-3 w-3">
                      <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
                      <polyline points="15 3 21 3 21 9" />
                      <line x1="10" y1="14" x2="21" y2="3" />
                    </svg>
                  </a>
                )}
              </div>
            </div>

            {/* Markdown Content (No outer border box) */}
            <div className="select-text pt-2 text-ink leading-relaxed">
              {isLoading ? (
                <div className="flex flex-col items-center justify-center py-16 text-muted text-xs gap-2.5">
                  <svg className="h-5 w-5 animate-spin text-primary" viewBox="0 0 24 24" fill="none">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" />
                    <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v4a4 4 0 00-4 4H4z" />
                  </svg>
                  <span>Đang tải nội dung văn bản gốc...</span>
                </div>
              ) : contentText ? (
                <Markdown text={contentText} />
              ) : (
                <div className="flex flex-col items-center justify-center py-16 text-muted-soft text-xs italic text-center">
                  (Không tìm thấy nội dung văn bản gốc cho đoạn trích này)
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </SlidePanel>
  );
}
