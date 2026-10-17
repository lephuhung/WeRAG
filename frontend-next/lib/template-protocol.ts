/* The chat-driven NĐ30 templates (prepare_document_template): the request
 * sent with a chat turn and the typed result the server answers with.
 * Copied from the Word add-in (src/lib/word/template-protocol.ts). */
import type { DocumentTemplateId } from './document-templates.ts';

export const TEMPLATE_CHOICES = [
  ['cong-van', 'Công văn'], ['ke-hoach', 'Kế hoạch'], ['bao-cao', 'Báo cáo'],
  ['to-trinh', 'Tờ trình'], ['thong-bao', 'Thông báo'], ['quyet-dinh', 'Quyết định cá biệt'],
] as const;
export function templatePrompt(id: DocumentTemplateId): string {
  const name = TEMPLATE_CHOICES.find(([key]) => key === id)?.[1];
  return `Tạo cho tôi văn bản ${name?.toLocaleLowerCase('vi-VN')} theo NĐ 30, thông tin tôi cần là: `;
}
export const TEMPLATE_HINT = 'Bạn có thể nhập: mục đích, nội dung chính, đối tượng nhận, thời gian, phạm vi và yêu cầu thực hiện. Bổ sung cơ quan hoặc người ký nếu khác thông tin đơn vị đã lưu; trợ lý sẽ hỏi tiếp những mục còn thiếu.';
export type TemplateRequest = { schema_version: 1; request_id: string; template_id: DocumentTemplateId; document_id?: string };
export type TemplateFields = {
  parent_agency?: string; agency: string; locality: string; date: string; number_symbol: string; subject: string;
  addressee?: string; grounds?: string[]; request_basis?: string; decision_authority?: string;
  paragraphs: { kind: 'body' | 'heading' | 'article'; text: string }[];
  recipients: string[]; signature_lines: string[]; signer_name: string;
};
export type TemplateResponse = Required<TemplateRequest> & (
  { status: 'needs_input'; missing_fields: string[] } | { status: 'ready'; fields: TemplateFields }
);
const record = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v);
const text = (v: unknown, max = 2000): v is string => typeof v === 'string' && !!v.trim() && v.length <= max && !/[\u0000-\u0008\u000B\u000C\u000E-\u001F]/.test(v);
const strings = (v: unknown, max: number) => Array.isArray(v) && v.length > 0 && v.length <= max && v.every(x => text(x));
/** Never parse assistant prose/Markdown into mutations. Only the explicit tool's typed JSON is accepted. */
export function parseTemplateResponse(value: unknown): TemplateResponse | null {
  if (!record(value) || value.schema_version !== 1 || !text(value.request_id, 128) || !text(value.document_id,128) || !TEMPLATE_CHOICES.some(([id]) => id === value.template_id)) return null;
  if (value.status === 'needs_input') return strings(value.missing_fields,30) ? value as TemplateResponse : null;
  if (value.status !== 'ready' || !record(value.fields)) return null;
  const f = value.fields;
  if (!['agency','locality','date','number_symbol','subject','signer_name'].every(key => text(f[key]))) return null;
  if (!strings(f.recipients,100) || !strings(f.signature_lines,5)) return null;
  if (!Array.isArray(f.paragraphs) || !f.paragraphs.length || f.paragraphs.length > 300 || !f.paragraphs.every(p => record(p) && ['body','heading','article'].includes(String(p.kind)) && text(p.text,20000))) return null;
  for (const key of ['parent_agency','addressee','request_basis','decision_authority']) if (f[key] !== undefined && !text(f[key])) return null;
  if (f.grounds !== undefined && !strings(f.grounds,50)) return null;
  if ((value.template_id === 'cong-van' || value.template_id === 'to-trinh') && !text(f.addressee)) return null;
  if (value.template_id === 'quyet-dinh' && (!strings(f.grounds,50) || !text(f.request_basis) || !text(f.decision_authority))) return null;
  return value as TemplateResponse;
}

type TemplateStore = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'> | null;
export type PendingTemplate = TemplateRequest & { sessionId?: string };
const cacheKey = (tenant: string, session: string, document: string) => `werag.template:${tenant}:${session}:${document}`;
export function saveTemplateRequest(store: TemplateStore, tenant: string, request: PendingTemplate): void {
  if (!request.sessionId || !request.document_id) return;
  try { store?.setItem(cacheKey(tenant, request.sessionId, request.document_id), JSON.stringify({ ...request, savedAt: Date.now() })); } catch { /* unavailable storage: current turn still works */ }
}
export function forgetTemplateRequest(store: TemplateStore, tenant: string, request: PendingTemplate | null): void {
  if (!request?.sessionId || !request.document_id) return;
  try {
    const key = cacheKey(tenant, request.sessionId, request.document_id);
    const current = JSON.parse(store?.getItem(key) ?? 'null');
    if (record(current) && current.request_id === request.request_id) store?.removeItem(key);
  } catch { /* unavailable */ }
}
export function restoreTemplateRequest(store: TemplateStore, tenant: string, session: string, document: string): PendingTemplate | null {
  try {
    const value: unknown = JSON.parse(store?.getItem(cacheKey(tenant, session, document)) ?? 'null');
    if (!record(value) || value.schema_version !== 1 || !text(value.request_id,128) || value.sessionId !== session || value.document_id !== document || !TEMPLATE_CHOICES.some(([id]) => id === value.template_id) || typeof value.savedAt !== 'number' || Date.now() - value.savedAt > 86400000) return null;
    return { schema_version: 1, request_id: value.request_id, template_id: value.template_id as DocumentTemplateId, document_id: document, sessionId: session };
  } catch { return null; }
}
