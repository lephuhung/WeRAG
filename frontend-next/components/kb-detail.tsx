"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import {
  getKnowledgeBase,
  listKnowledgeFiles,
  type KnowledgeBaseRow,
  type KnowledgeDoc,
} from "@/lib/api/knowledge";
import { WikiBrowser, WikiPageView } from "@/components/wiki/wiki-browser";
import { KbSettingsModal } from "@/components/settings/kb-settings";
import { DocPanel } from "@/components/doc-panel";
import { KnowledgeGraph } from "@/components/knowledge-graph";
import { UploadModal } from "@/components/knowledge/upload-modal";
import {
  IconChat,
  IconDoc,
  IconGraph,
  IconPlus,
  IconSearch,
  IconSettings,
} from "@/components/icons";
import { renderFileIconSvg } from "@/components/files/file-icon";
import { useT, type LocaleKey } from "@/lib/i18n";
import { useTenantRole } from "@/lib/auth";

/* Backend field is parse_status (types.Knowledge.go ParseStatus); values
 * are pending/processing/finalizing/completed/failed/cancelled. The port
 * previously read a phantom `status` field so every document fell through
 * to "Processing". */
const STATUS_STYLE: Record<
  string,
  { labelKey: LocaleKey; fallback: string; cls: string; dot: string }
> = {
  completed: { labelKey: "status.indexed", fallback: "Indexed", cls: "text-success", dot: "#16a34a" },
  processing: { labelKey: "status.processing", fallback: "Processing", cls: "text-muted", dot: "#a8a29e" },
  finalizing: { labelKey: "status.processing", fallback: "Processing", cls: "text-muted", dot: "#a8a29e" },
  pending: { labelKey: "status.pending", fallback: "Pending", cls: "text-muted", dot: "#a8a29e" },
  failed: { labelKey: "status.failed", fallback: "Failed", cls: "text-error", dot: "#dc2626" },
  cancelled: { labelKey: "status.cancelled", fallback: "Cancelled", cls: "text-muted", dot: "#a8a29e" },
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

type MainTab = "docs-wiki" | "graph";

export function KbDetail({ kbId }: { kbId: string }) {
  const { t } = useT();
  const { isOwner } = useTenantRole();
  const [activeTab, setActiveTab] = useState<MainTab>("docs-wiki");
  const [kb, setKb] = useState<KnowledgeBaseRow | null>(null);
  const [docs, setDocs] = useState<KnowledgeDoc[] | null>(null);
  const [openDoc, setOpenDoc] = useState<KnowledgeDoc | null>(null);
  const [selectedWikiSlug, setSelectedWikiSlug] = useState<string | null>(null);
  const [q, setQ] = useState("");
  const [wikiQ, setWikiQ] = useState("");
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [error, setError] = useState("");

  const reloadDocs = () => {
    listKnowledgeFiles(kbId, { page: 1, page_size: 100 })
      .then((res) => {
        const data = res.data;
        setDocs(Array.isArray(data) ? data : (data?.items ?? []));
      })
      .catch(() => setDocs([]));
  };

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
      {/* Header */}
      <div className="shrink-0 px-10 pt-7 pb-4">
        <div className="caption mb-4 flex items-center gap-2 text-muted">
          <Link href="/platform/knowledge-bases" className="hover:text-ink">
            Knowledge bases
          </Link>
          <span>/</span>
          <span className="text-ink">{kb?.name ?? kbId}</span>
        </div>

        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h1 className="display-lg">{kb?.name ?? "Knowledge base"}</h1>
            <p className="body-sm mt-1.5 max-w-[560px] text-body">{kb?.description ?? ""}</p>
            {error && <p className="caption mt-2 text-error">{error}</p>}
            <div className="caption mt-2.5 flex items-center gap-4 text-muted">
              <span>{kb?.knowledge_count ?? kb?.document_count ?? docs?.length ?? "—"} documents</span>
              {kb?.updated_at && <span className="whitespace-nowrap">Updated {fmtShortDate(kb.updated_at)}</span>}
            </div>
          </div>
          <div className="flex gap-3">
            {isOwner && (
              <>
                <button className="btn btn-outline" onClick={() => setSettingsOpen(true)}>
                  <IconSettings className="h-4 w-4" /> Settings
                </button>
                <button className="btn btn-outline" onClick={() => setUploadOpen(true)}>
                  <IconPlus className="h-4 w-4" /> Upload files
                </button>
              </>
            )}
            <Link
              href={`/platform/knowledge-bases/${kbId}/creatChat`}
              className="btn btn-primary"
            >
              <IconChat className="h-4 w-4" /> Chat
            </Link>
          </div>
        </div>

        {/* Page Main Navigation Tabs */}
        <div className="mt-5 flex items-center gap-1 rounded-full bg-surface-strong p-1 w-fit">
          <button
            onClick={() => setActiveTab("docs-wiki")}
            className={`flex items-center gap-2 rounded-full px-4 py-1.5 text-[13px] font-medium transition-colors ${
              activeTab === "docs-wiki"
                ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.08)]"
                : "text-muted hover:text-ink"
            }`}
          >
            <IconDoc className="h-4 w-4" />
            <span>Tài liệu & Wiki</span>
            {docs !== null && (
              <span className="rounded-full bg-surface-strong px-2 py-0.5 text-[11px] font-medium text-muted">
                {docs.length}
              </span>
            )}
          </button>

          <button
            onClick={() => setActiveTab("graph")}
            className={`flex items-center gap-2 rounded-full px-4 py-1.5 text-[13px] font-medium transition-colors ${
              activeTab === "graph"
                ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.08)]"
                : "text-muted hover:text-ink"
            }`}
          >
            <IconGraph className="h-4 w-4" />
            <span>Knowledge graph</span>
          </button>
        </div>
      </div>

      {/* Main Content Area */}
      <div className="flex min-h-0 flex-1 px-10 pb-6">
        {activeTab === "docs-wiki" ? (
          /* TAB 1: Split view — Left Wiki (smaller width), Right Document Cards */
          <div className="flex min-h-0 flex-1 flex-col gap-6 lg:flex-row">
            {/* Left: Wiki Section (increased width) */}
            <section className="card flex min-h-0 flex-col overflow-hidden w-full lg:w-[440px] xl:w-[480px] shrink-0">
              {/* Wiki Search & Header */}
              <div className="flex shrink-0 items-center justify-between border-b border-hairline px-4 py-2.5">
                <div className="flex items-center gap-2 font-medium text-[13.5px] text-ink">
                  <IconDoc className="h-4 w-4 text-muted" />
                  <span>Wiki</span>
                </div>
                <span className="text-[11.5px] text-muted-soft">Mục lục tri thức</span>
              </div>
              <div className="caption flex items-center gap-2 border-b border-hairline px-4 py-2">
                <IconSearch className="h-3.5 w-3.5 text-muted-soft shrink-0" />
                <input
                  className="w-full bg-transparent text-[12.5px] outline-none placeholder:text-muted-soft"
                  placeholder="Search wiki pages…"
                  value={wikiQ}
                  onChange={(e) => setWikiQ(e.target.value)}
                />
              </div>
              {/* Wiki Browser Tree */}
              <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
                <WikiBrowser kbId={kbId} q={wikiQ} />
              </div>
            </section>

            {/* Right: Document Cards Grid */}
            <section className="flex min-h-0 flex-1 flex-col min-w-0">
              {/* Document Search and Action Bar */}
              <div className="mb-3.5 flex shrink-0 items-center justify-between gap-4">
                <div className="relative w-full max-w-[320px]">
                  <IconSearch className="pointer-events-none absolute left-3.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-soft" />
                  <input
                    className="input h-9 pl-9 text-[13px]"
                    placeholder="Search documents…"
                    value={q}
                    onChange={(e) => setQ(e.target.value)}
                  />
                </div>
                <div className="flex items-center gap-3">
                  <span className="caption text-muted">{filtered.length} files</span>
                  {isOwner && (
                    <button
                      onClick={() => setUploadOpen(true)}
                      className="btn btn-outline btn-sm h-8"
                    >
                      <IconPlus className="h-3.5 w-3.5" /> Upload
                    </button>
                  )}
                </div>
              </div>

              {/* Cards Grid: 5 cards per row on desktop */}
              <div className="grid min-h-0 flex-1 auto-rows-min grid-cols-1 gap-2.5 overflow-y-auto pr-1 sm:grid-cols-2 md:grid-cols-3 xl:grid-cols-5">
                {filtered.map((d) => {
                  const st = statusStyle(d.parse_status ?? d.status);
                  return (
                    <button
                      key={d.id}
                      onClick={() => setOpenDoc(d)}
                      className="card card-hover flex flex-col justify-between p-3 text-left transition-all hover:border-ink/20"
                    >
                      <div>
                        <div className="flex items-start gap-2">
                          <span
                            className="w-[26px] shrink-0 mt-0.5"
                            dangerouslySetInnerHTML={{
                              __html: renderFileIconSvg(
                                d.file_name ?? d.title ?? "",
                                d.file_type,
                                d.profile?.doc_type,
                              ),
                            }}
                          />
                          <div className="min-w-0 flex-1">
                            <div className="truncate text-[13px] font-medium text-ink" title={docName(d)}>
                              {docName(d)}
                            </div>
                            <div className="caption mt-0.5 flex flex-wrap items-center gap-1 text-muted">
                              {d.profile?.doc_type && (
                                <span className="badge-pill text-[10px] py-0 px-1">{d.profile.doc_type}</span>
                              )}
                              <span className="text-[11px]">{docExt(d)}</span>
                              {d.file_size ? (
                                <span className="text-muted-soft text-[11px]">· {fmtBytes(d.file_size)}</span>
                              ) : null}
                            </div>
                          </div>
                        </div>

                        {/* Shortened summary / gist: 1 line */}
                        {(d.description || d.profile?.gist) && (
                          <p
                            className="mt-1.5 text-[11.5px] line-clamp-1 text-muted leading-normal"
                            title={d.description || d.profile?.gist}
                          >
                            {d.description || d.profile?.gist}
                          </p>
                        )}

                        {/* Topics */}
                        {d.profile?.topics && d.profile.topics.length > 0 && (
                          <div className="mt-2 flex flex-wrap gap-1">
                            {d.profile.topics.slice(0, 2).map((topic) => (
                              <span
                                key={topic}
                                className="inline-block max-w-[85px] truncate rounded-full bg-surface-strong px-1.5 py-0.2 text-[9.5px] text-muted"
                                title={topic}
                              >
                                {topic}
                              </span>
                            ))}
                            {d.profile.topics.length > 2 && (
                              <span className="text-[9.5px] text-muted-soft self-center">
                                +{d.profile.topics.length - 2}
                              </span>
                            )}
                          </div>
                        )}
                      </div>

                      {/* Bottom Status & Date */}
                      <div className="mt-2.5 flex items-center justify-between border-t border-hairline pt-2 text-[11px] text-muted">
                        <span className={`flex items-center gap-1.5 font-medium ${st.cls}`}>
                          <span
                            className="inline-block h-1.5 w-1.5 rounded-full"
                            style={{ background: st.dot }}
                          />
                          {t(st.labelKey) || st.fallback}
                        </span>
                        <span className="whitespace-nowrap text-muted-soft text-[10.5px]">
                          {fmtShortDate(d.updated_at)}
                        </span>
                      </div>
                    </button>
                  );
                })}
              </div>

              {docs !== null && docs.length === 0 && (
                <div className="mt-8 flex flex-col items-center justify-center rounded-xl border border-dashed border-hairline p-8 text-center">
                  <IconDoc className="mb-2 h-8 w-8 text-muted-soft" />
                  <p className="body-sm font-medium text-ink">Chưa có tài liệu nào</p>
                  <p className="caption mt-1 text-muted">Tải lên tài liệu để hệ thống bắt đầu xử lý và xây dựng wiki.</p>
                  {isOwner && (
                    <button onClick={() => setUploadOpen(true)} className="btn btn-primary btn-sm mt-4">
                      <IconPlus className="h-3.5 w-3.5" /> Tải lên tài liệu
                    </button>
                  )}
                </div>
              )}

              {docs !== null && docs.length > 0 && filtered.length === 0 && (
                <div className="mt-8 flex flex-col items-center justify-center text-center">
                  <p className="body-md text-muted">Không tìm thấy tài liệu phù hợp.</p>
                  <p className="caption mt-1 text-muted-soft">Thử tìm kiếm với từ khóa khác.</p>
                </div>
              )}
            </section>

          </div>
        ) : (
          /* TAB 2: Full Knowledge Graph */
          <section className="card flex min-h-0 flex-1 flex-col overflow-hidden">
            <KnowledgeGraph
              kbId={kbId}
              onSelectSlug={(slug) => setSelectedWikiSlug(slug)}
            />
          </section>
        )}
      </div>

      {/* Slide-in Panels & Modals */}
      <DocPanel doc={openDoc} onClose={() => setOpenDoc(null)} />

      {selectedWikiSlug && (
        <WikiPageView
          kbId={kbId}
          slug={selectedWikiSlug}
          title={selectedWikiSlug}
          onClose={() => setSelectedWikiSlug(null)}
          onNavigate={(nextSlug) => setSelectedWikiSlug(nextSlug)}
        />
      )}

      <KbSettingsModal
        kbId={kbId}
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
        onSaved={() => {
          getKnowledgeBase(kbId).then((row) => setKb(row ?? null)).catch(() => {});
        }}
      />
      <UploadModal
        kbId={kbId}
        open={uploadOpen}
        onClose={() => setUploadOpen(false)}
        onUploaded={() => reloadDocs()}
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
