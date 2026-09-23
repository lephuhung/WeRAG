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

import type { KnowledgeReferenceItem } from "@/components/chat/references-drawer";

export type StreamChunk = {
  id?: string;
  response_type?: string;
  type?: string;
  content?: string;
  done?: boolean;
  data?: {
    title?: string;
    session_id?: string;
    query?: string;
    request_id?: string;
    thought?: string;
    [key: string]: unknown;
  };
  session_id?: string;
  assistant_message_id?: string;
  knowledge_references?: KnowledgeReferenceItem[];
  // Reasoning and Tool properties
  tool_name?: string;
  tool_call_id?: string;
  tool_data?: Record<string, unknown>;
  tool_input?: unknown;
  tool_output?: unknown;
  reasoning_content?: string;
  thought?: string;
  pending?: boolean;
  success?: boolean;
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
  abbreviationCandidates?: string[];
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
    const rawTid = localStorage.getItem("weknora_selected_tenant_id");
    const validTid =
      rawTid &&
      rawTid.trim() !== "" &&
      rawTid !== "undefined" &&
      rawTid !== "null" &&
      !Number.isNaN(Number(rawTid)) &&
      Number(rawTid) > 0
        ? rawTid.trim()
        : null;
    return {
      token: localStorage.getItem("weknora_token"),
      refreshToken: localStorage.getItem("weknora_refresh_token"),
      tenantId: validTid,
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

function buildChatBody(params: StreamParams, isAgentChat: boolean, isEmbed: boolean) {
  const agentEnabled = params.agentEnabled !== undefined ? params.agentEnabled : isAgentChat;
  return {
    query: params.query,
    agent_enabled: agentEnabled,
    ...(params.agentId ? { agent_id: params.agentId } : {}),
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
    ...(isAgentChat && params.mcpServiceIds?.length
      ? { mcp_service_ids: params.mcpServiceIds }
      : {}),
    ...(isAgentChat && params.skillNames?.length ? { skill_names: params.skillNames } : {}),
    ...(params.attachmentIds?.length ? { attachment_ids: params.attachmentIds } : {}),
    ...(params.attachmentUploads?.length
      ? { attachment_uploads: params.attachmentUploads }
      : {}),
    ...(params.images?.length ? { images: params.images } : {}),
    ...(params.suggestionAttribution
      ? { suggestion_attribution: params.suggestionAttribution }
      : {}),
    ...(params.questionOrigin ? { question_origin: params.questionOrigin } : {}),
    ...(params.abbreviationCandidates?.length
      ? { abbreviation_candidates: params.abbreviationCandidates }
      : {}),
    channel: isEmbed ? "embed" : "web",
  };
}

async function readSSE(res: Response, onChunk: (c: StreamChunk) => void) {
  if (!res.body) throw new Error("Empty response body");
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";

  const dispatchFrame = (frame: string) => {
    const lines = frame.split(/\r?\n/);
    let data = "";
    for (const line of lines) {
      if (line.startsWith("data:")) {
        const text = line.replace(/^data:\s?/, "");
        data = data ? `${data}\n${text}` : text;
      }
    }
    if (!data || data === "[DONE]") return;
    try {
      const parsed = JSON.parse(data) as StreamChunk;
      onChunk(parsed);
    } catch (err) {
      console.warn("[readSSE] Failed to parse JSON frame:", err, "raw data:", data);
    }
  };

  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });

    // Split on double line break (\r\n\r\n or \n\n)
    let boundaryMatch: RegExpExecArray | null;
    const boundaryRegex = /\r?\n\r?\n/;
    while ((boundaryMatch = boundaryRegex.exec(buf)) !== null) {
      const frame = buf.slice(0, boundaryMatch.index);
      buf = buf.slice(boundaryMatch.index + boundaryMatch[0].length);
      if (frame.trim()) {
        dispatchFrame(frame);
      }
    }
  }

  // Flush any trailing event frame remaining after stream closes
  if (buf.trim()) {
    dispatchFrame(buf);
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
  const isAgentChat =
    params.agentEnabled === true &&
    Boolean(params.agentId) &&
    params.agentId !== "builtin-quick-answer";
  const endpoint = isAgentChat ? "agent-chat" : "knowledge-chat";
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
    body: JSON.stringify(buildChatBody(params, isAgentChat, Boolean(embed))),
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
