"""Segmentation regressions on layouts modelled after real văn bản hành chính
(UBND/Sở templates): multi-table headers, "SỞ ..." agencies, titles that
mention "năm 2025", TM./KT. signature tables, tab- or indent-placed signature
blocks, data tables in the body, decorative rule lines, content controls."""

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from test_check import build_docx, doc, p, row, tc  # noqa: E402

from docformat import check_document, inspect_document  # noqa: E402


def tbl(*rows, borders=False, cols=(4535, 4535)):
    pr = '<w:tblW w:w="9070" w:type="dxa"/>'
    if borders:
        pr += ('<w:tblBorders><w:top w:val="single"/><w:left w:val="single"/>'
               '<w:bottom w:val="single"/><w:right w:val="single"/>'
               '<w:insideH w:val="single"/><w:insideV w:val="single"/>'
               '</w:tblBorders>')
    grid = "".join("<w:gridCol w:w='%d'/>" % c for c in cols)
    return ("<w:tbl><w:tblPr>%s</w:tblPr><w:tblGrid>%s</w:tblGrid>%s</w:tbl>"
            % (pr, grid, "".join(rows)))


def ptabs(text, n_tabs=0, stops=(), align="left", bold=False, indent=None):
    """Paragraph with leading tabs and/or a left indent (twips)."""
    ppr = "<w:pPr>"
    if stops:
        ppr += "<w:tabs>%s</w:tabs>" % "".join(
            '<w:tab w:val="center" w:pos="%d"/>' % s for s in stops)
    if indent:
        ppr += '<w:ind w:left="%d"/>' % indent
    ppr += '<w:jc w:val="%s"/></w:pPr>' % align
    rpr = "<w:rPr><w:b/></w:rPr>" if bold else ""
    tabs = "<w:r><w:tab/></w:r>" * n_tabs
    return "<w:p>%s%s<w:r>%s<w:t>%s</w:t></w:r></w:p>" % (ppr, tabs, rpr, text)


def comps_of(content):
    d = inspect_document(content)
    return d, d["components"]


class TestSoAgencyQuyetDinh(unittest.TestCase):
    """UBND tỉnh / SỞ Y TẾ header, QĐ with thẩm quyền line, signature table."""

    def setUp(self):
        header = tbl(
            row(tc(p("UBND TỈNH THỪA THIÊN HUẾ", align="center", size=13),
                   p("SỞ Y TẾ", align="center", bold=True, size=13),
                   p("_______", align="center")),
                tc(p("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", align="center",
                     bold=True, size=13),
                   p("Độc lập - Tự do - Hạnh phúc", align="center", bold=True),
                   p("________________________", align="center"))),
            row(tc(p("Số: 1234/QĐ-SYT", align="center")),
                tc(p("Thừa Thiên Huế, ngày 05 tháng 10 năm 2026",
                     align="center", italic=True))),
        )
        sig = tbl(
            row(tc(p("Nơi nhận:", bold=True, italic=True, size=12),
                   p("- Như Điều 3;", size=11),
                   p("- UBND tỉnh (để b/c);", size=11),
                   p("- Lưu: VT, TCCB.", size=11)),
                tc(p("KT. GIÁM ĐỐC", align="center", bold=True),
                   p("PHÓ GIÁM ĐỐC", align="center", bold=True),
                   p("Trần Văn Bình", align="center", bold=True))),
        )
        body = (
            header
            + p("QUYẾT ĐỊNH", align="center", bold=True)
            + p("Về việc ban hành Kế hoạch triển khai Thông tư số 01/2025/TT-BYT",
                align="center", bold=True)
            + p("______", align="center")
            + p("GIÁM ĐỐC SỞ Y TẾ", align="center", bold=True)
            + p("Căn cứ Luật Tổ chức chính quyền địa phương;", align="both",
                italic=True)
            + p("Căn cứ Quyết định số 10/2024/QĐ-UBND;", align="both",
                italic=True)
            + p("QUYẾT ĐỊNH:", align="center", bold=True)
            + p("Điều 1. Ban hành kèm theo Quyết định này Kế hoạch ...",
                align="both")
            + p("Điều 2. Quyết định này có hiệu lực kể từ ngày ký.",
                align="both")
            + sig
        )
        self.content = build_docx(doc(body))
        self.d, self.c = comps_of(self.content)

    def test_agencies_not_so_ky_hieu(self):
        self.assertEqual(self.c["co_quan_chu_quan"]["text"],
                         "UBND TỈNH THỪA THIÊN HUẾ")
        self.assertEqual(self.c["co_quan_ban_hanh"]["text"], "SỞ Y TẾ")
        self.assertEqual(self.c["so_ky_hieu"]["text"], "Số: 1234/QĐ-SYT")

    def test_type_and_trich_yeu(self):
        self.assertEqual(self.d["detected_type"], "quyet_dinh")
        self.assertEqual(self.c["trich_yeu"]["text"].splitlines()[0],
                         "QUYẾT ĐỊNH")
        self.assertNotIn("GIÁM ĐỐC SỞ Y TẾ", self.c["trich_yeu"]["text"])
        self.assertEqual(self.c["tham_quyen_ban_hanh"]["text"],
                         "GIÁM ĐỐC SỞ Y TẾ")

    def test_signature_and_noi_nhan(self):
        self.assertEqual(self.c["chuc_danh"]["text"],
                         "KT. GIÁM ĐỐC\nPHÓ GIÁM ĐỐC")
        self.assertEqual(self.c["nguoi_ky"]["text"], "Trần Văn Bình")
        self.assertEqual(len(self.c["noi_nhan"]["paras"]), 4)

    def test_body_excludes_tail(self):
        body = self.c["noi_dung"]["text"]
        self.assertIn("Điều 1.", body)
        self.assertNotIn("Nơi nhận", body)
        self.assertNotIn("Trần Văn Bình", body)


class TestBaoCaoTwoHeaderTables(unittest.TestCase):
    """Header split over two tables, title mentions "năm 2025", a bordered
    data table in the body, flat right-aligned signature, flat nơi nhận."""

    def setUp(self):
        h1 = tbl(row(
            tc(p("ỦY BAN NHÂN DÂN", align="center", bold=True),
               p("THÀNH PHỐ HUẾ", align="center", bold=True)),
            tc(p("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", align="center",
                 bold=True),
               p("Độc lập - Tự do - Hạnh phúc", align="center", bold=True))))
        h2 = tbl(row(
            tc(p("Số:      /BC-UBND", align="center")),
            tc(p("Huế, ngày      tháng 10 năm 2026", align="center",
                 italic=True))))
        data = tbl(
            row(tc(p("STT", bold=True)), tc(p("CHỈ TIÊU", bold=True)),
                tc(p("TỔNG", bold=True))),
            row(tc(p("1")), tc(p("Dân số")), tc(p("1.000"))),
            row(tc(p("2")), tc(p("Hộ nghèo")), tc(p("12"))),
            borders=True, cols=(1000, 5000, 3070))
        body = (
            h1 + h2
            + p("BÁO CÁO", align="center", bold=True)
            + p("Kết quả thực hiện Nghị quyết số 12-NQ/TU năm 2025",
                align="center", bold=True)
            + p("I. TÌNH HÌNH CHUNG", bold=True, align="both")
            + p("Trong năm 2025, UBND thành phố đã triển khai ...",
                align="both")
            + data
            + p("Trên đây là báo cáo của UBND thành phố.", align="both")
            + p("TM. ỦY BAN NHÂN DÂN", align="right", bold=True)
            + p("CHỦ TỊCH", align="right", bold=True)
            + p("Nguyễn Văn Cường", align="right", bold=True)
            + p("Nơi nhận:", bold=True, italic=True)
            + p("- UBND tỉnh;")
            + p("- Lưu: VT.")
        )
        self.d, self.c = comps_of(build_docx(doc(body)))

    def test_header_over_two_tables(self):
        self.assertIn("/BC-UBND", self.c["so_ky_hieu"]["text"])
        self.assertTrue(self.c["dia_danh_ngay_thang"]["found"])
        # one agency name wrapped over two bold lines — no chủ quản
        self.assertEqual(self.c["co_quan_ban_hanh"]["text"],
                         "ỦY BAN NHÂN DÂN\nTHÀNH PHỐ HUẾ")
        self.assertNotIn("co_quan_chu_quan", self.c)

    def test_report_title(self):
        self.assertEqual(self.d["detected_type"], "bao_cao")
        self.assertIn("năm 2025", self.c["trich_yeu"]["text"])
        self.assertNotIn("Kết quả",
                         self.c["dia_danh_ngay_thang"]["text"])

    def test_data_table_is_body(self):
        self.assertIn("TỔNG", self.c["noi_dung"]["text"])
        self.assertNotIn("TỔNG", self.c["chuc_danh"]["text"])

    def test_signature_and_noi_nhan(self):
        self.assertEqual(self.c["chuc_danh"]["text"],
                         "TM. ỦY BAN NHÂN DÂN\nCHỦ TỊCH")
        self.assertEqual(self.c["nguoi_ky"]["text"], "Nguyễn Văn Cường")
        self.assertEqual(len(self.c["noi_nhan"]["paras"]), 3)


class TestCongVanTabSignature(unittest.TestCase):
    """Công văn: V/v in the header's left cell, số without a type code,
    signature pushed right with leading tabs / a left indent."""

    def build(self, sig_style):
        header = tbl(
            row(tc(p("UBND TỈNH THỪA THIÊN HUẾ", align="center"),
                   p("SỞ NỘI VỤ", align="center", bold=True)),
                tc(p("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", align="center",
                     bold=True),
                   p("Độc lập - Tự do - Hạnh phúc", align="center",
                     bold=True))),
            row(tc(p("Số: 45/SNV-VP", align="center"),
                   p("V/v thông báo lịch họp giao ban", align="center",
                     size=12),
                   p("tháng 10/2026", align="center", size=12)),
                tc(p("Thừa Thiên Huế, ngày 05 tháng 10 năm 2026",
                     align="center", italic=True))),
        )
        if sig_style == "tabs":
            sig = (ptabs("KT. GIÁM ĐỐC", 6, bold=True)
                   + ptabs("PHÓ GIÁM ĐỐC", 6, bold=True)
                   + ptabs("Lê Thị Dung", 6, bold=True))
        else:
            sig = (ptabs("KT. GIÁM ĐỐC", indent=5000, align="center", bold=True)
                   + ptabs("PHÓ GIÁM ĐỐC", indent=5000, align="center",
                           bold=True)
                   + ptabs("Lê Thị Dung", indent=5000, align="center",
                           bold=True))
        body = (
            header
            + p("Kính gửi: Các phòng, ban thuộc Sở.", align="center")
            + p("Sở Nội vụ thông báo lịch họp giao ban như sau: ...",
                align="both")
            + sig
            + p("Nơi nhận:", bold=True, italic=True)
            + p("- Như trên;")
            + p("- Lưu: VT, VP.")
        )
        return comps_of(build_docx(doc(body)))

    def check(self, d, c):
        self.assertEqual(d["detected_type"], "cong_van")
        self.assertEqual(c["co_quan_ban_hanh"]["text"], "SỞ NỘI VỤ")
        self.assertEqual(c["so_ky_hieu"]["text"], "Số: 45/SNV-VP")
        self.assertEqual(c["trich_yeu"]["text"],
                         "V/v thông báo lịch họp giao ban\ntháng 10/2026")
        self.assertTrue(c["kinh_gui"]["found"])
        self.assertEqual(c["chuc_danh"]["text"], "KT. GIÁM ĐỐC\nPHÓ GIÁM ĐỐC")
        self.assertEqual(c["nguoi_ky"]["text"], "Lê Thị Dung")
        self.assertEqual(len(c["noi_nhan"]["paras"]), 3)

    def test_tab_signature(self):
        self.check(*self.build("tabs"))

    def test_indent_signature(self):
        self.check(*self.build("indent"))


class TestDetectType(unittest.TestCase):
    def test_heading_wins_over_mentions(self):
        from importlib import import_module
        S = import_module("docformat.segment")
        cases = {
            "KẾ HOẠCH\nTriển khai Thông tư số 01/2025/TT-BNV": "ke_hoach",
            "TỜ TRÌNH\nVề việc ban hành Quyết định quy định ...": "to_trinh",
            "BÁO CÁO\nKết quả thực hiện Nghị quyết số 12": "bao_cao",
            "V/v thông báo lịch họp": "cong_van",
            "V/v báo cáo số liệu": "cong_van",
        }
        for trich, want in cases.items():
            self.assertEqual(S.detect_type(trich, ""), want, trich)
        self.assertEqual(S.detect_type("", "", "Số: 12/TTr-UBND"), "to_trinh")
        self.assertEqual(S.detect_type("", "", "Số: 12/UBND-VP"), "cong_van")
        self.assertEqual(S.detect_type("", "", "Số: 5/2025/NĐ-CP"), "nghi_dinh")

    def test_agency_and_place_patterns(self):
        from importlib import import_module
        S = import_module("docformat.segment")
        for t in ("SỞ Y TẾ", "SỞ NỘI VỤ TỈNH KHÁNH HÒA"):
            self.assertFalse(S._SO_KY_HIEU_RE.match(S.fold(t)), t)
            self.assertTrue(S._looks_agency(t, S.fold(t)), t)
            self.assertFalse(S._KHAN_RE.match(S.fold(t)), t)
        for t in ("Số: 12/QĐ-UBND", "Số:      /BC-UBND", "Số 7/UBND-VP"):
            self.assertTrue(S._SO_KY_HIEU_RE.match(S.fold(t)), t)
        self.assertFalse(S._DIA_DANH_RE.search(
            S.fold("Kết quả công tác năm 2025")))
        self.assertTrue(S._DIA_DANH_RE.search(
            S.fold("Huế, ngày      tháng     năm 2026")))
        for t in ("TM. ỦY BAN NHÂN DÂN", "TL. CHỦ TỊCH", "TUQ. CHỦ TỊCH",
                  "Q. GIÁM ĐỐC"):
            self.assertTrue(S._SIG_OPEN_RE.match(S.fold(t)), t)


class TestSplitSideFormatting(unittest.TestCase):
    """Tab-split header: each side is measured with its own runs — the
    longer quốc hiệu on the right must not lend its bold/size to the cơ quan
    on the left."""

    def test_side_props(self):
        para = (
            '<w:p><w:pPr><w:tabs><w:tab w:val="center" w:pos="6804"/></w:tabs>'
            '</w:pPr><w:r><w:rPr><w:sz w:val="26"/></w:rPr><w:t>SỞ Y TẾ</w:t></w:r>'
            '<w:r><w:tab/></w:r><w:r><w:rPr><w:b/><w:sz w:val="22"/></w:rPr>'
            '<w:t>CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM</w:t></w:r></w:p>')
        d = inspect_document(build_docx(doc(para)))
        from docformat.layout import inspect_docx
        lay = inspect_docx(build_docx(doc(para)))
        sp = lay.paragraphs[0]
        self.assertEqual(sp.zone, "split")
        self.assertEqual(sp.left_props["size_pt"], 13.0)
        self.assertFalse(sp.left_props["bold"])
        self.assertEqual(sp.right_props["size_pt"], 11.0)
        self.assertTrue(sp.right_props["bold"])
        rep = check_document(build_docx(doc(para)), segmenter="heuristic")
        qh = [c for c in rep["checks"] if c["id"] == "quoc_hieu.size"][0]
        # measured on the right side's run (11pt), not the left (13pt)
        self.assertEqual(qh["evidence"][0]["actual"], 11.0)
        self.assertTrue(d["components"]["quoc_hieu"]["found"])


class TestRuleOverride(unittest.TestCase):
    def test_type_check_replaces_base_by_id(self):
        from docformat.rules import load_rule_set
        rs = load_rule_set("bien_ban")
        sig = [c for c in rs["checks"] if c["id"] == "signature.zone"]
        self.assertEqual(len(sig), 1)
        self.assertEqual(sig[0]["value"], ["left", "right"])


class TestContentControls(unittest.TestCase):
    def test_sdt_wrapped_body(self):
        inner = (p("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", align="center",
                   bold=True)
                 + p("Độc lập - Tự do - Hạnh phúc", align="center", bold=True))
        body = ("<w:sdt><w:sdtPr/><w:sdtContent>%s</w:sdtContent></w:sdt>"
                % inner)
        _, c = comps_of(build_docx(doc(body)))
        self.assertTrue(c["quoc_hieu"]["found"])
        self.assertTrue(c["tieu_ngu"]["found"])


class TestCheckOnRealisticDoc(unittest.TestCase):
    def test_required_components_present(self):
        t = TestSoAgencyQuyetDinh()
        t.setUp()
        rep = check_document(t.content, document_type="auto")
        self.assertEqual(rep["document_type"]["detected"], "quyet_dinh")
        missing = [c["component"] for c in rep["checks"]
                   if c["id"].endswith(".present") and c["status"] == "fail"]
        self.assertEqual(missing, [])


if __name__ == "__main__":
    unittest.main()
