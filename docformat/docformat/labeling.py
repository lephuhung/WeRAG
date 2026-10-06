"""LLM-assisted component labeling.

The positional heuristic in ``segment.py`` breaks on unusual layouts, and a
mislabeled paragraph makes every downstream size/font check wrong.  Here an
LLM (or the calling agent) assigns each text unit of the document to an NĐ30
component; layout measurement and rule evaluation stay deterministic.

Flow::

    units = build_units(layout, heuristic_seg)      # compact, id-addressed
    task  = labeling_task(units)                    # prompt + schema
    reply = <LLM>(task)                             # {"document_type", "labels"}
    seg   = apply_labels(layout, reply, heuristic_seg)

A *unit* is one paragraph, or one side of a tab-split paragraph (id ``"12L"``
/ ``"12R"``).  Units the reply leaves out keep the heuristic label, so a
partial answer degrades gracefully instead of dropping components.
"""

from __future__ import annotations

import json
import re
from typing import Optional

from . import doctypes
from .layout import DocLayout
from .segment import Segmented, _add, _elements, detect_type, p_rightish

# NĐ30/2020 Phụ lục I components, in reading order.  Descriptions are written
# for the model: what the part is and where it normally sits.
COMPONENTS = [
    ("quoc_hieu", "Quốc hiệu: 'CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM', đầu trang, cột phải."),
    ("tieu_ngu", "Tiêu ngữ: 'Độc lập - Tự do - Hạnh phúc', ngay dưới quốc hiệu."),
    ("co_quan_chu_quan", "Tên cơ quan chủ quản trực tiếp — một cơ quan KHÁC, cấp trên của cơ quan ban hành — cột trái đầu trang, phía trên tên cơ quan ban hành. VD: 'UBND TỈNH THỪA THIÊN HUẾ' phía trên 'SỞ Y TẾ'. Văn bản của UBND, HĐND, Bộ, cơ quan trung ương thường KHÔNG có cơ quan chủ quản."),
    ("co_quan_ban_hanh", "Tên cơ quan, tổ chức ban hành văn bản (in đậm), cột trái đầu trang. Tên dài xuống dòng thì MỌI dòng đều là co_quan_ban_hanh: 'ỦY BAN NHÂN DÂN' + 'THÀNH PHỐ HUẾ' là MỘT tên (UBND thành phố Huế), không phải chủ quản + ban hành; tương tự 'HỘI ĐỒNG NHÂN DÂN' + 'TỈNH ...'."),
    ("so_ky_hieu", "Số, ký hiệu văn bản: 'Số: 12/QĐ-UBND', cột trái dưới tên cơ quan."),
    ("dia_danh_ngay_thang", "Địa danh và thời gian ban hành: 'Huế, ngày 05 tháng 10 năm 2026', cột phải dưới tiêu ngữ."),
    ("do_mat", "Dấu chỉ độ mật (MẬT, TỐI MẬT, TUYỆT MẬT)."),
    ("do_khan", "Dấu chỉ mức độ khẩn (KHẨN, THƯỢNG KHẨN, HỎA TỐC)."),
    ("trich_yeu", "Tên loại văn bản và trích yếu nội dung: dòng tên loại ('QUYẾT ĐỊNH', 'BÁO CÁO', 'KẾ HOẠCH'...) và (các) dòng trích yếu ngay dưới; với công văn là dòng 'V/v ...' dưới số ký hiệu (kể cả dòng xuống hàng)."),
    ("tham_quyen_ban_hanh", "Thẩm quyền ban hành của QĐ/NQ, căn giữa sau trích yếu, VD 'CHỦ TỊCH ỦY BAN NHÂN DÂN TỈNH ...', 'GIÁM ĐỐC SỞ Y TẾ'."),
    ("can_cu", "Các dòng 'Căn cứ ...' / 'Xét đề nghị ...' / 'Theo đề nghị ...' trước phần nội dung chính."),
    ("kinh_gui", "Dòng 'Kính gửi: ...' và danh sách nơi gửi đi kèm (công văn, tờ trình)."),
    ("noi_dung", "Nội dung văn bản: toàn bộ thân văn bản, kể cả đề mục (I., 1., Điều 1.), dòng 'QUYẾT ĐỊNH:' giữa văn bản, bảng số liệu, câu kết 'Trên đây là ...'."),
    ("chuc_danh", "Quyền hạn, chức vụ người ký trong khối ký (thường ở cột phải cuối văn bản, có khi chỉ căn giữa/thụt lề): 'TM. ỦY BAN NHÂN DÂN', 'KT. GIÁM ĐỐC', 'PHÓ GIÁM ĐỐC', 'CHỦ TỊCH'. Biên bản, văn bản liên tịch có NHIỀU người ký cạnh nhau (VD trái 'THƯ KÝ', phải 'CHỦ TRÌ') — mọi khối ký đều là chuc_danh/nguoi_ky, không phải noi_dung."),
    ("nguoi_ky", "Họ và tên người ký, dòng cuối của mỗi khối ký: 'Nguyễn Văn A'. Không gồm '(Đã ký)', '(Ký, đóng dấu)'."),
    ("noi_nhan", "Nơi nhận: dòng 'Nơi nhận:' và các dòng '- ...;' bên dưới, cột trái cuối văn bản."),
    ("phu_luc", "Phụ lục / văn bản kèm theo sau phần nơi nhận."),
    ("khac", "Không thuộc thành phần nào ở trên: dòng kẻ, ghi chú '(Đã ký)', dấu, chú thích, header/footer lạc vào."),
]
COMPONENT_KEYS = [k for k, _ in COMPONENTS]

# the 29 NĐ30 types + the QPPL types (doctypes.py), then "unknown"
DOC_TYPES = list(doctypes.SLUGS) + ["unknown"]

# units kept verbatim at each end of the document; the middle of a long body
# is elided (labelled noi_dung) unless the heuristic flags something there
_KEEP_HEAD = 45
_KEEP_TAIL = 45
_TEXT_MAX = 160


def build_units(layout: DocLayout, heuristic: Optional[Segmented] = None) -> list:
    """Id-addressed text units with the layout cues the model needs."""
    units = []
    split_seen: dict = {}
    for el in _elements(layout):
        p = el.para
        if p.zone == "split":
            side = "L" if el.zone == "left" else "R"
            uid = "%d%s" % (p.index, side)
            split_seen[p.index] = True
        else:
            uid = str(p.index)
        fmt = {"size_pt": p.size_pt, "bold": p.bold, "italic": p.italic}
        if p.zone == "split":
            fmt.update(p.left_props if el.zone == "left" else p.right_props)
        u = {
            "id": uid,
            "para": p.index,
            "text": el.text if len(el.text) <= _TEXT_MAX
            else el.text[:_TEXT_MAX] + "…",
            "zone": el.zone,
            "align": p.alignment,
            "size": fmt.get("size_pt"),
            "b": bool(fmt.get("bold")),
            "i": bool(fmt.get("italic")),
            "upper": p.text_is_upper if p.zone != "split"
            else _is_upper(el.text),
        }
        if p.table_index is not None:
            u["tbl"] = "%s:%s/%s:%s" % (p.table_index, p.table_layout or "",
                                         p.row, p.col)
        if heuristic is not None:
            hint = _heuristic_label(heuristic, p.index, el.zone)
            if hint:
                u["hint"] = hint
        units.append(u)
    return units


def _is_upper(text: str) -> bool:
    letters = [c for c in text if c.isalpha()]
    return len(letters) >= 2 and not any(c.islower() for c in letters)


def _heuristic_label(seg: Segmented, para_idx: int, zone: str) -> Optional[str]:
    """Label the heuristic gave this unit (zone-aware for split paras)."""
    found = None
    for key, comp in seg.components.items():
        if key == "signature":
            continue
        paras = comp.get("paras", [])
        zones = comp.get("zones", [])
        for i, idx in enumerate(paras):
            if idx != para_idx:
                continue
            z = zones[i] if i < len(zones) else None
            if z == zone or z is None or zone == "full":
                return key
            found = found or key
    return found


def _elide(units: list) -> tuple:
    """(units sent to the model, ids auto-labelled noi_dung)."""
    n = len(units)
    if n <= _KEEP_HEAD + _KEEP_TAIL + 10:
        return units, []
    sent, elided = [], []
    for i, u in enumerate(units):
        if i < _KEEP_HEAD or i >= n - _KEEP_TAIL \
                or u.get("hint") not in (None, "noi_dung"):
            sent.append(u)
        else:
            elided.append(u["id"])
    return sent, elided


SYSTEM_PROMPT = """Bạn là chuyên viên văn thư, nắm vững thể thức văn bản hành chính theo \
Nghị định 30/2020/NĐ-CP (Phụ lục I). Nhiệm vụ: gán MỖI đơn vị văn bản (unit) vào đúng \
một thành phần thể thức. Việc chấm cỡ chữ/font phụ thuộc hoàn toàn vào nhãn bạn gán, \
nên hãy gán theo VAI TRÒ của dòng trong văn bản, không theo định dạng của nó \
(văn bản cần kiểm tra có thể định dạng sai).

Gợi ý vị trí: đầu văn bản là khối 2 cột (trái: cơ quan, số ký hiệu, có thể cả 'V/v'; \
phải: quốc hiệu, tiêu ngữ, địa danh-ngày tháng). Sau đó là tên loại + trích yếu căn giữa, \
nội dung, rồi cuối văn bản là khối 2 cột (phải: chức danh + họ tên người ký; trái: nơi nhận).

Mỗi unit có: id, text, zone (left/right/full = cột trái/phải/toàn trang), align, size (pt), \
b (đậm), i (nghiêng), upper (in hoa), tbl ('bảng:loại/hàng:cột', loại 'columns' = bảng \
bố cục không viền, 'data' = bảng số liệu), hint (nhãn đoán bằng heuristic — có thể SAI).

document_type là TÊN LOẠI văn bản in ở dòng tên loại (QUYẾT ĐỊNH, BÁO CÁO, ...); \
văn bản không có dòng tên loại mà có 'V/v ...' dưới số ký hiệu là cong_van — \
đừng suy loại từ nội dung trích yếu ('V/v thông báo ...' vẫn là cong_van).

Trả về DUY NHẤT một JSON object:
{"document_type": "<một loại>", "labels": {"<id>": "<thành phần>", ...}, "notes": "<ngắn, tuỳ chọn>"}
- labels phải có mọi id được gửi; chỉ dùng tên thành phần trong danh sách.
- Các unit ở giữa thân văn bản đã được lược bớt sẽ tự gán noi_dung."""


def labeling_task(units: list, elided: Optional[list] = None) -> dict:
    """Everything a model (or the calling agent) needs to label the units."""
    return {
        "instructions": SYSTEM_PROMPT,
        "components": {k: d for k, d in COMPONENTS},
        "document_types": DOC_TYPES,
        "units": units,
        "elided_unit_count": len(elided or []),
        "response_format": {
            "document_type": "one of document_types",
            "labels": {"<unit id>": "<component key>"},
            "notes": "optional",
        },
    }


def task_messages(task: dict) -> list:
    """Chat messages for an OpenAI-compatible endpoint."""
    comp_lines = "\n".join("- %s: %s" % (k, d) for k, d in task["components"].items())
    unit_lines = "\n".join(json.dumps(u, ensure_ascii=False, separators=(",", ":"))
                           for u in task["units"])
    user = (
        "Thành phần:\n%s\n\nLoại văn bản: %s\n\n"
        "Units (%d, đã lược %d unit giữa thân văn bản):\n%s"
        % (comp_lines, ", ".join(task["document_types"]), len(task["units"]),
           task.get("elided_unit_count", 0), unit_lines)
    )
    return [{"role": "system", "content": task["instructions"]},
            {"role": "user", "content": user}]


def prepare(layout: DocLayout, heuristic: Segmented) -> tuple:
    """(task, all_units, elided_ids)."""
    units = build_units(layout, heuristic)
    sent, elided = _elide(units)
    return labeling_task(sent, elided), units, elided


def parse_reply(text: str) -> dict:
    """Extract the JSON object from a model reply (tolerates code fences and
    surrounding prose)."""
    if isinstance(text, dict):
        return text
    t = (text or "").strip()
    t = re.sub(r"^```(?:json)?\s*|\s*```$", "", t)
    try:
        return json.loads(t)
    except json.JSONDecodeError:
        pass
    start = t.find("{")
    end = t.rfind("}")
    if start >= 0 and end > start:
        return json.loads(t[start:end + 1])
    raise ValueError("no JSON object in model reply")


def apply_labels(layout: DocLayout, reply: dict, heuristic: Segmented,
                 units: Optional[list] = None, elided: Optional[list] = None
                 ) -> tuple:
    """Build a Segmented from model labels.

    Returns ``(segmented, diagnostics)``; diagnostics lists invalid labels,
    units the reply skipped (heuristic label kept) and every unit where the
    model disagreed with the heuristic — useful for auditing."""
    reply = parse_reply(reply)
    units = units if units is not None else build_units(layout, heuristic)
    elided = set(elided or [])
    raw = reply.get("labels") or {}
    if isinstance(raw, list):  # [{"id":..,"component":..}] variant
        raw = {str(x.get("id")): x.get("component") or x.get("label")
               for x in raw if isinstance(x, dict)}
    labels = {str(k): (v or "").strip() for k, v in raw.items()}

    diag = {"invalid": [], "missing": [], "disagreements": [],
            "unknown_ids": sorted(set(labels) - {u["id"] for u in units})}
    seg = Segmented()
    seg.para_table = {p.index: p.table_index for p in layout.paragraphs}

    for pos, u in enumerate(units):
        hint = u.get("hint")
        lab = labels.get(u["id"])
        if lab is None and u["id"] in elided:
            lab = "noi_dung"
        elif lab is None:
            diag["missing"].append(u["id"])
            lab = hint or "noi_dung"
        elif lab not in COMPONENT_KEYS:
            diag["invalid"].append({"id": u["id"], "label": lab})
            lab = hint or "noi_dung"
        if hint and lab != hint and u["id"] not in elided:
            diag["disagreements"].append({
                "id": u["id"], "text": u["text"][:80],
                "llm": lab, "heuristic": hint})
        if lab == "khac":
            continue
        _add(seg, lab, u["para"], _unit_text(layout, u),
             _effective_zone(layout, u), pos=pos)
        if lab == "noi_dung":
            seg.body_para_indices.append(u["para"])

    _add_signature(seg)

    # Loại văn bản is structural once the trích yếu is labelled: the type
    # heading line, "V/v" (công văn) or the ký hiệu in the số.  The model's
    # guess only fills in when those are absent — models tend to read the
    # type off the subject ("V/v thông báo ..." → thông báo).
    dt = (reply.get("document_type") or "").strip()
    structural = detect_type(seg.comp("trich_yeu")["text"], "",
                             seg.comp("so_ky_hieu")["text"])
    if structural != "unknown":
        seg.detected_type = structural
    elif dt in DOC_TYPES:
        seg.detected_type = dt
    else:
        seg.detected_type = "unknown"
    if dt and dt != seg.detected_type:
        diag["document_type"] = {"llm": dt, "used": seg.detected_type}
    return seg, diag


def _effective_zone(layout: DocLayout, u: dict) -> str:
    """Zone used by the rules: a full-width paragraph flush left (nơi nhận
    below a flat signature) sits in the left column, one pushed right by
    alignment/indent sits in the right column — same reading the heuristic
    segmenter applies."""
    if u["zone"] != "full":
        return u["zone"]
    p = layout.paragraphs[u["para"]]
    if p_rightish(p):
        return "right"
    if p.alignment == "left":
        return "left"
    return "full"


def _unit_text(layout: DocLayout, u: dict) -> str:
    p = layout.paragraphs[u["para"]]
    if u["id"].endswith("L"):
        return p.left_text
    if u["id"].endswith("R"):
        return p.right_text
    return p.text.strip()


def _add_signature(seg: Segmented):
    if seg.comp("chuc_danh")["found"] or seg.comp("nguoi_ky")["found"]:
        paras = seg.comp("chuc_danh")["paras"] + seg.comp("nguoi_ky")["paras"]
        seg.components["signature"] = {
            "found": True,
            "paras": sorted(set(paras)),
            "zones": ["right"] * len(set(paras)),
            "texts": [],
            "text": "\n".join(filter(None, [
                seg.comp("chuc_danh")["text"], seg.comp("nguoi_ky")["text"]])),
            "zone": "right",
            "pos": seg.comp("chuc_danh").get("pos"),
        }

