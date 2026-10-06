"""Tests for docformat: synthetic .docx built with zipfile + raw OOXML."""

import io
import os
import sys
import unittest
import zipfile

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from docformat import check_document, inspect_document  # noqa: E402

# keep the suite deterministic: never hit a real LLM from the environment
for _k in ("DOCFORMAT_LLM_BASE_URL", "DOCFORMAT_LLM_MODEL", "WEKNORA_API_KEY"):
    os.environ.pop(_k, None)

W = 'xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"'

STYLES = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles %s>
  <w:docDefaults>
    <w:rPrDefault><w:rPr>
      <w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman"/>
      <w:sz w:val="28"/>
    </w:rPr></w:rPrDefault>
    <w:pPrDefault><w:pPr/></w:pPrDefault>
  </w:docDefaults>
  <w:style w:type="paragraph" w:default="1" w:styleId="Normal">
    <w:name w:val="Normal"/>
  </w:style>
</w:styles>
""" % W


def p(text, align=None, bold=False, italic=False, size=None, font=None,
      style=None):
    ppr = ""
    if align or style:
        ppr = "<w:pPr>"
        if style:
            ppr += '<w:pStyle w:val="%s"/>' % style
        if align:
            ppr += '<w:jc w:val="%s"/>' % align
        ppr += "</w:pPr>"
    rpr = "<w:rPr>"
    if font:
        rpr += '<w:rFonts w:ascii="%s" w:hAnsi="%s"/>' % (font, font)
    if size:
        rpr += '<w:sz w:val="%d"/>' % int(size * 2)
    if bold:
        rpr += "<w:b/>"
    if italic:
        rpr += "<w:i/>"
    rpr += "</w:rPr>"
    esc = (text.replace("&", "&amp;").replace("<", "&lt;")
           .replace(">", "&gt;"))
    return "<w:p>%s<w:r>%s<w:t xml:space=\"preserve\">%s</w:t></w:r></w:p>" % (
        ppr, rpr, esc)


def tc(*paras):
    return "<w:tc><w:tcPr/>" + "".join(paras) + "</w:tc>"


def row(*cells):
    return "<w:tr>" + "".join(cells) + "</w:tr>"


def doc(body_xml, sect=None):
    sect = sect or (
        '<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>'
        '<w:pgMar w:top="1360" w:right="1080" w:bottom="1360" w:left="1920" '
        'w:header="720" w:footer="720" w:gutter="0"/></w:sectPr>'
    )
    return (
        '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
        '<w:document %s><w:body>%s%s</w:body></w:document>' % (W, body_xml, sect)
    )


def build_docx(document_xml, styles_xml=STYLES):
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as zf:
        zf.writestr("[Content_Types].xml",
                    '<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>')
        zf.writestr("word/document.xml", document_xml)
        zf.writestr("word/styles.xml", styles_xml)
    return buf.getvalue()


def good_cong_van(font="Times New Roman", size=14.0, margin_left="1920"):
    tbl = (
        "<w:tbl>"
        '<w:tblPr><w:tblW w:w="9070" w:type="dxa"/></w:tblPr>'
        "<w:tblGrid><w:gridCol w:w='4535'/><w:gridCol w:w='4535'/></w:tblGrid>"
        + row(
            tc(p("BAN QUẢN LÝ KHU X", align="center", size=12),
               p("PHÒNG QUẢN LÝ Y", align="center", bold=True, size=13)),
            tc(p("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", align="center",
                 bold=True, size=13),
               p("Độc lập - Tự do - Hạnh phúc", align="center", bold=True,
                 size=13)),
        )
        + row(
            tc(p("Số: 123/CV-PQLY", align="center", size=13)),
            tc(p("Hà Nội, ngày 05 tháng 10 năm 2026", align="center",
                 italic=True, size=14)),
        )
        + "</w:tbl>"
    )
    body = (
        tbl
        + p("CÔNG VĂN", align="center", bold=True, size=14, font=font)
        + p("Về việc triển khai công tác quản lý", align="center", bold=True,
            size=14, font=font)
        + p("Phòng Quản lý Y trân trọng báo cáo tình hình thực hiện "
            "trong tháng 10/2026 như sau.", align="both", size=size, font=font)
        + p("Trên đây là nội dung công văn, trân trọng cảm ơn.",
            align="both", size=size, font=font)
        + p("PHÒNG QUẢN LÝ Y", align="right", bold=True, size=14, font=font)
        + p("Nguyễn Văn A", align="right", bold=True, size=14, font=font)
        + p("Nơi nhận:", align="left", size=12, font=font)
        + p("- Ban Quản lý Khu X;", align="left", size=12, font=font)
        + p("- Lưu Phòng.", align="left", size=12, font=font)
    )
    sect = (
        '<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>'
        '<w:pgMar w:top="1360" w:right="1080" w:bottom="1360" '
        'w:left="%s" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr>'
        % margin_left
    )
    return build_docx(doc(body, sect))


class TestLayout(unittest.TestCase):
    def test_paragraphs_and_zones(self):
        d = inspect_document(good_cong_van())
        self.assertEqual(d["errors"], [])
        self.assertGreater(len(d["paragraphs"]), 8)
        def zone_of(sub):
            for p in d["paragraphs"]:
                if p["text"].startswith(sub):
                    return p["zone"]
            return None
        self.assertEqual(zone_of("BAN QUẢN LÝ KHU X"), "left")
        self.assertEqual(zone_of("CỘNG HÒA XÃ HỘI"), "right")
        self.assertEqual(zone_of("Nơi nhận:"), "full")

    def test_section_margins(self):
        d = inspect_document(good_cong_van())
        sec = d["sections"][0]
        self.assertAlmostEqual(sec["page_width_mm"], 210.0, places=0)
        self.assertAlmostEqual(sec["margin_left_mm"], 33.87, places=1)


def tab_paragraph(left, right):
    return (
        '<w:p><w:pPr><w:tabs><w:tab w:val="center" w:pos="5103"/></w:tabs>'
        '<w:jc w:val="left"/></w:pPr>'
        '<w:r><w:t>%s</w:t></w:r><w:r><w:tab/></w:r><w:r><w:t>%s</w:t></w:r></w:p>'
        % (left, right)
    )


def flat_cong_van():
    """Header rendered with tab stops instead of a two-column table."""
    body = (
        tab_paragraph("BAN QUẢN LÝ KHU X",
                      "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM")
        + tab_paragraph("PHÒNG QUẢN LÝ Y", "Độc lập - Tự do - Hạnh phúc")
        + tab_paragraph("Số: 45/CV-PQLY",
                        "Hà Nội, ngày 05 tháng 10 năm 2026")
        + p("CÔNG VĂN", align="center", bold=True)
        + p("Về việc công tác quản lý", align="center", bold=True)
        + p("Nội dung công văn gửi các đơn vị.", align="both")
        + p("PHÒNG QUẢN LÝ Y", align="right", bold=True)
        + p("Nguyễn Văn A", align="right", bold=True)
        + p("Nơi nhận:", align="left")
    )
    return build_docx(doc(body))


class TestSegment(unittest.TestCase):
    def test_components(self):
        d = inspect_document(good_cong_van())
        comps = d["components"]
        for key in ("quoc_hieu", "tieu_ngu", "co_quan_ban_hanh",
                    "so_ky_hieu", "dia_danh_ngay_thang", "trich_yeu",
                    "noi_dung", "signature", "noi_nhan"):
            self.assertTrue(comps[key]["found"], "missing %s" % key)
        self.assertEqual(d["detected_type"], "cong_van")
        self.assertIn("123/CV-PQLY", comps["so_ky_hieu"]["text"])
        self.assertIn("Nguyễn Văn A", comps["nguoi_ky"]["text"])

    def test_tab_split_layout(self):
        rep = check_document(flat_cong_van(), document_type="auto")
        comps = rep["components"]
        self.assertTrue(comps["quoc_hieu"]["found"])
        self.assertTrue(comps["so_ky_hieu"]["found"])
        self.assertTrue(comps["dia_danh_ngay_thang"]["found"])
        self.assertEqual(rep["document_type"]["detected"], "cong_van")


class TestCheck(unittest.TestCase):
    def test_good_doc_passes(self):
        rep = check_document(good_cong_van(), document_type="auto",
                             source_name="cv.docx")
        self.assertTrue(rep["ok"])
        self.assertEqual(rep["document_type"]["detected"], "cong_van")
        self.assertEqual(rep["summary"]["fail"], 0,
                         msg=str([c for c in rep["checks"]
                                  if c["status"] == "fail"]))

    def test_wrong_font_flagged(self):
        rep = check_document(good_cong_van(font="Arial"),
                             document_type="cong_van")
        font_check = [c for c in rep["checks"] if c["id"] == "noi_dung.font"][0]
        self.assertEqual(font_check["status"], "fail")
        self.assertTrue(font_check["evidence"])

    def test_wrong_margin_flagged(self):
        rep = check_document(good_cong_van(margin_left="2400"),
                             document_type="cong_van")
        mc = [c for c in rep["checks"] if c["id"] == "page.margin.left"][0]
        self.assertEqual(mc["status"], "fail")

    def test_missing_components_flagged(self):
        # strip nơi nhận: rebuild without it
        body = p("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", align="center",
                 bold=True) + p("Độc lập - Tự do - Hạnh phúc", align="center",
                                bold=True)
        rep = check_document(build_docx(doc(body)), document_type="cong_van")
        missing = [c["component"] for c in rep["checks"]
                   if c["id"].endswith(".present") and c["status"] != "pass"]
        self.assertIn("noi_nhan", missing)
        self.assertIn("trich_yeu", missing)

    def test_not_docx(self):
        rep = check_document(b"not a docx", document_type="auto")
        self.assertFalse(rep["ok"])


if __name__ == "__main__":
    unittest.main()
