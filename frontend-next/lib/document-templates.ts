/** NĐ30 documents built in the page from a ready prepare_document_template
 * result (the server's checked fields; no organisation or signing authority
 * is inferred here). Copied from the Word add-in
 * (src/lib/word/document-templates.ts); keep the two in step. */
import type { TemplateFields } from './template-protocol.ts';
import { AlignmentType, BorderStyle, Document, Header, Packer, PageNumber, Paragraph, Table, TableCell, TableLayoutType, TableRow, TextRun, WidthType } from 'docx';

export const DOCUMENT_TEMPLATES = [
  { id: 'cong-van', name: 'Công văn' },
  { id: 'ke-hoach', name: 'Kế hoạch' },
  { id: 'bao-cao', name: 'Báo cáo' },
  { id: 'to-trinh', name: 'Tờ trình' },
  { id: 'thong-bao', name: 'Thông báo' },
  { id: 'quyet-dinh', name: 'Quyết định cá biệt' },
] as const;
export type DocumentTemplateId = typeof DOCUMENT_TEMPLATES[number]['id'];


// rounded, not docx's truncating convertMillimetersToTwip: A4 is exactly
// 11906 × 16838 twips, 30 mm is 1701
const mm = (v: number) => Math.round((v * 1440) / 25.4);
const noBorder = { style: BorderStyle.NONE, size: 0, color: 'FFFFFF' };
const borders = { top: noBorder, bottom: noBorder, left: noBorder, right: noBorder, insideHorizontal: noBorder, insideVertical: noBorder };
function p(text: string, options: { bold?: boolean; italic?: boolean; size?: number; center?: boolean; body?: boolean } = {}) {
  return new Paragraph({
    alignment: options.center ? AlignmentType.CENTER : options.body ? AlignmentType.JUSTIFIED : AlignmentType.LEFT,
    spacing: { before: 0, after: options.body ? 120 : 60, line: 276 },
    indent: options.body ? { firstLine: mm(10) } : undefined,
    children: text.split(/\r?\n/).map((line, index) => new TextRun({ text: line, break: index ? 1 : undefined, font: 'Times New Roman', size: (options.size ?? 14) * 2, bold: options.bold ?? false, italics: options.italic ?? false })),
  });
}
function rule(indent: number) {
  return new Paragraph({ spacing: { before: 0, after: 0, line: 20 }, indent: { left: indent, right: indent }, border: { top: { style: BorderStyle.SINGLE, size: 4, color: '000000', space: 0 } }, children: [new TextRun({ text: '', size: 2 })] });
}
function cell(children: Paragraph[], width: number) {
  return new TableCell({ width: { size: width, type: WidthType.DXA }, margins: { top: 0, bottom: 0, left: 0, right: 0 }, borders, children });
}
function table(rows: TableRow[]) {
  return new Table({ width: { size: 9071, type: WidthType.DXA }, columnWidths: [3500, 5571], layout: TableLayoutType.FIXED, borders, rows });
}

export async function buildDocumentTemplate(id: DocumentTemplateId, data?: TemplateFields): Promise<Uint8Array> {
  const spec = DOCUMENT_TEMPLATES.find(t => t.id === id);
  if (!spec) throw new Error('Loại văn bản không được hỗ trợ.');
  const letter = id === 'cong-van';
  const decision = id === 'quyet-dinh';
  const code = ({ 'cong-van': '[CQ]-[ĐV]', 'ke-hoach': 'KH-[CQ]', 'bao-cao': 'BC-[CQ]', 'to-trinh': 'TTr-[CQ]', 'thong-bao': 'TB-[CQ]', 'quyet-dinh': 'QĐ-[CQ]' })[id];
  const heading = table([
    new TableRow({ cantSplit: true, children: [
      cell([...(data && !data.parent_agency ? [] : [p(data?.parent_agency ?? '[CƠ QUAN CHỦ QUẢN]', { center: true, size: 13 })]), p(data?.agency ?? '[CƠ QUAN BAN HÀNH]', { center: true, bold: true, size: 13 }), rule(1167)], 3500),
      cell([p('CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM', { center: true, bold: true, size: 13 }), p('Độc lập - Tự do - Hạnh phúc', { center: true, bold: true, size: 14 }), rule(1100)], 5571),
    ] }),
    new TableRow({ cantSplit: true, children: [
      cell([p(data ? `Số: ${data.number_symbol}` : `Số: [SỐ]/${code}`, { center: true, size: 13 }), ...(letter ? [p(data ? `V/v ${data.subject}` : 'V/v [Trích yếu nội dung công văn]', { center: true, size: 12 })] : [])], 3500),
      cell([p(data ? `${data.locality}, ${data.date}` : '[Địa danh], ngày [DD] tháng [MM] năm [YYYY]', { center: true, italic: true, size: 13 })], 5571),
    ] }),
  ]);
  const body: Paragraph[] = [];
  if (!letter) {
    body.push(p(decision ? 'QUYẾT ĐỊNH' : spec.name.toLocaleUpperCase('vi-VN'), { center: true, bold: true }));
    body.push(p(data?.subject ?? '[Trích yếu nội dung văn bản]', { center: true, bold: true }));
    body.push(rule(3500));
  }
  if (data) {
    if (decision) {
      body.push(p(data.decision_authority!, { center: true, bold: true, size: 13 }));
      for (const line of data.grounds ?? []) body.push(p(line, { body: true, italic: true }));
      body.push(p(data.request_basis!, { body: true, italic: true }), p('QUYẾT ĐỊNH:', { center: true, bold: true }));
    } else if (data.addressee) body.push(p(`Kính gửi: ${data.addressee}`, { center: true }));
    for (const block of data.paragraphs) body.push(p(block.text, { body: true, bold: block.kind !== 'body' }));
  } else if (decision) {
    body.push(p('[CHỨC VỤ NGƯỜI CÓ THẨM QUYỀN BAN HÀNH]', { bold: true, center: true, size: 13 }));
    body.push(p('Căn cứ [Văn bản quy định chức năng, nhiệm vụ và thẩm quyền];', { body: true, italic: true }));
    body.push(p('Căn cứ [Căn cứ pháp lý liên quan];', { body: true, italic: true }));
    body.push(p('Theo đề nghị của [Đơn vị đề nghị].', { body: true, italic: true }));
    body.push(p('QUYẾT ĐỊNH:', { center: true, bold: true }));
    for (const text of ['Điều 1. [Nội dung quyết định]', 'Điều 2. [Tổ chức thực hiện]', 'Điều 3. [Hiệu lực và trách nhiệm thi hành]']) body.push(p(text, { body: true, bold: true }));
  } else {
    if (letter || id === 'to-trinh') body.push(p('Kính gửi: [Cơ quan, tổ chức hoặc người nhận]', { center: true }));
    const sections = id === 'ke-hoach' ? ['I. MỤC ĐÍCH, YÊU CẦU', 'II. NỘI DUNG, NHIỆM VỤ', 'III. TỔ CHỨC THỰC HIỆN']
      : id === 'bao-cao' ? ['I. TÌNH HÌNH, KẾT QUẢ', 'II. ĐÁNH GIÁ', 'III. KIẾN NGHỊ, ĐỀ XUẤT']
      : id === 'to-trinh' ? ['I. SỰ CẦN THIẾT', 'II. NỘI DUNG ĐỀ NGHỊ', 'III. KIẾN NGHỊ'] : [];
    if (sections.length) for (const title of sections) body.push(p(title, { body: true, bold: true }), p('[Điền nội dung]', { body: true }));
    else body.push(p('[Điền nội dung văn bản]', { body: true }), p('[Tổ chức thực hiện hoặc đề nghị phối hợp]./.', { body: true }));
  }
  const signature = table([new TableRow({ cantSplit: true, children: [
    cell([p('Nơi nhận:', { bold: true, italic: true, size: 12 }), ...(data ? data.recipients.map(line => p(line, { size: 11 })) : [p('- [Cơ quan, tổ chức nhận];', { size: 11 }), p('- Lưu: VT, [Đơn vị soạn thảo].', { size: 11 })])], 3500),
    cell([...(data ? data.signature_lines : ['[CHỨC VỤ NGƯỜI KÝ]']).map(line => p(line, { center: true, bold: true, size: 13 })), p('(Ký, ghi rõ họ tên, đóng dấu)', { center: true, italic: true, size: 13 }), p(''), p(''), p(data?.signer_name ?? '[Họ và tên]', { center: true, bold: true })], 5571),
  ] })]);
  const doc = new Document({
    compatibility: { version: 15, doNotExpandShiftReturn: true },
    creator: 'WeRAG', title: spec.name, description: 'Khung mẫu theo Nghị định 30/2020/NĐ-CP; điền các trường trong ngoặc vuông.',
    styles: { default: { document: { run: { font: 'Times New Roman', size: 28, color: '000000' }, paragraph: { spacing: { after: 120, line: 276 } } } } },
    sections: [{ properties: { titlePage: true, page: { size: { width: mm(210), height: mm(297) }, margin: { top: mm(20), bottom: mm(20), left: mm(30), right: mm(20), header: mm(10), footer: mm(10) } } },
      headers: { default: new Header({ children: [new Paragraph({ alignment: AlignmentType.CENTER, children: [new TextRun({ children: [PageNumber.CURRENT], font: 'Times New Roman', size: 26 })] })] }), first: new Header({ children: [p('')] }) },
      children: [heading, ...body, signature, p('')],
    }],
  });
  return new Uint8Array(await Packer.toArrayBuffer(doc));
}

/** An empty A4 document with the NĐ30 page setup and body font: what
 * "Tạo văn bản mới" opens in the editor before a template is chosen. */
export async function buildBlankDocument(): Promise<Uint8Array> {
  const doc = new Document({
    compatibility: { version: 15, doNotExpandShiftReturn: true },
    creator: 'WeRAG', title: 'Văn bản mới',
    styles: { default: { document: { run: { font: 'Times New Roman', size: 28, color: '000000' }, paragraph: { spacing: { after: 120, line: 276 } } } } },
    sections: [{ properties: { page: { size: { width: mm(210), height: mm(297) }, margin: { top: mm(20), bottom: mm(20), left: mm(30), right: mm(20), header: mm(10), footer: mm(10) } } },
      children: [new Paragraph({ children: [] })] }],
  });
  return new Uint8Array(await Packer.toArrayBuffer(doc));
}
