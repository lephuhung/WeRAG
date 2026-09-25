"use client";

import { memo, useCallback, useEffect, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { listMessages, stopSession, forkSession, createSession, getSession, type SessionRow, type ChatMessage } from "@/lib/api/chat";
import { streamChat, continueStream, type StreamChunk } from "@/lib/api/stream";
import { uploadTemporaryAttachment } from "@/lib/api/attachments";
import { ChatProvider, useChatContext } from "@/lib/chat-context";
import { useAuth } from "@/lib/auth";
import { Composer, type ComposerSend } from "@/components/composer";
import { useAttachments, formatFileSize } from "@/components/use-attachments";
import { useBrowserKnownOffline } from "@/components/use-browser-status";
import {
  combinedCapError,
  consumeFirstTurnHandoff,
  mergeAttachmentIds,
  resolveFirstTurnHandoff,
  rotateHandoffSessionState,
  takeHandoffTurn,
  type HandoffSessionState,
  type ResolvedFirstTurn,
} from "@/lib/first-turn-handoff";
import { createStreamGeneration, type StreamGeneration } from "@/lib/stream-generation";
import { shouldCommitDeferredSendTurn } from "@/lib/deferred-send-guard";
import { uploadImagesWithFallback } from "@/lib/image-upload-fallback";
import { FollowUpSuggestions } from "@/components/chat/follow-up-suggestions";
import { Markdown } from "@/components/markdown";
import { IconGlobe, IconCopy, IconCheck, IconFork, IconRefresh, IconEdit } from "@/components/icons";
import { renderFileIconSvg } from "@/components/files/file-icon";
import { ThinkingDisplay } from "@/components/chat/thinking-display";
import { PeopleCard, type PeopleRecord } from "@/components/chat/people-card";
import {
  applyChunkToSteps,
  finalizeSteps,
  stepsFromHistory,
  type AgentStepItem,
} from "@/components/chat/agent-steps";
import { AbbreviationSuggestionCard } from "@/components/chat/abbreviation-suggestion-card";
import { ReferencesDrawer, type KnowledgeReferenceItem } from "@/components/chat/references-drawer";
import { copyToClipboard } from "@/lib/clipboard";

type UiAttachment = { name: string; size?: number; title?: string; isImage?: boolean };

type UiMessage = {
  id: string;
  role: "user" | "assistant";
  content: string;
  thinking?: string;
  /** Ordered agent timeline (thinking rounds + tool calls), rendered by
   * ThinkingDisplay — mirrors the Vue smart-agent event stream. */
  steps?: AgentStepItem[];
  agentDurationMs?: number;
  streaming?: boolean;
  isError?: boolean;
  assistantMessageId?: string;
  references?: KnowledgeReferenceItem[];
  abbreviationCandidates?: string[];
  peopleData?: PeopleRecord[];
  attachments?: UiAttachment[];
};

function extractPeopleRecords(data: unknown): PeopleRecord[] {
  if (!data || typeof data !== "object") return [];
  const persons = (data as Record<string, unknown>).persons;
  if (!Array.isArray(persons)) return [];
  return persons.filter(
    (p): p is PeopleRecord => p !== null && typeof p === "object" && !Array.isArray(p),
  );
}

// continue-stream replays the event log — dedupe so replayed tool results
// don't stack duplicate person cards.
function mergePeopleRecords(
  existing: PeopleRecord[] | undefined,
  incoming: PeopleRecord[],
): PeopleRecord[] {
  const seen = new Set((existing ?? []).map((p) => JSON.stringify(p)));
  const out = [...(existing ?? [])];
  for (const p of incoming) {
    const k = JSON.stringify(p);
    if (seen.has(k)) continue;
    seen.add(k);
    out.push(p);
  }
  return out;
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

function peopleDataFromHistory(m: ChatMessage): PeopleRecord[] {
  const out: PeopleRecord[] = [];
  for (const step of m.agent_steps ?? []) {
    for (const call of step.tool_calls ?? []) {
      if (call.name !== "people_lookup") continue;
      out.push(...extractPeopleRecords(call.result?.data));
    }
  }
  return out;
}

function extractAbbreviationCandidates(data: unknown): string[] {
  if (!data || typeof data !== "object") return [];
  const obj = data as Record<string, unknown>;
  const sources: unknown[] = [
    (obj.result as Record<string, unknown> | undefined)?.potential_abbreviations,
    (obj.abbreviation_resolution as Record<string, unknown> | undefined)
      ?.potential_abbreviations,
    obj.potential_abbreviations,
  ];
  for (const src of sources) {
    if (Array.isArray(src)) {
      const out = src.filter((x): x is string => typeof x === "string");
      if (out.length > 0) return out;
    }
  }
  return [];
}

function mergeAbbreviationCandidates(
  existing: string[] | undefined,
  incoming: string[],
): string[] {
  if (incoming.length === 0) return existing ?? [];
  const seen = new Set((existing ?? []).map((c) => c.toLowerCase()));
  const out = [...(existing ?? [])];
  for (const c of incoming) {
    if (out.length >= 10) break;
    const key = c.trim().toLowerCase();
    if (!key || seen.has(key)) continue;
    seen.add(key);
    out.push(c.trim());
  }
  return out.slice(0, 10);
}

function abbreviationCandidatesFromHistory(m: ChatMessage): string[] {
  const out: string[] = [];
  for (const step of m.agent_steps ?? []) {
    for (const call of step.tool_calls ?? []) {
      out.push(
        ...extractAbbreviationCandidates(call.result?.data),
      );
    }
  }
  return mergeAbbreviationCandidates(undefined, out);
}

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

// User messages carry attachment metadata so the bubble can show which files
// were sent — both the optimistic row and history rows from `m.attachments`.
function attachmentsFromHistory(m: ChatMessage): UiAttachment[] | undefined {
  const files = (m.attachments ?? [])
    .filter((a) => a.file_name)
    .map((a) => ({ name: a.file_name!, size: a.file_size }));
  const images = (m.images ?? []).map((img, i) => ({
    name: `Ảnh ${i + 1}`,
    title: img.caption?.trim(),
    isImage: true,
  }));
  const all = [...files, ...images];
  return all.length > 0 ? all : undefined;
}

function fileToDataUri(file: File): Promise<string> {
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = reject;
    reader.readAsDataURL(file);
  });
}

/* Answer text streamed between tool rounds is a preamble that the next
 * tool_call retracts (folded into the thinking card title). Longer output is
 * the real final answer — that is what collapses the thinking panel. */
const PREAMBLE_MAX_CHARS = 240;

const UserMessageBubble = memo(function UserMessageBubble({
  message,
  onFork,
  onEdit,
}: {
  message: UiMessage;
  onFork?: (m: UiMessage) => void;
  onEdit?: (content: string) => void;
}) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    const ok = await copyToClipboard(message.content);
    if (ok) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <div className="group mb-6 flex flex-col items-end">
      {(message.attachments?.length ?? 0) > 0 && (
        <div className="mb-1.5 flex max-w-[80%] flex-wrap justify-end gap-1.5">
          {message.attachments!.map((a, i) => (
            <span
              key={`${a.name}-${i}`}
              title={a.title ?? (a.size ? `${a.name} · ${formatFileSize(a.size)}` : a.name)}
              className="flex items-center gap-2 rounded-lg border border-[#cfe1fd] bg-[#edf5ff] px-2.5 py-1.5 text-[12px] font-medium text-[#0f2d59] dark:border-[#223d63] dark:bg-[#15273f] dark:text-[#dce9fe]"
            >
              <span
                className="w-[22px] shrink-0"
                dangerouslySetInnerHTML={{
                  __html: renderFileIconSvg(a.isImage && !a.name.includes(".") ? "image.png" : a.name),
                }}
              />
              <span className="max-w-[180px] truncate">{a.name}</span>
            </span>
          ))}
        </div>
      )}
      <div className="max-w-[80%] rounded-[16px] border border-[#cfe1fd] bg-[#edf5ff] px-4 py-2.5 text-[14px] leading-relaxed text-[#0f2d59] shadow-2xs dark:border-[#223d63] dark:bg-[#15273f] dark:text-[#dce9fe] break-words whitespace-pre-wrap">
        {message.content}
      </div>
      <div className="mt-1 flex items-center gap-1 pr-1 text-muted-soft opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 sm:opacity-80">
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
            <IconCheck className="h-3.5 w-3.5 text-emerald-600 dark:text-emerald-400" />
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
    </div>
  );
});

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

  const handleCopy = async () => {
    const ok = await copyToClipboard(content);
    if (ok) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
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
      {displayed.map((r, i) => {
        const name = r.knowledge_title ?? r.knowledge_filename ?? r.knowledge_id ?? "Nguồn";
        const isWeb =
          r.chunk_type === "web_search" || Boolean(r.metadata?.url) || r.id?.startsWith("http");
        return (
          <button
            key={r.chunk_id ?? r.id ?? `${r.knowledge_id || "ref"}-${i}`}
            type="button"
            onClick={() => onSelectRef?.(r, i)}
            title={name}
            className="group flex max-w-[220px] items-center gap-1.5 rounded-full border border-emerald-200 bg-emerald-50/80 px-2.5 py-1 text-[12.5px] font-medium text-emerald-900 shadow-[0_1px_2px_rgba(0,0,0,0.06)] transition-all hover:border-emerald-300 hover:bg-emerald-100 hover:text-emerald-950 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-200 dark:hover:border-emerald-500/50 dark:hover:bg-emerald-500/20 cursor-pointer select-none text-left"
          >
            <span className="flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-emerald-600 text-[10px] font-semibold text-white transition-colors group-hover:bg-emerald-700 dark:bg-emerald-500 dark:text-emerald-950 dark:group-hover:bg-emerald-400">
              {i + 1}
            </span>
            {isWeb ? (
              <IconGlobe className="h-3 w-3 shrink-0 text-emerald-600 transition-colors dark:text-emerald-400" />
            ) : (
              <span
                className="w-[12px] shrink-0 self-center"
                dangerouslySetInnerHTML={{
                  __html: renderFileIconSvg(r.knowledge_filename ?? r.knowledge_title ?? name),
                }}
              />
            )}
            <span className="truncate">{name}</span>
          </button>
        );
      })}

      {hasMore && (
        <button
          type="button"
          onClick={() => setShowAll((v) => !v)}
          className="flex items-center gap-1 rounded-full border border-emerald-200 bg-emerald-50/80 px-2.5 py-1 text-[12.5px] font-medium text-emerald-900 shadow-[0_1px_2px_rgba(0,0,0,0.06)] transition-all hover:border-emerald-300 hover:bg-emerald-100 hover:text-emerald-950 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-200 dark:hover:border-emerald-500/50 dark:hover:bg-emerald-500/20 select-none cursor-pointer"
          title={showAll ? "Thu gọn bớt nguồn" : `Xem thêm ${remaining} nguồn khác`}
        >
          <span>{showAll ? "Thu gọn" : `+${remaining}`}</span>
        </button>
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
  busy,
  abbreviationRefreshKey,
  onOpenDrawer,
  onFork,
  onRegenerate,
  onAsk,
}: {
  m: UiMessage;
  index: number;
  sessionId: string;
  busy: boolean;
  abbreviationRefreshKey: number;
  onOpenDrawer: (refs: KnowledgeReferenceItem[], activeItem?: KnowledgeReferenceItem, index?: number) => void;
  onFork: (m: UiMessage) => void;
  onRegenerate: (index: number) => void;
  onAsk: (text: string, attribution: { setId: string; questionId: string }, kbIds: string[]) => void;
}) {
  const hasPeopleCard = (m.peopleData?.length ?? 0) > 0;
  const shownContent = stripPeopleDump(m.content, hasPeopleCard);
  return (
    <div className="mb-6 flex gap-3 sm:gap-4">
      <div className="display-sm mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-surface-strong text-[14px]">
        W
      </div>
      <div className="w-full min-w-0 flex-1 pt-1.5">
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
            onViewReferences={() => onOpenDrawer(m.references || [])}
          />
        )}
        {shownContent ? (
          <div className={m.isError ? "rounded-lg border border-red-500/20 bg-red-500/5 p-3 text-red-500 dark:text-red-400" : "[&_.chat-markdown]:text-ink"}>
            <Markdown text={shownContent} streaming={m.streaming} />
          </div>
        ) : (
          <p className="text-[14px] leading-relaxed text-body">{m.streaming && !m.thinking && (!m.steps || m.steps.length === 0) ? "…" : ""}</p>
        )}
        {hasPeopleCard && (
          <PeopleCard people={m.peopleData!} isLoadingMore={m.streaming} />
        )}
        {!m.streaming && (m.abbreviationCandidates?.length ?? 0) > 0 && (
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
            canFork={sessionId !== "new" && Boolean(m.assistantMessageId)}
            onFork={m.assistantMessageId ? () => onFork(m) : undefined}
            onRegenerate={!busy ? () => onRegenerate(index) : undefined}
          />
        )}
        {sessionId !== "new" && (
          <FollowUpSuggestions
            sessionId={sessionId}
            messageId={m.assistantMessageId ?? null}
            enabled={!m.streaming}
            onAsk={onAsk}
          />
        )}
      </div>
    </div>
  );
});

function ChatBody({ id }: { id: string }) {
  const searchParams = useSearchParams();
  const initialQ = searchParams.get("q");
  // Session-keyed one-shot first-turn handoff from create-chat (?q / ?qokb /
  // ?qok plus the fh flag): the attachment IDs pre-uploaded against the fresh
  // session ride a one-shot same-tab sessionStorage record keyed by session
  // id — the URL never carries IDs or names. Consumed once per session id
  // during render (before the auto-send effect strips the query string) and
  // cached on a ref so StrictMode double-renders cannot consume it twice.
  // Keyed by session ID because this component may be reused for a new `id`
  // while old flags linger — a new session resolves its own handoff and
  // nothing leaks into later turns. When the flag is set but the record is
  // gone (reload / other tab), resolved.handoffError is set and the
  // auto-send effect keeps the draft instead of sending a file-less turn.
  const handoffRef = useRef<HandoffSessionState>({
    sessionId: "",
    attachmentIds: [],
    attachmentNames: [],
    consumed: false,
  });
  const handoffCacheRef = useRef<{ sessionId: string; resolved: ResolvedFirstTurn } | null>(null);
  if (handoffCacheRef.current?.sessionId !== id) {
    let stored: { attachmentIds: string[]; attachmentNames: string[] } | null = null;
    try {
      stored =
        typeof window !== "undefined" && window.sessionStorage
          ? consumeFirstTurnHandoff(window.sessionStorage, id)
          : null;
    } catch {
      stored = null;
    }
    handoffCacheRef.current = { sessionId: id, resolved: resolveFirstTurnHandoff(searchParams, stored) };
  }
  handoffRef.current = rotateHandoffSessionState(
    handoffRef.current,
    id,
    handoffCacheRef.current.resolved,
  );
  const ctx = useChatContext();
  const { user } = useAuth();
  // Non-admin callers never send a model override; the server resolves the
  // model assigned to the selected response mode.
  const canPickModel = user?.is_system_admin === true;

  const [session, setSession] = useState<SessionRow | null>(null);
  const [sessionTitle, setSessionTitle] = useState<string | null>(null);
  const [messages, setMessages] = useState<UiMessage[]>([]);
  const [abbreviationRefreshKey, setAbbreviationRefreshKey] = useState(0);
  const [input, setInput] = useState("");
  const [images, setImages] = useState<Array<{ preview: string; file: File }>>([]);
  const imageInputRef = useRef<HTMLInputElement>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [assistantMessageId, setAssistantMessageId] = useState<string | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const stickBottomRef = useRef(true);
  const sentInitial = useRef(false);
  const abortRef = useRef<AbortController | null>(null);
  // Generation guard for the streaming race: when the route id changes while
  // session A still streams, the session-change effect below bumps the
  // counter (and aborts A's controller) so A's late onChunk / then / catch /
  // finally callbacks — which may already be queued — skip every state write
  // instead of mutating new session B state (e.g. clearing B's busy flag).
  // Each send() also bumps on start, so a resend after stop() invalidates
  // the stopped stream's pending finalizer. idRef tracks the live route id
  // for the originating-id half of the guard.
  const generationsRef = useRef<StreamGeneration | null>(null);
  if (!generationsRef.current) generationsRef.current = createStreamGeneration();
  const idRef = useRef(id);
  idRef.current = id;
  // Pending follow-up attribution: set when a suggestion chip is clicked,
  // consumed (and cleared) by the very next send — mirrors the Vue
  // pendingSuggestionAttribution / pendingSuggestionKnowledgeBaseIds pair.
  const pendingAttribution = useRef<{ setId: string; questionId: string } | null>(null);
  const pendingKbIds = useRef<string[]>([]);
  const router = useRouter();
  const attachments = useAttachments(id === "new" ? undefined : id);
  // index.vue:1395 gates local_browser_enabled on !knownOffline so an offline
  // extension never claims browser sources.
  const browserKnownOffline = useBrowserKnownOffline();
  // Continue-stream scratch refs (reset per attach attempt).
  const accRef = useRef("");
  const stepsRef = useRef<AgentStepItem[]>([]);
  const srcSetRef = useRef<StreamChunk["knowledge_references"] | null>(null);

  // References slide-out panel (drawer)
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [drawerRefs, setDrawerRefs] = useState<KnowledgeReferenceItem[]>([]);
  const [activeRefKey, setActiveRefKey] = useState<string | null>(null);

  const handleOpenDrawer = useCallback((refs: KnowledgeReferenceItem[], activeItem?: KnowledgeReferenceItem, index = 0) => {
    setDrawerRefs(refs);
    if (activeItem) {
      setActiveRefKey(activeItem.chunk_id || activeItem.id || `${activeItem.knowledge_id || "ref"}-${index}`);
    } else {
      setActiveRefKey(null);
    }
    setDrawerOpen(true);
  }, []);

  // History and session details:
  // Mirrors Vue loadSessionAndHydrate: fetch session details to populate title
  // and hydrate input state (agent, model, KBs) from last_request_state.
  useEffect(() => {
    // A new route id invalidates every in-flight callback from the previous
    // session: abort its controller AND bump the generation, so already-
    // queued late chunks/finalizers from A skip their writes instead of
    // mutating B state. The reset below then starts B from a clean slate.
    abortRef.current?.abort();
    abortRef.current = null;
    const generations = generationsRef.current!;
    const gen = generations.next();
    const originId = id;
    const isLive = () => generations.isCurrent(gen) && idRef.current === originId;
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
              steps:
                m.role === "assistant"
                  ? (() => {
                      const s = stepsFromHistory(m);
                      return s.length ? s : undefined;
                    })()
                  : undefined,
              agentDurationMs: m.agent_duration_ms || undefined,
              references: refs?.length ? refs : undefined,
              assistantMessageId: m.role === "assistant" ? (m.id ?? undefined) : undefined,
              abbreviationCandidates:
                m.role === "assistant"
                  ? abbreviationCandidatesFromHistory(m)
                  : undefined,
              peopleData:
                m.role === "assistant" ? peopleDataFromHistory(m) : undefined,
              attachments:
                m.role === "user" ? attachmentsFromHistory(m) : undefined,
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
        // Resume-branch only: history merging above stays under `alive` (as
        // before) so a send issued while history loads never drops rows —
        // but attaching a continue-stream to a stale session must not run.
        if (alive && isLive() && last && last.role !== "user" && last.is_completed === false && last.id) {
          setBusy(true);
          const inflightId = last.id;
          const applyChunk = (c: StreamChunk) => {
            // Stale session/generation: A's late chunks must not touch B.
            if (!alive || !isLive()) return;
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
            if (kind === "tool_result") {
              const candidates = extractAbbreviationCandidates(c.data);
              if (candidates.length > 0) {
                setMessages((m) =>
                  m.map((msg) =>
                    msg.assistantMessageId === inflightId
                      ? {
                          ...msg,
                          abbreviationCandidates: mergeAbbreviationCandidates(
                            msg.abbreviationCandidates,
                            candidates,
                          ),
                        }
                      : msg,
                  ),
                );
              }
            }
            if (kind === "references" && c.knowledge_references?.length) {
              const refs = c.knowledge_references;
              srcSetRef.current = refs;
              return;
            }
            const toolScopedError =
              kind === "error" &&
              Boolean(
                c.tool_call_id || c.tool_name || c.data?.tool_call_id || c.data?.tool_name,
              );
            if (
              kind === "thinking" ||
              kind === "tool_call" ||
              kind === "tool_result" ||
              kind === "context_compacted" ||
              kind === "command_output" ||
              toolScopedError
            ) {
              applyChunkToSteps(stepsRef.current, c);
              const snap = [...stepsRef.current];
              setMessages((m) =>
                m.map((msg) =>
                  msg.assistantMessageId === inflightId ? { ...msg, steps: snap } : msg,
                ),
              );
              return;
            }
            if (kind === "complete" || kind === "stop" || kind === "agent_complete") {
              finalizeSteps(stepsRef.current);
              const snap = [...stepsRef.current];
              const dur = Number(c.data?.total_duration_ms) || 0;
              setMessages((m) =>
                m.map((msg) =>
                  msg.assistantMessageId === inflightId
                    ? { ...msg, steps: snap, agentDurationMs: dur || msg.agentDurationMs }
                    : msg,
                ),
              );
              return;
            }
            if (kind === "error") {
              const errorText = c.content || (c.data?.error as string) || "Stream failed";
              setMessages((m) =>
                m.map((msg) =>
                  msg.assistantMessageId === inflightId
                    ? {
                        ...msg,
                        content: msg.content || `⚠️ ${errorText}`,
                        isError: true,
                        streaming: false,
                      }
                    : msg,
                ),
              );
              // Replays of turns that died before the backend emitted a
              // `complete` event never end server-side — abort so the
              // .finally below clears `busy` instead of locking the chat.
              ctrl.abort();
              return;
            }
            // Same gate as the send path: only answer events carry
            // user-facing text — everything else has its own channel.
            if (kind && kind !== "answer") return;
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
          stepsRef.current = stepsFromHistory(last);
          setMessages((m) =>
            m.map((msg) =>
              msg.assistantMessageId === inflightId ? { ...msg, streaming: true } : msg,
            ),
          );
          const ctrl = new AbortController();
          abortRef.current = ctrl;
          continueStream({ sessionId: id, messageId: inflightId, signal: ctrl.signal, onChunk: applyChunk })
            .catch(() => {
              /* the turn may simply be finished server-side — history next
               * open re-reads it complete */
            })
            .finally(() => {
              // A stale finalizer must not clear the new session's busy
              // flag or settle its rows.
              if (!alive || !isLive()) return;
              setAbbreviationRefreshKey((v) => v + 1);
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
    // A new send invalidates any previous stream's pending callbacks (e.g.
    // a resend right after stop()): they capture `gen` and skip their late
    // writes once stale, so only the live turn touches state.
    const gen = generationsRef.current!.next();
    const originId = idRef.current;
    // Deferred-send liveness gate (same pure helper as
    // lib/deferred-send-guard.ts): every await below re-checks it before
    // any state write, so a turn whose upload is still pending when the
    // route moves A→B (or a resend bumps the generation) drops its commit
    // phase instead of appending A's bubble / overriding B's controller.
    const isLive = () =>
      shouldCommitDeferredSendTurn(generationsRef.current!, gen, idRef.current, originId);
    setBusy(true);

    // Combined file+image cap BEFORE creating a session or uploading, so an
    // over-cap send never leaves an orphan session/uploads or a server-
    // rejected stream. Input and selection stay intact for retry.
    const handoffPendingCount = !handoffRef.current.consumed
      ? handoffRef.current.attachmentIds.length
      : 0;
    const capErr = combinedCapError(
      s.attachments.length + s.imageFiles.length + handoffPendingCount,
    );
    if (capErr) {
      setError(capErr);
      setBusy(false);
      return;
    }

    let activeSessionId = id;
    if (activeSessionId === "new") {
      try {
        const res = await createSession({});
        // Route may have moved while createSession was pending: a stale A
        // turn must not reroute B's URL, dispatch session-created for B,
        // or continue into the upload/stream path — drop out silently.
        if (!isLive()) return;
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
        // Stale upload/session errors never surface into the new session.
        if (!isLive()) return;
        setError(e instanceof Error ? e.message : "Failed to create session");
        setBusy(false);
        return;
      }
    }

    const asstId = `a${Date.now()}`;

    // Web images upload as temporary documents (VLM reads them in background);
    // inline base64 is the per-image fallback for the files whose upload
    // actually failed — tracked by identity, never by success count.
    const { attachmentIds: imageAttachmentIds, inlineImages: fallbackInlineImages } =
      await uploadImagesWithFallback(
        s.imageFiles,
        async (file) => {
          const up = await uploadTemporaryAttachment(
            activeSessionId,
            file,
            ctx.settings.selectedAgentId || undefined,
            ctx.settings.selectedAgentSourceTenantId ?? undefined,
            "auto",
          );
          return up.data.id;
        },
        (file) => fileToDataUri(file),
      );
    // Navigating A→B while the image upload was pending leaves this turn
    // stale: never append A's bubble or start A's stream on B.
    if (!isLive()) return;

    // Local files picked before the session existed (create-chat page) upload now.
    // Their IDs are collected synchronously for THIS request: patching React
    // state is async, so reading them back from s.attachments (which lacks
    // documentId) would silently drop them from the turn.
    const localOnes = s.attachments.filter((a) => !a.documentId);
    let localUploadedIds: string[] = [];
    if (localOnes.length > 0) {
      try {
        localUploadedIds = await Promise.all(
          localOnes.map(async (a) => {
            const up = await uploadTemporaryAttachment(
              activeSessionId,
              a.file,
              ctx.settings.selectedAgentId || undefined,
              ctx.settings.selectedAgentSourceTenantId ?? undefined,
              "auto",
            );
            // The per-file callback runs after its own await: a stale A
            // turn must not patch attachment state now owned by B.
            if (!isLive()) return up.data.id;
            attachments.setItems((prev) => prev.map((x) => (x.localId === a.localId ? { ...x, documentId: up.data.id, status: up.data.status } : x)));
            return up.data.id;
          }),
        );
        // Route may have moved while the local uploads were pending.
        if (!isLive()) return;
      } catch (e) {
        // Stale upload errors never surface into the new session.
        if (!isLive()) return;
        setError(e instanceof Error ? e.message : "Attachment upload failed");
        setBusy(false);
        return;
      }
    }

    // Final commit gate: every await above has resolved. If the route moved
    // A→B (or a resend bumped the generation) while uploads were pending,
    // drop the stale A turn here — before consuming the one-shot handoff,
    // before appending the optimistic bubble, and before installing the
    // abort controller / launching the stream — so B is never touched.
    if (!isLive()) return;
    // One-shot first-turn handoff: the first send of this session rides the
    // pre-uploaded IDs (+ display names) and the retrieval hint; later sends
    // get empties so stale hints/IDs never re-attach (creatChat.vue clears
    // firstQuestionOrigin in the first sendMsg).
    const firstSend = !handoffRef.current.consumed;
    const handoffTake = takeHandoffTurn(handoffRef.current);
    handoffRef.current = handoffTake.next;
    const handoffIds = firstSend ? handoffTake.attachmentIds : [];
    const handoffNames = firstSend ? handoffTake.attachmentNames : [];

    // Echo which files were sent on the user bubble — the question alone is
    // meaningless when the content lives in an attached document. First-turn
    // handoff files arrive as IDs only, so their one-shot display names fill
    // the bubble instead of rendering it empty.
    const sentAttachments: UiAttachment[] = [
      ...s.attachments.map((a) => ({ name: a.name, size: a.size })),
      ...s.imageFiles.map((f) => ({ name: f.name, size: f.size, isImage: true })),
      ...handoffNames.map((name) => ({ name })),
    ];
    stickBottomRef.current = true;
    setMessages((m) => [...m, { id: `u${Date.now()}`, role: "user", content: t, attachments: sentAttachments.length > 0 ? sentAttachments : undefined }, { id: asstId, role: "assistant", content: "", streaming: true }]);
    setInput("");
    setImages([]);

    // Covered by the commit gate above (no await intervenes, so liveness
    // cannot change here): reaching this point means the turn is live, and
    // installing the abort controller / launching the stream is safe.
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    let acc = "";
    let thinkingAcc = "";
    let stepsList: AgentStepItem[] = [];
    // Length of `acc` at the last tool_call boundary. Text appended after that
    // point is the round preamble — Vue retracts it from the answer area and
    // folds it into the thinking card as its title once the tool call lands.
    let answerCursor = 0;

    // Mirror useChatStreamHandler.processStreamChunk:
    // handle thinking, tool execution, answer content and references.
    // Every branch below writes state for THIS turn only — when the route
    // changed (or a newer send started) the generation is stale and the
    // chunk is dropped so session A's tail never mutates session B.
    const applyChunk = (c: StreamChunk) => {
      if (!isLive()) return;
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
      // A tool-scoped error belongs to a single step — the turn continues;
      // only an unattributed error is fatal (handled further down).
      const toolScopedError =
        kind === "error" &&
        Boolean(c.tool_call_id || c.tool_name || c.data?.tool_call_id || c.data?.tool_name);

      if (kind === "error" && !toolScopedError) {
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
        // A backend error event is terminal for the turn, but older backends
        // keep the SSE socket open after it — without aborting, streamChat
        // never settles and `busy` stays locked, silently swallowing every
        // subsequent send.
        ctrl.abort();
        return;
      }

      // 1. Thinking / Reasoning chunks — feed both the rolling text stream
      // (thinkingAcc) and the structured step timeline (stepsList).
      const thoughtText =
        c.reasoning_content ??
        c.thought ??
        (c.data?.thought as string | undefined) ??
        (kind === "thinking" ? c.content : undefined);
      if (kind === "thinking") {
        thinkingAcc += thoughtText ?? c.content ?? "";
        applyChunkToSteps(stepsList, c);
        const snapThinking = thinkingAcc;
        const snapSteps = [...stepsList];
        setMessages((m) =>
          m.map((msg) =>
            matchAssistant(msg)
              ? {
                  ...msg,
                  thinking: snapThinking,
                  steps: snapSteps,
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

      // 2. Tool call events — fold the retracted preamble into the round's
      // thinking card (Vue superseded-answer behavior), then record the call.
      if (kind === "tool_call") {
        if (acc.length > answerCursor) {
          const preamble = acc.slice(answerCursor).trim();
          acc = acc.slice(0, answerCursor);
          if (preamble) {
            const lastThink = [...stepsList]
              .reverse()
              .find(
                (s): s is Extract<AgentStepItem, { type: "thinking" }> =>
                  s.type === "thinking",
              );
            if (lastThink && !lastThink.title) {
              lastThink.title = preamble;
            } else {
              stepsList.push({
                type: "thinking",
                id: `preamble-${stepsList.length}`,
                title: preamble,
                content: "",
                done: true,
              });
            }
          }
          const parsed = parseThinkAndContent(acc, thinkingAcc);
          const content = parsed.content;
          setMessages((m) =>
            m.map((msg) =>
              matchAssistant(msg) ? { ...msg, content } : msg,
            ),
          );
        }
        answerCursor = acc.length;
        applyChunkToSteps(stepsList, c);
        const snapSteps = [...stepsList];
        setMessages((m) =>
          m.map((msg) =>
            matchAssistant(msg)
              ? {
                  ...msg,
                  steps: snapSteps,
                  assistantMessageId: incomingAsstId ?? msg.assistantMessageId,
                }
              : msg,
          ),
        );
        return;
      }

      // 3. Tool result events (and tool-scoped errors / live command output /
      // compaction markers) — update the matching step; abbreviation/people
      // extraction stays as before.
      if (
        kind === "tool_result" ||
        toolScopedError ||
        kind === "context_compacted" ||
        kind === "command_output"
      ) {
        const toolName = c.tool_name || (c.data?.tool_name as string) || "";
        const abbrCandidates = extractAbbreviationCandidates(c.data);
        const peopleRecs =
          toolName === "people_lookup" ? extractPeopleRecords(c.data) : [];
        if (peopleRecs.length > 0) {
          setMessages((m) =>
            m.map((msg) =>
              matchAssistant(msg)
                ? { ...msg, peopleData: mergePeopleRecords(msg.peopleData, peopleRecs) }
                : msg,
            ),
          );
        }
        if (abbrCandidates.length > 0) {
          setMessages((m) =>
            m.map((msg) =>
              matchAssistant(msg)
                ? {
                    ...msg,
                    abbreviationCandidates: mergeAbbreviationCandidates(
                      msg.abbreviationCandidates,
                      abbrCandidates,
                    ),
                  }
                : msg,
            ),
          );
        }
        applyChunkToSteps(stepsList, c);
        const snapSteps = [...stepsList];
        setMessages((m) =>
          m.map((msg) =>
            matchAssistant(msg)
              ? {
                  ...msg,
                  steps: snapSteps,
                  assistantMessageId: incomingAsstId ?? msg.assistantMessageId,
                }
              : msg,
          ),
        );
        return;
      }

      // 4. Complete event — settle pending steps and record the turn duration.
      if (kind === "complete" || kind === "stop" || kind === "agent_complete") {
        finalizeSteps(stepsList);
        const snapSteps = [...stepsList];
        const durationMs = Number(c.data?.total_duration_ms) || 0;
        setMessages((m) =>
          m.map((msg) =>
            matchAssistant(msg)
              ? {
                  ...msg,
                  streaming: false,
                  steps: snapSteps,
                  agentDurationMs: durationMs || msg.agentDurationMs,
                  assistantMessageId: incomingAsstId ?? msg.assistantMessageId,
                }
              : msg,
          ),
        );
        return;
      }

      // 5. Regular answer content — only `answer` events carry user-facing
      // text. Other kinds (reflection, command_output, memory_recalled,
      // approval/oauth markers…) have their own channels; letting them fall
      // through would leak their payloads into the message (Vue ignores them).
      if (kind && kind !== "answer") return;
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
    const inlineImages = fallbackInlineImages;

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
    const attachmentIds = mergeAttachmentIds(
      s.attachments.map((a) => a.documentId).filter((x): x is string => Boolean(x)),
      localUploadedIds,
      imageAttachmentIds,
      handoffIds,
    );

    attachments.clear();
    // Follow-up attribution anchors this single turn to the clicked
    // suggestion (recorded pre-click) and keeps KB-backed retrieval scoped
    // to the suggestion's KBs; model-backed suggestions widen nothing.
    const attribution = pendingAttribution.current;
    pendingAttribution.current = null;
    const kbIdsOverride = pendingKbIds.current;
    pendingKbIds.current = [];
    // Explicit per-send hint wins; otherwise the one-shot first-turn hint
    // from the handoff state (already consumed above — later sends resolve
    // to undefined so a stale hint never re-attaches).
    const questionOrigin = s.questionOrigin ?? (firstSend ? handoffTake.questionOrigin : undefined);
    const previousMessage = messages[messages.length - 1];
    const priorCandidates =
      previousMessage?.role === "assistant"
        ? previousMessage.abbreviationCandidates
        : undefined;
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
      // Same gate as the Vue sender: knowledge-chat rejects
      // local_browser_enabled outright, so it only rides agent turns.
      // Plus the knownOffline gate (index.vue:1395): never claim browser
      // sources when the extension is offline.
      localBrowserEnabled: isAgentMode && ctx.settings.localBrowserEnabled && !browserKnownOffline,
      summaryModelId: s.modelId || ctx.settings.selectedChatModelId || undefined,
      suggestionAttribution: attribution
        ? { suggestion_set_id: attribution.setId, question_id: attribution.questionId }
        : undefined,
      // index.vue sends options?.questionOrigin the same way.
      questionOrigin,
      attachmentIds: attachmentIds.length > 0 ? attachmentIds : undefined,
      images: inlineImages,
      abbreviationCandidates: priorCandidates?.slice(0, 10),
      signal: ctrl.signal,
      onChunk: applyChunk,
    })
      .catch((e: unknown) => {
        // Stale turns never surface errors into the new session.
        if (!isLive()) return;
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
        // The core of the A→B race fix: a stale finalizer (old session A
        // completing while B streams) must NOT clear B's busy flag, settle
        // B's rows, or reroute — it simply drops out.
        if (!isLive()) return;
        setAbbreviationRefreshKey((v) => v + 1);
        setBusy(false);
        setAssistantMessageId(null);
        // Streams that end without a `complete` event (abort, socket drop)
        // would leave tool rows shimmering forever — settle them.
        finalizeSteps(stepsList);
        const snapSteps = [...stepsList];
        setMessages((m) =>
          m.map((msg) =>
            msg.id === asstId
              ? { ...msg, streaming: false, steps: snapSteps.length ? snapSteps : msg.steps }
              : msg,
          ),
        );
        if (id === "new" && activeSessionId !== "new") {
          router.replace(`/platform/chat/${activeSessionId}`);
        }
      });
  };

  // Latest-value refs + stable callbacks: every streamed chunk re-renders
  // ChatBody, so row components only stay memoized if their props keep
  // referential identity across renders.
  const sendRef = useRef(send);
  sendRef.current = send;
  const messagesRef = useRef(messages);
  messagesRef.current = messages;

  const handleEditQuestion = useCallback((content: string) => setInput(content), []);

  const handleFork = useCallback(
    (m: UiMessage) => {
      // User bubbles fork by row id; assistant turns fork by assistant_message_id.
      void forkSession(id, { message_id: m.assistantMessageId ?? m.id })
        .then((res) => {
          const newId = (res as { data?: { session?: { id?: string }; id?: string } }).data;
          const sessionId =
            newId && typeof newId === "object" && "session" in newId
              ? newId.session?.id
              : (newId as { id?: string } | undefined)?.id;
          if (sessionId) void router.push(`/platform/chat/${sessionId}`);
        })
        .catch(() => undefined);
    },
    [id, router],
  );

  const handleRegenerate = useCallback((index: number) => {
    const prevUser = messagesRef.current
      .slice(0, index)
      .reverse()
      .find((msg) => msg.role === "user");
    if (prevUser) {
      void sendRef.current({
        query: prevUser.content,
        attachments: [],
        imageFiles: [],
        mentionedItems: [],
        modelId: "",
      });
    }
  }, []);

  const handleAskFollowUp = useCallback(
    (text: string, attribution: { setId: string; questionId: string }, kbIds: string[]) => {
      pendingAttribution.current = attribution;
      pendingKbIds.current = kbIds;
      setInput(text);
      void sendRef.current({
        query: text,
        attachments: [],
        imageFiles: [],
        mentionedItems: [],
        modelId: "",
      });
    },
    [],
  );

  // create-chat ?q=… auto-send, like creatChat.vue navigateToSession(firstQuery).
  // Waits for ctx.hydrated: this effect runs before ChatProvider's settings
  // hydration, and firing early would send DEFAULTS — web_search_enabled=false,
  // no KB scope — for the user's very first question.
  useEffect(() => {
    if (initialQ && !sentInitial.current && ctx.hydrated) {
      sentInitial.current = true;
      // Flagged file handoff but the one-shot record is gone (reload, other
      // tab, expired storage): DO NOT auto-send the bare question without
      // its files — surface an actionable error and keep the question as a
      // draft for the user to re-attach and resend.
      const resolved = handoffCacheRef.current?.sessionId === id ? handoffCacheRef.current.resolved : null;
      if (typeof window !== "undefined") {
        window.history.replaceState(null, "", `/platform/chat/${id}`);
      }
      if (resolved?.handoffError) {
        setInput(initialQ);
        setError(resolved.handoffError);
        return;
      }
      void send({ query: initialQ, modelId: ctx.settings.selectedChatModelId, mentionedItems: [], imageFiles: [], attachments: [] });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialQ, id, ctx.hydrated]);

  // Follow the stream only while the user is parked near the bottom — scrolling
  // up releases the lock. Setting scrollTop directly (not smooth scrollIntoView):
  // restarting a smooth animation per streamed token is what made this stutter.
  useEffect(() => {
    const el = scrollRef.current;
    if (el && stickBottomRef.current) el.scrollTop = el.scrollHeight;
  }, [messages]);

  const handleMessagesScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    stickBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 96;
  };

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
      <div className="hairline-b flex h-14 shrink-0 items-center px-4 sm:px-8">
        <h1 className="truncate text-[15px] font-medium text-ink">{title}</h1>
      </div>

      <div ref={scrollRef} onScroll={handleMessagesScroll} className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-[1040px] px-4 py-5 sm:px-6 sm:py-8">
          {messages.map((m, index) =>
            m.role === "user" ? (
              <UserMessageBubble
                key={m.id}
                message={m}
                onEdit={handleEditQuestion}
                onFork={id !== "new" ? handleFork : undefined}
              />
            ) : (
              <AssistantMessage
                key={m.id}
                m={m}
                index={index}
                sessionId={id}
                busy={busy}
                abbreviationRefreshKey={abbreviationRefreshKey}
                onOpenDrawer={handleOpenDrawer}
                onFork={handleFork}
                onRegenerate={handleRegenerate}
                onAsk={handleAskFollowUp}
              />
            ),
          )}
          {error && <p className="body-sm mb-4 text-error">{error}</p>}
        </div>
      </div>

      <div className="shrink-0 px-3 pb-3 pt-2 sm:px-6 sm:pb-6">
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
