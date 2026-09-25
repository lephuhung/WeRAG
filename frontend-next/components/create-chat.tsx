"use client";

import { useRouter } from "next/navigation";
import { useRef, useState } from "react";
import { apiPost } from "@/lib/api-client";
import { getSuggestedQuestions, type SuggestedQuestion } from "@/lib/api/extra";
import { ChatProvider, useChatContext } from "@/lib/chat-context";
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

const FALLBACK_SUGGESTIONS = [
  "Summarize the latest release notes",
  "Compare embedding models for my docs",
  "Draft an FAQ from the support knowledge base",
  "Explain hybrid retrieval in this project",
];

function CreateChatBody({ kbId }: { kbId?: string }) {
  const router = useRouter();
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [suggested, setSuggested] = useState<SuggestedQuestion[] | null>(null);
  const [images, setImages] = useState<Array<{ preview: string; file: File }>>([]);
  const imageInputRef = useRef<HTMLInputElement>(null);
  const attachments = useAttachments(undefined);
  const ctx = useChatContext();

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

  // Session create takes title/description only (CreateSessionRequest); the
  // KB scope + first question travel via ?q=… + chat context, like the Vue flow.
  // A picked suggestion's retrieval hint rides as qokb/qok (creatChat.vue
  // firstQuestionOrigin) so the first stream keeps question_origin.
  const send = async (s: ComposerSend) => {
    const q = s.query.trim();
    if (!q || busy) return;
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
      if (!sessionId) throw new Error(res.message ?? "Failed to create session");
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
                  ctx.settings.selectedAgentSourceTenantId ?? undefined,
                  "auto",
                );
                return { id: up.data.id };
              },
              remove: (sid, id) => deleteTemporaryAttachment(sid, id),
            },
          );
        } catch (err) {
          setError(err instanceof Error ? err.message : "Attachment upload failed — files kept for retry");
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
      if (kbId) {
        try {
          const raw = localStorage.getItem("WeKnora_settings");
          const parsed = raw ? (JSON.parse(raw) as { selectedKnowledgeBases?: string[] }) : {};
          const ids = parsed.selectedKnowledgeBases ?? [];
          if (!ids.includes(kbId)) {
            localStorage.setItem("WeKnora_settings", JSON.stringify({ ...parsed, selectedKnowledgeBases: [...ids, kbId] }));
          }
        } catch {
          /* ignore */
        }
      }
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
      setError(err instanceof Error ? err.message : "Failed to create session");
    } finally {
      setBusy(false);
    }
  };

  const loadSuggested = async () => {
    try {
      const qs = await getSuggestedQuestions();
      setSuggested(qs);
    } catch {
      setSuggested([]);
    }
  };
  if (suggested === null && typeof window !== "undefined") void loadSuggested();

  const suggestions = suggested?.length ? suggested.map((s) => s.question) : FALLBACK_SUGGESTIONS;

  return (
    <div className="relative flex flex-1 flex-col items-center justify-center overflow-hidden px-4 sm:px-8">
      <Orb color="sky" size={560} className="-top-48 left-1/2 -translate-x-1/2" />
      <Orb color="rose" size={380} className="bottom-[-140px] right-[8%]" />
      <Orb color="mint" size={300} className="bottom-[-100px] left-[10%]" />

      <div className="relative w-full max-w-[720px]">
        <h1 className="display-xl mb-3 text-center">What would you like to know?</h1>
        <p className="mb-10 text-center text-muted">
          Ask across your knowledge bases — answers cite their sources.
        </p>

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

        <div className="mt-6 flex flex-wrap justify-center gap-2.5">
          {suggested?.length
            ? suggested.map((item) => (
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
                  disabled={busy}
                  className="rounded-full border border-hairline-strong bg-surface-card px-4 py-2 text-[14px] text-body transition-colors hover:border-ink hover:text-ink disabled:opacity-50"
                >
                  {item.question}
                </button>
              ))
            : FALLBACK_SUGGESTIONS.map((s) => (
                <button
                  key={s}
                  onClick={() =>
                    void send(
                      buildSuggestionSend({
                        query: s,
                        attachments: attachments.items,
                        imageFiles: images.map((i) => i.file),
                      }),
                    )
                  }
                  disabled={busy}
                  className="rounded-full border border-hairline-strong bg-surface-card px-4 py-2 text-[14px] text-body transition-colors hover:border-ink hover:text-ink disabled:opacity-50"
                >
                  {s}
                </button>
              ))}
        </div>
      </div>
    </div>
  );
}

export function CreateChat({ kbId }: { kbId?: string }) {
  return (
    <ChatProvider>
      <CreateChatBody kbId={kbId} />
    </ChatProvider>
  );
}
