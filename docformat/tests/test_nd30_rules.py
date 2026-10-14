"""Rules from the verified NĐ30 Phụ lục I text (mirrors
internal/docformat/nd30_rules_test.go): modes first/rest, regex_prop,
skip_when, run colour, page numbering, ô 11/12/13."""

import io
import os
import sys
import unittest
import zipfile

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
sys.path.insert(0, os.path.dirname(__file__))

from docformat import check_document  # noqa: E402
from docformat.layout import Para, inspect_docx  # noqa: E402
from docformat.rules import evaluate, load_rule_set  # noqa: E402
from docformat.segment import Segmented, segment  # noqa: E402
from test_check import STYLES, p, row, tc  # noqa: E402
from test_segment_real import tbl  # noqa: E402

W = ('xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" '
     'xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"')


def docx(body, header="", footer="", title_pg=False):
    refs, rels = "", ""
    if header:
        refs += '<w:headerReference w:type="default" r:id="rIdH"/>'
        rels += '<Relationship Id="rIdH" Type="x" Target="header1.xml"/>'
    if footer:
        refs += '<w:footerReference w:type="default" r:id="rIdF"/>'
        rels += '<Relationship Id="rIdF" Type="x" Target="/word/footer1.xml"/>'
    if title_pg:
        refs += "<w:titlePg/>"
    sect = ('<w:sectPr>%s<w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1247" '
            'w:right="1077" w:bottom="1247" w:left="1814"/></w:sectPr>' % refs)
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as zf:
        zf.writestr("word/document.xml", '<?xml version="1.0"?><w:document %s><w:body>%s%s'
                    "</w:body></w:document>" % (W, body, sect))
        zf.writestr("word/styles.xml", STYLES)
        zf.writestr("word/_rels/document.xml.rels",
                    '<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/'
                    'package/2006/relationships">%s</Relationships>' % rels)
        if header:
            zf.writestr("word/header1.xml", "<w:hdr %s>%s</w:hdr>" % (W, header))
        if footer:
            zf.writestr("word/footer1.xml", "<w:ftr %s>%s</w:ftr>" % (W, footer))
    return buf.getvalue()


class _L:
    def __init__(self, paras):
        self.paragraphs = paras
        self.sections = []
        self.page_numbering = {}


def comp_eval(key, paras, checks):
    for i, x in enumerate(paras):
        x.index, x.zone = i, "left"
    seg = Segmented()
    seg.components[key] = {"found": True, "paras": list(range(len(paras))),
                           "zones": ["left"] * len(paras), "texts": [], "text": "", "zone": "left"}
    out = evaluate(_L(paras), seg, {"checks": checks})
    return {c["id"]: c for c in out}


def line(text, size, bold, italic):
    return Para(index=0, text=text, size_pt=size, bold=bold, italic=italic)


def rule(doc_type, cid):
    return next(c for c in load_rule_set(doc_type)["checks"] if c["id"] == cid)


class TestModes(unittest.TestCase):
    def test_first_rest(self):
        ids = ["noi_nhan.label.size", "noi_nhan.label.italic", "noi_nhan.label.bold",
               "noi_nhan.list.size", "noi_nhan.list.italic"]
        checks = [rule("cong_van", i) for i in ids]
        ok = comp_eval("noi_nhan", [line("Nơi nhận:", 12, True, True),
                                    line("- Như trên;", 11, False, False),
                                    line("- Lưu: VT.", 11, False, False)], checks)
        self.assertEqual({i: ok[i]["status"] for i in ids}, {i: "pass" for i in ids})
        bad = comp_eval("noi_nhan", [line("Nơi nhận:", 11, False, True),
                                     line("- A;", 12, False, False), line("- B;", 11, False, False),
                                     line("- C;", 11, False, False), line("- Lưu: VT.", 11, False, False)], checks)
        self.assertEqual(bad["noi_nhan.label.size"]["status"], "warn")
        self.assertEqual(bad["noi_nhan.label.bold"]["status"], "warn")
        self.assertEqual(bad["noi_nhan.list.size"]["status"], "pass")  # 3/4 ≥ 70%
        one = comp_eval("noi_nhan", [line("Nơi nhận:", 12, True, True)], checks)
        self.assertEqual(one["noi_nhan.list.size"]["status"], "skip")


class TestRegexProp(unittest.TestCase):
    def dieu(self, bold_head):
        runs = [{"text": "Điều 1. ", "bold": bold_head},
                {"text": "Ban hành kèm theo Quyết định này Kế hoạch năm 2026.", "bold": False}]
        return Para(index=0, text="".join(r["text"] for r in runs), runs=runs, bold=False)

    def test_matched_runs(self):
        chk = [rule("quyet_dinh", "noi_dung.dieu.bold")]
        plain = Para(index=0, text="Căn cứ đề nghị của Chánh Văn phòng.", bold=False)
        r = comp_eval("noi_dung", [plain, self.dieu(True)], chk)["noi_dung.dieu.bold"]
        self.assertEqual((r["status"], r["actual"]), ("pass", {"satisfied": 1, "of": 1}))
        r = comp_eval("noi_dung", [self.dieu(True), self.dieu(False)], chk)["noi_dung.dieu.bold"]
        self.assertEqual(r["status"], "warn")
        r = comp_eval("noi_dung", [Para(index=0, text="x")], chk)["noi_dung.dieu.bold"]
        self.assertEqual(r["status"], "skip")


class TestSkipWhen(unittest.TestCase):
    def test_indent(self):
        chk = [rule("cong_van", "noi_dung.first_line_indent")]
        r = comp_eval("noi_dung", [Para(index=0, text="a")], chk)["noi_dung.first_line_indent"]
        self.assertEqual(r["status"], "skip")
        paras = [Para(index=0, text="a", indent_first_line_mm=10.0),
                 Para(index=0, text="- b", indent_first_line_mm=0.0, indent_hanging_mm=6.35),
                 Para(index=0, text="c", indent_first_line_mm=0.0, in_table=True)]
        r = comp_eval("noi_dung", paras, chk)["noi_dung.first_line_indent"]
        self.assertEqual((r["status"], r["actual"]), ("pass", {"satisfied": 1, "of": 1}))


class TestColorAndPageNumber(unittest.TestCase):
    def test_color(self):
        body = (p("Mặc định") + '<w:p><w:r><w:rPr><w:color w:val="FF0000"/></w:rPr>'
                "<w:t>Đỏ</w:t></w:r></w:p>")
        layout = inspect_docx(docx(body))
        self.assertEqual([x.color for x in layout.paragraphs], ["auto", "FF0000"])

    def test_page_number(self):
        body = p("Nội dung.", align="both")
        fld = ('<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:fldSimple w:instr=" PAGE ">'
               "<w:r><w:t>2</w:t></w:r></w:fldSimple></w:p>")
        cases = [
            (dict(header=fld, title_pg=True), "pass", True, False, True, True),
            (dict(header=fld), "warn", True, False, True, False),
            (dict(footer=fld, title_pg=True), "warn", False, True, False, True),
            (dict(), "warn", False, False, False, False),
        ]
        for kw, status, hdr, ftr, cen, tpg in cases:
            rep = check_document(docx(body, **kw), segmenter="heuristic")
            c = next(x for x in rep["checks"] if x["id"] == "page.number")
            self.assertEqual(c["status"], status, kw)
            self.assertEqual(c["actual"], {"header_page_field": hdr, "footer_page_field": ftr,
                                           "centered": cen, "title_pg": tpg}, kw)


def bottom_doc(nn_tail=(), after=""):
    head = tbl(
        row(tc(p("SỞ NỘI VỤ", align="center", bold=True, size=13)),
            tc(p("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", align="center", bold=True, size=13),
               p("Độc lập - Tự do - Hạnh phúc", align="center", bold=True, size=14))),
        row(tc(p("Số: 45/SNV-VP", align="center", size=13),
               p("V/v rà soát vị trí việc làm", align="center", size=12)),
            tc(p("Huế, ngày 05 tháng 10 năm 2026", align="center", italic=True, size=14))))
    body = (p("Kính gửi: Các phòng, ban thuộc Sở.", align="center", size=14)
            + p("Sở Nội vụ đề nghị các phòng, ban rà soát, điện thoại về Văn phòng khi cần; "
                "email báo cáo trước ngày 20.", align="both", size=14)
            + p("LƯU HÀNH NỘI BỘ", align="center", size=13)
            + p("Đề nghị các phòng, ban thực hiện./.", align="both", size=14))
    left = [p("Nơi nhận:", bold=True, italic=True, size=12), p("- Như trên;", size=11),
            p("- Lưu: VT, VP, NVA.15.", size=11)] + list(nn_tail)
    right = [p("GIÁM ĐỐC", align="center", bold=True, size=14), p("", align="center"),
             p("Lê Thị Dung", align="center", bold=True, size=14)]
    return docx(head + body + tbl(row(tc(*left), tc(*right))) + after)


class TestBottomComponents(unittest.TestCase):
    def test_found(self):
        seg = segment(inspect_docx(bottom_doc(
            [p("NVA.15", size=11)],
            p("Địa chỉ: 01 Lê Lai, Huế; ĐT: 0234 3822 111; Email: snv@hue.gov.vn",
              align="center", size=11))))
        self.assertEqual(seg.comp("chi_dan_luu_hanh")["text"], "LƯU HÀNH NỘI BỘ")
        self.assertEqual(seg.comp("ky_hieu_soan_thao")["text"], "NVA.15")
        self.assertTrue(seg.comp("dia_chi_co_quan")["text"].startswith("Địa chỉ: 01 Lê Lai"))
        self.assertIn("điện thoại về Văn phòng", seg.comp("noi_dung")["text"])

    def test_not_found(self):
        seg = segment(inspect_docx(bottom_doc()))
        self.assertFalse(seg.comp("ky_hieu_soan_thao")["found"])
        self.assertFalse(seg.comp("dia_chi_co_quan")["found"])
        seg = segment(inspect_docx(bottom_doc([p("TTr/2b", size=11), p("- Giám đốc (để b/c).", size=11)])))
        self.assertFalse(seg.comp("ky_hieu_soan_thao")["found"])
        seg = segment(inspect_docx(bottom_doc([p("TTr/2b", size=11)])))
        self.assertEqual(seg.comp("ky_hieu_soan_thao")["text"], "TTr/2b")


if __name__ == "__main__":
    unittest.main()
