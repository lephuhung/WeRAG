"use client";

import { useEffect, useRef, useState } from "react";
import { apiGet } from "@/lib/api-client";
import { useT } from "@/lib/i18n";
import { useChatContext, type MentionRequestItem, type MentionType } from "@/lib/chat-context";
import { IconDoc } from "@/components/icons";
import { FileTypeIcon } from "@/components/files/file-type-icon";

/* Ports MentionSelector.vue + the @-menu half of Input-field.vue:
 * - button opens a grouped picker (KB / file / tag / MCP / skill)
 * - typing @ in the textarea opens the same picker filtered by keyword
 * - files come from GET /api/v1/knowledge/search (searchKnowledge),
 *   tags from GET /api/v1/knowledge-bases/:id/tags (listKnowledgeTags)
 */

export type MentionResolved = MentionRequestItem;

export type PickerItem = MentionRequestItem & { description?: string; count?: number; file_type?: string };

function useMentionData(open: boolean, keyword: string, documents: MentionRequestItem[]) {
  const ctx = useChatContext();
  const [items, setItems] = useState<PickerItem[]>([]);
  const [loading, setLoading] = useState(false);
  const seq = useRef(0);

  useEffect(() => {
    if (!open) return;
    const my = ++seq.current;
    setLoading(true);
    const q = keyword.trim().toLowerCase();
    const match = (name: string) => !q || name.toLowerCase().includes(q);
    const run = async () => {
      const docItems: PickerItem[] = documents
        .filter((d) => match(d.name))
        .map((d) => ({ ...d, file_type: d.file_type || "docx" }));
      const kbItems: PickerItem[] = ctx.knowledgeBases
        .filter((k) => match(k.name))
        .slice(0, 20)
        .map((k) => ({
          id: k.id,
          name: k.name,
          type: "kb",
          kb_type: k.type === "faq" ? "faq" : "document",
          count: k.type === "faq" ? (k.chunk_count ?? 0) : (k.knowledge_count ?? 0),
          description: k.org_name ? `Shared · ${k.org_name}` : undefined,
        }));
      // open documents and KBs are local: list them before the searches return
      setItems([...docItems, ...kbItems]);
      let fileItems: PickerItem[] = [];
      try {
        const params = new URLSearchParams({
          offset: "0",
          limit: "20",
          ...(q ? { keyword: q } : { recent: "true" }),
        });
        const res = await apiGet<{
          success: boolean;
          data?: Array<{ id: string; title?: string; file_name?: string; description?: string; file_type?: string; knowledge_base_id?: string; knowledge_base_name?: string }>;
        }>(`/api/v1/knowledge/search?${params.toString()}`);
        fileItems = (res.data ?? [])
          .filter((f) => match(f.title || f.file_name || ""))
          .slice(0, 20)
          .map((f) => ({
            id: f.id,
            name: f.title || f.file_name || f.id,
            type: "file",
            file_type: f.file_type,
            kb_id: f.knowledge_base_id,
            kb_name: f.knowledge_base_name,
            description: f.description || f.knowledge_base_name,
          }));
      } catch {
        /* file search offline — KB/tag/MCP/skill still work */
      }
      let tagItems: PickerItem[] = [];
      try {
        const tagLists = await Promise.all(
          ctx.knowledgeBases.slice(0, 8).map(async (kb) => {
            const params = new URLSearchParams({ page: "1", page_size: "20", ...(q ? { keyword: q } : {}) });
            const res = await apiGet<{
              success: boolean;
              data?: { data?: Array<{ id: string; name: string }> } | Array<{ id: string; name: string }>;
            }>(`/api/v1/knowledge-bases/${kb.id}/tags?${params.toString()}`).catch(() => null);
            const payload = res?.data;
            const list = Array.isArray(payload) ? payload : (payload?.data ?? []);
            return list
              .filter((t) => match(t.name))
              .slice(0, 5)
              .map((t) => ({ id: t.id, name: t.name, type: "tag" as const, kb_id: kb.id, kb_name: kb.name, description: kb.name }));
          }),
        );
        tagItems = tagLists.flat();
      } catch {
        /* tags offline */
      }
      const mcpItems: PickerItem[] = ctx.mcpServices
        .filter((m) => match(m.name))
        .slice(0, 10)
        .map((m) => ({ id: m.id, name: m.name, type: "mcp", description: m.description }));
      const skillItems: PickerItem[] = ctx.skills
        .filter((s) => match(s.name))
        .slice(0, 10)
        .map((s) => ({ id: s.name, name: s.name, type: "skill", skill_name: s.name, description: s.description }));
      if (seq.current !== my) return;
      setItems([...docItems, ...kbItems, ...fileItems, ...tagItems, ...mcpItems, ...skillItems]);
      setLoading(false);
    };
    void run();
  }, [open, keyword, ctx.knowledgeBases, ctx.mcpServices, ctx.skills, documents]);

  return { items, loading };
}

const GROUP_LABEL: Record<MentionType, string> = {
  document: "",
  kb: "Knowledge bases",
  file: "Files",
  tag: "Tags",
  mcp: "MCP services",
  skill: "Skills",
};

const NO_DOCUMENTS: MentionRequestItem[] = [];

export function MentionPicker({
  open,
  keyword,
  activeIndex,
  onActiveIndex,
  onSelect,
  onClose,
  onItems,
  documents = NO_DOCUMENTS,
}: {
  open: boolean;
  keyword: string;
  activeIndex: number;
  onActiveIndex: (i: number) => void;
  onSelect: (item: MentionResolved) => void;
  onClose: () => void;
  /** The listed items in display order, for keyboard selection by index. */
  onItems?: (items: PickerItem[]) => void;
  /** The session's open documents (document assistant), listed first. */
  documents?: MentionRequestItem[];
}) {
  const { t } = useT();
  const [search, setSearch] = useState("");
  const searchRef = useRef<HTMLInputElement>(null);
  const { items, loading } = useMentionData(open, keyword || search, documents);
  const listRef = useRef<HTMLDivElement>(null);

  // Focus stays in the composer: typing after @ filters, arrows move and
  // Enter picks (the composer handles the keys). The search box is for a
  // click.
  useEffect(() => {
    if (open) setSearch("");
  }, [open]);

  useEffect(() => {
    onItems?.(items);
  }, [items, onItems]);

  useEffect(() => {
    listRef.current?.querySelector(`[data-idx="${activeIndex}"]`)?.scrollIntoView({ block: "nearest" });
  }, [activeIndex]);

  if (!open) return null;
  const groups: MentionType[] = ["document", "kb", "file", "tag", "mcp", "skill"];
  return (
    <>
      <div className="fixed inset-0 z-40" onClick={onClose} />
      <div className="card absolute bottom-full left-0 z-50 mb-2 max-h-[320px] w-[360px] max-w-[calc(100vw-2.5rem)] overflow-hidden shadow-[0_4px_16px_rgba(0,0,0,0.08)]">
        <div className="shrink-0 border-b border-hairline p-1.5">
          <div className="relative">
            <svg viewBox="0 0 24 24" className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
              <circle cx="11" cy="11" r="7" />
              <path d="M21 21l-4.3-4.3" />
            </svg>
            <input
              ref={searchRef}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              onKeyDown={(e) => {
                e.stopPropagation();
                if (e.key === "Escape") onClose();
                else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                  e.preventDefault();
                  const last = Math.max(0, items.length - 1);
                  onActiveIndex(e.key === "ArrowDown" ? Math.min(last, activeIndex + 1) : Math.max(0, activeIndex - 1));
                } else if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                  e.preventDefault();
                  const item = items[activeIndex];
                  if (item) onSelect(item);
                }
              }}
              placeholder="Tìm KB, file, tag…"
              className="w-full rounded-[8px] bg-surface-strong py-1.5 pl-8 pr-3 text-[13px] text-ink placeholder:text-muted-soft focus:outline-none focus:ring-1 focus:ring-primary/40"
            />
          </div>
        </div>
        <div ref={listRef} className="max-h-[280px] overflow-y-auto p-1.5">
          {loading && items.length === 0 && <div className="caption px-3 py-3 text-muted">Loading…</div>}
          {!loading && items.length === 0 && <div className="caption px-3 py-3 text-muted">No matches — sign in to load KBs</div>}
          {groups.map((g) => {
            const rows = items.filter((i) => i.type === g);
            if (rows.length === 0) return null;
            return (
              <div key={g}>
                <div className="caption-uppercase px-3 pb-1 pt-2 text-muted-soft">
                  {g === "document" ? t("docws.mentionGroup") : GROUP_LABEL[g]}
                </div>
                {rows.map((item) => {
                  const idx = items.indexOf(item);
                  return (
                    <button
                      key={`${item.type}:${item.id}:${item.kb_id ?? ""}`}
                      data-idx={idx}
                      onClick={() => onSelect(item)}
                      onMouseEnter={() => onActiveIndex(idx)}
                      className={`flex w-full items-center gap-3 rounded-[8px] px-3 py-2 text-left transition-colors ${
                        idx === activeIndex ? "bg-surface-strong" : "hover:bg-surface-strong"
                      }`}
                    >
                      {item.type === "file" || item.type === "document" ? (
                        <FileTypeIcon name={item.name} fileType={item.file_type} />
                      ) : item.type === "kb" ? (
                        <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary">
                          <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
                            <ellipse cx="12" cy="5.5" rx="8" ry="3" />
                            <path d="M4 5.5v6c0 1.66 3.58 3 8 3s8-1.34 8-3v-6" />
                            <path d="M4 11.5v6c0 1.66 3.58 3 8 3s8-1.34 8-3v-6" />
                          </svg>
                        </span>
                      ) : (
                        <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-surface-strong text-ink">
                          <IconDoc className="h-3.5 w-3.5" />
                        </span>
                      )}
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-[14px] font-medium text-ink">{item.name}</span>
                        {(item.description || typeof item.count === "number") && (
                          <span className="caption block truncate text-muted">
                            {item.description ?? ""}
                            {item.description && typeof item.count === "number" ? " · " : ""}
                            {typeof item.count === "number" ? `${item.count} docs` : ""}
                          </span>
                        )}
                      </span>
                    </button>
                  );
                })}
              </div>
            );
          })}
        </div>
      </div>
    </>
  );
}

export function MentionChips({ onRemove }: { onRemove?: (item: MentionRequestItem) => void }) {
  const { mentionItems, removeMention } = useChatContext();
  if (mentionItems.length === 0) return null;
  return (
    <div className="mb-2 flex flex-wrap gap-1.5">
      {mentionItems.map((item) => (
        <span
          key={`${item.type}:${item.id}:${item.kb_id ?? ""}`}
          className="flex items-center gap-1.5 rounded-full border border-hairline-strong bg-surface-strong px-2.5 py-1 text-[13px] font-medium text-ink"
          title={item.kb_name || item.name}
        >
          <span className="caption font-semibold uppercase text-muted">@{item.type}</span>
          <span className="max-w-[180px] truncate">{item.name}</span>
          <button
            className="text-muted hover:text-ink"
            onClick={() => (onRemove ? onRemove(item) : removeMention(item))}
            aria-label="Remove"
          >
            ×
          </button>
        </span>
      ))}
    </div>
  );
}
