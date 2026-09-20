"use client";

import { useEffect, useRef, useState } from "react";
import { useSearchParams } from "next/navigation";
import { listMessages } from "@/lib/api/chat";
import { stopSession } from "@/lib/api/chat";
import { streamChat, type StreamChunk } from "@/lib/api/stream";
import { ChatProvider, useChatContext } from "@/lib/chat-context";
import { useAuth } from "@/lib/auth";
import { Composer, type ComposerSend } from "@/components/composer";
import { useAttachments } from "@/components/use-attachments";
import { Markdown } from "@/components/markdown";
import { IconDoc } from "@/components/icons";

type UiMessage = {
  id: string;
  role: "user" | "assistant";
  content: string;
  streaming?: boolean;
  assistantMessageId?: string;
  references?: Array<{ knowledge_title?: string; knowledge_id?: string; chunk_id?: string }>;
};

function fileToDataUri(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
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
  const attachments = useAttachments(id === "new" ? undefined : id);

  // History: GET /api/v1/messages/:sessionId/load?limit=30 — same shape as Vue getMessageList.
  useEffect(() => {
    if (id === "new") return;
    let alive = true;
    listMessages(id, 30)
      .then((res) => {
        if (!alive || !res.data?.length) return;
        setMessages(
          res.data.map((m, i) => ({
            id: m.id ?? `h${i}`,
            role: m.role === "user" ? "user" : "assistant",
            content: m.content ?? "",
          })),
        );
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
    const asstId = `a${Date.now()}`;

    // Web images upload as temporary documents (VLM reads them in background);
    // inline base64 is the per-image fallback when the upload fails.
    const imageAttachmentIds: string[] = [];
    for (const file of s.imageFiles) {
      try {
        const { uploadTemporaryAttachment } = await import("@/lib/api/attachments");
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
        const { uploadTemporaryAttachment } = await import("@/lib/api/attachments");
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
    // Mirror useChatStreamHandler.processStreamChunk (normal RAG path):
    // only response_type=answer appends to content; references attach to the message.
    const applyChunk = (c: StreamChunk) => {
      const kind = c.response_type ?? c.type;
      if (kind === "session_title" || kind === "agent_query") {
        if (c.assistant_message_id) setAssistantMessageId(c.assistant_message_id);
        return;
      }
      if (kind === "references" && c.knowledge_references?.length) {
        const refs = c.knowledge_references;
        setMessages((m) => m.map((msg) => (msg.id === asstId ? { ...msg, references: refs } : msg)));
        return;
      }
      if (kind === "error") {
        setError(c.content || "Stream failed");
        return;
      }
      if (c.assistant_message_id) setAssistantMessageId(c.assistant_message_id);
      acc += c.content ?? "";
      const snapshot = acc;
      setMessages((m) => m.map((msg) => (msg.id === asstId ? { ...msg, content: snapshot, assistantMessageId: c.assistant_message_id ?? msg.assistantMessageId } : msg)));
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
    streamChat({
      sessionId: id === "new" ? "new" : id,
      query: t,
      agentEnabled: Boolean(agentId),
      agentId,
      agentSourceTenantId: ctx.settings.selectedAgentSourceTenantId ?? undefined,
      knowledgeBaseIds: [...kbIdSet],
      knowledgeIds: [...fileIdSet],
      tagIds: [...tagIds],
      mcpServiceIds: [...mcpIds],
      skillNames: [...skillSet],
      mentionedItems: s.mentionedItems,
      webSearchEnabled: ctx.settings.webSearchEnabled,
      localBrowserEnabled: ctx.settings.localBrowserEnabled,
      summaryModelId: canPickModel ? s.modelId || undefined : undefined,
      attachmentIds: attachmentIds.length > 0 ? attachmentIds : undefined,
      images: inlineImages,
      signal: ctrl.signal,
      onChunk: applyChunk,
    })
      .catch((e: unknown) => {
        if (e instanceof DOMException && e.name === "AbortError") return;
        setError(e instanceof Error ? e.message : "Stream failed");
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

  const title = id === "new" ? (initialQ ?? "New chat") : `Chat ${id.slice(0, 8)}`;

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
                  {m.content ? (
                    <Markdown text={m.content} streaming={m.streaming} />
                  ) : (
                    <p className="text-[15px] leading-relaxed text-body">{m.streaming ? "…" : ""}</p>
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
