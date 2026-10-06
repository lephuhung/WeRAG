#!/usr/bin/env python3
"""Generate a synthetic, gold-labelled corpus of văn bản hành chính (.docx).

Each document is assembled from layout variants seen in real UBND/Sở files
(header as one/two/3-col tables or tab-split lines, signature in a table, by
alignment, indent, leading tabs or not positioned at all, wrapped agency
names, drafts with blank số, độ khẩn, data tables, phụ lục...).  Every
paragraph records its NĐ30 component while it is emitted, so the gold labels
are exact.  Formatting mistakes (wrong sizes, fonts) are injected at random so
that labelling by *role* and labelling by *format* give different answers.

Output per document: ``NNN_<type>_<variant>.docx`` + ``.labels.json``
(``{"document_type", "labels": {unit_id: component}, "meta"}``) — the same
shape as an LLM labeling reply, so gold files for real documents can be
written the same way (see ``draft_labels.py``).

    python3 evaluation/gen_corpus.py --out evaluation/samples/synth --n 60
"""

from __future__ import annotations

import argparse
import io
import json
import os
import random
import sys
import zipfile

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))

from docformat.labeling import build_units  # noqa: E402
from docformat.layout import inspect_docx  # noqa: E402

W_NS = 'xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"'
STYLES = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles %s><w:docDefaults><w:rPrDefault><w:rPr>
<w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman" w:cs="Times New Roman"/>
<w:sz w:val="28"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr/></w:pPrDefault>
</w:docDefaults><w:style w:type="paragraph" w:default="1" w:styleId="Normal">
<w:name w:val="Normal"/></w:style></w:styles>""" % W_NS

# minimal package parts so Word / LibreOffice open the files for review
CONTENT_TYPES = (
    '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
    '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">'
    '<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>'
    '<Default Extension="xml" ContentType="application/xml"/>'
    '<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>'
    '<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>'
    '</Types>')
ROOT_RELS = (
    '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
    '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
    '<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>'
    '</Relationships>')
DOC_RELS = (
    '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
    '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
    '<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>'
    '</Relationships>')


def _esc(t: str) -> str:
    return t.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


def _rpr(size=None, bold=False, italic=False, font=None, underline=False):
    r = ""
    if font:
        r += '<w:rFonts w:ascii="%s" w:hAnsi="%s"/>' % (font, font)
    if bold:
        r += "<w:b/>"
    if italic:
        r += "<w:i/>"
    if underline:
        r += '<w:u w:val="single"/>'
    if size:
        r += '<w:sz w:val="%d"/>' % int(round(size * 2))
    return "<w:rPr>%s</w:rPr>" % r if r else ""


def _ppr(align=None, indent=None, first=None, tabs=(), page_break=False):
    r = ""
    if page_break:
        r += "<w:pageBreakBefore/>"
    if tabs:
        r += "<w:tabs>%s</w:tabs>" % "".join(
            '<w:tab w:val="%s" w:pos="%d"/>' % (v, pos) for v, pos in tabs)
    if indent or first:
        r += '<w:ind%s%s/>' % (' w:left="%d"' % indent if indent else "",
                                ' w:firstLine="%d"' % first if first else "")
    if align:
        r += '<w:jc w:val="%s"/>' % align
    return "<w:pPr>%s</w:pPr>" % r if r else ""


class Doc:
    """Emits OOXML while recording each paragraph's gold component."""

    def __init__(self, font=None):
        self.xml: list = []
        self.gold: list = []          # per paragraph: label | (L, R) | None
        self.font = font

    # -- paragraphs ---------------------------------------------------------
    def _p(self, text, label, align=None, size=None, bold=False, italic=False,
           indent=None, first=None, tabs=(), lead_tabs=0, page_break=False,
           underline=False, font=None):
        runs = "<w:r><w:tab/></w:r>" * lead_tabs
        if text:
            runs += '<w:r>%s<w:t xml:space="preserve">%s</w:t></w:r>' % (
                _rpr(size, bold, italic, font or self.font, underline), _esc(text))
        self.gold.append(label if text else None)
        return "<w:p>%s%s</w:p>" % (
            _ppr(align, indent, first, tabs, page_break), runs)

    def p(self, *a, **k):
        self.xml.append(self._p(*a, **k))

    def blank(self, n=1):
        for _ in range(n):
            self.p("", None)

    def split(self, left, right, lab_l, lab_r, size_l=13, size_r=13,
              bold_l=False, bold_r=False, italic_r=False, pos=6804):
        """One paragraph, left text TAB right text (tab-stop header)."""
        rl = _rpr(size_l, bold_l, False, self.font)
        rr = _rpr(size_r, bold_r, italic_r, self.font)
        xml = ('<w:p>%s<w:r>%s<w:t xml:space="preserve">%s</w:t></w:r>'
               '<w:r><w:tab/></w:r><w:r>%s<w:t xml:space="preserve">%s</w:t>'
               '</w:r></w:p>' % (_ppr("left", tabs=[("center", pos)]), rl,
                                 _esc(left), rr, _esc(right)))
        self.gold.append((lab_l, lab_r))
        self.xml.append(xml)

    # -- tables -------------------------------------------------------------
    def table(self, rows, widths=(4200, 5300), borders=False):
        """rows: list of rows; row: list of cells; cell: list of para
        kwargs dicts (text, label, ...)."""
        pr = '<w:tblW w:w="%d" w:type="dxa"/>' % sum(widths)
        if borders:
            pr += ('<w:tblBorders>' + "".join(
                '<w:%s w:val="single" w:sz="4"/>' % s for s in
                ("top", "left", "bottom", "right", "insideH", "insideV"))
                + '</w:tblBorders>')
        else:
            pr += ('<w:tblBorders>' + "".join(
                '<w:%s w:val="nil"/>' % s for s in
                ("top", "left", "bottom", "right", "insideH", "insideV"))
                + '</w:tblBorders>')
        out = ["<w:tbl><w:tblPr>%s</w:tblPr><w:tblGrid>%s</w:tblGrid>" % (
            pr, "".join('<w:gridCol w:w="%d"/>' % w for w in widths))]
        for r in rows:
            out.append("<w:tr>")
            for ci, cell in enumerate(r):
                out.append('<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/></w:tcPr>'
                           % widths[ci])
                if not cell:
                    cell = [dict(text="", label=None)]
                for kw in cell:
                    kw = dict(kw)
                    out.append(self._p(kw.pop("text"), kw.pop("label"), **kw))
                out.append("</w:tc>")
            out.append("</w:tr>")
        out.append("</w:tbl>")
        self.xml.append("".join(out))

    def build(self, margins=(1134, 851, 1134, 1701)) -> bytes:
        top, right, bottom, left = margins
        body = "".join(self.xml) + (
            '<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>'
            '<w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" '
            'w:header="720" w:footer="720" w:gutter="0"/></w:sectPr>'
            % (top, right, bottom, left))
        document = ('<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
                    '<w:document %s><w:body>%s</w:body></w:document>'
                    % (W_NS, body))
        buf = io.BytesIO()
        with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as zf:
            zf.writestr("[Content_Types].xml", CONTENT_TYPES)
            zf.writestr("_rels/.rels", ROOT_RELS)
            zf.writestr("word/_rels/document.xml.rels", DOC_RELS)
            zf.writestr("word/document.xml", document)
            zf.writestr("word/styles.xml", STYLES)
        return buf.getvalue()


# ---------------------------------------------------------------------------
# Content pools

AGENCIES = [
    # (chủ quản lines, ban hành lines, ký hiệu viết tắt, thẩm quyền QĐ, chức danh)
    (["UBND TỈNH THỪA THIÊN HUẾ"], ["SỞ Y TẾ"], "SYT", "GIÁM ĐỐC SỞ Y TẾ", "GIÁM ĐỐC"),
    (["UBND TỈNH THỪA THIÊN HUẾ"], ["SỞ NỘI VỤ"], "SNV", "GIÁM ĐỐC SỞ NỘI VỤ", "GIÁM ĐỐC"),
    (["UBND TỈNH THỪA THIÊN HUẾ"], ["SỞ TÀI NGUYÊN", "VÀ MÔI TRƯỜNG"], "STNMT",
     "GIÁM ĐỐC SỞ TÀI NGUYÊN VÀ MÔI TRƯỜNG", "GIÁM ĐỐC"),
    ([], ["ỦY BAN NHÂN DÂN", "THÀNH PHỐ HUẾ"], "UBND",
     "ỦY BAN NHÂN DÂN THÀNH PHỐ HUẾ", "CHỦ TỊCH"),
    ([], ["ỦY BAN NHÂN DÂN", "TỈNH THỪA THIÊN HUẾ"], "UBND",
     "ỦY BAN NHÂN DÂN TỈNH THỪA THIÊN HUẾ", "CHỦ TỊCH"),
    (["UBND THÀNH PHỐ HUẾ"], ["PHÒNG TÀI CHÍNH - KẾ HOẠCH"], "TCKH",
     "TRƯỞNG PHÒNG TÀI CHÍNH - KẾ HOẠCH", "TRƯỞNG PHÒNG"),
    (["SỞ GIÁO DỤC VÀ ĐÀO TẠO", "THỪA THIÊN HUẾ"], ["TRƯỜNG THPT QUỐC HỌC"], "QH",
     "HIỆU TRƯỞNG TRƯỜNG THPT QUỐC HỌC", "HIỆU TRƯỞNG"),
    (["UBND TỈNH THỪA THIÊN HUẾ"], ["BAN QUẢN LÝ KHU KINH TẾ,", "CÔNG NGHIỆP TỈNH"],
     "BQLKKT", "TRƯỞNG BAN QUẢN LÝ KHU KINH TẾ, CÔNG NGHIỆP TỈNH", "TRƯỞNG BAN"),
    ([], ["ỦY BAN NHÂN DÂN", "PHƯỜNG PHÚ HỘI"], "UBND",
     "ỦY BAN NHÂN DÂN PHƯỜNG PHÚ HỘI", "CHỦ TỊCH"),
]
PLACES = ["Thừa Thiên Huế", "Huế", "Thành phố Huế", "Phú Hội"]
NAMES = ["Nguyễn Văn An", "Trần Thị Bích Ngọc", "Lê Quang Hùng", "Phạm Minh Tuấn",
         "Hoàng Thị Thu Hà", "Võ Đình Khoa", "Ngô Thị Lan", "Đặng Hữu Phúc"]
SUBJECTS = [
    "triển khai thực hiện Thông tư số 01/2025/TT-BNV",
    "tăng cường công tác phòng, chống dịch bệnh mùa mưa bão năm 2026",
    "kiện toàn Ban Chỉ đạo chuyển đổi số",
    "tổ chức Hội nghị tổng kết công tác năm 2025",
    "thực hiện Nghị quyết số 12-NQ/TU của Tỉnh ủy",
    "đẩy mạnh cải cách thủ tục hành chính năm 2026",
    "phê duyệt dự toán kinh phí sửa chữa trụ sở làm việc",
    "cử cán bộ tham gia lớp bồi dưỡng kiến thức quản lý nhà nước",
]
BODY_SENT = [
    "Thực hiện chỉ đạo của Ủy ban nhân dân tỉnh tại Văn bản số 1234/UBND-NC ngày 15 tháng 9 năm 2026, đơn vị đã khẩn trương triển khai các nội dung có liên quan.",
    "Các cơ quan, đơn vị căn cứ chức năng, nhiệm vụ được giao chủ động xây dựng kế hoạch, bố trí nguồn lực để thực hiện đảm bảo hiệu quả, đúng tiến độ.",
    "Trong năm 2025, toàn ngành đã hoàn thành 18/20 chỉ tiêu được giao, trong đó có 05 chỉ tiêu vượt kế hoạch.",
    "Giao Văn phòng chủ trì, phối hợp với các phòng chuyên môn theo dõi, đôn đốc và tổng hợp kết quả thực hiện, báo cáo trước ngày 30 tháng 11 năm 2026.",
    "Kinh phí thực hiện được bố trí từ nguồn ngân sách nhà nước theo phân cấp hiện hành và các nguồn hợp pháp khác.",
    "Trong quá trình triển khai, nếu có khó khăn, vướng mắc, đề nghị các đơn vị phản ánh kịp thời để xem xét, giải quyết.",
]
NOI_NHAN = ["- Như trên;", "- Như Điều 3;", "- UBND tỉnh (để b/c);",
            "- Giám đốc, các PGĐ Sở;", "- Các phòng, đơn vị thuộc Sở;",
            "- Cổng TTĐT;", "- Lưu: VT, VP."]

TYPES = {
    # type: (heading, ký hiệu loại, có thẩm quyền, có căn cứ, kính gửi)
    "cong_van": (None, None, False, False, True),
    "quyet_dinh": ("QUYẾT ĐỊNH", "QĐ", True, True, False),
    "bao_cao": ("BÁO CÁO", "BC", False, False, False),
    "to_trinh": ("TỜ TRÌNH", "TTr", False, False, True),
    "ke_hoach": ("KẾ HOẠCH", "KH", False, False, False),
    "thong_bao": ("THÔNG BÁO", "TB", False, False, False),
    "nghi_quyet": ("NGHỊ QUYẾT", "NQ", True, True, False),
    "chi_thi": ("CHỈ THỊ", "CT", False, False, False),
    "bien_ban": ("BIÊN BẢN", "BB", False, False, False),
}
SUBTITLE = {
    "quyet_dinh": "Về việc %s", "bao_cao": "Kết quả %s",
    "to_trinh": "Về việc %s", "ke_hoach": "Triển khai %s",
    "thong_bao": "Kết luận của Giám đốc về việc %s", "nghi_quyet": "Về việc %s",
    "chi_thi": "Về việc %s", "bien_ban": "Họp về việc %s",
}

HEADERS = ["table2rows", "table1row", "two_tables", "tabs", "table3col"]
SIGS = ["table", "right_aligned", "indent", "lead_tabs", "centered"]


# ---------------------------------------------------------------------------
# Generator


class Gen:
    def __init__(self, rnd: random.Random):
        self.r = rnd

    def maybe(self, prob):
        return self.r.random() < prob

    def make(self, doc_type: str, header: str, sig: str) -> tuple:
        r = self.r
        font = "Arial" if self.maybe(0.1) else None  # injected font error
        d = Doc(font=font)
        chu_quan, ban_hanh, abbr, tham_quyen, chuc_vu = r.choice(AGENCIES)
        if doc_type == "nghi_quyet":
            chu_quan, ban_hanh, abbr = [], ["HỘI ĐỒNG NHÂN DÂN", "TỈNH THỪA THIÊN HUẾ"], "HĐND"
            tham_quyen, chuc_vu = "HỘI ĐỒNG NHÂN DÂN TỈNH THỪA THIÊN HUẾ", "CHỦ TỊCH"
        heading, code, has_tq, has_cc, has_kg = TYPES[doc_type]
        draft = self.maybe(0.25)
        num = "" if draft else str(r.randint(1, 3999))
        so = "Số: %s%s/%s" % (num, "     " if draft else "",
                              ("%s-%s" % (code, abbr)) if code else
                              "%s-%s" % (abbr, r.choice(["VP", "NV", "KHTC", "TCCB"])))
        day = "     " if draft and self.maybe(0.5) else "%02d" % r.randint(1, 28)
        dia_danh = "%s, ngày %s tháng %s năm 2026" % (
            r.choice(PLACES), day, "%02d" % r.randint(1, 12) if self.maybe(0.6) else str(r.randint(10, 12)))
        subject = r.choice(SUBJECTS)
        vv = ["V/v %s" % subject] if doc_type == "cong_van" else []
        if vv and len(vv[0]) > 45:
            cut = vv[0].rfind(" ", 0, 45)
            vv = [vv[0][:cut], vv[0][cut + 1:]]
        khan = "KHẨN" if self.maybe(0.1) else None
        bold_all = self.maybe(0.2)  # sloppy: chủ quản bolded too
        hs = r.choice([12, 13, 13, 13, 14])  # header size (inject errors)

        cq = [dict(text=t, label="co_quan_chu_quan", align="center", size=hs,
                   bold=bold_all) for t in chu_quan]
        bh = [dict(text=t, label="co_quan_ban_hanh", align="center", size=hs,
                   bold=True) for t in ban_hanh]
        rule = [dict(text="_______", label=None, align="center")] if self.maybe(0.5) else []
        qh = dict(text="CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", label="quoc_hieu",
                  align="center", size=hs, bold=True)
        tn = dict(text="Độc lập - Tự do - Hạnh phúc", label="tieu_ngu",
                  align="center", size=hs + 1, bold=True)
        tn_rule = [dict(text="________________________", label=None,
                        align="center")] if self.maybe(0.5) else []
        sk = dict(text=so, label="so_ky_hieu", align="center", size=13)
        vv_p = [dict(text=t, label="trich_yeu", align="center", size=12)
                for t in vv]
        kh_p = [dict(text=khan, label="do_khan", align="center", size=13,
                     bold=True)] if khan else []
        dd = dict(text=dia_danh, label="dia_danh_ngay_thang", align="center",
                  size=14, italic=True)

        if header == "table2rows":
            d.table([[cq + bh + rule, [qh, tn] + tn_rule],
                     [[sk] + vv_p + kh_p, [dd]]])
        elif header == "table1row":
            d.table([[cq + bh + rule + [sk] + vv_p + kh_p, [qh, tn] + tn_rule + [dd]]])
        elif header == "two_tables":
            d.table([[cq + bh + rule, [qh, tn] + tn_rule]])
            d.table([[[sk] + vv_p + kh_p, [dd]]])
        elif header == "table3col":
            d.table([[cq + bh + rule, [], [qh, tn] + tn_rule],
                     [[sk] + vv_p + kh_p, [], [dd]]], widths=(3900, 300, 5300))
        else:  # tabs: pair left lines with right lines
            left = [(x["text"], x["label"], x.get("bold", False)) for x in cq + bh]
            left += [(so, "so_ky_hieu", False)]
            right = [(qh["text"], "quoc_hieu", True), (tn["text"], "tieu_ngu", True)]
            while len(right) < len(left) - 1:
                right.append(("", None, False))
            right.append((dia_danh, "dia_danh_ngay_thang", False))
            for i in range(max(len(left), len(right))):
                lt, ll, lb = left[i] if i < len(left) else ("", None, False)
                rt, rl, rb = right[i] if i < len(right) else ("", None, False)
                if lt and rt:
                    d.split(lt, rt, ll, rl, bold_l=lb, bold_r=rb,
                            italic_r=rl == "dia_danh_ngay_thang")
                elif lt:
                    d.p(lt, ll, align="left", size=13, bold=lb)
                elif rt:
                    d.p(rt, rl, align="left", size=13, bold=rb, lead_tabs=0,
                        indent=5000)
            for x in vv_p:
                d.p(x["text"], "trich_yeu", align="left", size=12)
            for x in kh_p:
                d.p(x["text"], "do_khan", align="left", size=13, bold=True)

        d.blank()
        ts = r.choice([14, 14, 14, 13, 15])
        if heading:
            d.p(heading, "trich_yeu", align="center", size=ts, bold=True)
            sub = SUBTITLE[doc_type] % subject
            sub = sub[0].upper() + sub[1:]
            d.p(sub, "trich_yeu", align="center", size=14, bold=True)
            if self.maybe(0.5):
                d.p("___________", None, align="center")
        if has_tq:
            d.blank()
            d.p(tham_quyen, "tham_quyen_ban_hanh", align="center", size=14, bold=True)
            if doc_type == "nghi_quyet":
                d.p("KHÓA VIII, KỲ HỌP THỨ %d" % r.randint(5, 15),
                    "tham_quyen_ban_hanh", align="center", size=14, bold=True)
        if has_kg:
            d.blank()
            target = r.choice(["Ủy ban nhân dân tỉnh Thừa Thiên Huế.",
                               "Các phòng, ban, đơn vị trực thuộc.",
                               "Sở Tài chính tỉnh Thừa Thiên Huế."])
            d.p("Kính gửi: %s" % target, "kinh_gui", align="center", size=14)
        if has_cc:
            d.blank()
            for t in ["Căn cứ Luật Tổ chức chính quyền địa phương ngày 19 tháng 02 năm 2025;",
                      "Căn cứ Nghị định số 30/2020/NĐ-CP ngày 05 tháng 3 năm 2020 của Chính phủ về công tác văn thư;",
                      "Theo đề nghị của Chánh Văn phòng."][: r.randint(2, 3)]:
                d.p(t, "can_cu", align="both", size=14, italic=True, first=567)
            if doc_type in ("quyet_dinh", "nghi_quyet"):
                d.p("QUYẾT ĐỊNH:" if doc_type == "quyet_dinh" else "QUYẾT NGHỊ:",
                    "noi_dung", align="center", size=14, bold=True)

        bs = r.choice([14, 14, 14, 13, 13.5, 12])  # body size, sometimes wrong
        long_body = self.maybe(0.05)
        n_par = r.randint(80, 120) if long_body else r.randint(3, 9)
        for i in range(n_par):
            if doc_type in ("quyet_dinh", "nghi_quyet") and i < 3:
                d.p("Điều %d. %s" % (i + 1, r.choice(BODY_SENT)), "noi_dung",
                    align="both", size=bs, first=567)
            elif i % 4 == 0 and self.maybe(0.6):
                d.p("%s. %s" % ("I II III IV V VI".split()[min(i // 4, 5)],
                                r.choice(["MỤC ĐÍCH, YÊU CẦU", "NHIỆM VỤ, GIẢI PHÁP",
                                          "TỔ CHỨC THỰC HIỆN", "KẾT QUẢ ĐẠT ĐƯỢC"])),
                    "noi_dung", align="both", size=bs, bold=True, first=567)
            else:
                d.p(r.choice(BODY_SENT), "noi_dung", align="both", size=bs,
                    first=567)
        if doc_type == "bao_cao" and self.maybe(0.6):
            hdr = [[dict(text=t, label="noi_dung", align="center", size=13, bold=True)]
                   for t in ("STT", "CHỈ TIÊU", "KẾ HOẠCH", "THỰC HIỆN")]
            rows = [hdr] + [
                [[dict(text=str(k), label="noi_dung", size=13)],
                 [dict(text=c, label="noi_dung", size=13)],
                 [dict(text=str(r.randint(10, 99)), label="noi_dung", size=13)],
                 [dict(text=str(r.randint(10, 99)), label="noi_dung", size=13)]]
                for k, c in enumerate(["Tổng thu ngân sách", "Hộ nghèo giảm",
                                       "Hồ sơ trực tuyến"], 1)]
            d.table(rows, widths=(800, 4300, 2200, 2200), borders=True)
        d.p("Trên đây là nội dung %s, đề nghị các đơn vị quan tâm thực hiện./."
            % (heading.lower() if heading else "công văn"), "noi_dung",
            align="both", size=bs, first=567)
        d.blank()

        # ---- signature block -----------------------------------------------
        signer = r.choice(NAMES)
        quyen = r.choice(["", "", "KT", "TM", "TL", "Q"])
        if quyen == "KT":
            lines = ["KT. %s" % chuc_vu, "PHÓ %s" % chuc_vu]
        elif quyen == "TM":
            org = "HỘI ĐỒNG NHÂN DÂN" if doc_type == "nghi_quyet" else (
                "ỦY BAN NHÂN DÂN" if ban_hanh[0] == "ỦY BAN NHÂN DÂN" else " ".join(ban_hanh))
            lines = ["TM. %s" % org, chuc_vu]
        elif quyen == "TL":
            lines = ["TL. %s" % chuc_vu, "CHÁNH VĂN PHÒNG"]
        elif quyen == "Q":
            lines = ["Q. %s" % chuc_vu]
        else:
            lines = [chuc_vu]
        sig_sz = r.choice([14, 14, 14, 13, 12])
        name_sz = r.choice([14, 14, 14, 12, 13])
        da_ky = self.maybe(0.25)
        sig_paras = [dict(text=t, label="chuc_danh", size=sig_sz, bold=True) for t in lines]
        gap = [dict(text="", label=None)] * r.randint(1, 3)
        if da_ky:
            gap = gap[:1] + [dict(text="(Đã ký)", label="khac", italic=True, size=13)] + gap[:1]
        name_p = [dict(text=signer, label="nguoi_ky", size=name_sz, bold=True)]
        nn = [dict(text="Nơi nhận:", label="noi_nhan", size=12, bold=True, italic=True)]
        nn += [dict(text=t, label="noi_nhan", size=11)
               for t in r.sample(NOI_NHAN[:-1], r.randint(1, 4)) + [NOI_NHAN[-1]]]

        def emit(ps, **over):
            for kw in ps:
                kw = dict(kw, **over)
                d.p(kw.pop("text"), kw.pop("label"), **kw)

        if doc_type == "bien_ban":
            # two signers side by side: thư ký (left), chủ trì (right)
            left = [dict(text="THƯ KÝ", label="chuc_danh", align="center", size=14, bold=True),
                    dict(text="", label=None),
                    dict(text=r.choice(NAMES), label="nguoi_ky", align="center", size=14, bold=True)]
            right = [dict(text="CHỦ TRÌ", label="chuc_danh", align="center", size=14, bold=True),
                     dict(text="", label=None),
                     dict(text=signer, label="nguoi_ky", align="center", size=14, bold=True)]
            d.table([[left, right]], widths=(4650, 4650))
            emit(nn, align="left")
        elif sig == "table":
            d.table([[nn, [dict(k, align="center") for k in sig_paras + gap + name_p]]],
                    widths=(4500, 4800))
        else:
            if sig == "right_aligned":
                over = dict(align="right")
            elif sig == "indent":
                over = dict(align="center", indent=r.choice([4536, 5103]))
            elif sig == "lead_tabs":
                over = dict(align="left", lead_tabs=r.choice([5, 6, 7]))
            else:  # centered: no position cue at all
                over = dict(align="center")
            emit(sig_paras + gap + name_p, **over)
            d.blank()
            emit(nn, align="left")

        if self.maybe(0.15):
            d.p("PHỤ LỤC", "phu_luc", align="center", size=14, bold=True, page_break=True)
            d.p("Danh sách đơn vị tham gia (Kèm theo %s)" % so.replace("Số: ", "văn bản số "),
                "phu_luc", align="center", size=14, italic=True)
            for t in ["1. Văn phòng Sở", "2. Phòng Kế hoạch - Tài chính", "3. Thanh tra Sở"]:
                d.p(t, "phu_luc", align="left", size=14)

        meta = {"type": doc_type, "header": header, "signature": sig,
                "draft": draft, "font_error": bool(font), "long_body": long_body,
                "da_ky": da_ky, "quyen": quyen, "agency": ban_hanh}
        return d, meta


def gold_units(content: bytes, gold: list) -> tuple:
    """Map per-paragraph gold labels onto the checker's unit ids."""
    layout = inspect_docx(content)
    units = build_units(layout)
    labels, issues = {}, []
    for u in units:
        g = gold[u["para"]] if u["para"] < len(gold) else None
        if isinstance(g, tuple):
            if u["id"].endswith("L"):
                g = g[0]
            elif u["id"].endswith("R"):
                g = g[1]
            else:
                issues.append("para %d: tab line not split by layout" % u["para"])
                g = g[0]
        if g is None:
            issues.append("unit %s has no gold label: %r" % (u["id"], u["text"][:40]))
            g = "khac"
        labels[u["id"]] = g
    return labels, issues


def main(argv=None) -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default=os.path.join(
        os.path.dirname(os.path.abspath(__file__)), "samples", "synth"))
    ap.add_argument("--n", type=int, default=60)
    ap.add_argument("--seed", type=int, default=2026)
    args = ap.parse_args(argv)
    os.makedirs(args.out, exist_ok=True)
    rnd = random.Random(args.seed)
    gen = Gen(rnd)
    types = list(TYPES)
    n_issues = 0
    for i in range(args.n):
        # cycle the variant axes so every combination family is covered
        doc_type = types[i % len(types)]
        header = HEADERS[(i // len(types) + i) % len(HEADERS)]
        sig = SIGS[(i // 3 + i) % len(SIGS)]
        d, meta = gen.make(doc_type, header, sig)
        content = d.build()
        labels, issues = gold_units(content, d.gold)
        n_issues += len(issues)
        stem = "%03d_%s_%s_%s" % (i, doc_type, header, sig)
        with open(os.path.join(args.out, stem + ".docx"), "wb") as fh:
            fh.write(content)
        with open(os.path.join(args.out, stem + ".labels.json"), "w",
                  encoding="utf-8") as fh:
            json.dump({"document_type": doc_type, "labels": labels,
                       "meta": meta, "issues": issues}, fh,
                      ensure_ascii=False, indent=1)
    print("wrote %d documents to %s (%d gold-mapping issues)"
          % (args.n, args.out, n_issues))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
