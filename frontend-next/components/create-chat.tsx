"use client";

import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { apiPost } from "@/lib/api-client";
import { getSuggestedQuestions, type SuggestedQuestion } from "@/lib/api/extra";
import { BUILTIN_DOCUMENT_ASSISTANT_ID, ChatProvider, useChatContext } from "@/lib/chat-context";
import { Orb } from "@/components/orb";
import { Composer, type ComposerSend } from "@/components/composer";
import { useAttachments } from "@/components/use-attachments";
import { deleteTemporaryAttachment, uploadTemporaryAttachment } from "@/lib/api/attachments";
import { deleteSession } from "@/lib/api/chat";
import {
  buildFirstTurnSearch,
  buildSuggestionSend,
  cleanupSessionBestEffort,
  combinedCapError,
  saveFirstTurnHandoff,
  uploadFirstTurnFiles,
} from "@/lib/first-turn-handoff";
import { questionOriginFromSuggestion } from "@/lib/question-origin";
import { useT } from "@/lib/i18n";
import { SplitPane } from "@/components/doc-workspace/split-pane";
import { DocWorkspace } from "@/components/doc-workspace/doc-workspace";
import { GuestChatNote } from "@/components/chat/guest-chat-note";
import { LegalLookupBox } from "@/components/legal-lookup/legal-lookup-box";
import { chatErrorKey, chatErrorTextKey } from "@/lib/api/chat-errors";
import { shouldRefreshAccount } from "@/lib/guest-chat";
import { useAuth } from "@/lib/auth";
import { userErrorMessage } from "@/lib/user-error";

/* Suggested questions are fetched once per page load and shared: the
 * endpoint samples a random set on every call, so refetching on each visit
 * (or each render) swapped the list under the reader's eyes. The cache is
 * keyed by user + organization — sign-out/in and organization switches do
 * not reload the page, and another organization's questions must not show. */
let suggestedCache: { key: string; questions: SuggestedQuestion[] } | null = null;
let suggestedInFlight: { key: string; promise: Promise<SuggestedQuestion[]> } | null = null;
const SUGGESTION_SKELETON_WIDTHS = ["72%", "58%", "80%"];

function cachedSuggestions(key: string): SuggestedQuestion[] | null {
  return suggestedCache?.key === key ? suggestedCache.questions : null;
}

function loadSuggestedOnce(key: string): Promise<SuggestedQuestion[]> {
  const cached = cachedSuggestions(key);
  if (cached) return Promise.resolve(cached);
  if (suggestedInFlight?.key === key) return suggestedInFlight.promise;
  const promise = getSuggestedQuestions()
    .then((questions) => {
      suggestedCache = { key, questions };
      return questions;
    })
    // a failure is not cached: the next visit tries again
    .catch(() => [] as SuggestedQuestion[])
    .finally(() => {
      if (suggestedInFlight?.promise === promise) suggestedInFlight = null;
    });
  suggestedInFlight = { key, promise };
  return promise;
}

function CreateChatBody({ kbId }: { kbId?: string }) {
  const router = useRouter();
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  // The document pane is opening a picked file (it creates the session):
  // the composer must not create a second one meanwhile.
  const [docPaneBusy, setDocPaneBusyState] = useState(false);
  const docPaneBusyRef = useRef(false);
  const setDocPaneBusy = (v: boolean) => {
    docPaneBusyRef.current = v;
    setDocPaneBusyState(v);
  };
  const [error, setError] = useState<string | null>(null);
  const { refreshMe, user, selectedTenantId } = useAuth();
  const userId = user?.id;
  const suggestionKey = `${userId ?? ""}:${selectedTenantId ?? ""}`;
  const [suggested, setSuggested] = useState<SuggestedQuestion[] | null>(() => cachedSuggestions(suggestionKey));
  const [images, setImages] = useState<Array<{ preview: string; file: File }>>([]);
  const imageInputRef = useRef<HTMLInputElement>(null);
  const attachments = useAttachments(undefined);
  const ctx = useChatContext();
  const { t } = useT();

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

  /* KB variant: the chat page reads its KB scope from the chat context, so
   * the picked knowledge base joins the scope of this conversation and of
   * the next one — through the context, not by writing localStorage behind
   * its back (that would race the sticky scope and ignore a removal). */
  const persistKbScope = () => {
    if (!kbId) return;
    ctx.addKnowledgeBase(kbId);
  };

  // Session create takes title/description only (CreateSessionRequest); the
  // KB scope + first question travel via ?q=… + chat context, like the Vue flow.
  // A picked suggestion's retrieval hint rides as qokb/qok (creatChat.vue
  // firstQuestionOrigin) so the first stream keeps question_origin.
  const send = async (s: ComposerSend) => {
    const q = s.query.trim();
    if (!q || busy || docPaneBusyRef.current) return;
    setBusy(true);
    setError(null);
    try {
      // Combined file+image cap BEFORE creating a session or uploading, so
      // an over-cap send never leaves an orphan session/uploads behind.
      // The selection stays intact for retry (nothing is cleared here).
      const capErr = combinedCapError(s.attachments.length + s.imageFiles.length);
      if (capErr) {
        setError(capErr);
        return;
      }
      const res = await apiPost<{ success: boolean; data?: { id: string }; message?: string }>(
        `/api/v1/sessions`,
        {},
      );
      const sessionId = res.data?.id;
      if (!sessionId) throw new Error(res.message ?? t("ui.err.createSession"));
      // Upload the selected files/images against the fresh session BEFORE
      // navigating, then stage only the attachment IDs (opaque server
      // handles — never raw bytes) plus their display-only file names in a
      // one-shot same-tab sessionStorage record keyed by the new session id.
      // The navigation URL carries just the q/qokb/qok marker plus the
      // innocuous fh flag — never IDs or names. A failure keeps the user
      // here with the selection intact for retry, and the orphan session is
      // deleted best-effort (never masking the upload error).
      let attachmentIds: string[] = [];
      const attachmentNames = [...s.attachments.map((a) => a.name), ...s.imageFiles.map((f) => f.name)];
      if (s.attachments.length > 0 || s.imageFiles.length > 0) {
        try {
          attachmentIds = await uploadFirstTurnFiles(
            sessionId,
            s.attachments.map((a) => a.file),
            s.imageFiles,
            {
              upload: async (sid, file) => {
                const up = await uploadTemporaryAttachment(
                  sid,
                  file,
                  ctx.settings.selectedAgentId || undefined,
                  "auto",
                );
                return { id: up.data.id };
              },
              remove: (sid, id) => deleteTemporaryAttachment(sid, id),
            },
          );
        } catch (err) {
          setError(userErrorMessage(err, t, "err.attachmentUploadFailed"));
          await cleanupSessionBestEffort((id) => deleteSession(id), sessionId);
          return;
        }
        // sessionStorage itself is best-effort: when it is unavailable the
        // files cannot be handed off, so fail visibly BEFORE navigating —
        // never navigate without the files. The storage getter itself can
        // throw (blocked cookies / private mode SecurityError), so the
        // access lives INSIDE the try: any staging failure best-effort
        // cleans up the fresh session and keeps the selection for retry.
        try {
          if (typeof window === "undefined" || !window.sessionStorage) {
            throw new Error("browser storage unavailable");
          }
          saveFirstTurnHandoff(window.sessionStorage, sessionId, { attachmentIds, attachmentNames });
        } catch (err) {
          setError(err instanceof Error ? err.message : "Could not stage attachments — files kept for retry");
          await cleanupSessionBestEffort((id) => deleteSession(id), sessionId);
          return;
        }
      }
      persistKbScope();
      // The question and its retrieval hint travel one-shot via the route
      // (the chat page consumes the staged file handoff on the first send
      // and the auto-send effect strips the query string). The URL carries
      // only the innocuous marker — never attachment IDs or file names.
      const qs = buildFirstTurnSearch({
        query: q,
        questionOrigin: s.questionOrigin,
        hasAttachments: attachmentIds.length > 0,
      });
      router.push(`/platform/chat/${sessionId}?${qs}`);
    } catch (err) {
      // the account kind changed under the open page: read /auth/me again
      // and the page follows the new kind
      if (shouldRefreshAccount(err, { isGuest: ctx.isGuest, sessionCall: false })) void refreshMe();
      const key = chatErrorKey(err);
      setError(key ? t(chatErrorTextKey(key, ctx.isGuest)) : userErrorMessage(err, t, "ui.err.createSession"));
    } finally {
      setBusy(false);
    }
  };

  // a guest gets no suggestions: the route needs an organization
  useEffect(() => {
    // a member always has an organization; wait for it so the key is final
    if (ctx.isGuest || !userId || !selectedTenantId) return;
    setSuggested(cachedSuggestions(suggestionKey));
    let cancelled = false;
    void loadSuggestedOnce(suggestionKey).then((qs) => {
      if (!cancelled) setSuggested(qs);
    });
    return () => {
      cancelled = true;
    };
  }, [ctx.isGuest, userId, selectedTenantId, suggestionKey]);

  const isDocumentAssistant = ctx.settings.selectedAgentId === BUILTIN_DOCUMENT_ASSISTANT_ID;

  const column = (
    <div className="relative flex min-h-0 min-w-0 flex-1 flex-col items-center overflow-y-auto overflow-x-hidden px-4 sm:px-8">
      <div className="pointer-events-none absolute inset-0 overflow-hidden" aria-hidden="true">
        <Orb color="sky" size={560} className="-top-48 left-1/2 -translate-x-1/2" />
        <Orb color="rose" size={380} className="bottom-[-140px] right-[8%]" />
        <Orb color="mint" size={300} className="bottom-[-100px] left-[10%]" />
      </div>

      {/* Top-anchored, not vertically centred: the suggestions load after
          the page and their height must not move the title and composer. */}
      <div className="relative w-full max-w-[768px] shrink-0 pb-6 pt-[6vh] sm:pb-10 sm:pt-[14vh]">
        <h1 className="display-xl mb-3 text-center">{t("cc.title")}</h1>
        <p className={`${ctx.isGuest ? "mb-3" : "mb-10"} text-center text-muted`}>
          {t(ctx.isGuest ? "guest.ccSubtitle" : "cc.subtitle")}
        </p>
        {ctx.isGuest && <GuestChatNote className="mx-auto mb-8 max-w-[560px]" />}
        {isDocumentAssistant && (
          <p className="body-sm -mt-6 mb-6 text-center text-muted">{t("docws.createChatHint")}</p>
        )}

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
          value={value}
          onChange={setValue}
          onSend={(s) => void send(s)}
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
          autoFocus
        />
        {error && <p className="body-sm mt-4 text-center text-error">{error}</p>}

        {/* A guest checks validity here without spending a daily question. */}
        {ctx.isGuest && <LegalLookupBox className="mx-auto mt-6 max-w-[560px]" />}

        {/* Generic suggestions do not apply to the document assistant. */}
        {!isDocumentAssistant && !ctx.isGuest && (
        <div className="mt-6 flex flex-wrap justify-center gap-2.5" aria-busy={suggested === null}>
          {suggested === null ? (
            SUGGESTION_SKELETON_WIDTHS.map((w, i) => (
              <span key={i} aria-hidden className="h-9 max-w-full animate-pulse rounded-[20px] bg-surface-strong/70" style={{ width: w }} />
            ))
          ) : (
            <div className="contents motion-safe:[&>button]:animate-[suggestion-in_220ms_ease-out_both]">
              {suggested.map((item) => (
                <button
                  key={item.question}
                  onClick={() =>
                    void send(
                      buildSuggestionSend({
                        query: item.question,
                        questionOrigin: questionOriginFromSuggestion(item),
                        // A picked suggestion sends the CURRENT selection too —
                        // empty lists here would silently drop attached files.
                        attachments: attachments.items,
                        imageFiles: images.map((i) => i.file),
                      }),
                    )
                  }
                  disabled={busy || docPaneBusy}
                  className="h-auto max-w-full shrink-0 whitespace-normal break-words rounded-[20px] border border-hairline-strong bg-surface-card px-3 py-2 text-left text-[12px] leading-relaxed text-body transition-colors hover:border-ink hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink disabled:opacity-50 sm:px-4 sm:text-[14px]"
                >
                  {item.question}
                </button>
              ))}
            </div>
          )}
        </div>
        )}
      </div>
    </div>
  );

  // Always through SplitPane so the composer keeps its identity when the
  // document assistant is (de)selected. Pre-session pane: picking a file
  // creates the session and opens the chat on it (no first message).
  return (
    <SplitPane
      documentMode={isDocumentAssistant}
      left={
        isDocumentAssistant ? (
          <DocWorkspace
            sessionId={undefined}
            blocked={busy}
            onBusyChange={setDocPaneBusy}
            onSessionCreated={(sid) => {
              persistKbScope();
              router.push(`/platform/chat/${sid}`);
            }}
            onSelectionChange={() => {}}
          />
        ) : null
      }
      right={column}
    />
  );
}

export function CreateChat({ kbId }: { kbId?: string }) {
  return (
    <ChatProvider newChat>
      <CreateChatBody kbId={kbId} />
    </ChatProvider>
  );
}
