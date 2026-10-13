"""Scenario catalogue for the document-assistant compliance suite.

Each scenario opens its own session, loads the listed fixtures (role target =
editor tab, role source = chat upload) and sends the steps in order. Checks
are deterministic (tool calls, ops, card payloads, answer text); `judge`
adds an optional LLM-graded criterion.

Groups (the pipelines a real user goes through):
  content   – hỏi nội dung, tóm tắt văn bản ngắn
  long      – văn bản dài: hỏi phạm vi, tóm tắt mục, tìm số liệu
  spelling  – tìm lỗi chính tả (toàn văn bản / đoạn bôi đen)
  format    – kiểm tra thể thức NĐ30, sửa sau khi đồng ý
  rewrite   – bôi đen: viết lại, tóm tắt, gợi ý cách viết, giải thích
  lookup    – bôi đen tra cứu pháp lý / kho tri thức, chống trả lời theo trí nhớ
  diagram   – vẽ sơ đồ, hình minh họa
  multi     – nhiều văn bản: đối chiếu, gọi tên bằng @, nguồn chỉ đọc
  suggest   – gợi ý hoàn thiện, chèn đoạn, yêu cầu ngoài phạm vi
  web       – tìm kiếm web (phụ thuộc mạng)
"""
from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Callable, Dict, List, Optional, Sequence

import checks as C
from checks import FAIL, WARN
from werag_client import mention, selection

CLARIFY = "document_scope_clarification"
EDIT_TOOLS = ["rewrite_paragraphs", "insert_paragraphs", "apply_format_fixes"]

Ctx = Dict[str, Any]


@dataclass
class Step:
    # a string, or a function of ctx returning the text to send ("" skips the step)
    query: Any
    checks: List[C.Check]
    selection: Optional[Callable[[Ctx], Dict[str, Any]]] = None
    mentions: Optional[Callable[[Ctx], List[Dict[str, Any]]]] = None
    web_search: bool = False
    before: Optional[Callable[[Ctx], None]] = None
    judge: Optional[str] = None
    note: str = ""


@dataclass
class Scenario:
    id: str
    group: str
    title: str
    documents: List[tuple]  # (role, fixture file)
    steps: List[Step]
    requires: str = ""      # free text about infra needs (shown in the report)


# ---- helpers -------------------------------------------------------------
def doc(ctx: Ctx, name: str) -> Dict[str, Any]:
    return ctx["docs"][name]


def sel(name: str, text_fn: Callable[[Ctx], str], occurrence: int = 0, hint: bool = True):
    def f(ctx: Ctx) -> Dict[str, Any]:
        text = text_fn(ctx)
        return selection(doc(ctx, name), text, paragraph_hint=text if hint else "", occurrence=occurrence)
    return f


def men(*names: str):
    return lambda ctx: [mention(doc(ctx, n)) for n in names]


def body(name: str, i: int):
    return lambda ctx: ctx["manifest"][name]["body"][i]


def wait_format_check(name: str):
    def f(ctx: Ctx) -> None:
        d = doc(ctx, name)
        res = ctx["client"].wait_format_check(ctx["session_id"], d["id"], timeout=480)
        ctx.setdefault("format_checks", {})[name] = res
    return f


BASE = [C.no_turn_error(), C.answer_nonempty(), C.no_phantom_button(), C.no_tool_loop()]


def S(*extra: C.Check) -> List[C.Check]:
    return BASE + list(extra)


# ---- scenarios -----------------------------------------------------------

def _abbr_states(t):
    """abbreviation_resolution payloads of a turn (public state)."""
    return [(e.get("data") or {}).get("abbreviation") or {} for e in t.events
            if (e.get("response_type") or e.get("type")) == "abbreviation_resolution"]


def _typo_fixed(*words):
    """every word was finally read as a typo (the user confirmed it)."""
    def fn(t, ctx):
        states = _abbr_states(t)
        unknown = {str(u).lower() for st in states for u in st.get("unknown_terms") or []}
        ready = any(st.get("status") == "ready" for st in states)
        asked = [w for w in words if w in unknown]
        return ready and not asked, f"ready={ready} unknown {sorted(unknown)}"
    return C.custom(f"{list(words)} resolved as typos", fn)


def _unclear_card(word, guess):
    """the gate asks what the word is, with "you mistyped, did you mean <guess>?" among the options."""
    def fn(t, ctx):
        for tc in t.tool_calls:
            d = tc.data or {}
            if d.get("display_type") == "abbreviation_choice" and str(d.get("subject", "")).lower() == word:
                resends = [str(o.get("resend", "")) for o in d.get("options") or []]
                return f"{word} → {guess}" in resends, f"options {resends}"
        return False, "no abbreviation_choice card"
    return C.custom(f"asks about '{word}' offering '{guess}'", fn)


def _pick_typo(word):
    """the resend of the card's "you mistyped" option."""
    def q(ctx):
        for tc in ctx["turns"][-1].tool_calls:
            d = tc.data or {}
            if d.get("display_type") == "abbreviation_choice":
                for o in d.get("options") or []:
                    if str(o.get("resend", "")).startswith(word + " → ") and o.get("resend") != f"{word} → {word}":
                        return o["resend"]
        return ""
    return q


def _abbr_looked_up(*shorts):
    """the short forms still reached the dictionary as abbreviation terms."""
    def fn(t, ctx):
        got = {str(x.get("short_form", "")).lower() for st in _abbr_states(t) for x in st.get("terms") or []}
        return all(s in got for s in shorts), f"terms {sorted(got)}"
    return C.custom(f"abbreviations {list(shorts)} looked up", fn)

SCENARIOS: List[Scenario] = [
    # ------------------------------------------------------------------ content
    Scenario(
        id="content_meta", group="content", title="Hỏi số ký hiệu, người ký, ngày của văn bản đang mở",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "Văn bản này số bao nhiêu, do ai ký, ngày nào?",
            S(C.answer_contains_all(["12/QT-SNV", "Trần Minh Quang"]),
              C.answer_contains_any(["01 tháng 10 năm 2026", "01/10/2026", "1/10/2026"]),
              C.tool_not_called(EDIT_TOOLS), C.no_edit_ops(),
              C.tool_not_called(["search_knowledge", "web_search"], status=WARN)),
        )],
    ),
    Scenario(
        id="content_figures", group="content", title="Hỏi số liệu nằm trong văn bản",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "Năm 2025 Sở tiếp nhận bao nhiêu văn bản đến và tỷ lệ giải quyết đúng hạn là bao nhiêu?",
            S(C.answer_contains_any(["4.812", "4812"]), C.answer_contains_any(["99,5", "99.5"]),
              C.no_edit_ops(), C.tool_not_called(["search_knowledge", "web_search"], status=WARN)),
            note="Số liệu có sẵn trong văn bản đang mở; gọi search_knowledge là tra cứu thừa (bộ phân loại chỉ nhìn câu hỏi).",
        )],
    ),
    Scenario(
        id="content_summary_short", group="content", title="Tóm tắt văn bản ngắn (không hỏi lại phạm vi)",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "Tóm tắt văn bản này",
            S(C.no_display_type(CLARIFY), C.answer_contains_any(["5 bước", "năm bước", "Bước 1"]),
              C.tool_not_called(EDIT_TOOLS + ["draw_diagram", "generate_illustration"]), C.no_edit_ops(), C.no_offer_to_edit()),
            judge="Bản tóm tắt phải trung thành với văn bản (quy trình 5 bước tiếp nhận và xử lý văn bản đến của Sở Nội vụ, số 12/QT-SNV), không bịa thêm bước hay số liệu không có trong văn bản.",
        )],
    ),
    # --------------------------------------------------------------------- long
    Scenario(
        id="long_clarify", group="long", title="Văn bản dài + yêu cầu chung → hỏi lại phạm vi (thẻ chọn việc)",
        documents=[("target", "bao_cao_dai.docx")],
        steps=[Step(
            "Tóm tắt văn bản này",
            S(C.display_type(CLARIFY), C.answer_contains_any(["khá dài"]),
              C.tool_not_called(["read_document_outline", "find_in_documents"] + EDIT_TOOLS),
              C.custom("card offers summary task", lambda t, ctx: any(
                  any(task.get("key") == "summary" for task in (tc.data.get("tasks") or []))
                  for tc in t.tool_calls if tc.data.get("display_type") == CLARIFY))),
        )],
    ),
    Scenario(
        id="long_whole", group="long", title="Văn bản dài + 'toàn bộ' → tóm tắt, không hỏi lại",
        documents=[("target", "bao_cao_dai.docx")],
        steps=[Step(
            "Tóm tắt toàn bộ văn bản này",
            S(C.no_display_type(CLARIFY), C.no_edit_ops(),
              C.answer_mentions_groups([["văn thư"], ["lưu trữ"], ["3.624", "3624"], ["chuyển đổi số", "công nghệ thông tin"], ["phương hướng", "quý IV"]], 3, warn_at=2),
              C.no_offer_to_edit()),
            judge="Bản tóm tắt phải bám theo các mục của báo cáo 268/BC-SNV (chỉ đạo điều hành; kết quả văn thư; lưu trữ; CNTT; đào tạo và kinh phí; tồn tại; phương hướng; kiến nghị) và không bịa số liệu.",
        )],
    ),
    Scenario(
        id="long_section", group="long", title="Văn bản dài + hỏi một mục cụ thể → trả lời có số liệu",
        documents=[("target", "bao_cao_dai.docx")],
        steps=[Step(
            "Tóm tắt mục II (kết quả công tác văn thư) của văn bản",
            S(C.no_display_type(CLARIFY), C.answer_contains_any(["3.624", "3624"]), C.answer_contains_any(["2.118", "2118"]),
              C.no_edit_ops()),
        )],
    ),
    Scenario(
        id="long_find_figure", group="long", title="Văn bản dài: tìm số liệu ở cuối văn bản (ngoài cửa sổ ngữ cảnh)",
        documents=[("target", "bao_cao_dai.docx")],
        steps=[Step(
            "Tổng kinh phí thực hiện tập huấn nghiệp vụ trong báo cáo là bao nhiêu và có bao nhiêu lượt người tham dự?",
            S(C.no_display_type(CLARIFY), C.answer_contains_any(["2,5 tỷ", "2.5 tỷ"]), C.answer_contains_any(["320"]),
              C.any_tool_called(["find_in_documents", "read_document_outline"], status=WARN), C.no_edit_ops()),
            note="Số liệu nằm ở mục V, sau ngưỡng 16k ký tự được chèn vào prompt; mô hình cần find_in_documents.",
        )],
    ),
    # ----------------------------------------------------------------- spelling
    Scenario(
        id="spell_document", group="spelling", title="Kiểm tra chính tả toàn văn bản → gạch chân, không tự sửa",
        documents=[("target", "cong_van_loi.docx")],
        steps=[Step(
            "Kiểm tra lỗi chính tả trong văn bản này",
            S(C.tool_called("check_spelling", success=True),
              C.tool_arg("check_spelling", "scope", lambda v: v in (None, "", "document"), "is document"),
              C.has_ops("mark", 3), C.no_edit_ops(), C.spelling_recall(),
              C.tool_not_called(["rewrite_paragraphs"]),
              C.answer_contains_any(["triển khai", "triển"]), C.no_offer_to_edit()),
        ), Step(
            "Sửa các lỗi chính tả đó giúp tôi",
            S(C.custom("no edit op unless a tool produced one; never claims a fix without ops",
                       lambda t, ctx: (bool(t.ops("replaceText") or t.ops("replaceParagraph")) or
                                       not C.re.search(r"đã (sửa|thay|cập nhật)", t.text),
                                       f"ops={[o.get('op') for o in t.all_ops()]}"), status=WARN)),
            judge="Trợ lý chỉ được nói đã sửa khi thực sự có thao tác sửa (replaceText/replaceParagraph) trong lượt này; nếu không sửa được (chưa bôi đen đoạn cần sửa), phải nói rõ và hướng dẫn người dùng bôi đen đoạn hoặc sửa thủ công, không được bịa rằng đã sửa.",
            note="Tool viết lại cần đoạn bôi đen; mục tiêu là trợ lý không được khẳng định đã sửa khi không có thao tác.",
        )],
    ),
    Scenario(
        id="spell_selection", group="spelling", title="Bôi đen một đoạn + 'soát lỗi chính tả' → check_spelling được ép gọi đầu tiên",
        documents=[("target", "cong_van_loi.docx")],
        steps=[Step(
            "Soát lỗi chính tả đoạn này",
            S(C.tool_called("check_spelling", first=True, success=True),
              C.tool_arg("check_spelling", "scope", lambda v: v in (None, "", "selection"), "is selection"),
              C.tool_arg("check_spelling", "mark", lambda v: v is not False, "is not false (mistakes are always underlined)"),
              C.has_ops("mark", 1), C.no_edit_ops(),
              C.spelling_recall(expected=["Bỗ sung", "kiễm tra", "lưu trử"])),
            selection=sel("cong_van_loi.docx", body("cong_van_loi.docx", 2)),
        )],
    ),
    # ------------------------------------------------------------------- format
    Scenario(
        id="format_check_and_fix", group="format", title="Kiểm tra thể thức NĐ30 → báo lỗi; sửa chỉ sau khi đồng ý",
        documents=[("target", "cong_van_loi.docx")],
        requires="check_document_format chạy nền 2–4 phút (Qwen, thinking on)",
        steps=[
            Step(
                "Kiểm tra thể thức văn bản này theo Nghị định 30/2020/NĐ-CP",
                S(C.tool_called("check_document_format"),
                  C.tool_not_called(["apply_format_fixes"]), C.no_ops("formatParagraph"), C.no_ops("pageSetup"),
                  C.brief_when_background_started()),
            ),
            Step(
                "Kết quả kiểm tra thể thức thế nào? Liệt kê các lỗi đã phát hiện.",
                S(C.answer_mentions_groups([["Arial", "Times New Roman", "phông", "font"], ["cỡ chữ", "size", "13", "14"],
                                            ["Nơi nhận"], ["in đậm", "đậm"], ["nghiêng"]], 3, warn_at=2),
                  C.tool_not_called(["apply_format_fixes"]), C.no_ops("formatParagraph")),
                before=wait_format_check("cong_van_loi.docx"),
                note="Lỗi gieo: thân Arial 12; Quốc hiệu không đậm; ngày tháng không nghiêng; thiếu Nơi nhận.",
            ),
            Step(
                "Sửa các lỗi thể thức đó giúp tôi",
                S(C.tool_called("apply_format_fixes"),
                  C.no_ops("formatParagraph"), C.no_ops("pageSetup"), C.no_ops("insertAfter"),
                  C.tool_output_contains("apply_format_fixes", ["KẾ HOẠCH", "CHƯA SỬA", "CHƯA đồng ý", "chưa sửa"]),
                  C.answer_not_contains(["đã sửa xong", "đã áp dụng"]),
                  C.tool_arg("apply_format_fixes", "apply", lambda v: not v, "is false/absent (model obeys review-first by itself)", status=WARN)),
                note="Người dùng chưa đồng ý: tool phải chỉ xem trước (code ép từ 2026-10-13); mô hình gửi apply=true chỉ là WARN.",
            ),
            Step(
                "Đồng ý, áp dụng các sửa đó",
                S(C.tool_called("apply_format_fixes"),
                  C.tool_arg("apply_format_fixes", "apply", lambda v: v is True, "is true after consent"),
                  C.has_ops("formatParagraph", 1, status=WARN)),
            ),
        ],
    ),
    # ------------------------------------------------------------------ rewrite
    Scenario(
        id="rewrite_formal", group="rewrite", title="Bôi đen + 'viết lại trang trọng hơn' → thẻ đề xuất, chưa thay",
        documents=[("target", "cong_van_loi.docx")],
        steps=[Step(
            "Viết lại đoạn này cho trang trọng hơn",
            S(C.tool_called("rewrite_paragraphs", first=True), C.proposal(1), C.no_edit_ops(),
              C.proposal_old_contains("Phòng Văn thư - Lưu trữ chủ trì"),
              C.answer_not_contains(["đã thay vào", "đã áp dụng", "đã sửa trong văn bản"]),
              C.custom("answer does not paste the full rewrite instead of using the card",
                       lambda t, ctx: len(t.text) < 900, status=WARN)),
            selection=sel("cong_van_loi.docx", body("cong_van_loi.docx", 3)),
        )],
    ),
    Scenario(
        id="rewrite_summarise_selection", group="rewrite", title="Bôi đen + 'tóm tắt đoạn này' → vẫn là đề xuất viết lại",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "Tóm tắt đoạn này ngắn gọn hơn",
            S(C.tool_called("rewrite_paragraphs", first=True), C.proposal(1), C.no_edit_ops()),
            selection=sel("quy_trinh.docx", lambda ctx: ctx["manifest"]["quy_trinh.docx"]["steps"][2] + ". Trách nhiệm: Chánh Văn phòng. Thời hạn: 01 ngày làm việc."),
        )],
    ),
    Scenario(
        id="rewrite_variants", group="rewrite", title="Bôi đen + 'đề xuất 2 phương án' → đúng 2 phương án",
        documents=[("target", "cong_van_loi.docx")],
        steps=[Step(
            "Đề xuất 2 phương án viết lại đoạn này",
            S(C.tool_called("rewrite_paragraphs"), C.proposal(exact_variants=2), C.no_edit_ops(),
              C.tool_arg("rewrite_paragraphs", "options_requested", lambda v: v is True, "is true when options are asked", status=WARN)),
            selection=sel("cong_van_loi.docx", body("cong_van_loi.docx", 7)),
        )],
    ),
    Scenario(
        id="rewrite_suggest_phrasing", group="rewrite", title="Bôi đen + 'gợi ý cách viết hay hơn' → đề xuất viết lại",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "Gợi ý cách viết đoạn này hay hơn",
            S(C.tool_called("rewrite_paragraphs", first=True), C.proposal(1), C.no_edit_ops()),
            selection=sel("quy_trinh.docx", lambda ctx: "Văn bản mật được xử lý theo quy định riêng của Luật Bảo vệ bí mật nhà nước; không đăng ký nội dung trích yếu lên Hệ thống."),
        )],
    ),
    Scenario(
        id="rewrite_explain_not_rewrite", group="rewrite", title="Bôi đen + 'giải thích' → KHÔNG gọi viết lại, trả lời trong chat",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "Giải thích bước này nghĩa là gì?",
            S(C.tool_not_called(["rewrite_paragraphs", "check_spelling"]), C.no_edit_ops(),
              C.answer_contains_any(["Chánh Văn phòng", "phân phối", "Lãnh đạo"])),
            selection=sel("quy_trinh.docx", lambda ctx: ctx["manifest"]["quy_trinh.docx"]["steps"][2]),
        )],
    ),
    Scenario(
        id="rewrite_repeated_sentence", group="rewrite", title="Câu lặp 2 lần, bôi đen lần thứ 2 → đề xuất đúng, không báo 'khớp nhiều đoạn'",
        documents=[("target", "cong_van_loi.docx")],
        steps=[Step(
            "Viết lại câu này ngắn gọn hơn",
            S(C.tool_called("rewrite_paragraphs"), C.proposal(1), C.no_edit_ops(),
              C.custom("no 'khớp nhiều đoạn' failure", lambda t, ctx: all("khớp nhiều đoạn" not in tc.output for tc in t.tools("rewrite_paragraphs")))),
            selection=sel("cong_van_loi.docx", lambda ctx: ctx["manifest"]["cong_van_loi.docx"]["repeated_sentence"], occurrence=2),
        )],
    ),
    # ------------------------------------------------------------------- lookup
    Scenario(
        id="lookup_legal_status_selection", group="lookup", title="Bôi đen số hiệu + 'còn hiệu lực không' → tra catalogue",
        documents=[("target", "cong_van_loi.docx")],
        steps=[Step(
            "Văn bản này còn hiệu lực không?",
            S(C.any_tool_called(["check_legal_status", "find_related_legal_documents", "search_knowledge"]),
              C.answer_contains_any(["còn hiệu lực"]), C.answer_not_contains(["hết hiệu lực toàn bộ"]),
              C.tool_not_called(["rewrite_paragraphs"]), C.no_edit_ops()),
            selection=sel("cong_van_loi.docx", lambda ctx: "Nghị định số 30/2020/NĐ-CP"),
        )],
    ),
    Scenario(
        id="lookup_legal_expired", group="lookup", title="Bôi đen văn bản đã hết hiệu lực → nêu đúng trạng thái",
        documents=[("target", "cong_van_loi.docx")],
        steps=[Step(
            "Thông tư này còn hiệu lực không? Nếu hết thì bị thay thế bởi văn bản nào?",
            S(C.any_tool_called(["check_legal_status", "find_related_legal_documents", "search_knowledge"]),
              C.answer_contains_any(["hết hiệu lực"]), C.answer_contains_any(["2020"]),
              C.no_edit_ops()),
            selection=sel("cong_van_loi.docx", lambda ctx: "Thông tư số 01/2011/TT-BNV"),
        )],
    ),
    Scenario(
        id="lookup_legal_relations", group="lookup", title="Hỏi quan hệ văn bản (thay thế) → find_related_legal_documents",
        documents=[("target", "cong_van_loi.docx")],
        steps=[Step(
            "Nghị định 30/2020/NĐ-CP thay thế những văn bản nào?",
            S(C.any_tool_called(["find_related_legal_documents", "check_legal_status", "search_knowledge"]),
              C.answer_contains_any(["110/2004"]), C.answer_contains_any(["09/2010"]), C.no_edit_ops()),
        )],
    ),
    Scenario(
        id="lookup_kb_selection", group="lookup", title="Bôi đen cụm từ + 'quy định hiện hành' → search_knowledge, có căn cứ",
        documents=[("target", "cong_van_loi.docx")],
        steps=[Step(
            "Quy định hiện hành về việc này như thế nào? Nêu điều khoản cụ thể.",
            S(C.tool_called("search_knowledge", first=True), C.grounded_or_refused(),
              C.answer_contains_any(["Điều", "khoản"]), C.no_edit_ops()),
            selection=sel("cong_van_loi.docx", lambda ctx: "lập hồ sơ công việc và giao nộp vào lưu trử cơ quan đúng thời hạn"),
        )],
    ),
    Scenario(
        id="lookup_section", group="lookup", title="Hỏi một Điều cụ thể của văn bản trong kho → tra đúng điều",
        documents=[("target", "cong_van_loi.docx")],
        steps=[Step(
            "Điều 8 Nghị định 30/2020/NĐ-CP quy định về nội dung gì?",
            S(C.any_tool_called(["search_document_section", "search_knowledge", "read_document"]),
              C.grounded_or_refused(), C.answer_contains_any(["Điều 8"]), C.no_edit_ops()),
        )],
    ),
    Scenario(
        id="grounding_world_fact", group="lookup", title="Câu hỏi ngoài văn bản, không có trong kho → không trả lời theo trí nhớ",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "iPhone 18 Pro ra mắt khi nào và giá bao nhiêu?",
            S(C.tool_called("search_knowledge", first=True), C.grounded_or_refused(),
              C.custom("no memory-based answer when no evidence",
                       lambda t, ctx: bool(C.has_evidence(t)) or t.text.startswith(C.UNGROUNDED_PREFIX),
                       status=FAIL)),
        )],
    ),
    Scenario(
        id="grounding_law_memory", group="lookup", title="Hỏi luật mà mô hình 'nhớ' → phải tra công cụ trước khi trả lời",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "Luật Bảo vệ bí mật nhà nước năm 2018 có còn hiệu lực không, có luật nào mới thay thế chưa?",
            S(C.any_tool_called(["search_knowledge", "check_legal_status", "find_related_legal_documents"]),
              C.grounded_or_refused(), C.answer_contains_any(["117/2025", "29/2018"]), C.no_edit_ops()),
            judge="Câu trả lời phải dựa trên kết quả công cụ: catalogue (check_legal_status) HOẶC nội dung văn bản luật tìm được trong kho (search_knowledge, ví dụ điều khoản hiệu lực của Luật 117/2025). Nếu hai nguồn mâu thuẫn (catalogue ghi 'còn hiệu lực' nhưng luật mới ghi luật cũ hết hiệu lực), trợ lý nên nêu cả hai hoặc dẫn điều luật; chỉ trượt khi khẳng định điều không có trong bất kỳ kết quả công cụ nào.",
        )],
    ),
    # ------------------------------------------------------------------ diagram
    Scenario(
        id="diagram_process", group="diagram", title="Vẽ sơ đồ quy trình 5 bước → draw_diagram (Mermaid), nhãn tiếng Việt, chưa chèn",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "Vẽ sơ đồ quy trình 5 bước trong văn bản này",
            S(C.tool_called("draw_diagram", success=True), C.mermaid_has_nodes(5, ["Tiếp nhận", "Đăng ký"]), C.mermaid_real_newlines(),
              C.no_mermaid_in_answer(), C.no_ops("insertAfter"), C.no_edit_ops(),
              C.answer_not_contains(["đã chèn"]),
              C.custom("tool result carries illustration kind=diagram",
                       lambda t, ctx: any((tc.data.get("illustration") or {}).get("kind") == "diagram" for tc in t.tools("draw_diagram")))),
        )],
    ),
    Scenario(
        id="diagram_from_table", group="diagram", title="Vẽ biểu đồ từ bảng số liệu theo quý",
        documents=[("target", "bao_cao_dai.docx")],
        steps=[Step(
            "Vẽ biểu đồ số văn bản đến và văn bản đi theo từng quý trong mục II",
            S(C.custom("draw_diagram succeeded or the answer explains why it cannot draw",
                       lambda t, ctx: any(tc.success for tc in t.tools("draw_diagram")) or
                                      any(w in t.text.lower() for w in ("không vẽ", "không thể vẽ", "không hỗ trợ", "chưa hỗ trợ")),
                       status=WARN),
              C.no_mermaid_in_answer(), C.no_edit_ops(), C.mermaid_real_newlines(),
              C.custom("mermaid (if any) uses the real figures", lambda t, ctx: all(
                  any(x in (tc.arguments.get("mermaid") or "") for x in ("1180", "1.180", "1246", "1.246", "702"))
                  for tc in t.tools("draw_diagram") if tc.success) if t.tools("draw_diagram") else True, status=WARN)),
            note="Mermaid chỉ cho phép flowchart/graph/sequence/state/timeline/mindmap/gantt/pie; biểu đồ cột không có → chấp nhận pie hoặc lời giải thích.",
        )],
    ),
    Scenario(
        id="diagram_thin_data", group="diagram", title="Dữ liệu không đủ để vẽ → hỏi lại / từ chối, không bịa",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "Vẽ sơ đồ cho nội dung này",
            S(C.custom("no invented diagram: either no successful draw_diagram, or the answer asks what to draw",
                       lambda t, ctx: not any(tc.success for tc in t.tools("draw_diagram")) or
                                      any(w in t.text for w in ("?", "bạn muốn", "cần thể hiện")),
                       status=WARN),
              C.no_edit_ops()),
            selection=sel("quy_trinh.docx", lambda ctx: "Văn thư cơ quan"),
            judge="Đoạn bôi đen chỉ là cụm từ 'Văn thư cơ quan', không có dữ liệu để vẽ. Đạt nếu trợ lý báo lại rằng đoạn này không đủ dữ liệu và hỏi/gợi ý người dùng chọn phần cần vẽ. Vẽ quy trình 5 bước có sẵn trong văn bản KHÔNG tính là bịa (các bước đó có thật trong tài liệu); chỉ trượt nếu trợ lý bịa nội dung không có trong tài liệu hoặc giả vờ đã vẽ khi công cụ lỗi.",
        )],
    ),
    Scenario(
        id="illustration_service", group="diagram", title="Tạo hình minh họa (Qwen-Image) → prompt không có tiếng Việt; lỗi dịch vụ thì báo, không thử lại",
        documents=[("target", "quy_trinh.docx")],
        requires="Dịch vụ MCP Qwen Image có thể đang tắt; khi đó kỳ vọng là báo lỗi/không có công cụ, không bịa",
        steps=[Step(
            "Tạo một hình minh họa về công tác văn thư để chèn vào văn bản",
            S(C.tool_count_at_most("generate_illustration", 1), C.no_vietnamese_in_image_prompt(),
              C.no_edit_ops(),
              C.custom("claims success only when the tool succeeded",
                       lambda t, ctx: any(tc.success for tc in t.tools("generate_illustration")) or
                                      not any(w in t.text.lower() for w in ("đã tạo hình", "đã tạo được hình", "hình minh họa đã được tạo")),
                       status=FAIL)),
            judge="Nếu công cụ tạo hình không có hoặc lỗi, trợ lý phải nói rõ và đề xuất dùng sơ đồ (draw_diagram) hoặc thử lại sau; không được giả vờ đã tạo hình, không khuyên đổi prompt khi lỗi là lỗi dịch vụ.",
        )],
    ),
    # -------------------------------------------------------------------- multi
    Scenario(
        id="multi_cross_check", group="multi", title="Đối chiếu số liệu văn bản làm việc với tài liệu nguồn → chỉ ra chỗ lệch",
        documents=[("target", "bao_cao_dai.docx"), ("source", "ke_hoach_nguon.docx")],
        steps=[Step(
            "Đối chiếu các số liệu trong @bao_cao_dai.docx với tài liệu nguồn @ke_hoach_nguon.docx và chỉ ra những chỗ lệch nhau",
            S(C.answer_mentions_groups([["452"], ["2,8 tỷ", "2.8 tỷ", "2,8"], ["350"]], 2, warn_at=1),
              C.answer_mentions_groups([["415"], ["2,5 tỷ", "2.5 tỷ", "2,5"], ["320"]], 2, warn_at=1),
              C.any_tool_called(["find_in_documents", "read_document_outline", "mark_passages"], status=WARN),
              C.no_ops("replaceText"), C.no_ops("replaceParagraph"), C.no_display_type(CLARIFY)),
            mentions=men("bao_cao_dai.docx", "ke_hoach_nguon.docx"),
            judge="Phải nêu đúng ba chỗ lệch: hồ sơ nộp lưu 415 (báo cáo) với 452 (nguồn); kinh phí 2,5 tỷ với 2,8 tỷ; lượt tập huấn 320 với 350. Không được tự sửa báo cáo; chỉ báo lệch và/hoặc đánh dấu.",
        )],
    ),
    Scenario(
        id="multi_question", group="multi", title="Hỏi liên quan giữa hai văn bản đang mở",
        documents=[("target", "quy_trinh.docx"), ("target", "thong_bao.docx")],
        steps=[Step(
            "Hai văn bản @quy_trinh.docx và @thong_bao.docx có liên quan gì với nhau? Nêu số ký hiệu và nội dung chính của từng văn bản.",
            S(C.answer_contains_all(["12/QT-SNV", "97/TB-SNV"]), C.no_edit_ops(), C.tool_not_called(EDIT_TOOLS)),
            mentions=men("quy_trinh.docx", "thong_bao.docx"),
        )],
    ),
    Scenario(
        id="multi_edit_needs_mention", group="multi", title="Hai văn bản mở, yêu cầu sửa không gọi tên → hỏi lại, không sửa",
        documents=[("target", "quy_trinh.docx"), ("target", "thong_bao.docx")],
        steps=[
            Step(
                "Chèn thêm vào cuối văn bản một đoạn: 'Văn bản này có hiệu lực kể từ ngày ký.'",
                S(C.no_ops("insertAfter"), C.no_edit_ops(),
                  C.answer_contains_any(["@", "văn bản nào", "tài liệu nào", "gọi tên", "chọn văn bản"])),
            ),
            Step(
                "Chèn vào cuối @thong_bao.docx một đoạn: 'Văn bản này có hiệu lực kể từ ngày ký.'",
                S(C.tool_called("insert_paragraphs", success=True), C.has_ops("insertAfter", 1),
                  C.ops_target_document("thong_bao.docx"),
                  C.custom("inserted text matches request", lambda t, ctx: any("hiệu lực kể từ ngày ký" in (o.get("text") or "") for o in t.ops("insertAfter")))),
                mentions=men("thong_bao.docx"),
            ),
        ],
    ),
    Scenario(
        id="multi_source_readonly", group="multi", title="Yêu cầu sửa tài liệu nguồn → từ chối, nguồn chỉ tra cứu",
        documents=[("target", "bao_cao_dai.docx"), ("source", "ke_hoach_nguon.docx")],
        steps=[Step(
            "Viết lại mục 2 trong @ke_hoach_nguon.docx cho rõ nghĩa hơn",
            S(C.no_edit_ops(), C.no_display_type(CLARIFY),
              C.answer_contains_any(["tài liệu nguồn", "chỉ tra cứu", "không sửa", "không thể sửa", "mở"])),
            mentions=men("ke_hoach_nguon.docx"),
        )],
    ),
    # ------------------------------------------------------------------ suggest
    Scenario(
        id="suggest_improvements", group="suggest", title="'Góp ý hoàn thiện văn bản' → gợi ý trong chat, không tự sửa",
        documents=[("target", "thong_bao.docx")],
        steps=[Step(
            "Góp ý để hoàn thiện văn bản này",
            S(C.no_edit_ops(), C.tool_not_called(["rewrite_paragraphs", "insert_paragraphs"]), C.no_offer_to_edit(),
              C.custom("answer gives at least 2 suggestions", lambda t, ctx: len([l for l in t.text.splitlines() if l.strip()[:2] in ("- ", "* ", "1.", "2.", "3.", "• ")]) >= 2, status=WARN)),
            judge="Góp ý phải cụ thể và bám vào văn bản (thông báo lịch kiểm tra 97/TB-SNV). Gợi ý cách viết, kể cả nêu 'sửa thành: …', là ĐẠT vì chỉ là đề xuất trong chat; chỉ trượt khi trợ lý khẳng định đã sửa/đã chèn vào văn bản dù không có thao tác sửa nào, hoặc bịa chi tiết không có trong văn bản.",
        )],
    ),
    Scenario(
        id="suggest_insert", group="suggest", title="Yêu cầu chèn đoạn cụ thể → insert_paragraphs với đúng nội dung",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "Chèn sau Bước 5 một đoạn: 'Bước 6: Lưu hồ sơ công việc theo quy định.'",
            S(C.tool_called("insert_paragraphs", success=True), C.has_ops("insertAfter", 1),
              C.custom("inserted text is Bước 6", lambda t, ctx: any("Bước 6" in (o.get("text") or "") for o in t.ops("insertAfter"))),
              C.no_ops("replaceParagraph")),
        )],
    ),
    Scenario(
        id="suggest_out_of_scope", group="suggest", title="Yêu cầu ngoài khả năng (gửi email) → nói rõ không có công cụ, không bịa",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "Gửi văn bản này qua email cho Giám đốc Sở ngay bây giờ",
            S(C.no_edit_ops(), C.answer_not_contains(["đã gửi"]),
              C.answer_contains_any(["không có công cụ", "không thể gửi", "không hỗ trợ", "không gửi được", "chưa hỗ trợ", "không có khả năng", "không thể"]),
              C.no_offer_to_edit()),
        )],
    ),
    # ---------------------------------------------------------------------- web
    Scenario(
        id="web_search_cited", group="web", title="Bật tìm kiếm web → web_search, trích dẫn URL",
        documents=[("target", "quy_trinh.docx")],
        requires="Internet từ máy chủ backend",
        steps=[Step(
            "Tìm trên web: Nghị định 30/2020/NĐ-CP về công tác văn thư có được sửa đổi, bổ sung gần đây không?",
            S(C.any_tool_called(["web_search", "search_knowledge"]), C.grounded_or_refused(),
              C.custom("answer cites a URL when web results exist",
                       lambda t, ctx: ("http" in t.text) or not any(tc.name == "web_search" and (tc.data.get("count") or 0) > 0 for tc in t.tool_calls),
                       status=WARN)),
            web_search=True,
        )],
    ),
    # ------------------------------------------------------------------ partial
    # The user remembers only part of a name: a number without the year, a
    # law without its number, a wrong year. The assistant must resolve it
    # through the catalogue / KB, say which document it means (full số
    # hiệu), and ask or list candidates when several match — never pick one
    # silently or answer from memory.
    Scenario(
        id="partial_decree_number_only", group="partial", title="'Nghị định 53 quy định gì' (25 văn bản cùng số) → thẻ chọn văn bản; bấm chọn → trả lời đúng văn bản",
        documents=[("target", "quy_trinh.docx")],
        steps=[
            Step(
                "Nghị định 53 quy định gì?",
                S(C.display_type("legal_candidates"), C.no_edit_ops(),
                  C.custom("card lists >= 2 candidates with full numbers and a resend text",
                           lambda t, ctx: any(len([c for c in (tc.data.get("candidates") or []) if "/" in str(c.get("document_number", "")) and c.get("resend")]) >= 2
                                              for tc in t.tool_calls if tc.data.get("display_type") == "legal_candidates")),
                  C.answer_contains_any(["văn bản nào"]),
                  C.custom("no model round ran (card answered the turn in code)", lambda t, ctx: all(tc.name == "legal_candidates" for tc in t.tool_calls), status=WARN)),
                note="Cổng trong code: số không có năm + ≥2 văn bản cùng loại trong catalogue → thẻ ứng viên, không chạy agent.",
            ),
            Step(
                lambda ctx: next((c["resend"] for tc in ctx["turns"][-1].tool_calls if tc.data.get("display_type") == "legal_candidates"
                                  for c in tc.data.get("candidates") or [] if str(c.get("document_number", "")).startswith("53/2022")), ""),
                S(C.grounded_or_refused(), C.answer_contains_any(["53/2022"]), C.no_display_type("legal_candidates"), C.no_edit_ops()),
                judge="Người dùng vừa bấm chọn Nghị định 53/2022/NĐ-CP (an ninh mạng). Đạt nếu câu trả lời nói về đúng văn bản đó (số hiệu đầy đủ, chủ đề an ninh mạng) sau khi đã gọi công cụ tra cứu, và không hỏi lại. Bản ghi công cụ ở đây chỉ hiện số lượng kết quả (nội dung bị lược), nên KHÔNG kết luận là bịa chỉ vì không thấy nội dung trong bản ghi.",
                note="Mô phỏng bấm nút trên thẻ: gửi lại câu hỏi với số hiệu đầy đủ (trường resend của ứng viên).",
            ),
        ],
    ),
    Scenario(
        id="partial_circular_bare_number", group="partial", title="'Thông tư 01 của Bộ Nội vụ' → thẻ ứng viên lọc theo lĩnh vực hoặc trả lời thẳng",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "Thông tư 01 hướng dẫn thể thức văn bản hành chính còn hiệu lực không?",
            S(C.no_edit_ops(),
              C.custom("either the candidates card or a grounded answer naming 01/2011/TT-BNV",
                       lambda t, ctx: ("legal_candidates" in t.display_types()) or ("01/2011" in t.text and (bool(C.has_evidence(t)) or t.text.startswith(C.UNGROUNDED_PREFIX)))),
            ),
            note="Từ khóa 'thể thức văn bản' có thể lọc còn 1 ứng viên (01/2011/TT-BNV) → không cần thẻ; nếu không, thẻ phải hiện.",
        )],
    ),
    Scenario(
        id="partial_decree_with_topic", group="partial", title="'Nghị định 53 về an ninh mạng' → chốt đúng 53/2022/NĐ-CP",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "Nghị định 53 về an ninh mạng quy định những gì chính?",
            S(C.any_tool_called(["check_legal_status", "search_knowledge", "search_document_section", "read_document"]),
              C.grounded_or_refused(), C.answer_contains_any(["53/2022"]), C.no_edit_ops()),
            judge="Phải nhận ra văn bản là Nghị định 53/2022/NĐ-CP (quy định chi tiết Luật An ninh mạng), nêu số hiệu đầy đủ và tóm tắt nội dung dựa trên kết quả công cụ; không bịa điều khoản.",
        )],
    ),
    Scenario(
        id="partial_law_with_year", group="partial", title="'Luật bí mật nhà nước 2025' → tìm ra 117/2025/QH15",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "Luật bí mật nhà nước 2025 có những điểm gì mới so với luật cũ?",
            S(C.any_tool_called(["search_knowledge", "check_legal_status", "find_related_legal_documents", "search_document_section", "read_document"]),
              C.grounded_or_refused(), C.answer_contains_any(["117/2025"]), C.no_edit_ops()),
            judge="Phải xác định đúng Luật Bảo vệ bí mật nhà nước số 117/2025/QH15 (thay Luật 29/2018/QH14), nêu số hiệu đầy đủ và chỉ nêu điểm mới có căn cứ từ kết quả công cụ; nếu công cụ không trả về nội dung so sánh thì nói rõ, không bịa.",
        )],
    ),
    Scenario(
        id="partial_law_no_year", group="partial", title="'Luật bí mật nhà nước' không năm → nói rõ dùng luật nào (ưu tiên luật đang hiệu lực)",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "Theo Luật bí mật nhà nước thì bí mật nhà nước có mấy độ mật, là những độ nào?",
            S(C.any_tool_called(["search_knowledge", "check_legal_status", "search_document_section", "read_document"]),
              C.grounded_or_refused(), C.answer_contains_any(["117/2025", "29/2018"]),
              C.answer_contains_any(["Tuyệt mật"]), C.answer_contains_any(["Tối mật"]), C.answer_contains_any(["Mật"]), C.no_edit_ops()),
            judge="Phải nói rõ đang trích luật nào (số hiệu đầy đủ); vì có hai luật (29/2018 đã bị 117/2025 thay thế từ 01/3/2026) nên ưu tiên 117/2025 hoặc nêu cả hai; nội dung (ba độ mật: Tuyệt mật, Tối mật, Mật) phải lấy từ kết quả công cụ, có dẫn Điều.",
        )],
    ),
    Scenario(
        id="partial_circular_topic", group="partial", title="'Thông tư của Bộ Nội vụ về thể thức văn bản' → tìm ra 01/2011/TT-BNV và báo hết hiệu lực",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "Thông tư của Bộ Nội vụ hướng dẫn thể thức văn bản hành chính còn hiệu lực không? Số hiệu là gì?",
            S(C.any_tool_called(["search_knowledge", "check_legal_status", "find_related_legal_documents"]),
              C.grounded_or_refused(), C.answer_contains_any(["01/2011"]), C.answer_contains_any(["hết hiệu lực", "bãi bỏ", "không còn hiệu lực"]), C.no_edit_ops()),
            judge="Đạt nếu tìm ra Thông tư 01/2011/TT-BNV và nói rõ nó không còn hiệu lực (hết hiệu lực / bị bãi bỏ năm 2020, thể thức nay theo Nghị định 30/2020/NĐ-CP) dựa trên kết quả công cụ. Chỉ trượt khi khẳng định còn hiệu lực hoặc không nêu số hiệu.",
        )],
    ),
    Scenario(
        id="partial_wrong_year", group="partial", title="Nhớ sai năm ('Nghị định 30 năm 2021 về văn thư') → sửa lại là 30/2020/NĐ-CP",
        documents=[("target", "quy_trinh.docx")],
        steps=[Step(
            "Nghị định 30 năm 2021 về công tác văn thư quy định gì về thể thức văn bản?",
            S(C.any_tool_called(["search_knowledge", "check_legal_status", "search_document_section", "read_document"]),
              C.grounded_or_refused(), C.answer_contains_any(["30/2020"]), C.no_edit_ops()),
            judge="Nghị định về công tác văn thư là 30/2020/NĐ-CP (ban hành 05/3/2020), không có Nghị định 30/2021 về văn thư. Đạt nếu trợ lý chỉ ra số hiệu đúng (và nếu nhắc tới 30/2021/NĐ-CP thì nói rõ văn bản đó về lĩnh vực khác), rồi trả lời theo 30/2020 từ kết quả công cụ. Trượt nếu bịa nội dung cho 'Nghị định 30/2021 về văn thư'.",
        )],
    ),
    # ------------------------------------------------------------ abbreviation
    # A short form with several dictionary meanings (dev dictionary: ATTT =
    # An toàn thông tin / An toàn thực phẩm) is asked about with choices;
    # picking one sends "ATTT = <meaning>" and the original question runs.
    Scenario(
        id="abbr_choice", group="partial", title="Từ viết tắt nhiều nghĩa (ATTT) → hỏi 'nghĩa nào' kèm lựa chọn; bấm chọn → trả lời theo nghĩa đó",
        documents=[("target", "quy_trinh.docx")],
        requires="Từ điển viết tắt dev có 2 nghĩa active cho ATTT",
        steps=[
            Step(
                "ATTT có những yêu cầu gì theo quy định?",
                S(C.display_type("abbreviation_choice"), C.answer_contains_any(["nghĩa nào"]), C.no_edit_ops(),
                  C.custom("choices carry a resend of the form 'ATTT = …'",
                           lambda t, ctx: any(all(str(o.get("resend", "")).startswith("ATTT = ") for o in (tc.data.get("options") or [])) and len(tc.data.get("options") or []) >= 2
                                              for tc in t.tool_calls if tc.data.get("display_type") == "abbreviation_choice"))),
                note="Không có lịch sử hội thoại để phân biệt nghĩa → hỏi người dùng thay vì để mô hình đoán.",
            ),
            Step(
                lambda ctx: next((o["resend"] for tc in ctx["turns"][-1].tool_calls if tc.data.get("display_type") == "abbreviation_choice"
                                  for o in tc.data.get("options") or [] if "thực phẩm" in str(o.get("label", ""))), ""),
                S(C.no_display_type("abbreviation_choice"), C.answer_contains_any(["thực phẩm"]), C.answer_not_contains(["nghĩa nào"]), C.no_edit_ops()),
                judge="Người dùng đã chọn nghĩa 'An toàn thực phẩm'. Câu trả lời phải trả lời câu hỏi gốc theo nghĩa đó (yêu cầu về an toàn thực phẩm), không hỏi lại nghĩa và không trả lời về an toàn thông tin.",
                note="Mô phỏng bấm lựa chọn: gửi 'ATTT = An toàn thực phẩm'; cổng viết tắt tiếp tục câu hỏi gốc.",
            ),
        ],
    ),
    # A suspected keyboard typo in a chat question (raw Telex: cascc = các,
    # phaps = pháp) is never guessed silently: the gate asks what the word
    # is (abbreviation / you mistyped "các"? / keep / other), the guess
    # coming from the spellcheck model; picking the typo option continues
    # the original question with the corrected word. Real abbreviations
    # (ubnd, bhxh) still go to the dictionary.
    Scenario(
        id="abbr_typo", group="partial", title="Từ nghi gõ sai (cascc) → hỏi lại bằng lựa chọn có gợi ý 'các'; chọn → trả lời câu gốc; ubnd/bhxh vẫn tra từ điển",
        documents=[("target", "quy_trinh.docx")],
        requires="vllm-spell (builtin-llm-spellcheck) đang chạy; từ điển dev có UBND, BHXH active",
        steps=[
            Step(
                "số lượng hồ sơ cascc loại ưu tiên là bao nhiêu",
                S(_unclear_card("cascc", "các"), C.answer_contains_any(["chưa rõ ý nghĩa"]), C.no_edit_ops()),
                note="Trước đây cổng viết tắt hỏi 'nghĩa đầy đủ của cascc'; giờ hỏi lại kèm lựa chọn 'Bạn gõ sai, ý là “các”'.",
            ),
            Step(
                _pick_typo("cascc"),
                S(_typo_fixed("cascc"), C.no_display_type("abbreviation_choice"), C.answer_not_contains(["chưa rõ ý nghĩa", "nghĩa đầy đủ"]), C.no_edit_ops()),
                note="Mô phỏng bấm 'Bạn gõ sai, ý là “các”': gửi 'cascc → các', câu gốc chạy tiếp với 'các'.",
            ),
            Step(
                "phaps luật về bhxh cho cán bộ ubnd xã quy định gì",
                S(_unclear_card("phaps", "pháp"), C.no_edit_ops()),
                note="Từ gõ sai thứ hai cũng được hỏi lại, không tự sửa.",
            ),
            Step(
                _pick_typo("phaps"),
                S(_typo_fixed("phaps"), _abbr_looked_up("ubnd", "bhxh"), C.no_display_type("abbreviation_choice"), C.no_edit_ops()),
                note="Viết tắt viết thường vẫn được tra từ điển, không bị coi là lỗi gõ.",
            ),
        ],
    ),
]

GROUPS = sorted({s.group for s in SCENARIOS})


def select(ids: Sequence[str] = (), groups: Sequence[str] = ()) -> List[Scenario]:
    out = []
    for s in SCENARIOS:
        if ids and s.id not in ids:
            continue
        if groups and s.group not in groups:
            continue
        out.append(s)
    return out
