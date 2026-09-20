/* Embed visitor chat — ported from frontend/src/views/embed/EmbedPage.vue +
 * EmbedChatCore.vue + composables/useEmbedBridge.ts.
 * Boot chain: token (URL ?token= / hash / postMessage from host) → optional
 * publish→session exchange (secure mode) → public config → resume-or-create
 * signed session → message list → streamEmbedChat (Embed auth, no Bearer).
 */
"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useParams, useSearchParams } from "next/navigation";
import {
  getEmbedConfig,
  createEmbedSession,
  exchangeEmbedSession,
  getEmbedMessageList,
  getOrCreateEmbedVisitorId,
  embedChatSessionStorageKey,
  clearEmbedStoredChatSession,
  isEmbedSessionToken,
  parseEmbedTokenFromLocation,
  type EmbedChannelPublicConfig,
} from "@/lib/api/embed";
import { streamEmbedChat, type StreamChunk } from "@/lib/api/stream";
import { Markdown } from "@/components/markdown";
import { Composer, type ComposerSend } from "@/components/composer";
import { IconDoc, IconPlus } from "@/components/icons";

type StoredSession = { id: string; sig: string; agentId: string };

type UiMessage = {
  id: string;
  role: "user" | "assistant";
  content: string;
  streaming?: boolean;
  references?: Array<{ knowledge_title?: string; knowledge_id?: string }>;
};

function readStoredSession(channelId: string): StoredSession | null {
  try {
    const raw = localStorage.getItem(embedChatSessionStorageKey(channelId));
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<StoredSession>;
    if (parsed.id && parsed.sig) return { id: parsed.id, sig: parsed.sig, agentId: parsed.agentId ?? "" };
  } catch {
    /* ignore */
  }
  return null;
}

export default function EmbedPage() {
  const params = useParams<{ channelId: string }>();
  const channelId = params.channelId;
  const search = useSearchParams();
  const queryToken = search.get("token");

  const [token, setToken] = useState("");
  const [config, setConfig] = useState<EmbedChannelPublicConfig | null>(null);
  const [session, setSession] = useState<StoredSession | null>(null);
  const [messages, setMessages] = useState<UiMessage[]>([]);
  const [input, setInput] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [status, setStatus] = useState("");
  const bottomRef = useRef<HTMLDivElement>(null);
  const abortRef = useRef<AbortController | null>(null);

  const visitorId = getOrCreateEmbedVisitorId(channelId);

  useEffect(() => {
    document.documentElement.lang = "en";
  }, []);

  useEffect(() => () => abortRef.current?.abort(), []);

  const bootstrap = useCallback(async () => {
    if (!channelId) return;
    const urlToken = parseEmbedTokenFromLocation() || queryToken || "";
    if (!urlToken) {
      setStatus("awaiting-token");
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      let apiToken = urlToken;
      // Secure mode: publish token → short-lived session token. The exchange
      // endpoint only accepts publish tokens; a real ems_ session token skips it.
      if (!isEmbedSessionToken(urlToken)) {
        const exchangeRes = await exchangeEmbedSession(channelId, urlToken);
        const exchanged = (exchangeRes as { data?: { session_token?: string } }).data?.session_token;
        if (exchanged) apiToken = exchanged;
      }
      const cfgRes = await getEmbedConfig(channelId, apiToken);
      if (!cfgRes.data) throw new Error("Invalid channel");

      setConfig(cfgRes.data);
      setToken(apiToken);
      if (cfgRes.data.page_title) document.title = cfgRes.data.page_title;

      /* Resume a persisted session bound to the same agent; else mint one. */
      const agentId = String(cfgRes.data.agent_id || "").trim();
      let resolved: StoredSession | null = null;
      const stored = readStoredSession(channelId);
      if (stored && (!stored.agentId || !agentId || stored.agentId === agentId)) {
        try {
          const list = await getEmbedMessageList(channelId, apiToken, stored.id, 30, "", stored.sig);
          if ((list as { data?: unknown }).data) resolved = stored;
        } catch {
          resolved = null;
        }
      }
      if (!resolved) {
        const sessionRes = await createEmbedSession(channelId, apiToken);
        resolved = { id: sessionRes.data.id, sig: sessionRes.data.sig, agentId };
      }
      setSession(resolved);
      try {
        localStorage.setItem(
          embedChatSessionStorageKey(channelId),
          JSON.stringify(resolved),
        );
      } catch {
        /* private mode */
      }
      const list = await getEmbedMessageList(channelId, apiToken, resolved.id, 30, "", resolved.sig);
      const rows = (list as { data?: Array<{ id?: string; role?: string; content?: string }> }).data ?? [];
      setMessages(
        rows.map((m, i) => ({
          id: m.id ?? `h${i}`,
          role: m.role === "user" ? "user" : "assistant",
          content: m.content ?? "",
        })),
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load chat");
    } finally {
      setLoading(false);
    }
  }, [channelId, queryToken]);

  useEffect(() => {
    void bootstrap();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [channelId]);

  /* Host-frame token handshake: the widget script waits for this event. */
  useEffect(() => {
    if (token) return;
    const onToken = (e: MessageEvent) => {
      if (e.data && typeof e.data === "object" && "embed_token" in e.data) {
        const tk = String((e.data as { embed_token?: string }).embed_token ?? "");
        if (tk) {
          setStatus("");
          void bootstrap();
        }
      }
    };
    window.addEventListener("message", onToken);
    return () => window.removeEventListener("message", onToken);
  }, [token, bootstrap]);

  const newChat = useCallback(async () => {
    if (!channelId || !token) return;
    try {
      const sessionRes = await createEmbedSession(channelId, token);
      const next: StoredSession = {
        id: sessionRes.data.id,
        sig: sessionRes.data.sig,
        agentId: config?.agent_id ?? "",
      };
      clearEmbedStoredChatSession(channelId);
      setSession(next);
      try {
        localStorage.setItem(embedChatSessionStorageKey(channelId), JSON.stringify(next));
      } catch {
        /* ignore */
      }
      setMessages([]);
    } catch {
      /* keep current session on failure */
    }
  }, [channelId, token, config?.agent_id]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  const send = async (s: ComposerSend) => {
    const q = s.query.trim();
    if (!q || busy || !session) return;
    setError("");
    setBusy(true);
    const asstId = `a${Date.now()}`;
    setMessages((m) => [
      ...m,
      { id: `u${Date.now()}`, role: "user", content: q },
      { id: asstId, role: "assistant", content: "", streaming: true },
    ]);
    setInput("");

    const ctrl = new AbortController();
    abortRef.current = ctrl;
    let acc = "";
    const applyChunk = (c: StreamChunk) => {
      const kind = c.response_type ?? c.type;
      if (kind === "session_title" || kind === "agent_query") return;
      if (kind === "references" && c.knowledge_references?.length) {
        const refs = c.knowledge_references;
        setMessages((m) => m.map((msg) => (msg.id === asstId ? { ...msg, references: refs } : msg)));
        return;
      }
      if (kind === "error") {
        setError(c.content || "Stream failed");
        return;
      }
      acc += c.content ?? "";
      const snapshot = acc;
      setMessages((m) => m.map((msg) => (msg.id === asstId ? { ...msg, content: snapshot } : msg)));
    };

    const inlineImages: Array<{ data: string }> = [];
    for (const file of s.imageFiles) {
      inlineImages.push({ data: await new Promise<string>((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(String(reader.result));
        reader.onerror = reject;
        reader.readAsDataURL(file);
      }) });
    }

    streamEmbedChat({
      sessionId: session.id,
      channelId,
      embedToken: token,
      sessionSig: session.sig,
      visitorId,
      query: q,
      agentEnabled: true,
      agentId: config?.agent_id || undefined,
      knowledgeBaseIds: config?.knowledge_base_ids ?? [],
      webSearchEnabled: false,
      images: inlineImages.length > 0 ? inlineImages : undefined,
      signal: ctrl.signal,
      onChunk: applyChunk,
    })
      .catch((e: unknown) => {
        if (e instanceof DOMException && e.name === "AbortError") return;
        setError(e instanceof Error ? e.message : "Stream failed");
      })
      .finally(() => {
        setBusy(false);
        setMessages((m) => m.map((msg) => (msg.id === asstId ? { ...msg, streaming: false } : msg)));
      });
  };

  if (loading) {
    return (
      <div className="flex h-screen items-center justify-center bg-canvas">
        <p className="caption text-muted">Loading chat…</p>
      </div>
    );
  }
  if (status === "awaiting-token") {
    return (
      <div className="flex h-screen items-center justify-center bg-canvas">
        <p className="caption text-muted">Waiting for channel token…</p>
      </div>
    );
  }
  if (error && !config) {
    return (
      <div className="flex h-screen items-center justify-center bg-canvas px-6">
        <p className="caption text-error">{error}</p>
      </div>
    );
  }
  if (!config || !session) return null;

  return (
    <div className="flex h-screen min-h-0 flex-col bg-canvas">
      <header className="hairline-b flex shrink-0 items-center gap-3 px-5 py-3">
        {config.agent_avatar ? (
          <span className="flex h-9 w-9 items-center justify-center rounded-full bg-surface-strong text-[15px]">
            {config.agent_avatar}
          </span>
        ) : null}
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-[15px] font-medium text-ink">
            {config.display_title || config.name}
          </h1>
          {config.agent_name && (
            <p className="caption truncate text-muted">{config.agent_name}</p>
          )}
        </div>
        <button
          className="btn btn-outline btn-sm"
          disabled={messages.length === 0}
          onClick={() => void newChat()}
          aria-label="New chat"
        >
          <IconPlus className="h-4 w-4" />
        </button>
      </header>
      {config.welcome_message && (
        <div className="border-b border-hairline bg-surface-card px-5 py-3">
          <p className="body-sm text-body">{config.welcome_message}</p>
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-[720px] px-5 py-6">
          {messages.map((m) =>
            m.role === "user" ? (
              <div key={m.id} className="mb-5 flex justify-end">
                <div className="max-w-[80%] rounded-[16px] bg-primary px-4 py-2.5 text-[15px] leading-relaxed text-on-primary">
                  {m.content}
                </div>
              </div>
            ) : (
              <div key={m.id} className="mb-5">
                {m.content ? (
                  <Markdown text={m.content} streaming={m.streaming} />
                ) : (
                  m.streaming && <p className="text-[15px] text-body">…</p>
                )}
                {m.references && m.references.length > 0 && (
                  <div className="mt-2 flex flex-wrap gap-2">
                    {m.references.map((r, i) => (
                      <span
                        key={`${r.knowledge_id}-${i}`}
                        className="flex items-center gap-1.5 rounded-full border border-hairline bg-surface-card px-2.5 py-1 text-[12px] text-body"
                      >
                        <span className="flex h-4 w-4 items-center justify-center rounded-full bg-surface-strong text-[10px] font-semibold text-ink">
                          {i + 1}
                        </span>
                        <IconDoc className="h-3 w-3 text-muted" />
                        {r.knowledge_title ?? r.knowledge_id ?? "source"}
                      </span>
                    ))}
                  </div>
                )}
              </div>
            ),
          )}
          {error && <p className="body-sm mb-3 text-error">{error}</p>}
          <div ref={bottomRef} />
        </div>
      </div>

      <div className="shrink-0 px-5 pb-4 pt-2">
        <div className="mx-auto max-w-[720px]">
          <Composer
            value={input}
            onChange={setInput}
            onSend={(s) => void send(s)}
            isReplying={busy}
            attachments={[]}
            images={[]}
            onRemoveAttachment={() => {}}
            onRemoveImage={() => {}}
            onPickFiles={() => {}}
            onPickImages={() => {}}
            autoFocus
          />
        </div>
      </div>
    </div>
  );
}
