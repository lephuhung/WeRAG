"""Chrome Screen AI OCR engine (libchromescreenai.so) via ctypes.

Ported from github.com/ayismas/chrome-ocr (MIT, Windows DLL) to the Linux
component. The library returns a VisualAnnotation protobuf: one LineBox per
text line with bbox, block/paragraph ids and a confidence. ``to_senocr_text``
lays those lines out the way SenOCR-Vi answers "OCR:" — one paragraph per
line, table rows on one line, no Markdown — so the output can serve as a
training target for SenOCR.
"""

from __future__ import annotations

import ctypes
import logging
import os
import re
import struct
import threading
from dataclasses import dataclass, field

import numpy as np
from PIL import Image

logger = logging.getLogger(__name__)

# --- protobuf wire format (no .proto needed) --------------------------------


def _varint(buf: bytes, i: int) -> tuple[int, int]:
    shift = val = 0
    while True:
        b = buf[i]
        i += 1
        val |= (b & 0x7F) << shift
        if not b & 0x80:
            return val, i
        shift += 7


def _int32(v: int) -> int:
    v &= 0xFFFFFFFFFFFFFFFF
    return v - (1 << 64) if v >= 1 << 63 else v


def _decode(buf: bytes) -> dict[int, list]:
    fields: dict[int, list] = {}
    i = 0
    while i < len(buf):
        tag, i = _varint(buf, i)
        fn, wt = tag >> 3, tag & 7
        if wt == 0:
            val, i = _varint(buf, i)
        elif wt == 1:
            val = struct.unpack_from("<d", buf, i)[0]
            i += 8
        elif wt == 2:
            n, i = _varint(buf, i)
            val = buf[i : i + n]
            i += n
        elif wt == 5:
            val = struct.unpack_from("<f", buf, i)[0]
            i += 4
        else:
            break
        fields.setdefault(fn, []).append(val)
    return fields


@dataclass
class Line:
    text: str
    x: int
    y: int
    w: int
    h: int
    block_id: int
    paragraph_id: int
    content_type: int  # 0 printed, 1 handwritten, 6 formula
    confidence: float
    language: str = ""
    words: list[tuple[str, float]] = field(default_factory=list)

    @property
    def x1(self) -> int:
        return self.x + self.w

    @property
    def y1(self) -> int:
        return self.y + self.h

    def as_dict(self) -> dict:
        return {
            "text": self.text,
            "bbox": [self.x, self.y, self.w, self.h],
            "block_id": self.block_id,
            "paragraph_id": self.paragraph_id,
            "content_type": self.content_type,
            "confidence": round(self.confidence, 4),
            "language": self.language,
            "words": [{"text": t, "confidence": round(c, 4)} for t, c in self.words],
        }


# Screen AI's multiscript recogniser sometimes returns Cyrillic look-alikes
# inside Vietnamese text ("NAМ", "ср"). Vietnamese documents carry no Cyrillic,
# so map them back to Latin before the text is used as a label.
_HOMOGLYPHS = str.maketrans(
    "АВЕКМНОРСТХУҮІЈЅаеорсухіјѕԁԛԝ",
    "ABEKMHOPCTXYYIJSaeopcyxijsdqw",
)


def fix_homoglyphs(text: str) -> str:
    return text.translate(_HOMOGLYPHS)


def parse_annotation(data: bytes) -> list[Line]:
    lines = []
    for raw in _decode(data).get(2, []):  # field 2 = repeated LineBox
        if not isinstance(raw, bytes):
            continue
        f = _decode(raw)
        rect = _decode(f[2][0]) if 2 in f and isinstance(f[2][0], bytes) else {}
        text = fix_homoglyphs(f[3][0].decode("utf-8", "replace")) if 3 in f else ""
        if not text.strip():
            continue
        words = []
        for wraw in f.get(1, []):  # field 1 = repeated WordBox
            if isinstance(wraw, bytes):
                wf = _decode(wraw)
                if 3 in wf:
                    words.append((fix_homoglyphs(wf[3][0].decode("utf-8", "replace")), float(wf.get(15, [0.0])[0])))
        lines.append(
            Line(
                text=text.strip(),
                x=_int32(rect.get(1, [0])[0]),
                y=_int32(rect.get(2, [0])[0]),
                w=_int32(rect.get(3, [0])[0]),
                h=_int32(rect.get(4, [0])[0]),
                block_id=_int32(f.get(5, [-1])[0]),
                paragraph_id=_int32(f.get(11, [-1])[0]),
                content_type=f.get(8, [0])[0],
                confidence=float(f.get(10, [0.0])[0]),
                language=f[4][0].decode("utf-8", "replace") if 4 in f else "",
                words=words,
            )
        )
    return lines


# --- layout: SenOCR-style plain text ----------------------------------------


@dataclass
class _Para:
    lines: list[Line] = field(default_factory=list)
    texts: list[str] = field(default_factory=list)

    @property
    def x(self) -> int:
        return min(l.x for l in self.lines)

    @property
    def x1(self) -> int:
        return max(l.x1 for l in self.lines)

    @property
    def y(self) -> int:
        return min(l.y for l in self.lines)

    @property
    def y1(self) -> int:
        return max(l.y1 for l in self.lines)


def _paragraphs(lines: list[Line], med_h: float) -> list[_Para]:
    """Group lines by Screen AI paragraph and join wrapped lines with a space.

    A line is joined to the previous one only when the previous line runs to
    the paragraph's right edge and this one starts at its left edge (justified
    body text wrapping). Centred titles and the national motto keep one line
    each, as SenOCR writes them.
    """
    groups: list[list[Line]] = []
    for ln in lines:
        if groups and (groups[-1][-1].block_id, groups[-1][-1].paragraph_id) == (
            ln.block_id,
            ln.paragraph_id,
        ):
            groups[-1].append(ln)
        else:
            groups.append([ln])

    out: list[_Para] = []
    tol = 1.5 * med_h
    for g in groups:
        left = min(l.x for l in g)
        right = max(l.x1 for l in g)
        para = _Para(lines=[g[0]], texts=[g[0].text])
        for prev, ln in zip(g, g[1:]):
            if prev.x1 >= right - tol and ln.x <= left + tol:
                para.lines.append(ln)
                para.texts[-1] += " " + ln.text
            else:
                out.append(para)
                para = _Para(lines=[ln], texts=[ln.text])
        out.append(para)
    return out


_RULE = re.compile(r"(?:(?<=\s)|^)\|+(?=\s|-|$)")


def _strip_rules(text: str) -> str:
    """Drop "|" glyphs read from table borders ("| - Tài liệu", "|-")."""
    return re.sub(r"\s{2,}", " ", _RULE.sub("", text)).strip()


def to_senocr_text(lines: list[Line]) -> str:
    """Lay lines out like SenOCR-Vi's "OCR:" answer.

    Paragraphs become one line each. Table rows (runs of side-by-side cells,
    ≥4 rows, or ≥3 rows with a ≥3-cell row) become one line with the cells in
    left-to-right order, which is how SenOCR writes tables under "OCR:". Two-
    column document headers (cơ quan | quốc hiệu) stay column by column.
    """
    if not lines:
        return ""
    heights = sorted(l.h for l in lines if l.h > 0) or [20]
    med_h = heights[(len(heights) - 1) // 2]
    paras = _paragraphs(lines, med_h)

    # Rows: consecutive paragraphs that sit side by side within one band.
    rows: list[list[_Para]] = []
    for p in paras:
        if rows:
            row = rows[-1]
            top = min(q.y for q in row)
            bottom = max(q.y1 for q in row)
            centre = (p.y + p.y1) / 2
            if top <= centre <= bottom and p.x >= row[-1].x1 - med_h:
                row.append(p)
                continue
        rows.append([p])

    table = [False] * len(rows)
    i = 0
    while i < len(rows):
        j = i
        while j < len(rows) and len(rows[j]) >= 2:
            j += 1
        run = rows[i:j]
        if len(run) >= 4 or (len(run) >= 3 and max(len(r) for r in run) >= 3):
            for k in range(i, j):
                table[k] = True
        i = j + 1 if j == i else j

    out: list[str] = []
    for row, is_table in zip(rows, table):
        row_out_start = len(out)
        if is_table:
            cells = sorted(row, key=lambda q: q.x)
            out.append(" ".join(" ".join(c.texts) for c in cells))
        else:
            for p in row:
                out.extend(p.texts)
        out[row_out_start:] = [_strip_rules(t) for t in out[row_out_start:]]
    return "\n".join(t for t in out if t)


def to_lines_text(lines: list[Line]) -> str:
    return "\n".join(l.text for l in lines)


# --- engine -----------------------------------------------------------------

_CB_SIZE = ctypes.CFUNCTYPE(ctypes.c_uint32, ctypes.c_char_p)
_CB_CONTENT = ctypes.CFUNCTYPE(None, ctypes.c_char_p, ctypes.c_uint32, ctypes.c_void_p)
_kBGRA_8888 = 6
_kPremul = 2


class ScreenAIEngine:
    """One initialised library per process (a second InitOCRUsingCallback
    corrupts the engine). PerformOCR is serialised with a lock; run several
    server processes for throughput."""

    _instance: "ScreenAIEngine | None" = None
    _init_lock = threading.Lock()

    @classmethod
    def get(cls, so_path: str) -> "ScreenAIEngine":
        with cls._init_lock:
            if cls._instance is None:
                cls._instance = cls(so_path)
            return cls._instance

    def __init__(self, so_path: str):
        so_path = os.path.realpath(so_path)
        comp_dir = os.path.dirname(so_path)
        # Chrome's host process normally provides this symbol; the image ships
        # a one-line stub (SCREENAI_STUB), a local fetch builds one next to it.
        stub = os.environ.get("SCREENAI_STUB") or os.path.join(comp_dir, "libthreadlogger_stub.so")
        if os.path.exists(stub):
            ctypes.CDLL(stub, mode=ctypes.RTLD_GLOBAL)
        dll = ctypes.CDLL(so_path)
        self._lock = threading.Lock()

        def _size(p: bytes) -> int:
            fp = os.path.join(comp_dir, p.decode().lstrip("/"))
            return os.path.getsize(fp) if os.path.exists(fp) else 0

        def _read(p: bytes, sz: int, buf) -> None:
            with open(os.path.join(comp_dir, p.decode().lstrip("/")), "rb") as f:
                ctypes.memmove(buf, f.read(sz), sz)

        self._callbacks = [_CB_SIZE(_size), _CB_CONTENT(_read)]
        dll.SetFileContentFunctions(*self._callbacks)
        dll.InitOCRUsingCallback.restype = ctypes.c_bool
        if not dll.InitOCRUsingCallback():
            raise RuntimeError("InitOCRUsingCallback returned false")
        dll.GetMaxImageDimension.restype = ctypes.c_uint32
        dll.PerformOCR.argtypes = [ctypes.c_void_p, ctypes.POINTER(ctypes.c_uint32)]
        dll.PerformOCR.restype = ctypes.c_void_p
        dll.FreeLibraryAllocatedCharArray.argtypes = [ctypes.c_void_p]
        dll.GetLibraryVersion.argtypes = [ctypes.POINTER(ctypes.c_uint32)] * 2
        major, minor = ctypes.c_uint32(), ctypes.c_uint32()
        dll.GetLibraryVersion(ctypes.byref(major), ctypes.byref(minor))
        self.version = f"{major.value}.{minor.value}"
        self.max_dimension = int(dll.GetMaxImageDimension()) or 2048
        self._dll = dll
        logger.info("Screen AI %s ready (max %d px)", self.version, self.max_dimension)

    def ocr(self, image: Image.Image) -> list[Line]:
        image = image.convert("RGB")
        w, h = image.size
        if max(w, h) > self.max_dimension:
            r = self.max_dimension / max(w, h)
            w, h = max(1, int(w * r)), max(1, int(h * r))
            image = image.resize((w, h), Image.LANCZOS)
        rgb = np.asarray(image)
        bgra = np.empty((h, w, 4), np.uint8)
        bgra[..., 0] = rgb[..., 2]
        bgra[..., 1] = rgb[..., 1]
        bgra[..., 2] = rgb[..., 0]
        bgra[..., 3] = 255
        bgra = np.ascontiguousarray(bgra)
        return parse_annotation(self._perform(bgra, w, h))

    def _perform(self, bgra: np.ndarray, w: int, h: int) -> bytes:
        row_bytes = w * 4
        pix_ptr = bgra.ctypes.data
        # SkPixelRef stub: only has to be non-null and carry size/pixels.
        pr = bytearray(48)
        struct.pack_into("<Qi", pr, 0, 0, 999999)
        struct.pack_into("<iiQQ", pr, 16, w, h, pix_ptr, row_bytes)
        pr_buf = (ctypes.c_uint8 * 48)(*pr)
        # SkBitmap layout (Chromium/Skia, x86-64).
        bm = bytearray(48)
        struct.pack_into("<QQQ", bm, 0, ctypes.addressof(pr_buf), pix_ptr, row_bytes)
        struct.pack_into("<Qii", bm, 24, 0, _kBGRA_8888, _kPremul)
        struct.pack_into("<ii", bm, 40, w, h)
        bm_buf = (ctypes.c_uint8 * 48)(*bm)

        n = ctypes.c_uint32(0)
        with self._lock:
            ptr = self._dll.PerformOCR(ctypes.addressof(bm_buf), ctypes.byref(n))
            if not ptr or not n.value:
                return b""
            data = ctypes.string_at(ptr, n.value)
            self._dll.FreeLibraryAllocatedCharArray(ptr)
        return data
