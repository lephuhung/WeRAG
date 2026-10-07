/* Document-assistant workspace: one editable .docx per chat session, opened
 * in an embedded ONLYOFFICE editor.
 *
 *   GET  /api/v1/sessions/:id/document            → 200 view | 404 none | 503 editor disabled
 *   POST /api/v1/sessions/:id/document            {attachment_id} → 201 view | 409 exists
 *   POST /api/v1/sessions/:id/document/forcesave  → 202 {revision}
 *   GET  /api/v1/sessions/:id/document/download   → docx bytes
 */
import { ApiError, apiDownload, apiGet, apiPost, authHeaders } from "../api-client.ts";

export type DocumentWorkspaceStatus = "open" | "closed";

export interface DocumentEditorBootstrap {
  /** Public base URL of the ONLYOFFICE Document Server (no trailing slash needed). */
  document_server_url: string;
  /** Complete, server-signed DocsAPI.DocEditor config (without `events`). */
  config: Record<string, unknown>;
}

export interface DocumentWorkspaceView {
  id: string;
  session_id: string;
  file_name: string;
  file_type: "docx";
  file_size: number;
  revision: number;
  status: DocumentWorkspaceStatus;
  save_count: number;
  last_saved_at?: string;
  closed_at?: string;
  created_at: string;
  updated_at: string;
  editor_key: string;
  editor?: DocumentEditorBootstrap;
}

/** Editor selection attached to a chat turn (`document_selection` in the body). */
export interface DocumentSelection {
  text: string;
  paragraph_hint?: string;
}

export type DocumentWorkspaceErrorCode = "editor_disabled" | "already_exists" | "request_failed";

export class DocumentWorkspaceError extends Error {
  code: DocumentWorkspaceErrorCode;
  status: number;
  constructor(code: DocumentWorkspaceErrorCode, status: number, message: string) {
    super(message);
    this.code = code;
    this.status = status;
  }
}

type Envelope<T> = { success?: boolean; data?: T };

const base = (sessionId: string) => `/api/v1/sessions/${encodeURIComponent(sessionId)}/document`;

function toWorkspaceError(err: unknown): unknown {
  if (err instanceof ApiError) {
    if (err.status === 503) return new DocumentWorkspaceError("editor_disabled", 503, err.message);
    if (err.status === 409) return new DocumentWorkspaceError("already_exists", 409, err.message);
    return new DocumentWorkspaceError("request_failed", err.status, err.message);
  }
  return err;
}

/** Returns null when the session has no workspace yet (404). Throws
 * DocumentWorkspaceError{code:"editor_disabled"} on 503. */
export async function getDocumentWorkspace(sessionId: string): Promise<DocumentWorkspaceView | null> {
  try {
    const res = await apiGet<Envelope<DocumentWorkspaceView>>(base(sessionId));
    return res?.data ?? null;
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) return null;
    throw toWorkspaceError(err);
  }
}

/** Opens a workspace from an already-uploaded session attachment. A 409
 * surfaces as DocumentWorkspaceError{code:"already_exists"}. */
export async function createDocumentWorkspace(
  sessionId: string,
  attachmentId: string,
): Promise<DocumentWorkspaceView> {
  try {
    const res = await apiPost<Envelope<DocumentWorkspaceView>>(base(sessionId), {
      attachment_id: attachmentId,
    });
    if (!res?.data) throw new DocumentWorkspaceError("request_failed", 0, "Empty workspace response");
    return res.data;
  } catch (err) {
    throw toWorkspaceError(err);
  }
}

export async function forceSaveDocumentWorkspace(sessionId: string): Promise<{ revision: number }> {
  try {
    const res = await apiPost<Envelope<{ revision: number }>>(`${base(sessionId)}/forcesave`, {});
    return { revision: Number(res?.data?.revision) || 0 };
  } catch (err) {
    throw toWorkspaceError(err);
  }
}

export function downloadDocumentWorkspace(sessionId: string): Promise<Blob> {
  return apiDownload(`${base(sessionId)}/download`);
}

/** Fire-and-forget forcesave for beforeunload/pagehide: `keepalive` lets the
 * request outlive the page; no refresh/replay is possible at that point. */
export function forceSaveDocumentWorkspaceKeepalive(sessionId: string): Promise<boolean> {
  try {
    return fetch(`${base(sessionId)}/forcesave`, {
      method: "POST",
      keepalive: true,
      headers: { "Content-Type": "application/json", ...authHeaders() },
      body: "{}",
    })
      .then((res) => res.ok)
      .catch(() => false);
  } catch {
    return Promise.resolve(false);
  }
}

/** Origin of the Document Server, used to authenticate postMessage events
 * coming from the WeRAG plugin. Null for an unparseable URL. */
export function documentServerOrigin(url: string | undefined | null): string | null {
  if (!url) return null;
  try {
    return new URL(url).origin;
  } catch {
    return null;
  }
}

/** True for attachments that can be opened in the workspace (.docx/.doc). */
export function isWordAttachment(fileName: string | undefined | null, fileType?: string | null): boolean {
  const ext = (fileType || fileName?.split(".").pop() || "").toLowerCase().replace(/^\./, "");
  return ext === "docx" || ext === "doc";
}

/** Parses a message posted by the werag-assistant ONLYOFFICE plugin. Returns
 * the selection (null for an empty one) or undefined when the payload is not
 * a WeRAG selection message at all. */
export function parsePluginSelectionMessage(data: unknown): DocumentSelection | null | undefined {
  if (!data || typeof data !== "object") return undefined;
  const msg = data as { source?: unknown; type?: unknown; text?: unknown; paragraphHint?: unknown };
  if (msg.source !== "werag-onlyoffice" || msg.type !== "selection") return undefined;
  const text = typeof msg.text === "string" ? msg.text.trim() : "";
  if (!text) return null;
  const hint = typeof msg.paragraphHint === "string" ? msg.paragraphHint.trim() : "";
  return hint ? { text, paragraph_hint: hint } : { text };
}

/** Revision carried by a document tool result, or null when the chunk is not
 * one (tool name not in the editing set or no numeric revision). */
export const DOCUMENT_EDIT_TOOLS = new Set(["apply_format_fixes", "rewrite_paragraphs"]);
export function documentRevisionFromToolData(
  toolName: string | undefined,
  data: Record<string, unknown> | undefined,
): number | null {
  const name = toolName || (typeof data?.tool_name === "string" ? data.tool_name : "");
  if (!DOCUMENT_EDIT_TOOLS.has(name)) return null;
  const raw = data?.document_revision;
  const n = typeof raw === "number" ? raw : typeof raw === "string" && raw.trim() ? Number(raw) : NaN;
  return Number.isFinite(n) ? n : null;
}

/** Whether the open editor should swap to the workspace's current version.
 * `editor_key` (not the revision) is the identity: a final save rotates the
 * key without any tool result, and the same key must never be applied twice.
 * The swap waits while the editor is still loading (before onDocumentReady)
 * or while a previous refresh is still in flight; the caller re-checks later. */
export function shouldRefreshEditor(s: {
  currentKey: string | null | undefined;
  nextKey: string | null | undefined;
  editorReady: boolean;
  inFlight: boolean;
}): boolean {
  if (!s.nextKey || s.nextKey === s.currentKey) return false;
  return s.editorReady && !s.inFlight;
}

export interface OpenDocumentInNewSessionDeps<F> {
  /** POST /sessions {} → new session id. */
  createSession: () => Promise<string>;
  /** Upload `file` as a temporary attachment of the session → attachment id. */
  upload: (sessionId: string, file: F) => Promise<string>;
  /** POST /sessions/:id/document {attachment_id}. */
  createWorkspace: (sessionId: string, attachmentId: string) => Promise<unknown>;
  /** Best-effort cleanup of the fresh session when a later step fails. */
  deleteSession: (sessionId: string) => Promise<unknown>;
}

/** Pre-session document assistant: pick a file → create a session → upload
 * it → open the workspace. Resolves with the new session id. Any failure
 * after the session exists deletes it best-effort (never masking the
 * original error) and rethrows, so the caller can keep the file for retry.
 * A 409 on the workspace means it is already open — treated as success. */
export async function openDocumentInNewSession<F>(file: F, deps: OpenDocumentInNewSessionDeps<F>): Promise<string> {
  const sessionId = await deps.createSession();
  if (!sessionId) throw new Error("Failed to create session");
  try {
    const attachmentId = await deps.upload(sessionId, file);
    if (!attachmentId) throw new Error("upload returned no attachment id");
    try {
      await deps.createWorkspace(sessionId, attachmentId);
    } catch (err) {
      if (!(err instanceof DocumentWorkspaceError && err.code === "already_exists")) throw err;
    }
    return sessionId;
  } catch (err) {
    try {
      await deps.deleteSession(sessionId);
    } catch {
      /* orphan cleanup must not mask the original error */
    }
    throw err;
  }
}

/** Normalizes a persisted/optimistic `document_selection` for display:
 * undefined unless it carries non-empty text. */
export function documentSelectionForDisplay(raw: unknown): DocumentSelection | undefined {
  if (!raw || typeof raw !== "object") return undefined;
  const r = raw as { text?: unknown; paragraph_hint?: unknown };
  const text = typeof r.text === "string" ? r.text.trim() : "";
  if (!text) return undefined;
  const hint = typeof r.paragraph_hint === "string" ? r.paragraph_hint.trim() : "";
  return hint ? { text, paragraph_hint: hint } : { text };
}

/** Whether the quoted selection in a user bubble should start collapsed
 * (~4 lines): more than `maxLines` lines, or longer than `maxChars`
 * (wrapped lines). */
export const SELECTION_PREVIEW_MAX_LINES = 4;
export const SELECTION_PREVIEW_MAX_CHARS = 280;
export function selectionNeedsCollapse(
  text: string,
  maxChars = SELECTION_PREVIEW_MAX_CHARS,
  maxLines = SELECTION_PREVIEW_MAX_LINES,
): boolean {
  const t = text.trim();
  if (!t) return false;
  return t.length > maxChars || t.split(/\r?\n/).length > maxLines;
}
