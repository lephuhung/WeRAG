"""Text of a Party document's attachment as blocks (paragraphs and tables).

A block is ``("p", text)`` or ``("table", [[cell, ...], ...])``. Legacy
Vietnamese encodings are converted to Unicode here:

* TCVN3 (ABC, fonts ``.VnTime``/``.VnTimeH``…): one byte per letter on the
  Latin-1 code points (``µ`` = à, ``©`` = â …). The ``…H`` fonts draw the
  lower-case codes as capitals, so a run in such a font is upper-cased.
* VNI (``VNI-Times``…): a base letter followed by a mark character
  (``aù`` = á, ``oâ`` = ô, ``ñ`` = đ).

Scanned pages are not read here: they go to the OCR draft on the server.
"""
from __future__ import annotations

import bisect
import io
import re
import struct
import unicodedata
import zipfile
from xml.etree import ElementTree as ET

import olefile
import pymupdf

# --- TCVN3 -----------------------------------------------------------------
_TCVN3 = {
    "µ": "à", "¶": "ả", "·": "ã", "¸": "á", "¹": "ạ",
    "¨": "ă", "»": "ằ", "¼": "ẳ", "½": "ẵ", "¾": "ắ", "Æ": "ặ",
    "©": "â", "Ç": "ầ", "È": "ẩ", "É": "ẫ", "Ê": "ấ", "Ë": "ậ",
    "®": "đ",
    "Ì": "è", "Î": "ẻ", "Ï": "ẽ", "Ð": "é", "Ñ": "ẹ",
    "ª": "ê", "Ò": "ề", "Ó": "ể", "Ô": "ễ", "Õ": "ế", "Ö": "ệ",
    "×": "ì", "Ø": "ỉ", "Ü": "ĩ", "Ý": "í", "Þ": "ị",
    "ß": "ò", "á": "ỏ", "â": "õ", "ã": "ó", "ä": "ọ",
    "«": "ô", "å": "ồ", "æ": "ổ", "ç": "ỗ", "è": "ố", "é": "ộ",
    "¬": "ơ", "ê": "ờ", "ë": "ở", "ì": "ỡ", "í": "ớ", "î": "ợ",
    "ï": "ù", "ñ": "ủ", "ò": "ũ", "ó": "ú", "ô": "ụ",
    "\xad": "ư", "õ": "ừ", "ö": "ử", "÷": "ữ", "ø": "ứ", "ù": "ự",
    "ú": "ỳ", "û": "ỷ", "ü": "ỹ", "ý": "ý", "þ": "ỵ",
    "¡": "Ă", "¢": "Â", "§": "Đ", "£": "Ê", "¤": "Ô", "¥": "Ơ", "¦": "Ư",
}
_TCVN3_TABLE = str.maketrans(_TCVN3)


def tcvn3_to_unicode(text: str, capitals: bool = False) -> str:
    out = text.translate(_TCVN3_TABLE)
    return out.upper() if capitals else out


# --- VNI -------------------------------------------------------------------
_VNI_TONE = {"ù": "́", "ø": "̀", "û": "̉", "õ": "̃", "ï": "̣"}
_VNI_CIRC_TONE = {"á": "́", "à": "̀", "å": "̉", "ã": "̃", "ä": "̣"}
_VNI_BREVE_TONE = {"é": "́", "è": "̀", "ú": "̉", "ü": "̃", "ë": "̣"}
_VNI_I = {"í": "í", "ì": "ì", "æ": "ỉ", "ó": "ĩ", "ò": "ị"}


def _vni_letter(m: re.Match) -> str:
    base, marks = m.group(1), m.group(2)
    upper = base.isupper()
    b = base.lower()
    lm = marks.lower()
    shape, tone = "", ""
    for ch in lm:
        if ch == "â" and b in "aeo":
            shape = "̂"
        elif ch == "ê" and b == "a":
            shape = "̆"
        elif ch == "ô" and b in "ou":
            shape = "̛"
        elif ch == "ö" and b == "u":
            shape = "̛"
        elif ch in _VNI_TONE:
            tone = _VNI_TONE[ch]
        elif ch in _VNI_CIRC_TONE and b in "aeo":
            shape, tone = "̂", _VNI_CIRC_TONE[ch]
        elif ch in _VNI_BREVE_TONE and b == "a":
            shape, tone = "̆", _VNI_BREVE_TONE[ch]
        else:
            return m.group(0)
    s = unicodedata.normalize("NFC", b + shape + tone)
    return s.upper() if upper else s


_VNI_RE = re.compile(r"([aeiouyAEIOUY])([âêùøûõïáàåãäéèúüëÂÊÙØÛÕÏÁÀÅÃÄÉÈÚÜË]{1,2})")


def vni_to_unicode(text: str) -> str:
    text = text.replace("ñ", "đ").replace("Ñ", "Đ")
    # ô / ö standing alone are VNI's ơ / ư (Unicode ô is written "oâ")
    text = text.translate(str.maketrans({"ô": "ơ", "Ô": "Ơ", "ö": "ư", "Ö": "Ư"}))
    text = re.sub(r"([ơƠưƯ])([ùøûõïÙØÛÕÏ])", lambda m: unicodedata.normalize(
        "NFC", m.group(1) + _VNI_TONE[m.group(2).lower()]), text)
    text = re.sub(r"[íìæóòÍÌÆÓÒ]", lambda m: _VNI_I.get(m.group(0).lower(), m.group(0)) if m.group(0).islower()
                  else _VNI_I.get(m.group(0).lower(), m.group(0)).upper(), text)
    return _VNI_RE.sub(_vni_letter, text)


LEGACY_TCVN3_FONT = re.compile(r"^\.?Vn", re.I)
LEGACY_VNI_FONT = re.compile(r"^VNI[-_ ]", re.I)


def font_encoding(font: str) -> str:
    f = (font or "").split("+")[-1].strip()
    if LEGACY_TCVN3_FONT.match(f):
        return "tcvn3"
    if LEGACY_VNI_FONT.match(f):
        return "vni"
    return "unicode"


def convert_run(text: str, font: str) -> str:
    enc = font_encoding(font)
    if enc == "tcvn3":
        name = (font or "").split("+")[-1]
        return tcvn3_to_unicode(text, capitals=bool(re.search(r"H(\b|-|_|$)", name.split(",")[0])))
    if enc == "vni":
        return vni_to_unicode(text)
    return text


# --- .docx -----------------------------------------------------------------
W = "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}"


def _docx_fonts(z: zipfile.ZipFile) -> tuple[dict, str]:
    """style id → font, and the document default font."""
    styles: dict[str, str] = {}
    default = ""
    try:
        root = ET.fromstring(z.read("word/styles.xml"))
    except KeyError:
        return styles, default
    d = root.find(f"{W}docDefaults/{W}rPrDefault/{W}rPr/{W}rFonts")
    if d is not None:
        default = d.get(f"{W}ascii") or d.get(f"{W}hAnsi") or ""
    for st in root.iter(f"{W}style"):
        f = st.find(f"{W}rPr/{W}rFonts")
        if f is not None:
            styles[st.get(f"{W}styleId")] = f.get(f"{W}ascii") or f.get(f"{W}hAnsi") or ""
    return styles, default


def _docx_par(p, styles, default) -> str:
    pstyle = p.find(f"{W}pPr/{W}pStyle")
    pfont = styles.get(pstyle.get(f"{W}val"), "") if pstyle is not None else ""
    out = []
    for r in p.iter(f"{W}r"):
        f = r.find(f"{W}rPr/{W}rFonts")
        rstyle = r.find(f"{W}rPr/{W}rStyle")
        font = (f.get(f"{W}ascii") or f.get(f"{W}hAnsi") or "") if f is not None else ""
        if not font and rstyle is not None:
            font = styles.get(rstyle.get(f"{W}val"), "")
        font = font or pfont or default
        caps = r.find(f"{W}rPr/{W}caps") is not None
        parts = []
        for el in r:
            if el.tag == f"{W}t":
                parts.append(el.text or "")
            elif el.tag == f"{W}tab":
                parts.append("\t")
            elif el.tag in (f"{W}br", f"{W}cr"):
                parts.append("\n")
        t = convert_run("".join(parts), font)
        out.append(t.upper() if caps else t)
    return "".join(out)


def docx_blocks(data: bytes) -> list:
    z = zipfile.ZipFile(io.BytesIO(data))
    styles, default = _docx_fonts(z)
    body = ET.fromstring(z.read("word/document.xml")).find(f"{W}body")
    blocks: list = []

    def walk(el):
        for child in el:
            if child.tag == f"{W}p":
                blocks.append(("p", _docx_par(child, styles, default)))
            elif child.tag == f"{W}tbl":
                rows = []
                for tr in child.findall(f"{W}tr"):
                    rows.append(["\n".join(_docx_par(p, styles, default) for p in tc.iter(f"{W}p")).strip()
                                 for tc in tr.findall(f"{W}tc")])
                blocks.append(("table", rows))
            elif child.tag == f"{W}sdt":
                c = child.find(f"{W}sdtContent")
                if c is not None:
                    walk(c)
    walk(body)
    return blocks


# --- .doc (Word 97-2003) ---------------------------------------------------
def _doc_streams(data: bytes):
    ole = olefile.OleFileIO(io.BytesIO(data))
    wd = ole.openstream("WordDocument").read()
    flags = struct.unpack_from("<H", wd, 0x0A)[0]
    if flags & 0x0100:
        raise ValueError("encrypted .doc")
    table = ole.openstream("1Table" if flags & 0x0200 else "0Table").read()
    return wd, table


def _doc_chars(wd: bytes, table: bytes):
    """(cp → (text, is8bit, fc)) pieces of the main text."""
    fc_clx, lcb_clx = struct.unpack_from("<II", wd, 0x01A2)
    clx = table[fc_clx:fc_clx + lcb_clx]
    i = 0
    while i < len(clx) and clx[i] == 0x01:
        i += 3 + struct.unpack_from("<H", clx, i + 1)[0]
    if i >= len(clx) or clx[i] != 0x02:
        raise ValueError("no piece table")
    lcb = struct.unpack_from("<I", clx, i + 1)[0]
    plc = clx[i + 5:i + 5 + lcb]
    n = (lcb - 4) // 12
    cps = struct.unpack_from(f"<{n + 1}I", plc, 0)
    ccp_text = struct.unpack_from("<I", wd, 0x004C)[0]
    pieces = []
    for k in range(n):
        pcd = plc[(n + 1) * 4 + k * 8:(n + 1) * 4 + k * 8 + 8]
        fc = struct.unpack_from("<I", pcd, 2)[0]
        cp0, cp1 = cps[k], min(cps[k + 1], ccp_text)
        if cp0 >= cp1:
            continue
        length = cp1 - cp0
        if fc & 0x40000000:
            off = (fc & 0x3FFFFFFF) // 2
            pieces.append((cp0, wd[off:off + length].decode("latin-1"), True, off))
        else:
            pieces.append((cp0, wd[fc:fc + 2 * length].decode("utf-16-le", "replace"), False, fc))
    return pieces


def _doc_fonts(wd: bytes, table: bytes) -> list[str]:
    fc, lcb = struct.unpack_from("<II", wd, 0x0112)
    raw = table[fc:fc + lcb]
    if len(raw) < 4:
        return []
    count = struct.unpack_from("<H", raw, 0)[0]
    names, pos = [], 4
    for _ in range(count):
        if pos >= len(raw):
            break
        cb = raw[pos] + 1
        ffn = raw[pos:pos + cb]
        name = ffn[40:].decode("utf-16-le", "ignore").split("\x00")[0]
        names.append(name)
        pos += cb
    return names


def _doc_char_fonts(wd: bytes, table: bytes, fonts: list[str]) -> list[tuple[int, int, str]]:
    """(fc start, fc end, font name) runs from the CHPX FKPs (sprmCRgFtc0)."""
    fc_plc, lcb_plc = struct.unpack_from("<II", wd, 0x00FA)
    plc = table[fc_plc:fc_plc + lcb_plc]
    n = (len(plc) - 4) // 8
    if n <= 0:
        return []
    pns = struct.unpack_from(f"<{n}I", plc, (n + 1) * 4)
    runs = []
    for pn in pns:
        page = wd[pn * 512:(pn + 1) * 512]
        if len(page) < 512:
            continue
        crun = page[511]
        fcs = struct.unpack_from(f"<{crun + 1}I", page, 0)
        for j in range(crun):
            b = page[4 * (crun + 1) + j]
            font = ""
            if b:
                off = b * 2
                cb = page[off]
                grpprl = page[off + 1:off + 1 + cb]
                k = 0
                while k + 2 <= len(grpprl):
                    sprm = struct.unpack_from("<H", grpprl, k)[0]
                    spra = sprm >> 13
                    size = {0: 1, 1: 1, 2: 2, 3: 4, 4: 2, 5: 2, 7: 3}.get(spra)
                    if spra == 6:
                        size = 1 + grpprl[k + 2] if k + 2 < len(grpprl) else 1
                    if sprm in (0x4A4F, 0x4A51) and k + 4 <= len(grpprl):  # sprmCRgFtc0 / CRgFtc2
                        ftc = struct.unpack_from("<H", grpprl, k + 2)[0]
                        if ftc < len(fonts) and (sprm == 0x4A4F or not font):
                            font = fonts[ftc]
                    k += 2 + (size or 1)
            runs.append((fcs[j], fcs[j + 1], font))
    return runs


def _doc_papx_runs(wd: bytes, table: bytes) -> list[tuple[int, int, bool, bool]]:
    """(fc start, fc end, in a table, row end) of every paragraph, from the
    PAPX FKPs (sprmPFInTable 0x2416, sprmPFTtp 0x2417, sprmPItap 0x6649,
    sprmPFInnerTtp 0x244C)."""
    fc_plc, lcb_plc = struct.unpack_from("<II", wd, 0x0102)
    plc = table[fc_plc:fc_plc + lcb_plc]
    n = (len(plc) - 4) // 8
    runs = []
    for pn in (struct.unpack_from(f"<{n}I", plc, (n + 1) * 4) if n > 0 else ()):
        page = wd[pn * 512:(pn + 1) * 512]
        if len(page) < 512:
            continue
        crun = page[511]
        fcs = struct.unpack_from(f"<{crun + 1}I", page, 0)
        for j in range(crun):
            b = page[4 * (crun + 1) + 13 * j]
            in_table = ttp = False
            if b:
                off = b * 2
                cb = page[off]
                if cb == 0:
                    size = 2 * page[off + 1]
                    start = off + 2
                else:
                    size = 2 * cb - 1
                    start = off + 1
                grpprl = page[start + 2:start + size]  # after istd
                k = 0
                while k + 2 <= len(grpprl):
                    sprm = struct.unpack_from("<H", grpprl, k)[0]
                    spra = sprm >> 13
                    if spra == 6:
                        size2 = 1 + (grpprl[k + 2] if k + 2 < len(grpprl) else 0)
                        if sprm == 0xC615 and k + 4 <= len(grpprl):  # sprmPChgTabs: cb may be 255
                            size2 = 1 + grpprl[k + 2]
                    else:
                        size2 = {0: 1, 1: 1, 2: 2, 3: 4, 4: 2, 5: 2, 7: 3}[spra]
                    val = grpprl[k + 2] if k + 2 < len(grpprl) else 0
                    if sprm == 0x2416:
                        in_table = bool(val)
                    elif sprm in (0x2417, 0x244C):
                        ttp = ttp or bool(val)
                    elif sprm == 0x6649 and k + 6 <= len(grpprl):
                        in_table = in_table or struct.unpack_from("<I", grpprl, k + 2)[0] > 0
                    k += 2 + size2
            runs.append((fcs[j], fcs[j + 1], in_table, ttp))
    runs.sort()
    return runs


def _lookup(runs, starts, pos):
    i = bisect.bisect_right(starts, pos) - 1
    if i >= 0 and runs[i][0] <= pos < runs[i][1]:
        return runs[i]
    return None


def _doc_chars_fc(data: bytes):
    """(char, fc, font) of the main text, fonts decoded per run."""
    wd, table = _doc_streams(data)
    pieces = _doc_chars(wd, table)
    try:
        fonts = _doc_fonts(wd, table)
        runs = sorted(_doc_char_fonts(wd, table, fonts))
    except Exception:  # noqa: BLE001 - fonts only refine the encoding
        runs = []
    starts = [r[0] for r in runs]
    out = []
    for _cp, text, is8, fc in pieces:
        step = 1 if is8 else 2
        for idx, ch in enumerate(text):
            pos = fc + idx * step
            r = _lookup(runs, starts, pos) if runs else None
            out.append((ch, pos, r[2] if r else ""))
    return out, wd, table


def _decode_runs(chars) -> list[tuple[str, int]]:
    """Group chars by font, convert each group; (char, fc) back."""
    out, buf, cur = [], [], None

    def flush():
        if not buf:
            return
        text = convert_run("".join(c for c, _ in buf), cur or "")
        if len(text) == len(buf):
            out.extend((t, fc) for t, (_c, fc) in zip(text, buf))
        else:
            out.extend((t, buf[0][1]) for t in text)
        buf.clear()

    for ch, fc, font in chars:
        if font != cur:
            flush()
            cur = font
        buf.append((ch, fc))
    flush()
    return out


def doc_text(data: bytes) -> str:
    chars, _wd, _table = _doc_chars_fc(data)
    return "".join(c for c, _ in _decode_runs(chars))


_FIELD_RE = re.compile(r"\x13[^\x14\x15]*\x14([^\x15]*)\x15|\x13[^\x15]*\x15")


def doc_blocks(data: bytes) -> list:
    chars, wd, table = _doc_chars_fc(data)
    decoded = _decode_runs(chars)
    try:
        papx = _doc_papx_runs(wd, table)
    except Exception:  # noqa: BLE001 - no table info: cells read as paragraphs
        papx = []
    starts = [r[0] for r in papx]
    blocks: list = []
    rows: list[list[str]] = []
    row: list[str] = []
    cell: list[str] = []
    para: list[str] = []

    def text_of(parts):
        t = _FIELD_RE.sub(lambda m: m.group(1) or "", "".join(parts))
        return re.sub(r"[\x00-\x08\x0e-\x1f]", "", t.replace("\x0b", "\n").replace("\x0c", "\n"))

    def close_table():
        nonlocal rows
        if row:
            rows.append(row.copy())
            row.clear()
        if rows:
            blocks.append(("table", rows))
            rows = []

    for ch, fc in decoded:
        if ch not in "\r\x07":
            para.append(ch)
            continue
        info = _lookup(papx, starts, fc) if papx else None
        in_table, ttp = (info[2], info[3]) if info else (ch == "\x07", False)
        if ch == "\x07" and ttp:
            rows.append(row.copy())
            row.clear()
            para.clear()
        elif ch == "\x07":
            cell.append(text_of(para))
            row.append("\n".join(cell).strip())
            cell.clear()
            para.clear()
        elif in_table:
            cell.append(text_of(para))
            para.clear()
        else:
            close_table()
            blocks.append(("p", text_of(para)))
            para.clear()
    if para:
        close_table()
        blocks.append(("p", text_of(para)))
    close_table()
    return blocks


# --- PDF with a text layer -------------------------------------------------
def pdf_blocks(data: bytes) -> tuple[list, list[int]]:
    """Blocks of the text pages and the page numbers (1-based) that have no
    usable text (scanned): those need OCR."""
    doc = pymupdf.open(stream=data, filetype="pdf")
    blocks: list = []
    scanned: list[int] = []
    for i, pg in enumerate(doc, 1):
        d = pg.get_text("dict")
        lines = []
        chars = 0
        for b in d["blocks"]:
            if b.get("type") != 0:
                continue
            for ln in b["lines"]:
                t = "".join(convert_run(s["text"], s.get("font", "")) for s in ln["spans"])
                chars += len(t.strip())
                lines.append((round(ln["bbox"][1]), ln["bbox"][0], t))
        if chars < 150:
            scanned.append(i)
            continue
        # one paragraph per line; lines of one paragraph are joined when the
        # next line does not start a new item
        lines.sort()
        cur = ""
        for _y, _x, t in lines:
            t = t.strip()
            if not t:
                continue
            if cur and not NEW_PARA_RE.match(t) and not cur.endswith((".", ":", ";")):
                cur += " " + t
            else:
                if cur:
                    blocks.append(("p", cur))
                cur = t
        if cur:
            blocks.append(("p", cur))
    return blocks, scanned


NEW_PARA_RE = re.compile(r"^(Điều\s+\d+|Chương\s+[IVXLC\d]+|Mục\s+\d+|Phần\s+|[IVXLC]+\.|\d+(\.\d+)*\.|[a-zđ]\)|-|\+|•|–)", re.U)


def blocks_of(kind: str, data: bytes) -> tuple[list, list[int]]:
    if kind == "docx":
        return docx_blocks(data), []
    if kind == "doc":
        return doc_blocks(data), []
    if kind == "pdf":
        return pdf_blocks(data)
    raise ValueError(kind)
