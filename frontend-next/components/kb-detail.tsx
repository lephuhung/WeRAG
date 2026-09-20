"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import {
  getKnowledgeBase,
  listKnowledgeFiles,
  type KnowledgeBaseRow,
  type KnowledgeDoc,
} from "@/lib/api/knowledge";
import { getWikiIndex, type WikiIndexGroup } from "@/lib/api/wiki";
import { DocPanel } from "@/components/doc-panel";
import { KnowledgeGraph } from "@/components/knowledge-graph";
import { IconChat, IconDoc, IconPlus, IconSearch } from "@/components/icons";

const STATUS_STYLE: Record<string, { label: string; cls: string; dot: string }> = {
  parsed: { label: "Indexed", cls: "text-success", dot: "#16a34a" },
  indexed: { label: "Indexed", cls: "text-success", dot: "#16a34a" },
  processing: { label: "Processing", cls: "text-muted", dot: "#a8a29e" },
  parsing: { label: "Processing", cls: "text-muted", dot: "#a8a29e" },
  pending: { label: "Processing", cls: "text-muted", dot: "#a8a29e" },
  failed: { label: "Failed", cls: "text-error", dot: "#dc2626" },
};

function statusStyle(status?: string) {
  return STATUS_STYLE[status ?? ""] ?? STATUS_STYLE.processing;
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
  const [groups, setGroups] = useState<WikiIndexGroup[] | null>(null);
  const [openDoc, setOpenDoc] = useState<KnowledgeDoc | null>(null);
  const [q, setQ] = useState("");
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
    getWikiIndex(kbId)
      .then((res: unknown) => {
        if (!alive) return;
        const r = res as { groups?: WikiIndexGroup[]; data?: { groups?: WikiIndexGroup[] } };
        setGroups(r.groups ?? r.data?.groups ?? []);
      })
      .catch(() => alive && setGroups([]));
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
              <span>{kb?.document_count ?? docs?.length ?? "—"} documents</span>
              <span>{(kb?.chunk_count ?? 0).toLocaleString()} chunks</span>
              {kb?.updated_at && <span>Updated {kb.updated_at}</span>}
            </div>
          </div>
          <div className="flex gap-3">
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
      <div className="grid min-h-0 flex-1 grid-cols-1 gap-5 px-10 pb-8 lg:grid-cols-2">
        {/* left — documents as cards */}
        <section className="flex min-h-0 flex-col">
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

          <div className="grid min-h-0 flex-1 auto-rows-min grid-cols-1 gap-3 overflow-y-auto pr-1 xl:grid-cols-2">
            {filtered.map((d) => {
              const st = statusStyle(d.status);
              return (
                <button
                  key={d.id}
                  onClick={() => setOpenDoc(d)}
                  className="card card-hover p-4 text-left"
                >
                  <div className="flex items-start gap-3">
                    <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-surface-strong text-ink">
                      <IconDoc className="h-4 w-4" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-[14px] font-medium text-ink">
                        {docName(d)}
                      </div>
                      <div className="caption mt-0.5 text-muted">{docExt(d)}</div>
                    </div>
                  </div>
                  <div className="caption mt-3 flex items-center justify-between border-t border-hairline pt-3">
                    <span className={`flex items-center gap-1.5 font-medium ${st.cls}`}>
                      <span
                        className="inline-block h-1.5 w-1.5 rounded-full"
                        style={{ background: st.dot }}
                      />
                      {st.label}
                    </span>
                    <span className="text-muted">{d.updated_at ?? ""}</span>
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

        {/* right — wiki / graph switchable pane */}
        <section className="card flex min-h-0 flex-col overflow-hidden">
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
              {pane === "wiki"
                ? `${groups?.length ?? 0} sections`
                : "Entities & relations"}
            </span>
          </div>

          {pane === "wiki" ? (
            <div className="min-h-0 flex-1 overflow-y-auto p-5">
              {(groups ?? []).map((g, i) => (
                <div key={g.type} className={i > 0 ? "mt-5" : ""}>
                  <div className="mb-1 flex items-center gap-2 text-[15px] font-medium text-ink">
                    <IconDoc className="h-4 w-4 text-muted" />
                    {g.type}
                    <span className="caption text-muted-soft">{g.total}</span>
                  </div>
                  <div className="ml-6 border-l border-hairline pl-4">
                    {g.items.map((c) => (
                      <div
                        key={c.slug ?? c.title}
                        className="cursor-pointer py-1.5 text-[14px] text-body hover:text-ink"
                      >
                        {c.title}
                      </div>
                    ))}
                  </div>
                </div>
              ))}
              {groups !== null && groups.length === 0 && (
                <p className="caption text-muted-soft">
                  No wiki pages yet — they are generated after documents are indexed.
                </p>
              )}
            </div>
          ) : (
            <KnowledgeGraph kbId={kbId} />
          )}
        </section>
      </div>

      {/* document slide-in panel */}
      <DocPanel doc={openDoc} onClose={() => setOpenDoc(null)} />
    </div>
  );
}
