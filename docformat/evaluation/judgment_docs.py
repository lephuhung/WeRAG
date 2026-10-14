#!/usr/bin/env python3
"""Documents with known judgment outcomes for the Go live evaluation
harness (internal/docformat/live_eval_test.go).

Each document is correctly formatted where the rules measure (fonts,
sizes, positions), so what differs is what the skills judge: signing
authority, Nơi nhận, required lines, body structure.  ``expected.json``
maps each document to the Bảng kiểm items (the-thuc-chung) the judgment
must mark ``Lỗi``; a document with an empty list is correct and any
``Lỗi`` on it is a false alarm.  Items listed under ``any`` are not scored.

    python3 docformat/evaluation/judgment_docs.py <out_dir>
"""

import io
import json
import os
import re
import sys
import zipfile

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "tests"))
sys.path.insert(0, os.path.join(HERE, ".."))

from test_check import STYLES, W, p, row, tc  # noqa: E402
from test_segment_real import tbl  # noqa: E402

SO_NOI_VU = ("UBND TỈNH THỪA THIÊN HUẾ", "SỞ NỘI VỤ")
SO_Y_TE = ("UBND TỈNH THỪA THIÊN HUẾ", "SỞ Y TẾ")
DATE = "Huế, ngày 05 tháng 10 năm 2026"


R = 'xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"'

# NĐ30 Phụ lục I: số trang ở giữa, phía trên (header), trang đầu không
# đánh số (w:titlePg)
PAGE_HEADER = ('<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:fldSimple w:instr=" PAGE ">'
               '<w:r><w:rPr><w:sz w:val="28"/></w:rPr><w:t>2</w:t></w:r></w:fldSimple></w:p>')
SECT = ('<w:sectPr><w:headerReference w:type="default" r:id="rIdHeader"/><w:titlePg/>'
        '<w:pgSz w:w="11906" w:h="16838"/>'
        '<w:pgMar w:top="1360" w:right="1080" w:bottom="1360" w:left="1920" '
        'w:header="720" w:footer="720" w:gutter="0"/></w:sectPr>')


def doc(body_xml):
    return ('<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
            '<w:document %s %s><w:body>%s%s</w:body></w:document>' % (W, R, body_xml, SECT))


def build_docx(document_xml):
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as zf:
        zf.writestr("[Content_Types].xml",
                    '<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>')
        zf.writestr("word/document.xml", document_xml)
        zf.writestr("word/styles.xml", STYLES)
        zf.writestr("word/_rels/document.xml.rels",
                    '<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
                    '<Relationship Id="rIdHeader" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" '
                    'Target="header1.xml"/></Relationships>')
        zf.writestr("word/header1.xml", "<w:hdr %s>%s</w:hdr>" % (W, PAGE_HEADER))
    return buf.getvalue()


def _esc(t):
    return t.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


_DIEU_RE = re.compile(r"^(Điều \d+\.)\s*(.*)$")


def body_p(text):
    """A body paragraph: justified, cỡ 14, thụt đầu dòng 1 cm; "Điều N." in
    bold followed by the regular text of the điều."""
    ppr = '<w:pPr><w:ind w:firstLine="567"/><w:jc w:val="both"/></w:pPr>'
    run = '<w:r><w:rPr>%s<w:sz w:val="28"/></w:rPr><w:t xml:space="preserve">%s</w:t></w:r>'
    m = _DIEU_RE.match(text)
    if m:
        return "<w:p>%s%s%s</w:p>" % (ppr, run % ("<w:b/>", _esc(m.group(1) + " ")),
                                      run % ("", _esc(m.group(2))))
    return "<w:p>%s%s</w:p>" % (ppr, run % ("", _esc(text)))


def header(agency, so, vv=None, date=DATE):
    chu_quan, ban_hanh = agency
    left = []
    if chu_quan:
        left.append(p(chu_quan, align="center", size=13))
    for line in ban_hanh if isinstance(ban_hanh, list) else [ban_hanh]:
        left.append(p(line, align="center", size=13, bold=True))
    row2_left = [p(so, align="center", size=13)]
    if vv:
        row2_left.append(p(vv, align="center", size=12))
    return tbl(
        row(tc(*left),
            tc(p("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", align="center", size=13, bold=True),
               p("Độc lập - Tự do - Hạnh phúc", align="center", size=14, bold=True))),
        row(tc(*row2_left), tc(p(date, align="center", size=14, italic=True))),
    )


def title(kind, subject):
    return (p(kind, align="center", size=14, bold=True)
            + p(subject, align="center", size=14, bold=True))


def body(*paras):
    out = ""
    for t in paras:
        if t.endswith(":") and t.isupper():
            out += p(t, align="center", size=14, bold=True)
        else:
            out += body_p(t)
    return out


def can_cu(*lines):
    return "".join(p(t, align="both", size=14, italic=True) for t in lines)


def signature(sig_lines, name, noi_nhan):
    return tbl(row(
        tc(*([p("Nơi nhận:", bold=True, italic=True, size=12)]
             + [p(x, size=11) for x in noi_nhan])),
        tc(*([p(x, align="center", size=14, bold=True) for x in sig_lines]
             + [p("", align="center"), p(name, align="center", size=14, bold=True)])),
    ))


CV_BODY = (
    "Thực hiện Văn bản số 1234/UBND-NC ngày 15 tháng 9 năm 2026 của Ủy ban nhân dân tỉnh "
    "về việc rà soát vị trí việc làm, Sở Nội vụ đề nghị các phòng, ban thuộc Sở rà soát, "
    "tổng hợp số liệu theo biểu mẫu kèm theo.",
    "Báo cáo gửi về Văn phòng Sở trước ngày 20 tháng 10 năm 2026 để tổng hợp.",
    "Đề nghị các phòng, ban quan tâm, phối hợp thực hiện./.",
)


def cong_van(sig, noi_nhan, type_line=False, name="Lê Thị Dung"):
    head = header(SO_NOI_VU, "Số: 45/SNV-VP",
                  vv=None if type_line else "V/v rà soát vị trí việc làm năm 2026")
    if type_line:
        head += title("CÔNG VĂN", "Về việc rà soát vị trí việc làm năm 2026")
    return build_docx(doc(
        head + p("Kính gửi: Các phòng, ban thuộc Sở.", align="center", size=14)
        + body(*CV_BODY) + signature(sig, name, noi_nhan)))


def quyet_dinh(dieu, noi_nhan):
    return build_docx(doc(
        header(SO_Y_TE, "Số: 125/QĐ-SYT")
        + title("QUYẾT ĐỊNH", "Về việc ban hành Kế hoạch chuyển đổi số năm 2026")
        + p("GIÁM ĐỐC SỞ Y TẾ", align="center", size=14, bold=True)
        + can_cu("Căn cứ Luật Tổ chức chính quyền địa phương ngày 19 tháng 02 năm 2025;",
                 "Căn cứ Quyết định số 10/2024/QĐ-UBND ngày 12 tháng 3 năm 2024 của Ủy ban nhân dân "
                 "tỉnh quy định chức năng, nhiệm vụ của Sở Y tế;",
                 "Theo đề nghị của Chánh Văn phòng Sở.")
        + body("QUYẾT ĐỊNH:", *dieu)
        + signature(["GIÁM ĐỐC"], "Nguyễn Văn An", noi_nhan)))


QD_OK_DIEU = (
    "Điều 1. Ban hành kèm theo Quyết định này Kế hoạch chuyển đổi số năm 2026 của Sở Y tế.",
    "Điều 2. Quyết định này có hiệu lực kể từ ngày ký.",
    "Điều 3. Chánh Văn phòng Sở, Trưởng các phòng chức năng và Thủ trưởng các đơn vị trực thuộc "
    "chịu trách nhiệm thi hành Quyết định này./.",
)

QD_NO_EFFECT_DIEU = (
    "Điều 1. Ban hành kèm theo Quyết định này Kế hoạch chuyển đổi số năm 2026 của Sở Y tế.",
    "Điều 2. Giao Văn phòng Sở hướng dẫn, đôn đốc các đơn vị triển khai các nhiệm vụ tại Kế hoạch; "
    "định kỳ hằng quý tổng hợp kết quả./.",
)


def bao_cao():
    return build_docx(doc(
        header(SO_NOI_VU, "Số: 210/BC-SNV")
        + title("BÁO CÁO", "Kết quả công tác cải cách hành chính năm 2025")
        + p("Kính gửi: Ủy ban nhân dân tỉnh Thừa Thiên Huế.", align="center", size=14)
        + body("Thực hiện Kế hoạch số 15/KH-UBND ngày 20 tháng 01 năm 2025 của Ủy ban nhân dân tỉnh, "
               "Sở Nội vụ báo cáo kết quả công tác cải cách hành chính năm 2025 như sau:",
               "I. ĐẶC ĐIỂM, TÌNH HÌNH CHUNG",
               "Năm 2025, Sở được giao 18 chỉ tiêu cải cách hành chính, trong đó có 05 chỉ tiêu mới.",
               "II. KẾT QUẢ THỰC HIỆN",
               "Sở đã hoàn thành 17/18 chỉ tiêu; tỷ lệ hồ sơ trực tuyến toàn trình đạt 82%.",
               "III. TỒN TẠI, HẠN CHẾ VÀ NGUYÊN NHÂN",
               "Việc số hóa hồ sơ, kết quả giải quyết thủ tục hành chính còn chậm.",
               "IV. PHƯƠNG HƯỚNG, NHIỆM VỤ NĂM 2026",
               "Tiếp tục đẩy mạnh số hóa, nâng tỷ lệ hồ sơ trực tuyến toàn trình lên 90%.",
               "Trên đây là báo cáo kết quả công tác cải cách hành chính năm 2025 của Sở Nội vụ./.")
        + signature(["GIÁM ĐỐC"], "Lê Thị Dung",
                    ["- Như trên;", "- Bộ Nội vụ (để báo cáo);", "- Lưu: VT, CCHC."])))


def to_trinh():
    return build_docx(doc(
        header(SO_NOI_VU, "Số: 88/TTr-SNV")
        + title("TỜ TRÌNH", "Về việc đề nghị phê duyệt Đề án vị trí việc làm của Sở Nội vụ")
        + p("Kính gửi: Ủy ban nhân dân tỉnh Thừa Thiên Huế.", align="center", size=14)
        + body("Căn cứ Nghị định số 62/2020/NĐ-CP ngày 01 tháng 6 năm 2020 của Chính phủ về vị trí "
               "việc làm và biên chế công chức, Sở Nội vụ đã xây dựng Đề án vị trí việc làm.",
               "Đề án xác định 45 vị trí việc làm, trong đó 08 vị trí lãnh đạo, quản lý.",
               "Sở Nội vụ kính trình Ủy ban nhân dân tỉnh xem xét, phê duyệt./.")
        + signature(["KT. GIÁM ĐỐC", "PHÓ GIÁM ĐỐC"], "Trần Văn Bình",
                    ["- Như trên;", "- Giám đốc Sở (để b/c);", "- Lưu: VT, TCCB."])))


def ke_hoach():
    return build_docx(doc(
        header(SO_Y_TE, "Số: 30/KH-SYT")
        + title("KẾ HOẠCH", "Triển khai chuyển đổi số ngành y tế năm 2026")
        + body("Thực hiện Quyết định số 749/QĐ-TTg ngày 03 tháng 6 năm 2020 của Thủ tướng Chính phủ, "
               "Sở Y tế ban hành Kế hoạch triển khai chuyển đổi số năm 2026 như sau:",
               "I. MỤC ĐÍCH, YÊU CẦU",
               "Đẩy mạnh ứng dụng công nghệ số trong khám bệnh, chữa bệnh và quản lý y tế.",
               "II. NỘI DUNG, NHIỆM VỤ",
               "Triển khai bệnh án điện tử tại 100% bệnh viện tuyến tỉnh.",
               "III. TỔ CHỨC THỰC HIỆN",
               "Văn phòng Sở chủ trì, theo dõi, tổng hợp kết quả, báo cáo Giám đốc Sở trước ngày 15 tháng 12 năm 2026./.")
        + signature(["GIÁM ĐỐC"], "Nguyễn Văn An",
                    ["- UBND tỉnh (để báo cáo);", "- Các đơn vị trực thuộc;", "- Lưu: VT, VP."])))


def nghi_quyet():
    return build_docx(doc(
        header((None, ["HỘI ĐỒNG NHÂN DÂN", "TỈNH THỪA THIÊN HUẾ"]), "Số: 15/NQ-HĐND",
               date="Thừa Thiên Huế, ngày 10 tháng 7 năm 2026")
        + title("NGHỊ QUYẾT", "Về việc thông qua Đề án phát triển du lịch giai đoạn 2026 - 2030")
        + p("HỘI ĐỒNG NHÂN DÂN TỈNH THỪA THIÊN HUẾ", align="center", size=14, bold=True)
        + p("KHÓA VIII, KỲ HỌP THỨ 12", align="center", size=14, bold=True)
        + can_cu("Căn cứ Luật Tổ chức chính quyền địa phương ngày 19 tháng 02 năm 2025;",
                 "Xét Tờ trình số 120/TTr-UBND ngày 20 tháng 6 năm 2026 của Ủy ban nhân dân tỉnh; "
                 "Báo cáo thẩm tra của Ban Kinh tế - Ngân sách; ý kiến thảo luận của đại biểu "
                 "Hội đồng nhân dân tỉnh tại kỳ họp.")
        + body("QUYẾT NGHỊ:",
               "Điều 1. Thông qua Đề án phát triển du lịch tỉnh Thừa Thiên Huế giai đoạn 2026 - 2030.",
               "Điều 2. Giao Ủy ban nhân dân tỉnh tổ chức thực hiện Nghị quyết; Thường trực Hội đồng "
               "nhân dân, các Ban và đại biểu Hội đồng nhân dân tỉnh giám sát việc thực hiện.",
               "Điều 3. Nghị quyết này đã được Hội đồng nhân dân tỉnh Thừa Thiên Huế khóa VIII, kỳ họp "
               "thứ 12 thông qua ngày 10 tháng 7 năm 2026 và có hiệu lực kể từ ngày thông qua./.")
        + signature(["TM. HỘI ĐỒNG NHÂN DÂN", "CHỦ TỊCH"], "Phạm Minh Tuấn",
                    ["- Ủy ban Thường vụ Quốc hội;", "- Chính phủ;", "- UBND tỉnh;", "- Lưu: VT, VP."])))


def documents():
    """name → (content, expected Lỗi items, unscored items, note)."""
    head = ["GIÁM ĐỐC"]
    kt = ["KT. GIÁM ĐỐC", "PHÓ GIÁM ĐỐC"]
    return {
        "cv_ok": (cong_van(head, ["- Như trên;", "- Lưu: VT, VP."]), [], [],
                  "công văn, giám đốc ký"),
        "cv_kt_ok": (cong_van(kt, ["- Như trên;", "- Giám đốc Sở (để b/c);", "- Lưu: VT, VP."], name="Trần Văn Bình"),
                     [], [], "công văn, KT. + giám đốc (để b/c)"),
        "qd_ok": (quyet_dinh(QD_OK_DIEU, ["- Như Điều 3;", "- Lưu: VT, VP."]), [], [],
                  "quyết định đủ QUYẾT ĐỊNH:, Điều hiệu lực/thi hành"),
        "bc_ok": (bao_cao(), [], [], "báo cáo đủ 4 đề mục"),
        "ttr_ok": (to_trinh(), [], [], "tờ trình KT. + kiến nghị"),
        "kh_ok": (ke_hoach(), [], [], "kế hoạch đủ 3 đề mục"),
        "nq_ok": (nghi_quyet(), [], [], "nghị quyết TM. HĐND, QUYẾT NGHỊ:"),
        # judgment errors
        "cv_kt_missing": (cong_van(["GIÁM ĐỐC", "PHÓ GIÁM ĐỐC"], ["- Như trên;", "- Lưu: VT, VP."], name="Trần Văn Bình"),
                          [2, 3], [], "PHÓ không có KT., không báo cáo người đứng đầu"),
        "cv_kt_no_head": (cong_van(kt, ["- Như trên;", "- Phó Giám đốc Sở;", "- Lưu: VT, VP."], name="Trần Văn Bình"),
                          [3], [], "KT. nhưng nơi nhận chỉ có Phó Giám đốc"),
        "cv_luu_no_vt": (cong_van(head, ["- Như trên;", "- Lưu: VP."]), [4], [],
                         "Lưu thiếu VT"),
        "cv_type_line": (cong_van(head, ["- Như trên;", "- Lưu: VT, VP."], type_line=True), [6], [],
                         "công văn có dòng tên loại CÔNG VĂN"),
        "qd_no_effect": (quyet_dinh(QD_NO_EFFECT_DIEU, ["- Như Điều 2;", "- Lưu: VT, VP."]), [6], [],
                         "quyết định thiếu điều hiệu lực/thi hành"),
    }


def main(argv=None) -> int:
    argv = argv if argv is not None else sys.argv[1:]
    out = argv[0] if argv else os.path.join(HERE, "samples", "judgment")
    os.makedirs(out, exist_ok=True)
    expected = {}
    for name, (content, errs, any_items, note) in documents().items():
        with open(os.path.join(out, name + ".docx"), "wb") as fh:
            fh.write(content)
        expected[name] = {"errors": errs, "any": any_items, "note": note}
    with open(os.path.join(out, "expected.json"), "w", encoding="utf-8") as fh:
        json.dump(expected, fh, ensure_ascii=False, indent=1)
    print("wrote %d documents to %s" % (len(expected), out))
    return 0


if __name__ == "__main__":
    sys.exit(main())
