"""Segment a DocLayout into the component blocks of a Vietnamese
administrative document (Nghị định 30/2020/NĐ-CP, Phụ lục I).

The segmentation is positional: the header block of a văn bản is a two-column
layout (cơ quan on the left, quốc hiệu / tiêu ngữ / địa danh on the right)
implemented either as a borderless table or via tab stops, followed by a
centered trích yếu, the body, a right-column signature block and a bottom-left
"Nơi nhận" block.
"""

from __future__ import annotations

import re
import unicodedata
from dataclasses import dataclass, field
from typing import Optional

from . import doctypes
from .layout import DocLayout, Para, line_view

# ---------------------------------------------------------------------------
# Text helpers

_TONE = re.compile(r"[̀-̣ͯ]")


def fold(text: str) -> str:
    """Uppercase, strip accents/diacritics and collapse whitespace so that
    component keywords match regardless of casing or sloppy accents."""
    t = unicodedata.normalize("NFD", text or "")
    t = "".join(c for c in t if unicodedata.category(c) != "Mn")
    t = t.replace("đ", "d").replace("Đ", "D")
    return re.sub(r"\s+", " ", t).strip().upper()


_QUOC_HIEU_RE = re.compile(
    r"^(CONG\s*HOA?\s*XA\s*HOI|.*XA\s*HOI\s*CHU\s*NGHIA\s*VIET\s*NAM|"
    r"CONG\s*HOA\s*XA\s*HOI).*$"
)
_TIEU_NGU_RE = re.compile(
    r"^DOC\s*LAP\s*[-–—]\s*TU\s*DO\s*[-–—]\s*HANH\s*PHUC\.?$"
)
# "Số: 12/QĐ-UBND", "Số:    /BC-SYT" (dự thảo), "Số 12/UBND-VP", "12/2025/NĐ-CP".
# Must NOT match agency names starting with "SỞ" (folds to "SO" too).
# A văn bản quy phạm pháp luật names its type before the number: "Nghị
# quyết số: 162/2024/QH15", "Luật số: 64/2025/QH15" — the whole line, so a
# body sentence citing "Quyết định số 12/…" does not match.
_TYPED_SO_PREFIX = (r"(LUAT|BO\s+LUAT|PHAP\s+LENH|LENH|NGHI\s+QUYET(\s+LIEN\s+TICH)?|"
                    r"NGHI\s+DINH|QUYET\s+DINH|THONG\s+TU(\s+LIEN\s+TICH)?|CHI\s+THI)")
_SO_KY_HIEU_RE = re.compile(
    r"^SO\s*[:.]|^SO\s+\d|^SO\s*/|^SO\s+\S*/\s*[A-Z]|"
    r"^\d{1,5}[A-Z]?\s*/\s*(\d{4}\s*/\s*)?[A-Z]{1,8}\b|"
    r"^" + _TYPED_SO_PREFIX + r"\s+SO\s*[:.]?\s*[\d.…_ ]*/\s*[0-9A-Z/.\-]+$"
)
# a line holding the quốc hiệu alone — not a sentence quoting "nước Cộng
# hòa xã hội chủ nghĩa Việt Nam"
_QUOC_HIEU_LINE_RE = re.compile(
    r"^CONG\s*HOA?\s*XA\s*HOI\s*CHU\s*NGHIA\s*VIET\s*NAM\.?$")
# Địa danh – ngày tháng: "Huế, ngày 05 tháng 10 năm 2026" / drafts with blank
# day/month.  Anchored: a title like "Báo cáo ... năm 2025" or a body sentence
# mentioning a date must not match.
_DIA_DANH_RE = re.compile(
    r"^([^,\d]{2,60},\s*)?NGAY\s*[\d.…_ ]{0,8}\s*THANG\s*[\d.…_ ]{0,8}\s*NAM\b"
)
_MAT_RE = re.compile(r"^(DO\s+)?MAT(\s*[:.]|\s*$)|^DO\s+MAT\b")
_KHAN_RE = re.compile(r"^(DO\s+KHAN\s*[:.]?\s*)?(KHAN|HOA\s*TOC|THUONG\s*KHAN)\b")
_NOI_NHAN_RE = re.compile(r"^NOI\s*NHAN\s*[:.]?")
_KINH_GUI_RE = re.compile(r"^KINH\s+GUI\s*[:.]?")
_CAN_CU_RE = re.compile(r"^(CAN\s+CU|THEO\s+DE\s+NGHI|XET\s+DE\s+NGHI|XET\s+TO\s+TRINH)\b")
_PHU_LUC_RE = re.compile(r"^PHU\s+LUC\b")
# ô 11 — chỉ dẫn về phạm vi lưu hành, a whole line on its own
_CHI_DAN_RE = re.compile(r"^(XEM\s+XONG\s+TRA\s+LAI|LUU\s+HANH\s+NOI\s+BO)\.?$")
# ô 12 — ký hiệu người soạn thảo + số bản ("NVA.15", "TTr/2b"), raw text
_KY_HIEU_ST_RE = re.compile(r"^[^\W\d_]{1,8}\s?[./]\s?\d{1,3}[^\W\d_]?$")
# ô 13 — địa chỉ, điện thoại, fax, email, website of the cơ quan
_DIA_CHI_KW = r"(DIA\s+CHI|DT|DIEN\s+THOAI|FAX|E-?MAIL|WEBSITE)\b"
_DIA_CHI_START_RE = re.compile(r"^" + _DIA_CHI_KW)
_DIA_CHI_ANY_RE = re.compile(r"\b" + _DIA_CHI_KW)
_SO_PHACH_RE = re.compile(r"^SO\s*PHACH|^SO\s+LUONG\s+BAN")
_TRICH_VV_RE = re.compile(r"^V\s*/\s*V\b|^VE\s+VIEC\b")
# decorative rules typed as text under quốc hiệu / cơ quan / trích yếu
_RULE_LINE_RE = re.compile(r"^[\s_\-–—=~.*·•]+$")
# "CHỦ TỊCH ỦY BAN NHÂN DÂN TỈNH ..." — thẩm quyền ban hành line of a QĐ/NQ,
# printed centered right after the trích yếu
_THAM_QUYEN_RE = re.compile(
    r"^(CHU\s+TICH|BO\s+TRUONG|GIAM\s+DOC|THU\s+TUONG|TONG\s+GIAM\s+DOC|"
    r"CHANH\s+AN|VIEN\s+TRUONG|UY\s+BAN\s+NHAN\s+DAN|HOI\s+DONG\s+NHAN\s+DAN|"
    r"HOI\s+DONG\s+QUAN\s+TRI|THU\s+TRUONG|CUC\s+TRUONG|GIAM\s+DOC|"
    r"TRUONG\s+BAN|CHINH\s+PHU|BAN\s+THUONG\s+VU|BAN\s+CHAP\s+HANH)\b"
)

# ký hiệu loại văn bản trong số ký hiệu (Phụ lục III NĐ30) — see doctypes.py.
# Exact codes keep Đ (HĐ ≠ HD, ĐA ≠ DA); a code whose Đ was lost to a bad
# font/OCR ("QD", "ND") falls back to the accent-folded form when that form
# is not itself another type's code.
_SYMBOL_TYPES = dict(doctypes.SYMBOL_TYPES)
_SYMBOL_TYPES_FOLDED = {}
for _code, _slug in doctypes.SYMBOL_TYPES.items():
    _f = _code.replace("Đ", "D")
    if _f not in _SYMBOL_TYPES:
        _SYMBOL_TYPES_FOLDED[_f] = _slug

# tên loại line patterns, longest name first ("THONG TU LIEN TICH" before
# "THONG TU")
_DOC_TYPE_PATTERNS = [
    (d.slug, re.compile(r"\b" + r"\s+".join(fold(d.name).split()) + r"\b"))
    for d in sorted(doctypes.DOC_TYPES, key=lambda d: -len(d.name))
]

# Chức danh/authority lines that open a signature block.
_SIG_OPEN_RE = re.compile(
    r"^(TM|KT|TL|TUQ|Q)\s*\.|"
    r"^(KY\s+DUYET|QUYEN|KIEM\s+NGHIEM|CHU\s+TICH|PHO\s+CHU\s+TICH|"
    r"THU\s+TUONG|PHO\s+THU\s+TUONG|BO\s+TRUONG|PHO\s+BO\s+TRUONG|"
    r"GIAM\s+DOC|PHO\s+GIAM\s+DOC|THU\s+TRUONG|PHO\s+THU\s+TRUONG|"
    r"TRUONG\s+(PHONG|BAN|DOAN|KHOA|CHI\s+NHANH)|PHO\s+TRUONG|"
    r"TO\s+TRUONG|CHU\s+NHAN|TONG\s+GIAM\s+DOC|PHO\s+TONG|"
    r"VIEN\s+TRUONG|PHO\s+VIEN|CHANH\s+VAN\s+PHONG|PHO\s+CHANH|"
    r"N[/.]?D|U\.?B\.?N\.?D\.?|TONG\s+BIEN\s+TAP|BI\s+THU|PHO\s+BI\s+THU|"
    r"TRUONG\s+BAN|PHO\s+TRUONG\s+BAN|PHAT\s+NGON\s+VIEN|"
    r"DAI\s+DIEN|NGUOI\s+(KY|LAP|DUYET|VIET|GHI)|THU\s+KY|CHU\s+TOA)\b"
)


@dataclass
class Element:
    """A positioned text fragment: one per paragraph, or two for a split
    (tab-separated) paragraph."""
    para: Para
    zone: str          # left | right | full
    text: str          # the text that sits in this zone
    # the line of a paragraph read line by line (para is then its
    # line_view), -1 for a whole paragraph
    sub: int = -1


@dataclass
class Segmented:
    components: dict = field(default_factory=dict)
    para_labels: dict = field(default_factory=dict)  # para index -> component key
    detected_type: str = "unknown"
    body_para_indices: list = field(default_factory=list)
    para_table: dict = field(default_factory=dict)   # para index -> table_index|None

    def comp(self, key: str) -> dict:
        return self.components.get(key, {"found": False, "paras": [], "text": ""})

    def to_dict(self) -> dict:
        out = {
            "detected_type": self.detected_type,
            "components": {},
        }
        for key, comp in self.components.items():
            out["components"][key] = {
                "found": comp["found"],
                "paras": comp["paras"],
                "text": comp["text"],
                "zone": comp.get("zone"),
            }
        return out


def _line_split(p: Para) -> Optional[list]:
    """Content lines (indexes into p.text.split("\\n")) of a paragraph to
    read line by line, None to read it whole: a web export puts "CỘNG HÒA
    …⏎Độc lập …⏎-----", "KT. THỦ TƯỚNG⏎PHÓ THỦ TƯỚNG⏎⏎Tên" or "Nơi
    nhận:⏎- …;⏎- …" in one paragraph. Split when a line is the quốc hiệu or
    tiêu ngữ, when it opens with "Nơi nhận", or when it opens a khối ký and
    is short, unjustified lines; soft breaks in body prose keep it whole."""
    if p.zone == "split" or "\n" not in p.text:
        return None
    lines = p.text.split("\n")
    content = []
    short = True
    for k, ln in enumerate(lines):
        t = ln.strip()
        if not t or _RULE_LINE_RE.match(t):
            continue
        content.append(k)
        if len(t) > 80:
            short = False
    if len(content) < 2:
        return None
    for k in content:
        f = fold(lines[k])
        if _QUOC_HIEU_LINE_RE.match(f) or _TIEU_NGU_RE.match(f):
            return content
    first = fold(lines[content[0]])
    if _NOI_NHAN_RE.match(first):
        return content
    if short and p.alignment not in ("justify", "both") and _SIG_OPEN_RE.match(first):
        return content
    return None


def _elements(layout: DocLayout) -> list:
    """Expand paragraphs into zone elements in document order."""
    els = []
    for p in layout.paragraphs:
        subs = _line_split(p)
        if subs is not None:
            lines = p.text.split("\n")
            for k in subs:
                els.append(Element(line_view(p, k), p.zone, lines[k].strip(), k))
            continue
        text = p.text.strip()
        if p.zone == "split":
            if p.left_text and not _RULE_LINE_RE.match(p.left_text):
                els.append(Element(p, "left", p.left_text))
            if p.right_text and not _RULE_LINE_RE.match(p.right_text):
                els.append(Element(p, "right", p.right_text))
            continue
        if not text or _RULE_LINE_RE.match(text):
            continue
        els.append(Element(p, p.zone if p.zone != "full" else "full", text))
    return els


def _add(seg: Segmented, key: str, para_idx: int, text: str, zone: str,
         pos: Optional[int] = None, sub: int = -1):
    """Record a unit of component ``key``: paragraph ``para_idx``, or line
    ``sub`` of it (sub >= 0, a paragraph read line by line) — kept in
    comp["subs"] {para: [line, …]}."""
    comp = seg.components.setdefault(
        key, {"found": False, "paras": [], "texts": [], "text": "",
              "zone": zone, "zones": [], "pos": None})
    comp["found"] = True
    if pos is not None and (comp["pos"] is None or pos < comp["pos"]):
        comp["pos"] = pos
    if para_idx not in comp["paras"]:
        comp["paras"].append(para_idx)
        comp["zones"].append(zone)  # aligned with paras
    if sub >= 0:
        subs = comp.setdefault("subs", {}).setdefault(para_idx, [])
        if sub not in subs:
            subs.append(sub)
    if text:
        comp["texts"].append(text)
        comp["text"] = "\n".join(comp["texts"])
    seg.para_labels.setdefault(para_idx, key)


def _looks_agency(text: str, folded: str) -> bool:
    """Left-block header lines are org names: uppercase-ish or org keywords.
    The uppercase ratio must be measured on the raw text — `folded` is
    already uppercased so it is useless for that test."""
    if not folded:
        return False
    if _SO_KY_HIEU_RE.match(folded) or _MAT_RE.match(folded) or _KHAN_RE.match(folded):
        return False
    org_kw = re.compile(
        r"^(UBND|UY\s+BAN|BO\s|SO\s|CUC|VU\s|TONG\s+CUC|CONG\s+TY|"
        r"TAP\s+DOAN|TRUONG\s+(DAI\s+HOC|HOC)|VIEN|BAN\s|PHONG|"
        r"DAI\s+SU\s+QUAN|LANH\s+SU|QUAN|HUYEN|PHUONG|XA|THI\s+XA|"
        r"THANH\s+PHO|TINH|CHINH\s+PHU|QUOC\s+HOI|DANG|MAT\s+TRAN|"
        r"CONG\s+DOAN|DOAN\s+TN|HOI\s|TRUNG\s+TAM|SO\s+HUU|NHA\s+NUOC)"
    )
    if org_kw.match(folded):
        return True
    letters = [c for c in text if c.isalpha()]
    return len(letters) >= 4 and sum(1 for c in letters if c.isupper()) / len(letters) > 0.8


def _type_from_line(line: str) -> Optional[str]:
    f = fold(line)
    for name, rx in _DOC_TYPE_PATTERNS:
        m = rx.search(f)
        if m and m.start() == 0:
            return name
    return None


def _type_from_symbol(so_ky_hieu: str) -> Optional[str]:
    """'Số: 12/QĐ-UBND' → quyet_dinh; 'Số: 12/UBND-VP' (no type code) →
    cong_van (NĐ30: công văn ký hiệu = cơ quan-đơn vị soạn thảo, also
    "45/PA05-Đ4"); 'Số: 24/2018/QH14', '05/VBHN-BCT' → None (not a type
    code)."""
    t = unicodedata.normalize("NFC", so_ky_hieu or "").upper()
    m = re.search(r"/\s*(?:\d{4}\s*/\s*)?([A-ZĐ]+)(\d*)", t)
    if not m:
        return None
    code = m.group(1)
    if m.group(2):
        # letters run straight into digits: an issuing unit's code ("PA05",
        # "PV01" of Công an units, "QH14"), never a type code — the type
        # code stands alone before "-" ("QĐ-", "BC-")
        return None if code in doctypes.NON_TYPE_SYMBOLS else "cong_van"
    if code in _SYMBOL_TYPES:
        return _SYMBOL_TYPES[code]
    if code in _SYMBOL_TYPES_FOLDED:
        return _SYMBOL_TYPES_FOLDED[code]
    if code in doctypes.NON_TYPE_SYMBOLS:
        return None
    return "cong_van"


def detect_type(trich_text: str, fallback_scan: str,
                so_ky_hieu: str = "") -> str:
    """Loại văn bản: the type heading that opens the trích yếu wins
    ("BÁO CÁO / Kết quả thực hiện Nghị quyết…" is a báo cáo, not a nghị
    quyết), then the ký hiệu in the số, then "V/v" (công văn)."""
    lines = [ln for ln in (trich_text or "").splitlines() if ln.strip()]
    if lines:
        t = _type_from_line(lines[0])
        if t:
            return t
        if _TRICH_VV_RE.match(fold(lines[0])):
            return "cong_van"
    # "Nghị quyết số: 162/2024/QH15" names its type
    t = _type_from_line(so_ky_hieu) if so_ky_hieu else None
    if t:
        return t
    t = _type_from_symbol(so_ky_hieu) if so_ky_hieu else None
    if t:
        return t
    for ln in (fallback_scan or "").splitlines()[:60]:
        f = fold(ln)
        if len(f) <= 40:
            t = _type_from_line(ln)
            if t:
                return t
    return "unknown"


# a collective-body name that continues on the next line with its locality:
# "ỦY BAN NHÂN DÂN / TỈNH THỪA THIÊN HUẾ", "HỘI ĐỒNG NHÂN DÂN / XÃ ..."
_GENERIC_ORG_RE = re.compile(
    r"^(UY\s+BAN\s+NHAN\s+DAN|HOI\s+DONG\s+NHAN\s+DAN|"
    r"UY\s+BAN\s+MAT\s+TRAN\s+TO\s+QUOC(\s+VIET\s+NAM)?|BAN\s+CHAP\s+HANH|"
    r"BAN\s+THUONG\s+VU|DANG\s+UY|DANG\s+BO)$"
)


def _ban_hanh_line_count(left_lines: list) -> int:
    """How many trailing agency lines form the cơ quan ban hành.

    NĐ30: chủ quản is regular weight, ban hành bold — so the trailing run of
    lines sharing the last line's weight is ban hành, as long as the lines
    above differ.  When every line has the same weight, fall back to wrapping
    rules: a generic collective name joins the locality line below it."""
    n = len(left_lines)
    last_bold = bool(left_lines[-1][1].para.bold)
    k = 1
    while k < n and bool(left_lines[-k - 1][1].para.bold) == last_bold:
        k += 1
    if k == n:
        # uniform weight: ban hành = last line, plus the lines it continues
        # — a generic collective name ("ỦY BAN NHÂN DÂN") or a line broken
        # after a comma / connector ("BAN QUẢN LÝ KHU KINH TẾ," + "CÔNG
        # NGHIỆP TỈNH")
        k = 1
        while k < n:
            prev = left_lines[-k - 1][1].text.strip()
            fp = fold(prev)
            if _GENERIC_ORG_RE.match(fp) or re.search(r"[,\-–&]$|\s(VA|CUA)$", fp):
                k += 1
                continue
            break
    # a block opening with "VÀ ..." continues the line above it
    # ("SỞ TÀI NGUYÊN" / "VÀ MÔI TRƯỜNG")
    while k < n and re.match(r"(VA|CUA)\s", fold(left_lines[-k][1].text.strip())):
        k += 1
    return k


def _is_header_line(f: str) -> bool:
    return bool(_QUOC_HIEU_RE.match(f) or _TIEU_NGU_RE.match(f)
                or _DIA_DANH_RE.search(f) or _SO_KY_HIEU_RE.match(f))


def _is_heading(el: Element, f: str) -> bool:
    """A full-width line that opens the trích yếu block."""
    p = el.para
    if el.zone != "full" or (p.in_table and p.table_layout == "columns"):
        return False
    if _QUOC_HIEU_RE.match(f) or _TIEU_NGU_RE.match(f) or _DIA_DANH_RE.search(f):
        return False
    if _TRICH_VV_RE.match(f):
        return True
    if p.alignment != "center":
        return False
    return bool(p.bold or p.text_is_upper or p.caps
                or _type_from_line(el.text))


def _header_end(els: list) -> int:
    """Index (in els) one past the two-column header block.

    The header is the leading run of side-by-side content: layout-table cells
    and tab-split lines, plus full-width lines carrying a header pattern
    (quốc hiệu, tiêu ngữ, số ký hiệu, địa danh) or an agency name.  It may be
    spread over several tables (cơ quan/quốc hiệu in one, số/ngày in the next)
    or mix a table with tab-split lines.  Full-width lines that are neither
    (an agency name longer than the right column, "V/v", "KHẨN") are kept
    pending and join the header only if header content follows them."""
    seen_anchor = False
    end = 0
    pending = 0
    limit = min(len(els), 40)
    for i in range(limit):
        el = els[i]
        f = fold(el.text)
        side = el.zone in ("left", "right") or el.para.zone == "split"
        if _is_header_line(f) or _MAT_RE.match(f) or side:
            seen_anchor = seen_anchor or not side or _is_header_line(f)
            end = i + 1
            pending = 0
            continue
        if _CHI_DAN_RE.match(f):
            # ô 11 sits by the header (with độ mật / độ khẩn)
            end = i + 1
            pending = 0
            continue
        if _is_heading(el, f) and not _TRICH_VV_RE.match(f):
            break
        if _TRICH_VV_RE.match(f) or _KHAN_RE.match(f):
            # V/v (công văn) / độ khẩn under the số ký hiệu
            end = i + 1
            pending = 0
            continue
        if _looks_agency(el.text, f) and pending < 3:
            pending += 1
            if not seen_anchor:
                end = i + 1
            continue
        break
    return end if seen_anchor else 0


def segment(layout: DocLayout) -> Segmented:
    seg = Segmented()
    seg.para_table = {p.index: p.table_index for p in layout.paragraphs}
    els = _elements(layout)
    if not els:
        return seg

    header_el_end = _header_end(els)
    header_els = els[:header_el_end]

    # ---- header: left column (cơ quan, số ký hiệu), right column ---------
    left_lines = []
    trich_opened = False
    for hi, el in enumerate(header_els):
        f = fold(el.text)
        if not f:
            continue
        if _CHI_DAN_RE.match(f):
            _add(seg, "chi_dan_luu_hanh", el.para.index, el.text, el.zone,
                 pos=hi, sub=el.sub)
            continue
        if _MAT_RE.match(f):
            _add(seg, "do_mat", el.para.index, el.text, el.zone, pos=hi, sub=el.sub)
            continue
        if _KHAN_RE.match(f) and el.zone != "full" and len(f) < 30:
            _add(seg, "do_khan", el.para.index, el.text, el.zone, pos=hi, sub=el.sub)
            continue
        # content patterns win over zone: quốc hiệu / tiêu ngữ / địa danh /
        # số ký hiệu are recognised even in flat (non-table) layouts.
        if _TIEU_NGU_RE.match(f):
            _add(seg, "tieu_ngu", el.para.index, el.text, el.zone, pos=hi, sub=el.sub)
        elif _QUOC_HIEU_RE.match(f):
            _add(seg, "quoc_hieu", el.para.index, el.text, el.zone, pos=hi, sub=el.sub)
        elif _DIA_DANH_RE.search(f):
            _add(seg, "dia_danh_ngay_thang", el.para.index, el.text,
                 el.zone, pos=hi, sub=el.sub)
        elif _SO_KY_HIEU_RE.match(f) and el.zone != "right":
            _add(seg, "so_ky_hieu", el.para.index, el.text, "left", pos=hi, sub=el.sub)
        elif _TRICH_VV_RE.match(f):
            # "V/v ..." trích yếu embedded in the header (usually the left
            # cell of a header table, under the số ký hiệu)
            _add(seg, "trich_yeu", el.para.index, el.text, el.zone, pos=hi, sub=el.sub)
            trich_opened = True
        elif el.zone == "left" or (el.zone == "full" and p_leftish(el.para)) \
                or (el.zone == "full" and not seg.comp("quoc_hieu")["found"]):
            if trich_opened:
                # wrapped continuation of an embedded trích yếu
                _add(seg, "trich_yeu", el.para.index, el.text, "left", pos=hi, sub=el.sub)
            elif seg.comp("so_ky_hieu")["found"] and not _looks_agency(el.text, f):
                _add(seg, "header_left_other", el.para.index, el.text,
                     "left", pos=hi, sub=el.sub)
            else:
                left_lines.append((hi, el))
        elif el.zone == "right" or (el.zone == "full" and p_rightish(el.para)):
            _add(seg, "header_right_other", el.para.index, el.text,
                 "right", pos=hi, sub=el.sub)
        else:
            _add(seg, "header_other", el.para.index, el.text, el.zone, pos=hi, sub=el.sub)

    # split agency lines into chủ quản (upper lines) vs ban hành (last line,
    # usually bold/underlined) per NĐ30.
    if left_lines:
        n_ban_hanh = _ban_hanh_line_count(left_lines)
        for hi, el in left_lines[:-n_ban_hanh]:
            _add(seg, "co_quan_chu_quan", el.para.index, el.text, "left",
                 pos=hi, sub=el.sub)
        for hi, el in left_lines[-n_ban_hanh:]:
            _add(seg, "co_quan_ban_hanh", el.para.index, el.text, "left",
                 pos=hi, sub=el.sub)

    # ---- trích yếu block (full-width heading after the header) -----------
    trich_start = None
    if not trich_opened:
        scan_to = min(len(els), header_el_end + 8)
        for i in range(header_el_end, scan_to):
            if _CHI_DAN_RE.match(fold(els[i].text)):
                continue
            if _is_heading(els[i], fold(els[i].text)):
                trich_start = i
                break
            if els[i].zone == "full" and len(els[i].text) > 120:
                break  # a body paragraph reached — no heading
    body_start = header_el_end
    if trich_start is not None:
        _add(seg, "trich_yeu", els[trich_start].para.index,
             els[trich_start].text, "full", pos=trich_start, sub=els[trich_start].sub)
        j = trich_start + 1
        while j < len(els):
            el = els[j]
            p = el.para
            f = fold(el.text)
            if el.zone != "full" or p.alignment != "center" \
                    or _CAN_CU_RE.match(f) or _KINH_GUI_RE.match(f) \
                    or _CHI_DAN_RE.match(f):
                break
            if _THAM_QUYEN_RE.match(f) and el.text.strip() and (
                    p.text_is_upper or p.caps):
                # "CHỦ TỊCH ỦY BAN NHÂN DÂN TỈNH ..." of a QĐ — thẩm quyền
                _add(seg, "tham_quyen_ban_hanh", p.index, el.text, "full",
                     pos=j, sub=el.sub)
                j += 1
                continue
            if seg.comp("tham_quyen_ban_hanh")["found"]:
                if p.text_is_upper or p.caps:
                    # wrapped / second line: "KHÓA VIII, KỲ HỌP THỨ 10"
                    _add(seg, "tham_quyen_ban_hanh", p.index, el.text,
                         "full", pos=j, sub=el.sub)
                    j += 1
                    continue
                break
            _add(seg, "trich_yeu", p.index, el.text, "full", pos=j, sub=el.sub)
            j += 1
        body_start = j

    seg.detected_type = detect_type(
        seg.comp("trich_yeu")["text"],
        "\n".join(e.text for e in els[:60]),
        seg.comp("so_ky_hieu")["text"],
    )

    # ---- tail: nơi nhận + signature --------------------------------------
    # The bottom block is two-column again (signature on the right, "Nơi
    # nhận" on the left) — frequently one borderless table, so right-zone and
    # left-zone elements interleave. Split by zone, not by position.
    # a second header (quốc hiệu) after the body opens a document attached
    # to this one (the Quy chế a Quyết định issues): the main document's
    # tail ends there, the rest is its phụ lục
    attach = _attached_start(els, header_el_end, body_start)
    tail = els[body_start:attach]

    def is_right(el: Element) -> bool:
        return el.zone == "right" or (el.zone == "full" and p_rightish(el.para))

    noi_nhan_idx = None
    for k, el in enumerate(tail):
        if _NOI_NHAN_RE.match(fold(el.text)):
            noi_nhan_idx = k  # last one wins: phụ lục may quote "Nơi nhận"
    noi_nhan_end = len(tail)
    if noi_nhan_idx is not None:
        for k in range(noi_nhan_idx + 1, len(tail)):
            el = tail[k]
            f = fold(el.text)
            if _PHU_LUC_RE.match(f) or (
                    el.zone == "full" and el.para.alignment == "center"
                    and (el.para.bold or el.para.text_is_upper)):
                noi_nhan_end = k
                break

    # Signature = the right-zone run after the last body paragraph (ignoring
    # the nơi nhận list); it opens at the first authority/uppercase line.
    last_body = -1
    for k, el in enumerate(tail[:noi_nhan_end]):
        if noi_nhan_idx is not None and k >= noi_nhan_idx:
            break
        if not is_right(el) and el.zone != "left":
            last_body = k
    sig_open_idx = None
    cands = [k for k in range(last_body + 1, noi_nhan_end) if is_right(tail[k])]
    for k in cands:
        f = fold(tail[k].text)
        if _SIG_OPEN_RE.match(f) or tail[k].para.text_is_upper:
            sig_open_idx = k
            break
    if sig_open_idx is None and cands:
        sig_open_idx = cands[0]

    def is_dia_chi(f: str) -> bool:
        return bool(_DIA_CHI_START_RE.match(f)) or len(_DIA_CHI_ANY_RE.findall(f)) >= 2

    # ô 12/13 sit under nơi nhận (or under the signature when there is no
    # nơi nhận): only lines past that point are considered
    bottom_from = noi_nhan_idx if noi_nhan_idx is not None else sig_open_idx
    ky_hieu_k = None
    if noi_nhan_idx is not None:
        for k in range(noi_nhan_end - 1, noi_nhan_idx, -1):
            el = tail[k]
            if is_right(el) or is_dia_chi(fold(el.text)):
                continue
            t = el.text.strip()
            if len(t) <= 12 and _KY_HIEU_ST_RE.match(t):
                ky_hieu_k = k
            break  # only the last left line of the nơi nhận block

    sig_els = []
    body_els = []
    for k, el in enumerate(tail):
        f = fold(el.text)
        p = el.para
        pos = body_start + k
        in_noi_nhan = noi_nhan_idx is not None and noi_nhan_idx <= k < noi_nhan_end
        in_sig = sig_open_idx is not None and sig_open_idx <= k < noi_nhan_end
        if in_sig and is_right(el):
            sig_els.append((pos, el))
            continue
        if _CHI_DAN_RE.match(f) and not seg.comp("noi_dung")["found"]:
            # ô 11 belongs to the head block; in the body it is body text
            _add(seg, "chi_dan_luu_hanh", p.index, el.text, el.zone, pos=pos, sub=el.sub)
            continue
        if (bottom_from is not None and bottom_from < k < noi_nhan_end
                and is_dia_chi(f)):
            _add(seg, "dia_chi_co_quan", p.index, el.text, el.zone, pos=pos, sub=el.sub)
            continue
        if k == ky_hieu_k:
            _add(seg, "ky_hieu_soan_thao", p.index, el.text, "left", pos=pos, sub=el.sub)
            continue
        if in_noi_nhan and not is_right(el):
            _add(seg, "noi_nhan", p.index, el.text, "left", pos=pos, sub=el.sub)
            continue
        if _PHU_LUC_RE.match(f) and k >= noi_nhan_end - 1:
            _add(seg, "phu_luc", p.index, el.text, el.zone, pos=pos, sub=el.sub)
            continue
        if _KINH_GUI_RE.match(f):
            _add(seg, "kinh_gui", p.index, el.text, el.zone, pos=pos, sub=el.sub)
            continue
        if _CAN_CU_RE.match(f) and not seg.comp("noi_dung")["found"]:
            _add(seg, "can_cu", p.index, el.text, "full", pos=pos, sub=el.sub)
            continue
        if _can_cu_more(seg, el, f):
            # "Nhằm …;", "Theo đề nghị của …;" continue the căn cứ block
            _add(seg, "can_cu", p.index, el.text, "full", pos=pos, sub=el.sub)
            continue
        if k >= noi_nhan_end:
            _add(seg, "phu_luc", p.index, el.text, el.zone, pos=pos, sub=el.sub)
            continue
        body_els.append((pos, el))
        seg.body_para_indices.append(p.index)
        _add(seg, "noi_dung", p.index, el.text, el.zone, pos=pos, sub=el.sub)

    # chuc_danh = authority lines; nguoi_ky = the final right-zone line when
    # it reads as a person's name (not an all-caps title, not "(Đã ký)").
    if sig_els:
        last_pos, last_el = sig_els[-1]
        lf = fold(last_el.text)
        name_like = (not last_el.para.text_is_upper
                     and not _SIG_OPEN_RE.match(lf)
                     and not re.match(r"^\(?\s*(DA\s+KY|KY\s+SO|DAU)", lf))
        head = sig_els[:-1] if name_like and len(sig_els) > 1 else sig_els
        for pos, el in head:
            if re.match(r"^\(\s*(DA\s+KY|KY|DAU)", fold(el.text)):
                continue  # "(Đã ký)" / "(Ký, đóng dấu)" placeholders
            _add(seg, "chuc_danh", el.para.index, el.text, "right", pos=pos, sub=el.sub)
        if name_like and len(sig_els) > 1:
            _add(seg, "nguoi_ky", last_el.para.index, last_el.text, "right",
                 pos=last_pos, sub=last_el.sub)

    for k, el in enumerate(els[attach:]):
        _add(seg, "phu_luc", el.para.index, el.text, el.zone, pos=attach + k,
             sub=el.sub)

    # mark the signature block as a whole once chuc_danh/nguoi_ky seen
    add_signature(seg)

    # ô 13 printed in the page-1 footer: a component without body paragraphs
    if not seg.comp("dia_chi_co_quan")["found"]:
        lines = [(p.index, p.text.strip()) for p in layout.footer_paras
                 if p.text.strip() and is_dia_chi(fold(p.text.strip()))]
        if lines:
            seg.components["dia_chi_co_quan"] = {
                "found": True, "paras": [], "zones": [],
                "footer_paras": [i for i, _ in lines],
                "texts": [t for _, t in lines],
                "text": "\n".join(t for _, t in lines),
                "zone": "footer", "pos": None,
            }

    return seg


def add_signature(seg: Segmented):
    """The whole signature block once chức danh / người ký are known (their
    paragraphs, and the lines of a paragraph read line by line)."""
    cd, nk = seg.comp("chuc_danh"), seg.comp("nguoi_ky")
    if not (cd["found"] or nk["found"]):
        return
    paras = cd["paras"] + nk["paras"]
    subs = {}
    for c in (cd, nk):
        for para, ks in (c.get("subs") or {}).items():
            for k in ks:
                if k not in subs.setdefault(para, []):
                    subs[para].append(k)
    for ks in subs.values():
        ks.sort()
    seg.components["signature"] = {
        "found": True,
        "paras": sorted(set(paras)),
        "zones": ["right"] * len(set(paras)),
        "texts": [],
        "text": "\n".join(filter(None, [cd["text"], nk["text"]])),
        "zone": "right",
        "pos": cd.get("pos"),
        "subs": subs,
    }


def quoc_hieu_elements(els: list) -> list:
    """Indexes of the elements holding the quốc hiệu alone."""
    return [i for i, el in enumerate(els) if _QUOC_HIEU_LINE_RE.match(fold(el.text))]


def _attached_start(els: list, h_end: int, body_start: int) -> int:
    """Element where an attached document begins — the header block (same
    paragraph or same table) of the second quốc hiệu, when the first one
    heads the document and the second comes after the body — or len(els).
    Forms attached after it (more quốc hiệu) stay in the attached part."""
    qh = quoc_hieu_elements(els)
    if len(qh) < 2 or qh[0] >= h_end or qh[1] <= body_start:
        return len(els)

    def same_block(x: Element, y: Element) -> bool:
        if x.para.index == y.para.index:
            return True
        return (x.para.table_index is not None and y.para.table_index is not None
                and x.para.table_index == y.para.table_index)

    a = qh[1]
    while a > body_start and same_block(els[a - 1], els[a]):
        a -= 1
    return a


# the resolution line ("QUYẾT ĐỊNH:", "Thủ tướng Chính phủ ban hành Quyết
# định …") and the first article / chapter end the căn cứ block
_RESOLUTION_RE = re.compile(
    r"^(QUYET\s+DINH|QUYET\s+NGHI|NGHI\s+QUYET|CHI\s+THI)\s*:|"
    r"\bBAN\s+HANH\s+(QUYET\s+DINH|NGHI\s+DINH|THONG\s+TU|NGHI\s+QUYET|CHI\s+THI|LUAT|PHAP\s+LENH)\b")
_ARTICLE_RE = re.compile(r"^(DIEU\s+\d|CHUONG\s+[IVXLC\d]|PHAN\s+[IVXLC\d]|MUC\s+\d)")


def _can_cu_more(seg: Segmented, el: Element, f: str) -> bool:
    """An italic line ending in ";" or "." right after the căn cứ lines
    ("Nhằm triển khai …;", "Theo đề nghị của …;") still belongs to the căn
    cứ block — until the resolution line, the first Điều, or a non-italic
    paragraph."""
    if not seg.comp("can_cu")["found"] or seg.comp("noi_dung")["found"] \
            or not el.para.italic:
        return False
    t = el.text.strip()
    if not (t.endswith(";") or t.endswith(".")):
        return False
    return not _RESOLUTION_RE.search(f) and not _ARTICLE_RE.match(f)


def p_leftish(p: Para) -> bool:
    return p.alignment in ("left", "justify") and (p.indent_left_mm or 0) < 40


def p_rightish(p: Para) -> bool:
    if p.alignment == "right":
        return True
    if p.alignment == "center" and (p.indent_left_mm or 0) > 60:
        return True
    return False
