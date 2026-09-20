/* SSE chat stream ported from frontend/src/api/chat/streame.ts +
 * views/chat continue-stream usage.
 *
 * Endpoints:
 *   POST /api/v1/agent-chat/:sessionId          — agent pipeline
 *   POST /api/v1/agent-chat/new-session         — agent chat, server mints the session
 *   POST /api/v1/knowledge-chat/:sessionId      — plain RAG pipeline
 *   GET  /api/v1/sessions/continue-stream/:sessionId?message_id=<id>
 *                                               — attach to an in-flight reply
 *   POST /api/v1/embed/:channelId/agent-chat/:sessionId      — embed visitor
 *   POST /api/v1/embed/:channelId/knowledge-chat/:sessionId  — embed visitor
 *
 * Replay-once on 401: refreshes weknora_refresh_token and re-opens the stream.
 * Embed streams authenticate with `Embed <token>` (+ X-Embed-Session /
 * X-Embed-Visitor) and never send Bearer or X-Tenant-ID.
 */

export type StreamChunk = {
  id?: string;
  response_type?: string;
  type?: string;
  content?: string;
  done?: boolean;
  data?: { title?: string; session_id?: string; query?: string; request_id?: string };
  session_id?: string;
  assistant_message_id?: string;
  knowledge_references?: Array<{
    knowledge_title?: string;
    knowledge_id?: string;
    chunk_id?: string;
  }>;
};

export type MentionedItem = {
  id: string;
  name: string;
  type: string;
  kb_type?: string;
  kb_id?: string;
  kb_name?: string;
  service_id?: string;
  skill_name?: string;
};

export type StreamAttachmentUpload = { data: string; file_name: string; file_size: number };

export type StreamParams = {
  sessionId: string;
  query: string;
  agentEnabled?: boolean;
  agentId?: string;
  agentSourceTenantId?: string | number;
  knowledgeBaseIds?: string[];
  knowledgeIds?: string[];
  tagIds?: string[];
  mcpServiceIds?: string[];
  skillNames?: string[];
  mentionedItems?: MentionedItem[];
  webSearchEnabled?: boolean;
  localBrowserEnabled?: boolean;
  attachmentIds?: string[];
  attachmentUploads?: StreamAttachmentUpload[];
  summaryModelId?: string;
  images?: Array<{ data: string }>;
  suggestionAttribution?: { suggestion_set_id: string; question_id: string };
  questionOrigin?: { knowledge_base_id: string; knowledge_id?: string };
  signal?: AbortSignal;
  onChunk: (c: StreamChunk) => void;
};

/** Embed-visitor stream params: the Embed token replaces Bearer auth. */
export type EmbedStreamParams = StreamParams & {
  channelId: string;
  embedToken: string;
  sessionSig?: string;
  visitorId?: string;
};

function readTokens(): { token: string | null; refreshToken: string | null; tenantId: string | null } {
  try {
    return {
      token: localStorage.getItem("weknora_token"),
      refreshToken: localStorage.getItem("weknora_refresh_token"),
      tenantId: localStorage.getItem("weknora_selected_tenant_id"),
    };
  } catch {
    return { token: null, refreshToken: null, tenantId: null };
  }
}

async function refreshTokenNow(refreshToken: string): Promise<string> {
  const res = await fetch("/api/v1/auth/refresh", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ refreshToken }),
  });
  const data = await res.json().catch(() => null);
  const token =
    data && typeof data === "object" && "access_token" in data
      ? (data as { access_token?: string }).access_token
      : undefined;
  if (!res.ok || !token) throw new Error("Session expired");
  try {
    localStorage.setItem("weknora_token", token);
  } catch {
    /* private mode */
  }
  return token;
}

function buildChatBody(params: StreamParams, agentId: string | undefined, isEmbed: boolean) {
  return {
    query: params.query,
    agent_enabled: params.agentEnabled !== undefined ? params.agentEnabled : Boolean(agentId),
    ...(agentId ? { agent_id: agentId } : {}),
    ...(params.agentSourceTenantId
      ? { agent_source_tenant_id: Number(params.agentSourceTenantId) || params.agentSourceTenantId }
      : {}),
    ...(params.knowledgeBaseIds?.length ? { knowledge_base_ids: params.knowledgeBaseIds } : {}),
    ...(params.knowledgeIds?.length ? { knowledge_ids: params.knowledgeIds } : {}),
    ...(params.tagIds?.length ? { tag_ids: params.tagIds } : {}),
    ...(params.mentionedItems?.length ? { mentioned_items: params.mentionedItems } : {}),
    ...(params.webSearchEnabled !== undefined
      ? { web_search_enabled: params.webSearchEnabled }
      : {}),
    ...(params.localBrowserEnabled !== undefined
      ? { local_browser_enabled: params.localBrowserEnabled }
      : {}),
    ...(params.summaryModelId ? { summary_model_id: params.summaryModelId } : {}),
    // MCP/skills only ride the agent pipeline — same guard as the Vue sender.
    ...(agentId && params.mcpServiceIds?.length
      ? { mcp_service_ids: params.mcpServiceIds }
      : {}),
    ...(agentId && params.skillNames?.length ? { skill_names: params.skillNames } : {}),
    ...(params.attachmentIds?.length ? { attachment_ids: params.attachmentIds } : {}),
    ...(params.attachmentUploads?.length
      ? { attachment_uploads: params.attachmentUploads }
      : {}),
    ...(params.images?.length ? { images: params.images } : {}),
    ...(params.suggestionAttribution
      ? { suggestion_attribution: params.suggestionAttribution }
      : {}),
    ...(params.questionOrigin ? { question_origin: params.questionOrigin } : {}),
    channel: isEmbed ? "embed" : "web",
  };
}

async function readSSE(res: Response, onChunk: (c: StreamChunk) => void) {
  if (!res.body) throw new Error("Empty response body");
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    const frames = buf.split("\n\n");
    buf = frames.pop() ?? "";
    for (const frame of frames) {
      for (const line of frame.split("\n")) {
        const text = line.startsWith("data:") ? line.slice(5).trim() : "";
        if (!text || text === "[DONE]") continue;
        try {
          onChunk(JSON.parse(text) as StreamChunk);
        } catch {
          /* keep-alive comment or partial frame */
        }
      }
    }
  }
}

function streamError(res: Response, body = ""): Error {
  const err = new Error(
    res.status === 401 ? "unauthorized" : `HTTP ${res.status}${body ? `: ${body.slice(0, 300)}` : ""}`,
  ) as Error & { status?: number };
  err.status = res.status;
  return err;
}

async function openChatStream(
  params: StreamParams | EmbedStreamParams,
  token: string,
  embed: EmbedStreamParams | null,
) {
  const { tenantId } = readTokens();
  // Backend AgentQA 400s when agent_enabled=true without a resolvable
  // agent_id (qa.go sanity gate). Vue's default is builtin-quick-answer
  // (RAG pipeline): the agent id decides the endpoint, not a bare flag.
  const agentId =
    params.agentId && params.agentId !== "builtin-quick-answer" ? params.agentId : undefined;
  const endpoint = agentId ? "agent-chat" : "knowledge-chat";
  const url = embed
    ? `/api/v1/embed/${encodeURIComponent(embed.channelId)}/${endpoint}/${encodeURIComponent(params.sessionId)}`
    : `/api/v1/${endpoint}/${encodeURIComponent(params.sessionId)}`;
  const res = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "text/event-stream",
      Authorization: embed ? `Embed ${embed.embedToken}` : `Bearer ${token}`,
      "X-Request-ID": Math.random().toString(36).slice(2, 14),
      // Embed visitors have no tenant context — never send X-Tenant-ID.
      ...(!embed && tenantId ? { "X-Tenant-ID": tenantId } : {}),
      ...(embed?.sessionSig ? { "X-Embed-Session": embed.sessionSig } : {}),
      ...(embed?.visitorId ? { "X-Embed-Visitor": embed.visitorId } : {}),
    },
    body: JSON.stringify(buildChatBody(params, agentId, Boolean(embed))),
    signal: params.signal,
  });
  if (res.status === 401) throw streamError(res);
  if (!res.ok) throw streamError(res, await res.text().catch(() => ""));
  await readSSE(res, params.onChunk);
}

/** POST a chat turn and consume the SSE answer stream. */
export async function streamChat(params: StreamParams): Promise<void> {
  const { token, refreshToken } = readTokens();
  if (!token) throw new Error("Please sign in again");
  try {
    await openChatStream(params, token, null);
  } catch (err) {
    const status = err && typeof err === "object" && "status" in err ? err.status : undefined;
    if (status === 401 && refreshToken) {
      const next = await refreshTokenNow(refreshToken);
      await openChatStream(params, next, null);
      return;
    }
    throw err;
  }
}

/* Agent chat without an existing session — POST /api/v1/agent-chat/new-session.
 * The backend creates the session and reports its id in the stream frames
 * (watch for data.session_id / session_id on chunks). */
export function streamNewAgentSession(
  params: Omit<StreamParams, "sessionId">,
): Promise<void> {
  return streamChat({ ...params, sessionId: "new-session" });
}

/* Attach to an in-flight assistant reply: GET
 * /api/v1/sessions/continue-stream/:sessionId?message_id=<assistantMessageId>.
 * Used after a page refresh or to follow an IM-originated reply — the server
 * replays the event log then continues streaming. 404 means the run already
 * finished (or was never on this server); callers fall back to polling the
 * message list. */
export async function continueStream(params: {
  sessionId: string;
  messageId: string;
  signal?: AbortSignal;
  onChunk: (c: StreamChunk) => void;
}): Promise<void> {
  const { token, refreshToken, tenantId } = readTokens();
  if (!token) throw new Error("Please sign in again");
  const url = `/api/v1/sessions/continue-stream/${encodeURIComponent(params.sessionId)}?message_id=${encodeURIComponent(params.messageId)}`;
  const open = (authToken: string) =>
    fetch(url, {
      headers: {
        Accept: "text/event-stream",
        Authorization: `Bearer ${authToken}`,
        "X-Request-ID": Math.random().toString(36).slice(2, 14),
        ...(tenantId ? { "X-Tenant-ID": tenantId } : {}),
      },
      signal: params.signal,
    });
  let res = await open(token);
  if (res.status === 401 && refreshToken) {
    res = await open(await refreshTokenNow(refreshToken));
  }
  if (res.status === 401) throw streamError(res);
  if (!res.ok) throw streamError(res, await res.text().catch(() => ""));
  await readSSE(res, params.onChunk);
}

/** Embed-visitor chat stream. No Bearer, no X-Tenant-ID, no 401 refresh. */
export async function streamEmbedChat(params: EmbedStreamParams): Promise<void> {
  await openChatStream(params, params.embedToken, params);
}
