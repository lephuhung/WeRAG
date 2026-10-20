"use client";

import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { IconDoc, IconGlobe, IconImage, IconPaperclip, IconSend } from "@/components/icons";
import { useChatContext, type MentionRequestItem } from "@/lib/chat-context";
import type { QuestionOrigin } from "@/lib/question-origin";
import { MentionChips, MentionPicker, type PickerItem } from "@/components/mention-picker";
import { AgentLockNotice, AgentModeButton, AgentSelector, useAgentModelSync } from "@/components/agent-selector";
import { formatFileSize, type PendingAttachment } from "@/components/use-attachments";
import { isWordAttachment } from "@/lib/api/document-workspace";
import { useT } from "@/lib/i18n";
import { DictationButton } from "@/components/dictation-button";
import { FileTypeIcon } from "@/components/files/file-type-icon";
import { DOC_MENTION_COLOR, DocMentionIcon } from "@/components/doc-mention";
import { documentsNamedIn, splitDocumentMentions } from "@/lib/document-mentions";
import { ErrorNote } from "@/components/chat/error-note";

/* Ports Input-field.vue's composer: textarea + @mention picker + image/file
 * attachments + agent-mode switch + websearch toggle + model picker + send/stop.
 * Emits the same send-msg tuple the Vue chat view consumes:
 * (query, modelId, mentionedItems, imageFiles, attachmentFiles).
 */

/* The composer field grows with the text, up to this height (px). */
const COMPOSER_MAX_HEIGHT = 160;

/* useLayoutEffect warns when the component is rendered on the server. */
const useIsomorphicLayoutEffect = typeof window !== "undefined" ? useLayoutEffect : useEffect;

export type ComposerSend = {
  query: string;
  modelId: string;
  mentionedItems: MentionRequestItem[];
  imageFiles: File[];
  attachments: PendingAttachment[];
  // Retrieval hint from a picked suggested question (creatChat.vue
  // questionOriginFromSuggestion); plain typed text leaves it undefined.
  questionOrigin?: QuestionOrigin;
  /** Resend of a question whose automatic word readings the user rejected. */
  abbreviationAskWords?: string[];
};

export function Composer({
  sessionId,
  value,
  onChange,
  onSend,
  onStop,
  isReplying,
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
  sendRequest = null,
  toolbarStart = null,
  toolbarEnd = null,
}: {
  sessionId?: string;
  value: string;
  onChange: (v: string) => void;
  onSend: (s: ComposerSend) => void;
  onStop?: () => void;
  isReplying?: boolean;
  attachments: PendingAttachment[];
  images: Array<{ preview: string; file: File }>;
  onRemoveAttachment: (localId: string) => void;
  onRemoveImage: (index: number) => void;
  onPickFiles: () => void;
  onPickImages: () => void;
  autoFocus?: boolean;
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
  /** Sends this text right away (a picked quick action); a new seq sends again. */
  sendRequest?: { text: string; seq: number } | null;
  /** Extra document tools after attachment and web search. */
  toolbarStart?: ReactNode;
  /** Shown just before the send button (e.g. the context usage ring). */
  toolbarEnd?: ReactNode;
}) {
  const ctx = useChatContext();
  const { t } = useT();
  const { settings, mentionItems, webSearchReady, toggleWebSearch, selectedAgent, isGuest } = ctx;
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

  /* The composer grows with the text: a wrapped paragraph must not scroll
   * inside a one-row field. The height is measured (scrollHeight) and
   * capped; the field shrinks again when the text is deleted. */
  useIsomorphicLayoutEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, COMPOSER_MAX_HEIGHT)}px`;
  }, [value, compact]);

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
    // A guest has nothing to mention (no KBs, files, MCP or skills).
    if (!isGuest && at >= 0 && !before.slice(at + 1).includes(" ") && !before.slice(at + 1).includes("\n")) {
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
  const placeholder = isGuest
    ? t("guest.composerPlaceholder")
    : documents.length > 1 && documentMentions.length === 0
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
  const [dictating, setDictating] = useState(false);
  const [dictationMsg, setDictationMsg] = useState("");
  const stopDictation = useRef<(() => void) | null>(null);
  // Keep the newest dictated words in view.
  useEffect(() => {
    const el = textareaRef.current;
    if (dictating && el) el.scrollTop = el.scrollHeight;
  }, [dictating, value]);

  const submit = (text = value) => {
    if (!text.trim() || isReplying) return;
    if (attachments.some((a) => a.status === "uploading")) {
      setSendBlockMsg(t("composer.uploadBlocked"));
      return;
    }
    const failed = attachments.find((a) => a.status === "failed");
    if (failed) {
      setSendBlockMsg(
        t("composer.uploadFailed", {
          name: failed.name,
          error: failed.error ?? t("composer.unknownError"),
        }),
      );
      return;
    }
    setSendBlockMsg("");
    stopDictation.current?.();
    onSend({
      query: text.trim(),
      modelId: settings.selectedChatModelId || "",
      mentionedItems: [...(text === value ? documentMentions : documentsNamedIn(text, documents)), ...mentionItems],
      imageFiles: images.map((i) => i.file),
      attachments,
    });
  };

  const sendRequestSeq = sendRequest?.seq;
  useEffect(() => {
    if (sendRequest) submit(sendRequest.text);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sendRequestSeq]);

  /* The Stop button stays reachable while the answer streams: a draft in
   * the field must not take it away (it used to disappear as soon as the
   * user typed, leaving neither a stop nor a send). It only renders when
   * the host can actually abort the turn — a permanently disabled button
   * is worse than no button. */
  const showStop = Boolean(isReplying && onStop);

  const agentControl = isGuest ? (
    // fixed: a guest chats with the public-lookup assistant only
    <span
      className="flex h-7 max-w-[160px] items-center rounded-full bg-accent-surface px-2.5 text-[12.5px] font-medium text-accent sm:max-w-[260px]"
      title={t("guest.agentLocked")}
    >
      <span className="min-w-0 truncate">{selectedAgent?.name ?? t("guest.agentName")}</span>
    </span>
  ) : (
    <div className={uploadsBecomeSources ? "relative min-w-0 [&>button]:max-w-full" : "relative"}>
      <AgentModeButton locked={!!agentLockFileName} onOpen={() => { closePopups(); setAgentOpen(true); }} />
      {agentLockFileName ? (
        <AgentLockNotice open={agentOpen} fileName={agentLockFileName} onClose={() => setAgentOpen(false)} />
      ) : (
        <AgentSelector open={agentOpen} onClose={() => setAgentOpen(false)} />
      )}
    </div>
  );
  const webControl = (
    <button
      type="button"
      onClick={() => toggleWebSearch(!websearchOn)}
      aria-pressed={websearchOn}
      aria-label={t("composer.webSearch")}
      title={
        webSearchReady
          ? websearchOn
            ? t("composer.webSearchOn")
            : t("composer.webSearchOff")
          : t("composer.webSearchUnavailable")
      }
      className={`flex h-7 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full text-[12.5px] font-medium transition-colors ${uploadsBecomeSources ? "w-7 justify-center" : "px-2 sm:px-2.5"} ${
        websearchOn ? "bg-accent-surface text-accent" : "text-muted hover:bg-surface-strong hover:text-ink"
      } ${webSearchReady ? "" : "opacity-50"}`}
    >
      <IconGlobe className="h-3.5 w-3.5" />{!uploadsBecomeSources && ` ${t("composer.webLabel")}`}
    </button>
  );
  const attachmentControl = (
    <div className="relative">
      <button
        type="button"
        onClick={() => {
          const next = !attachOpen;
          closePopups();
          setAttachOpen(next);
        }}
        aria-label={t("composer.attach")}
        aria-expanded={attachOpen}
        title={
          uploadsBecomeSources
            ? t("composer.uploadsBecomeSourcesHint")
            : sessionId
              ? t("composer.attachFiles")
              : t("composer.attachAfterSession")
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
              className="flex w-full items-center gap-2.5 rounded-md2 px-3 py-2 text-left text-[13px] text-body transition-colors hover:bg-surface-strong hover:text-ink"
            >
              <IconDoc className="h-4 w-4 shrink-0" />
              <span className="min-w-0 flex-1 leading-tight">
                <span className="block whitespace-nowrap">{t("composer.uploadFile")}</span>
                <span className={`caption block text-muted ${uploadsBecomeSources ? "" : "whitespace-nowrap"}`}>
                  {uploadsBecomeSources ? t("composer.uploadsBecomeSources") : t("composer.fileTypes")}
                </span>
              </span>
            </button>
            <button
              onClick={() => {
                setAttachOpen(false);
                onPickImages();
              }}
              disabled={!imageCapable}
              title={imageCapable ? undefined : t("composer.agentNoImages")}
              className="flex w-full items-center gap-2.5 rounded-md2 px-3 py-2 text-left text-[13px] text-body transition-colors hover:bg-surface-strong hover:text-ink disabled:opacity-50 disabled:hover:bg-transparent disabled:hover:text-body"
            >
              <IconImage className="h-4 w-4 shrink-0" />
              <span className="min-w-0 flex-1 leading-tight">
                <span className="block whitespace-nowrap">{t("composer.uploadImage")}</span>
                <span className="caption block whitespace-nowrap text-muted">
                  {imageCapable ? t("composer.imageTypes") : t("composer.imagesNotSupported")}
                </span>
              </span>
            </button>
          </div>
        </>
      )}
    </div>
  );

  return (
    <div
      className={`card relative flex flex-col gap-2 focus-within:ring-2 focus-within:ring-ink/25 ${compact ? "p-2.5" : "p-3 sm:p-4"}`}
      onClick={() => closePopups()}
    >
      {images.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {images.map((img, i) => (
            <span key={i} className="relative inline-block">
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img src={img.preview} alt="" className="h-14 w-14 rounded-md2 border border-hairline object-cover" />
              <button
                className="absolute -right-1.5 -top-1.5 flex h-5 w-5 items-center justify-center rounded-full bg-surface-dark text-[12px] text-on-dark"
                onClick={(e) => {
                  e.stopPropagation();
                  onRemoveImage(i);
                }}
                aria-label={t("composer.removeImage")}
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
            <div key={a.localId} className="flex items-center gap-3 rounded-md2 border border-hairline px-3 py-2">
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
                    ? t("composer.uploading", { pct: a.progress ?? 0 })
                    : a.status === "local"
                      ? t("composer.uploadOnSend")
                      : a.status === "ready"
                        ? t("composer.ready")
                        : a.status === "failed"
                          ? (a.error ?? t("composer.failed"))
                          : t("composer.parsing")}
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
              <button
                type="button"
                className="text-muted hover:text-ink"
                onClick={() => onRemoveAttachment(a.localId)}
                aria-label={t("composer.removeFile")}
              >
                ×
              </button>
            </div>
          ))}
        </div>
      )}

      {sendBlockMsg && <ErrorNote compact>{sendBlockMsg}</ErrorNote>}
      {dictationMsg && <ErrorNote compact>{dictationMsg}</ErrorNote>}
      {dictating && <p className="caption text-muted">{t("dictation.listening")}</p>}

      {!isGuest && <MentionChips />}

      <div className="relative" onClick={(e) => e.stopPropagation()}>
        {documentMentions.length > 0 && (
          // The visible text, drawn behind the textarea (its own text is
          // transparent; only the caret and selection show). Glyph widths
          // must match the textarea's: the "@" stays as an invisible
          // placeholder under the file icon, and the bold is a stroke.
          <div
            ref={highlightRef}
            data-composer-highlight
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
          readOnly={dictating}
          onScroll={(e) => {
            if (highlightRef.current) highlightRef.current.scrollTop = e.currentTarget.scrollTop;
          }}
          rows={1}
          placeholder={placeholder}
          className={`relative max-h-[160px] w-full flex-1 resize-none break-words bg-transparent ${compact ? "py-1.5 text-[13px]" : "py-2 text-[14px]"} leading-relaxed ${documentMentions.length > 0 ? "text-transparent caret-ink" : "text-ink"} outline-none placeholder:text-muted-soft`}
        />
        {!isGuest && <MentionPicker
          open={mentionOpen}
          keyword={mentionKeyword}
          activeIndex={mentionIndex}
          onActiveIndex={setMentionIndex}
          onSelect={commitMention}
          onClose={() => setMentionOpen(false)}
          onItems={setMentionList}
          documents={documents}
        />}
      </div>

      <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
        {/* Left group wraps internally on narrow screens; send stays pinned
            right on the first row instead of dropping to a line of its own. */}
        <div className={uploadsBecomeSources ? "flex shrink-0 items-center gap-1.5" : "flex min-w-0 flex-1 flex-wrap items-center gap-1.5 sm:gap-2"}>
          {isGuest ? (
            // no web search, attachments or images for a guest
            <>{agentControl}{toolbarStart}</>
          ) : uploadsBecomeSources ? (
            <>{attachmentControl}{webControl}{toolbarStart}</>
          ) : (
            <>{agentControl}{webControl}{attachmentControl}{toolbarStart}</>
          )}

          <DictationButton
            value={value}
            onChange={onChange}
            onListeningChange={setDictating}
            onError={setDictationMsg}
            stopRef={stopDictation}
            disabled={isReplying && !dictating}
          />
        </div>

        <div className={uploadsBecomeSources ? "relative ml-auto flex min-w-0 flex-1 items-center justify-end gap-1.5" : "relative ml-auto flex shrink-0 items-center gap-2"}>
          {toolbarEnd}
          {uploadsBecomeSources && agentControl}
          {showStop ? (
            <button
              type="button"
              onClick={() => onStop?.()}
              className="btn btn-outline h-9 shrink-0"
              title={t("composer.stopTitle")}
              aria-label={t("composer.stopTitle")}
            >
              ■ {t("composer.stop")}
            </button>
          ) : (
            <button
              type="button"
              onClick={() => submit()}
              disabled={!value.trim() || isReplying}
              className="btn btn-primary h-9 w-9 shrink-0 p-0! disabled:opacity-40"
              title={t("composer.sendTitle")}
              aria-label={t("composer.send")}
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

