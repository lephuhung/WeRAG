"""Documents signed by a deputy (ký thay), with and without the KT. line
and the head in Nơi nhận — parity fixtures and samples for the signing
authority rules of the evaluation skills (internal/docformat/skills)."""

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
