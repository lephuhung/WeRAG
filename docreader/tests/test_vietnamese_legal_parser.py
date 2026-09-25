import base64
import unittest
from unittest import mock

from docreader.models.document import Document
from docreader.parser.vietnamese_legal_parser import (
    VietnameseLegalPDFParser,
    _PAGE_IMG_BLOCK_RE,
    _collapse_degenerate_tail,
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

    def test_valid_nfd_vietnamese_is_not_broken(self):
        import unicodedata

        # Valid Vietnamese in NFD (base + combining tones) must NFC-compose
        # back to tone characters instead of flagging as a broken layer.
        healthy = "Điều 1. Bộ luật dân sự quy định độc lập tự do hạnh phúc. " * 20
        nfd = unicodedata.normalize("NFD", healthy)
        # Sanity: NFD really decomposes (no pre-composed tones left).
        self.assertNotIn("\u1ec1", nfd)
        self.assertFalse(_has_broken_vn_text_layer(nfd))

    def test_broken_layer_still_detected_after_nfc(self):
        text = "B lut dân s quy đnh đc lâp t do hnh phúc. " * 20
        self.assertTrue(_has_broken_vn_text_layer(text))


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


class TestCollapseDegenerateTail(unittest.TestCase):
    def test_collapses_numbered_repeat_tail(self):
        junk = "".join(
            f"{i}. Use LaTeX to output the output content. " for i in range(1, 16)
        )
        text = "Điều 1. Nội dung thật của trang scan.\n" + junk
        self.assertEqual(
            _collapse_degenerate_tail(text),
            "Điều 1. Nội dung thật của trang scan.",
        )

    def test_collapses_truncated_final_item(self):
        # Model hit max_tokens mid-repeat: the last item is a truncation of
        # the repeated one and would slip past a strict equality check.
        junk = "".join(
            f"{i}. Use LaTeX to output the output content. " for i in range(1, 15)
        ) + "15. Use LaTeX to,"
        text = "Điều 1. Nội dung thật.\n" + junk
        self.assertEqual(_collapse_degenerate_tail(text), "Điều 1. Nội dung thật.")

    def test_entire_output_degenerate_returns_empty(self):
        junk = "".join(
            f"{i}. Use LaTeX to output the output content. " for i in range(1, 21)
        )
        self.assertEqual(_collapse_degenerate_tail(junk), "")

    def test_legit_numbered_list_kept(self):
        text = (
            "Steps:\n1. Open the file.\n2. Read the contents.\n"
            "3. Close the file.\n4. Save the draft.\n5. Submit it."
        )
        self.assertEqual(_collapse_degenerate_tail(text), text)

    def test_shared_prefix_different_items_kept(self):
        text = "Steps:\n1. go to market\n2. go to school\n3. go to work\n4. go to bed\n5. go"
        self.assertEqual(_collapse_degenerate_tail(text), text)


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

    def test_mixed_success_keeps_scanned_pdf_for_residual(self):
        # One page OCR'd, one failed: the residual image must stay for Go
        # OCR, so image_source_type stays scanned_pdf (vn_ocr only when
        # every scanned image was replaced).
        doc = _make_doc()
        with mock.patch(
            "docreader.parser.vietnamese_legal_parser._ocr_page_image",
            side_effect=["OCR trang 2", ""],
        ):
            out = _parser()._ocr_scanned_pages(doc)

        self.assertIn("OCR trang 2", out.content)
        self.assertIn("images/nd_page_3.jpg", out.content)
        self.assertNotIn("nd_page_2.jpg](images", out.content)
        self.assertEqual(out.metadata["vn_ocr_pages"], 1)
        self.assertEqual(out.metadata["image_source_type"], "scanned_pdf")
        self.assertEqual(len(out.images), 1)
        # Page markers for all pages survive the partial replacement.
        self.assertIn("<!-- page 1 -->", out.content)
        self.assertIn("<!-- page 2 -->", out.content)
        self.assertIn("<!-- page 3 -->", out.content)

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


def _capture_ocr_payload(**ocr_kwargs):
    """Run _ocr_page_image with a mocked Session and return the payload."""
    from docreader.parser import vietnamese_legal_parser as m

    captured = {}

    class _Resp:
        def raise_for_status(self):
            pass

        def json(self):
            return {"choices": [{"message": {"content": "ok"}}]}

    class _Session:
        def post(self, url, json=None, headers=None, timeout=None):
            captured["payload"] = json
            return _Resp()

    with mock.patch.object(m, "_get_ocr_session", return_value=_Session()):
        m._ocr_page_image(
            b"jpeg", 1, url="http://ocr/v1", model="mdl", **ocr_kwargs
        )
    return captured["payload"]


class TestRepetitionPenalty(unittest.TestCase):
    def test_default_sends_no_repetition_penalty(self):
        with mock.patch.dict("os.environ", {}, clear=False):
            import os

            os.environ.pop("DOCREADER_VN_OCR_REPETITION_PENALTY", None)
            payload = _capture_ocr_payload()
        self.assertNotIn("repetition_penalty", payload)

    def test_explicit_env_is_sent(self):
        with mock.patch.dict(
            "os.environ", {"DOCREADER_VN_OCR_REPETITION_PENALTY": "1.2"}
        ):
            payload = _capture_ocr_payload()
        self.assertEqual(payload.get("repetition_penalty"), 1.2)

    def test_explicit_env_one_disables(self):
        with mock.patch.dict(
            "os.environ", {"DOCREADER_VN_OCR_REPETITION_PENALTY": "1"}
        ):
            payload = _capture_ocr_payload()
        self.assertNotIn("repetition_penalty", payload)

    def test_per_upload_override_wins_over_env(self):
        with mock.patch.dict(
            "os.environ", {"DOCREADER_VN_OCR_REPETITION_PENALTY": "1.2"}
        ):
            payload = _capture_ocr_payload(repetition_penalty=1.4)
        self.assertEqual(payload.get("repetition_penalty"), 1.4)

    def test_per_upload_one_disables_despite_env(self):
        with mock.patch.dict(
            "os.environ", {"DOCREADER_VN_OCR_REPETITION_PENALTY": "1.2"}
        ):
            payload = _capture_ocr_payload(repetition_penalty=1.0)
        self.assertNotIn("repetition_penalty", payload)

    def test_per_upload_zero_disables_despite_env(self):
        with mock.patch.dict(
            "os.environ", {"DOCREADER_VN_OCR_REPETITION_PENALTY": "1.2"}
        ):
            payload = _capture_ocr_payload(repetition_penalty=0.0)
        self.assertNotIn("repetition_penalty", payload)


class TestForceScannedMarkers(unittest.TestCase):
    def _image_pdf(self, num_pages=2):
        import io

        from PIL import Image

        buf = io.BytesIO()
        pages = [Image.new("RGB", (64, 64), c) for c in ("white", "black")]
        pages = (pages * ((num_pages // 2) + 1))[:num_pages]
        pages[0].save(buf, format="PDF", save_all=True, append_images=pages[1:])
        return buf.getvalue()

    def test_force_scanned_has_marker_per_image_page(self):
        pdf_bytes = self._image_pdf(2)
        p = VietnameseLegalPDFParser(
            file_name="forced.pdf", file_type="pdf", pdf_force_scanned="true"
        )
        with mock.patch.object(
            VietnameseLegalPDFParser, "_ocr_enabled", return_value=False
        ):
            doc = p.parse_into_text(pdf_bytes)
        self.assertEqual(doc.metadata.get("page_count"), 2)
        self.assertEqual(doc.metadata.get("scanned_page_count"), 2)
        self.assertEqual(doc.content.count("<!-- page "), 2)
        self.assertIn("<!-- page 1 -->", doc.content)
        self.assertIn("<!-- page 2 -->", doc.content)
        self.assertIn("images/forced_page_1.jpg", doc.content)
        self.assertIn("images/forced_page_2.jpg", doc.content)
        self.assertEqual(len(doc.images), 2)

    def test_fallback_render_gets_markers_without_duplicates(self):
        # Simulate the routing-fallback path: _route fails, super falls back
        # to full image rendering. Markers must be added exactly once.
        from docreader.parser import vietnamese_legal_parser as m

        pdf_bytes = self._image_pdf(2)
        p = VietnameseLegalPDFParser(file_name="fb.pdf", file_type="pdf")
        real_route = p._route
        with mock.patch.object(
            m.PDFParser, "_route", side_effect=RuntimeError("boom")
        ), mock.patch.object(
            VietnameseLegalPDFParser, "_ocr_enabled", return_value=False
        ):
            doc = p.parse_into_text(pdf_bytes)
        self.assertEqual(doc.content.count("<!-- page "), 2)
        self.assertIn("images/fb_page_1.jpg", doc.content)
        # Sanity: the normal route is unchanged (no duplication there either).
        with mock.patch.object(
            VietnameseLegalPDFParser, "_ocr_enabled", return_value=False
        ):
            doc2 = p.parse_into_text(pdf_bytes)
        _ = real_route  # keep linters quiet about unused capture
        self.assertEqual(doc2.content.count("<!-- page "), 2)

    def test_mixed_route_does_not_duplicate_markers(self):
        doc = _make_doc()
        out = _parser()._ensure_image_page_markers(doc)
        self.assertEqual(out.content, doc.content)
        self.assertEqual(out.content.count("<!-- page "), 3)


if __name__ == "__main__":
    unittest.main()
