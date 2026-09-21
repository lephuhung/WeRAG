"use client";

import { useEffect, useRef, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { useCommandPalette } from "./command-palette-context";
import {
  IconAgent,
  IconArtifact,
  IconBook,
  IconChat,
  IconClose,
  IconDoc,
  IconOrg,
  IconPulse,
  IconSearch,
  IconSettings,
} from "@/components/icons";
import {
  knowledgeSemanticSearch,
  listKnowledgeBases,
  type KnowledgeBaseRow,
  type KnowledgeSemanticSearchResult,
} from "@/lib/api/knowledge";
import { searchMessages, type MessageSearchGroupItem } from "@/lib/api/chat";
import { listAgents, type AgentRow } from "@/lib/api/agents";
import { useAuth } from "@/lib/auth";

interface CommandItem {
  id: string;
  category: "quick" | "kbs" | "agents" | "chunks" | "messages" | "ask";
  title: string;
  subtitle?: string;
  badge?: string;
  icon?: React.ReactNode;
  onSelect: () => void;
}

export function GlobalCommandPalette() {
  const { isOpen, close } = useCommandPalette();
  const router = useRouter();
  const { user } = useAuth();
  const isSystemAdmin = user?.is_system_admin === true;

  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState(0);

  const [allKbs, setAllKbs] = useState<KnowledgeBaseRow[]>([]);
  const [allAgents, setAllAgents] = useState<AgentRow[]>([]);

  const [chunks, setChunks] = useState<KnowledgeSemanticSearchResult[]>([]);
  const [messages, setMessages] = useState<MessageSearchGroupItem[]>([]);

  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const [, startTransition] = useTransition();

  // Load KBs and Agents once when opened
  useEffect(() => {
    if (isOpen) {
      setQuery("");
      setSelectedIndex(0);
      setChunks([]);
      setMessages([]);
      setTimeout(() => inputRef.current?.focus(), 50);

      listKnowledgeBases()
        .then((res) => setAllKbs(res ?? []))
        .catch(() => {});
      if (isSystemAdmin) {
        listAgents()
          .then((res) => setAllAgents(res.data ?? []))
          .catch(() => {});
      } else {
        setAllAgents([]);
      }
    }
  }, [isOpen, isSystemAdmin]);

  // Debounced search when query changes
  useEffect(() => {
    const q = query.trim();
    if (!q) {
      setChunks([]);
      setMessages([]);
      setLoading(false);
      return;
    }

    setLoading(true);
    const timer = setTimeout(async () => {
      try {
        const [chunkRes, msgRes] = await Promise.allSettled([
          knowledgeSemanticSearch({ query: q }),
          searchMessages({ query: q, mode: "hybrid", limit: 6 }),
        ]);

        if (chunkRes.status === "fulfilled" && chunkRes.value?.data) {
          setChunks(chunkRes.value.data.slice(0, 5));
        } else {
          setChunks([]);
        }

        if (msgRes.status === "fulfilled" && (msgRes.value as any)?.data?.items) {
          setMessages(((msgRes.value as any).data.items as MessageSearchGroupItem[]).slice(0, 5));
        } else {
          setMessages([]);
        }
      } finally {
        setLoading(false);
      }
    }, 300);

    return () => clearTimeout(timer);
  }, [query]);

  // Build items list
  const q = query.trim().toLowerCase();

  const quickActions: CommandItem[] = [
    {
      id: "quick-new-chat",
      category: "quick",
      title: "New chat",
      subtitle: "Start a fresh AI conversation",
      icon: <IconChat className="h-4 w-4" />,
      onSelect: () => {
        close();
        router.push("/platform/creatChat");
      },
    },
    {
      id: "quick-kbs",
      category: "quick",
      title: "Knowledge bases",
      subtitle: "Manage documents and datasets",
      icon: <IconBook className="h-4 w-4" />,
      onSelect: () => {
        close();
        router.push("/platform/knowledge-bases");
      },
    },
    ...(isSystemAdmin
      ? [
          {
            id: "quick-agents",
            category: "quick" as const,
            title: "Agents",
            subtitle: "Configure autonomous AI assistants",
            icon: <IconAgent className="h-4 w-4" />,
            onSelect: () => {
              close();
              router.push("/platform/system/agents");
            },
          },
        ]
      : []),
    {
      id: "quick-artifacts",
      category: "quick",
      title: "Artifacts",
      subtitle: "Browse generated files and exports",
      icon: <IconArtifact className="h-4 w-4" />,
      onSelect: () => {
        close();
        router.push("/platform/artifacts");
      },
    },
    {
      id: "quick-orgs",
      category: "quick",
      title: "Organizations",
      subtitle: "Workspaces and shared teams",
      icon: <IconOrg className="h-4 w-4" />,
      onSelect: () => {
        close();
        router.push("/platform/organizations");
      },
    },
    {
      id: "quick-settings",
      category: "quick",
      title: "Settings",
      subtitle: "Preferences, models, and extensions",
      icon: <IconSettings className="h-4 w-4" />,
      onSelect: () => {
        close();
        router.push("/platform/system/workspace");
      },
    },
    {
      id: "quick-logs",
      category: "quick",
      title: "System audit logs",
      subtitle: "View platform security events",
      icon: <IconPulse className="h-4 w-4" />,
      onSelect: () => {
        close();
        router.push("/platform/system/logs");
      },
    },
  ];

  const filteredQuick = q
    ? quickActions.filter((a) => a.title.toLowerCase().includes(q) || a.subtitle?.toLowerCase().includes(q))
    : quickActions;

  const matchedKbs: CommandItem[] = q
    ? allKbs
        .filter((k) => k.name.toLowerCase().includes(q))
        .slice(0, 4)
        .map((k) => ({
          id: `kb-${k.id}`,
          category: "kbs",
          title: k.name,
          subtitle: `${k.doc_count ?? 0} docs`,
          badge: "Knowledge base",
          icon: <IconBook className="h-4 w-4" />,
          onSelect: () => {
            close();
            router.push(`/platform/knowledge-bases/${k.id}`);
          },
        }))
    : [];

  const matchedAgents: CommandItem[] = isSystemAdmin && q
    ? allAgents
        .filter((a) => a.name.toLowerCase().includes(q) || a.description?.toLowerCase().includes(q))
        .slice(0, 4)
        .map((a) => ({
          id: `agent-${a.id}`,
          category: "agents",
          title: a.name,
          subtitle: a.description || "Custom AI agent",
          badge: "Agent",
          icon: <IconAgent className="h-4 w-4" />,
          onSelect: () => {
            close();
            router.push(`/platform/system/agents`);
          },
        }))
    : [];

  const chunkItems: CommandItem[] = chunks.map((c, i) => ({
    id: `chunk-${c.id ?? c.chunk_id ?? i}`,
    category: "chunks",
    title: c.knowledge_title || c.knowledge_filename || "Knowledge chunk",
    subtitle: (c.matched_content || c.content).slice(0, 120),
    badge: c.match_type === "vector" ? "Semantic" : "Keyword",
    icon: <IconDoc className="h-4 w-4" />,
    onSelect: () => {
      close();
      if (c.knowledge_base_id) {
        router.push(`/platform/knowledge-bases/${c.knowledge_base_id}`);
      }
    },
  }));

  const messageItems: CommandItem[] = messages.map((m) => ({
    id: `msg-${m.request_id}`,
    category: "messages",
    title: m.session_title || "Chat session",
    subtitle: (m.query_content || m.answer_content || "").slice(0, 120),
    badge: "Chat",
    icon: <IconChat className="h-4 w-4" />,
    onSelect: () => {
      close();
      router.push(`/platform/chat/${m.session_id}`);
    },
  }));

  const askAiItem: CommandItem[] = q
    ? [
        {
          id: "ask-ai",
          category: "ask",
          title: `Ask AI: "${q}"`,
          subtitle: "Launch a new chat answering this query",
          badge: "AI Query",
          icon: <IconChat className="h-4 w-4 text-accent" />,
          onSelect: () => {
            close();
            router.push(`/platform/creatChat?prompt=${encodeURIComponent(q)}`);
          },
        },
      ]
    : [];

  // Grouped items
  const items: CommandItem[] = [
    ...askAiItem,
    ...matchedKbs,
    ...matchedAgents,
    ...chunkItems,
    ...messageItems,
    ...filteredQuick,
  ];

  // Keyboard navigation
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelectedIndex((prev) => (prev + 1) % Math.max(1, items.length));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelectedIndex((prev) => (prev - 1 + items.length) % Math.max(1, items.length));
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (items[selectedIndex]) {
        items[selectedIndex].onSelect();
      }
    }
  };

  // Scroll active item into view
  useEffect(() => {
    const el = listRef.current?.querySelector(`[data-index="${selectedIndex}"]`);
    if (el) {
      el.scrollIntoView({ block: "nearest" });
    }
  }, [selectedIndex]);

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/40 p-4 pt-[12vh] backdrop-blur-sm animate-in fade-in duration-150">
      <div
        className="fixed inset-0"
        onClick={close}
        aria-hidden="true"
      />
      <div
        className="relative flex w-full max-w-[620px] flex-col overflow-hidden rounded-[20px] border border-hairline-strong bg-surface-card shadow-[0_24px_48px_rgba(0,0,0,0.18)]"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={handleKeyDown}
      >
        {/* Search header */}
        <div className="flex items-center gap-3 border-b border-hairline px-4 py-3.5">
          <IconSearch className="h-5 w-5 shrink-0 text-muted" />
          <input
            ref={inputRef}
            className="flex-1 bg-transparent text-[15px] font-medium text-ink outline-none placeholder:text-muted"
            placeholder="Type a command or search knowledge..."
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setSelectedIndex(0);
            }}
          />
          {loading && (
            <span className="h-4 w-4 animate-spin rounded-full border-2 border-hairline-strong border-t-ink" />
          )}
          {query ? (
            <button
              onClick={() => {
                setQuery("");
                inputRef.current?.focus();
              }}
              className="rounded-full p-1 text-muted hover:bg-surface-strong hover:text-ink"
            >
              <IconClose className="h-4 w-4" />
            </button>
          ) : (
            <kbd className="caption rounded border border-hairline px-1.5 py-0.5 text-[11px] text-muted">
              ESC
            </kbd>
          )}
        </div>

        {/* Results list */}
        <div
          ref={listRef}
          className="max-h-[380px] min-h-[140px] overflow-y-auto p-2"
        >
          {items.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-10 text-center text-muted">
              <IconSearch className="mb-2 h-6 w-6 text-muted-soft" />
              <p className="text-[14px]">No results found for &ldquo;{query}&rdquo;</p>
              <button
                onClick={() => {
                  close();
                  router.push(`/platform/creatChat?prompt=${encodeURIComponent(query)}`);
                }}
                className="btn btn-outline btn-sm mt-3"
              >
                Ask AI about this
              </button>
            </div>
          ) : (
            items.map((item, idx) => {
              const isSelected = idx === selectedIndex;
              return (
                <div
                  key={item.id}
                  data-index={idx}
                  onClick={item.onSelect}
                  onMouseEnter={() => setSelectedIndex(idx)}
                  className={`flex cursor-pointer items-center gap-3 rounded-[12px] px-3.5 py-2.5 transition-colors ${
                    isSelected
                      ? "bg-surface-strong text-ink shadow-[0_1px_2px_rgba(0,0,0,0.04)]"
                      : "text-ink hover:bg-surface-strong"
                  }`}
                >
                  <div
                    className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-lg ${
                      isSelected
                        ? "bg-surface-card text-ink"
                        : "bg-surface text-muted"
                    }`}
                  >
                    {item.icon}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-[14px] font-medium leading-tight">
                        {item.title}
                      </span>
                      {item.badge && (
                        <span className="badge-pill shrink-0 text-[10px] uppercase">
                          {item.badge}
                        </span>
                      )}
                    </div>
                    {item.subtitle && (
                      <p className="caption mt-0.5 truncate text-muted">
                        {item.subtitle}
                      </p>
                    )}
                  </div>
                  {isSelected && (
                    <kbd className="caption shrink-0 rounded border border-hairline px-1.5 py-0.5 text-[11px] text-muted">
                      ↵
                    </kbd>
                  )}
                </div>
              );
            })
          )}
        </div>

        {/* Footer shortcuts */}
        <div className="flex items-center justify-between border-t border-hairline bg-surface/50 px-4 py-2.5 text-[12px] text-muted">
          <div className="flex items-center gap-3">
            <span className="flex items-center gap-1">
              <kbd className="rounded border border-hairline px-1 text-[10px]">↑</kbd>
              <kbd className="rounded border border-hairline px-1 text-[10px]">↓</kbd>
              <span>navigate</span>
            </span>
            <span className="flex items-center gap-1">
              <kbd className="rounded border border-hairline px-1 text-[10px]">↵</kbd>
              <span>select</span>
            </span>
            <span className="flex items-center gap-1">
              <kbd className="rounded border border-hairline px-1 text-[10px]">esc</kbd>
              <span>close</span>
            </span>
          </div>
          <span className="caption text-muted-soft">WeRAG Command Palette</span>
        </div>
      </div>
    </div>
  );
}
