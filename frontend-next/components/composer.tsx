"use client";

import { useEffect, useRef, useState } from "react";
import { IconDoc, IconGlobe, IconImage, IconPaperclip, IconSend } from "@/components/icons";
import { useChatContext, type MentionRequestItem } from "@/lib/chat-context";
import type { QuestionOrigin } from "@/lib/question-origin";
import { MentionChips, MentionPicker } from "@/components/mention-picker";
import { AgentModeButton, AgentSelector, useAgentModelSync } from "@/components/agent-selector";
import { formatFileSize, type PendingAttachment } from "@/components/use-attachments";

/* Ports Input-field.vue's composer: textarea + @mention picker + image/file
 * attachments + agent-mode switch + websearch toggle + model picker + send/stop.
 * Emits the same send-msg tuple the Vue chat view consumes:
 * (query, modelId, mentionedItems, imageFiles, attachmentFiles).
 */

export type ComposerSend = {
  query: string;
  modelId: string;
  mentionedItems: MentionRequestItem[];
  imageFiles: File[];
  attachments: PendingAttachment[];
  // Retrieval hint from a picked suggested question (creatChat.vue
  // questionOriginFromSuggestion); plain typed text leaves it undefined.
  questionOrigin?: QuestionOrigin;
};

export function Composer({
  sessionId,
  value,
  onChange,
  onSend,
  onStop,
  isReplying,
  canStop,
  attachments,
  images,
  onRemoveAttachment,
  onRemoveImage,
  onPickFiles,
  onPickImages,
  autoFocus = false,
}: {
  sessionId?: string;
  value: string;
  onChange: (v: string) => void;
  onSend: (s: ComposerSend) => void;
  onStop?: () => void;
  isReplying?: boolean;
  canStop?: boolean;
  attachments: PendingAttachment[];
  images: Array<{ preview: string; file: File }>;
  onRemoveAttachment: (localId: string) => void;
  onRemoveImage: (index: number) => void;
  onPickFiles: () => void;
  onPickImages: () => void;
  autoFocus?: boolean;
  placeholder?: string;
}) {
  const ctx = useChatContext();
  const { settings, mentionItems, webSearchReady, toggleWebSearch, selectedAgent } = ctx;
  useAgentModelSync();

  const [mentionOpen, setMentionOpen] = useState(false);
  const [mentionKeyword, setMentionKeyword] = useState("");
  const [mentionIndex, setMentionIndex] = useState(0);
  const [mentionAnchor, setMentionAnchor] = useState(0);
  const [mentionItemsCount, setMentionItemsCount] = useState(0);
  const [agentOpen, setAgentOpen] = useState(false);
  const [attachOpen, setAttachOpen] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (autoFocus) textareaRef.current?.focus();
  }, [autoFocus]);

  const closePopups = () => {
    setMentionOpen(false);
    setAgentOpen(false);
    setAttachOpen(false);
  };

  const handleInput = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const v = e.target.value;
    const cursor = e.target.selectionStart ?? v.length;
    onChange(v);
    const before = v.slice(0, cursor);
    const at = before.lastIndexOf("@");
    // Mirror onInput: @ opens the menu, space closes it, text after @ filters.
    if (at >= 0 && !before.slice(at + 1).includes(" ") && !before.slice(at + 1).includes("\n")) {
      setMentionAnchor(at);
      setMentionKeyword(before.slice(at + 1));
      setMentionOpen(true);
      setMentionIndex(0);
    } else if (mentionOpen) {
      setMentionOpen(false);
    }
  };

  const commitMention = (item: MentionRequestItem) => {
    if (item.type === "kb") ctx.addKnowledgeBase(item.id);
    else if (item.type === "file") ctx.addFile(item.id, item.kb_id, item.name);
    else if (item.type === "tag" && item.kb_id) ctx.addTag({ id: item.id, name: item.name, kbId: item.kb_id, kbName: item.kb_name });
    else if (item.type === "mcp") ctx.addMCPService(item.id);
    else if (item.type === "skill") ctx.addSkill(item.skill_name ?? item.id);
    // Strip the @query from the text like onMentionSelect.
    const el = textareaRef.current;
    const cursor = el?.selectionStart ?? value.length;
    onChange(value.slice(0, mentionAnchor) + value.slice(cursor));
    setMentionOpen(false);
    requestAnimationFrame(() => {
      if (!el) return;
      el.selectionStart = el.selectionEnd = mentionAnchor;
      el.focus();
    });
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (mentionOpen) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setMentionIndex((i) => Math.min(mentionItemsCount - 1, i + 1));
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setMentionIndex((i) => Math.max(0, i - 1));
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setMentionOpen(false);
        return;
      }
      if (e.key === "Enter") {
        e.preventDefault();
        document.querySelector(`[data-idx="${mentionIndex}"]`)?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
        return;
      }
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      submit();
    }
  };

  const imageCapable = selectedAgent?.config?.image_upload_enabled === true;
  const websearchOn = settings.webSearchEnabled;
  const placeholder =
    selectedAgent && !selectedAgent.is_builtin
      ? selectedAgent.description || `Ask ${selectedAgent.name}…`
      : mentionItems.length > 0
        ? "Ask with selected context… (Enter to send, Shift+Enter for newline)"
        : websearchOn
          ? "Ask anything — web search on…"
          : "Ask anything… (@ to quote KB, files, tags)";

  const [sendBlockMsg, setSendBlockMsg] = useState("");
  useEffect(() => setSendBlockMsg(""), [attachments, value]);

  const submit = () => {
    if (!value.trim() || isReplying) return;
    if (attachments.some((a) => a.status === "uploading")) {
      setSendBlockMsg("An attachment is still uploading — wait for it to finish or remove it.");
      return;
    }
    const failed = attachments.find((a) => a.status === "failed");
    if (failed) {
      setSendBlockMsg(`“${failed.name}” failed to process (${failed.error ?? "unknown error"}) — remove it or re-upload before sending.`);
      return;
    }
    setSendBlockMsg("");
    onSend({
      query: value.trim(),
      modelId: settings.selectedChatModelId || "",
      mentionedItems: mentionItems,
      imageFiles: images.map((i) => i.file),
      attachments,
    });
  };

  const showStop = isReplying && canStop !== false && !value.trim();

  return (
    <div className="card relative flex flex-col gap-2 p-3 sm:p-4" onClick={() => closePopups()}>
      {images.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {images.map((img, i) => (
            <span key={i} className="relative inline-block">
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img src={img.preview} alt="" className="h-14 w-14 rounded-[8px] border border-hairline object-cover" />
              <button
                className="absolute -right-1.5 -top-1.5 flex h-5 w-5 items-center justify-center rounded-full bg-surface-dark text-[12px] text-on-dark"
                onClick={(e) => {
                  e.stopPropagation();
                  onRemoveImage(i);
                }}
                aria-label="Remove image"
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}

      {attachments.length > 0 && (
        <div className="flex flex-col gap-1.5">
          {attachments.map((a) => (
            <div key={a.localId} className="flex items-center gap-3 rounded-[8px] border border-hairline px-3 py-2">
              <span className="min-w-0 flex-1">
                <span className="block truncate text-[14px] font-medium text-ink">{a.name}</span>
                <span className="caption text-muted">
                  {formatFileSize(a.size)} ·{" "}
                  {a.status === "uploading"
                    ? `Uploading ${a.progress ?? 0}%`
                    : a.status === "local"
                      ? "Will upload on send"
                      : a.status === "ready"
                        ? "Ready"
                        : a.status === "failed"
                          ? (a.error ?? "Failed")
                          : "Parsing…"}
                </span>
              </span>
              {(a.status === "uploading" || a.status === "processing" || a.status === "uploaded") && (
                <span className="caption text-muted">…</span>
              )}
              <button className="text-muted hover:text-ink" onClick={() => onRemoveAttachment(a.localId)} aria-label="Remove file">
                ×
              </button>
            </div>
          ))}
        </div>
      )}

      {sendBlockMsg && <p className="caption text-error">{sendBlockMsg}</p>}

      <MentionChips />

      <div className="relative" onClick={(e) => e.stopPropagation()}>
        <textarea
          ref={textareaRef}
          value={value}
          autoFocus={autoFocus}
          onChange={handleInput}
          onKeyDown={onKeyDown}
          rows={Math.min(6, Math.max(1, value.split("\n").length))}
          placeholder={placeholder}
          className="max-h-[160px] w-full flex-1 resize-none bg-transparent py-2 text-[14px] leading-relaxed text-ink outline-none placeholder:text-muted-soft"
        />
        <MentionPicker
          open={mentionOpen}
          keyword={mentionKeyword}
          activeIndex={mentionIndex}
          onActiveIndex={setMentionIndex}
          onSelect={commitMention}
          onClose={() => setMentionOpen(false)}
        />
        {/* count bridge for arrow-key clamp (picker owns the list) */}
        <MentionCounter keyword={mentionKeyword} open={mentionOpen} onCount={setMentionItemsCount} />
      </div>

      <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
        {/* Left group wraps internally on narrow screens; send stays pinned
            right on the first row instead of dropping to a line of its own. */}
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5 sm:gap-2">
          <div className="relative">
            <AgentModeButton onOpen={() => { closePopups(); setAgentOpen(true); }} />
            <AgentSelector open={agentOpen} onClose={() => setAgentOpen(false)} />
          </div>

          <button
            onClick={() => toggleWebSearch(!websearchOn)}
            title={webSearchReady ? (websearchOn ? "Web search on" : "Web search off") : "No default search provider"}
            className={`flex h-7 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border px-2 text-[12.5px] font-medium transition-colors sm:px-2.5 ${
              websearchOn ? "border-[#cfe1fd] bg-[#edf5ff] text-[#0f2d59] dark:border-[#223d63] dark:bg-[#15273f] dark:text-[#dce9fe]" : "border-hairline-strong text-muted hover:border-ink hover:text-ink"
            } ${webSearchReady ? "" : "opacity-50"}`}
          >
            <IconGlobe className="h-3.5 w-3.5" /> {websearchOn ? "Web on" : "Web"}
          </button>

          <div className="relative">
            <button
              onClick={() => {
                const next = !attachOpen;
                closePopups();
                setAttachOpen(next);
              }}
              title={sessionId ? "Attach files or images" : "Attach (upload after session is created)"}
              className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-full border transition-colors ${
                images.length + attachments.length > 0 ? "border-ink text-ink" : "border-hairline-strong text-muted hover:border-ink hover:text-ink"
              }`}
            >
              <IconPaperclip className="h-3.5 w-3.5" />
              {images.length + attachments.length > 0 && (
                <span className="caption ml-0.5">{images.length + attachments.length}</span>
              )}
            </button>
            {attachOpen && (
              <>
                <div className="fixed inset-0 z-40" onClick={() => setAttachOpen(false)} />
                <div className="card absolute bottom-full left-0 z-50 mb-2 w-[200px] p-1.5 shadow-[0_4px_16px_rgba(0,0,0,0.08)]">
                  <button
                    onClick={() => {
                      setAttachOpen(false);
                      onPickFiles();
                    }}
                    className="flex w-full items-center gap-2.5 rounded-[8px] px-3 py-2 text-left text-[13px] text-body transition-colors hover:bg-surface-strong hover:text-ink"
                  >
                    <IconDoc className="h-4 w-4 shrink-0" />
                    <span className="min-w-0 flex-1">
                      <span className="block">Upload file</span>
                      <span className="caption block text-muted">PDF, DOCX, XLSX…</span>
                    </span>
                  </button>
                  <button
                    onClick={() => {
                      setAttachOpen(false);
                      onPickImages();
                    }}
                    disabled={!imageCapable}
                    title={imageCapable ? undefined : "Current agent doesn't support images"}
                    className="flex w-full items-center gap-2.5 rounded-[8px] px-3 py-2 text-left text-[13px] text-body transition-colors hover:bg-surface-strong hover:text-ink disabled:opacity-50 disabled:hover:bg-transparent disabled:hover:text-body"
                  >
                    <IconImage className="h-4 w-4 shrink-0" />
                    <span className="min-w-0 flex-1">
                      <span className="block">Upload image</span>
                      <span className="caption block text-muted">
                        {imageCapable ? "JPEG, PNG, GIF, WebP" : "Not supported by this agent"}
                      </span>
                    </span>
                  </button>
                </div>
              </>
            )}
          </div>
        </div>

        <div className="relative ml-auto flex shrink-0 items-center gap-2">
          {showStop ? (
            <button onClick={onStop} className="btn btn-outline h-9 shrink-0" title="Stop generation">
              ■ Stop
            </button>
          ) : (
            <button
              onClick={submit}
              disabled={!value.trim() || isReplying}
              className="btn btn-primary h-9 w-9 shrink-0 p-0! disabled:opacity-40"
              title="Send (Enter)"
            >
              <IconSend className="h-4 w-4" />
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

function MentionCounter({ keyword, open, onCount }: { keyword: string; open: boolean; onCount: (n: number) => void }) {
  const { knowledgeBases, mcpServices, skills } = useChatContext();
  useEffect(() => {
    if (!open) return;
    const q = keyword.trim().toLowerCase();
    const match = (n: string) => !q || n.toLowerCase().includes(q);
    onCount(
      knowledgeBases.filter((k) => match(k.name)).length +
        mcpServices.filter((m) => match(m.name)).length +
        skills.filter((s) => match(s.name)).length +
        20 /* files+tags fetched remotely */,
    );
  }, [keyword, open, knowledgeBases, mcpServices, skills, onCount]);
  return null;
}
