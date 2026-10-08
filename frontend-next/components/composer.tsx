"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { IconDoc, IconGlobe, IconImage, IconPaperclip, IconSend } from "@/components/icons";
import { useChatContext, type MentionRequestItem } from "@/lib/chat-context";
import type { QuestionOrigin } from "@/lib/question-origin";
import { MentionChips, MentionPicker, type PickerItem } from "@/components/mention-picker";
import { AgentLockNotice, AgentModeButton, AgentSelector, useAgentModelSync } from "@/components/agent-selector";
import { formatFileSize, type PendingAttachment } from "@/components/use-attachments";
import { isWordAttachment } from "@/lib/api/document-workspace";
import { useT } from "@/lib/i18n";
import { FileTypeIcon } from "@/components/files/file-type-icon";
import { DOC_MENTION_COLOR, DocMentionIcon } from "@/components/doc-mention";
import { documentsNamedIn, splitDocumentMentions } from "@/lib/document-mentions";

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
  compact = false,
  agentLockFileName = null,
  documents = NO_DOCUMENT_MENTIONS,
  uploadsBecomeSources = false,
  onOpenAttachmentForEditing,
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
  /** Document-assistant split view: phone-sized padding and type. */
  compact?: boolean;
  /** The conversation holds this open document: the mode picker is locked. */
  agentLockFileName?: string | null;
  /** Document assistant: the session's open documents, offered first by @.
   *  Picking one writes "@<name>" into the text; the names left in the text
   *  on send are the mentioned documents. */
  documents?: MentionRequestItem[];
  /** Document assistant: a parsed upload becomes a source document. */
  uploadsBecomeSources?: boolean;
  /** Opens a Word source in the editor ("Mở để soạn thảo"). */
  onOpenAttachmentForEditing?: (a: PendingAttachment) => void;
}) {
  const ctx = useChatContext();
  const { t } = useT();
  const { settings, mentionItems, webSearchReady, toggleWebSearch, selectedAgent } = ctx;
  useAgentModelSync();

  const [mentionOpen, setMentionOpen] = useState(false);
  const [mentionKeyword, setMentionKeyword] = useState("");
  const [mentionIndex, setMentionIndex] = useState(0);
  const [mentionAnchor, setMentionAnchor] = useState(0);
  const [mentionList, setMentionList] = useState<PickerItem[]>([]);
  const [agentOpen, setAgentOpen] = useState(false);
  const [attachOpen, setAttachOpen] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const highlightRef = useRef<HTMLDivElement>(null);

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

  const documentMentions = useMemo(() => documentsNamedIn(value, documents), [value, documents]);

  const commitMention = (item: MentionRequestItem) => {
    const el = textareaRef.current;
    const cursor = el?.selectionStart ?? value.length;
    if (item.type === "document") {
      // Keep the document inline where it was typed, so the sentence reads on.
      const token = `@${item.name} `;
      const rest = value.slice(cursor);
      onChange(value.slice(0, mentionAnchor) + token + (rest.startsWith(" ") ? rest.slice(1) : rest));
      setMentionOpen(false);
      requestAnimationFrame(() => {
        if (!el) return;
        el.selectionStart = el.selectionEnd = mentionAnchor + token.length;
        el.focus();
      });
      return;
    }
    if (item.type === "kb") ctx.addKnowledgeBase(item.id);
    else if (item.type === "file") ctx.addFile(item.id, item.kb_id, item.name);
    else if (item.type === "tag" && item.kb_id) ctx.addTag({ id: item.id, name: item.name, kbId: item.kb_id, kbName: item.kb_name });
    else if (item.type === "mcp") ctx.addMCPService(item.id);
    else if (item.type === "skill") ctx.addSkill(item.skill_name ?? item.id);
    // Strip the @query from the text like onMentionSelect.
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
        setMentionIndex((i) => Math.min(Math.max(0, mentionList.length - 1), i + 1));
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
      if ((e.key === "Enter" || e.key === "Tab") && !e.nativeEvent.isComposing) {
        const item = mentionList[mentionIndex];
        if (item) {
          e.preventDefault();
          commitMention(item);
          return;
        }
        // nothing listed yet: Tab moves on, Enter does not send half a mention
        if (e.key === "Enter") {
          e.preventDefault();
          return;
        }
      }
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      submit();
      return;
    }
    if (e.key === "Backspace" && documents.length > 0) dropMentionBeforeCursor(e);
  };

  /* Backspace right after "@name" (or "@name ") removes the whole mention. */
  const dropMentionBeforeCursor = (e: React.KeyboardEvent) => {
    const el = textareaRef.current;
    if (!el || el.selectionStart !== el.selectionEnd) return;
    const cursor = el.selectionStart;
    const segs = splitDocumentMentions(value.slice(0, cursor), documents.map((d) => d.name));
    const last = segs[segs.length - 1];
    const prev = segs[segs.length - 2];
    let start = -1;
    if (last?.mention) start = cursor - last.text.length;
    else if (last?.text === " " && prev?.mention) start = cursor - 1 - prev.text.length;
    if (start < 0) return;
    e.preventDefault();
    onChange(value.slice(0, start) + value.slice(cursor));
    requestAnimationFrame(() => {
      el.selectionStart = el.selectionEnd = start;
    });
  };

  const imageCapable = selectedAgent?.config?.image_upload_enabled === true;
  const websearchOn = settings.webSearchEnabled;
  const placeholder =
    documents.length > 1 && documentMentions.length === 0
      ? t("docws.mentionHint")
      : selectedAgent && !selectedAgent.is_builtin
      ? selectedAgent.description || t("composer.askAgent", { name: selectedAgent.name })
      : mentionItems.length > 0
        ? t("composer.askContext")
        : websearchOn
          ? t("composer.askAnyWeb")
          : t("composer.askAny");

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
      mentionedItems: [...documentMentions, ...mentionItems],
      imageFiles: images.map((i) => i.file),
      attachments,
    });
  };

  const showStop = isReplying && canStop !== false && !value.trim();

  return (
    <div className={`card relative flex flex-col gap-2 ${compact ? "p-2.5" : "p-3 sm:p-4"}`} onClick={() => closePopups()}>
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
              <FileTypeIcon name={a.name} />
              <span className="min-w-0 flex-1">
                <span className="flex min-w-0 items-center gap-1.5">
                  <span className="truncate text-[14px] font-medium text-ink">{a.name}</span>
                  {a.source && (
                    <span className="caption shrink-0 rounded-full bg-surface-strong px-1.5 py-px text-muted">
                      {a.source.handle ? `${a.source.handle} · ` : ""}
                      {a.source.role === "target" ? t("docws.roleTarget") : t("docws.roleSource")}
                    </span>
                  )}
                </span>
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
              {a.source?.role === "source" && onOpenAttachmentForEditing && isWordAttachment(a.name) && (
                <button type="button" className="btn btn-outline btn-sm shrink-0" onClick={() => onOpenAttachmentForEditing(a)}>
                  {t("docws.openForEditing")}
                </button>
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
        {documentMentions.length > 0 && (
          // The visible text, drawn behind the textarea (its own text is
          // transparent; only the caret and selection show). Glyph widths
          // must match the textarea's: the "@" stays as an invisible
          // placeholder under the file icon, and the bold is a stroke.
          <div
            ref={highlightRef}
            aria-hidden
            className={`pointer-events-none absolute inset-0 overflow-hidden whitespace-pre-wrap break-words ${compact ? "py-1.5 text-[13px]" : "py-2 text-[14px]"} leading-relaxed text-ink`}
          >
            {splitDocumentMentions(value, documents.map((d) => d.name)).map((seg, i) =>
              seg.mention ? (
                <span key={i} className={`relative ${DOC_MENTION_COLOR}`} style={{ WebkitTextStroke: "0.35px currentColor" }}>
                  <span className="text-transparent">@</span>
                  <DocMentionIcon name={seg.text.slice(1)} className="absolute left-0 top-[0.2em]" />
                  {seg.text.slice(1)}
                </span>
              ) : (
                <span key={i}>{seg.text}</span>
              ),
            )}
            {"\n"}
          </div>
        )}
        <textarea
          ref={textareaRef}
          value={value}
          autoFocus={autoFocus}
          onChange={handleInput}
          onKeyDown={onKeyDown}
          onScroll={(e) => {
            if (highlightRef.current) highlightRef.current.scrollTop = e.currentTarget.scrollTop;
          }}
          rows={Math.min(6, Math.max(1, value.split("\n").length))}
          placeholder={placeholder}
          className={`relative max-h-[160px] w-full flex-1 resize-none break-words bg-transparent ${compact ? "py-1.5 text-[13px]" : "py-2 text-[14px]"} leading-relaxed ${documentMentions.length > 0 ? "text-transparent caret-ink" : "text-ink"} outline-none placeholder:text-muted-soft`}
        />
        <MentionPicker
          open={mentionOpen}
          keyword={mentionKeyword}
          activeIndex={mentionIndex}
          onActiveIndex={setMentionIndex}
          onSelect={commitMention}
          onClose={() => setMentionOpen(false)}
          onItems={setMentionList}
          documents={documents}
        />
      </div>

      <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
        {/* Left group wraps internally on narrow screens; send stays pinned
            right on the first row instead of dropping to a line of its own. */}
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5 sm:gap-2">
          <div className="relative">
            <AgentModeButton locked={!!agentLockFileName} onOpen={() => { closePopups(); setAgentOpen(true); }} />
            {agentLockFileName ? (
              <AgentLockNotice open={agentOpen} fileName={agentLockFileName} onClose={() => setAgentOpen(false)} />
            ) : (
              <AgentSelector open={agentOpen} onClose={() => setAgentOpen(false)} />
            )}
          </div>

          <button
            onClick={() => toggleWebSearch(!websearchOn)}
            title={webSearchReady ? (websearchOn ? "Web search on" : "Web search off") : "No default search provider"}
            className={`flex h-7 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full px-2 text-[12.5px] font-medium transition-colors sm:px-2.5 ${
              websearchOn ? "bg-[#edf5ff] text-[#0f2d59] dark:bg-[#15273f] dark:text-[#dce9fe]" : "text-muted hover:bg-surface-strong hover:text-ink"
            } ${webSearchReady ? "" : "opacity-50"}`}
          >
            <IconGlobe className="h-3.5 w-3.5" /> Web
          </button>

          <div className="relative">
            <button
              onClick={() => {
                const next = !attachOpen;
                closePopups();
                setAttachOpen(next);
              }}
              title={
                uploadsBecomeSources
                  ? t("composer.uploadsBecomeSourcesHint")
                  : sessionId
                    ? "Attach files or images"
                    : "Attach (upload after session is created)"
              }
              className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-full transition-colors ${
                images.length + attachments.length > 0 ? "bg-surface-strong text-ink" : "text-muted hover:bg-surface-strong hover:text-ink"
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
                <div className="card absolute bottom-full left-0 z-50 mb-2 w-[230px] p-1.5 shadow-[0_4px_16px_rgba(0,0,0,0.08)]">
                  <button
                    onClick={() => {
                      setAttachOpen(false);
                      onPickFiles();
                    }}
                    className="flex w-full items-center gap-2.5 rounded-[8px] px-3 py-2 text-left text-[13px] text-body transition-colors hover:bg-surface-strong hover:text-ink"
                  >
                    <IconDoc className="h-4 w-4 shrink-0" />
                    <span className="min-w-0 flex-1 leading-tight">
                      <span className="block whitespace-nowrap">Upload file</span>
                      <span className={`caption block text-muted ${uploadsBecomeSources ? "" : "whitespace-nowrap"}`}>
                        {uploadsBecomeSources ? t("composer.uploadsBecomeSources") : "PDF, DOCX, XLSX…"}
                      </span>
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
                    <span className="min-w-0 flex-1 leading-tight">
                      <span className="block whitespace-nowrap">Upload image</span>
                      <span className="caption block whitespace-nowrap text-muted">
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

const NO_DOCUMENT_MENTIONS: MentionRequestItem[] = [];

