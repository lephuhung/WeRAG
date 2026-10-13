"""Assertion helpers. Every check is `fn(turn, ctx) -> Result`.

Status values: PASS, FAIL, WARN (soft expectation, e.g. model quality or an
infrastructure dependency), SKIP (not applicable / judge disabled).
"""
from __future__ import annotations

import json
import re
import unicodedata
from dataclasses import dataclass
from typing import Any, Callable, Dict, Iterable, List, Optional, Sequence

from werag_client import Turn

PASS, FAIL, WARN, SKIP = "PASS", "FAIL", "WARN", "SKIP"

EDIT_OPS = {"replaceText", "replaceParagraph", "insertAfter", "formatParagraph", "pageSetup"}
MARK_OPS = {"mark", "unmark"}

# Exact text of the grounding gate's refusal (internal/agent/grounding_gate.go).
UNGROUNDED_PREFIX = "Tôi không tìm thấy căn cứ cho câu hỏi này trong kho tri thức"

VIET_CHARS_RE = re.compile(r"[Ạ-ỹăâđêôơưĂÂĐÊÔƠƯ]")
OFFER_TO_EDIT_RE = re.compile(r"(bạn|anh/chị|anh chị|quý vị) (có )?muốn tôi (sửa|thay|chỉnh|áp dụng|viết lại|chèn|bổ sung|thêm|cập nhật)", re.I)


@dataclass
class Result:
    status: str
    label: str
    detail: str = ""

    def as_dict(self) -> Dict[str, str]:
        return {"status": self.status, "label": self.label, "detail": self.detail}


Check = Callable[[Turn, Dict[str, Any]], Result]


def fold(s: str) -> str:
    """Lower-case, strip diacritics (for tolerant text matching)."""
    s = unicodedata.normalize("NFD", s or "")
    s = "".join(ch for ch in s if unicodedata.category(ch) != "Mn")
    return s.replace("đ", "d").replace("Đ", "D").lower()


def _contains(hay: str, needle: str, tolerant: bool) -> bool:
    if tolerant:
        return fold(needle) in fold(hay)
    return needle in hay


def _tool_blob(turn: Turn, name: Optional[str] = None) -> str:
    parts = []
    for t in turn.tools(name):
        parts.append(t.output)
        try:
            parts.append(json.dumps(t.data, ensure_ascii=False))
        except (TypeError, ValueError):
            pass
    return "\n".join(parts)


# ---- tool presence ---------------------------------------------------------
def tool_called(name: str, *, first: bool = False, success: Optional[bool] = None, status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        label = f"tool {name} called" + (" first" if first else "")
        calls = turn.tools(name)
        if not calls:
            return Result(status, label, f"tools called: {turn.tool_names() or 'none'}")
        if first and turn.first_tool() != name:
            return Result(status, label, f"first tool was {turn.first_tool()}; order {turn.tool_names()}")
        if success is not None:
            ok = [c for c in calls if c.success is success]
            if not ok:
                errs = [(c.result or {}).get("error") or c.output[:200] for c in calls]
                return Result(status, label + f" with success={success}", f"results: {errs}")
        return Result(PASS, label, f"order {turn.tool_names()}")
    return run


def any_tool_called(names: Sequence[str], status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        label = f"one of {list(names)} called"
        hit = [n for n in turn.tool_names() if n in names]
        if hit:
            return Result(PASS, label, f"order {turn.tool_names()}")
        return Result(status, label, f"tools called: {turn.tool_names() or 'none'}")
    return run


def tool_not_called(names: Sequence[str], status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        label = f"none of {list(names)} called"
        hit = [n for n in turn.tool_names() if n in names]
        if hit:
            return Result(status, label, f"called {hit}")
        return Result(PASS, label)
    return run


def tool_count_at_most(name: str, n: int, status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        c = len(turn.tools(name))
        label = f"{name} called at most {n}x"
        return Result(PASS if c <= n else status, label, f"called {c}x")
    return run


def no_tool_called(status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        return Result(PASS if not turn.tool_calls else status, "no tool called", f"tools: {turn.tool_names()}")
    return run


def tool_arg(name: str, key: str, pred: Callable[[Any], bool], desc: str, status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        label = f"{name}.{key} {desc}"
        calls = turn.tools(name)
        if not calls:
            return Result(status, label, "tool not called")
        vals = [c.arguments.get(key) for c in calls]
        if any(pred(v) for v in vals):
            return Result(PASS, label, f"values {vals}")
        return Result(status, label, f"values {vals}")
    return run


def tool_output_contains(name: str, needles: Sequence[str], *, tolerant: bool = True, status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        label = f"{name} result mentions one of {list(needles)}"
        blob = _tool_blob(turn, name)
        if not blob:
            return Result(status, label, "tool not called / no result")
        hit = [n for n in needles if _contains(blob, n, tolerant)]
        return Result(PASS if hit else status, label, f"hit {hit}")
    return run


# ---- document ops -----------------------------------------------------------
def no_edit_ops(status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        ops = [o.get("op") for o in turn.all_ops() if o.get("op") in EDIT_OPS]
        return Result(PASS if not ops else status, "no document-changing ops (replace/insert/format)", f"ops {ops}")
    return run


def has_ops(kind: str, minimum: int = 1, status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        n = len(turn.ops(kind))
        return Result(PASS if n >= minimum else status, f">= {minimum} '{kind}' op(s)", f"{n} found; all ops {[o.get('op') for o in turn.all_ops()]}")
    return run


def no_ops(kind: str, status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        n = len(turn.ops(kind))
        return Result(PASS if n == 0 else status, f"no '{kind}' op", f"{n} found")
    return run


def ops_target_document(doc_key: str, status: str = FAIL) -> Check:
    """Every edit-plan in the turn must point at ctx['docs'][doc_key]."""
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        want = ctx["docs"][doc_key]["id"]
        label = f"edit plan targets {doc_key}"
        ids = [t.data.get("document_id") for t in turn.tool_calls if t.document_ops]
        if not ids:
            return Result(status, label, "no edit plan in turn")
        bad = [i for i in ids if i != want]
        return Result(PASS if not bad else status, label, f"document_ids {ids}, wanted {want}")
    return run


# ---- proposals / cards -------------------------------------------------------
def proposal(min_variants: int = 1, exact_variants: Optional[int] = None, status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        label = "rewrite proposal card present"
        props = turn.proposals()
        if not props:
            failed = [t.output[:300] for t in turn.tools("rewrite_paragraphs")]
            return Result(status, label, f"no proposal; rewrite outputs {failed}")
        variants = props[-1].data.get("variants") or []
        n = len(variants)
        if exact_variants is not None and n != exact_variants:
            return Result(status, label + f" with {exact_variants} variants", f"{n} variants")
        if n < min_variants:
            return Result(status, label + f" with >= {min_variants} variants", f"{n} variants")
        planned = props[-1].data.get("planned")
        failed = props[-1].data.get("failed")
        return Result(PASS, label, f"{n} variant(s), planned={planned} failed={failed}")
    return run


def proposal_old_contains(needle: str, status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        label = "proposal 'old' text is the selected passage"
        props = turn.proposals()
        if not props:
            return Result(status, label, "no proposal")
        olds = []
        for v in props[-1].data.get("variants") or []:
            olds.append(str(v.get("old") or ""))
        for ch in props[-1].data.get("changes") or []:
            olds.append(str(ch.get("old") or ""))
        ok = any(fold(needle[:60]) in fold(o) for o in olds)
        return Result(PASS if ok else status, label, f"olds {[o[:80] for o in olds]}")
    return run


def display_type(name: str, status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        dts = turn.display_types()
        return Result(PASS if name in dts else status, f"display_type {name} present", f"display types {dts}")
    return run


def no_display_type(name: str, status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        dts = turn.display_types()
        return Result(PASS if name not in dts else status, f"display_type {name} absent", f"display types {dts}")
    return run


# ---- answer text -----------------------------------------------------------
def answer_contains_any(needles: Sequence[str], *, tolerant: bool = True, status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        hit = [n for n in needles if _contains(turn.text, n, tolerant)]
        return Result(PASS if hit else status, f"answer mentions one of {list(needles)}", f"hit {hit}")
    return run


def answer_contains_all(needles: Sequence[str], *, tolerant: bool = True, status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        miss = [n for n in needles if not _contains(turn.text, n, tolerant)]
        return Result(PASS if not miss else status, f"answer mentions all of {list(needles)}", f"missing {miss}")
    return run


def answer_mentions_groups(groups: Sequence[Sequence[str]], minimum: int, *, tolerant: bool = True,
                           status: str = FAIL, warn_at: Optional[int] = None) -> Check:
    """At least `minimum` of the groups have one needle present in the answer."""
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        hits = [g for g in groups if any(_contains(turn.text, n, tolerant) for n in g)]
        n = len(hits)
        label = f"answer covers >= {minimum}/{len(groups)} expected points"
        if n >= minimum:
            return Result(PASS, label, f"covered {n}: {[g[0] for g in hits]}")
        if warn_at is not None and n >= warn_at:
            return Result(WARN, label, f"covered only {n}: {[g[0] for g in hits]}")
        return Result(status, label, f"covered {n}: {[g[0] for g in hits]}")
    return run


def answer_not_contains(needles: Sequence[str], *, tolerant: bool = True, status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        hit = [n for n in needles if _contains(turn.text, n, tolerant)]
        return Result(PASS if not hit else status, f"answer avoids {list(needles)}", f"hit {hit}")
    return run


def answer_not_regex(pattern: str, label: str, status: str = FAIL) -> Check:
    rx = re.compile(pattern, re.I | re.S)
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        m = rx.search(turn.text)
        return Result(PASS if not m else status, label, f"matched {m.group(0)[:80]!r}" if m else "")
    return run


def answer_nonempty() -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        return Result(PASS if turn.text else FAIL, "answer not empty", f"{len(turn.text)} chars")
    return run


def no_turn_error() -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        return Result(PASS if not turn.error else FAIL, "turn completed without error", turn.error or "")
    return run


def no_offer_to_edit(status: str = WARN) -> Check:
    """Prompt rule: never close by offering to change the document yourself."""
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        m = OFFER_TO_EDIT_RE.search(turn.text)
        return Result(PASS if not m else status, "answer does not offer to edit the document itself", m.group(0) if m else "")
    return run


def no_phantom_button(status: str = FAIL) -> Check:
    """The answer may only invite the user to press a card button when the
    turn produced that card (rewrite proposal, illustration, format-fix plan,
    editor ops)."""
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        label = "no invitation to press a button that does not exist"
        text = turn.text
        has_proposal = bool(turn.proposals())
        has_illustration = any(tc.data.get("illustration") for tc in turn.tool_calls if tc.success)
        has_fix_plan = any(tc.name == "apply_format_fixes" and tc.success for tc in turn.tool_calls)
        has_ops = bool(turn.all_ops())
        problems = []
        if "Thay vào văn bản" in text and not has_proposal:
            problems.append("'Thay vào văn bản' without a proposal card")
        if "Chèn vào văn bản" in text and not (has_illustration or has_ops):
            problems.append("'Chèn vào văn bản' without an illustration")
        if "Áp dụng các sửa thể thức" in text and not has_fix_plan:
            problems.append("'Áp dụng các sửa thể thức' without a fix plan")
        return Result(PASS if not problems else status, label, "; ".join(problems))
    return run


def no_tool_loop(max_same: int = 3, max_total: int = 6, status: str = FAIL) -> Check:
    """A loop is the same tool failing `max_same` times with the same
    arguments, or `max_total` times in all (different dead URLs for
    web_fetch are not a loop)."""
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        fails: Dict[str, int] = {}
        same: Dict[str, int] = {}
        for t in turn.tool_calls:
            if t.success is False:
                fails[t.name] = fails.get(t.name, 0) + 1
                key = t.name + ":" + json.dumps(t.arguments, sort_keys=True, ensure_ascii=False)
                same[key] = same.get(key, 0) + 1
        bad_total = {k: v for k, v in fails.items() if v >= max_total}
        bad_same = {k[:60]: v for k, v in same.items() if v >= max_same}
        label = f"no tool loop (same call failing >= {max_same}x or >= {max_total} failures)"
        if bad_total or bad_same:
            return Result(status, label, f"same-args {bad_same} total {fails}")
        return Result(PASS, label, f"failures {fails}" if fails else "")
    return run


def brief_when_background_started(max_chars: int = 500, status: str = FAIL) -> Check:
    """When check_document_format answers 'vừa bắt đầu ở chế độ nền', the
    prompt says: reply briefly and stop. No pre-emptive error list."""
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        label = "brief reply when the format check just started in background"
        started = any("vừa bắt đầu ở chế độ nền" in t.output for t in turn.tools("check_document_format"))
        if not started:
            return Result(SKIP, label, "format check did not report 'started'")
        n = len(turn.text)
        return Result(PASS if n <= max_chars else status, label, f"answer {n} chars (limit {max_chars})")
    return run


def no_mermaid_in_answer(status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        bad = "```mermaid" in turn.text or "graph TD" in turn.text or "flowchart" in turn.text
        return Result(PASS if not bad else status, "no inline mermaid in the answer (diagram only via tool)")
    return run


def ungrounded_refusal(status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        ok = turn.text.startswith(UNGROUNDED_PREFIX)
        return Result(PASS if ok else status, "fixed 'không tìm thấy căn cứ' refusal", turn.text[:120])
    return run


def grounded_or_refused(status: str = FAIL) -> Check:
    """Grounding gate: a lookup question must either be answered from tool
    evidence or get the fixed refusal. Answering with no tool call at all = FAIL."""
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        label = "answer grounded in tool evidence or fixed refusal"
        if not turn.tool_calls:
            return Result(status, label, "no tool was called; answered from model memory")
        evidence = has_evidence(turn)
        refused = turn.text.startswith(UNGROUNDED_PREFIX)
        if evidence:
            return Result(PASS, label, f"evidence from {evidence}")
        if refused:
            return Result(PASS, label, "no evidence → fixed refusal")
        return Result(status, label, f"no evidence and no refusal; tools {turn.tool_names()}")
    return run


def has_evidence(turn: Turn) -> List[str]:
    out = []
    for t in turn.tool_calls:
        if t.success is not True:
            continue
        d = t.data
        if t.name in ("search_knowledge", "web_search"):
            cnt = d.get("count")
            try:
                if cnt is not None and int(cnt) > 0:
                    out.append(t.name)
            except (TypeError, ValueError):
                pass
        elif t.name in ("read_document", "search_document_section", "query_knowledge_graph", "web_fetch"):
            if t.output.strip():
                out.append(t.name)
        elif t.name in ("check_legal_status", "find_related_legal_documents"):
            if t.output.strip() and "No document in the legal catalogue matches" not in t.output:
                out.append(t.name)
    return out


# ---- domain-specific ---------------------------------------------------------
def spelling_recall(seeded_key: str = "spelling_errors", doc_key: str = "cong_van_loi.docx",
                    pass_ratio: float = 0.6, warn_ratio: float = 0.3, expected: Optional[Sequence[str]] = None) -> Check:
    """Fraction of the seeded wrong forms reported by check_spelling.

    Matches on findings[].wrong (a word of the seeded phrase) and, when no
    findings data exists, on the answer text. Mark anchors are ignored since
    they quote whole paragraphs."""
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        pairs = list(expected or [w for w, _ in ctx["manifest"][doc_key][seeded_key]])
        wrongs: List[str] = []
        for t in turn.tools("check_spelling") + turn.tools("check_document_format"):
            for f in t.data.get("findings") or []:
                if isinstance(f, dict) and f.get("wrong"):
                    wrongs.append(str(f["wrong"]).lower())
        blob = turn.text.lower()
        found = []
        for w in pairs:
            tokens = {tok.lower() for tok in w.split()}
            if any(x in tokens or w.lower() in x for x in wrongs) or (not wrongs and w.lower() in blob):
                found.append(w)
        ratio = len(found) / max(1, len(pairs))
        label = f"spelling recall >= {pass_ratio:.0%} of seeded errors"
        detail = f"{len(found)}/{len(pairs)} found: {found}; missed {[w for w in pairs if w not in found]}"
        if ratio >= pass_ratio:
            return Result(PASS, label, detail)
        if ratio >= warn_ratio:
            return Result(WARN, label, detail)
        return Result(FAIL, label, detail)
    return run


def no_vietnamese_in_image_prompt(status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        calls = turn.tools("generate_illustration")
        if not calls:
            return Result(SKIP, "image prompt has no Vietnamese letters", "generate_illustration not called")
        bad = [c.arguments.get("prompt") for c in calls if VIET_CHARS_RE.search(str(c.arguments.get("prompt", "")))]
        return Result(PASS if not bad else status, "image prompt has no Vietnamese letters", f"bad prompts {bad}")
    return run


def mermaid_real_newlines(status: str = WARN) -> Check:
    """Diagnostic: the model sometimes sends the mermaid source with a literal
    backslash-n instead of line breaks; the tool then sees one line and
    refuses ('ít hơn hai thành phần')."""
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        label = "draw_diagram mermaid uses real line breaks (not literal \\n)"
        calls = turn.tools("draw_diagram")
        if not calls:
            return Result(SKIP, label, "draw_diagram not called")
        bad = [c for c in calls if "\\n" in str(c.arguments.get("mermaid", "")) and "\n" not in str(c.arguments.get("mermaid", ""))]
        return Result(PASS if not bad else status, label, f"{len(bad)}/{len(calls)} calls sent literal \\n")
    return run


def mermaid_has_nodes(minimum: int, needles: Sequence[str] = (), status: str = FAIL) -> Check:
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        label = f"draw_diagram mermaid has >= {minimum} elements" + (f" and mentions {list(needles)}" if needles else "")
        calls = [c for c in turn.tools("draw_diagram") if c.success]
        if not calls:
            return Result(status, label, "no successful draw_diagram")
        best = None
        for c in calls:
            mm = c.arguments.get("mermaid") or (c.data.get("illustration") or {}).get("mermaid") or ""
            best = mm if best is None or len(mm) > len(best) else best
        lines = [l for l in (best or "").splitlines()[1:] if l.strip()]
        miss = [n for n in needles if fold(n) not in fold(best or "")]
        ok = len(lines) >= minimum and not miss
        return Result(PASS if ok else status, label, f"{len(lines)} lines; missing {miss}")
    return run


def custom(label: str, fn: Callable[[Turn, Dict[str, Any]], Any], status: str = FAIL) -> Check:
    """fn returns True/False or (bool, detail)."""
    def run(turn: Turn, ctx: Dict[str, Any]) -> Result:
        out = fn(turn, ctx)
        detail = ""
        if isinstance(out, tuple):
            out, detail = out
        return Result(PASS if out else status, label, str(detail))
    return run
