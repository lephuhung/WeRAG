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
import { IconDoc } from "@/components/icons";
import { ThinkingDisplay } from "@/components/chat/thinking-display";
import { ToolResultCard, type ToolEventItem } from "@/components/chat/tool-result-card";

type UiMessage = {
  id: string;
  role: "user" | "assistant";
  content: string;
  thinking?: string;
  toolEvents?: ToolEventItem[];
  streaming?: boolean;
  isError?: boolean;
  assistantMessageId?: string;
  references?: Array<{ knowledge_title?: string; knowledge_id?: string; chunk_id?: string }>;
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

  // History and session details:
  // Mirrors Vue loadSessionAndHydrate: fetch session details to populate title
  // and hydrate input state (agent, model, KBs) from last_request_state.
  useEffect(() => {
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

    listMessages(id, 30)
      .then((res) => {
        if (!alive) return;
        const rows = res.data ?? [];
        setMessages(
          rows.map((m, i) => {
            const rawContent = m.content ?? "";
            const parsed =
              m.role === "assistant"
                ? parseThinkAndContent(rawContent)
                : { thinking: undefined, content: rawContent };
            return {
              id: m.id ?? `h${i}`,
              role: (m.role === "user" ? "user" : "assistant") as "user" | "assistant",
              content: parsed.content,
              thinking: parsed.thinking,
              assistantMessageId: m.role === "assistant" ? (m.id ?? undefined) : undefined,
            };
          }),
        );
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

    // No backend session exists for /platform/chat/new — the backend 404s on
    // unknown session ids, so create one first and re-enter through the ?q=
    // auto-send flow (same path the creatChat page uses).
    if (id === "new") {
      try {
        const res = await createSession({});
        const sid = res.data?.id;
        if (!sid) throw new Error("Failed to create session");
        router.replace(`/platform/chat/${sid}?q=${encodeURIComponent(t)}`);
      } catch (e) {
        setError(e instanceof Error ? e.message : "Failed to create session");
        setBusy(false);
      }
      return;
    }

    const asstId = `a${Date.now()}`;

    // Web images upload as temporary documents (VLM reads them in background);
    // inline base64 is the per-image fallback when the upload fails.
    const imageAttachmentIds: string[] = [];
    for (const file of s.imageFiles) {
      try {
        const up = await uploadTemporaryAttachment(
          id,
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
              id,
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
        if (c.assistant_message_id) setAssistantMessageId(c.assistant_message_id);
        return;
      }
      if (kind === "agent_query") {
        if (c.assistant_message_id) setAssistantMessageId(c.assistant_message_id);
        return;
      }
      if (kind === "references" && c.knowledge_references?.length) {
        const refs = c.knowledge_references;
        setMessages((m) => m.map((msg) => (msg.id === asstId ? { ...msg, references: refs } : msg)));
        return;
      }
      if (kind === "error") {
        const msg = c.content || "Stream failed";
        setError(msg);
        setMessages((m) =>
          m.map((msg) =>
            msg.id === asstId
              ? {
                  ...msg,
                  content: msg.content || `⚠️ ${msg}`,
                  isError: true,
                  streaming: false,
                }
              : msg,
          ),
        );
        return;
      }
      if (c.assistant_message_id) setAssistantMessageId(c.assistant_message_id);

      // 1. Thinking / Reasoning chunks
      const thoughtText = c.reasoning_content ?? c.thought ?? (c.data?.thought as string | undefined);
      if (kind === "thinking" || thoughtText) {
        thinkingAcc += thoughtText ?? c.content ?? "";
        const snapThinking = thinkingAcc;
        setMessages((m) =>
          m.map((msg) =>
            msg.id === asstId
              ? { ...msg, thinking: snapThinking, assistantMessageId: c.assistant_message_id ?? msg.assistantMessageId }
              : msg,
          ),
        );
        return;
      }

      // 2. Tool call events
      if (kind === "tool_call") {
        const callId = c.tool_call_id || c.id || `tool-${Date.now()}-${Math.random()}`;
        const existingIdx = toolEventsList.findIndex((t) => t.id === callId);
        const item: ToolEventItem = {
          id: callId,
          tool_name: c.tool_name,
          title: (c.data?.title as string) || c.tool_name,
          input: c.tool_data ?? c.tool_input,
          status: "pending",
        };
        if (existingIdx >= 0) {
          toolEventsList[existingIdx] = { ...toolEventsList[existingIdx], ...item };
        } else {
          toolEventsList.push(item);
        }
        const snapTools = [...toolEventsList];
        setMessages((m) => m.map((msg) => (msg.id === asstId ? { ...msg, toolEvents: snapTools } : msg)));
        return;
      }

      // 3. Tool result events
      if (kind === "tool_result") {
        const callId = c.tool_call_id || c.id;
        const existingIdx = toolEventsList.findIndex((t) => (callId ? t.id === callId : t.tool_name === c.tool_name));
        const status = c.success === false ? "error" : "success";
        const output = c.tool_output ?? c.content;
        if (existingIdx >= 0) {
          toolEventsList[existingIdx] = {
            ...toolEventsList[existingIdx],
            status,
            output,
            error: c.success === false ? (c.content || "Tool error") : undefined,
          };
        } else {
          toolEventsList.push({
            id: callId || `tool-${Date.now()}`,
            tool_name: c.tool_name,
            status,
            output,
            error: c.success === false ? (c.content || "Tool error") : undefined,
          });
        }
        const snapTools = [...toolEventsList];
        setMessages((m) => m.map((msg) => (msg.id === asstId ? { ...msg, toolEvents: snapTools } : msg)));
        return;
      }

      // 4. Regular answer content
      acc += c.content ?? "";
      const parsed = parseThinkAndContent(acc, thinkingAcc);
      setMessages((m) =>
        m.map((msg) =>
          msg.id === asstId
            ? {
                ...msg,
                content: parsed.content,
                thinking: parsed.thinking,
                assistantMessageId: c.assistant_message_id ?? msg.assistantMessageId,
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
    const agentId =
      ctx.settings.selectedAgentId && ctx.settings.selectedAgentId !== "builtin-quick-answer"
        ? ctx.settings.selectedAgentId
        : undefined;
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
      sessionId: id === "new" ? "new" : id,
      query: t,
      agentEnabled: Boolean(agentId),
      agentId,
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
        const msg = e instanceof Error ? e.message : "Stream failed";
        setError(msg);
        setMessages((m) =>
          m.map((msg) =>
            msg.id === asstId
              ? {
                  ...msg,
                  content: msg.content || `⚠️ ${msg}`,
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
      });
  };

  // create-chat ?q=… auto-send, like creatChat.vue navigateToSession(firstQuery).
  useEffect(() => {
    if (initialQ && !sentInitial.current) {
      sentInitial.current = true;
      void send({ query: initialQ, modelId: ctx.settings.selectedChatModelId, mentionedItems: [], imageFiles: [], attachments: [] });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialQ]);

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
        <div className="mx-auto max-w-[760px] px-6 py-10">
          {messages.map((m) =>
            m.role === "user" ? (
              <div key={m.id} className="mb-6 flex justify-end">
                <div className="max-w-[75%] rounded-[16px] bg-primary px-5 py-3 text-[15px] leading-relaxed text-on-primary">
                  {m.content}
                </div>
              </div>
            ) : (
              <div key={m.id} className="mb-6 flex gap-4">
                <div className="display-sm mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-surface-strong text-[14px]">
                  W
                </div>
                <div className="max-w-[85%] min-w-0 flex-1 pt-1.5">
                  {m.thinking && (
                    <ThinkingDisplay content={m.thinking} streaming={m.streaming && !m.content} />
                  )}
                  {m.toolEvents && m.toolEvents.length > 0 && (
                    <div className="mb-3 space-y-1">
                      {m.toolEvents.map((t) => (
                        <ToolResultCard key={t.id} event={t} />
                      ))}
                    </div>
                  )}
                  {m.content ? (
                    <div className={m.isError ? "rounded-lg border border-red-500/20 bg-red-500/5 p-3 text-red-500 dark:text-red-400" : ""}>
                      <Markdown text={m.content} streaming={m.streaming} />
                    </div>
                  ) : (
                    <p className="text-[15px] leading-relaxed text-body">{m.streaming && !m.thinking ? "…" : ""}</p>
                  )}
                  {m.references && m.references.length > 0 && (
                    <div className="mt-3 flex flex-wrap gap-2">
                      {m.references.map((r, i) => (
                        <span
                          key={r.chunk_id ?? `${r.knowledge_id}-${i}`}
                          className="flex items-center gap-2 rounded-full border border-hairline bg-surface-card px-3 py-1.5 text-[13px] text-body"
                        >
                          <span className="flex h-4 w-4 items-center justify-center rounded-full bg-surface-strong text-[10px] font-semibold text-ink">
                            {i + 1}
                          </span>
                          <IconDoc className="h-3.5 w-3.5 text-muted" />
                          {r.knowledge_title ?? r.knowledge_id ?? "source"}
                        </span>
                      ))}
                    </div>
                  )}
                  {!m.streaming && m.assistantMessageId && (
                    <div className="caption mt-2 flex items-center gap-3 text-muted-soft">
                      <button
                        className="transition-colors hover:text-ink"
                        onClick={() => void navigator.clipboard.writeText(m.content)}
                      >
                        Copy
                      </button>
                      {id !== "new" && (
                        <button
                          className="transition-colors hover:text-ink"
                          onClick={() => {
                            void forkSession(id, { message_id: m.assistantMessageId! })
                              .then((res) => {
                                const newId = (res as { data?: { session?: { id?: string }; id?: string } }).data;
                                const sessionId = newId && typeof newId === "object" && "session" in newId
                                  ? newId.session?.id
                                  : (newId as { id?: string } | undefined)?.id;
                                if (sessionId ?? sessionId) void 0;
                                if (sessionId && router) void router.push(`/platform/chat/${sessionId}`);
                              })
                              .catch(() => undefined);
                          }}
                        >
                          Fork
                        </button>
                      )}
                    </div>
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
