/** NĐ30 documents built in the page from a ready prepare_document_template
 * result (the server's checked fields; no organisation or signing authority
 * is inferred here). Copied from the Word add-in
 * (src/lib/word/document-templates.ts); keep the two in step. */
import type { TemplateFields, TemplateFontSizes } from './template-protocol.ts';
import { AlignmentType, BorderStyle, Document, Header, Packer, PageNumber, Paragraph, Table, TableCell, TableLayoutType, TableRow, TextRun, WidthType } from 'docx';

export const DOCUMENT_TEMPLATES = [
  { id: 'cong-van', name: 'Công văn' },
  { id: 'ke-hoach', name: 'Kế hoạch' },
  { id: 'bao-cao', name: 'Báo cáo' },
  { id: 'to-trinh', name: 'Tờ trình' },
  { id: 'thong-bao', name: 'Thông báo' },
  { id: 'quyet-dinh', name: 'Quyết định cá biệt' },
  { id: 'nghi-quyet', name: 'Nghị quyết cá biệt' },
  { id: 'chi-thi', name: 'Chỉ thị' },
  { id: 'cong-dien', name: 'Công điện' },
  { id: 'giay-moi', name: 'Giấy mời' },
  { id: 'giay-gioi-thieu', name: 'Giấy giới thiệu' },
  { id: 'bien-ban', name: 'Biên bản' },
  { id: 'giay-nghi-phep', name: 'Giấy nghỉ phép' },
] as const;
export type DocumentTemplateId = typeof DOCUMENT_TEMPLATES[number]['id'];


// rounded, not docx's truncating convertMillimetersToTwip: A4 is exactly
// 11906 × 16838 twips, 30 mm is 1701
const mm = (v: number) => Math.round((v * 1440) / 25.4);
/** NĐ30 sizes (pt) and margins (mm) when the server sends none; the server's ranges and
 * defaults are types.TemplateFontSizeSpecs. Số ký hiệu (13) and Nơi nhận
 * (12/11) have a single NĐ30 size. */
const DEFAULT_SIZES = {
  co_quan_chu_quan: 13, co_quan_ban_hanh: 13, quoc_hieu: 13, tieu_ngu: 14, trich_yeu_cong_van: 12,
  dia_danh_ngay_thang: 13, ten_loai: 14, trich_yeu: 14, tham_quyen_ban_hanh: 13, noi_dung: 14,
  chuc_vu_nguoi_ky: 13, ho_ten_nguoi_ky: 14, so_trang: 13,
  // page margins, mm
  le_tren: 20, le_duoi: 20, le_trai: 30, le_phai: 20,
};
const noBorder = { style: BorderStyle.NONE, size: 0, color: 'FFFFFF' };
const borders = { top: noBorder, bottom: noBorder, left: noBorder, right: noBorder, insideHorizontal: noBorder, insideVertical: noBorder };
// tight: the two-line header blocks (cơ quan chủ quản / ban hành, quốc hiệu /
// tiêu ngữ) sit on single spacing with no gap, as printed NĐ30 documents do
function p(text: string, options: { bold?: boolean; italic?: boolean; size?: number; center?: boolean; body?: boolean; tight?: boolean } = {}) {
  return new Paragraph({
    alignment: options.center ? AlignmentType.CENTER : options.body ? AlignmentType.JUSTIFIED : AlignmentType.LEFT,
    spacing: options.tight ? { before: 0, after: 0, line: 240 } : { before: 0, after: options.body ? 120 : 60, line: 276 },
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
// right: the width of the right column, the text width less the 3500-twip left one
function table(rows: TableRow[], right: number) {
  return new Table({ width: { size: 3500 + right, type: WidthType.DXA }, columnWidths: [3500, right], layout: TableLayoutType.FIXED, borders, rows });
}

// số ký hiệu codes (NĐ30 Phụ lục III); công văn has none
const TYPE_CODES: Record<DocumentTemplateId, string> = {
  'cong-van': '', 'ke-hoach': 'KH', 'bao-cao': 'BC', 'to-trinh': 'TTr', 'thong-bao': 'TB', 'quyet-dinh': 'QĐ',
  'nghi-quyet': 'NQ', 'chi-thi': 'CT', 'cong-dien': 'CĐ', 'giay-moi': 'GM', 'giay-gioi-thieu': 'GGT', 'bien-ban': 'BB', 'giay-nghi-phep': 'GNP',
};
// the body lines of the forms (Phụ lục III, Mẫu 1.6–1.10) when no draft is given
const FORM_SKELETONS: Partial<Record<DocumentTemplateId, string[]>> = {
  'cong-dien': ['- [Cơ quan, tổ chức nhận điện];', '[Nội dung điện]./.'],
  'giay-moi': ['[Tên cơ quan] trân trọng kính mời: [Người hoặc cơ quan được mời].', 'Tới dự [Tên cuộc họp, hội nghị].', 'Chủ trì: [Người chủ trì].', 'Thời gian: [Giờ, ngày].', 'Địa điểm: [Địa điểm].', '[Các vấn đề cần lưu ý]./.'],
  'giay-gioi-thieu': ['[Tên cơ quan] trân trọng giới thiệu:', 'Ông (bà): [Họ và tên]', 'Chức vụ: [Chức vụ, đơn vị công tác]', 'Được cử đến: [Cơ quan, tổ chức]', 'Về việc: [Nội dung công việc]', 'Đề nghị Quý cơ quan tạo điều kiện để ông (bà) có tên ở trên hoàn thành nhiệm vụ.', 'Giấy này có giá trị đến hết ngày [ngày]./.'],
  'bien-ban': ['Thời gian bắt đầu: [Giờ, ngày].', 'Địa điểm: [Địa điểm].', 'Thành phần tham dự: [Thành phần].', 'Chủ trì (chủ tọa): [Họ và tên, chức vụ].', 'Thư ký (người ghi biên bản): [Họ và tên, chức vụ].', 'Nội dung (theo diễn biến cuộc họp/hội nghị/hội thảo):', '[Diễn biến, ý kiến, kết luận]', 'Cuộc họp (hội nghị, hội thảo) kết thúc vào [giờ], ngày [ngày]./.'],
  'giay-nghi-phep': ['Xét Đơn đề nghị nghỉ phép ngày [ngày] của ông (bà) [Họ và tên],', '[Tên cơ quan] cấp cho:', 'Ông (bà): [Họ và tên]', 'Chức vụ: [Chức vụ, đơn vị công tác]', 'Được nghỉ phép trong thời gian [số ngày], kể từ ngày [ngày] đến hết ngày [ngày] tại [nơi nghỉ phép].', 'Số ngày nghỉ phép nêu trên được tính vào thời gian [nghỉ phép năm …]./.'],
};

export async function buildDocumentTemplate(id: DocumentTemplateId, data?: TemplateFields, sizes?: TemplateFontSizes): Promise<Uint8Array> {
  const z: typeof DEFAULT_SIZES = { ...DEFAULT_SIZES };
  for (const key of Object.keys(z) as (keyof typeof z)[]) if (Number.isInteger(sizes?.[key])) z[key] = sizes![key]!;
  // body text: the size every paragraph without its own size takes
  const b = { size: z.noi_dung };
  const right = mm(210 - z.le_trai - z.le_phai) - 3500;
  const spec = DOCUMENT_TEMPLATES.find(t => t.id === id);
  if (!spec) throw new Error('Loại văn bản không được hỗ trợ.');
  const letter = id === 'cong-van';
  // the types issued under an authority line and "Căn cứ …"
  const decision = id === 'quyet-dinh' || id === 'nghi-quyet';
  // Mẫu 1.8 and 1.10 print no trích yếu
  const untitled = id === 'giay-gioi-thieu' || id === 'giay-nghi-phep';
  const minutes = id === 'bien-ban';
  const code = `${TYPE_CODES[id]}-[CQ]`.replace(/^-\[CQ\]$/, '[CQ]-[ĐV]');
  const heading = table([
    new TableRow({ cantSplit: true, children: [
      cell([...(data && !data.parent_agency ? [] : [p(data?.parent_agency ?? '[CƠ QUAN CHỦ QUẢN]', { center: true, size: z.co_quan_chu_quan, tight: true })]), p(data?.agency ?? '[CƠ QUAN BAN HÀNH]', { center: true, bold: true, size: z.co_quan_ban_hanh, tight: true }), rule(1167)], 3500),
      cell([p('CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM', { center: true, bold: true, size: z.quoc_hieu, tight: true }), p('Độc lập - Tự do - Hạnh phúc', { center: true, bold: true, size: z.tieu_ngu, tight: true }), rule(1100)], right),
    ] }),
    new TableRow({ cantSplit: true, children: [
      cell([p(data ? `Số: ${data.number_symbol}` : `Số: [SỐ]/${code}`, { center: true, size: 13 }), ...(letter ? [p(data ? `V/v ${data.subject}` : 'V/v [Trích yếu nội dung công văn]', { center: true, size: z.trich_yeu_cong_van })] : [])], 3500),
      cell([p(data ? `${data.locality}, ${data.date}` : '[Địa danh], ngày [DD] tháng [MM] năm [YYYY]', { center: true, italic: true, size: z.dia_danh_ngay_thang })], right),
    ] }),
  ], right);
  const body: Paragraph[] = [];
  if (!letter) {
    body.push(p(id === 'quyet-dinh' ? 'QUYẾT ĐỊNH' : id === 'nghi-quyet' ? 'NGHỊ QUYẾT' : spec.name.toLocaleUpperCase('vi-VN'), { center: true, bold: true, size: z.ten_loai }));
    if (!untitled) body.push(p(data?.subject ?? (minutes ? '[Tên cuộc họp, hội nghị, hội thảo]' : '[Trích yếu nội dung văn bản]'), { center: true, bold: true, size: z.trich_yeu }));
    if (id !== 'giay-gioi-thieu') body.push(rule(3500));
  }
  const resolves = id === 'nghi-quyet' ? 'QUYẾT NGHỊ:' : 'QUYẾT ĐỊNH:';
  if (data) {
    if (decision) {
      body.push(p(data.decision_authority!, { center: true, bold: true, size: z.tham_quyen_ban_hanh }));
      for (const line of data.grounds ?? []) body.push(p(line, { ...b, body: true, italic: true }));
      if (data.request_basis) body.push(p(data.request_basis, { ...b, body: true, italic: true }));
      body.push(p(resolves, { ...b, center: true, bold: true }));
    } else if (data.addressee) body.push(p(`Kính gửi: ${data.addressee}`, { ...b, center: true }));
    if (data.issuer_line) {
      body.push(p(data.issuer_line, { ...b, center: true, bold: true }));
      for (const line of data.receiver_lines ?? []) body.push(p(line, { ...b, body: true }));
    }
    for (const block of data.paragraphs) body.push(p(block.text, { ...b, body: true, bold: block.kind !== 'body' }));
  } else if (decision) {
    body.push(p(id === 'nghi-quyet' ? '[TÊN TẬP THỂ CÓ THẨM QUYỀN BAN HÀNH]' : '[CHỨC VỤ NGƯỜI CÓ THẨM QUYỀN BAN HÀNH]', { bold: true, center: true, size: z.tham_quyen_ban_hanh }));
    body.push(p('Căn cứ [Văn bản quy định chức năng, nhiệm vụ và thẩm quyền];', { ...b, body: true, italic: true }));
    body.push(p('Căn cứ [Căn cứ pháp lý liên quan];', { ...b, body: true, italic: true }));
    body.push(p(id === 'nghi-quyet' ? 'Xét [Tờ trình, đề nghị (nếu có)].' : 'Theo đề nghị của [Đơn vị đề nghị].', { ...b, body: true, italic: true }));
    body.push(p(resolves, { ...b, center: true, bold: true }));
    for (const text of ['Điều 1. [Nội dung]', 'Điều 2. [Tổ chức thực hiện]', 'Điều 3. [Hiệu lực và trách nhiệm thi hành]']) body.push(p(text, { ...b, body: true, bold: true }));
  } else if (FORM_SKELETONS[id]) {
    const lines = FORM_SKELETONS[id]!;
    if (id === 'cong-dien') body.push(p('[TÊN CƠ QUAN HOẶC CHỨC DANH NGƯỜI ĐỨNG ĐẦU] điện:', { ...b, center: true, bold: true }));
    for (const line of lines) body.push(p(line, { ...b, body: true, bold: line.endsWith('):') }));
  } else {
    if (letter || id === 'to-trinh') body.push(p('Kính gửi: [Cơ quan, tổ chức hoặc người nhận]', { ...b, center: true }));
    const sections = id === 'ke-hoach' ? ['I. MỤC ĐÍCH, YÊU CẦU', 'II. NỘI DUNG, NHIỆM VỤ', 'III. TỔ CHỨC THỰC HIỆN']
      : id === 'bao-cao' ? ['I. TÌNH HÌNH, KẾT QUẢ', 'II. ĐÁNH GIÁ', 'III. KIẾN NGHỊ, ĐỀ XUẤT']
      : id === 'to-trinh' ? ['I. SỰ CẦN THIẾT', 'II. NỘI DUNG ĐỀ NGHỊ', 'III. KIẾN NGHỊ'] : [];
    if (sections.length) for (const title of sections) body.push(p(title, { ...b, body: true, bold: true }), p('[Điền nội dung]', { ...b, body: true }));
    else body.push(p('[Điền nội dung văn bản]', { ...b, body: true }), p('[Tổ chức thực hiện hoặc đề nghị phối hợp]./.', { ...b, body: true }));
  }
  const recipients = [p('Nơi nhận:', { bold: true, italic: true, size: 12 }), ...(data ? data.recipients.map(line => p(line, { size: 11 })) : [p('- [Cơ quan, tổ chức nhận];', { size: 11 }), p(minutes ? '- Lưu: VT, Hồ sơ.' : '- Lưu: VT, [Đơn vị soạn thảo].', { size: 11 })])];
  const signer = (lines: readonly string[], note: string, name: string, width: number) => cell([
    ...lines.map(line => p(line, { center: true, bold: true, size: z.chuc_vu_nguoi_ky })), p(note, { center: true, italic: true, size: z.chuc_vu_nguoi_ky }),
    p('', b), p('', b), p(name, { center: true, bold: true, size: z.ho_ten_nguoi_ky }),
  ], width);
  const head = signer(data ? data.signature_lines : [minutes ? 'CHỦ TỌA' : '[CHỨC VỤ NGƯỜI KÝ]'], '(Ký, ghi rõ họ tên, đóng dấu)', data?.signer_name ?? '[Họ và tên]', right);
  // a biên bản (Mẫu 1.9): the secretary signs on the left, Nơi nhận goes below
  const signature = minutes
    ? table([
      new TableRow({ cantSplit: true, children: [signer(['THƯ KÝ'], '(Ký, ghi rõ họ tên)', data?.secretary_name ?? '[Họ và tên]', 3500), head] }),
      new TableRow({ cantSplit: true, children: [cell(recipients, 3500), cell([p('', b)], right)] }),
    ], right)
    : table([new TableRow({ cantSplit: true, children: [cell(recipients, 3500), head] })], right);
  const doc = new Document({
    compatibility: { version: 15, doNotExpandShiftReturn: true },
    creator: 'WeRAG', title: spec.name, description: 'Khung mẫu theo Nghị định 30/2020/NĐ-CP; điền các trường trong ngoặc vuông.',
    styles: { default: { document: { run: { font: 'Times New Roman', size: z.noi_dung * 2, color: '000000' }, paragraph: { spacing: { after: 120, line: 276 } } } } },
    sections: [{ properties: { titlePage: true, page: { size: { width: mm(210), height: mm(297) }, margin: { top: mm(z.le_tren), bottom: mm(z.le_duoi), left: mm(z.le_trai), right: mm(z.le_phai), header: mm(10), footer: mm(10) } } },
      headers: { default: new Header({ children: [new Paragraph({ alignment: AlignmentType.CENTER, children: [new TextRun({ children: [PageNumber.CURRENT], font: 'Times New Roman', size: z.so_trang * 2 })] })] }), first: new Header({ children: [p('')] }) },
      children: [heading, ...body, signature, p('', b)],
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
