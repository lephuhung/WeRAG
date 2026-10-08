/* Document-assistant workspaces: up to 4 editable .docx files per chat
 * session, one ONLYOFFICE editor tab each.
 *
 *   GET    /api/v1/sessions/:id/documents             → {documents, active_id, max_documents, …}
 *   POST   /api/v1/sessions/:id/documents             {attachment_id} → 201 view | 409 limit | 400 too large
 *   GET    /api/v1/sessions/:id/documents/:doc        → 200 view | 404 none | 503 editor disabled
 *   DELETE /api/v1/sessions/:id/documents/:doc        → closes the tab
 *   POST   /api/v1/sessions/:id/documents/:doc/activate
 *   POST   /api/v1/sessions/:id/documents/:doc/forcesave  → 202 {revision}
 *   GET    /api/v1/sessions/:id/documents/:doc/download   → docx bytes
 *   GET    /api/v1/sessions/:id/documents/:doc/revisions  → [{seq,label,source,created_at}]
 *   POST   /api/v1/sessions/:id/documents/:doc/revisions/:seq/restore → {revision, editor_key}
 *
 * Every per-document call takes an optional documentId; without one it uses
 * the legacy /document routes, which act on the session's active document.
 */
import { ApiError, apiDel, apiDownload, apiGet, apiPost, authHeaders } from "../api-client.ts";

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
  /** The agent's name for the document (vb1, vb2, …). */
  handle?: string;
  /** 1-based order the document was opened in (tab order). */
  position?: number;
  attachment_id?: string;
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
  /** Background NĐ30 format check started when the document was opened. */
  format_check?: DocumentFormatCheck;
}

export type DocumentFormatCheckStatus = "running" | "ready" | "failed";

export interface DocumentFormatCheck {
  status: DocumentFormatCheckStatus;
  /** Workspace revision that was checked; a newer revision makes it stale. */
  revision: number;
  /** Detected rule set slug (e.g. quy_che) and its name (e.g. "Quy chế"). */
  document_type?: string;
  document_type_label?: string;
  started_at: string;
  finished_at?: string;
  /** Latest save the result still describes (moved forward when a save
   * left the format unchanged); defaults to started_at. */
  checked_saved_at?: string;
}

/** Editor selection attached to a chat turn (`document_selection` in the body). */
export interface DocumentSelection {
  text: string;
  paragraph_hint?: string;
  /** Workspace (tab) the text was selected in. */
  document_id?: string;
  /** Server-side label of that document ("vb2 · Tờ trình.docx"), on stored messages. */
  document?: string;
}

/** GET /sessions/:id/documents. */
export interface DocumentWorkspaceList {
  documents: DocumentWorkspaceView[];
  active_id: string;
  max_documents: number;
  max_file_bytes: number;
  max_media_bytes: number;
}

/** Fallback limits (the server's are authoritative). */
export const MAX_DOCUMENTS_PER_SESSION = 4;
export const MAX_DOCUMENT_FILE_BYTES = 10 * 1024 * 1024;

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

const sessionBase = (sessionId: string) => `/api/v1/sessions/${encodeURIComponent(sessionId)}`;
const base = (sessionId: string, documentId?: string) =>
  documentId
    ? `${sessionBase(sessionId)}/documents/${encodeURIComponent(documentId)}`
    : `${sessionBase(sessionId)}/document`;

function toWorkspaceError(err: unknown): unknown {
  if (err instanceof ApiError) {
    if (err.status === 503) return new DocumentWorkspaceError("editor_disabled", 503, err.message);
    if (err.status === 409) return new DocumentWorkspaceError("already_exists", 409, err.message);
    return new DocumentWorkspaceError("request_failed", err.status, err.message);
  }
  return err;
}

/** Returns null when the session has no such document (404). Throws
 * DocumentWorkspaceError{code:"editor_disabled"} on 503. Without
 * documentId it loads the session's active document. */
export async function getDocumentWorkspace(
  sessionId: string,
  documentId?: string,
): Promise<DocumentWorkspaceView | null> {
  try {
    const res = await apiGet<Envelope<DocumentWorkspaceView>>(base(sessionId, documentId));
    return res?.data ?? null;
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) return null;
    throw toWorkspaceError(err);
  }
}

/** The session's documents in tab order (no editor configs). */
export async function listDocumentWorkspaces(sessionId: string): Promise<DocumentWorkspaceList> {
  try {
    const res = await apiGet<Envelope<Partial<DocumentWorkspaceList>>>(`${sessionBase(sessionId)}/documents`);
    const d = res?.data ?? {};
    return {
      documents: Array.isArray(d.documents) ? d.documents : [],
      active_id: typeof d.active_id === "string" ? d.active_id : "",
      max_documents: Number(d.max_documents) || MAX_DOCUMENTS_PER_SESSION,
      max_file_bytes: Number(d.max_file_bytes) || MAX_DOCUMENT_FILE_BYTES,
      max_media_bytes: Number(d.max_media_bytes) || 0,
    };
  } catch (err) {
    throw toWorkspaceError(err);
  }
}

/** Opens an already-uploaded session attachment as a new document (tab);
 * opening the same upload again returns its existing document. A 409 (the
 * session already holds the maximum) surfaces as
 * DocumentWorkspaceError{code:"already_exists"} with the server's message. */
export async function createDocumentWorkspace(
  sessionId: string,
  attachmentId: string,
): Promise<DocumentWorkspaceView> {
  try {
    const res = await apiPost<Envelope<DocumentWorkspaceView>>(`${sessionBase(sessionId)}/documents`, {
      attachment_id: attachmentId,
    });
    if (!res?.data) throw new DocumentWorkspaceError("request_failed", 0, "Empty workspace response");
    return res.data;
  } catch (err) {
    throw toWorkspaceError(err);
  }
}

/** Records the tab the user switched to (the agent's default document). */
export async function activateDocumentWorkspace(sessionId: string, documentId: string): Promise<void> {
  try {
    await apiPost(`${base(sessionId, documentId)}/activate`, {});
  } catch (err) {
    throw toWorkspaceError(err);
  }
}

/** Closes a document's tab (the server snapshots it first). */
export async function closeDocumentWorkspace(sessionId: string, documentId: string): Promise<void> {
  try {
    await apiDel(base(sessionId, documentId));
  } catch (err) {
    throw toWorkspaceError(err);
  }
}

export async function forceSaveDocumentWorkspace(sessionId: string, documentId?: string): Promise<{ revision: number }> {
  try {
    const res = await apiPost<Envelope<{ revision: number }>>(`${base(sessionId, documentId)}/forcesave`, {});
    return { revision: Number(res?.data?.revision) || 0 };
  } catch (err) {
    throw toWorkspaceError(err);
  }
}

export function downloadDocumentWorkspace(sessionId: string, documentId?: string): Promise<Blob> {
  return apiDownload(`${base(sessionId, documentId)}/download`);
}

/** Fire-and-forget forcesave for beforeunload/pagehide: `keepalive` lets the
 * request outlive the page; no refresh/replay is possible at that point. */
export function forceSaveDocumentWorkspaceKeepalive(sessionId: string, documentId?: string): Promise<boolean> {
  try {
    return fetch(`${base(sessionId, documentId)}/forcesave`, {
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
  /** POST /sessions/:id/documents {attachment_id}. */
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
  const r = raw as { text?: unknown; paragraph_hint?: unknown; document_id?: unknown; document?: unknown };
  const text = typeof r.text === "string" ? r.text.trim() : "";
  if (!text) return undefined;
  const out: DocumentSelection = { text };
  const hint = typeof r.paragraph_hint === "string" ? r.paragraph_hint.trim() : "";
  if (hint) out.paragraph_hint = hint;
  if (typeof r.document_id === "string" && r.document_id) out.document_id = r.document_id;
  if (typeof r.document === "string" && r.document.trim()) out.document = r.document.trim();
  return out;
}

/** Client-side guard before uploading a document (the server also checks
 * embedded pictures, which only it can measure). */
export function documentFileTooLarge(file: { size: number }, maxBytes = MAX_DOCUMENT_FILE_BYTES): boolean {
  return file.size > maxBytes;
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


/* ---------- snapshot timeline ---------- */

export type DocumentRevisionSource = "ai" | "manual" | "close" | "restore";

export interface DocumentRevisionEntry {
  seq: number;
  label: string;
  source: DocumentRevisionSource | string;
  created_at: string;
}

export async function listDocumentRevisions(sessionId: string, documentId?: string): Promise<DocumentRevisionEntry[]> {
  try {
    const res = await apiGet<Envelope<DocumentRevisionEntry[]>>(`${base(sessionId, documentId)}/revisions`);
    return Array.isArray(res?.data) ? res.data : [];
  } catch (err) {
    throw toWorkspaceError(err);
  }
}

/** Restores snapshot `seq`; the server rotates the editor key, so the
 * workspace re-check then reloads the editor through refreshFile(). */
export async function restoreDocumentRevision(
  sessionId: string,
  seq: number,
  documentId?: string,
): Promise<{ revision: number; editor_key: string }> {
  try {
    const res = await apiPost<Envelope<{ revision: number; editor_key: string }>>(
      `${base(sessionId, documentId)}/revisions/${encodeURIComponent(String(seq))}/restore`,
      {},
    );
    return { revision: Number(res?.data?.revision) || 0, editor_key: String(res?.data?.editor_key ?? "") };
  } catch (err) {
    throw toWorkspaceError(err);
  }
}

/** Timeline order: newest (highest seq) first; does not mutate the input. */
export function revisionsNewestFirst<T extends { seq: number }>(list: T[]): T[] {
  return [...list].sort((a, b) => b.seq - a.seq);
}

/** Latest snapshot of `source` (highest seq), or null. */
export function latestRevisionOf<T extends { seq: number; source: string }>(list: T[], source: string): T | null {
  let best: T | null = null;
  for (const r of list) if (r.source === source && (!best || r.seq > best.seq)) best = r;
  return best;
}

/** Whether a background check still describes the document. AI edits are
 * applied inside the editor and only show up as a save, so a finished check
 * is stale once the document was saved after the save it covers (or an
 * external write moved the revision past it). The backend moves the covered
 * save forward when an edit left the format unchanged, and starts a new
 * check when it did not. */
export function formatCheckIsCurrent(
  check: DocumentFormatCheck,
  doc: { revision: number; last_saved_at?: string },
): boolean {
  if (check.status === "running") return true;
  if (doc.revision > check.revision) return false;
  const saved = doc.last_saved_at ? Date.parse(doc.last_saved_at) : NaN;
  const started = Date.parse(check.checked_saved_at ?? check.started_at);
  return !(Number.isFinite(saved) && Number.isFinite(started) && saved > started);
}

/** Identity of one background check result: the chat's format-check ring
 * shows an unseen dot until this result is opened. */
export function formatCheckResultKey(scopeId: string, check: DocumentFormatCheck): string {
  return `${scopeId}|${check.status}|${check.revision}|${check.finished_at ?? check.started_at}`;
}

/** Typical duration of the background check (segmentation + reasoning). */
export const FORMAT_CHECK_EXPECTED_MS = 75_000;

/** Estimated progress (0–0.95) of a running check from its start time: the
 * backend reports no percentage, so the ring follows the typical duration
 * and stops short of full until the check reports done. */
export function formatCheckProgress(startedAt: string, nowMs: number): number {
  const start = Date.parse(startedAt);
  if (!Number.isFinite(start)) return 0.05;
  const ratio = Math.max(0, nowMs - start) / FORMAT_CHECK_EXPECTED_MS;
  // eases out: fast at first, slows as it nears the cap
  return Math.min(0.95, Math.max(0.05, 1 - Math.exp(-2.2 * ratio)));
}
