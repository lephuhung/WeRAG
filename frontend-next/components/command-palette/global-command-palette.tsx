"use client";

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useDialogFocus } from "@/components/use-dialog-focus";
import { useRouter } from "next/navigation";
import { useCommandPalette } from "./command-palette-context";
import {
  IconAgent,
  IconArtifact,
  IconBook,
  IconChat,
  IconClose,
  IconDoc,
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
import { useAuth, useTenantRole } from "@/lib/auth";
import { canSeeSection } from "@/components/settings/nav-config";
import { SYSTEM_TABS } from "@/components/system/system-nav";
import { adminSectionHref } from "@/components/admin/admin-routes";
import { useT, type LocaleKey } from "@/lib/i18n";

interface CommandItem {
  id: string;
  category: "quick" | "kbs" | "agents" | "chunks" | "messages" | "ask" | "settings";
  title: string;
  subtitle?: string;
  badge?: string;
  icon?: React.ReactNode;
  onSelect: () => void;
}

const LISTBOX_ID = "cmdk-listbox";
const optionId = (index: number) => `cmdk-option-${index}`;

export function GlobalCommandPalette() {
  const { isOpen, close } = useCommandPalette();
  const router = useRouter();
  const { t } = useT();
  const { user, memberships, selectedTenantId, tenant } = useAuth();
  const isSystemAdmin = user?.is_system_admin === true;
  // message search needs an organization; a guest's would only answer 409
  const { isGuest } = useTenantRole();
  const currentRole =
    memberships.find(
      (m) => String(m.tenant_id) === String(selectedTenantId ?? tenant?.id ?? ""),
    )?.role ?? "";

  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState(0);

  const [allKbs, setAllKbs] = useState<KnowledgeBaseRow[]>([]);
  const [allAgents, setAllAgents] = useState<AgentRow[]>([]);

  const [chunks, setChunks] = useState<KnowledgeSemanticSearchResult[]>([]);
  const [messages, setMessages] = useState<MessageSearchGroupItem[]>([]);

  const dialogRef = useRef<HTMLDivElement>(null);
  useDialogFocus({ open: isOpen, containerRef: dialogRef, onClose: close });
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

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
          isGuest ? Promise.resolve(null) : searchMessages({ query: q, mode: "hybrid", limit: 6 }),
        ]);

        if (chunkRes.status === "fulfilled" && chunkRes.value?.data) {
          setChunks(chunkRes.value.data.slice(0, 5));
        } else {
          setChunks([]);
        }

        if (msgRes.status === "fulfilled") {
          // The search endpoint answers with an unknown shape: narrow it.
          const body = msgRes.value;
          const data = body && typeof body === "object" && "data" in body ? body.data : undefined;
          const rawItems =
            data && typeof data === "object" && "items" in data && Array.isArray(data.items)
              ? data.items
              : [];
          setMessages(
            rawItems
              .filter((x): x is MessageSearchGroupItem => !!x && typeof x === "object")
              .slice(0, 5),
          );
        } else {
          setMessages([]);
        }
      } finally {
        setLoading(false);
      }
    }, 300);

    return () => clearTimeout(timer);
  }, [query, isGuest]);

  // Build items list
  const q = query.trim().toLowerCase();

  const quickActions: CommandItem[] = [
    {
      id: "quick-new-chat",
      category: "quick",
      title: t("cmdk.newChat"),
      subtitle: t("cmdk.newChatSub"),
      icon: <IconChat className="h-4 w-4" />,
      onSelect: () => {
        close();
        router.push("/platform/creatChat");
      },
    },
    {
      id: "quick-kbs",
      category: "quick",
      title: t("cmdk.knowledgeBases"),
      subtitle: t("cmdk.knowledgeBasesSub"),
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
            title: t("cmdk.agents"),
            subtitle: t("cmdk.agentsSub"),
            icon: <IconAgent className="h-4 w-4" />,
            onSelect: () => {
              close();
              router.push("/platform/system/extensions/agents");
            },
          },
        ]
      : []),
    {
      id: "quick-artifacts",
      category: "quick",
      title: t("cmdk.artifacts"),
      subtitle: t("cmdk.artifactsSub"),
      icon: <IconArtifact className="h-4 w-4" />,
      onSelect: () => {
        close();
        router.push("/platform/artifacts");
      },
    },
    {
      id: "quick-settings",
      category: "quick",
      title: t("cmdk.settings"),
      subtitle: t("cmdk.settingsSub"),
      icon: <IconSettings className="h-4 w-4" />,
      onSelect: () => {
        close();
        router.push("/platform/system/workspace");
      },
    },
    {
      id: "quick-logs",
      category: "quick",
      title: t("cmdk.auditLogs"),
      subtitle: t("cmdk.auditLogsSub"),
      icon: <IconPulse className="h-4 w-4" />,
      onSelect: () => {
        close();
        router.push(adminSectionHref("logs"));
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
          subtitle: t("cmdk.kbDocs", { n: k.doc_count ?? 0 }),
          badge: t("cmdk.badgeKb"),
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
          subtitle: a.description || t("cmdk.customAgent"),
          badge: t("cmdk.badgeAgent"),
          icon: <IconAgent className="h-4 w-4" />,
          onSelect: () => {
            close();
            router.push(`/platform/system/extensions/agents`);
          },
        }))
    : [];

  const chunkItems: CommandItem[] = chunks.map((c, i) => ({
    id: `chunk-${c.id ?? c.chunk_id ?? i}`,
    category: "chunks",
    title: c.knowledge_title || c.knowledge_filename || t("cmdk.chunk"),
    subtitle: (c.matched_content || c.content).slice(0, 120),
    badge: c.match_type === "vector" ? t("cmdk.badgeSemantic") : t("cmdk.badgeKeyword"),
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
    title: m.session_title || t("cmdk.chatSession"),
    subtitle: (m.query_content || m.answer_content || "").slice(0, 120),
    badge: t("cmdk.badgeChat"),
    icon: <IconChat className="h-4 w-4" />,
    onSelect: () => {
      close();
      router.push(`/platform/chat/${m.session_id}`);
    },
  }));

  /* Every /platform/system sub-page is a real route — index them all so any
   * settings surface is one ⌘K search away, gated by the same role rules as
   * the nav. */
  const settingItems: CommandItem[] = q
    ? SYSTEM_TABS.flatMap((tab) =>
        tab.groups.flatMap((g) =>
          g.items
            .filter(
              (it) =>
                canSeeSection(
                  { key: it.key, fallbackLabel: it.label, minRole: it.minRole ?? "member" },
                  currentRole,
                  isSystemAdmin,
                ) &&
                (it.label.toLowerCase().includes(q) ||
                  tab.label.toLowerCase().includes(q)),
            )
            .map((it) => ({
              id: `setting-${tab.key}-${it.key}`,
              category: "settings" as const,
              title: it.labelKey ? t(it.labelKey as LocaleKey) : it.label,
              subtitle: t("cmdk.settingItem", {
                section: tab.labelKey ? t(tab.labelKey as LocaleKey) : tab.label,
              }),
              icon: <IconSettings className="h-4 w-4" />,
              onSelect: () => {
                close();
                router.push(it.href);
              },
            })),
        ),
      ).slice(0, 6)
    : [];

  const askAiItem: CommandItem[] = q
    ? [
        {
          id: "ask-ai",
          category: "ask",
          title: t("cmdk.askAi", { q: query.trim() }),
          subtitle: t("cmdk.askAiSub"),
          badge: t("cmdk.badgeAi"),
          icon: <IconChat className="h-4 w-4 text-primary" />,
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
    ...settingItems,
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

  const activeOptionId = items.length > 0 ? optionId(selectedIndex) : undefined;

  return createPortal(
    <div className="fixed inset-0 z-[90] flex items-start justify-center bg-scrim/40 p-4 pt-[12vh] backdrop-blur-sm animate-in fade-in duration-150">
      <div
        className="fixed inset-0"
        onClick={close}
        aria-hidden="true"
      />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-label={t("cmdk.dialogLabel")}
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
            placeholder={t("cmdk.placeholder")}
            value={query}
            role="combobox"
            aria-expanded
            aria-controls={LISTBOX_ID}
            aria-activedescendant={activeOptionId}
            aria-autocomplete="list"
            aria-label={t("cmdk.placeholder")}
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
              type="button"
              onClick={() => {
                setQuery("");
                inputRef.current?.focus();
              }}
              aria-label={t("cmdk.clear")}
              title={t("cmdk.clear")}
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
          id={LISTBOX_ID}
          role="listbox"
          aria-label={t("cmdk.results")}
          className="max-h-[380px] min-h-[140px] overflow-y-auto p-2"
        >
          {items.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-10 text-center text-muted">
              <IconSearch className="mb-2 h-6 w-6 text-muted-soft" />
              <p className="text-[14px]">{t("cmdk.noResults", { q: query.trim() })}</p>
              <button
                onClick={() => {
                  close();
                  router.push(`/platform/creatChat?prompt=${encodeURIComponent(query)}`);
                }}
                className="btn btn-outline btn-sm mt-3"
              >
                {t("cmdk.askAiAbout")}
              </button>
            </div>
          ) : (
            items.map((item, idx) => {
              const isSelected = idx === selectedIndex;
              return (
                <div
                  key={item.id}
                  id={optionId(idx)}
                  data-index={idx}
                  role="option"
                  aria-selected={isSelected}
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
                        : "bg-surface-card text-muted"
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

        {/* How many results the search found — announced to screen readers. */}
        <p className="sr-only" role="status" aria-live="polite">
          {t("cmdk.resultsCount", { n: items.length })}
        </p>

        {/* Footer shortcuts */}
        <div className="flex items-center justify-between border-t border-hairline bg-surface-card/50 px-4 py-2.5 text-[12px] text-muted">
          <div className="flex items-center gap-3">
            <span className="flex items-center gap-1">
              <kbd className="rounded border border-hairline px-1 text-[10px]">↑</kbd>
              <kbd className="rounded border border-hairline px-1 text-[10px]">↓</kbd>
              <span>{t("cmdk.navigate")}</span>
            </span>
            <span className="flex items-center gap-1">
              <kbd className="rounded border border-hairline px-1 text-[10px]">↵</kbd>
              <span>{t("cmdk.select")}</span>
            </span>
            <span className="flex items-center gap-1">
              <kbd className="rounded border border-hairline px-1 text-[10px]">esc</kbd>
              <span>{t("cmdk.close")}</span>
            </span>
          </div>
          <span className="caption text-muted-soft">{t("cmdk.brand")}</span>
        </div>
      </div>
    </div>, document.body,
  );
}
