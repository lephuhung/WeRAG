/* Document-assistant workspaces. Every document of a session has a role:
 * a target (văn bản làm việc) is an editable .docx with its own ONLYOFFICE
 * editor tab (up to 4); a source (tài liệu nguồn) is a file uploaded at chat
 * (docx, pdf, xlsx, scans…, up to 10) that the assistant only looks up — no
 * tab, never edited. Both share the vb1…vbN handles.
 *
 *   GET    /api/v1/sessions/:id/documents             → {documents (both roles), active_id, max_documents, max_sources, scope, …}
 *   PUT    /api/v1/sessions/:id/documents/scope       {document_ids, sections, task, set_by: "user"} → scope
 *   DELETE /api/v1/sessions/:id/documents/scope
 *   POST   /api/v1/sessions/:id/documents             {attachment_id} → 201 view (a source of that upload is promoted) | 409 limit | 400 too large
 *   POST   /api/v1/sessions/:id/documents/:doc/role   {role: "target"|"source"} → view | 409 limit | 400 not Word
 *   GET    /api/v1/sessions/:id/documents/:doc        → 200 view | 404 none | 503 editor disabled
 *   DELETE /api/v1/sessions/:id/documents/:doc        → closes the tab
 *   POST   /api/v1/sessions/:id/documents/:doc/activate
 *   POST   /api/v1/sessions/:id/documents/:doc/forcesave  → 202 {revision}
 *   GET    /api/v1/sessions/:id/documents/:doc/download   → docx bytes
 *   GET    /api/v1/sessions/:id/documents/:doc/revisions  → [{seq,label,source,created_at}]
 *   GET    /api/v1/sessions/:id/documents/:doc/format-check → {check, report}
 *   POST   /api/v1/sessions/:id/documents/:doc/revisions/:seq/restore → {revision, editor_key}
 *   POST   /api/v1/sessions/:id/documents/:doc/proposals/apply {batch_id, variant_id} → {snapshot_seq}
 *   POST   /api/v1/sessions/:id/documents/:doc/content?revision=N  <docx bytes> → {revision, file_size, last_saved_at}
 *          (Word add-in: the taskpane uploads the file Word holds; 409 after a restore it has not loaded)
 *
 * Every per-document call takes an optional documentId; without one it uses
 * the legacy /document routes, which act on the session's active document.
 */
import { ApiError, apiDel, apiDownload, apiGet, apiPost, apiPut, authHeaders, refreshAccessToken } from "../api-client.ts";
import { parseDocumentScope, type DocumentScope, type DocumentScopeSection, type DocumentScopeTask } from "../document-scope.ts";

export type DocumentWorkspaceStatus = "open" | "closed";

/** onlyoffice: the embedded editor; word_addin: Microsoft Word through the
 * add-in, whose taskpane uploads the file. */
export type DocumentEditorKind = "onlyoffice" | "word_addin";

/** target: editor tab; source: chat upload, looked up only. */
export type DocumentWorkspaceRole = "target" | "source";

/** A source's parsed text: copied from the upload once parsed. */
export type DocumentSourceTextStatus = "processing" | "ready" | "failed";

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
  /** "docx" for a target; a source keeps its extension (pdf, xlsx, …). */
  file_type: string;
  /** Missing on servers that predate roles: a target. */
  role?: DocumentWorkspaceRole;
  /** Sources only. */
  text_status?: DocumentSourceTextStatus;
  file_size: number;
  revision: number;
  status: DocumentWorkspaceStatus;
  /** Missing on servers that predate the Word add-in: onlyoffice. */
  editor_kind?: DocumentEditorKind;
  save_count: number;
  last_saved_at?: string;
  closed_at?: string;
  created_at: string;
  updated_at: string;
  editor_key: string;
  editor?: DocumentEditorBootstrap;
  /** Background NĐ30 format check started when the document was opened. */
  format_check?: DocumentFormatCheck;
  /** The document's card (both roles), made in the background. */
  profile?: DocumentProfile;
}

/** queued: waiting for a background slot (shared with the format check). */
export type DocumentProfileStatus = "queued" | "running" | "ready" | "failed";

export interface DocumentProfileSection {
  title: string;
  /** Inclusive paragraph (target) or chunk/line (source) indexes. */
  from: number;
  to: number;
  summary?: string;
}

/** The card of a session document: identity, gist, sections and the
 * questions it answers. Shown fields are empty until status is "ready". */
export interface DocumentProfile {
  status: DocumentProfileStatus;
  document_number?: string;
  issuer?: string;
  date?: string;
  doc_type?: string;
  doc_type_code?: string;
  subject?: string;
  gist?: string;
  key_points?: string[];
  sections?: DocumentProfileSection[];
  topics?: string[];
  entities?: { units?: string[]; cited_numbers?: string[]; dates?: string[]; figures?: string[] };
  typical_questions?: string[];
  unit?: "paragraph" | "chunk" | "line";
  model?: string;
  generated_at?: string;
  /** Edited since the profile was made (a refresh is planned). */
  stale?: boolean;
  error?: string;
  started_at?: string;
}

/** queued: waiting for one of the few background check slots (several
 * documents opened at once are checked a couple at a time). */
export type DocumentFormatCheckStatus = "queued" | "running" | "ready" | "failed";

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
  max_sources: number;
  max_file_bytes: number;
  max_media_bytes: number;
  /** The session's document scope (user or router), null when none. */
  scope: DocumentScope | null;
}

/** Fallback limits (the server's are authoritative). */
export const MAX_DOCUMENTS_PER_SESSION = 4;
export const MAX_SOURCES_PER_SESSION = 10;
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
      max_sources: Number(d.max_sources) || MAX_SOURCES_PER_SESSION,
      max_file_bytes: Number(d.max_file_bytes) || MAX_DOCUMENT_FILE_BYTES,
      max_media_bytes: Number(d.max_media_bytes) || 0,
      scope: parseDocumentScope((d as { scope?: unknown }).scope),
    };
  } catch (err) {
    throw toWorkspaceError(err);
  }
}

/** Sets the session's scope from the user (the clarification card, the
 * scope chip); the next turns read it until it is cleared. */
export async function setDocumentScope(
  sessionId: string,
  scope: { document_ids: string[]; sections?: DocumentScopeSection[]; task?: DocumentScopeTask; set_by: "user" },
): Promise<DocumentScope | null> {
  try {
    const res = await apiPut<Envelope<unknown>>(`${sessionBase(sessionId)}/documents/scope`, scope);
    return parseDocumentScope(res?.data);
  } catch (err) {
    throw toWorkspaceError(err);
  }
}

/** Clears the session's scope (the user's or the router's). */
export async function clearDocumentScope(sessionId: string): Promise<void> {
  try {
    await apiDel(`${sessionBase(sessionId)}/documents/scope`);
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
  editorKind?: DocumentEditorKind,
): Promise<DocumentWorkspaceView> {
  try {
    const res = await apiPost<Envelope<DocumentWorkspaceView>>(`${sessionBase(sessionId)}/documents`, {
      attachment_id: attachmentId,
      ...(editorKind ? { editor_kind: editorKind } : {}),
    });
    if (!res?.data) throw new DocumentWorkspaceError("request_failed", 0, "Empty workspace response");
    return res.data;
  } catch (err) {
    throw toWorkspaceError(err);
  }
}

/** Switches a document's role: "target" opens a Word source in the editor
 * (it keeps its handle), "source" takes a tab out of the editor and keeps
 * its text for lookups. A 409 (limit reached) surfaces as
 * DocumentWorkspaceError{code:"already_exists"} with the server's message. */
export async function setDocumentWorkspaceRole(
  sessionId: string,
  documentId: string,
  role: DocumentWorkspaceRole,
): Promise<DocumentWorkspaceView> {
  try {
    const res = await apiPost<Envelope<DocumentWorkspaceView>>(`${base(sessionId, documentId)}/role`, { role });
    if (!res?.data) throw new DocumentWorkspaceError("request_failed", 0, "Empty workspace response");
    return res.data;
  } catch (err) {
    throw toWorkspaceError(err);
  }
}

/** A source document (chat upload); a row without a role is a target. */
export function isSourceDocument(doc: { role?: string | null } | null | undefined): boolean {
  return doc?.role === "source";
}

/** Splits the session's documents (handle order kept): targets are the
 * editor tabs, sources the lookup-only uploads. */
export function splitDocumentsByRole<T extends { role?: string | null }>(docs: T[]): { targets: T[]; sources: T[] } {
  const targets: T[] = [];
  const sources: T[] = [];
  for (const d of docs) (isSourceDocument(d) ? sources : targets).push(d);
  return { targets, sources };
}

/** Session uploads that are Word files, parsed or parsing (not failed),
 * and have no document of either role — e.g. uploaded before roles existed.
 * They can still be opened for editing through POST /documents. */
export function unopenedWordUploads<A extends { id: string; file_name: string; file_type?: string; status?: string }>(
  attachments: A[],
  docs: { attachment_id?: string }[],
): A[] {
  const used = new Set(docs.map((d) => d.attachment_id).filter(Boolean));
  return attachments.filter((a) => isWordAttachment(a.file_name, a.file_type) && a.status !== "failed" && !used.has(a.id));
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

/** Word add-in: stores the file Word holds as the document's latest
 * version. `revision` is the one the taskpane last saw; a 409 means a
 * restore happened since (reload the document into Word first). */
export async function uploadDocumentContent(
  sessionId: string,
  documentId: string,
  revision: number,
  docx: Uint8Array,
): Promise<{ revision: number; fileSize: number; lastSavedAt?: string }> {
  const url = `${base(sessionId, documentId)}/content?revision=${encodeURIComponent(String(revision))}`;
  const send = () =>
    fetch(url, {
      method: "POST",
      headers: { ...authHeaders(), "Content-Type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document" },
      body: docx as BodyInit,
    });
  let res = await send();
  if (res.status === 401) {
    await refreshAccessToken();
    res = await send();
  }
  const payload = (await res.json().catch(() => null)) as Envelope<{
    revision: number;
    file_size: number;
    last_saved_at?: string;
  }> & { error?: { message?: string } | string; message?: string } | null;
  if (!res.ok) {
    const msg =
      (typeof payload?.error === "string" ? payload.error : payload?.error?.message) ?? payload?.message ?? "Upload failed";
    throw toWorkspaceError(new ApiError(res.status, msg, payload));
  }
  return {
    revision: Number(payload?.data?.revision) || 0,
    fileSize: Number(payload?.data?.file_size) || 0,
    lastSavedAt: payload?.data?.last_saved_at,
  };
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

/** Takes the AI snapshot (undo point) before the user's chosen version of a
 * rewrite proposal is applied in the editor. */
export async function applyRewriteProposal(
  sessionId: string,
  documentId: string,
  batchId: string,
  variantId: string,
): Promise<{ snapshotSeq: number }> {
  try {
    const res = await apiPost<Envelope<{ snapshot_seq: number }>>(`${base(sessionId, documentId)}/proposals/apply`, {
      batch_id: batchId,
      variant_id: variantId,
    });
    return { snapshotSeq: Number(res?.data?.snapshot_seq) || 0 };
  } catch (err) {
    throw toWorkspaceError(err);
  }
}

/** The evaluation of a finished background format check. */
export interface DocumentFormatReport {
  file_name: string;
  document_type?: string;
  document_type_label?: string;
  summary?: { pass: number; fail: number; warn: number; skip: number };
  /** Markdown judgment against NĐ30/2020/NĐ-CP. */
  evaluation: string;
  checked_at: string;
}

/** GET …/documents/:doc/format-check: the check state and, once ready, its
 * evaluation (null when the server no longer keeps it). */
export async function getDocumentFormatCheck(
  sessionId: string,
  documentId: string,
): Promise<{ check: DocumentFormatCheck | null; report: DocumentFormatReport | null }> {
  try {
    const res = await apiGet<Envelope<{ check: DocumentFormatCheck | null; report: DocumentFormatReport | null }>>(
      `${base(sessionId, documentId)}/format-check`,
    );
    return { check: res?.data?.check ?? null, report: res?.data?.report ?? null };
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
/** A check that has not finished yet (waiting for a slot or running). */
export function formatCheckInProgress(check: DocumentFormatCheck): boolean {
  return check.status === "queued" || check.status === "running";
}

export function formatCheckIsCurrent(
  check: DocumentFormatCheck,
  doc: { revision: number; last_saved_at?: string },
): boolean {
  if (formatCheckInProgress(check)) return true;
  if (doc.revision > check.revision) return false;
  const saved = doc.last_saved_at ? Date.parse(doc.last_saved_at) : NaN;
  const started = Date.parse(check.checked_saved_at ?? check.started_at);
  return !(Number.isFinite(saved) && Number.isFinite(started) && saved > started);
}

/** A profile that has not finished yet (waiting for a slot or running). */
export function profileInProgress(p: { status?: string } | null | undefined): boolean {
  return p?.status === "queued" || p?.status === "running";
}

/** The header ring shows "reading the document" while the visible
 * document's profile is made and its format check has not started yet
 * (none, or still waiting for a slot): the profile runs first. */
export function profileReadingPhase(
  check: { status: string } | null | undefined,
  profile: { status?: string } | null | undefined,
): boolean {
  return profileInProgress(profile) && (!check || check.status === "queued");
}

/** One line under a source in the sources panel: its số ký hiệu and gist,
 * once its profile is ready ("" otherwise). */
export function sourceProfileLine(p: DocumentProfile | null | undefined): string {
  if (!p || p.status !== "ready") return "";
  return [p.document_number, p.gist || p.subject].filter((x) => x && x.trim()).join(" · ");
}

export interface SourceQuestionChip {
  documentId: string;
  handle?: string;
  fileName: string;
  question: string;
}

/** Suggestion chips for sources uploaded in this view (freshIds) whose
 * profile is ready: their typical questions, at most `perSource` each and
 * `limit` in all, in handle order. */
export function sourceQuestionChips(
  docs: { id: string; file_name: string; handle?: string; role?: string; profile?: { status?: string; typical_questions?: string[] } }[],
  freshIds: ReadonlySet<string>,
  perSource = 3,
  limit = 3,
): SourceQuestionChip[] {
  const out: SourceQuestionChip[] = [];
  for (const d of docs) {
    if (d.role !== "source" || !freshIds.has(d.id) || d.profile?.status !== "ready") continue;
    for (const q of (d.profile.typical_questions ?? []).slice(0, perSource)) {
      const question = q.trim();
      if (!question) continue;
      out.push({ documentId: d.id, handle: d.handle, fileName: d.file_name, question });
      if (out.length >= limit) return out;
    }
  }
  return out;
}

/** Identity of one background check result: the chat's format-check ring
 * shows an unseen dot until this result is opened. */
export function formatCheckResultKey(scopeId: string, check: DocumentFormatCheck): string {
  return `${scopeId}|${check.status}|${check.revision}|${check.finished_at ?? check.started_at}`;
}

/** Typical duration of the background check (segmentation + reasoning). */
export const FORMAT_CHECK_EXPECTED_MS = 75_000;

/** Typical duration of a document profile (one thinking-off call). */
export const PROFILE_EXPECTED_MS = 25_000;

/** Estimated progress (0–0.95) of a running check from its start time: the
 * backend reports no percentage, so the ring follows the typical duration
 * and stops short of full until the check reports done. */
export function formatCheckProgress(startedAt: string, nowMs: number, expectedMs = FORMAT_CHECK_EXPECTED_MS): number {
  const start = Date.parse(startedAt);
  if (!Number.isFinite(start)) return 0.05;
  const ratio = Math.max(0, nowMs - start) / expectedMs;
  // eases out: fast at first, slows as it nears the cap
  return Math.min(0.95, Math.max(0.05, 1 - Math.exp(-2.2 * ratio)));
}
