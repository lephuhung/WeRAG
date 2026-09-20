"use client";

import { useEffect, useState } from "react";
import {
  downloadKnowledge,
  listKnowledgeChunks,
  type KnowledgeDoc,
} from "@/lib/api/knowledge";
import { SlidePanel, SlidePanelHeader } from "@/components/slide-panel";
import { IconDoc } from "@/components/icons";

const STATUS_STYLE: Record<string, { label: string; cls: string; dot: string }> = {
  parsed: { label: "Indexed", cls: "text-success", dot: "#16a34a" },
  indexed: { label: "Indexed", cls: "text-success", dot: "#16a34a" },
  processing: { label: "Processing", cls: "text-muted", dot: "#a8a29e" },
  parsing: { label: "Processing", cls: "text-muted", dot: "#a8a29e" },
  pending: { label: "Processing", cls: "text-muted", dot: "#a8a29e" },
  failed: { label: "Failed", cls: "text-error", dot: "#dc2626" },
};

type ChunkRow = { id?: string; content?: string };

function chunkText(res: unknown): string[] {
  const r = res as { data?: { items?: ChunkRow[] } | ChunkRow[] };
  const items = Array.isArray(r?.data) ? r.data : (r?.data?.items ?? []);
  return items.map((c) => c.content ?? "").filter(Boolean);
}

/** Right slide-in panel showing a KB document's extracted chunks. */
export function DocPanel({
  doc,
  onClose,
}: {
  doc: KnowledgeDoc | null;
  onClose: () => void;
}) {
  const [chunks, setChunks] = useState<string[] | null>(null);
  const [downloading, setDownloading] = useState(false);

  useEffect(() => {
    setChunks(null);
    if (!doc) return;
    let alive = true;
    listKnowledgeChunks(doc.id, 1)
      .then((res) => alive && setChunks(chunkText(res)))
      .catch(() => alive && setChunks([]));
    return () => {
      alive = false;
    };
  }, [doc]);

  const st = doc ? (STATUS_STYLE[doc.status ?? ""] ?? STATUS_STYLE.processing) : null;
  const name = doc ? (doc.title || doc.file_name || doc.id) : "";

  const download = async () => {
    if (!doc || downloading) return;
    setDownloading(true);
    try {
      const blob = await downloadKnowledge(doc.id);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = doc.file_name || name;
      a.click();
      URL.revokeObjectURL(url);
    } finally {
      setDownloading(false);
    }
  };

  return (
    <SlidePanel open={doc !== null} onClose={onClose} label="Document" width="w-[520px]">
      <SlidePanelHeader title={name} subtitle="Extracted chunks" onClose={onClose}>
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-surface-strong text-ink">
          <IconDoc className="h-4 w-4" />
        </div>
      </SlidePanelHeader>

      {doc && st && (
        <>
          {/* meta */}
          <div className="flex shrink-0 flex-wrap items-center gap-3 border-b border-hairline px-5 py-3">
            <span className="badge-pill">
              {(() => {
                const n = doc.file_name ?? doc.title ?? "";
                const dot = n.lastIndexOf(".");
                return dot >= 0 ? n.slice(dot + 1).toUpperCase() : "FILE";
              })()}
            </span>
            <span className={`caption flex items-center gap-1.5 font-medium ${st.cls}`}>
              <span
                className="inline-block h-1.5 w-1.5 rounded-full"
                style={{ background: st.dot }}
              />
              {st.label}
            </span>
            <span className="caption ml-auto text-muted">
              {doc.chunk_count ? `${doc.chunk_count} chunks` : ""} {doc.updated_at ?? ""}
            </span>
          </div>

          {/* extracted text */}
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-6">
            <div className="caption-uppercase mb-3 text-muted-soft">Extracted text</div>
            {chunks === null && <p className="caption text-muted">Loading…</p>}
            {chunks !== null && chunks.length === 0 && (
              <p className="caption text-muted">No extracted text available.</p>
            )}
            <div className="flex flex-col gap-4">
              {(chunks ?? []).map((p, i) => (
                <p
                  key={i}
                  className="whitespace-pre-wrap text-[15px] leading-[1.8] text-body"
                >
                  {p}
                </p>
              ))}
            </div>

            <div className="mt-8 flex gap-3 border-t border-hairline pt-5">
              <button
                className="btn btn-outline btn-sm"
                onClick={() => void download()}
                disabled={downloading}
              >
                <IconDoc className="h-3.5 w-3.5" />{" "}
                {downloading ? "Downloading…" : "Download original"}
              </button>
            </div>
          </div>
        </>
      )}
    </SlidePanel>
  );
}
