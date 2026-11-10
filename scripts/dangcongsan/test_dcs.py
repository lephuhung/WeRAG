"""python3 -I -m unittest discover -s scripts/dangcongsan -p 'test_*.py'"""
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import dcs_build as b  # noqa: E402
import dcs_classify as c  # noqa: E402
import dcs_fetch as f  # noqa: E402
import dcs_text as t  # noqa: E402


class Encodings(unittest.TestCase):
    def test_tcvn3(self):
        self.assertEqual(t.tcvn3_to_unicode("Ban chÊp hµnh Trung \xad¬ng"), "Ban chấp hành Trung ương")
        self.assertEqual(t.convert_run("§¶ng céng s¶n", ".VnTime"), "Đảng cộng sản")
        self.assertEqual(t.convert_run("nghÞ quyÕt", ".VnTimeH"), "NGHỊ QUYẾT")

    def test_vni(self):
        self.assertEqual(t.vni_to_unicode("Ban chaáp haønh Trung öông Ñaûng"), "Ban chấp hành Trung ương Đảng")
        self.assertEqual(t.vni_to_unicode("Nghò quyeát"), "Nghị quyết")

    def test_detect(self):
        self.assertEqual(c.encoding_of("Ban Chấp hành Trung ương Đảng Cộng sản Việt Nam " * 3), "unicode")
        self.assertEqual(c.encoding_of("Ban chÊp hµnh Trung \xad¬ng §¶ng céng s¶n ViÖt Nam " * 3), "tcvn3")


class Identity(unittest.TestCase):
    def test_numbers(self):
        self.assertEqual(b.canonical_number("76/KL/TW"), "76-KL/TW")
        self.assertEqual(b.canonical_number("36/NQ-TW"), "36-NQ/TW")
        self.assertEqual(b.canonical_number(" 40 - HD/BTCTW"), "40-HD/BTCTW")
        self.assertEqual(b.canonical_number("342-QÐ/BTCTW"), "342-QĐ/BTCTW")
        self.assertEqual(b.canonical_number("1225/QĐ-TTg"), "1225/QĐ-TTg")  # State: left as is
        self.assertTrue(b.PARTY_NUMBER_RE.match("529-CV/VPTW/nb"))

    def test_slug(self):
        self.assertEqual(b.from_slug("https://x/van-ban-cua-dang/quyet-dinh-so-245-qd-tw-ngay-24-01-2025-cua-bo-chinh-tri.html"),
                         ("245-QĐ/TW", "2025-01-24"))
        self.assertEqual(b.from_slug("https://x/huong-dan-19-hd-vptw-ngay-10-8-2026-cua-van-phong.html"), ("", "2026-08-10"))

    def test_type(self):
        self.assertEqual(b.doc_type_of("114-KL/TW", "kết luận", "")[0], "Kết luận")
        self.assertEqual(b.doc_type_of("228-QĐ/TW", "Quy định", "")[0], "Quy định")
        self.assertEqual(b.doc_type_of("71-KH/BTCTW", "Văn bản khác", "")[0], "Kế hoạch")
        self.assertEqual(b.doc_type_of("1232-CV/BTCTW", "Hướng dẫn", "")[0], "Công văn")

    def test_title(self):
        self.assertEqual(b.full_title("Kết luận", "114-KL/TW", "Bộ Chính trị", "Về tiếp tục hoàn thiện bộ máy"),
                         "Kết luận số 114-KL/TW của Bộ Chính trị về tiếp tục hoàn thiện bộ máy")
        self.assertEqual(b.full_title("Quy định", "228-QĐ/TW", "Ban Bí thư", "Quy định của Ban Bí thư về sinh hoạt đảng trực tuyến"),
                         "Quy định số 228-QĐ/TW của Ban Bí thư về sinh hoạt đảng trực tuyến")

    def test_identity_from_text(self):
        own = b.identity_from_text("BAN CHẤP HÀNH TRUNG ƯƠNG BAN TỔ CHỨC * Số 40 - HD/BTCTW ĐẢNG CỘNG SẢN VIỆT NAM "
                                   "Hà Nội, ngày 3 tháng 10 năm 2025 HƯỚNG DẪN … Căn cứ Kết luận số 210-KL/TW, ngày 12 tháng 11 năm 2025")
        self.assertEqual(own, {"number": "40-HD/BTCTW", "type": "Hướng dẫn", "issued": "2025-10-03"})


class Html(unittest.TestCase):
    def test_structure(self):
        blocks = [("p", "QUY ĐỊNH"), ("p", "Chương I"), ("p", "Điều 1. Phạm vi"), ("p", "1. Quy định này …"),
                  ("p", "a) điểm"), ("p", "Điều 2. Đối tượng"), ("table", [["A", "B"]])]
        html, rep = b.build_html("T", "dangcongsan-tulieu-1", blocks, [("pl.docx", [("p", "PHỤ LỤC")])])
        self.assertTrue(rep["structured"])
        self.assertIn('class="prov-article" id="dangcongsan-tulieu-1-dieu:1"', html)
        self.assertIn('id="dangcongsan-tulieu-1-dieu:1/khoan:1/diem:a"', html)
        self.assertIn('<div class="doc-region-appendix">', html)
        plain, rep = b.build_html("T", "p", [("p", "1. Thống nhất …"), ("p", "Điều 3. trích")], [])
        self.assertFalse(rep["structured"])
        self.assertNotIn("prov-", plain)


class Fetch(unittest.TestCase):
    def test_quote(self):
        self.assertEqual(f.quote_url("https://h/upload/QĐ 1.pdf"), "https://h/upload/Q%C4%90%201.pdf")
        self.assertEqual(f.quote_url("https://h/a%20b.pdf"), "https://h/a%20b.pdf")

    def test_sniff(self):
        self.assertEqual(c.sniff(b"%PDF-1.4"), "pdf")
        self.assertEqual(c.sniff(b"<!DOCTYPE html><html>"), "html")
        self.assertEqual(c.sniff(b""), "empty")


if __name__ == "__main__":
    unittest.main()
