"""Document-type registry — mirror of internal/vietnamese_legal/doctype.go.

Keep the two in sync: the first 29 entries are the văn bản hành chính of
Điều 7 Nghị định 30/2020/NĐ-CP (decree order), with the ký hiệu of Phụ lục
III (công văn and thư công have none); the rest are the văn bản quy phạm
pháp luật types also recognised by the Go side.

Ký hiệu are case-insensitive but Đ is significant: HĐ (hợp đồng) ≠ HD
(hướng dẫn), ĐA (đề án) ≠ DA (dự án).
"""
from __future__ import annotations

from typing import NamedTuple, Optional


class DocType(NamedTuple):
    slug: str
    name: str            # display name, e.g. "Kế hoạch"
    group: str           # "hanh_chinh" (NĐ30 Điều 7) | "qppl"
    symbols: tuple       # ký hiệu codes in số ký hiệu


HANH_CHINH = "hanh_chinh"
QPPL = "qppl"

DOC_TYPES = (
    DocType("nghi_quyet", "Nghị quyết", HANH_CHINH, ("NQ",)),
    DocType("quyet_dinh", "Quyết định", HANH_CHINH, ("QĐ",)),
    DocType("chi_thi", "Chỉ thị", HANH_CHINH, ("CT",)),
    DocType("quy_che", "Quy chế", HANH_CHINH, ("QC",)),
    DocType("quy_dinh", "Quy định", HANH_CHINH, ("QyĐ",)),
    DocType("thong_cao", "Thông cáo", HANH_CHINH, ("TC",)),
    DocType("thong_bao", "Thông báo", HANH_CHINH, ("TB",)),
    DocType("huong_dan", "Hướng dẫn", HANH_CHINH, ("HD",)),
    DocType("chuong_trinh", "Chương trình", HANH_CHINH, ("CTr",)),
    DocType("ke_hoach", "Kế hoạch", HANH_CHINH, ("KH",)),
    DocType("phuong_an", "Phương án", HANH_CHINH, ("PA",)),
    DocType("de_an", "Đề án", HANH_CHINH, ("ĐA",)),
    DocType("du_an", "Dự án", HANH_CHINH, ("DA",)),
    DocType("bao_cao", "Báo cáo", HANH_CHINH, ("BC",)),
    DocType("bien_ban", "Biên bản", HANH_CHINH, ("BB",)),
    DocType("to_trinh", "Tờ trình", HANH_CHINH, ("TTr",)),
    DocType("hop_dong", "Hợp đồng", HANH_CHINH, ("HĐ",)),
    DocType("cong_van", "Công văn", HANH_CHINH, ()),
    DocType("cong_dien", "Công điện", HANH_CHINH, ("CĐ",)),
    DocType("ban_ghi_nho", "Bản ghi nhớ", HANH_CHINH, ("GN",)),
    DocType("ban_thoa_thuan", "Bản thỏa thuận", HANH_CHINH, ("TTh",)),
    DocType("giay_uy_quyen", "Giấy ủy quyền", HANH_CHINH, ("GUQ",)),
    DocType("giay_moi", "Giấy mời", HANH_CHINH, ("GM",)),
    DocType("giay_gioi_thieu", "Giấy giới thiệu", HANH_CHINH, ("GGT",)),
    DocType("giay_nghi_phep", "Giấy nghỉ phép", HANH_CHINH, ("NP",)),
    DocType("phieu_gui", "Phiếu gửi", HANH_CHINH, ("PG",)),
    DocType("phieu_chuyen", "Phiếu chuyển", HANH_CHINH, ("PC",)),
    DocType("phieu_bao", "Phiếu báo", HANH_CHINH, ("PB",)),
    DocType("thu_cong", "Thư công", HANH_CHINH, ()),

    DocType("hien_phap", "Hiến pháp", QPPL, ()),
    DocType("bo_luat", "Bộ luật", QPPL, ()),
    DocType("luat", "Luật", QPPL, ()),
    DocType("phap_lenh", "Pháp lệnh", QPPL, ()),
    DocType("nghi_dinh", "Nghị định", QPPL, ("NĐ",)),
    DocType("thong_tu_lien_tich", "Thông tư liên tịch", QPPL, ("TTLT",)),
    DocType("thong_tu", "Thông tư", QPPL, ("TT",)),
)

ND30_TYPE_COUNT = 29
ND30_TYPES = tuple(d for d in DOC_TYPES if d.group == HANH_CHINH)
SLUGS = tuple(d.slug for d in DOC_TYPES)
BY_SLUG = {d.slug: d for d in DOC_TYPES}

# exact ký hiệu (uppercased, Đ kept) -> slug
SYMBOL_TYPES = {s.upper(): d.slug for d in DOC_TYPES for s in d.symbols}

# Ký hiệu that are not a type of their own ("QH14" Luật/NQ of Quốc hội,
# "L-CTN" Lệnh, "VBHN" văn bản hợp nhất) — never read as công văn.
NON_TYPE_SYMBOLS = ("QH", "UBTVQH", "CTN", "L", "VBHN")


def name_of(slug: str) -> Optional[str]:
    d = BY_SLUG.get(slug)
    return d.name if d else None


assert len(ND30_TYPES) == ND30_TYPE_COUNT
assert DOC_TYPES[:ND30_TYPE_COUNT] == ND30_TYPES
