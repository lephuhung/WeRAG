"use client";

import { useEffect, useRef, useState } from "react";
import { apiGet } from "@/lib/api-client";
import { useChatContext, type MentionRequestItem, type MentionType } from "@/lib/chat-context";
import { IconDoc } from "@/components/icons";

/* Ports MentionSelector.vue + the @-menu half of Input-field.vue:
 * - button opens a grouped picker (KB / file / tag / MCP / skill)
 * - typing @ in the textarea opens the same picker filtered by keyword
 * - files come from GET /api/v1/knowledge/search (searchKnowledge),
 *   tags from GET /api/v1/knowledge-bases/:id/tags (listKnowledgeTags)
 */

export type MentionResolved = MentionRequestItem;

type PickerItem = MentionRequestItem & { description?: string; count?: number };

function useMentionData(open: boolean, keyword: string) {
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
      let fileItems: PickerItem[] = [];
      try {
        const params = new URLSearchParams({
          offset: "0",
          limit: "20",
          ...(q ? { keyword: q } : { recent: "true" }),
        });
        const res = await apiGet<{
          success: boolean;
          data?: Array<{ id: string; title?: string; file_name?: string; knowledge_base_id?: string; knowledge_base_name?: string }>;
        }>(`/api/v1/knowledge/search?${params.toString()}`);
        fileItems = (res.data ?? [])
          .filter((f) => match(f.title || f.file_name || ""))
          .slice(0, 20)
          .map((f) => ({
            id: f.id,
            name: f.title || f.file_name || f.id,
            type: "file",
            kb_id: f.knowledge_base_id,
            kb_name: f.knowledge_base_name,
            description: f.knowledge_base_name,
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
      setItems([...kbItems, ...fileItems, ...tagItems, ...mcpItems, ...skillItems]);
      setLoading(false);
    };
    void run();
  }, [open, keyword, ctx.knowledgeBases, ctx.mcpServices, ctx.skills]);

  return { items, loading };
}

const GROUP_LABEL: Record<MentionType, string> = {
  kb: "Knowledge bases",
  file: "Files",
  tag: "Tags",
  mcp: "MCP services",
  skill: "Skills",
};

export function MentionPicker({
  open,
  keyword,
  activeIndex,
  onActiveIndex,
  onSelect,
  onClose,
}: {
  open: boolean;
  keyword: string;
  activeIndex: number;
  onActiveIndex: (i: number) => void;
  onSelect: (item: MentionResolved) => void;
  onClose: () => void;
}) {
  const { items, loading } = useMentionData(open, keyword);
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    listRef.current?.querySelector(`[data-idx="${activeIndex}"]`)?.scrollIntoView({ block: "nearest" });
  }, [activeIndex]);

  if (!open) return null;
  const groups: MentionType[] = ["kb", "file", "tag", "mcp", "skill"];
  return (
    <>
      <div className="fixed inset-0 z-40" onClick={onClose} />
      <div className="card absolute bottom-full left-0 z-50 mb-2 max-h-[320px] w-[360px] overflow-hidden shadow-[0_4px_16px_rgba(0,0,0,0.08)]">
        <div ref={listRef} className="max-h-[320px] overflow-y-auto p-1.5">
          {loading && items.length === 0 && <div className="caption px-3 py-3 text-muted">Loading…</div>}
          {!loading && items.length === 0 && <div className="caption px-3 py-3 text-muted">No matches — sign in to load KBs</div>}
          {groups.map((g) => {
            const rows = items.filter((i) => i.type === g);
            if (rows.length === 0) return null;
            return (
              <div key={g}>
                <div className="caption-uppercase px-3 pb-1 pt-2 text-muted-soft">{GROUP_LABEL[g]}</div>
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
                      <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-surface-strong text-ink">
                        <IconDoc className="h-3.5 w-3.5" />
                      </span>
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
