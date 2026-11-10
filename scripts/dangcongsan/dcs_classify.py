#!/usr/bin/env python3
"""Classify what each fetched Party document's attachments hold.

Reads ``records.jsonl`` from dcs_fetch.py and writes ``classified.jsonl``
(one line per record, every attachment analysed) and a summary on stderr.

Per attachment:

* real type from the bytes (a ``.pdf`` link may serve an HTML error page);
* PDF, per page: characters of the text layer, how much of the page images
  cover, the fonts; a page is ``text`` (born digital), ``ocr_layer`` (a scan
  with a scanner's OCR text under it), ``scan`` (image, no usable text) or
  ``blank``; a digital signature stamp ("Ký bởi: …", a date) on a scan is
  not text;
* Word (.docx/.doc): text, fonts, embedded pictures (scans pasted in Word);
* zip/rar: the members, analysed the same way;
* the Vietnamese encoding of any text: ``unicode``, ``tcvn3`` (.VnTime fonts,
  "Ban chÊp hµnh"), ``vni`` (VNI-Times, "Ban chaáp haønh") or ``none``.
"""
from __future__ import annotations

import argparse
import collections
import io
import json
import os
import re
import struct
import subprocess
import sys
import tempfile
import unicodedata
import zipfile

import olefile
import pymupdf

pymupdf.TOOLS.mupdf_display_errors(False)

VI_UNICODE = set("ăâđêôơưĂÂĐÊÔƠƯ") | {chr(c) for c in range(0x1EA0, 0x1EFA)} | set("àáảãạèéẻẽẹìíỉĩịòóỏõọùúủũụỳýỷỹỵÀÁÈÉÌÍÒÓÙÚÝ")
# TCVN3 (ABC) puts Vietnamese letters on cp1252 code points: µ¸¶·¹ ¨»¾¼½Æ ©ÇÊÈÉË ®  ...
TCVN3_MARKS = set("µ¸¶·¹¨»¾¼½Æ©ÇÊÈÉË®ÌÐÎÏÑªÒÕÓÔÖ×ÝØÜÞßãáâä«åèæçé¬êíëìîïóñòôõøöù÷úûüþ§¡¢£¤¥¦")
# VNI-Times writes a base letter then a tone/shape character
VNI_PAIR = re.compile(r"[aeiouyAEIOUY][øùûõïæåäãâáàÙØÛÕÏÆÅÄÃÂÁÀ]|[ôÔ][ùøûõï]|ñ|Ñ")
LEGACY_FONT = re.compile(r"(^|[+,])\.?Vn|VNI[-_ ]|VnTime|VnArial|ABC", re.I)
SIGN_STAMP = re.compile(r"(Ký bởi|Ký ngày|Signed by|Số và ký hiệu văn bản|\d{2}-\d{2}-\d{4} \d{2}:\d{2}:\d{2})", re.I)
OCR_PRODUCER = re.compile(r"OmniPage|PaperPort|ABBYY|FineReader|Readiris|Tesseract|ocrmypdf|Acrobat.*Paper Capture|Scan", re.I)


def sniff(data: bytes) -> str:
    head = data[:8]
    if not data:
        return "empty"
    if head.startswith(b"%PDF") or b"%PDF-" in data[:1024]:
        return "pdf"
    if head.startswith(b"PK\x03\x04"):
        try:
            names = zipfile.ZipFile(io.BytesIO(data)).namelist()
        except zipfile.BadZipFile:
            return "broken_zip"
        if "word/document.xml" in names:
            return "docx"
        return "zip"
    if head.startswith(b"\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"):
        return "doc"
    if head.startswith(b"Rar!"):
        return "rar"
    if head.startswith(b"7z\xbc\xaf"):
        return "7z"
    if head.startswith(b"{\\rtf"):
        return "rtf"
    low = data[:2048].lower()
    if b"<html" in low or b"<!doctype" in low:
        return "html"
    return "other"


def encoding_of(text: str, fonts: list[str] | None = None) -> str:
    letters = [c for c in text if c.isalpha()]
    if len(letters) < 40:
        return "none"
    vi = sum(c in VI_UNICODE for c in letters) / len(letters)
    tc = sum(c in TCVN3_MARKS for c in letters) / len(letters)
    vni = len(VNI_PAIR.findall(text)) / max(1, len(text.split()))
    legacy_font = any(LEGACY_FONT.search(f or "") for f in (fonts or []))
    if vi >= 0.08 and vi > tc * 2:
        return "unicode"
    if vni >= 0.15:
        return "vni"
    if tc >= 0.04 or (legacy_font and tc > 0.01):
        return "tcvn3"
    if vi >= 0.03:
        return "unicode"
    return "latin_only"  # text without Vietnamese marks: OCR without diacritics, or not Vietnamese


def vi_quality(text: str) -> float:
    """Share of word tokens that look like Vietnamese syllables (letters, at
    most 7 long, no digit/letter mix). Low on garbled OCR layers."""
    toks = re.findall(r"\w+", text)
    if not toks:
        return 0.0
    good = sum(1 for t in toks if t.isalpha() and len(t) <= 7)
    return round(good / len(toks), 3)


def analyse_pdf(data: bytes) -> dict:
    out: dict = {"type": "pdf"}
    try:
        doc = pymupdf.open(stream=data, filetype="pdf")
    except Exception as e:  # noqa: BLE001
        return {"type": "pdf", "error": f"open: {e}"}
    if doc.needs_pass:
        return {"type": "pdf", "error": "encrypted"}
    out["producer"] = (doc.metadata or {}).get("producer", "") or ""
    out["creator"] = (doc.metadata or {}).get("creator", "") or ""
    pages, fonts, texts = [], set(), []
    for pg in doc:
        try:
            text = pg.get_text()
        except Exception:  # noqa: BLE001
            text = ""
        area = abs(pg.rect) or 1
        img_cover = 0.0
        try:
            for info in pg.get_image_info():
                r = pymupdf.Rect(info["bbox"]) & pg.rect
                img_cover += abs(r)
        except Exception:  # noqa: BLE001
            pass
        img_cover = min(1.0, img_cover / area)
        for f in pg.get_fonts():
            fonts.add(f[3])
        # invisible text (render mode 3) = an OCR layer under a scan
        invisible = 0
        try:
            for b in pg.get_texttrace():
                if b.get("type") == 3 or b.get("opacity", 1) == 0:
                    invisible += len(b.get("chars", ()))
        except Exception:  # noqa: BLE001
            pass
        stripped = SIGN_STAMP.sub("", text)
        chars = len(re.sub(r"\s", "", stripped))
        if chars < 150 and img_cover >= 0.5:
            kind = "scan"
        elif chars < 30:
            kind = "blank" if img_cover < 0.1 else "scan"
        elif img_cover >= 0.5 and (invisible >= chars * 0.5 or OCR_PRODUCER.search(out["producer"] + out["creator"])):
            kind = "ocr_layer"
        else:
            kind = "text"
        pages.append({"kind": kind, "chars": chars, "img": round(img_cover, 2)})
        if kind in ("text", "ocr_layer"):
            texts.append(text)
    text = "\n".join(texts)
    out["pages"] = len(pages)
    out["page_kinds"] = dict(collections.Counter(p["kind"] for p in pages))
    out["fonts"] = sorted(fonts)[:20]
    out["text_chars"] = len(text)
    out["encoding"] = encoding_of(text, list(fonts))
    out["quality"] = vi_quality(text)
    out["head"] = re.sub(r"\s+", " ", text)[:300]
    kinds = out["page_kinds"]
    content = [k for k in ("text", "ocr_layer", "scan") if kinds.get(k)]
    out["class"] = "pdf_" + ("+".join(content) if content else "blank")
    return out


def analyse_docx(data: bytes) -> dict:
    out: dict = {"type": "docx"}
    try:
        z = zipfile.ZipFile(io.BytesIO(data))
        xml = z.read("word/document.xml").decode("utf-8", "replace")
    except Exception as e:  # noqa: BLE001
        return {"type": "docx", "error": str(e)}
    paras = re.findall(r"<w:p[ >].*?</w:p>", xml, re.S)
    text = "\n".join("".join(re.findall(r"<w:t(?: [^>]*)?>([^<]*)</w:t>", p)) for p in paras)
    text = re.sub(r"&amp;", "&", re.sub(r"&lt;", "<", re.sub(r"&gt;", ">", text)))
    fonts = set(re.findall(r'w:ascii="([^"]+)"', xml))
    try:
        fonts |= set(re.findall(r'w:name="([^"]+)"', z.read("word/fontTable.xml").decode("utf-8", "replace")))
    except KeyError:
        pass
    pics = [n for n in z.namelist() if n.startswith("word/media/")]
    out.update(text_chars=len(text), pictures=len(pics), fonts=sorted(fonts)[:20],
               encoding=encoding_of(text, list(fonts)), quality=vi_quality(text),
               head=re.sub(r"\s+", " ", text)[:300])
    out["class"] = "word_pictures" if len(text.strip()) < 300 and pics else "word_text"
    return out


def doc_text(data: bytes) -> tuple[str, list[str]]:
    """Text of a Word 97-2003 file from its piece table (no LibreOffice),
    and the font names of its font table."""
    ole = olefile.OleFileIO(io.BytesIO(data))
    wd = ole.openstream("WordDocument").read()
    flags = struct.unpack_from("<H", wd, 0x0A)[0]
    table = ole.openstream("1Table" if flags & 0x0200 else "0Table").read()
    # FibRgFcLcb97: fcClx at 0x01A2, lcbClx at 0x01A6; fcSttbfFfn 0x0112
    fc_clx, lcb_clx = struct.unpack_from("<II", wd, 0x01A2)
    clx = table[fc_clx:fc_clx + lcb_clx]
    i = 0
    while i < len(clx) and clx[i] == 0x01:  # skip Prc
        i += 3 + struct.unpack_from("<H", clx, i + 1)[0]
    if i >= len(clx) or clx[i] != 0x02:
        raise ValueError("no piece table")
    lcb = struct.unpack_from("<I", clx, i + 1)[0]
    plc = clx[i + 5:i + 5 + lcb]
    n = (lcb - 4) // 12
    cps = struct.unpack_from(f"<{n + 1}I", plc, 0)
    parts = []
    for k in range(n):
        pcd = plc[(n + 1) * 4 + k * 8:(n + 1) * 4 + k * 8 + 8]
        fc = struct.unpack_from("<I", pcd, 2)[0]
        length = cps[k + 1] - cps[k]
        if fc & 0x40000000:
            off = (fc & 0x3FFFFFFF) // 2
            parts.append(wd[off:off + length].decode("cp1252", "replace"))
        else:
            parts.append(wd[fc:fc + 2 * length].decode("utf-16-le", "replace"))
    text = "".join(parts).replace("\r", "\n").replace("\x07", "\t")
    fonts: list[str] = []
    try:
        fc_ffn, lcb_ffn = struct.unpack_from("<II", wd, 0x0112)
        raw = table[fc_ffn:fc_ffn + lcb_ffn]
        fonts = sorted(set(re.findall(r"[A-Za-z.][\w .\-]{2,30}", raw[4:].decode("utf-16-le", "ignore"))))
    except Exception:  # noqa: BLE001
        pass
    return text, fonts


def analyse_doc(data: bytes) -> dict:
    out: dict = {"type": "doc"}
    try:
        text, fonts = doc_text(data)
    except Exception as e:  # noqa: BLE001
        return {"type": "doc", "error": f"read: {e}"}
    out.update(text_chars=len(text), fonts=fonts[:20], encoding=encoding_of(text, fonts),
               quality=vi_quality(text), head=re.sub(r"\s+", " ", text)[:300])
    out["class"] = "word_text" if len(text.strip()) >= 300 else "word_empty"
    return out


def analyse_archive(data: bytes, kind: str) -> dict:
    out: dict = {"type": kind, "members": []}
    with tempfile.TemporaryDirectory() as d:
        src = os.path.join(d, "a." + kind)
        with open(src, "wb") as f:
            f.write(data)
        dst = os.path.join(d, "x")
        r = subprocess.run(["7z", "x", "-y", "-o" + dst, src], capture_output=True, timeout=120)
        if r.returncode != 0:
            return {**out, "error": "extract failed"}
        for root, _, files in os.walk(dst):
            for fn in sorted(files):
                with open(os.path.join(root, fn), "rb") as f:
                    m = analyse_bytes(f.read())
                m["name"] = fn
                out["members"].append(m)
    out["class"] = "archive"
    return out


def analyse_bytes(data: bytes) -> dict:
    kind = sniff(data)
    if kind == "pdf":
        return analyse_pdf(data)
    if kind == "docx":
        return analyse_docx(data)
    if kind == "doc":
        return analyse_doc(data)
    if kind in ("zip", "rar", "7z"):
        return analyse_archive(data, kind)
    return {"type": kind, "class": "unusable_" + kind, "bytes": len(data)}


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("records")
    ap.add_argument("out")
    a = ap.parse_args(argv)
    n = 0
    with open(a.records, encoding="utf-8") as fin, open(a.out, "w", encoding="utf-8") as fout:
        for line in fin:
            rec = json.loads(line)
            for att in rec["attachments"]:
                if att.get("path") and os.path.exists(att["path"]):
                    with open(att["path"], "rb") as f:
                        att["analysis"] = analyse_bytes(f.read())
                else:
                    att["analysis"] = {"class": "missing", "error": att.get("error", "")}
            fout.write(json.dumps(rec, ensure_ascii=False) + "\n")
            n += 1
    print(f"{n} records → {a.out}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
