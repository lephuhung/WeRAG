#!/usr/bin/env python3
"""Build the .docx fixtures for the document-assistant evaluation.

Every fixture is generated here so the seeded mistakes (spelling, NĐ30 format,
conflicting figures) are known to the scenarios. Run:

    python3 build_fixtures.py [out_dir]

Requires python-docx (see ../README.md for the venv).
"""
from __future__ import annotations

import json
import os
import sys

from docx import Document
from docx.enum.table import WD_TABLE_ALIGNMENT
from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.oxml import OxmlElement
from docx.oxml.ns import qn
from docx.shared import Mm, Pt

TNR = "Times New Roman"


# --------------------------------------------------------------------------
# helpers
# --------------------------------------------------------------------------
def set_run_font(run, name=TNR, size=14, bold=None, italic=None):
    run.font.name = name
    rpr = run._element.get_or_add_rPr()
    rfonts = rpr.find(qn("w:rFonts"))
    if rfonts is None:
        rfonts = OxmlElement("w:rFonts")
        rpr.append(rfonts)
    for attr in ("w:ascii", "w:hAnsi", "w:cs", "w:eastAsia"):
        rfonts.set(qn(attr), name)
    run.font.size = Pt(size)
    if bold is not None:
        run.font.bold = bold
    if italic is not None:
        run.font.italic = italic


def para(container, text, *, align="justify", size=14, bold=False, italic=False,
         font=TNR, first_line_mm=10, space_after=6):
    p = container.add_paragraph()
    p.alignment = {
        "left": WD_ALIGN_PARAGRAPH.LEFT,
        "center": WD_ALIGN_PARAGRAPH.CENTER,
        "right": WD_ALIGN_PARAGRAPH.RIGHT,
        "justify": WD_ALIGN_PARAGRAPH.JUSTIFY,
    }[align]
    pf = p.paragraph_format
    pf.space_after = Pt(space_after)
    pf.space_before = Pt(0)
    if first_line_mm and align == "justify":
        pf.first_line_indent = Mm(first_line_mm)
    run = p.add_run(text)
    set_run_font(run, font, size, bold, italic)
    return p


def cell_para(cell, text, *, align="center", size=13, bold=False, italic=False, font=TNR):
    # python-docx cells start with one empty paragraph; reuse it the first time.
    if len(cell.paragraphs) == 1 and not cell.paragraphs[0].text:
        p = cell.paragraphs[0]
    else:
        p = cell.add_paragraph()
    p.alignment = {"left": WD_ALIGN_PARAGRAPH.LEFT, "center": WD_ALIGN_PARAGRAPH.CENTER,
                   "right": WD_ALIGN_PARAGRAPH.RIGHT}[align]
    p.paragraph_format.space_after = Pt(0)
    run = p.add_run(text)
    set_run_font(run, font, size, bold, italic)
    return p


def no_borders(table):
    tbl = table._tbl
    tblPr = tbl.tblPr
    borders = OxmlElement("w:tblBorders")
    for edge in ("top", "left", "bottom", "right", "insideH", "insideV"):
        el = OxmlElement(f"w:{edge}")
        el.set(qn("w:val"), "nil")
        borders.append(el)
    tblPr.append(borders)


def new_doc(*, margins=(20, 20, 30, 20), a4=True):
    doc = Document()
    sec = doc.sections[0]
    if a4:
        sec.page_width = Mm(210)
        sec.page_height = Mm(297)
    top, bottom, left, right = margins
    sec.top_margin, sec.bottom_margin = Mm(top), Mm(bottom)
    sec.left_margin, sec.right_margin = Mm(left), Mm(right)
    style = doc.styles["Normal"]
    style.font.name = TNR
    style.font.size = Pt(14)
    style.element.rPr.rFonts.set(qn("w:eastAsia"), TNR)
    return doc


def header_block(doc, *, parent, issuer, number, place_date, quoc_hieu_bold=True,
                 date_italic=True, body_font=TNR):
    """Two-column NĐ30 header: cơ quan | quốc hiệu; số ký hiệu | địa danh ngày."""
    t = doc.add_table(rows=2, cols=2)
    t.alignment = WD_TABLE_ALIGNMENT.CENTER
    no_borders(t)
    left, right = t.rows[0].cells
    cell_para(left, parent, size=12, font=body_font)
    cell_para(left, issuer, size=13, bold=True, font=body_font)
    cell_para(right, "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", size=13, bold=quoc_hieu_bold, font=body_font)
    cell_para(right, "Độc lập - Tự do - Hạnh phúc", size=13, bold=True, font=body_font)
    left2, right2 = t.rows[1].cells
    cell_para(left2, number, size=13, font=body_font)
    cell_para(right2, place_date, size=14, italic=date_italic, font=body_font)


def signature_block(doc, *, title, name, size=14):
    para(doc, title, align="right", size=size, bold=True, first_line_mm=0)
    para(doc, name, align="right", size=size, bold=True, first_line_mm=0)


def recipients_block(doc, lines):
    para(doc, "Nơi nhận:", align="left", size=12, bold=True, italic=True, first_line_mm=0, space_after=0)
    for line in lines:
        para(doc, line, align="left", size=12, first_line_mm=0, space_after=0)


# --------------------------------------------------------------------------
# fixture 1: công văn with seeded spelling + format errors
# --------------------------------------------------------------------------
SPELLING_ERRORS = [
    # (wrong, correct)
    ("triễn khai", "triển khai"),
    ("xữ lý", "xử lý"),
    ("hoàng thành", "hoàn thành"),
    ("bỗ sung", "bổ sung"),
    ("kiễm tra", "kiểm tra"),
    ("lưu trử", "lưu trữ"),
    ("thực hiên", "thực hiện"),
    ("bão cáo", "báo cáo"),
]

REPEATED_SENTENCE = "Các phòng, ban nghiêm túc thực hiện và báo cáo kết quả về Văn phòng trước ngày 30 hằng tháng."

CONG_VAN_BODY = [
    # paragraph index in body, text. Errors are embedded in the first three.
    "Thực hiện Nghị định số 30/2020/NĐ-CP ngày 05 tháng 3 năm 2020 của Chính phủ về công tác văn thư, Sở Nội vụ đề nghị các phòng, ban, đơn vị trực thuộc triễn khai một số nội dung sau:",
    "1. Rà soát, xữ lý dứt điểm các văn bản đến còn tồn đọng trong quý III năm 2026; hoàng thành việc cập nhật sổ đăng ký văn bản đến, văn bản đi trên Hệ thống quản lý văn bản và điều hành trước ngày 20 tháng 10 năm 2026.",
    "2. Bỗ sung đầy đủ thành phần thể thức theo Phụ lục I ban hành kèm theo Nghị định số 30/2020/NĐ-CP; kiễm tra chữ ký số của người có thẩm quyền trước khi phát hành; lập hồ sơ công việc và giao nộp vào lưu trử cơ quan đúng thời hạn.",
    "3. Phòng Văn thư - Lưu trữ chủ trì theo dõi, đôn đốc việc thực hiên; tổng hợp, bão cáo Giám đốc Sở kết quả thực hiện theo định kỳ hằng quý.",
    REPEATED_SENTENCE,
    "4. Các đơn vị không còn áp dụng Thông tư số 01/2011/TT-BNV ngày 19 tháng 01 năm 2011 của Bộ Nội vụ trong trình bày văn bản; mọi vướng mắc phản ánh về Phòng Văn thư - Lưu trữ (điện thoại 0234.3822.xxx) để được hướng dẫn.",
    REPEATED_SENTENCE,
    "Sở Nội vụ đề nghị Thủ trưởng các đơn vị quan tâm chỉ đạo thực hiện./.",
]


def build_cong_van_loi(out):
    doc = new_doc(margins=(20, 20, 30, 20))
    header_block(doc, parent="UBND THÀNH PHỐ HUẾ", issuer="SỞ NỘI VỤ",
                 number="Số: 1875/SNV-VTLT", place_date="Huế, ngày 08 tháng 10 năm 2026",
                 quoc_hieu_bold=False,   # seeded format error: quốc hiệu not bold
                 date_italic=False)      # seeded format error: date not italic
    para(doc, "V/v triển khai công tác văn thư quý IV năm 2026", align="center", size=12, first_line_mm=0)
    para(doc, "Kính gửi: Các phòng, ban, đơn vị trực thuộc Sở", align="center", size=14, first_line_mm=0)
    for i, text in enumerate(CONG_VAN_BODY):
        # seeded format error: body in Arial 12 (NĐ30 wants Times New Roman 13-14)
        para(doc, text, font="Arial", size=12)
    signature_block(doc, title="GIÁM ĐỐC", name="Trần Minh Quang")
    # seeded format error: no "Nơi nhận" block
    path = os.path.join(out, "cong_van_loi.docx")
    doc.save(path)
    return {
        "file": "cong_van_loi.docx",
        "doc_type": "cong_van",
        "number": "1875/SNV-VTLT",
        "issuer": "Sở Nội vụ",
        "date": "08 tháng 10 năm 2026",
        "signer": "Trần Minh Quang",
        "signer_title": "Giám đốc",
        "spelling_errors": SPELLING_ERRORS,
        "format_errors": [
            "body font Arial 12 instead of Times New Roman 13-14",
            "quốc hiệu not bold",
            "địa danh ngày tháng not italic",
            "no Nơi nhận block",
        ],
        "repeated_sentence": REPEATED_SENTENCE,
        "body": CONG_VAN_BODY,
    }


# --------------------------------------------------------------------------
# fixture 2: short, correct quy trình document (diagram + summary + insert)
# --------------------------------------------------------------------------
QUY_TRINH_STEPS = [
    ("Bước 1", "Tiếp nhận văn bản đến tại Văn thư cơ quan", "Văn thư", "trong ngày"),
    ("Bước 2", "Đăng ký văn bản đến vào Hệ thống quản lý văn bản và điều hành", "Văn thư", "trong ngày"),
    ("Bước 3", "Trình Lãnh đạo Sở cho ý kiến phân phối", "Chánh Văn phòng", "01 ngày làm việc"),
    ("Bước 4", "Chuyển giao văn bản đến phòng chuyên môn giải quyết", "Văn thư", "trong ngày"),
    ("Bước 5", "Theo dõi, đôn đốc và báo cáo kết quả giải quyết", "Phòng chuyên môn", "theo thời hạn ghi trên văn bản"),
]


def build_quy_trinh(out):
    doc = new_doc()
    header_block(doc, parent="UBND THÀNH PHỐ HUẾ", issuer="SỞ NỘI VỤ",
                 number="Số: 12/QT-SNV", place_date="Huế, ngày 01 tháng 10 năm 2026")
    para(doc, "QUY TRÌNH", align="center", size=14, bold=True, first_line_mm=0)
    para(doc, "Tiếp nhận và xử lý văn bản đến", align="center", size=14, bold=True, first_line_mm=0)
    para(doc, "Quy trình này áp dụng cho toàn bộ văn bản giấy và văn bản điện tử gửi đến Sở Nội vụ, gồm 5 bước theo thứ tự sau:")
    for code, what, who, when in QUY_TRINH_STEPS:
        para(doc, f"{code}: {what}. Trách nhiệm: {who}. Thời hạn: {when}.")
    para(doc, "Văn bản mật được xử lý theo quy định riêng của Luật Bảo vệ bí mật nhà nước; không đăng ký nội dung trích yếu lên Hệ thống.")
    para(doc, "Trong năm 2025, Sở đã tiếp nhận 4.812 văn bản đến, trong đó 4.790 văn bản được giải quyết đúng hạn (đạt 99,5%).")
    signature_block(doc, title="GIÁM ĐỐC", name="Trần Minh Quang")
    recipients_block(doc, ["- Các phòng, ban thuộc Sở;", "- Lưu: VT, VTLT."])
    path = os.path.join(out, "quy_trinh.docx")
    doc.save(path)
    return {
        "file": "quy_trinh.docx",
        "doc_type": "quy_trinh",
        "number": "12/QT-SNV",
        "steps": [s[0] + ": " + s[1] for s in QUY_TRINH_STEPS],
        "steps_detail": [{"buoc": s[0], "noi_dung": s[1], "trach_nhiem": s[2], "thoi_han": s[3]} for s in QUY_TRINH_STEPS],
        "notes": [
            "Áp dụng cho toàn bộ văn bản giấy và văn bản điện tử gửi đến Sở Nội vụ",
            "Văn bản mật được xử lý theo quy định riêng của Luật Bảo vệ bí mật nhà nước; không đăng ký nội dung trích yếu lên Hệ thống",
        ],
        "figures": {"văn bản đến 2025": "4.812", "đúng hạn": "4.790", "tỷ lệ": "99,5%"},
        "issuer": "Sở Nội vụ, UBND thành phố Huế",
        "date": "01 tháng 10 năm 2026",
        "signer": "Trần Minh Quang",
        "signer_title": "Giám đốc",
    }


# --------------------------------------------------------------------------
# fixture 3: long báo cáo (≥ 16k runes, ≥ 150 paragraphs) with sections
# --------------------------------------------------------------------------
BAO_CAO_FIGURES = {  # figure used in the target; the source (kế hoạch) differs on the marked ones
    "văn bản đến 9 tháng": "3.624",
    "văn bản đi 9 tháng": "2.118",
    "hồ sơ nộp lưu": "415",
    "kinh phí": "2,5 tỷ đồng",
    "tỷ lệ ký số": "97,2%",
    "lớp tập huấn": "06",
    "cán bộ tập huấn": "320",
}

QUARTER_DATA = [  # (quý, văn bản đến, văn bản đi) - data a chart can be drawn from
    ("Quý I", 1180, 702), ("Quý II", 1246, 688), ("Quý III", 1198, 728),
]

SECTIONS = [
    "I. CÔNG TÁC CHỈ ĐẠO, ĐIỀU HÀNH",
    "II. KẾT QUẢ THỰC HIỆN CÔNG TÁC VĂN THƯ",
    "III. KẾT QUẢ THỰC HIỆN CÔNG TÁC LƯU TRỮ",
    "IV. ỨNG DỤNG CÔNG NGHỆ THÔNG TIN VÀ CHUYỂN ĐỔI SỐ",
    "V. ĐÀO TẠO, BỒI DƯỠNG VÀ KINH PHÍ",
    "VI. TỒN TẠI, HẠN CHẾ VÀ NGUYÊN NHÂN",
    "VII. PHƯƠNG HƯỚNG, NHIỆM VỤ QUÝ IV NĂM 2026",
    "VIII. KIẾN NGHỊ, ĐỀ XUẤT",
]

FILLER = [
    "Các phòng, ban đã quán triệt đầy đủ các quy định của Nghị định số 30/2020/NĐ-CP, bảo đảm văn bản phát hành đúng thể thức, kỹ thuật trình bày và thẩm quyền ký.",
    "Việc lập hồ sơ công việc được thực hiện ngay từ khi mở hồ sơ, cập nhật thường xuyên trên Hệ thống và nộp lưu đúng thời hạn quy định.",
    "Công tác bảo vệ bí mật nhà nước trong soạn thảo, phát hành và lưu trữ văn bản được kiểm tra định kỳ, chưa phát hiện vi phạm.",
    "Sở đã rà soát, ban hành Danh mục hồ sơ năm 2026 và phổ biến đến toàn thể công chức, viên chức.",
    "Hệ thống quản lý văn bản và điều hành được nâng cấp, liên thông bốn cấp, bảo đảm gửi nhận văn bản điện tử ổn định.",
    "Tỷ lệ văn bản đi được ký số và phát hành điện tử tiếp tục tăng so với cùng kỳ năm 2025.",
    "Công chức làm công tác văn thư, lưu trữ được bố trí đủ số lượng, có trình độ chuyên môn phù hợp với vị trí việc làm.",
    "Kho lưu trữ cơ quan được bố trí giá, kệ, thiết bị phòng cháy chữa cháy và theo dõi nhiệt độ, độ ẩm theo quy định.",
    "Việc khai thác, sử dụng tài liệu lưu trữ phục vụ công tác chỉ đạo, điều hành được thực hiện kịp thời, đúng đối tượng.",
    "Một số đơn vị còn chậm cập nhật kết quả giải quyết văn bản đến trên Hệ thống, ảnh hưởng đến việc theo dõi, đôn đốc.",
    "Trang thiết bị phục vụ số hóa tài liệu lưu trữ còn thiếu, chưa đáp ứng yêu cầu chuyển đổi số toàn diện.",
    "Nhận thức của một bộ phận công chức về trách nhiệm lập hồ sơ công việc chưa đầy đủ, còn dồn việc vào cuối năm.",
]


def build_bao_cao_dai(out):
    doc = new_doc()
    header_block(doc, parent="UBND THÀNH PHỐ HUẾ", issuer="SỞ NỘI VỤ",
                 number="Số: 268/BC-SNV", place_date="Huế, ngày 05 tháng 10 năm 2026")
    para(doc, "BÁO CÁO", align="center", size=14, bold=True, first_line_mm=0)
    para(doc, "Kết quả công tác văn thư, lưu trữ 9 tháng đầu năm 2026 và phương hướng, nhiệm vụ quý IV năm 2026",
         align="center", size=14, bold=True, first_line_mm=0)
    para(doc, "Thực hiện Công văn số 1021/UBND-NC ngày 15 tháng 9 năm 2026 của UBND thành phố Huế, Sở Nội vụ báo cáo kết quả công tác văn thư, lưu trữ 9 tháng đầu năm 2026 như sau:")
    count = 0
    for si, title in enumerate(SECTIONS):
        para(doc, title, align="left", size=14, bold=True, first_line_mm=0)
        if si == 1:
            para(doc, f"Trong 9 tháng đầu năm 2026, Sở đã tiếp nhận {BAO_CAO_FIGURES['văn bản đến 9 tháng']} văn bản đến và phát hành {BAO_CAO_FIGURES['văn bản đi 9 tháng']} văn bản đi; tỷ lệ văn bản đi được ký số đạt {BAO_CAO_FIGURES['tỷ lệ ký số']}.")
            para(doc, "Số liệu văn bản đến, văn bản đi theo từng quý như sau:")
            t = doc.add_table(rows=1, cols=3)
            t.style = "Table Grid"
            for c, h in zip(t.rows[0].cells, ("Quý", "Văn bản đến", "Văn bản đi")):
                cell_para(c, h, bold=True)
            for q, inn, outn in QUARTER_DATA:
                r = t.add_row().cells
                cell_para(r[0], q, align="left")
                cell_para(r[1], f"{inn:,}".replace(",", "."))
                cell_para(r[2], f"{outn:,}".replace(",", "."))
        if si == 2:
            para(doc, f"Toàn Sở đã lập và nộp lưu {BAO_CAO_FIGURES['hồ sơ nộp lưu']} hồ sơ công việc vào Lưu trữ cơ quan, tăng 12% so với cùng kỳ năm 2025.")
            para(doc, "Quy trình xử lý văn bản đến gồm 5 bước: tiếp nhận tại Văn thư; đăng ký vào Hệ thống; trình Lãnh đạo phân phối; chuyển phòng chuyên môn giải quyết; theo dõi, đôn đốc và báo cáo kết quả.")
        if si == 4:
            para(doc, f"Sở đã tổ chức {BAO_CAO_FIGURES['lớp tập huấn']} lớp tập huấn nghiệp vụ văn thư, lưu trữ cho {BAO_CAO_FIGURES['cán bộ tập huấn']} lượt công chức, viên chức; tổng kinh phí thực hiện là {BAO_CAO_FIGURES['kinh phí']}.")
        for k in range(19):
            para(doc, f"{si + 1}.{k + 1}. " + FILLER[(si * 7 + k) % len(FILLER)] + (" Kết quả này được ghi nhận tại cuộc họp giao ban tháng %d năm 2026." % ((k % 9) + 1)))
            count += 1
    para(doc, "Trên đây là báo cáo kết quả công tác văn thư, lưu trữ 9 tháng đầu năm 2026 của Sở Nội vụ, kính báo cáo UBND thành phố./.")
    signature_block(doc, title="GIÁM ĐỐC", name="Trần Minh Quang")
    recipients_block(doc, ["- UBND thành phố (để b/c);", "- Lãnh đạo Sở;", "- Lưu: VT, VTLT."])
    path = os.path.join(out, "bao_cao_dai.docx")
    doc.save(path)
    return {
        "file": "bao_cao_dai.docx",
        "doc_type": "bao_cao",
        "number": "268/BC-SNV",
        "date": "05 tháng 10 năm 2026",
        "signer": "Trần Minh Quang",
        "sections": SECTIONS,
        "figures": BAO_CAO_FIGURES,
        "quarters": QUARTER_DATA,
        "filler_paragraphs": count,
    }


# --------------------------------------------------------------------------
# fixture 4: kế hoạch (source) whose figures conflict with the báo cáo
# --------------------------------------------------------------------------
KE_HOACH_FIGURES = {
    "văn bản đến 9 tháng": "3.624",     # same
    "văn bản đi 9 tháng": "2.118",      # same
    "hồ sơ nộp lưu": "452",             # CONFLICT (báo cáo: 415)
    "kinh phí": "2,8 tỷ đồng",          # CONFLICT (báo cáo: 2,5 tỷ đồng)
    "lớp tập huấn": "06",               # same
    "cán bộ tập huấn": "350",           # CONFLICT (báo cáo: 320)
}


def build_ke_hoach_nguon(out):
    doc = new_doc()
    header_block(doc, parent="UBND THÀNH PHỐ HUẾ", issuer="VĂN PHÒNG SỞ NỘI VỤ",
                 number="Số: 41/TH-VP", place_date="Huế, ngày 02 tháng 10 năm 2026")
    para(doc, "BẢNG TỔNG HỢP SỐ LIỆU", align="center", size=14, bold=True, first_line_mm=0)
    para(doc, "Công tác văn thư, lưu trữ 9 tháng đầu năm 2026 (số liệu chốt ngày 30/9/2026)", align="center", size=14, bold=True, first_line_mm=0)
    para(doc, f"1. Văn bản đến: {KE_HOACH_FIGURES['văn bản đến 9 tháng']} văn bản; văn bản đi: {KE_HOACH_FIGURES['văn bản đi 9 tháng']} văn bản.")
    para(doc, f"2. Hồ sơ công việc đã nộp lưu vào Lưu trữ cơ quan: {KE_HOACH_FIGURES['hồ sơ nộp lưu']} hồ sơ.")
    para(doc, f"3. Tập huấn nghiệp vụ: {KE_HOACH_FIGURES['lớp tập huấn']} lớp, {KE_HOACH_FIGURES['cán bộ tập huấn']} lượt người tham dự.")
    para(doc, f"4. Kinh phí đã thực hiện: {KE_HOACH_FIGURES['kinh phí']} (nguồn ngân sách thường xuyên).")
    para(doc, "5. Số liệu trên do Phòng Văn thư - Lưu trữ tổng hợp từ Hệ thống quản lý văn bản và sổ nộp lưu hồ sơ.")
    signature_block(doc, title="CHÁNH VĂN PHÒNG", name="Lê Thị Hồng Nhung")
    recipients_block(doc, ["- Lãnh đạo Sở;", "- Lưu: VP."])
    path = os.path.join(out, "ke_hoach_nguon.docx")
    doc.save(path)
    return {
        "file": "ke_hoach_nguon.docx",
        "doc_type": "tong_hop",
        "number": "41/TH-VP",
        "signer": "Lê Thị Hồng Nhung",
        "figures": KE_HOACH_FIGURES,
        "conflicts_with_bao_cao": {
            "hồ sơ nộp lưu": {"bao_cao": "415", "nguon": "452"},
            "kinh phí": {"bao_cao": "2,5 tỷ đồng", "nguon": "2,8 tỷ đồng"},
            "cán bộ tập huấn": {"bao_cao": "320", "nguon": "350"},
        },
    }


# --------------------------------------------------------------------------
# fixture 5: thông báo (second target for the multi-document naming rule)
# --------------------------------------------------------------------------
THONG_BAO_BODY = [
    "Sở Nội vụ thông báo lịch kiểm tra công tác văn thư, lưu trữ năm 2026 tại các đơn vị trực thuộc như sau:",
    "1. Thời gian: từ ngày 20 tháng 10 năm 2026 đến ngày 31 tháng 10 năm 2026.",
    "2. Thành phần đoàn kiểm tra: Lãnh đạo Sở, Phòng Văn thư - Lưu trữ, Thanh tra Sở.",
    "3. Nội dung kiểm tra: thể thức văn bản, lập hồ sơ công việc, nộp lưu hồ sơ, bảo vệ bí mật nhà nước.",
    "Đề nghị các đơn vị chuẩn bị đầy đủ hồ sơ, tài liệu phục vụ đoàn kiểm tra./.",
]


def build_thong_bao(out):
    doc = new_doc()
    header_block(doc, parent="UBND THÀNH PHỐ HUẾ", issuer="SỞ NỘI VỤ",
                 number="Số: 97/TB-SNV", place_date="Huế, ngày 09 tháng 10 năm 2026")
    para(doc, "THÔNG BÁO", align="center", size=14, bold=True, first_line_mm=0)
    para(doc, "Lịch kiểm tra công tác văn thư, lưu trữ năm 2026", align="center", size=14, bold=True, first_line_mm=0)
    for text in THONG_BAO_BODY:
        para(doc, text)
    signature_block(doc, title="KT. GIÁM ĐỐC\nPHÓ GIÁM ĐỐC", name="Nguyễn Thị Mai")
    recipients_block(doc, ["- Các đơn vị trực thuộc;", "- Lưu: VT, TTr."])
    path = os.path.join(out, "thong_bao.docx")
    doc.save(path)
    return {
        "file": "thong_bao.docx",
        "doc_type": "thong_bao",
        "number": "97/TB-SNV",
        "signer": "Nguyễn Thị Mai",
        "body": THONG_BAO_BODY,
    }


def main():
    out = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(__file__), "out")
    os.makedirs(out, exist_ok=True)
    manifest = {}
    for builder in (build_cong_van_loi, build_quy_trinh, build_bao_cao_dai, build_ke_hoach_nguon, build_thong_bao):
        meta = builder(out)
        manifest[meta["file"]] = meta
    with open(os.path.join(out, "manifest.json"), "w", encoding="utf-8") as f:
        json.dump(manifest, f, ensure_ascii=False, indent=2)
    for name in manifest:
        print(name, os.path.getsize(os.path.join(out, name)), "bytes")


if __name__ == "__main__":
    main()
