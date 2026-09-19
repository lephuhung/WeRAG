"""Vietnamese legal-document PDF parser (AIRAG-inspired).

Extends the builtin per-page PDF router with three behaviours ported from
AIRAG's deep_document_parser / ocr_service:

* **Broken Vietnamese text-layer detection.** Many Vietnamese government
  PDFs ship a text layer whose font mapping dropped the complex tone marks
  ("BỘ"→"B", "Độc lập"→"Đc lâp"): the Vietnamese base letters (ă â đ ê ô
  ơ ư) survive but the pre-composed tone characters (U+1EA0–1EF9) are gone.
  Trusting that layer produces garbled RAG content, so such pages are
  reclassified ``scanned`` and routed through OCR like real scans.
* **``<!-- page N -->`` markers** between page blocks so downstream Go-side
  chunking/citation can attribute content to exact pages (AIRAG uses the
  same markers).
* **Optional in-parser OCR** of scanned pages through an OpenAI-compatible
  endpoint (SenOCR-Vi / PaddleOCR-VL-class models served by vLLM). When
  ``DOCREADER_VN_OCR_URL`` is unset the parser keeps the stock behaviour:
  scanned pages are emitted as ``image_source_type=scanned_pdf`` image
  references and the Go App's OCR/VLM path handles them — no regression.

Environment (docreader service, ``DOCREADER_*`` convention):

* ``DOCREADER_VN_OCR_URL``         — e.g. http://vllm-ocr:8001/v1 (empty=off)
* ``DOCREADER_VN_OCR_MODEL``       — model name for /chat/completions
* ``DOCREADER_VN_OCR_API_KEY``     — optional Bearer token
* ``DOCREADER_VN_OCR_PROMPT``      — task prompt (default "OCR:", the
  PaddleOCR-VL task prompt SenOCR-Vi was trained with)
* ``DOCREADER_VN_OCR_MAX_TOKENS``  — default 4096
* ``DOCREADER_VN_OCR_CONCURRENCY`` — default 4 pages in flight
* ``DOCREADER_VN_OCR_TIMEOUT``     — seconds, default 180
* ``DOCREADER_VN_OCR_VLLM_XARGS``  — "0" (default) plain chat completions;
  "1" sends Unlimited-OCR's ngram decoding extras + skip_special_tokens=false
  (only for engines that still run Unlimited-OCR's NGram processor)
* ``DOCREADER_VN_OCR_DETECT_BROKEN_VN``   — default true
* ``DOCREADER_VN_OCR_VN_TONE_MIN_RATIO``  — default 0.02 (AIRAG
  HRAG_OCR_VN_TONE_MIN_RATIO)
"""

import base64
import logging
import os
import re
import threading
from concurrent.futures import ThreadPoolExecutor

import requests

from docreader.models.document import Document
from docreader.parser.pdf_parser import (
    PDFParser,
    _env_bool,
    _env_float,
    _env_int,
)

logger = logging.getLogger(__name__)

# --- Configuration ---------------------------------------------------------

VN_OCR_URL = os.environ.get("DOCREADER_VN_OCR_URL", "").strip().rstrip("/")
VN_OCR_MODEL = os.environ.get("DOCREADER_VN_OCR_MODEL", "").strip()
VN_OCR_API_KEY = os.environ.get("DOCREADER_VN_OCR_API_KEY", "").strip()
VN_OCR_PROMPT = os.environ.get("DOCREADER_VN_OCR_PROMPT", "OCR:")
VN_OCR_MAX_TOKENS = _env_int("DOCREADER_VN_OCR_MAX_TOKENS", 4096)
VN_OCR_CONCURRENCY = _env_int("DOCREADER_VN_OCR_CONCURRENCY", 4)
VN_OCR_TIMEOUT = _env_float("DOCREADER_VN_OCR_TIMEOUT", 180.0)
VN_OCR_VLLM_XARGS = _env_bool("DOCREADER_VN_OCR_VLLM_XARGS", False)
VN_DETECT_BROKEN = _env_bool("DOCREADER_VN_OCR_DETECT_BROKEN_VN", True)
VN_TONE_MIN_RATIO = _env_float("DOCREADER_VN_OCR_VN_TONE_MIN_RATIO", 0.02)

# --- Broken-Vietnamese text-layer detection --------------------------------

# Vietnamese-specific base letters that survive a corrupt text layer.
_VN_BASE_LETTERS = set("ăâđêôơưĂÂĐÊÔƠƯ")
# Complex tone characters lost by a corrupt layer (U+1EA0–1EF9).
_VN_TONE_LO, _VN_TONE_HI = "\u1ea0", "\u1ef9"
# Minimum letters for a confident verdict (AIRAG: too little selectable text
# means the scanned/native classifier decides, not this check).
_VN_MIN_LETTERS = 200


def _has_broken_vn_text_layer(text: str) -> bool:
    """True when the page looks Vietnamese but its tone chars are missing.

    Vietnamese base letters present + tone-char ratio below threshold → the
    embedded layer is corrupt; the page should be rendered and OCR'd.
    """
    letters = tone = base = 0
    for ch in text:
        if ch.isalpha():
            letters += 1
        if _VN_TONE_LO <= ch <= _VN_TONE_HI:
            tone += 1
        if ch in _VN_BASE_LETTERS:
            base += 1
    if letters < _VN_MIN_LETTERS:
        return False
    return base >= 5 and tone / letters < VN_TONE_MIN_RATIO


# --- OCR output cleanup (AIRAG _strip_ocr_markup) ---------------------------

# Detector-style OCR models (e.g. Unlimited-OCR run with "document parsing.",
# skip_special_tokens=False) wrap every recognised region as
# <|det|>LABEL [x1,y1,x2,y2]<|/det|>ACTUAL TEXT. SenOCR-Vi does not emit
# these, but keep the text, drop the detection prefix and leftover special
# tokens (<|im_end|>…) so output stays clean regardless of backend.
_DET_BLOCK_RE = re.compile(r"<\|det\|>.*?<\|/det\|>", re.DOTALL)
_SPECIAL_TOK_RE = re.compile(r"<\|[^|]*?\|>")
_MULTI_BLANK_RE = re.compile(r"\n{3,}")


def _strip_ocr_markup(text: str) -> str:
    text = _DET_BLOCK_RE.sub("", text)
    text = _SPECIAL_TOK_RE.sub("", text)
    return _MULTI_BLANK_RE.sub("\n\n", text).strip()


# Scanned-page image references emitted by PDFParser._assemble_blocks.
_PAGE_IMG_BLOCK_RE = re.compile(
    r"!\[[^\]\n]*_page_(\d+)\.jpg\]\((images/[^\)\n]*_page_\d+\.jpg)\)"
)

_OCR_HTTP_LOCK = threading.Lock()
_ocr_session = None


def _get_ocr_session() -> requests.Session:
    global _ocr_session
    with _OCR_HTTP_LOCK:
        if _ocr_session is None:
            _ocr_session = requests.Session()
    return _ocr_session


def _ocr_page_image(
    jpeg_bytes: bytes,
    page_no: int,
    *,
    url: str,
    model: str,
    api_key: str = "",
    prompt: str = "",
    vllm_xargs: bool = False,
) -> str:
    """One page image → OCR text via an OpenAI-compatible chat endpoint."""
    data_uri = "data:image/jpeg;base64," + base64.b64encode(jpeg_bytes).decode("ascii")
    payload = {
        "model": model,
        "messages": [
            {
                "role": "user",
                "content": [
                    {"type": "image_url", "image_url": {"url": data_uri}},
                    {"type": "text", "text": prompt or VN_OCR_PROMPT},
                ],
            }
        ],
        "temperature": 0.0,
        "max_tokens": VN_OCR_MAX_TOKENS,
    }
    if vllm_xargs:
        # Only for engines still running Unlimited-OCR's NGram processor:
        # structural special tokens must NOT be stripped, and the processor
        # needs these args per request (vLLM recipe). Off for SenOCR-Vi.
        payload["skip_special_tokens"] = False
        payload["vllm_xargs"] = {"ngram_size": 35, "window_size": 128}

    headers = {"Content-Type": "application/json"}
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"

    try:
        resp = _get_ocr_session().post(
            f"{url}/chat/completions",
            json=payload,
            headers=headers,
            timeout=VN_OCR_TIMEOUT,
        )
        resp.raise_for_status()
        text = resp.json()["choices"][0]["message"]["content"]
        return _strip_ocr_markup(text or "")
    except Exception as e:
        logger.error("[VN-OCR] page %d failed: %s", page_no, e)
        return ""


class VietnameseLegalPDFParser(PDFParser):
    """PDF parser tuned for Vietnamese legal documents.

    Reuses the builtin per-page router (native text vs scanned rendering,
    embedded figures, layout ordering) and adds broken-VN-layer detection,
    ``<!-- page N -->`` markers, and optional endpoint OCR for scanned pages.

    OCR endpoint config can come per-request via engine overrides
    (``openai_ocr_url``, ``openai_ocr_model``, ``openai_ocr_api_key``,
    ``openai_ocr_prompt``, ``openai_ocr_vllm_xargs`` — forwarded by the Go
    ``paddleocr_vl`` engine in OpenAI mode) and falls back to the
    ``DOCREADER_VN_OCR_*`` environment.
    """

    def __init__(self, *args, **kwargs):
        self._ocr_url = (
            (kwargs.pop("openai_ocr_url", "") or kwargs.pop("ocr_url", "") or "")
            .strip()
            .rstrip("/")
        )
        self._ocr_model = (
            kwargs.pop("openai_ocr_model", "") or kwargs.pop("ocr_model", "") or ""
        ).strip()
        self._ocr_api_key = (
            kwargs.pop("openai_ocr_api_key", "") or kwargs.pop("ocr_api_key", "") or ""
        )
        self._ocr_prompt = (
            kwargs.pop("openai_ocr_prompt", "") or kwargs.pop("ocr_prompt", "") or ""
        )
        xargs = str(kwargs.pop("openai_ocr_vllm_xargs", "") or "").strip()
        self._ocr_vllm_xargs = (
            VN_OCR_VLLM_XARGS
            if not xargs
            else xargs.lower() not in ("0", "false", "no", "off")
        )
        super().__init__(*args, **kwargs)
        # Env fallbacks when the request did not pin an endpoint.
        self._ocr_url = self._ocr_url or VN_OCR_URL
        self._ocr_model = self._ocr_model or VN_OCR_MODEL
        self._ocr_api_key = self._ocr_api_key or VN_OCR_API_KEY
        self._ocr_prompt = self._ocr_prompt or VN_OCR_PROMPT

    def _ocr_enabled(self) -> bool:
        return bool(self._ocr_url and self._ocr_model)

    def _reclassify_page(self, cls: str, plain_text: str) -> str:
        if (
            cls == "text"
            and VN_DETECT_BROKEN
            and _has_broken_vn_text_layer(plain_text)
        ):
            return "scanned"
        return cls

    def _page_marker(self, page_index: int) -> str:
        return f"<!-- page {page_index + 1} -->\n\n"

    def parse_into_text(self, content: bytes) -> Document:
        doc = super().parse_into_text(content)
        doc.metadata["vn_legal_parser"] = "1"
        if self._ocr_enabled() and doc.images:
            doc = self._ocr_scanned_pages(doc)
        return doc

    def _ocr_scanned_pages(self, doc: Document) -> Document:
        """Replace scanned-page image refs with endpoint-OCR'd text.

        Runs after ``_route`` so the pdfium lock is already released — the
        network calls here do not serialize against other PDF parses.
        On OCR failure the image ref is kept so the Go-side OCR fallback
        still sees the page.
        """
        blocks = doc.content.split("\n\n")
        jobs = {}  # block index -> (page_no, image ref_path)
        for i, block in enumerate(blocks):
            m = _PAGE_IMG_BLOCK_RE.search(block)
            if m and m.group(2) in doc.images:
                jobs[i] = (int(m.group(1)), m.group(2))
        if not jobs:
            return doc

        results = {}
        workers = max(1, VN_OCR_CONCURRENCY)
        with ThreadPoolExecutor(max_workers=workers) as pool:
            futs = {
                pool.submit(
                    _ocr_page_image,
                    base64.b64decode(doc.images[ref]),
                    page_no,
                    url=self._ocr_url,
                    model=self._ocr_model,
                    api_key=self._ocr_api_key,
                    prompt=self._ocr_prompt,
                    vllm_xargs=self._ocr_vllm_xargs,
                ): i
                for i, (page_no, ref) in jobs.items()
            }
            for fut, i in futs.items():
                try:
                    results[i] = fut.result()
                except Exception as e:  # defensive; _ocr_page_image catches
                    logger.error("[VN-OCR] page %d worker error: %s", jobs[i][0], e)
                    results[i] = ""

        ocr_pages = 0
        for i, (page_no, ref) in jobs.items():
            text = results.get(i, "")
            if text.strip():
                # The "<!-- page N -->" marker already sits in the preceding
                # block (page markers and page blocks are separate elements
                # after the \n\n split) — drop in the text alone.
                blocks[i] = text.strip()
                doc.images.pop(ref, None)
                ocr_pages += 1
            else:
                # OCR failed → keep the image ref so the Go-side VLM/OCR
                # fallback still processes this page.
                logger.warning(
                    "[VN-OCR] page %d produced no text; keeping image ref",
                    page_no,
                )

        doc.content = "\n\n".join(blocks)
        doc.metadata["vn_ocr_pages"] = ocr_pages
        doc.metadata["vn_ocr_model"] = self._ocr_model
        if ocr_pages:
            doc.metadata["image_source_type"] = "vn_ocr"
        return doc


def vietnamese_legal_available(overrides) -> tuple:
    """Always available — OCR is optional; without it the parser emits the
    same scanned-page image refs as the builtin engine (Go-side OCR path)."""
    return True, ""
