"""Ký thay (KT.) rules: a deputy signs under "KT. <head's title>", and a
document signed KT. lists the head in Nơi nhận "(để báo cáo)"."""

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from test_check import build_docx, doc, p, row, tc  # noqa: E402
from test_segment_real import tbl  # noqa: E402

from docformat import check_document  # noqa: E402


def signed_doc(sig_lines, noi_nhan):
    header = tbl(
        row(tc(p("CÔNG AN TỈNH HÀ TĨNH", align="center"),
               p("PHÒNG PA05", align="center", bold=True)),
            tc(p("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", align="center", bold=True),
               p("Độc lập - Tự do - Hạnh phúc", align="center", bold=True))),
        row(tc(p("Số: 12/PA05-Đ4", align="center"),
               p("V/v cung cấp số liệu", align="center", size=12)),
            tc(p("Hà Tĩnh, ngày 07 tháng 7 năm 2026", align="center",
                 italic=True))),
    )
    sig = tbl(row(
        tc(*([p("Nơi nhận:", bold=True, italic=True, size=12)]
             + [p(x, size=11) for x in noi_nhan])),
        tc(*([p(x, align="center", bold=True) for x in sig_lines]
             + [p("Bùi Anh Việt", align="center", bold=True)])),
    ))
    body = (header
            + p("Kính gửi: Phòng PV01", align="center")
            + p("Phòng PA05 trao đổi nội dung như sau.", align="both")
            + sig)
    return build_docx(doc(body))


def checks(content):
    rep = check_document(content, segmenter="heuristic")
    return {c["id"]: c for c in rep["checks"]}


class TestKyThay(unittest.TestCase):
    def test_correct_ky_thay(self):
        c = checks(signed_doc(["KT. TRƯỞNG PHÒNG", "PHÓ TRƯỞNG PHÒNG"],
                              ["- Như trên;", "- Đ/c Trưởng phòng (để b/c);",
                               "- Lưu: VT."]))
        self.assertEqual(c["signature.ky_thay"]["status"], "pass")
        self.assertEqual(c["noi_nhan.bao_cao_cap_truong"]["status"], "pass")

    def test_missing_kt_line(self):
        c = checks(signed_doc(["TRƯỞNG PHÒNG", "PHÓ TRƯỞNG PHÒNG"],
                              ["- Như trên;", "- Lưu: PA05 (Đ4)."]))
        kt = c["signature.ky_thay"]
        self.assertEqual(kt["status"], "fail")
        self.assertIn("KT. TRƯỞNG PHÒNG", kt["evidence"][0]["actual"])
        nn = c["noi_nhan.bao_cao_cap_truong"]
        self.assertEqual(nn["status"], "fail")
        self.assertIn("TRƯỞNG PHÒNG (để báo cáo)", nn["evidence"][0]["actual"])

    def test_deputy_alone(self):
        c = checks(signed_doc(["PHÓ GIÁM ĐỐC"], ["- Như trên;", "- Lưu: VT."]))
        self.assertEqual(c["signature.ky_thay"]["status"], "fail")

    def test_mismatched_title(self):
        c = checks(signed_doc(["KT. GIÁM ĐỐC", "PHÓ TRƯỞNG PHÒNG"],
                              ["- Giám đốc (để b/c);"]))
        self.assertEqual(c["signature.ky_thay"]["status"], "fail")
        self.assertIn("không khớp", c["signature.ky_thay"]["evidence"][0]["actual"])

    def test_tm_kt_chain_and_initials(self):
        c = checks(signed_doc(["TM. ỦY BAN NHÂN DÂN", "KT. CHỦ TỊCH", "PHÓ CHỦ TỊCH"],
                              ["- CT UBND tỉnh (để báo cáo);", "- Lưu: VT."]))
        self.assertEqual(c["signature.ky_thay"]["status"], "pass")
        self.assertEqual(c["noi_nhan.bao_cao_cap_truong"]["status"], "pass")

    def test_head_listed_without_bao_cao_is_warn(self):
        c = checks(signed_doc(["KT. TRƯỞNG PHÒNG", "PHÓ TRƯỞNG PHÒNG"],
                              ["- Trưởng phòng;", "- Lưu: VT."]))
        self.assertEqual(c["noi_nhan.bao_cao_cap_truong"]["status"], "warn")

    def test_deputy_in_noi_nhan_is_not_the_head(self):
        c = checks(signed_doc(["KT. TRƯỞNG PHÒNG", "PHÓ TRƯỞNG PHÒNG"],
                              ["- Phó Trưởng phòng (để b/c);", "- Lưu: VT."]))
        self.assertEqual(c["noi_nhan.bao_cao_cap_truong"]["status"], "fail")

    def test_head_signing_is_not_applicable(self):
        c = checks(signed_doc(["TRƯỞNG PHÒNG"], ["- Như trên;", "- Lưu: VT."]))
        for cid in ("signature.ky_thay", "noi_nhan.bao_cao_cap_truong"):
            self.assertEqual(c[cid]["status"], "skip")
            self.assertTrue(c[cid]["note"].startswith("không áp dụng"))


if __name__ == "__main__":
    unittest.main()
