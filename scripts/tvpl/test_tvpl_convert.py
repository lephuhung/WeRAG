"""Tests of tvpl_convert (python3 -m unittest scripts/tvpl/test_tvpl_convert.py).

The fixtures are short hand-written pages in the shape of real TVPL markup
(checked 2026-11-03 on Luật Doanh nghiệp 59/2020/QH14 and Nghị định
123/2020/NĐ-CP); no TVPL page is stored in the repository.
"""

import json
import os
import re
import sys
import unittest
import html as htmlmod

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import tvpl_convert as c  # noqa: E402

TIP_LUAT = ("<div id=\"bmTitle\" class = \"clsBookmark1 bmTitle\" tt='4789558-1'  crid = '659899'   prid = '' >"
            "Khoản này được sửa đổi bởi Điểm a Khoản 1 Điều 1 Luật Doanh nghiệp sửa đổi 2025 có hiệu lực từ ngày "
            "01/07/2025 </div><div id=\"bmContent\" class='ct-4789558-1'>Điều 1. …<br/>“5. Cổ tức là …”;<br/>"
            "<b><a target=\"_blank\" href='https://thuvienphapluat.vn/van-ban/Doanh-nghiep/"
            "Luat-Doanh-nghiep-sua-doi-2025-so-76-2025-QH15-659899.aspx?anchor=diem_a_1_1'>Xem nội dung VB</a></b></div>"
            "<div id=\"bmContentOld\" class='ctOld-4789558-1'>Điều 4. …</div>")

# two amending documents on one provision; the link names only the Điều
TIP_TWO = ("<div id=\"bmTitle\" class=\"clsBookmark1 bmTitle\" tt='77-1' crid='577816'>Cụm từ này bị thay thế bởi "
           "Khoản 3 Điều 2 Nghị định 70/2025/NĐ-CP có hiệu lực từ ngày 01/06/2025 (VB hết hiệu lực: 01/07/2026)</div>"
           "<div id=\"bmContent\" class='ct-77-1'>… <a href='https://thuvienphapluat.vn/van-ban/Thue-Phi-Le-Phi/"
           "Nghi-dinh-70-2025-ND-CP-sua-doi-Nghi-dinh-123-2020-ND-CP-hoa-don-chung-tu-577816.aspx?anchor=dieu_2'>"
           "Xem nội dung VB</a></div>"
           "<div id=\"bmTitle\" class=\"clsBookmark1 bmTitle\" tt='77-2' crid='1'>Khoản này được hướng dẫn bởi "
           "Điều 5 Thông tư 32/2025/TT-BTC có hiệu lực từ ngày 01/06/2025</div>"
           "<div id=\"bmContent\" class='ct-77-2'><a href='https://thuvienphapluat.vn/van-ban/x/"
           "Thong-tu-32-2025-TT-BTC-huong-dan-659105.aspx?anchor=dieu_5'>Xem</a></div>")

TIP_DD = ("<div id=\"bmTitle\" class=\"clsBookmark1 bmTitle\" tt='9-1' crid='577816'>Điều này được bổ sung bởi "
          "Điểm đ Khoản 7 Điều 1 Nghị định 70/2025/NĐ-CP có hiệu lực từ ngày 01/06/2025</div>"
          "<div id=\"bmContent\" class='ct-9-1'><a href='https://thuvienphapluat.vn/van-ban/a/"
          "Nghi-dinh-70-2025-ND-CP-sua-doi-577816.aspx?anchor=diem_dd_7_1'>Xem</a></div>")

TIP_NO_OP = ("<div id=\"bmTitle\" class=\"clsBookmark1 bmTitle\" tt='5-1' crid='700681'>Không quy định thủ tục "
             "tiêu huỷ hoá đơn</div>")


def bm(n):
    return (f' class="clsBookmark" onmouseover="LS_Tootip_Type_Bookmark(\'.lqhlTootip-{n}\')" '
            f'atmm=".lqhlTootip-{n}" data-type="amend-guidance"')


PAGE = f"""<html><head><title>Nghị định 1/2020/NĐ-CP</title></head><body>
<table><tr><td>Số hiệu:</td><td>1/2020/NĐ-CP</td><td>Loại văn bản:</td><td>Nghị định</td></tr>
<tr><td>Nơi ban hành:</td><td>Chính phủ</td><td>Người ký:</td><td>X</td></tr>
<tr><td>Ngày ban hành:</td><td>19/10/2020</td><td>Ngày hiệu lực:</td><td>Đã biết</td></tr></table>
<script>var cLawID ='445980';</script>
<div class="content1"><div>
<p>CHÍNH PHỦ</p>
<p>1. Căn cứ không phải khoản (chưa có Điều).</p>
<p><a name="chuong_1"><b>Chương I</b></a></p>
<p><a name="chuong_1_name">QUY ĐỊNH CHUNG</a></p>
<p><a name="dieu_1"><b>Điều 1. Phạm vi</b></a></p>
<p><a name="khoan_1_1"{bm(4789558)}>1. Khoản một.</a><huongdan class="huong-dan-dieu-khoan" id="span-note_khoan_1_1">Bổ&nbsp;sung</huongdan></p>
<p>a) điểm a;</p>
<p><a name="diem_dd_1_1">đ) điểm đ;</a></p>
<p>Đoạn không đánh số của khoản 1<a{bm(77)}>có cụm từ</a>.</p>
<p>2. Khoản hai 1.000 đồng.</p>
<p>1.000.000 đồng là mức phạt (không phải khoản).</p>
<p><a name="dieu_2"><b>Điều 2. Sửa đổi</b></a></p>
<p>1. Sửa đổi khoản 3 như sau:</p>
<p>“3. Khoản được trích.</p>
<p>a) điểm trích;”.</p>
<p{bm(9)}>2. Khoản hai của Điều 2.</p>
<p><a name="muc_2"{bm(5)}>Mục 2. ĐIỆN TỬ</a></p>
<p><a name="dieu_3"{bm(404)}>Điều 3. Không có ghi chú</a></p>
<p>PHỤ LỤC I</p>
<p>1. Dòng của phụ lục</p>
</div></div>
<div id="note_khoan_1_1">nội dung chèn |~| nguồn</div>
</body></html>"""

TIPS = {"4789558": TIP_LUAT, "77": TIP_TWO, "9": TIP_DD, "5": TIP_NO_OP, "1": ""}


class TipTests(unittest.TestCase):
    def test_luat_note(self):
        marks, probs = c.parse_tip(TIP_LUAT)
        self.assertEqual(probs, [])
        self.assertEqual(marks, [{"op": "sua_doi", "doc_number": "76/2025/QH15", "path": "dieu:1/khoan:1/diem:a",
                                  "eff_from": "2025-07-01", "tvpl_doc_id": "659899",
                                  "note": "Khoản này được sửa đổi bởi Điểm a Khoản 1 Điều 1 Luật Doanh nghiệp "
                                          "sửa đổi 2025 có hiệu lực từ ngày 01/07/2025"}])

    def test_two_documents_and_more_specific_path(self):
        marks, probs = c.parse_tip(TIP_TWO)
        self.assertEqual(probs, [])
        self.assertEqual([m["op"] for m in marks], ["thay_the", "huong_dan"])
        self.assertEqual(marks[0]["doc_number"], "70/2025/NĐ-CP")
        self.assertEqual(marks[0]["path"], "dieu:2/khoan:3")  # note is more precise than anchor=dieu_2
        self.assertNotIn("eff_to", marks[0])  # "VB hết hiệu lực" of B is not an end date of the change
        self.assertEqual(marks[1]["path"], "dieu:5")

    def test_diem_dd(self):
        marks, probs = c.parse_tip(TIP_DD)
        self.assertEqual(probs, [])
        self.assertEqual(marks[0]["path"], "dieu:1/khoan:7/diem:đ")
        self.assertEqual(marks[0]["op"], "bo_sung")

    def test_no_operation(self):
        # no operation and no B: reported
        marks, probs = c.parse_tip(TIP_NO_OP)
        self.assertEqual(marks, [])
        self.assertTrue(probs[0].startswith("no operation and no amending document"), probs)

    def test_free_form_note_is_informational(self):
        raw = ("<div class='clsBookmark1 bmTitle' tt='3-1' crid='660608'>Thẩm quyền giao đất của Ủy ban nhân dân "
               "cấp tỉnh do Chủ tịch Ủy ban nhân dân cấp xã thực hiện theo quy định tại Điểm a Khoản 3 Điều 10 "
               "Nghị định 151/2025/NĐ-CP có hiệu lực từ ngày 01/07/2025</div>")
        marks, probs = c.parse_tip(raw)
        self.assertEqual(probs, [])
        self.assertEqual({k: marks[0][k] for k in ("op", "op_inferred", "doc_number", "path")},
                         {"op": "huong_dan", "op_inferred": "1", "doc_number": "151/2025/NĐ-CP",
                          "path": "dieu:10/khoan:3/diem:a"})

    def test_change_to_the_guiding_document_is_not_a_change_to_a(self):
        raw = ("<div class='clsBookmark1 bmTitle' tt='6-1' crid='679921'>Nội dung hướng dẫn Điều này tại Nghị định "
               "103/2024/NĐ-CP nay được sửa đổi bởi Khoản 4 Điều 1 Nghị định 291/2025/NĐ-CP có hiệu lực từ ngày "
               "01/11/2025</div>")
        marks, _ = c.parse_tip(raw)
        self.assertEqual((marks[0]["op"], marks[0].get("op_inferred"), marks[0]["doc_number"]),
                         ("huong_dan", "1", "291/2025/NĐ-CP"))

    def test_het_hieu_luc_boi(self):
        raw = ("<div class='clsBookmark1 bmTitle' tt='4-1' crid='586814'>Điều này hết hiệu lực bởi Khoản 6 Điều 57 "
               "Luật Quy hoạch 2025 có hiệu lực từ ngày 01/03/2026</div><div class='ct-4-1'><a href='https://"
               "thuvienphapluat.vn/van-ban/a/Luat-Quy-hoach-2025-so-112-2025-QH15-586814.aspx?anchor=dieu_57'>x</a></div>")
        marks, probs = c.parse_tip(raw)
        self.assertEqual(probs, [])
        self.assertEqual((marks[0]["op"], marks[0]["doc_number"], marks[0]["path"]),
                         ("bai_bo", "112/2025/QH15", "dieu:57/khoan:6"))

    def test_ops(self):
        for phrase, op in [("được sửa đổi, bổ sung bởi", "sua_doi_bo_sung"), ("bị bãi bỏ bởi", "bai_bo"),
                           ("được bổ sung bởi", "bo_sung"), ("bị đính chính bởi", "dinh_chinh"),
                           ("bị tạm ngưng hiệu lực bởi", "tam_ngung"), ("bị thay thế bởi", "thay_the")]:
            raw = (f"<div class='clsBookmark1 bmTitle' tt='1-1'>Khoản này {phrase} Điều 2 "
                   f"Nghị định 5/2021/NĐ-CP có hiệu lực từ ngày 1/2/2021</div>")
            marks, _ = c.parse_tip(raw)
            self.assertEqual(marks[0]["op"], op, phrase)
            self.assertEqual(marks[0]["eff_from"], "2021-02-01")


class HelperTests(unittest.TestCase):
    def test_number_from_slug(self):
        base = "https://thuvienphapluat.vn/van-ban/x/"
        self.assertEqual(c.number_from_slug(base + "Luat-Doanh-nghiep-sua-doi-2025-so-76-2025-QH15-659899.aspx"),
                         "76/2025/QH15")
        self.assertEqual(c.number_from_slug(
            base + "Nghi-dinh-70-2025-ND-CP-sua-doi-Nghi-dinh-123-2020-ND-CP-hoa-don-577816.aspx"), "70/2025/NĐ-CP")
        self.assertEqual(c.number_from_slug(base + "Quyet-dinh-152-QD-UBND-2024-phe-duyet-603456.aspx"),
                         "152/QĐ-UBND")
        self.assertEqual(c.number_from_slug(base + "Luat-Dat-dai-2024-31-2024-QH15-523642.aspx"), "31/2024/QH15")
        self.assertEqual(c.number_from_slug(base + "Luat-Dat-dai-523642.aspx"), "")

    def test_merge_paths(self):
        self.assertEqual(c.merge_paths("dieu:2", "dieu:2/khoan:3"), ("dieu:2/khoan:3", ""))
        self.assertEqual(c.merge_paths("dieu:1/khoan:1/diem:a", "dieu:1"), ("dieu:1/khoan:1/diem:a", ""))
        p, why = c.merge_paths("dieu:8/khoan:2", "dieu:9")
        self.assertEqual(p, "dieu:8/khoan:2")
        self.assertIn("kept the link's", why)


class ConvertTests(unittest.TestCase):
    def setUp(self):
        self.doc, self.meta, self.rep = c.convert(PAGE, TIPS, "https://thuvienphapluat.vn/van-ban/x/a-445980.aspx")
        self.paras = re.findall(r'<p class="([^"]+)"(?: id="([^"]+)")?(?: data-amend="([^"]*)")?>(.*?)</p>', self.doc)

    def marks_of(self, eid):
        for cls, i, amend, _ in self.paras:
            if i == eid:
                return json.loads(htmlmod.unescape(amend)) if amend else []
        self.fail(f"no element {eid}")

    def test_metadata(self):
        self.assertEqual(self.meta, {"source": "tvpl", "source_url": "https://thuvienphapluat.vn/van-ban/x/a-445980.aspx",
                                     "source_doc_id": "445980", "format": "werag-legal-html/1",
                                     "doc_number": "1/2020/NĐ-CP", "doc_type": "Nghị định", "issuer": "Chính phủ",
                                     "issue_date": "2020-10-19"})

    def test_structure(self):
        ids = [i for _, i, _, _ in self.paras if i]
        self.assertEqual(ids, ["tvpl-445980-dieu:1", "tvpl-445980-dieu:1/khoan:1", "tvpl-445980-dieu:1/khoan:1/diem:a",
                               "tvpl-445980-dieu:1/khoan:1/diem:đ", "tvpl-445980-dieu:1/khoan:2",
                               "tvpl-445980-dieu:2", "tvpl-445980-dieu:2/khoan:1", "tvpl-445980-dieu:2/khoan:2",
                               "tvpl-445980-dieu:3"])
        classes = {t: cls for cls, _, _, t in self.paras}
        self.assertEqual(classes["Chương I"], "prov-chapter")
        self.assertEqual(classes["1.000.000 đồng là mức phạt (không phải khoản)."], "prov-content")
        self.assertEqual(classes["“3. Khoản được trích."], "prov-content")  # quoted wording
        self.assertEqual(classes["a) điểm trích;”."], "prov-content")
        self.assertEqual(classes["1. Căn cứ không phải khoản (chưa có Điều)."], "prov-content")
        self.assertIn('<div class="doc-region-appendix">', self.doc)
        self.assertEqual(classes["1. Dòng của phụ lục"], "prov-content")

    def test_tvpl_additions_dropped(self):
        self.assertNotIn("Bổ sung", self.doc)
        self.assertNotIn("nội dung chèn", self.doc)

    def test_marks(self):
        # the unnumbered paragraph after điểm đ: "Khoản này …" goes to khoản 1,
        # "Cụm từ này …" to the nearest provision (điểm đ)
        self.assertEqual([m["doc_number"] for m in self.marks_of("tvpl-445980-dieu:1/khoan:1")],
                         ["76/2025/QH15", "32/2025/TT-BTC"])
        self.assertEqual([m["doc_number"] for m in self.marks_of("tvpl-445980-dieu:1/khoan:1/diem:đ")],
                         ["70/2025/NĐ-CP"])
        self.assertEqual(self.marks_of("tvpl-445980-dieu:2/khoan:2")[0]["path"], "dieu:1/khoan:7/diem:đ")
        self.assertEqual(self.marks_of("tvpl-445980-dieu:3"), [])
        r = self.rep
        self.assertEqual((r["marked"], r["marks"], r["marks_on_parent"], r["bookmarks_without_note"]), (3, 4, 2, 1))
        reasons = [p["reason"] for p in r["problems"]]
        self.assertTrue(any(x.startswith("no operation") for x in reasons), reasons)

    def test_heading_mark_is_reported(self):
        page = PAGE.replace("<p><a name=\"muc_2\"" + bm(5), "<p><a name=\"muc_2\"" + bm(77))
        _, _, rep = c.convert(page, TIPS, "u")
        self.assertEqual(rep["unplaced_marks"], 2)
        self.assertTrue(any("heading" in p["reason"] for p in rep["problems"]))


def page(body, law="445980"):
    return (f"<html><body><script>var cLawID ='{law}';</script><div class=\"content1\"><div>"
            + body + "</div></div></body></html>")


def ids_and_classes(doc):
    return re.findall(r'<p class="([^"]+)"(?: id="([^"]+)")?[^>]*>(.*?)</p>', doc)


class StructureEdgeTests(unittest.TestCase):
    def test_quoted_term_does_not_open_a_quote(self):
        doc, _, _ = c.convert(page("<p>Điều 1. Giải thích</p><p>“Hóa đơn” là chứng từ kế toán.</p>"
                                   "<p>Điều 2. Áp dụng</p><p>1. Khoản một.</p>"), {}, "u")
        ids = [i for _, i, _ in ids_and_classes(doc) if i]
        self.assertEqual(ids, ["tvpl-445980-dieu:1", "tvpl-445980-dieu:2", "tvpl-445980-dieu:2/khoan:1"])

    def test_open_quote_is_closed_by_a_real_article(self):
        doc, _, _ = c.convert(page("<p>Điều 1. Sửa đổi</p><p>1. Sửa khoản 3 như sau:</p><p>“3. Lời văn mới”. Đồng"
                                   " thời bãi bỏ khoản 4.</p><p>“4. Lời văn không đóng</p>"
                                   "<p>Điều 2. Hiệu lực</p><p>1. Khoản một.</p>"), {}, "u")
        ids = [i for _, i, _ in ids_and_classes(doc) if i]
        self.assertEqual(ids, ["tvpl-445980-dieu:1", "tvpl-445980-dieu:1/khoan:1",
                               "tvpl-445980-dieu:2", "tvpl-445980-dieu:2/khoan:1"])

    def test_phu_luc_sentence_does_not_open_the_appendix(self):
        doc, _, _ = c.convert(page("<p>Điều 1. Mẫu</p><p>Phụ lục I kèm theo Nghị định này gồm các mẫu.</p>"
                                   "<p>Điều 2. Hiệu lực</p><p>1. Khoản một.</p><p>PHỤ LỤC I</p><p>1. Dòng</p>"),
                              {}, "u")
        self.assertEqual(doc.count("doc-region-appendix"), 1)
        self.assertLess(doc.index("tvpl-445980-dieu:2/khoan:1"), doc.index("doc-region-appendix"))

    def test_numbers_the_server_cannot_read_stay_content(self):
        doc, _, rep = c.convert(page('<p>Điều 1. A</p><p><a name="khoan_2_1">Khoản không đánh số</a></p>'
                                     "<p>2a. Khoản chèn</p><p>1.1. Tiểu khoản</p><p>Điều 5 Luật này quy định</p>"),
                                {}, "u")
        ids = [i for _, i, _ in ids_and_classes(doc) if i]
        self.assertEqual(ids, ["tvpl-445980-dieu:1"])
        self.assertTrue(any("no number in the text" in p["reason"] for p in rep["problems"]))

    def test_mark_in_a_table_is_reported(self):
        _, _, rep = c.convert(page(f'<p>Điều 1. A</p><table><tr><td{bm(4789558)}>x</td></tr></table>'),
                              TIPS, "u")
        self.assertEqual(rep["unplaced_marks"], 1)
        self.assertEqual(rep["problems"][-1]["reason"], "mark inside a table")

    def test_document_id_from_argument_or_fails(self):
        body = "<html><body><div class=\"content1\"><p>Điều 1. A</p></div></body></html>"
        doc, meta, _ = c.convert(body, {}, "u", source_doc_id="777")
        self.assertEqual(meta["source_doc_id"], "777")
        self.assertIn('id="tvpl-777-dieu:1"', doc)
        with self.assertRaises(ValueError):
            c.convert(body, {}, "u")

    def test_slug_codes_with_lower_case(self):
        b = "https://thuvienphapluat.vn/van-ban/x/"
        self.assertEqual(c.number_from_slug(b + "Quyet-dinh-12-2024-QD-TTg-abc-1.aspx"), "12/2024/QĐ-TTg")
        self.assertEqual(c.number_from_slug(b + "Chi-thi-05-CT-TTg-xyz-2.aspx"), "05/CT-TTg")

    def test_expiry_without_boi(self):
        marks, _ = c.parse_tip("<div class='clsBookmark1 bmTitle' tt='1-1'>Điểm này hết hiệu lực từ ngày "
                               "01/01/2024 theo Nghị định 1/2023/NĐ-CP</div>")
        self.assertEqual((marks[0]["op"], marks[0]["doc_number"], marks[0]["eff_from"]),
                         ("bai_bo", "1/2023/NĐ-CP", "2024-01-01"))

    def test_b_expiry_suffix_is_not_an_expiry_of_a(self):
        marks, _ = c.parse_tip("<div class='clsBookmark1 bmTitle' tt='1-1'>Quy hoạch cấp xã thực hiện theo quy định "
                               "tại Điều 19 Nghị định 151/2025/NĐ-CP có hiệu lực từ ngày 01/07/2025 (VB hết hiệu lực: "
                               "01/03/2027)</div>")
        self.assertEqual((marks[0]["op"], marks[0]["op_inferred"], marks[0]["eff_from"]), ("huong_dan", "1", "2025-07-01"))
        self.assertTrue(marks[0]["note"].endswith("(VB hết hiệu lực: 01/03/2027)"))  # kept verbatim

    def test_number_in_a_quoted_phrase_does_not_downgrade(self):
        marks, _ = c.parse_tip("<div class='clsBookmark1 bmTitle' tt='1-1'>Cụm từ “Nghị định 123/2020/NĐ-CP” tại "
                               "Khoản này bị thay thế bởi Khoản 2 Điều 1 Nghị định 70/2025/NĐ-CP</div>")
        self.assertEqual((marks[0]["op"], marks[0].get("op_inferred")), ("thay_the", None))


class FetchAndPushHelperTests(unittest.TestCase):
    def test_doc_id_of_url(self):
        import tvpl_fetch as tf
        self.assertEqual(tf.doc_id_of_url("https://thuvienphapluat.vn/van-ban/a/Luat-427301.aspx?tab=1"), "427301")
        self.assertEqual(tf.doc_id_of_url("https://thuvienphapluat.vn/"), "")

    def test_extract_tips(self):
        import tvpl_fetch as tf
        data = htmlmod.escape(json.dumps({"bookmarks": 3, "tips": {"1": "<div>x</div>"}, "errors": {"2": "HTTP 500"}}))
        tips, errors = tf.extract_tips(f'<body><pre id="werag-tvpl-tips">{data}</pre></body>')
        self.assertEqual(tips, {"1": "<div>x</div>"})
        self.assertIn("2", errors)
        self.assertIn("*", errors)  # 2 of 3 answered
        self.assertIn("*", tf.extract_tips("<body></body>")[1])

    def test_challenge_stops_before_json(self):
        import subprocess
        import tvpl_fetch as tf
        f = tf.Fetcher.__new__(tf.Fetcher)
        f.bin, f.cache_dir, f.cookie_jar, f.interval, f.timeout, f._last = "x", "/tmp", "/nonexistent", 0, 10, 0
        f._pace = lambda: None
        orig = subprocess.run
        subprocess.run = lambda *a, **k: subprocess.CompletedProcess(a, 1, b"", b'$msg="bot challenge" url=u')
        try:
            with self.assertRaises(tf.TVPLStop):
                f._run("https://thuvienphapluat.vn/", session=False)
        finally:
            subprocess.run = orig

    def test_pick_result(self):
        import tvpl_push as tp
        res = [("https://thuvienphapluat.vn/van-ban/a/Nghi-dinh-70-2025-ND-CP-sua-doi-Nghi-dinh-123-2020-ND-CP-1.aspx", ""),
               ("https://thuvienphapluat.vn/van-ban/a/Nghi-dinh-123-2020-ND-CP-hoa-don-445980.aspx", "")]
        self.assertEqual(tp.pick_result(res, "123/2020/NĐ-CP")[0], res[1][0])
        url, why = tp.pick_result(res, "1/2020/NĐ-CP")
        self.assertEqual(url, "")
        self.assertIn("70/2025/NĐ-CP", why)

    def test_search_results(self):
        import tvpl_push as tp
        h = ('<div class="nq"><p class="nqTitle"><a href="https://thuvienphapluat.vn/van-ban/a/X-1.aspx?tab=1">X</a>'
             '</p></div><p class="nqTitle"><a href="https://thuvienphapluat.vn/van-ban/a/X-1.aspx">X</a></p>')
        self.assertEqual(tp.search_results(h), [("https://thuvienphapluat.vn/van-ban/a/X-1.aspx", "X")])


if __name__ == "__main__":
    unittest.main()
