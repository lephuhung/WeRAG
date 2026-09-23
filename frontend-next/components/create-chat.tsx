"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { apiPost } from "@/lib/api-client";
import { getSuggestedQuestions, type SuggestedQuestion } from "@/lib/api/extra";
import { ChatProvider } from "@/lib/chat-context";
import { Orb } from "@/components/orb";
import { Composer, type ComposerSend } from "@/components/composer";
import { useAttachments } from "@/components/use-attachments";
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
  const attachments = useAttachments(undefined);

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
      const res = await apiPost<{ success: boolean; data?: { id: string }; message?: string }>(
        `/api/v1/sessions`,
        {},
      );
      const sessionId = res.data?.id;
      if (!sessionId) throw new Error(res.message ?? "Failed to create session");
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
      const qp = new URLSearchParams({ q });
      if (s.questionOrigin?.knowledge_base_id) qp.set("qokb", s.questionOrigin.knowledge_base_id);
      if (s.questionOrigin?.knowledge_id) qp.set("qok", s.questionOrigin.knowledge_id);
      router.push(`/platform/chat/${sessionId}?${qp.toString()}`);
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
          onPickImages={() => {}}
          autoFocus
        />
        {error && <p className="body-sm mt-4 text-center text-error">{error}</p>}

        <div className="mt-6 flex flex-wrap justify-center gap-2.5">
          {suggested?.length
            ? suggested.map((item) => (
                <button
                  key={item.question}
                  onClick={() =>
                    void send({
                      query: item.question,
                      modelId: "",
                      mentionedItems: [],
                      imageFiles: [],
                      attachments: [],
                      questionOrigin: questionOriginFromSuggestion(item),
                    })
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
                  onClick={() => void send({ query: s, modelId: "", mentionedItems: [], imageFiles: [], attachments: [] })}
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
