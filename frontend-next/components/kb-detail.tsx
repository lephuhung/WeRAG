"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import {
  getKnowledgeBase,
  listKnowledgeFiles,
  type KnowledgeBaseRow,
  type KnowledgeDoc,
} from "@/lib/api/knowledge";
import { WikiBrowser } from "@/components/wiki/wiki-browser";
import { KbSettingsModal } from "@/components/settings/kb-settings";
import { DocPanel } from "@/components/doc-panel";
import { KnowledgeGraph } from "@/components/knowledge-graph";
import { IconChat, IconDoc, IconPlus, IconSearch, IconSettings } from "@/components/icons";
import { renderFileIconSvg } from "@/components/files/file-icon";

/* Backend field is parse_status (types.Knowledge.go ParseStatus); values
 * are pending/processing/finalizing/completed/failed/cancelled. The port
 * previously read a phantom `status` field so every document fell through
 * to "Processing". */
const STATUS_STYLE: Record<string, { label: string; cls: string; dot: string }> = {
  completed: { label: "Indexed", cls: "text-success", dot: "#16a34a" },
  processing: { label: "Processing", cls: "text-muted", dot: "#a8a29e" },
  finalizing: { label: "Processing", cls: "text-muted", dot: "#a8a29e" },
  pending: { label: "Pending", cls: "text-muted", dot: "#a8a29e" },
  failed: { label: "Failed", cls: "text-error", dot: "#dc2626" },
  cancelled: { label: "Cancelled", cls: "text-muted", dot: "#a8a29e" },
};

function statusStyle(status?: string) {
  return STATUS_STYLE[status ?? ""] ?? STATUS_STYLE.pending;
}

function docName(d: KnowledgeDoc) {
  return d.title || d.file_name || d.id;
}

function docExt(d: KnowledgeDoc) {
  const name = d.file_name ?? d.title ?? "";
  const dot = name.lastIndexOf(".");
  return dot >= 0 ? name.slice(dot + 1).toUpperCase() : "FILE";
}

type PaneTab = "wiki" | "graph";

export function KbDetail({ kbId }: { kbId: string }) {
  const [pane, setPane] = useState<PaneTab>("wiki");
  const [kb, setKb] = useState<KnowledgeBaseRow | null>(null);
  const [docs, setDocs] = useState<KnowledgeDoc[] | null>(null);
  const [openDoc, setOpenDoc] = useState<KnowledgeDoc | null>(null);
  const [q, setQ] = useState("");
  const [wikiQ, setWikiQ] = useState("");
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    getKnowledgeBase(kbId)
      .then((row) => alive && setKb(row ?? null))
      .catch((e) => alive && setError(e instanceof Error ? e.message : "Failed to load"));
    listKnowledgeFiles(kbId, { page: 1, page_size: 100 })
      .then((res) => {
        if (!alive) return;
        const data = res.data;
        setDocs(Array.isArray(data) ? data : (data?.items ?? []));
      })
      .catch(() => alive && setDocs([]));
    return () => {
      alive = false;
    };
  }, [kbId]);

  const filtered = (docs ?? []).filter(
    (d) => !q || docName(d).toLowerCase().includes(q.toLowerCase()),
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      {/* header */}
      <div className="shrink-0 px-10 pt-8 pb-6">
        <div className="caption mb-5 flex items-center gap-2 text-muted">
          <Link href="/platform/knowledge-bases" className="hover:text-ink">
            Knowledge bases
          </Link>
          <span>/</span>
          <span className="text-ink">{kb?.name ?? kbId}</span>
        </div>

        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h1 className="display-lg">{kb?.name ?? "Knowledge base"}</h1>
            <p className="body-sm mt-2 max-w-[560px] text-body">{kb?.description ?? ""}</p>
            {error && <p className="caption mt-2 text-error">{error}</p>}
            <div className="caption mt-3 flex items-center gap-4 text-muted">
              <span>{kb?.knowledge_count ?? kb?.document_count ?? docs?.length ?? "—"} documents</span>
              {kb?.updated_at && <span className="whitespace-nowrap">Updated {fmtShortDate(kb.updated_at)}</span>}
            </div>
          </div>
          <div className="flex gap-3">
            <button className="btn btn-outline" onClick={() => setSettingsOpen(true)}>
              <IconSettings className="h-4 w-4" /> Settings
            </button>
            <button className="btn btn-outline">
              <IconPlus className="h-4 w-4" /> Upload files
            </button>
            <Link
              href={`/platform/knowledge-bases/${kbId}/creatChat`}
              className="btn btn-primary"
            >
              <IconChat className="h-4 w-4" /> Chat
            </Link>
          </div>
        </div>
      </div>

      {/* split panes: left documents, right wiki/graph */}
      <div className="grid min-h-0 flex-1 grid-cols-1 gap-5 px-10 pb-8 lg:grid-cols-3">
        {/* left — documents as cards */}
        <section className="flex min-h-0 flex-col lg:col-span-1">
          <div className="mb-4 flex shrink-0 items-center justify-between">
            <div className="relative w-full max-w-[300px]">
              <IconSearch className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-soft" />
              <input
                className="input h-10 pl-10 text-[14px]"
                placeholder="Search documents…"
                value={q}
                onChange={(e) => setQ(e.target.value)}
              />
            </div>
            <div className="caption text-muted">{filtered.length} files</div>
          </div>

          <div className="grid min-h-0 flex-1 auto-rows-min grid-cols-1 gap-3 overflow-y-auto pr-1">
            {filtered.map((d) => {
              const st = statusStyle(d.parse_status ?? d.status);
              return (
                <button
                  key={d.id}
                  onClick={() => setOpenDoc(d)}
                  className="card card-hover p-4 text-left"
                >
                  <div className="flex items-start gap-3">
                    <span
                      className="w-[30px] shrink-0"
                      dangerouslySetInnerHTML={{
                        __html: renderFileIconSvg(d.file_name ?? d.title ?? "", d.file_type, d.profile?.doc_type),
                      }}
                    />
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-[14px] font-medium text-ink">
                        {docName(d)}
                      </div>
                      {/* type badges: profile.doc_type first, ext + size as remaining line */}
                      <div className="caption mt-1 flex flex-wrap items-center gap-1.5 text-muted">
                        {d.profile?.doc_type && (
                          <span className="badge-pill">{d.profile.doc_type}</span>
                        )}
                        <span>{docExt(d)}</span>
                        {d.file_size ? (
                          <span className="text-muted-soft">· {fmtBytes(d.file_size)}</span>
                        ) : null}
                      </div>
                    </div>
                  </div>
                  {/* summary — description wins over profile.gist when both exist */}
                  {(d.description || d.profile?.gist) && (
                    <p className="body-sm mt-2 line-clamp-3 text-body">
                      {d.description || d.profile?.gist}
                    </p>
                  )}
                  {d.profile?.topics && d.profile.topics.length > 0 && (
                    <div className="mt-2 flex flex-wrap gap-1.5">
                      {d.profile.topics.slice(0, 4).map((topic) => (
                        <span
                          key={topic}
                          className="inline-block rounded-full bg-surface-strong px-2 py-0.5 text-[11px] text-muted"
                        >
                          {topic}
                        </span>
                      ))}
                      {d.profile.topics.length > 4 && (
                        <span className="caption text-muted-soft">
                          +{d.profile.topics.length - 4}
                        </span>
                      )}
                    </div>
                  )}
                  <div className="caption mt-3 flex items-center justify-between border-t border-hairline pt-3">
                    <span className={`flex items-center gap-1.5 font-medium ${st.cls}`}>
                      <span
                        className="inline-block h-1.5 w-1.5 rounded-full"
                        style={{ background: st.dot }}
                      />
                      {st.label}
                    </span>
                    <span className="whitespace-nowrap">{fmtShortDate(d.updated_at)}</span>
                  </div>
                </button>
              );
            })}

            {/* upload card */}
            <button className="flex min-h-[104px] items-center justify-center rounded-[16px] border border-dashed border-hairline-strong text-muted transition-colors hover:border-ink hover:text-ink">
              <span className="flex items-center gap-2 text-[14px] font-medium">
                <IconPlus className="h-4 w-4" /> Upload
              </span>
            </button>
          </div>
          {docs !== null && filtered.length === 0 && (
            <p className="caption mt-2 text-muted-soft">No documents found.</p>
          )}
        </section>

        <section className="card flex min-h-0 flex-col overflow-hidden lg:col-span-2">
          <div className="flex shrink-0 items-center justify-between border-b border-hairline px-5 py-3">
            <div className="flex items-center gap-1 rounded-full bg-surface-strong p-1">
              {(
                [
                  { id: "wiki", label: "Wiki" },
                  { id: "graph", label: "Knowledge graph" },
                ] as { id: PaneTab; label: string }[]
              ).map((t) => (
                <button
                  key={t.id}
                  onClick={() => setPane(t.id)}
                  className={`rounded-full px-3.5 py-1.5 text-[13px] font-medium transition-colors ${
                    pane === t.id
                      ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.06)]"
                      : "text-muted hover:text-ink"
                  }`}
                >
                  {t.label}
                </button>
              ))}
            </div>
            <span className="caption text-muted-soft">
              {pane === "wiki" ? "Wiki pages" : "Entities & relations"}
            </span>
          </div>
          {pane === "wiki" ? (
            <div className="flex min-h-0 flex-1 flex-col">
              <div className="caption flex items-center gap-2 border-b border-hairline px-5 py-2">
                <IconDoc className="h-4 w-4 text-muted-soft" />
                <input
                  className="bg-transparent text-[13px] outline-none placeholder:text-muted-soft"
                  placeholder="Search wiki pages…"
                  value={wikiQ}
                  onChange={(e) => setWikiQ(e.target.value)}
                />
              </div>
              <WikiBrowser kbId={kbId} q={wikiQ} />
            </div>
          ) : (
            <KnowledgeGraph kbId={kbId} />
          )}
        </section>
      </div>

      {/* document slide-in panel */}
      <DocPanel doc={openDoc} onClose={() => setOpenDoc(null)} />
      <KbSettingsModal
        kbId={kbId}
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
        onSaved={() => {
          getKnowledgeBase(kbId).then((row) => setKb(row ?? null)).catch(() => {});
        }}
      />
    </div>
  );
}	
/* Compact header timestamp — RFC3339 is too long for the stats row. */
function fmtShortDate(v?: string): string {
  if (!v) return "";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return v;
  return d.toLocaleDateString(undefined, { year: "2-digit", month: "short", day: "numeric" });
}

/* Human file size for the metadata row. */
function fmtBytes(bytes: number): string {
  if (!bytes) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1);
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`;
}
