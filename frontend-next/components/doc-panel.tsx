"use client";

import { useEffect, useRef, useState } from "react";
import {
  downloadKnowledge,
  listKnowledgeChunks,
  previewKnowledgeFile,
  type KnowledgeDoc,
} from "@/lib/api/knowledge";
import { SlidePanel, SlidePanelHeader } from "@/components/slide-panel";
import { Markdown } from "@/components/markdown";
import { renderFileIconSvg } from "@/components/files/file-icon";
import { IconDoc, IconExternal, IconPulse } from "@/components/icons";
import { DocPreviewModal, type DocPreviewSource } from "@/components/doc-preview-modal";
import {
  ProcessingTimeline,
  ProcessingTimelineDrawer,
  type TimelineSummary,
} from "@/components/knowledge/processing-timeline";
import { useT } from "@/lib/i18n";

const STATUS_STYLE: Record<string, { label: string; cls: string; dot: string }> = {
  completed: { label: "Indexed", cls: "text-success", dot: "#16a34a" },
  processing: { label: "Processing", cls: "text-muted", dot: "#a8a29e" },
  finalizing: { label: "Processing", cls: "text-muted", dot: "#a8a29e" },
  pending: { label: "Pending", cls: "text-muted", dot: "#a8a29e" },
  failed: { label: "Failed", cls: "text-error", dot: "#dc2626" },
  cancelled: { label: "Cancelled", cls: "text-muted", dot: "#a8a29e" },
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
  const [previewSource, setPreviewSource] = useState<DocPreviewSource | null>(null);
  /* Processing pipeline trace (port of the Vue doc-content.vue mounts):
   * a hidden compact timeline keeps /spans polling while parsing is in
   * flight and reports hasSpans + a one-line summary; the trace button
   * in the header opens the full waterfall in a secondary drawer. */
  const [hasTrace, setHasTrace] = useState(false);
  const [traceOpen, setTraceOpen] = useState(false);
  const [traceSummary, setTraceSummary] = useState<TimelineSummary | null>(null);
  const traceOpenRef = useRef(false);
  traceOpenRef.current = traceOpen;
  const { t } = useT();

  useEffect(() => {
    setChunks(null);
    setHasTrace(false);
    setTraceOpen(false);
    setTraceSummary(null);
    if (!doc) return;
    let alive = true;
    listKnowledgeChunks(doc.id, 1, { includeImageText: true })
      .then((res) => alive && setChunks(chunkText(res)))
      .catch(() => alive && setChunks([]));
    return () => {
      alive = false;
    };
  }, [doc]);

  const st = doc ? (STATUS_STYLE[doc.parse_status ?? doc.status ?? ""] ?? STATUS_STYLE.pending) : null;
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

  const openPreview = () => {
    if (!doc) return;
    setPreviewSource({
      title: name,
      fileName: doc.file_name || name,
      fileType: doc.file_type,
      sizeBytes: doc.file_size,
      fetchBlob: () =>
        previewKnowledgeFile(doc.id).catch(() => downloadKnowledge(doc.id)),
    });
  };

  const traceIconColor =
    traceSummary && ["done", "completed"].includes(traceSummary.status)
      ? "text-success"
      : traceSummary && ["failed"].includes(traceSummary.status)
        ? "text-error"
        : traceSummary && ["running", "processing", "pending"].includes(traceSummary.status)
          ? "text-amber-500"
          : "text-muted";

  const traceTitle = (() => {
    let tip = t("ks.viewTrace");
    if (traceSummary && traceSummary.totalMs > 0) {
      tip += ` · ${formatDurationText(traceSummary.totalMs)}`;
    } else if (traceSummary && traceSummary.stageTotal > 0) {
      tip += ` · ${traceSummary.stageIndex}/${traceSummary.stageTotal}`;
    }
    return tip;
  })();

  /* Escape/backdrop must not close the doc panel while the trace drawer
   * is open above it — SlidePanel's window keydown would otherwise
   * close both at once. */
  const closePanel = () => {
    if (traceOpenRef.current) return;
    onClose();
  };

  return (
    <>
      <SlidePanel open={doc !== null} onClose={closePanel} label="Document" width="w-[560px]">
        <SlidePanelHeader
          title={name}
          subtitle="Extracted chunks"
          onClose={closePanel}
          actions={
            doc && hasTrace ? (
              <button
                type="button"
                title={traceTitle}
                aria-label={traceTitle}
                onClick={() => setTraceOpen(true)}
                className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-full transition-colors hover:bg-surface-strong ${traceIconColor}`}
              >
                <IconPulse className="h-4 w-4" />
              </button>
            ) : undefined
          }
        >
          {doc && (
            <span
              className="w-[30px] shrink-0"
              dangerouslySetInnerHTML={{
                __html: renderFileIconSvg(doc.file_name ?? name, doc.file_type, doc.profile?.doc_type),
              }}
            />
          )}
        </SlidePanelHeader>

        {/* Hidden compact mount: keeps the timeline fetching so the
         * header button's status stays live even before the user opens
         * the trace drawer. gracePoll=false → stops polling once the
         * parse pipeline itself reaches a terminal status. */}
        {doc && (
          <div className="hidden" aria-hidden="true">
            <ProcessingTimeline
              knowledgeId={doc.id}
              parseStatus={doc.parse_status ?? doc.status}
              compact
              gracePoll={false}
              onHasSpans={setHasTrace}
              onSummary={setTraceSummary}
            />
          </div>
        )}
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
              <span className="caption ml-auto whitespace-nowrap text-muted">
                {doc.chunk_count ? `${doc.chunk_count} chunks · ` : ""}
                {fmtShortDate(doc.updated_at ?? "")}
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
                  <div key={i} className="max-w-[640px]">
                    <Markdown text={p} />
                  </div>
                ))}
              </div>

              <div className="mt-8 flex flex-wrap gap-3 border-t border-hairline pt-5">
                <button
                  className="btn btn-primary btn-sm"
                  onClick={openPreview}
                >
                  <IconExternal className="h-3.5 w-3.5" /> Preview file
                </button>
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
      <DocPreviewModal
        source={previewSource}
        onClose={() => setPreviewSource(null)}
      />
      {doc && (
        <ProcessingTimelineDrawer
          open={traceOpen}
          knowledgeId={doc.id}
          parseStatus={doc.parse_status ?? doc.status}
          docTitle={name}
          onClose={() => setTraceOpen(false)}
        />
      )}
    </>
  );
}
/* Duration for the trace button tooltip (same shape as the timeline's). */
function formatDurationText(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(2)}s`;
  const mins = Math.floor(ms / 60000);
  const rem = ((ms % 60000) / 1000).toFixed(1);
  return `${mins}m${rem}s`;
}
/* Compact header timestamp — RFC3339 too long for the meta row. */
function fmtShortDate(v?: string): string {
  if (!v) return "";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return v;
  return d.toLocaleDateString(undefined, { year: "2-digit", month: "short", day: "numeric" });
}
