/* Ported from frontend/src/views/knowledge/wiki/WikiBrowser.vue +
 * WikiRevisionDrawer.vue (subset: browse/edit/history/parent navigation;
 * folder moves stay in the Vue app until the Next port adds them).
 * Renders the index groups returned by getWikiIndex and opens pages into a
 * slide panel with a markdown editor (70/30 split of the Vue browser, which
 * combines the tree, page canvas and history in one window).
 */
"use client";

import { useCallback, useEffect, useState } from "react";
import {
  getWikiIndex,
  getWikiPage,
  updateWikiPage,
  listWikiRevisions,
  revertWikiPage,
  type WikiIndexEntryDTO,
  type WikiIndexGroup,
  type WikiPage,
  type WikiPageRevision,
  type WikiRevisionListResponse,
} from "@/lib/api/wiki";
import { renderChatMarkdown } from "@/lib/markdown";
import { useT } from "@/lib/i18n";
import { IconDoc } from "@/components/icons";
import { SlidePanel, SlidePanelHeader } from "@/components/slide-panel";
import { Modal } from "@/components/modal";

/* Collect the group entries into one keyed map so the tree can render
 * parents/children from any group bucket. */
function flatten(groups: WikiIndexGroup[]): WikiIndexEntryDTO[] {
  return groups.flatMap((g) => g.items);
}

export function WikiBrowser({
  kbId,
  changed,
  q = "",
}: {
  kbId: string;
  changed?: number;
  q?: string;
}) {
  const { t } = useT();
  const [index, setIndex] = useState<{
    intro: string;
    groups: WikiIndexGroup[];
  } | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [openSlug, setOpenSlug] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await getWikiIndex(kbId, { limit: 50 });
      setIndex(res as { intro: string; groups: WikiIndexGroup[] });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load wiki index");
    } finally {
      setLoading(false);
    }
  }, [kbId]);

  useEffect(() => {
    void load();
  }, [load, changed]);

  const toggle = (slug: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(slug)) next.delete(slug);
      else next.add(slug);
      return next;
    });
  };

  const item = openSlug
    ? flatten(index?.groups ?? []).find((i) => i.slug === openSlug)
    : null;
  const table = flatten(index?.groups ?? []);
  const childrenOf = (slug: string) =>
    table.filter((i) => i.parent_slug === slug);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* intro */}
      {index?.intro && (
        <div
          className="chat-markdown mb-4 shrink-0 text-[14px] text-body"
          dangerouslySetInnerHTML={{ __html: renderChatMarkdown(index.intro) }}
        />
      )}
      {error && <p className="caption text-error">{error}</p>}
      {loading && <p className="caption text-muted">Loading wiki…</p>}

      {/* tree */}
      <div className="min-h-0 flex-1 overflow-y-auto pr-1">
        {(index?.groups ?? []).map((g) => {
          /* group-level search filter */
          const items = g.items.filter(
            (i) =>
              !q ||
              i.title.toLowerCase().includes(q.toLowerCase()) ||
              i.slug.toLowerCase().includes(q.toLowerCase()),
          );
          if (q && items.length === 0) return null;
          return (
            <div key={g.type} className="mb-5">
              <div className="mb-1 flex items-center gap-2 text-[15px] font-medium text-ink">
                <IconDoc className="h-4 w-4 text-muted" />
                <span className="capitalize">{g.type}</span>
                <span className="caption text-muted-soft">{g.total}</span>
              </div>
              <div className="ml-6 border-l border-hairline pl-4">
                {items.map((it) => {
                  const kids = childrenOf(it.slug);
                  const isOpen = expanded.has(it.slug);
                  return (
                    <div key={it.slug}>
                      <div className="flex items-center gap-1">
                        <button
                          onClick={() => toggle(it.slug)}
                          className={`flex h-5 w-5 shrink-0 items-center justify-center text-muted transition-transform ${
                            kids.length ? "hover:text-ink" : "invisible"
                          }`}
                          aria-label="Toggle subtree"
                        >
                          <svg
                            viewBox="0 0 12 12"
                            className={`h-3 w-3 ${isOpen ? "rotate-90" : ""}`}
                            fill="none"
                            stroke="currentColor"
                            strokeWidth="1.4"
                          >
                            <path d="M4 2l4 4-4 4" />
                          </svg>
                        </button>
                        <button
                          onClick={() => setOpenSlug(it.slug)}
                          className="truncate py-1.5 text-left text-[14px] text-body hover:text-ink"
                        >
                          {it.title}
                          {kids.length > 0 && (
                            <span className="caption ml-1.5 text-muted-soft">{kids.length}</span>
                          )}
                        </button>
                      </div>
                      {isOpen && kids.length > 0 && (
                        <div className="ml-5">
                          {kids.map((k) => (
                            <button
                              key={k.slug}
                              onClick={() => setOpenSlug(k.slug)}
                              className="block w-full truncate py-1 text-left caption text-body hover:text-ink"
                            >
                              {k.title}
                            </button>
                          ))}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          );
        })}
        {!loading && (index?.groups ?? []).length === 0 && (
          <p className="caption text-muted-soft">
            No wiki pages yet — they are generated after documents are indexed.
          </p>
        )}
      </div>

      {item && (
        <WikiPageView
          kbId={kbId}
          slug={item.slug}
          title={item.title}
          onClose={() => setOpenSlug(null)}
        />
      )}
    </div>
  );
}

export function WikiPageView({ kbId, slug, title, onClose }: {
  kbId: string;
  slug: string;
  title: string;
  onClose: () => void;
}) {
  const { t } = useT();
  const [page, setPage] = useState<WikiPage | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState(false);
  const [draftContent, setDraftContent] = useState("");
  const [draftVersion, setDraftVersion] = useState<number | null>(null);
  const [saving, setSaving] = useState(false);
  const [revisions, setRevisions] = useState<WikiPageRevision[] | null>(null);
  const [showHistory, setShowHistory] = useState(false);

  useEffect(() => {
    let alive = true;
    setPage(null);
    setRevisions(null);
    getWikiPage(kbId, slug)
      .then((res) => {
        if (!alive) return;
        if (res.data) setPage(res.data);
        else setError("Page not found");
      })
      .catch((e: unknown) => alive && setError(e instanceof Error ? e.message : "Failed to load page"));
  }, [kbId, slug]);

  const startEdit = () => {
    if (!page) return;
    setDraftContent(page.content);
    setDraftVersion(page.version);
    setEditing(true);
  };

  const save = async () => {
    if (!page || draftVersion === null) return;
    setSaving(true);
    setError("");
    try {
      const res = await updateWikiPage(kbId, slug, {
        content: draftContent,
        version: draftVersion,
      });
      if (res.data) setPage(res.data);
      setEditing(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSaving(false);
    }
  };

  const showRevList = async () => {
    setShowHistory(true);
    try {
      const res = await listWikiRevisions(kbId, slug, { limit: 50 });
      setRevisions(res.data?.revisions ?? []);
    } catch {
      setRevisions([]);
    }
  };

  const revertTo = async (version: number) => {
    try {
      await revertWikiPage(kbId, slug, version);
      setShowHistory(false);
      const res = await getWikiPage(kbId, slug);
      setPage(res.data ?? null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Revert failed");
    }
  };

  return (
    <SlidePanel open onClose={onClose} label={page?.title ?? title} width="w-[640px]">
      <SlidePanelHeader title={page?.title ?? title} onClose={onClose} />
      <div className="flex min-h-0 flex-1 flex-col gap-3 p-5">
        {error && <p className="caption text-error">{error}</p>}
        {/* meta row */}
        {page && (
          <div className="caption flex flex-wrap items-center gap-4 text-muted">
            <span className="capitalize">{page.page_type}</span>
            <span>{page.status}</span>
            <span>v{page.version}</span>
            {page.source_refs.length > 0 && <span>{page.source_refs.length} sources</span>}
            <div className="ml-auto flex gap-2">
              {!editing && (
                <>
                  <button className="btn btn-outline btn-sm" onClick={showRevList}>
                    {t("wiki.history")}
                  </button>
                  <button className="btn btn-primary btn-sm" onClick={startEdit}>
                    {t("wiki.edit")}
                  </button>
                </>
              )}
              {editing && (
                <>
                  <button className="btn btn-outline btn-sm" onClick={() => setEditing(false)} disabled={saving}>
                    {t("common.cancel")}
                  </button>
                  <button className="btn btn-primary btn-sm" onClick={() => void save()} disabled={saving}>
                    {t("common.save")}
                  </button>
                </>
              )}
            </div>
          </div>
        )}

        {/* content */}
        {loading && <p className="caption text-muted">Loading page…</p>}
        {editing ? (
          <textarea
            className="input min-h-0 flex-1 resize-none font-mono text-[13px]"
            value={draftContent}
            onChange={(e) => setDraftContent(e.target.value)}
          />
        ) : (
          <div className="min-h-0 flex-1 overflow-y-auto pr-2">
            <div
              className="chat-markdown"
              dangerouslySetInnerHTML={{ __html: renderChatMarkdown(page?.content ?? "") }}
            />
          </div>
        )}

        {/* history drawer */}
        {showHistory && (
          <Modal open title={t("wiki.history")} onClose={() => setShowHistory(false)} width="w-[560px]">
            <div className="max-h-[400px] overflow-y-auto">
              {(revisions ?? []).map((r) => (
                <div key={r.id} className="flex items-center justify-between border-b border-hairline py-2.5">
                  <div className="min-w-0">
                    <div className="caption text-ink">
                      v{r.version} · {r.edit_source || "pipeline"}
                    </div>
                    <div className="caption mt-0.5 text-muted">
                      {r.created_at ? new Date(r.created_at).toLocaleString() : ""}
                    </div>
                  </div>
                  <button className="btn btn-outline btn-sm" onClick={() => void revertTo(r.version)}>
                    {t("wiki.revertTo")}
                  </button>
                </div>
              ))}
              {revisions !== null && revisions.length === 0 && (
                <p className="caption text-muted">No revisions recorded yet.</p>
              )}
            </div>
          </Modal>
        )}
      </div>
    </SlidePanel>
  );
}


