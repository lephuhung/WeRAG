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

export interface AttachmentResponse {
  success: boolean;
  data: TemporaryAttachment;
}

export function uploadTemporaryAttachment(
  sessionId: string,
  file: File,
  agentId?: string,
  agentSourceTenantId?: string,
  parserEngine?: string,
  onProgress?: (percent: number) => void,
): Promise<AttachmentResponse> {
  const form = new FormData();
  form.append("file", file);
  if (agentId) form.append("agent_id", agentId);
  if (agentSourceTenantId) form.append("agent_source_tenant_id", agentSourceTenantId);
  if (parserEngine) form.append("parser_engine", parserEngine);
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
