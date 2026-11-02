/* The message-level parts of the conversation: the user's bubble, the
 * assistant's answer with its cards, the source chips and the small
 * utilities under a turn. They were part of chat-client.tsx; they live here
 * now so the chat screen file holds the state machine only. */
"use client";

import { memo, useState } from "react";
import { DOC_MENTION_COLOR, DocMention, DocMentionIcon } from "@/components/doc-mention";
import { splitDocumentMentions } from "@/lib/document-mentions";
import { selectionNeedsCollapse } from "@/lib/api/document-workspace";
import type { DocumentSelection } from "@/lib/api/document-workspace";
import { dayLabel, messageDateTime, messageTime } from "@/lib/message-time";
import { useT } from "@/lib/i18n";
import { setMessageFeedback, type MessageFeedback } from "@/lib/api/chat";
import { useToast } from "@/components/toast";
import { copyToClipboard } from "@/lib/clipboard";
import { Markdown } from "@/components/markdown";
import { useSmoothText } from "@/lib/use-smooth-text";
import { IconCopy, IconCheck, IconFork, IconEdit, IconGlobe, IconRefresh, IconThumbUp, IconThumbDown } from "@/components/icons";
import { renderFileIconSvg } from "@/components/files/file-icon";
import { formatFileSize } from "@/components/use-attachments";
import { ThinkingDisplay } from "@/components/chat/thinking-display";
import { PeopleCard } from "@/components/chat/people-card";
import { SavedMemoryNotice } from "@/components/chat/saved-memory-notice";
import { ChoiceListView } from "@/components/chat/choice-list";
import { LegalRequestCardView } from "@/components/chat/legal-request-card";
import { RewriteProposalCard, type ProposalOutcome } from "@/components/chat/rewrite-proposal-card";
import { IllustrationCard } from "@/components/chat/illustration-card";
import { AbbreviationSuggestionCard } from "@/components/chat/abbreviation-suggestion-card";
import { FollowUpSuggestions } from "@/components/chat/follow-up-suggestions";
import { ErrorNote } from "@/components/chat/error-note";
import { ReferenceChip } from "@/components/chat/reference-chip";
import type { KnowledgeReferenceItem } from "@/components/chat/references-drawer";
import type { ChoiceOption } from "@/lib/choice-list";
import type { Illustration, RewriteProposal, RewriteVariant } from "@/lib/api/document-ops";
import type { RasterImage } from "@/lib/illustration-render";
import type { UiMessage } from "@/components/chat/chat-types";

/* Answer text streamed between tool rounds is a preamble that the next
 * tool_call retracts (folded into the thinking card title). Longer output is
 * the real final answer — that is what collapses the thinking panel. */
const PREAMBLE_MAX_CHARS = 240;

/* "vb2 · Tờ trình.docx" (stored selection label) → "Tờ trình.docx". */
function documentNameOfLabel(label: string | undefined): string | undefined {
  if (!label) return undefined;
  return label.replace(/^vb\d+\s*·\s*/, "") || undefined;
}

// When a people card renders, the raw profile dump must not also print as
// answer text: strip <people_lookup> blocks, and cut the old-style persisted
// dump ("👤 HỒ SƠ #n" blocks with ═ separators). The "Tìm thấy N người"
// headline stays — it's a useful one-line summary above the cards.
function stripPeopleDump(content: string, hasCard: boolean): string {
  if (!hasCard) return content;
  let text = content.replace(/<people_lookup>[\s\S]*?<\/people_lookup>/g, "");
  const dumpStart = text.search(/👤|HỒ SƠ #|═{5,}/);
  if (dumpStart >= 0) text = text.slice(0, dumpStart);
  return text.trim();
}

/* Quoted editor passage above a user message (document assistant). */
function SelectionQuote({ selection }: { selection: DocumentSelection }) {
  const { t } = useT();
  const [expanded, setExpanded] = useState(false);
  const collapsible = selectionNeedsCollapse(selection.text);
  const docName = documentNameOfLabel(selection.document);
  return (
    <figure className="mb-1.5 max-w-[80%] self-end rounded-md2 bg-surface-strong/60 px-3 py-2">
      <figcaption className="caption-uppercase mb-1 truncate text-muted-soft" title={docName}>
        {t("docws.selectionLabel").replace(/:\s*$/, "")}
        {docName ? ` · ${docName}` : ""}
      </figcaption>
      <blockquote
        className={`border-l-2 border-hairline-strong pl-2.5 text-[12.5px] leading-relaxed text-muted break-words whitespace-pre-wrap ${
          collapsible && !expanded ? "line-clamp-4" : ""
        }`}
        title={selection.paragraph_hint}
      >
        {selection.text}
      </blockquote>
      {collapsible && (
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          aria-expanded={expanded}
          className="mt-1 text-[12px] font-medium text-muted hover:text-ink cursor-pointer"
        >
          {expanded ? t("docws.showLess") : t("docws.showMore")}
        </button>
      )}
    </figure>
  );
}

/** Send time under a message; the full date shows on hover. */
function MessageTime({ iso }: { iso?: string }) {
  const { locale } = useT();
  const time = messageTime(iso, locale);
  if (!time) return null;
  return (
    <time dateTime={iso} title={messageDateTime(iso, locale)} className="px-1 text-[11px] tabular-nums text-muted-soft">
      {time}
    </time>
  );
}

/** Rule with the day's name where the conversation moves to another day. */
function DayDivider({ iso }: { iso?: string }) {
  const { t } = useT();
  const label = dayLabel(iso);
  if (!label) return null;
  const text =
    label.kind === "date" ? label.text : label.kind === "today" ? t("chat.dayToday") : t("chat.dayYesterday");
  return (
    <div className="my-4 flex items-center gap-3 text-[12px] text-muted" role="separator">
      <span className="h-px flex-1 bg-hairline" />
      <span>{text}</span>
      <span className="h-px flex-1 bg-hairline" />
    </div>
  );
}

const UserMessageBubble = memo(function UserMessageBubble({
  message,
  onFork,
  onEdit,
  onRejectReading,
  compact = false,
}: {
  message: UiMessage;
  /** "Không phải?" under an automatic word reading: ask the question again,
   * with the gate asking about those words. */
  onRejectReading?: (m: UiMessage) => void;
  /** Document-assistant split view: mobile-sized, full-width column. */
  compact?: boolean;
  onFork?: (m: UiMessage) => void;
  onEdit?: (content: string) => void;
}) {
  const { t } = useT();
  const [copied, setCopied] = useState(false);
  const mentionNames = message.document_mentions ?? [];
  const segments = splitDocumentMentions(message.content, mentionNames);
  // Turns sent before mentions went inline carry the names only as items.
  const detachedMentions = mentionNames.filter((n) => !segments.some((seg) => seg.mention && seg.text === `@${n}`));

  const handleCopy = async () => {
    const ok = await copyToClipboard(message.content);
    if (ok) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <div className={`group flex flex-col items-end ${compact ? "mb-3" : "mb-4"}`}>
      {(message.attachments?.length ?? 0) > 0 && (
        <div className="mb-1.5 flex max-w-[80%] flex-wrap justify-end gap-1.5">
          {message.attachments!.map((a, i) => (
            <span
              key={`${a.name}-${i}`}
              title={a.title ?? (a.size ? `${a.name} · ${formatFileSize(a.size)}` : a.name)}
              className="flex items-center gap-2 rounded-md2 border border-bubble-border bg-bubble px-2.5 py-1.5 text-[12px] font-medium text-bubble-ink"
            >
              <span
                className="inline-block h-[24px] w-[22px] shrink-0"
                dangerouslySetInnerHTML={{
                  __html: renderFileIconSvg(a.isImage && !a.name.includes(".") ? "image.png" : a.name),
                }}
              />
              <span className="max-w-[180px] truncate">{a.name}</span>
            </span>
          ))}
        </div>
      )}
      {detachedMentions.length > 0 && (
        <div className="mb-1.5 flex max-w-[80%] flex-wrap justify-end gap-1.5">
          {detachedMentions.map((name, i) => (
            <span
              key={`${name}-${i}`}
              title={name}
              className={`flex items-center gap-1.5 rounded-full border border-hairline-strong bg-surface-strong px-2.5 py-1 text-[12px] font-semibold ${DOC_MENTION_COLOR}`}
            >
              <DocMentionIcon name={name} />
              <span className="max-w-[200px] truncate">{name}</span>
            </span>
          ))}
        </div>
      )}
      {message.document_selection && <SelectionQuote selection={message.document_selection} />}
      <div className={`max-w-[80%] rounded-xl2 border border-bubble-border bg-bubble ${compact ? "px-3 py-2 text-[13px]" : "px-4 py-2.5 text-[14px]"} leading-normal text-bubble-ink shadow-2xs break-words whitespace-pre-wrap`}>
        {segments.map((seg, i) =>
          seg.mention ? (
            <DocMention key={i} token={seg.text} className="text-bubble-ink" />
          ) : (
            seg.text
          ),
        )}
      </div>
      {(message.autoCorrections?.length ?? 0) > 0 && (
        <div className="mt-1 max-w-[80%] pr-1 text-right text-[12px] text-muted-soft">
          {message.autoCorrections!
            .map((tc) => t("chat.typoUnderstoodAs", { wrong: tc.wrong, correct: tc.correct }))
            .join(" · ")}
          {onRejectReading && (
            <>
              {" · "}
              <button type="button" className="underline hover:text-ink" onClick={() => onRejectReading(message)}>
                {t("chat.typoReject")}
              </button>
            </>
          )}
        </div>
      )}
      {/* Touch has no hover — actions stay visible on phones; on desktop they
          appear only while hovering the user message. */}
      <div className="mt-1 flex items-center gap-1 pr-1">
      <div className="flex items-center gap-1 text-muted-soft opacity-100 transition-opacity group-hover:opacity-100 focus-within:opacity-100 sm:opacity-0 sm:group-hover:opacity-100">
        {onEdit && (
          <button
            type="button"
            onClick={() => onEdit(message.content)}
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
            <IconCheck className="h-3.5 w-3.5 text-success" />
          ) : (
            <IconCopy className="h-3.5 w-3.5" />
          )}
        </button>
        {onFork && (
          <button
            type="button"
            onClick={() => onFork(message)}
            className="flex h-7 w-7 items-center justify-center rounded-md border border-transparent text-muted hover:border-hairline hover:bg-surface-strong hover:text-ink transition-colors cursor-pointer"
            title="Tạo nhánh từ câu hỏi này"
            aria-label="Tạo nhánh từ câu hỏi này"
          >
            <IconFork className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
      <MessageTime iso={message.createdAt} />
      </div>
    </div>
  );
});

const ACTION_BUTTON =
  "flex h-7 w-7 items-center justify-center rounded-md border border-hairline/60 bg-surface-card/60 text-muted hover:border-hairline hover:bg-surface-strong hover:text-ink transition-colors cursor-pointer";

/** Thumbs up / down on a finished answer. A down vote opens an optional note;
 * clicking the active thumb again removes the rating. The vote also lands in
 * Langfuse as the "user-feedback" score of the turn's trace. */
function AnswerFeedback({
  sessionId,
  messageId,
  initial,
  initialComment,
}: {
  sessionId: string;
  messageId: string;
  initial?: MessageFeedback;
  initialComment?: string;
}) {
  const { t } = useT();
  const toast = useToast();
  const [vote, setVote] = useState<MessageFeedback>(initial ?? 0);
  const [comment, setComment] = useState(initialComment ?? "");
  const [askNote, setAskNote] = useState(false);
  const [saving, setSaving] = useState(false);

  const save = async (next: MessageFeedback, note: string) => {
    const previous = vote;
    setVote(next);
    setSaving(true);
    try {
      await setMessageFeedback(sessionId, messageId, next, note);
      return true;
    } catch {
      setVote(previous);
      toast.error(t("chat.feedback.failed"));
      return false;
    } finally {
      setSaving(false);
    }
  };

  const onThumb = async (value: 1 | -1) => {
    if (saving) return;
    if (vote === value) {
      setAskNote(false);
      setComment("");
      await save(0, "");
      return;
    }
    const ok = await save(value, "");
    if (!ok) return;
    setComment("");
    if (value === -1) {
      setAskNote(true);
    } else {
      setAskNote(false);
      toast.success(t("chat.feedback.thanks"));
    }
  };

  const sendNote = async () => {
    const note = comment.trim();
    if (note && (await save(-1, note))) {
      toast.success(t("chat.feedback.thanks"));
    }
    setAskNote(false);
  };

  const thumbClass = (active: boolean) =>
    `${ACTION_BUTTON} disabled:cursor-default disabled:opacity-60 ${
      active ? "border-hairline bg-surface-strong text-ink" : ""
    }`;

  return (
    <>
      <button
        type="button"
        onClick={() => onThumb(1)}
        disabled={saving}
        aria-pressed={vote === 1}
        className={thumbClass(vote === 1)}
        title={vote === 1 ? t("chat.feedback.clear") : t("chat.feedback.up")}
        aria-label={t("chat.feedback.up")}
      >
        <IconThumbUp className={`h-3.5 w-3.5 ${vote === 1 ? "fill-current" : ""}`} />
      </button>
      <button
        type="button"
        onClick={() => onThumb(-1)}
        disabled={saving}
        aria-pressed={vote === -1}
        className={thumbClass(vote === -1)}
        title={vote === 1 ? t("chat.feedback.clear") : t("chat.feedback.down")}
        aria-label={t("chat.feedback.down")}
      >
        <IconThumbDown className={`h-3.5 w-3.5 ${vote === -1 ? "fill-current" : ""}`} />
      </button>
      {askNote && (
        <form
          className="basis-full mt-1.5 flex max-w-[520px] flex-col gap-1.5 rounded-lg2 border border-hairline bg-surface-card p-3"
          onSubmit={(e) => {
            e.preventDefault();
            void sendNote();
          }}
        >
          <label htmlFor={`feedback-${messageId}`} className="text-[12.5px] font-medium text-ink">
            {t("chat.feedback.commentLabel")}
          </label>
          <textarea
            id={`feedback-${messageId}`}
            value={comment}
            onChange={(e) => setComment(e.target.value)}
            maxLength={2000}
            rows={2}
            autoFocus
            placeholder={t("chat.feedback.commentPlaceholder")}
            className="input h-auto min-h-[56px] w-full resize-y text-[13px]"
          />
          <div className="flex justify-end gap-1.5">
            <button
              type="button"
              onClick={() => setAskNote(false)}
              className="rounded-md px-2.5 py-1 text-[12.5px] text-muted hover:bg-surface-strong hover:text-ink cursor-pointer"
            >
              {t("chat.feedback.skip")}
            </button>
            <button
              type="submit"
              disabled={saving || !comment.trim()}
              className="rounded-md border border-hairline bg-surface-strong px-2.5 py-1 text-[12.5px] font-medium text-ink hover:border-hairline-strong disabled:cursor-default disabled:opacity-50 cursor-pointer"
            >
              {t("chat.feedback.send")}
            </button>
          </div>
        </form>
      )}
    </>
  );
}

function BotMessageActions({
  content,
  canFork,
  onFork,
  onRegenerate,
  feedback,
  createdAt,
}: {
  content: string;
  createdAt?: string;
  canFork?: boolean;
  onFork?: () => void;
  onRegenerate?: () => void;
  /** Present when the answer is finished and saved, so it can be rated. */
  feedback?: { sessionId: string; messageId: string; initial?: MessageFeedback; initialComment?: string };
}) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    const ok = await copyToClipboard(content);
    if (ok) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <div className="mt-2.5 flex flex-wrap items-center gap-1.5 text-muted-soft">
      <button
        type="button"
        onClick={handleCopy}
        className="flex h-7 w-7 items-center justify-center rounded-md border border-hairline/60 bg-surface-card/60 text-muted hover:border-hairline hover:bg-surface-strong hover:text-ink transition-colors cursor-pointer"
        title={copied ? "Đã sao chép" : "Sao chép câu trả lời"}
        aria-label="Sao chép câu trả lời"
      >
        {copied ? (
          <IconCheck className="h-3.5 w-3.5 text-success" />
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

      {feedback && (
        <AnswerFeedback
          key={feedback.messageId}
          sessionId={feedback.sessionId}
          messageId={feedback.messageId}
          initial={feedback.initial}
          initialComment={feedback.initialComment}
        />
      )}
      <MessageTime iso={createdAt} />
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
  const { t } = useT();
  const [showAll, setShowAll] = useState(false);
  const maxInitial = 5;
  const hasMore = references.length > maxInitial;
  const displayed = showAll ? references : references.slice(0, maxInitial);
  const remaining = references.length - maxInitial;

  return (
    <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
      {displayed.map((r, i) => {
        const name = r.knowledge_title ?? r.knowledge_filename ?? r.knowledge_id ?? t("chat.refs.source");
        const isWeb =
          r.chunk_type === "web_search" || Boolean(r.metadata?.url) || r.id?.startsWith("http");
        return (
          <ReferenceChip
            key={r.chunk_id ?? r.id ?? `${r.knowledge_id || "ref"}-${i}`}
            label={name}
            index={i + 1}
            title={name}
            onClick={() => onSelectRef?.(r, i)}
            icon={
              isWeb ? (
                <IconGlobe className="h-3 w-3" />
              ) : (
                <span
                  className="inline-block h-[15px] w-[12px] shrink-0 self-center"
                  dangerouslySetInnerHTML={{
                    __html: renderFileIconSvg(r.knowledge_filename ?? r.knowledge_title ?? name),
                  }}
                />
              )
            }
          />
        );
      })}

      {hasMore && (
        <ReferenceChip
          label={showAll ? t("chat.refs.less") : `+${remaining}`}
          title={showAll ? t("chat.refs.less") : t("chat.refs.more", { n: remaining })}
          onClick={() => setShowAll((v) => !v)}
          className="px-2.5 py-1 text-[12.5px]"
        />
      )}
    </div>
  );
}

// One assistant turn. memo() keeps history rows from reconciling on every
// streamed chunk — only the row whose message object actually changed renders.
const AssistantMessage = memo(function AssistantMessage({
  m,
  index,
  sessionId,
  chatId,
  busy,
  abbreviationRefreshKey,
  onOpenDrawer,
  onFork,
  onRegenerate,
  onAsk,
  onChoicePick,
  scopeCardActive = false,
  activeProposalIds = "",
  proposalsDisabled = false,
  proposalOutcomeFor,
  onProposalApply,
  onProposalApplied,
  insertableDocIds = "",
  onIllustrationInsert,
  onIllustrationInserted,
  compact = false,
  guest = false,
}: {
  m: UiMessage;
  index: number;
  /** Guest chat: no fork, follow-up suggestions, abbreviation or memory
   * cards — their routes need an organization. */
  guest?: boolean;
  /** Sends the message of a picked option (choice list under the answer). */
  onChoicePick?: (option: ChoiceOption) => Promise<void>;
  /** The cards are live only on the last turn; older ones show their text. */
  scopeCardActive?: boolean;
  /** Comma-joined batch ids of this turn's proposals that are the newest of
   * their document (only those can apply). */
  activeProposalIds?: string;
  proposalsDisabled?: boolean;
  proposalOutcomeFor?: (batchId: string) => ProposalOutcome | undefined;
  onProposalApply?: (proposal: RewriteProposal, variant: RewriteVariant) => Promise<void>;
  onProposalApplied?: (proposal: RewriteProposal, variantId: string) => void;
  /** Comma-joined ids of the documents open in an editor tab (an
   * illustration can be inserted into those only). */
  insertableDocIds?: string;
  onIllustrationInsert?: (ill: Illustration, picture: RasterImage) => Promise<void>;
  onIllustrationInserted?: (ill: Illustration) => void;
  /** Document-assistant split view: no avatar, smaller type, tighter spacing. */
  compact?: boolean;
  sessionId: string;
  /** Chat route id — artifact downloads are scoped to the session. */
  chatId: string;
  busy: boolean;
  abbreviationRefreshKey: number;
  onOpenDrawer: (refs: KnowledgeReferenceItem[], activeItem?: KnowledgeReferenceItem, index?: number) => void;
  onFork: (m: UiMessage) => void;
  onRegenerate: (index: number) => void;
  onAsk: (text: string, attribution: { setId: string; questionId: string }, kbIds: string[]) => void;
}) {
  const { t } = useT();
  const hasPeopleCard = (m.peopleData?.length ?? 0) > 0;
  const shownContent = stripPeopleDump(m.content, hasPeopleCard);
  // answers arrive a sentence at a time; reveal them at a steady pace
  const smooth = useSmoothText(shownContent, Boolean(m.streaming));
  return (
    <div data-message-id={m.id} className={compact ? "mb-3 flex" : "mb-4 flex"}>
      {/* No avatar: the answer starts at the left edge of the column. */}
      <div className="w-full min-w-0 flex-1">
        {(m.thinking ||
          (m.steps?.length ?? 0) > 0 ||
          (m.references?.length ?? 0) > 0 ||
          (m.streaming && !m.content)) && (
          <ThinkingDisplay
            content={m.thinking ?? ""}
            /* Open while the agent reasons/calls tools; fold once the final
               answer streams. Answer text between rounds is a preamble that
               the next tool call retracts into a thinking title — only a
               sustained answer past this threshold counts as "the answer",
               otherwise the panel would flicker once per agent round. */
            streaming={
              m.streaming &&
              !(m.content.length > ((m.steps?.length ?? 0) > 0 ? PREAMBLE_MAX_CHARS : 0))
            }
            steps={m.steps}
            durationMs={m.agentDurationMs}
            references={m.references}
            compact={compact}
          />
        )}
        {/* Faint rule separating the reasoning timeline from the answer. */}
        {(m.thinking || (m.steps?.length ?? 0) > 0) && shownContent && (
          <div className={`${compact ? "my-2" : "my-3"} h-px w-full bg-hairline`} aria-hidden="true" />
        )}
        {m.legalRequestCard && <LegalRequestCardView card={m.legalRequestCard} />}
        {shownContent ? (
          <div className="[&_.chat-markdown]:text-ink">
            {/* The answer is the live region of its turn: a screen reader
                follows the text as it streams in. */}
            <Markdown
              text={smooth.text}
              streaming={m.streaming || smooth.revealing}
              live={!m.errorText}
              artifacts={m.artifacts}
              imageContext={m.assistantMessageId && chatId !== "new" ? { sessionId: chatId, messageId: m.assistantMessageId } : null}
            />
          </div>
        ) : m.errorText ? null : (
          <p className={`${compact ? "text-[13px]" : "text-[14px]"} leading-relaxed text-body`}>{m.streaming && !m.thinking && (!m.steps || m.steps.length === 0) ? "…" : ""}</p>
        )}
        {/* One error surface per turn — the same text is never shown twice. */}
        {m.errorText && <ErrorNote className="mt-2">{m.errorText}</ErrorNote>}
        {hasPeopleCard && (
          <PeopleCard people={m.peopleData!} isLoadingMore={m.streaming} />
        )}
        {!guest && (m.savedMemories?.length ?? 0) > 0 && <SavedMemoryNotice memories={m.savedMemories!} compact={compact} />}
        {/* the card itself sits above the composer (see pendingScopeCard) */}
        {!m.streaming && m.scopeCard && scopeCardActive && (
          <p className="caption mt-2 text-muted-soft">{t("docws.scopeCard.pickBelow")}</p>
        )}
        {!m.streaming && m.choiceList && scopeCardActive && onChoicePick && (
          <ChoiceListView card={m.choiceList} disabled={busy} onPick={onChoicePick} />
        )}
        {!m.streaming &&
          proposalOutcomeFor &&
          onProposalApply &&
          onProposalApplied &&
          m.rewriteProposals?.map((p) => (
            <RewriteProposalCard
              key={p.batchId}
              proposal={p}
              appliedVariantId={p.appliedVariantId}
              active={activeProposalIds.split(",").includes(p.batchId)}
              disabled={proposalsDisabled}
              outcomeFor={proposalOutcomeFor}
              onApply={onProposalApply}
              onApplied={onProposalApplied}
            />
          ))}
        {!m.streaming &&
          proposalOutcomeFor &&
          onIllustrationInsert &&
          onIllustrationInserted &&
          m.illustrations?.map((ill) => (
            <IllustrationCard
              key={ill.batchId}
              illustration={ill}
              applied={ill.applied}
              canInsert={!!ill.documentId && insertableDocIds.split(",").includes(ill.documentId)}
              disabled={proposalsDisabled}
              artifacts={m.artifacts}
              imageContext={
                m.assistantMessageId && chatId !== "new" ? { sessionId: chatId, messageId: m.assistantMessageId } : null
              }
              outcomeFor={proposalOutcomeFor}
              onInsert={onIllustrationInsert}
              onInserted={onIllustrationInserted}
            />
          ))}
        {!guest && !m.streaming && (m.abbreviationCandidates?.length ?? 0) > 0 && (
          <AbbreviationSuggestionCard
            candidates={m.abbreviationCandidates!}
            refreshKey={abbreviationRefreshKey}
          />
        )}
        {m.references && m.references.length > 0 && (
          <CompactReferencesList
            references={m.references}
            onSelectRef={(r, i) => onOpenDrawer(m.references || [], r, i)}
          />
        )}
        {!m.streaming && (m.assistantMessageId || m.content) && (
          <BotMessageActions
            content={m.content}
            createdAt={m.createdAt}
            canFork={!guest && sessionId !== "new" && Boolean(m.assistantMessageId)}
            onFork={!guest && m.assistantMessageId ? () => onFork(m) : undefined}
            onRegenerate={!busy ? () => onRegenerate(index) : undefined}
            feedback={
              sessionId !== "new" && m.assistantMessageId && m.isCompleted === true && !m.isError
                ? {
                    sessionId,
                    messageId: m.assistantMessageId,
                    initial: m.feedback,
                    initialComment: m.feedbackComment,
                  }
                : undefined
            }
          />
        )}
        {sessionId !== "new" && !compact && !guest && (
          <FollowUpSuggestions
            sessionId={sessionId}
            messageId={m.assistantMessageId ?? null}
            enabled={!m.streaming && m.isCompleted === true}
            ensure={m.suggestionEnsure === true}
            onAsk={onAsk}
          />
        )}
      </div>
    </div>
  );
});

export { AssistantMessage, DayDivider, UserMessageBubble };
