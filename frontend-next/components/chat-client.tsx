"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { listMessages, stopSession, forkSession, createSession, getSession, type SessionRow } from "@/lib/api/chat";
import { streamChat, continueStream, type StreamChunk } from "@/lib/api/stream";
import { uploadTemporaryAttachment } from "@/lib/api/attachments";
import { ChatProvider, useChatContext } from "@/lib/chat-context";
import { useAuth } from "@/lib/auth";
import { Composer, type ComposerSend } from "@/components/composer";
import { useAttachments } from "@/components/use-attachments";
import { FollowUpSuggestions } from "@/components/chat/follow-up-suggestions";
import { Markdown } from "@/components/markdown";
import { IconDoc, IconCopy, IconCheck, IconFork, IconRefresh, IconEdit } from "@/components/icons";
import { ThinkingDisplay } from "@/components/chat/thinking-display";
import { type ToolEventItem } from "@/components/chat/tool-result-card";
import { RagPipelineProgress } from "@/components/chat/rag-pipeline-progress";
import { ReferencesDrawer, type KnowledgeReferenceItem } from "@/components/chat/references-drawer";

type UiMessage = {
  id: string;
  role: "user" | "assistant";
  content: string;
  thinking?: string;
  toolEvents?: ToolEventItem[];
  streaming?: boolean;
  isError?: boolean;
  assistantMessageId?: string;
  references?: KnowledgeReferenceItem[];
};

function parseThinkAndContent(
  accText: string,
  explicitThinking = "",
): { thinking?: string; content: string } {
  const thinkCloseTag = "</think>";
  if (accText.includes("<think>") && accText.includes(thinkCloseTag)) {
    const index = accText.lastIndexOf(thinkCloseTag);
    const inTag = accText.substring(0, index).replace("<think>", "").trim();
    const rest = accText.substring(index + thinkCloseTag.length).trim();
    const combinedThinking = [explicitThinking, inTag].filter(Boolean).join("\n").trim();
    return { thinking: combinedThinking || undefined, content: rest };
  }
  if (accText.includes("<think>")) {
    const inTag = accText.replace("<think>", "").trim();
    const combinedThinking = [explicitThinking, inTag].filter(Boolean).join("\n").trim();
    return { thinking: combinedThinking || undefined, content: "" };
  }
  return { thinking: explicitThinking || undefined, content: accText };
}

function fileToDataUri(file: File): Promise<string> {
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = reject;
    reader.readAsDataURL(file);
  });
}

function UserMessageBubble({
  content,
  onCopy,
  onFork,
  onEdit,
}: {
  content: string;
  onCopy?: () => void;
  onFork?: () => void;
  onEdit?: () => void;
}) {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    void navigator.clipboard.writeText(content);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
    onCopy?.();
  };

  return (
    <div className="group mb-6 flex flex-col items-end">
      <div className="max-w-[80%] rounded-[16px] border border-[#cfe1fd] bg-[#edf5ff] px-4 py-2.5 text-[14px] leading-relaxed text-[#0f2d59] shadow-2xs dark:border-[#223d63] dark:bg-[#15273f] dark:text-[#dce9fe] break-words whitespace-pre-wrap">
        {content}
      </div>
      <div className="mt-1 flex items-center gap-1 pr-1 text-muted-soft opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 sm:opacity-80">
        {onEdit && (
          <button
            type="button"
            onClick={onEdit}
            className="flex h-7 w-7 items-center justify-center rounded-md border border-transparent text-muted hover:border-hairline hover:bg-surface-strong hover:text-ink transition-colors cursor-pointer"
            title="Chỉnh sửa câu hỏi"
            aria-label="Chỉnh sửa câu hỏi"
          >
            <IconEdit className="h-3.5 w-3.5" />
          </button>
        )}
        <button
          type="button"
          onClick={handleCopy}
          className="flex h-7 w-7 items-center justify-center rounded-md border border-transparent text-muted hover:border-hairline hover:bg-surface-strong hover:text-ink transition-colors cursor-pointer"
          title={copied ? "Đã sao chép" : "Sao chép câu hỏi"}
          aria-label="Sao chép câu hỏi"
        >
          {copied ? (
            <IconCheck className="h-3.5 w-3.5 text-emerald-600 dark:text-emerald-400" />
          ) : (
            <IconCopy className="h-3.5 w-3.5" />
          )}
        </button>
        {onFork && (
          <button
            type="button"
            onClick={onFork}
            className="flex h-7 w-7 items-center justify-center rounded-md border border-transparent text-muted hover:border-hairline hover:bg-surface-strong hover:text-ink transition-colors cursor-pointer"
            title="Tạo nhánh từ câu hỏi này"
            aria-label="Tạo nhánh từ câu hỏi này"
          >
            <IconFork className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
    </div>
  );
}

function BotMessageActions({
  content,
  canFork,
  onFork,
  onRegenerate,
}: {
  content: string;
  canFork?: boolean;
  onFork?: () => void;
  onRegenerate?: () => void;
}) {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    void navigator.clipboard.writeText(content);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="mt-2.5 flex items-center gap-1.5 text-muted-soft">
      <button
        type="button"
        onClick={handleCopy}
        className="flex h-7 w-7 items-center justify-center rounded-md border border-hairline/60 bg-surface-card/60 text-muted hover:border-hairline hover:bg-surface-strong hover:text-ink transition-colors cursor-pointer"
        title={copied ? "Đã sao chép" : "Sao chép câu trả lời"}
        aria-label="Sao chép câu trả lời"
      >
        {copied ? (
          <IconCheck className="h-3.5 w-3.5 text-emerald-600 dark:text-emerald-400" />
        ) : (
          <IconCopy className="h-3.5 w-3.5" />
        )}
      </button>

      {canFork && onFork && (
        <button
          type="button"
          onClick={onFork}
          className="flex h-7 w-7 items-center justify-center rounded-md border border-hairline/60 bg-surface-card/60 text-muted hover:border-hairline hover:bg-surface-strong hover:text-ink transition-colors cursor-pointer"
          title="Tạo nhánh hội thoại mới"
          aria-label="Tạo nhánh hội thoại mới"
        >
          <IconFork className="h-3.5 w-3.5" />
        </button>
      )}

      {onRegenerate && (
        <button
          type="button"
          onClick={onRegenerate}
          className="flex h-7 w-7 items-center justify-center rounded-md border border-hairline/60 bg-surface-card/60 text-muted hover:border-hairline hover:bg-surface-strong hover:text-ink transition-colors cursor-pointer"
          title="Tạo lại câu trả lời"
          aria-label="Tạo lại câu trả lời"
        >
          <IconRefresh className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  );
}

function CompactReferencesList({
  references,
  onSelectRef,
}: {
  references: KnowledgeReferenceItem[];
  onSelectRef?: (ref: KnowledgeReferenceItem, index: number) => void;
}) {
  const [showAll, setShowAll] = useState(false);
  const maxInitial = 5;
  const hasMore = references.length > maxInitial;
  const displayed = showAll ? references : references.slice(0, maxInitial);
  const remaining = references.length - maxInitial;

  return (
    <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
      {displayed.map((r, i) => (
        <button
          key={r.chunk_id ?? r.id ?? `${r.knowledge_id || "ref"}-${i}`}
          type="button"
          onClick={() => onSelectRef?.(r, i)}
          title={r.knowledge_title ?? r.knowledge_filename ?? r.knowledge_id ?? "Tài liệu"}
          className="group flex max-w-[220px] items-center gap-1.5 rounded-full border border-hairline bg-surface-card px-2.5 py-1 text-[12px] text-body transition-all hover:border-primary/40 hover:bg-surface-strong/60 hover:text-ink cursor-pointer select-none text-left"
        >
          <span className="flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-surface-strong group-hover:bg-primary/15 group-hover:text-primary text-[10px] font-semibold text-ink transition-colors">
            {i + 1}
          </span>
          <IconDoc className="h-3 w-3 shrink-0 text-muted group-hover:text-primary transition-colors" />
          <span className="truncate">{r.knowledge_title ?? r.knowledge_filename ?? r.knowledge_id ?? "Nguồn"}</span>
        </button>
      ))}

      {hasMore && (
        <button
          type="button"
          onClick={() => setShowAll((v) => !v)}
          className="flex items-center gap-1 rounded-full border border-hairline bg-surface-strong/70 px-2.5 py-1 text-[12px] font-medium text-body hover:bg-surface-strong hover:text-ink transition-colors select-none cursor-pointer"
          title={showAll ? "Thu gọn bớt nguồn" : `Xem thêm ${remaining} nguồn khác`}
        >
          <span>{showAll ? "Thu gọn" : `+${remaining}`}</span>
        </button>
      )}
    </div>
  );
}

function ChatBody({ id }: { id: string }) {
  const searchParams = useSearchParams();
  const initialQ = searchParams.get("q");
  const ctx = useChatContext();
  const { user } = useAuth();
  // Non-admin callers never send a model override; the server resolves the
  // model assigned to the selected response mode.
  const canPickModel = user?.is_system_admin === true;

  const [session, setSession] = useState<SessionRow | null>(null);
  const [sessionTitle, setSessionTitle] = useState<string | null>(null);
  const [messages, setMessages] = useState<UiMessage[]>([]);
  const [input, setInput] = useState("");
  const [images, setImages] = useState<Array<{ preview: string; file: File }>>([]);
  const imageInputRef = useRef<HTMLInputElement>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [assistantMessageId, setAssistantMessageId] = useState<string | null>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const sentInitial = useRef(false);
  const abortRef = useRef<AbortController | null>(null);
  // Pending follow-up attribution: set when a suggestion chip is clicked,
  // consumed (and cleared) by the very next send — mirrors the Vue
  // pendingSuggestionAttribution / pendingSuggestionKnowledgeBaseIds pair.
  const pendingAttribution = useRef<{ setId: string; questionId: string } | null>(null);
  const pendingKbIds = useRef<string[]>([]);
  const router = useRouter();
  const attachments = useAttachments(id === "new" ? undefined : id);
  // Continue-stream scratch refs (reset per attach attempt).
  const accRef = useRef("");
  const srcSetRef = useRef<StreamChunk["knowledge_references"] | null>(null);

  // References slide-out panel (drawer)
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [drawerRefs, setDrawerRefs] = useState<KnowledgeReferenceItem[]>([]);
  const [activeRefKey, setActiveRefKey] = useState<string | null>(null);

  const handleOpenDrawer = (refs: KnowledgeReferenceItem[], activeItem?: KnowledgeReferenceItem, index = 0) => {
    setDrawerRefs(refs);
    if (activeItem) {
      setActiveRefKey(activeItem.chunk_id || activeItem.id || `${activeItem.knowledge_id || "ref"}-${index}`);
    } else {
      setActiveRefKey(null);
    }
    setDrawerOpen(true);
  };

  // History and session details:
  // Mirrors Vue loadSessionAndHydrate: fetch session details to populate title
  // and hydrate input state (agent, model, KBs) from last_request_state.
  useEffect(() => {
    sentInitial.current = false;
    setMessages([]);
    setError(null);
    setBusy(false);
    if (id === "new") {
      setSession(null);
      setSessionTitle(null);
      return;
    }
    let alive = true;

    getSession(id)
      .then((res) => {
        if (!alive || !res.data) return;
        setSession(res.data);
        if (res.data.title) {
          setSessionTitle(res.data.title);
        }
        if (res.data.last_request_state) {
          ctx.hydrateSessionState(res.data.last_request_state);
        }
      })
      .catch((err) => {
        console.error("Failed to load session details:", err);
      });

    // If there is an initial query waiting to be auto-sent on mount,
    // skip initial listMessages to prevent race condition wiping out optimistic message.
    if (initialQ && !sentInitial.current) {
      return () => {
        alive = false;
      };
    }

    listMessages(id, 30)
      .then((res) => {
        if (!alive) return;
        const rows = res.data ?? [];
        setMessages((prev) => {
          const streamingMsgs = prev.filter((m) => m.streaming);
          const mapped: UiMessage[] = rows.map((m, i) => {
            const rawContent = m.content ?? "";
            const parsed =
              m.role === "assistant"
                ? parseThinkAndContent(rawContent)
                : { thinking: undefined, content: rawContent };
            const refs = (m as { knowledge_references?: KnowledgeReferenceItem[] }).knowledge_references;
            return {
              id: m.id ?? `h${i}`,
              role: (m.role === "user" ? "user" : "assistant") as "user" | "assistant",
              content: parsed.content,
              thinking: parsed.thinking,
              references: refs?.length ? refs : undefined,
              assistantMessageId: m.role === "assistant" ? (m.id ?? undefined) : undefined,
            };
          });
          if (streamingMsgs.length > 0) {
            const nonStreamingRows = mapped.filter(
              (m) =>
                !streamingMsgs.some(
                  (s) =>
                    s.id === m.id ||
                    (s.assistantMessageId &&
                      (s.assistantMessageId === m.assistantMessageId || s.assistantMessageId === m.id)),
                ),
            );
            return [...nonStreamingRows, ...streamingMsgs];
          }
          return mapped;
        });
        const last = rows[rows.length - 1];
        if (alive && last && last.role !== "user" && last.is_completed === false && last.id) {
          setBusy(true);
          const inflightId = last.id;
          const applyChunk = (c: StreamChunk) => {
            const kind = c.response_type ?? c.type;
            if (kind === "session_title") {
              const newTitle = c.content || c.data?.title;
              if (newTitle) {
                setSessionTitle(newTitle);
                if (typeof window !== "undefined") {
                  window.dispatchEvent(
                    new CustomEvent("weknora:session-title-updated", {
                      detail: { sessionId: id, title: newTitle },
                    }),
                  );
                }
              }
              return;
            }
            if (kind === "agent_query") return;
            if (kind === "references" && c.knowledge_references?.length) {
              const refs = c.knowledge_references;
              srcSetRef.current = refs;
              return;
            }
            accRef.current += c.content ?? "";
            const parsed = parseThinkAndContent(accRef.current);
            setMessages((m) =>
              m.map((msg) =>
                msg.assistantMessageId === inflightId
                  ? {
                      ...msg,
                      content: parsed.content,
                      thinking: parsed.thinking ?? msg.thinking,
                      references: srcSetRef.current ?? msg.references,
                    }
                  : msg,
              ),
            );
          };
          accRef.current = last.content ?? "";
          continueStream({ sessionId: id, messageId: inflightId, onChunk: applyChunk })
            .catch(() => {
              /* the turn may simply be finished server-side — history next
               * open re-reads it complete */
            })
            .finally(() => {
              if (!alive) return;
              setBusy(false);
              setMessages((m) =>
                m.map((msg) =>
                  msg.assistantMessageId === inflightId ? { ...msg, streaming: false } : msg,
                ),
              );
            });
        }
      })
      .catch(() => {
        /* keep empty — no seed fallback so failures stay visible */
      });
    return () => {
      alive = false;
    };
  }, [id]);

  useEffect(() => () => abortRef.current?.abort(), []);
  useEffect(() => () => images.forEach((i) => URL.revokeObjectURL(i.preview)), [images]);

  const addImages = (files: File[]) => {
    const allowed = ["image/jpeg", "image/png", "image/gif", "image/webp"];
    setImages((prev) => {
      const next = [...prev];
      for (const file of files) {
        if (next.length >= 5) break;
        if (!allowed.includes(file.type) || file.size > 10 * 1024 * 1024) continue;
        next.push({ file, preview: URL.createObjectURL(file) });
      }
      return next;
    });
  };

  const send = async (s: ComposerSend) => {
    const t = s.query.trim();
    if (!t || busy) return;
    setError(null);
    setBusy(true);

    let activeSessionId = id;
    if (activeSessionId === "new") {
      try {
        const res = await createSession({});
        const sid = res.data?.id;
        if (!sid) throw new Error("Failed to create session");
        activeSessionId = sid;
        if (typeof window !== "undefined") {
          window.history.replaceState(null, "", `/platform/chat/${sid}`);
          window.dispatchEvent(
            new CustomEvent("weknora:session-created", {
              detail: { sessionId: sid },
            }),
          );
        }
      } catch (e) {
        setError(e instanceof Error ? e.message : "Failed to create session");
        setBusy(false);
        return;
      }
    }

    const asstId = `a${Date.now()}`;

    // Web images upload as temporary documents (VLM reads them in background);
    // inline base64 is the per-image fallback when the upload fails.
    const imageAttachmentIds: string[] = [];
    for (const file of s.imageFiles) {
      try {
        const up = await uploadTemporaryAttachment(
          activeSessionId,
          file,
          ctx.settings.selectedAgentId || undefined,
          ctx.settings.selectedAgentSourceTenantId ?? undefined,
        );
        imageAttachmentIds.push(up.data.id);
      } catch {
        /* base64 fallback below */
      }
    }

    // Local files picked before the session existed (create-chat page) upload now.
    const localOnes = s.attachments.filter((a) => !a.documentId);
    if (localOnes.length > 0) {
      try {
        await Promise.all(
          localOnes.map(async (a) => {
            const up = await uploadTemporaryAttachment(
              activeSessionId,
              a.file,
              ctx.settings.selectedAgentId || undefined,
              ctx.settings.selectedAgentSourceTenantId ?? undefined,
            );
            attachments.setItems((prev) => prev.map((x) => (x.localId === a.localId ? { ...x, documentId: up.data.id, status: up.data.status } : x)));
          }),
        );
      } catch (e) {
        setError(e instanceof Error ? e.message : "Attachment upload failed");
        setBusy(false);
        return;
      }
    }

    setMessages((m) => [...m, { id: `u${Date.now()}`, role: "user", content: t }, { id: asstId, role: "assistant", content: "", streaming: true }]);
    setInput("");
    setImages([]);

    const ctrl = new AbortController();
    abortRef.current = ctrl;
    let acc = "";
    let thinkingAcc = "";
    let toolEventsList: ToolEventItem[] = [];

    // Mirror useChatStreamHandler.processStreamChunk:
    // handle thinking, tool execution, answer content and references.
    const applyChunk = (c: StreamChunk) => {
      const kind = c.response_type ?? c.type;
      const incomingAsstId = c.assistant_message_id;
      if (incomingAsstId) {
        setAssistantMessageId(incomingAsstId);
      }
      const matchAssistant = (msg: UiMessage) =>
        msg.id === asstId || (Boolean(incomingAsstId) && msg.assistantMessageId === incomingAsstId);

      if (kind === "session_title") {
        const newTitle = c.content || c.data?.title;
        if (newTitle) {
          setSessionTitle(newTitle);
          if (typeof window !== "undefined") {
            window.dispatchEvent(
              new CustomEvent("weknora:session-title-updated", {
                detail: { sessionId: activeSessionId, title: newTitle },
              }),
            );
          }
        }
        if (incomingAsstId) {
          setMessages((m) =>
            m.map((msg) =>
              msg.id === asstId ? { ...msg, assistantMessageId: incomingAsstId } : msg,
            ),
          );
        }
        return;
      }
      if (kind === "agent_query") {
        if (incomingAsstId) {
          setMessages((m) =>
            m.map((msg) =>
              msg.id === asstId ? { ...msg, assistantMessageId: incomingAsstId } : msg,
            ),
          );
        }
        return;
      }

      const streamRefs =
        c.knowledge_references ??
        (c.data as { references?: KnowledgeReferenceItem[] } | undefined)?.references ??
        (c.data as { knowledge_references?: KnowledgeReferenceItem[] } | undefined)?.knowledge_references;

      if ((kind === "references" || streamRefs) && streamRefs && streamRefs.length > 0) {
        setMessages((m) =>
          m.map((msg) =>
            matchAssistant(msg)
              ? { ...msg, references: streamRefs, assistantMessageId: incomingAsstId ?? msg.assistantMessageId }
              : msg,
          ),
        );
        if (kind === "references") return;
      }
      if (kind === "error") {
        const errorText = c.content || (c.data?.error as string) || "Stream failed";
        console.error("[applyChunk] Error event received:", errorText, c);
        setError(errorText);
        setMessages((m) =>
          m.map((msg) =>
            matchAssistant(msg)
              ? {
                  ...msg,
                  content: msg.content || `⚠️ ${errorText}`,
                  isError: true,
                  streaming: false,
                  assistantMessageId: incomingAsstId ?? msg.assistantMessageId,
                }
              : msg,
          ),
        );
        return;
      }

      // 1. Thinking / Reasoning chunks
      const thoughtText =
        c.reasoning_content ??
        c.thought ??
        (c.data?.thought as string | undefined) ??
        (kind === "thinking" ? c.content : undefined);
      if (kind === "thinking") {
        thinkingAcc += thoughtText ?? c.content ?? "";
        const snapThinking = thinkingAcc;
        setMessages((m) =>
          m.map((msg) =>
            matchAssistant(msg)
              ? {
                  ...msg,
                  thinking: snapThinking,
                  assistantMessageId: incomingAsstId ?? msg.assistantMessageId,
                }
              : msg,
          ),
        );
        return;
      }
      if (thoughtText) {
        thinkingAcc += thoughtText;
      }

      // 2. Tool call events
      if (kind === "tool_call") {
        const toolName = c.tool_name || (c.data?.tool_name as string) || "";
        const callId =
          c.tool_call_id ||
          (c.data?.tool_call_id as string) ||
          (c.data?.event_id as string) ||
          `tool-${toolName}-${Date.now()}`;
        const existingIdx = toolEventsList.findIndex((t) => t.id === callId);
        const item: ToolEventItem = {
          id: callId,
          tool_name: toolName,
          title: (c.data?.title as string) || toolName,
          input: c.tool_data ?? c.tool_input ?? c.data?.arguments ?? c.data,
          status: "pending",
        };
        if (existingIdx >= 0) {
          toolEventsList[existingIdx] = { ...toolEventsList[existingIdx], ...item };
        } else {
          toolEventsList.push(item);
        }
        const snapTools = [...toolEventsList];
        setMessages((m) =>
          m.map((msg) =>
            matchAssistant(msg)
              ? {
                  ...msg,
                  toolEvents: snapTools,
                  assistantMessageId: incomingAsstId ?? msg.assistantMessageId,
                }
              : msg,
          ),
        );
        return;
      }

      // 3. Tool result events
      if (kind === "tool_result") {
        const toolName = c.tool_name || (c.data?.tool_name as string) || "";
        const callId = c.tool_call_id || (c.data?.tool_call_id as string) || (c.data?.event_id as string);
        const existingIdx = toolEventsList.findIndex((t) =>
          callId ? t.id === callId : t.tool_name === toolName,
        );
        const success = c.success !== false && c.data?.success !== false;
        const output = c.tool_output ?? c.content ?? c.data;
        if (existingIdx >= 0) {
          toolEventsList[existingIdx] = {
            ...toolEventsList[existingIdx],
            status: success ? "success" : "error",
            output,
            error: !success ? (c.content || (c.data?.error as string) || "Tool error") : undefined,
          };
        } else {
          toolEventsList.push({
            id: callId || `tool-${toolName}-${Date.now()}`,
            tool_name: toolName,
            status: success ? "success" : "error",
            output,
            error: !success ? (c.content || (c.data?.error as string) || "Tool error") : undefined,
          });
        }
        const snapTools = [...toolEventsList];
        setMessages((m) =>
          m.map((msg) =>
            matchAssistant(msg)
              ? {
                  ...msg,
                  toolEvents: snapTools,
                  assistantMessageId: incomingAsstId ?? msg.assistantMessageId,
                }
              : msg,
          ),
        );
        return;
      }

      // 4. Complete event
      if (kind === "complete" || kind === "stop") {
        setMessages((m) =>
          m.map((msg) =>
            matchAssistant(msg)
              ? {
                  ...msg,
                  streaming: false,
                  assistantMessageId: incomingAsstId ?? msg.assistantMessageId,
                }
              : msg,
          ),
        );
        return;
      }

      // 5. Regular answer content
      acc += c.content ?? "";
      const parsed = parseThinkAndContent(acc, thinkingAcc);
      setMessages((m) =>
        m.map((msg) =>
          matchAssistant(msg)
            ? {
                ...msg,
                content: parsed.content,
                thinking: parsed.thinking || (thinkingAcc || undefined),
                assistantMessageId: incomingAsstId ?? msg.assistantMessageId,
              }
            : msg,
        ),
      );
    };

    // Inline image payload is the embedded/base64-fallback path only.
    let inlineImages: Array<{ data: string }> | undefined;
    if (s.imageFiles.length > imageAttachmentIds.length) {
      inlineImages = [];
      for (const file of s.imageFiles.slice(imageAttachmentIds.length)) {
        try {
          inlineImages.push({ data: await fileToDataUri(file) });
        } catch {
          /* skip unreadable */
        }
      }
      if (inlineImages.length === 0) inlineImages = undefined;
    }

    // Mirror index.vue sendMsg: sidebar KB/file ids ∪ @mentioned ids; MCP/skills
    // only on the agent pipeline; summary_model_id only from the admin picker.
    const kbIdSet = new Set(ctx.settings.selectedKnowledgeBases);
    const fileIdSet = new Set(ctx.settings.selectedFiles);
    const tagIds = new Set<string>();
    const mcpIds = new Set(ctx.settings.selectedMCPServices);
    const skillSet = new Set(ctx.settings.selectedSkills);
    for (const item of s.mentionedItems) {
      if (item.type === "kb") kbIdSet.add(item.id);
      else if (item.type === "file") fileIdSet.add(item.id);
      else if (item.type === "tag") tagIds.add(item.id);
      else if (item.type === "mcp") mcpIds.add(item.id);
      else if (item.type === "skill") skillSet.add(item.skill_name ?? item.id);
    }
    const selectedAgentId = ctx.settings.selectedAgentId || "builtin-quick-answer";
    const isAgentMode =
      selectedAgentId === "builtin-smart-reasoning" ||
      (selectedAgentId !== "builtin-quick-answer" && ctx.settings.isAgentEnabled);
    const attachmentIds = [
      ...s.attachments.map((a) => a.documentId).filter((x): x is string => Boolean(x)),
      ...imageAttachmentIds,
    ];

    attachments.clear();
    // Follow-up attribution anchors this single turn to the clicked
    // suggestion (recorded pre-click) and keeps KB-backed retrieval scoped
    // to the suggestion's KBs; model-backed suggestions widen nothing.
    const attribution = pendingAttribution.current;
    pendingAttribution.current = null;
    const kbIdsOverride = pendingKbIds.current;
    pendingKbIds.current = [];
    streamChat({
      sessionId: activeSessionId,
      query: t,
      agentEnabled: isAgentMode,
      agentId: selectedAgentId,
      agentSourceTenantId: ctx.settings.selectedAgentSourceTenantId ?? undefined,
      knowledgeBaseIds: kbIdsOverride.length > 0 ? kbIdsOverride : [...kbIdSet],
      knowledgeIds: [...fileIdSet],
      tagIds: [...tagIds],
      mcpServiceIds: [...mcpIds],
      skillNames: [...skillSet],
      mentionedItems: s.mentionedItems,
      webSearchEnabled: ctx.settings.webSearchEnabled,
      localBrowserEnabled: ctx.settings.localBrowserEnabled,
      summaryModelId: s.modelId || ctx.settings.selectedChatModelId || undefined,
      suggestionAttribution: attribution
        ? { suggestion_set_id: attribution.setId, question_id: attribution.questionId }
        : undefined,
      attachmentIds: attachmentIds.length > 0 ? attachmentIds : undefined,
      images: inlineImages,
      signal: ctrl.signal,
      onChunk: applyChunk,
    })
      .catch((e: unknown) => {
        if (e instanceof DOMException && e.name === "AbortError") return;
        const errorText = e instanceof Error ? e.message : "Stream failed";
        console.error("[streamChat] Error:", e);
        setError(errorText);
        setMessages((m) =>
          m.map((msg) =>
            msg.id === asstId
              ? {
                  ...msg,
                  content: msg.content || `⚠️ ${errorText}`,
                  isError: true,
                  streaming: false,
                }
              : msg,
          ),
        );
      })
      .finally(() => {
        setBusy(false);
        setAssistantMessageId(null);
        setMessages((m) => m.map((msg) => (msg.id === asstId ? { ...msg, streaming: false } : msg)));
        if (id === "new" && activeSessionId !== "new") {
          router.replace(`/platform/chat/${activeSessionId}`);
        }
      });
  };

  // create-chat ?q=… auto-send, like creatChat.vue navigateToSession(firstQuery).
  useEffect(() => {
    if (initialQ && !sentInitial.current) {
      sentInitial.current = true;
      if (typeof window !== "undefined") {
        window.history.replaceState(null, "", `/platform/chat/${id}`);
      }
      void send({ query: initialQ, modelId: ctx.settings.selectedChatModelId, mentionedItems: [], imageFiles: [], attachments: [] });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialQ, id]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  const stop = async () => {
    abortRef.current?.abort();
    if (id !== "new" && assistantMessageId) {
      try {
        await stopSession(id, assistantMessageId);
      } catch {
        /* abort already stopped the UI stream */
      }
    }
    setBusy(false);
  };

  const title =
    id === "new"
      ? (initialQ ?? "New chat")
      : (sessionTitle || session?.title?.trim() || "New chat");

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      <div className="hairline-b flex h-14 shrink-0 items-center px-8">
        <h1 className="truncate text-[15px] font-medium text-ink">{title}</h1>
      </div>

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-[1040px] px-6 py-8">
          {messages.map((m, index) =>
            m.role === "user" ? (
              <UserMessageBubble
                key={m.id}
                content={m.content}
                onEdit={() => setInput(m.content)}
                onFork={
                  id !== "new"
                    ? () => {
                        void forkSession(id, { message_id: m.id })
                          .then((res) => {
                            const newId = (res as { data?: { session?: { id?: string }; id?: string } }).data;
                            const sessionId =
                              newId && typeof newId === "object" && "session" in newId
                                ? newId.session?.id
                                : (newId as { id?: string } | undefined)?.id;
                            if (sessionId && router) void router.push(`/platform/chat/${sessionId}`);
                          })
                          .catch(() => undefined);
                      }
                    : undefined
                }
              />
            ) : (
              <div key={m.id} className="mb-6 flex gap-4">
                <div className="display-sm mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-surface-strong text-[14px]">
                  W
                </div>
                <div className="w-full min-w-0 flex-1 pt-1.5">
                  <RagPipelineProgress
                    events={m.toolEvents}
                    references={m.references}
                    isStreaming={m.streaming}
                    hasAnswer={Boolean(m.content)}
                    isCompleted={!m.streaming}
                    onViewReferences={() => handleOpenDrawer(m.references || [])}
                  />
                  {m.thinking && (
                    <ThinkingDisplay content={m.thinking} streaming={m.streaming && !m.content} />
                  )}
                  {m.content ? (
                    <div className={m.isError ? "rounded-lg border border-red-500/20 bg-red-500/5 p-3 text-red-500 dark:text-red-400" : ""}>
                      <Markdown text={m.content} streaming={m.streaming} />
                    </div>
                  ) : (
                    <p className="text-[14px] leading-relaxed text-body">{m.streaming && !m.thinking && (!m.toolEvents || m.toolEvents.length === 0) ? "…" : ""}</p>
                  )}
                  {m.references && m.references.length > 0 && (
                    <CompactReferencesList
                      references={m.references}
                      onSelectRef={(r, i) => handleOpenDrawer(m.references || [], r, i)}
                    />
                  )}
                  {!m.streaming && (m.assistantMessageId || m.content) && (
                    <BotMessageActions
                      content={m.content}
                      canFork={id !== "new" && Boolean(m.assistantMessageId)}
                      onFork={() => {
                        if (!m.assistantMessageId) return;
                        void forkSession(id, { message_id: m.assistantMessageId })
                          .then((res) => {
                            const newId = (res as { data?: { session?: { id?: string }; id?: string } }).data;
                            const sessionId =
                              newId && typeof newId === "object" && "session" in newId
                                ? newId.session?.id
                                : (newId as { id?: string } | undefined)?.id;
                            if (sessionId && router) void router.push(`/platform/chat/${sessionId}`);
                          })
                          .catch(() => undefined);
                      }}
                      onRegenerate={
                        !busy
                          ? () => {
                              const prevUser = messages
                                .slice(0, index)
                                .reverse()
                                .find((msg) => msg.role === "user");
                              if (prevUser) {
                                void send({
                                  query: prevUser.content,
                                  attachments: [],
                                  imageFiles: [],
                                  mentionedItems: [],
                                  modelId: "",
                                });
                              }
                            }
                          : undefined
                      }
                    />
                  )}
                  {id !== "new" && (
                    <FollowUpSuggestions
                      sessionId={id}
                      messageId={m.assistantMessageId ?? null}
                      enabled={!m.streaming}
                      onAsk={(text, attribution, kbIds) => {
                        pendingAttribution.current = attribution;
                        pendingKbIds.current = kbIds;
                        setInput(text);
                        void send({
                          query: text,
                          attachments: [],
                          imageFiles: [],
                          mentionedItems: [],
                          modelId: "",
                        });
                      }}
                    />
                  )}
                </div>
              </div>
            ),
          )}
          {error && <p className="body-sm mb-4 text-error">{error}</p>}
          <div ref={bottomRef} />
        </div>
      </div>

      <div className="shrink-0 px-6 pb-6 pt-2">
        <div className="mx-auto max-w-[760px]">
          <input
            ref={attachments.inputRef}
            type="file"
            multiple
            className="hidden"
            onChange={(e) => {
              if (e.target.files) attachments.addFiles(Array.from(e.target.files));
              e.target.value = "";
            }}
          />
          <input
            ref={imageInputRef}
            type="file"
            accept="image/jpeg,image/png,image/gif,image/webp"
            multiple
            className="hidden"
            onChange={(e) => {
              if (e.target.files) addImages(Array.from(e.target.files));
              e.target.value = "";
            }}
          />
          <Composer
            sessionId={id === "new" ? undefined : id}
            value={input}
            onChange={setInput}
            onSend={(s) => void send(s)}
            onStop={() => void stop()}
            isReplying={busy}
            attachments={attachments.items}
            images={images}
            onRemoveAttachment={attachments.remove}
            onRemoveImage={(i) =>
              setImages((prev) => {
                URL.revokeObjectURL(prev[i].preview);
                return prev.filter((_, x) => x !== i);
              })
            }
            onPickFiles={() => attachments.trigger()}
            onPickImages={() => imageInputRef.current?.click()}
          />
          <p className="caption mt-3 text-center text-muted-soft">
            Answers are grounded in your knowledge bases — verify important details.
          </p>
        </div>
      </div>

      <ReferencesDrawer
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        references={drawerRefs}
        activeKey={activeRefKey}
      />
    </div>
  );
}

export function ChatClient({ id }: { id: string }) {
  return (
    <ChatProvider>
      <ChatBody id={id} />
    </ChatProvider>
  );
}
