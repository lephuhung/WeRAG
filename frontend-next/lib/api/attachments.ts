/* Ported from frontend/src/api/chat/temporary-attachments.ts. */
import { apiDel, apiDownload, apiGet, apiUpload } from "@/lib/api-client";

export type TemporaryAttachmentStatus = "uploaded" | "processing" | "ready" | "failed";

export interface TemporaryAttachment {
  id: string;
  session_id: string;
  file_name: string;
  file_type: string;
  file_size: number;
  mime_type?: string;
  status: TemporaryAttachmentStatus;
  token_count: number;
  chunk_count: number;
  image_refs?: Array<{ original_ref?: string; url: string; mime_type?: string }>;
  error_message?: string;
  expires_at: string;
}

/** The source document a chat upload of the document assistant became. */
export interface AttachmentSourceDocument {
  id: string;
  handle?: string;
  role?: "target" | "source";
  file_type?: string;
}

export interface AttachmentResponse {
  success: boolean;
  data: TemporaryAttachment;
  /** Document assistant: the upload was recorded as a source document. */
  document?: AttachmentSourceDocument;
}

export function uploadTemporaryAttachment(
  sessionId: string,
  file: File,
  agentId?: string,
  parserEngine?: string,
  onProgress?: (percent: number) => void,
  /** "target": the editor pane opens the upload as a tab right after, so the
   * document assistant must not record it as a source first. */
  documentRole?: "target",
): Promise<AttachmentResponse> {
  const form = new FormData();
  form.append("file", file);
  if (agentId) form.append("agent_id", agentId);
  if (parserEngine) form.append("parser_engine", parserEngine);
  if (documentRole) form.append("document_role", documentRole);
  return apiUpload<AttachmentResponse>(
    `/api/v1/sessions/${sessionId}/attachments`,
    form,
    onProgress,
  );
}

export function getTemporaryAttachment(
  sessionId: string,
  attachmentId: string,
): Promise<AttachmentResponse> {
  return apiGet(`/api/v1/sessions/${sessionId}/attachments/${attachmentId}`);
}

export function previewTemporaryAttachment(
  sessionId: string,
  attachmentId: string,
): Promise<Blob> {
  return apiDownload(`/api/v1/sessions/${sessionId}/attachments/${attachmentId}/preview`);
}

export function deleteTemporaryAttachment(
  sessionId: string,
  attachmentId: string,
): Promise<void> {
  return apiDel(`/api/v1/sessions/${sessionId}/attachments/${attachmentId}`);
}

/** GET /api/v1/sessions/:id/attachments — every temporary attachment of the
 * session. Tolerates both the `{success,data:[...]}` envelope and a bare
 * array so a backend shape tweak does not blank the list. */
export async function listTemporaryAttachments(
  sessionId: string,
): Promise<TemporaryAttachment[]> {
  const res = await apiGet<{ success?: boolean; data?: TemporaryAttachment[] } | TemporaryAttachment[]>(
    `/api/v1/sessions/${sessionId}/attachments`,
  );
  if (Array.isArray(res)) return res;
  return Array.isArray(res?.data) ? res.data : [];
}
