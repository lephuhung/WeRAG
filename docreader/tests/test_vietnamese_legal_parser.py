import base64
import unittest
from unittest import mock

from docreader.models.document import Document
from docreader.parser.vietnamese_legal_parser import (
    VietnameseLegalPDFParser,
    _PAGE_IMG_BLOCK_RE,
    _has_broken_vn_text_layer,
    _strip_ocr_markup,
)


def _parser():
    return VietnameseLegalPDFParser(file_name="nd.pdf", file_type="pdf")


class TestBrokenVNTextLayer(unittest.TestCase):
    def test_healthy_vietnamese_layer_is_not_broken(self):
        # Tone characters present -> trustworthy text layer.
        text = "Điều 1. Bộ luật dân sự quy định độc lập tự do hạnh phúc. " * 20
        self.assertFalse(_has_broken_vn_text_layer(text))

    def test_broken_layer_missing_tones_detected(self):
        # Vietnamese base letters (đ, â, ư…) survive but every pre-composed
        # tone character is gone -> corrupt font mapping. Keep the sample
        # free of U+1EA0–1EF9 — "Điều" (ề = U+1EC1) would inject a real
        # tone char; ú/á/ỹ-style Latin-1 accents are not counted.
        text = "B lut dân s quy đnh đc lâp t do hnh phúc. " * 20
        self.assertTrue(_has_broken_vn_text_layer(text))

    def test_short_text_not_flagged(self):
        self.assertFalse(_has_broken_vn_text_layer("Đc lâp"))

    def test_ascii_only_not_flagged(self):
        # No Vietnamese base letters -> cannot tell; leave classification
        # to the geometry-based scanned/native check.
        text = "Article 1. This law provides for independence. " * 20
        self.assertFalse(_has_broken_vn_text_layer(text))


class TestStripOcrMarkup(unittest.TestCase):
    def test_strips_detection_blocks_and_special_tokens(self):
        raw = (
            "<|det|>text [1,2,3,4]<|/det|>Điều 1. Nội dung\n"
            "<|im_end|>\n\n\n<|ref|>x<|/ref|>"
        )
        out = _strip_ocr_markup(raw)
        self.assertIn("Điều 1. Nội dung", out)
        self.assertNotIn("<|", out)
        self.assertNotIn("<|im_end|>", out)

    def test_collapses_blank_runs(self):
        self.assertEqual(_strip_ocr_markup("a\n\n\n\nb"), "a\n\nb")


class TestPageMarkers(unittest.TestCase):
    def test_marker_format(self):
        self.assertEqual(_parser()._page_marker(0), "<!-- page 1 -->\n\n")
        self.assertEqual(_parser()._page_marker(4), "<!-- page 5 -->\n\n")


class TestReclassifyPage(unittest.TestCase):
    def test_text_page_with_broken_vn_layer_becomes_scanned(self):
        p = _parser()
        broken = "B lut dân s quy đnh đc lâp t do hnh phúc. " * 20
        self.assertEqual(p._reclassify_page("text", broken), "scanned")

    def test_healthy_text_page_unchanged(self):
        p = _parser()
        good = "Điều 1. Bộ luật dân sự quy định độc lập tự do. " * 20
        self.assertEqual(p._reclassify_page("text", good), "text")

    def test_scanned_page_stays_scanned(self):
        p = _parser()
        self.assertEqual(p._reclassify_page("scanned", "anything"), "scanned")


class TestPageImageBlockRe(unittest.TestCase):
    def test_matches_scanned_page_ref(self):
        block = "<!-- page 3 -->\n\n![doc_page_3.jpg](images/doc_page_3.jpg)"
        m = _PAGE_IMG_BLOCK_RE.search(block)
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), "3")
        self.assertEqual(m.group(2), "images/doc_page_3.jpg")

    def test_ignores_embedded_image_refs(self):
        self.assertIsNone(
            _PAGE_IMG_BLOCK_RE.search("![fig.png](images/fig.png)")
        )


def _make_doc():
    jpeg = base64.b64encode(b"fakejpeg").decode()
    return Document(
        content=(
            "<!-- page 1 -->\n\n"
            "Điều 1. Phạm vi điều chỉnh.\n\n"
            "<!-- page 2 -->\n\n"
            "![nd_page_2.jpg](images/nd_page_2.jpg)\n\n"
            "<!-- page 3 -->\n\n"
            "![nd_page_3.jpg](images/nd_page_3.jpg)"
        ),
        images={
            "images/nd_page_2.jpg": jpeg,
            "images/nd_page_3.jpg": jpeg,
        },
        metadata={"page_count": 3, "scanned_page_count": 2},
    )


class TestOcrScannedPages(unittest.TestCase):
    def test_scanned_pages_replaced_by_ocr_text(self):
        doc = _make_doc()
        with mock.patch(
            "docreader.parser.vietnamese_legal_parser._ocr_page_image",
            side_effect=lambda jpeg, page_no, **kw: f"OCR trang {page_no}",
        ):
            out = _parser()._ocr_scanned_pages(doc)

        self.assertIn("<!-- page 1 -->\n\nĐiều 1.", out.content)
        self.assertIn("<!-- page 2 -->\n\nOCR trang 2", out.content)
        self.assertIn("<!-- page 3 -->\n\nOCR trang 3", out.content)
        self.assertNotIn("nd_page_2.jpg](images", out.content)
        self.assertEqual(out.images, {})
        self.assertEqual(out.metadata["vn_ocr_pages"], 2)
        self.assertEqual(out.metadata["image_source_type"], "vn_ocr")

    def test_failed_ocr_keeps_image_ref_for_go_fallback(self):
        doc = _make_doc()
        with mock.patch(
            "docreader.parser.vietnamese_legal_parser._ocr_page_image",
            return_value="",
        ):
            out = _parser()._ocr_scanned_pages(doc)

        self.assertIn("images/nd_page_2.jpg", out.content)
        self.assertEqual(len(out.images), 2)
        self.assertEqual(out.metadata["vn_ocr_pages"], 0)
        self.assertNotEqual(out.metadata.get("image_source_type"), "vn_ocr")

    def test_no_scanned_refs_is_noop(self):
        doc = Document(content="plain text", images={}, metadata={})
        with mock.patch(
            "docreader.parser.vietnamese_legal_parser._ocr_page_image"
        ) as ocr:
            out = _parser()._ocr_scanned_pages(doc)
        ocr.assert_not_called()
        self.assertEqual(out.content, "plain text")


class TestRequestOverrides(unittest.TestCase):
    def test_openai_ocr_overrides_win_over_env(self):
        p = VietnameseLegalPDFParser(
            file_name="nd.pdf",
            file_type="pdf",
            openai_ocr_url="http://tenant-ocr:8001/v1/",
            openai_ocr_model="tenant-model",
            openai_ocr_api_key="k",
            openai_ocr_prompt="P",
            openai_ocr_vllm_xargs="0",
        )
        self.assertEqual(p._ocr_url, "http://tenant-ocr:8001/v1")
        self.assertEqual(p._ocr_model, "tenant-model")
        self.assertEqual(p._ocr_api_key, "k")
        self.assertEqual(p._ocr_prompt, "P")
        self.assertFalse(p._ocr_vllm_xargs)
        self.assertTrue(p._ocr_enabled())

    def test_env_fallback_when_no_overrides(self):
        from docreader.parser import vietnamese_legal_parser as m

        p = _parser()
        self.assertEqual(p._ocr_url, m.VN_OCR_URL)
        self.assertEqual(p._ocr_model, m.VN_OCR_MODEL)

    def test_unknown_overrides_do_not_break_ctor(self):
        # BaseParser ignores **kwargs; engine-specific overrides for other
        # engines must not raise.
        VietnameseLegalPDFParser(
            file_name="nd.pdf", file_type="pdf", mineru_backend="pipeline"
        )


if __name__ == "__main__":
    unittest.main()
