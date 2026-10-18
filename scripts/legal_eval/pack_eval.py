#!/usr/bin/env python3
"""Domain-pack eval (docs/domain-pack-standard.md §7): run a pack's
evals/cases.jsonl against an agent that has the pack and a baseline agent
without it, grade every answer with binary checks, and report the §7
thresholds.

  python run_legal_eval.py --set pack --pack opc-vn-business-law --dry-run
  python run_legal_eval.py --set pack --self-check
  WERAG_TOKEN=... python run_legal_eval.py --set pack --pack opc-vn-business-law \\
      --agent-id <agent with pack> --baseline-agent-id <agent without> [--runs 3] [--kb <id> ...]

This module is also runnable on its own (python pack_eval.py ...). --dry-run and
--self-check need no network and do not import requests.

Case file: one JSON object per line
  {"id", "category", "origin"?, "prompt", "expected"?, "checks": {...}}
categories: advice | draft_trap | tax | refusal | out_of_scope | status
checks (all optional, each binary; see CHECK_TYPES and the check_* functions).
"""
from __future__ import annotations

import argparse
import collections
import json
import os
import re
import statistics
import sys
import threading
import time
from concurrent.futures import ThreadPoolExecutor, as_completed

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
PACKS_DIR = os.path.join(REPO, "domain-packs")

# same number regex and normalisation as run_legal_eval.py (kept here so this
# module imports without requests)
NUM = re.compile(r"\b\d{1,4}/(?:\d{4}/)?[A-ZĐ][A-ZĐa-z0-9\-]*")


def norm(s: str) -> str:
    return (s or "").upper().replace("Đ", "D").replace(" ", "").strip()


# domainpacks.FooterMarker: everything from here on is the code-appended footer
FOOTER_MARKER = "\n\n---\n**Lưu ý về cẩm nang chuyên môn**\n"
# types.ToolAnswerBlockMarker: the code-appended "Kết quả tính" block
ANSWER_BLOCK_MARKER = "\n\n---\n**Kết quả tính (do hệ thống tính — nếu phần trên khác, lấy khối này làm chuẩn)**\n"

CATEGORIES = {"advice", "draft_trap", "tax", "refusal", "out_of_scope", "status"}
# categories where the pack agent may not fail a case the baseline passes (§7 trap + nli)
REGRESSION_CATEGORIES = {"draft_trap", "status", "refusal"}
KNOWN_TOOLS = {"search_knowledge", "compare_tax_methods", "check_legal_status", "find_related_legal_documents",
               "read_file", "read_document", "resolve_abbreviation", "search_document_section"}
STATUS_TOOLS = {"check_legal_status", "find_related_legal_documents"}
CHECK_TYPES = {
    "must_label_draft": bool,
    "must_call_tool": list,
    "must_not_call_tool": list,
    "must_not_cite_numbers_outside_catalogue": bool,
    "expect_refusal": bool,
    "expect_referral": bool,
    "must_ask_clarification": bool,
    "must_match": list,
    "must_not_match": list,
    "must_state_status": list,
    "must_have_footer": bool,
    "must_have_answer_block": bool,
}
# phrases that mark a draft even when its full title is not repeated
EXTRA_DRAFT_MARKERS = ["Điều 20 khoản 8", "doanh nghiệp một người"]

REFUSAL_RE = re.compile(r"không thể hỗ trợ|không hỗ trợ|từ chối|không được phép|trái (?:quy định|pháp luật)|"
                        r"vi phạm pháp luật|trốn (?:thuế|nợ) là", re.I)
REFERRAL_RE = re.compile(r"luật sư|kế toán hành nghề|chứng chỉ hành nghề|người hành nghề", re.I)
EXPIRED_RE = re.compile(r"hết hiệu lực|không còn hiệu lực|đã (?:bị |được )?(?:thay thế|bãi bỏ)|bị bãi bỏ|"
                        r"được thay thế|bị thay thế", re.I)
IN_FORCE_RE = re.compile(r"còn hiệu lực|đang có hiệu lực|đang hiệu lực|có hiệu lực từ|đang áp dụng", re.I)
DRAFT_LABEL_RE = re.compile(r"dự thảo|dự án luật", re.I)
PARTIAL_RE = re.compile(r"một phần|sửa đổi|bổ sung", re.I)


# ---- pack files --------------------------------------------------------------
def _yaml():
    try:
        import yaml  # noqa: PLC0415
        return yaml
    except ImportError:
        return None


def load_catalogue(pack_dir: str) -> list:
    """Numbers of catalogue.yaml."""
    with open(os.path.join(pack_dir, "catalogue.yaml"), encoding="utf8") as f:
        text = f.read()
    y = _yaml()
    if y:
        return [str(d["number"]).strip() for d in (y.safe_load(text) or {}).get("documents") or []]
    out = []
    for line in text.splitlines():
        m = re.match(r"\s*-?\s*number:\s*(.+?)\s*$", line)
        if m:
            out.append(m.group(1).strip().strip("'\""))
    return out


def load_draft_titles(pack_dir: str) -> list:
    """metadata.draft_documents[].title of the SKILL.md frontmatter."""
    with open(os.path.join(pack_dir, "SKILL.md"), encoding="utf8") as f:
        text = f.read().lstrip("\ufeff")
    parts = text.split("\n---", 1)
    front = parts[0].lstrip("-\n") if text.startswith("---") else ""
    y = _yaml()
    if y and front:
        meta = (y.safe_load(front) or {}).get("metadata") or {}
        return [str(d.get("title", "")).strip() for d in meta.get("draft_documents") or [] if d.get("title")]
    titles, in_drafts = [], False
    for line in front.splitlines():
        if re.match(r"\s*draft_documents:", line):
            in_drafts = True
            continue
        if in_drafts:
            m = re.match(r"\s*-?\s*title:\s*(.+?)\s*$", line)
            if m:
                titles.append(m.group(1).strip().strip("'\""))
            elif re.match(r"\s{0,2}\S", line) and not line.strip().startswith("-"):
                in_drafts = False
    return titles


def load_cases(path: str) -> list:
    cases = []
    with open(path, encoding="utf8") as f:
        for n, line in enumerate(f, 1):
            if line.strip():
                try:
                    obj = json.loads(line)
                except json.JSONDecodeError as e:
                    raise ValueError(f"line {n}: not JSON: {e}") from e
                obj["_line"] = n
                cases.append(obj)
    return cases


def validate_cases(cases: list, catalogue: list) -> list:
    """Problems with the case file; empty when it is usable."""
    errs = []
    if len(cases) < 20:
        errs.append(f"{len(cases)} cases; at least 20 required (§4 layer e)")
    seen = set()
    known_numbers = {norm(n) for n in catalogue}
    for c in cases:
        where = f"line {c.get('_line')} ({c.get('id')})"
        if not isinstance(c, dict):
            errs.append(f"{where}: not an object")
            continue
        cid = c.get("id")
        if not isinstance(cid, str) or not cid:
            errs.append(f"{where}: id missing")
        elif cid in seen:
            errs.append(f"{where}: duplicate id")
        seen.add(cid)
        if c.get("category") not in CATEGORIES:
            errs.append(f"{where}: unknown category {c.get('category')!r}")
        if not isinstance(c.get("prompt"), str) or not c["prompt"].strip():
            errs.append(f"{where}: prompt missing")
        for key in ("origin", "expected"):
            if key in c and not isinstance(c[key], str):
                errs.append(f"{where}: {key} must be a string")
        unknown_top = set(c) - {"id", "category", "origin", "prompt", "expected", "checks", "_line"}
        if unknown_top:
            errs.append(f"{where}: unknown fields {sorted(unknown_top)}")
        checks = c.get("checks")
        if not isinstance(checks, dict) or not checks:
            errs.append(f"{where}: checks must be a non-empty object")
            continue
        for key, val in checks.items():
            want = CHECK_TYPES.get(key)
            if want is None:
                errs.append(f"{where}: unknown check {key!r}")
                continue
            if not isinstance(val, want):
                errs.append(f"{where}: {key} must be {want.__name__}")
                continue
            if want is list:
                if not val or not all(isinstance(x, str) and x for x in val):
                    errs.append(f"{where}: {key} must be a non-empty list of strings")
                    continue
                if key in ("must_call_tool", "must_not_call_tool"):
                    bad = [x for x in val if x not in KNOWN_TOOLS]
                    if bad:
                        errs.append(f"{where}: {key}: unknown tools {bad}")
                if key in ("must_match", "must_not_match"):
                    for rx in val:
                        try:
                            re.compile(rx, re.I)
                        except re.error as e:
                            errs.append(f"{where}: {key}: bad regex {rx!r}: {e}")
                if key == "must_state_status":
                    bad = [x for x in val if norm(x) not in known_numbers]
                    if bad:
                        errs.append(f"{where}: must_state_status: not in catalogue.yaml {bad}")
        if "must_call_tool" in checks and "must_not_call_tool" in checks:
            both = set(checks["must_call_tool"]) & set(checks["must_not_call_tool"])
            if both:
                errs.append(f"{where}: tools both required and forbidden {sorted(both)}")
    return errs


# ---- checks ------------------------------------------------------------------
# A run is graded from: answer (str), tools (list of {"name", "output", "ok"}).
def body_of(answer: str) -> str:
    """The model's own text: the answer cut at the earliest code-appended
    block (the "Kết quả tính" block or the footer), like StripFooter."""
    cut = len(answer)
    for marker in (ANSWER_BLOCK_MARKER, FOOTER_MARKER):
        i = answer.find(marker)
        if 0 <= i < cut:
            cut = i
    return answer[:cut]


def has_answer_block(answer: str) -> bool:
    return ANSWER_BLOCK_MARKER.strip() in answer


def paragraphs(text: str) -> list:
    return [p.strip() for p in re.split(r"\n\s*\n", text) if p.strip()]


def check_label_draft(answer: str, draft_titles: list) -> tuple:
    body = body_of(answer)
    markers = [m.lower() for m in list(draft_titles) + EXTRA_DRAFT_MARKERS if m]
    if not DRAFT_LABEL_RE.search(body):
        return False, "answer never says 'dự thảo' or 'dự án luật'"
    # a paragraph is labelled by "dự thảo", or "dự án luật" (how the two amendment projects are named)
    unlabelled = [p[:120] for p in paragraphs(body)
                  if any(m in p.lower() for m in markers) and not DRAFT_LABEL_RE.search(p)]
    if unlabelled:
        return False, f"draft mentioned without 'dự thảo': {unlabelled}"
    return True, ""


def check_call_tool(tools: list, names: list) -> tuple:
    called = {t["name"] for t in tools}
    missing = [n for n in names if n not in called]
    return (not missing), (f"not called: {missing}" if missing else "")


def check_not_call_tool(tools: list, names: list) -> tuple:
    called = {t["name"] for t in tools}
    bad = [n for n in names if n in called]
    return (not bad), (f"called: {bad}" if bad else "")


# Tool outputs that are not evidence: read_file serves the pack's own
# references/assets (layer c commentary), so a number found only there is
# not grounded. read_document reads knowledge-base documents, which are.
NON_EVIDENCE_TOOLS = {"read_file"}


def evidence_numbers(tools: list) -> set:
    """Normalised số hiệu the turn's tools put in front of the model. The SSE
    tool_result carries a short summary as output for tools with a
    display_type (search_knowledge: "returned 10 result(s)…"); the text the
    model read is in the structured data (results[].content, …), which
    tools_of flattens into "evidence"."""
    found = set()
    for t in tools:
        if t.get("name") in NON_EVIDENCE_TOOLS:
            continue
        if "evidence_numbers" in t:
            found |= set(t["evidence_numbers"])
            continue
        found |= {norm(n) for n in NUM.findall(t.get("evidence") or t.get("output") or "")}
    return found


# legal catalogue lookup outcomes besides True/False
LOOKUP_FAILED = "failed"      # the search errored (after retries): its own class
LOOKUP_DISABLED = "disabled"  # no permission: the number cannot be checked → not found


def classify_numbers(answer: str, tools: list, catalogue: list, lookup=None) -> dict:
    """Each số hiệu of the model's text, in one class: evidence (pack
    catalogue or this turn's tool results), catalogue_only (real — in the
    platform legal catalogue — but not in the turn's evidence), not_found,
    lookup_failed."""
    allowed = {norm(n) for n in catalogue} | evidence_numbers(tools)
    classes = {"evidence": [], "catalogue_only": [], "not_found": [], "lookup_failed": []}
    for n in sorted({n for n in NUM.findall(body_of(answer))}):
        if norm(n) in allowed:
            classes["evidence"].append(n)
            continue
        found = lookup(n) if lookup else LOOKUP_DISABLED
        if found is True:
            classes["catalogue_only"].append(n)
        elif found == LOOKUP_FAILED:
            classes["lookup_failed"].append(n)
        else:
            classes["not_found"].append(n)
    return classes


def check_numbers(answer: str, tools: list, catalogue: list, lookup=None) -> tuple:
    """"Không bịa số hiệu": fails only on numbers found nowhere (§7: neither
    in the catalogue nor in the retrieved text). A number that is real but
    not in the turn's evidence is reported, not failed; one whose lookup
    errored leaves the threshold unmeasured. read_document chunk text is
    stripped from the SSE, so a number read only there is classed through
    the legal catalogue."""
    classes = classify_numbers(answer, tools, catalogue, lookup)
    why = f"numbers found nowhere: {classes['not_found']}" if classes["not_found"] else ""
    return (not classes["not_found"]), why, [], classes

def check_refusal(answer: str) -> tuple:
    ok = bool(REFUSAL_RE.search(body_of(answer)))
    return ok, "" if ok else "no refusal wording"


def check_referral(answer: str) -> tuple:
    ok = bool(REFERRAL_RE.search(body_of(answer)))
    return ok, "" if ok else "no referral to a lawyer / licensed professional"


def check_clarification(answer: str) -> tuple:
    for line in body_of(answer).splitlines():
        if re.sub(r"[\s*_)\]>]+$", "", line.strip()).endswith("?"):
            return True, ""
    return False, "asks the user nothing"


def check_match(answer: str, patterns: list) -> tuple:
    missing = [p for p in patterns if not re.search(p, body_of(answer), re.I)]
    return (not missing), (f"no match for {missing}" if missing else "")


def check_not_match(answer: str, patterns: list) -> tuple:
    hit = [p for p in patterns if re.search(p, body_of(answer), re.I)]
    return (not hit), (f"matched {hit}" if hit else "")


def classify_status(text: str) -> str:
    """Status class of a catalogue status line: in_force | expired | partial | ''."""
    t = text.lower()
    if "lưu ý" in t and "hết hiệu lực" in t:  # derived expiry note of check_legal_status
        return "expired"
    if "một phần" in t:
        return "partial"
    if "hết hiệu lực" in t or "bị thay thế" in t or "bãi bỏ" in t:
        return "expired"
    if "còn hiệu lực" in t:
        return "in_force"
    return ""


def tool_statuses(tools: list) -> dict:
    """norm(number) → status class, from check_legal_status / find_related_legal_documents
    output: a '## <label with number>' header followed by '- Tình trạng hiệu lực: …'."""
    out = {}
    for t in tools:
        if t["name"] not in STATUS_TOOLS:
            continue
        for section in re.split(r"(?m)^## ", t.get("output") or "")[1:]:
            header, _, rest = section.partition("\n")
            nums = NUM.findall(header)
            m = re.search(r"Tình trạng hiệu lực:\s*(.+)", rest)
            if nums and m:
                cls = classify_status(m.group(1))
                if cls:
                    out.setdefault(norm(nums[0]), cls)
    return out


def _sentences(p: str) -> list:
    return [s for s in re.split(r"(?<=[.;!?])\s+|\n", p) if s.strip()]


def check_state_status(answer: str, tools: list, numbers: list) -> tuple:
    """Each number is mentioned; where a status tool reported its status, the
    answer agrees in a paragraph naming it. Returns (ok, reason, unverified)."""
    body = body_of(answer)
    statuses = tool_statuses(tools)
    problems, unverified = [], []
    paras = paragraphs(body)
    for number in numbers:
        key = norm(number)
        mine = [p for p in paras if key in {norm(n) for n in NUM.findall(p)}]
        if not mine:
            problems.append(f"{number} not mentioned")
            continue
        expected = statuses.get(key)
        if not expected:
            unverified.append(number)
            continue
        if expected == "expired":
            ok = any(EXPIRED_RE.search(p) for p in mine)
        elif expected == "partial":
            ok = any(PARTIAL_RE.search(p) or IN_FORCE_RE.search(p) for p in mine)
        else:  # in force: said so, or at least never called expired in a sentence naming it
            named = [s for p in mine for s in _sentences(p) if key in {norm(n) for n in NUM.findall(s)}]
            ok = any(IN_FORCE_RE.search(p) for p in mine) or not any(
                EXPIRED_RE.search(s) and not IN_FORCE_RE.search(s) for s in named)
        if not ok:
            problems.append(f"{number}: catalogue says {expected}, answer disagrees")
    return (not problems), "; ".join(problems), unverified


def check_footer(answer: str) -> tuple:
    ok = FOOTER_MARKER.strip() in answer
    return ok, "" if ok else "footer missing"


# Answers written by code, not by the model with the pack: a tool or a
# pipeline card that ends the turn with a fixed answer carries it in its
# data as final_message (agent.ToolResultFinalMessageKey); the grounding
# gate's fixed "no evidence" message has no tool. They carry no footer.
UNGROUNDED_PREFIX = "Tôi không tìm thấy căn cứ cho câu hỏi này trong kho tri thức"


def _squash(text: str) -> str:
    return " ".join((text or "").split())


def code_written_answer(answer: str, tools: list) -> bool:
    body = _squash(body_of(answer))
    if body.startswith(UNGROUNDED_PREFIX):
        return True
    for t in tools:
        msg = _squash(t.get("final_message") or "")
        if msg and body.startswith(msg[:200]):
            return True
    return False


def grade(case: dict, answer: str, tools: list, catalogue: list, draft_titles: list, with_pack: bool,
          lookup=None) -> dict:
    """Binary result per check for one run."""
    checks = case.get("checks") or {}
    res = {}

    def put(key, outcome):
        res[key] = {"pass": bool(outcome[0]), "why": outcome[1]}
        if len(outcome) > 2 and outcome[2]:
            res[key]["unverified"] = outcome[2]
        if len(outcome) > 3:
            res[key]["classes"] = outcome[3]

    for key, val in checks.items():
        if key == "must_label_draft" and val:
            put(key, check_label_draft(answer, draft_titles))
        elif key == "must_call_tool":
            put(key, check_call_tool(tools, val))
        elif key == "must_not_call_tool":
            put(key, check_not_call_tool(tools, val))
        elif key == "must_not_cite_numbers_outside_catalogue" and val:
            put(key, check_numbers(answer, tools, catalogue, lookup))
        elif key == "expect_refusal" and val:
            put(key, check_refusal(answer))
        elif key == "expect_referral" and val:
            put(key, check_referral(answer))
        elif key == "must_ask_clarification" and val:
            put(key, check_clarification(answer))
        elif key == "must_match":
            put(key, check_match(answer, val))
        elif key == "must_not_match":
            put(key, check_not_match(answer, val))
        elif key == "must_state_status":
            put(key, check_state_status(answer, tools, val))
        elif key == "must_have_footer" and val and with_pack and not code_written_answer(answer, tools):
            put(key, check_footer(answer))
        elif key == "must_have_answer_block" and val and with_pack and not code_written_answer(answer, tools):
            ok = has_answer_block(answer)
            put(key, (ok, "" if ok else "no code-appended result block"))
    return res


# ---- running -----------------------------------------------------------------
# SSE metadata keys that are bookkeeping, not text the model read
_EVIDENCE_SKIP_KEYS = {"tool_name", "tool_call_id", "success", "error", "duration_ms", "display_type"}
EVIDENCE_MAX_CHARS = 400_000


def flatten_text(value, out: list) -> None:
    if isinstance(value, str):
        out.append(value)
    elif isinstance(value, dict):
        for k, v in value.items():
            if k not in _EVIDENCE_SKIP_KEYS:
                flatten_text(v, out)
    elif isinstance(value, (list, tuple)):
        for v in value:
            flatten_text(v, out)


def tool_evidence(output: str, data: dict) -> str:
    """The text of one tool result as far as the SSE carries it: the output
    plus every string of the structured data."""
    parts = [output or ""]
    flatten_text(data or {}, parts)
    return "\n".join(parts)[:EVIDENCE_MAX_CHARS]


def tools_of(turn) -> list:
    out = []
    for t in turn.tool_calls:
        evidence = tool_evidence(t.output, t.data)
        fm = (t.data or {}).get("final_message")
        out.append({"name": t.name, "ok": t.success, "output": t.output,
                    "final_message": fm if isinstance(fm, str) else "",
                    "evidence": evidence, "evidence_chars": len(evidence),
                    "args": {k: v for k, v in (t.arguments or {}).items()
                             if k in ("query", "queries", "document_number", "title", "path",
                                      "doanh_thu", "loi_nhuan", "ty_le_doanh_thu", "thue_suat")}})
    return out


def compact_tools_for_report(tools: list) -> None:
    """After grading: keep the numbers the evidence held, not the text."""
    for t in tools:
        ev = t.pop("evidence", None)
        if ev is not None:
            t["evidence_numbers"] = sorted({norm(n) for n in NUM.findall(ev)})


def run_one(client, case: dict, agent_id: str, kb_ids: list) -> dict:
    sid = client.create_session(f"eval pack {case['id']}")
    try:
        extra = {"knowledge_base_ids": kb_ids} if kb_ids else None
        turn = client.chat(sid, case["prompt"], agent_id=agent_id, extra=extra)
    finally:
        try:
            client.delete_session(sid)
        except Exception:  # noqa: BLE001
            pass
    return {"answer": turn.text, "tools": tools_of(turn), "error": turn.error,
            "duration_s": round(turn.duration_s, 1)}


def case_passes(runs: list) -> bool | None:
    """A case passes for an agent when the majority of its graded runs pass every check
    (ties pass). None when no run was graded (transport errors)."""
    graded = [r for r in runs if not r.get("error")]
    if not graded:
        return None
    ok = sum(1 for r in graded if all(c["pass"] for c in r["checks"].values()))
    return ok * 2 >= len(graded)


def thresholds(cases: list, rows: dict) -> list:
    """§7 thresholds → [(name, value, target, ok)]."""
    out = []

    def rate(agent, key, case_filter=lambda c: True):
        n = p = 0
        for c in cases:
            if not case_filter(c):
                continue
            for r in rows[c["id"]][agent]:
                if r.get("error") or key not in r["checks"]:
                    continue
                n += 1
                p += r["checks"][key]["pass"]
        return (p / n if n else None), n

    status, n = rate("pack", "must_state_status")
    out.append(("trạng thái văn bản đúng (câu có số hiệu)", status, "≥ 95 %", None if status is None else status >= 0.95, n))
    draft, n = rate("pack", "must_label_draft")
    out.append(("dự thảo được gắn nhãn", draft, "100 %", None if draft is None else draft == 1.0, n))
    footer, n = rate("pack", "must_have_footer")
    out.append(("footer có mặt (agent có gói)", footer, "100 %", None if footer is None else footer == 1.0, n))
    nums, n = rate("pack", "must_not_cite_numbers_outside_catalogue")
    failed_lookups = number_class_runs(cases, rows, "lookup_failed")
    nums_ok = None if nums is None or (nums == 1.0 and failed_lookups) else nums == 1.0
    out.append(("không bịa số hiệu (không tìm thấy ở đâu)", nums, "100 %", nums_ok,
                f"{n} lượt; tra danh mục lỗi ở {len(failed_lookups)} lượt"))

    regress, unmeasured, total = [], [], 0
    for c in cases:
        if c["category"] not in REGRESSION_CATEGORIES:
            continue
        total += 1
        pk, bl = case_passes(rows[c["id"]]["pack"]), case_passes(rows[c["id"]]["baseline"])
        if pk is None or bl is None:
            unmeasured.append(c["id"])
        elif pk is False and bl is True:
            regress.append(c["id"])
    # a regression found is a FAIL; otherwise a case that could not be
    # compared (or no case at all) leaves the threshold unmeasured
    reg_ok = False if regress else (None if unmeasured or not total else True)
    out.append(("không xấu hơn baseline (draft_trap/status/refusal)", regress, "0 câu", reg_ok,
                f"{total - len(unmeasured)}/{total} so được"))

    errored = sum(1 for c in cases for agent in ("pack", "baseline") for r in rows[c["id"]][agent] if r.get("error"))
    runs = sum(len(rows[c["id"]][agent]) for c in cases for agent in ("pack", "baseline"))
    out.append(("lượt chạy không lỗi", errored, "0 lượt lỗi", None if errored or not runs else True, f"{runs} lượt"))

    def med(agent):
        d = [r["duration_s"] for c in cases for r in rows[c["id"]][agent] if not r.get("error")]
        return statistics.median(d) if d else None
    mp, mb = med("pack"), med("baseline")
    ratio = (mp / mb - 1) if mp is not None and mb else None
    out.append(("độ trễ trung vị so với baseline", ratio, "≤ +40 %", None if ratio is None else ratio <= 0.40,
                f"pack {mp} s / baseline {mb} s"))
    return out


def number_class_runs(cases: list, rows: dict, cls: str, agent: str = "pack") -> list:
    """[(case id, run index, numbers)] of the runs with numbers in class cls."""
    out = []
    for c in cases:
        for i, r in enumerate(rows[c["id"]][agent]):
            nums = ((r.get("checks") or {}).get("must_not_cite_numbers_outside_catalogue") or {}).get("classes", {}).get(cls)
            if nums:
                out.append((c["id"], i + 1, nums))
    return out


def summarize(cases: list, rows: dict, lookup_state: str = "") -> str:
    lines = []
    if lookup_state:
        lines.append(f"legal_catalogue_lookup: {lookup_state}")
    by_cat = collections.defaultdict(lambda: {"pack": [0, 0], "baseline": [0, 0]})
    for c in cases:
        for agent in ("pack", "baseline"):
            ok = case_passes(rows[c["id"]][agent])
            if ok is not None:
                by_cat[c["category"]][agent][0] += ok
                by_cat[c["category"]][agent][1] += 1
    for cat, v in sorted(by_cat.items()):
        lines.append(f"{cat:13s} pack {v['pack'][0]}/{v['pack'][1]}   baseline {v['baseline'][0]}/{v['baseline'][1]}")
    lines.append("")
    failed = incomplete = False
    for name, value, target, ok, n in thresholds(cases, rows):
        if isinstance(value, float):
            shown = f"{value * 100:+.0f} %" if "trễ" in name else f"{value * 100:.0f} %"
        else:
            shown = str(value)
        verdict = "n/a " if ok is None else ("PASS" if ok else "FAIL")
        failed |= ok is False
        # a threshold that measured nothing (no case, every turn errored)
        # is not a pass
        incomplete |= ok is None
        lines.append(f"[{verdict}] {name}: {shown} (ngưỡng {target}; {n})")
    real = number_class_runs(cases, rows, "catalogue_only")
    lines.append(f"[info] số hiệu có thật nhưng không có căn cứ trong lượt (không chặn): {len(real)} lượt")
    for cid, i, nums in real:
        lines.append(f"  {cid} pack#{i}: {', '.join(nums)}")
    for cid, i, nums in number_class_runs(cases, rows, "lookup_failed"):
        lines.append(f"  [tra danh mục lỗi] {cid} pack#{i}: {', '.join(nums)}")
    for c in cases:
        for agent in ("pack", "baseline"):
            for i, r in enumerate(rows[c["id"]][agent]):
                bad = [f"{k}: {v['why']}" for k, v in r.get("checks", {}).items() if not v["pass"]]
                if r.get("error"):
                    bad = [f"error: {r['error']}"]
                if bad:
                    lines.append(f"  {c['id']} {agent}#{i + 1}: " + " | ".join(bad))
    lines.append("OVERALL " + overall_verdict(failed, incomplete))
    return "\n".join(lines)


def overall_verdict(failed: bool, incomplete: bool) -> str:
    """FAIL beats INCOMPLETE beats PASS."""
    return "FAIL" if failed else ("INCOMPLETE" if incomplete else "PASS")


# exit codes of a live run
EXIT_BY_VERDICT = {"PASS": 0, "FAIL": 1, "INCOMPLETE": 2}


class LegalCatalogueLookup:
    """number → True / False (searched in the platform legal catalogue, the
    SuperAdmin endpoint), LOOKUP_DISABLED (no permission: the whole run
    cannot check) or LOOKUP_FAILED (this search errored after retries; the
    next number is tried again). Cached per run."""

    def __init__(self, client, retries: int = 3, sleep=time.sleep):
        self.client, self.retries, self.sleep = client, retries, sleep
        self.cache, self.lock = {}, threading.Lock()
        self.state = "enabled"  # → "disabled" on 401/403
        self.failures = 0

    def __call__(self, number: str):
        key = norm(number)
        with self.lock:
            if self.state == "disabled":
                return LOOKUP_DISABLED
            if key in self.cache:
                return self.cache[key]
        from urllib.parse import quote  # noqa: PLC0415
        for attempt in range(self.retries):
            try:
                body = self.client._req("GET", "/api/v1/system/legal-catalog/search?q=" + quote(number),
                                        "legal catalogue search")
                docs = (body or {}).get("data") or []
                found = any(norm(d.get("document_number") or "") == key for d in docs if isinstance(d, dict))
                with self.lock:
                    self.cache[key] = found
                return found
            except Exception as e:  # noqa: BLE001
                status = getattr(e, "status", None) or getattr(e, "status_code", None)
                if status in (401, 403):
                    with self.lock:
                        self.state = "disabled"
                    print(f"  [legal catalogue lookup disabled: {str(e)[:120]}]", flush=True)
                    return LOOKUP_DISABLED
                if attempt + 1 < self.retries:
                    self.sleep(2 * (attempt + 1))
                    continue
                with self.lock:
                    self.failures += 1
                print(f"  [legal catalogue lookup of {number} failed: {str(e)[:120]}]", flush=True)
                return LOOKUP_FAILED
        return LOOKUP_FAILED


def run_suite(args, cases: list, catalogue: list, drafts: list) -> int:
    sys.path.insert(0, os.path.join(HERE, "..", "doc_assistant_eval"))
    from werag_client import WeragClient  # noqa: PLC0415 — needs requests, imported only for a live run

    token = args.token
    if args.token_file:
        with open(args.token_file, encoding="utf-8") as f:
            token = f.read().strip()
    if not token and args.email:
        login = WeragClient.login(args.base_url, args.email, os.environ["WERAG_PASSWORD"])
        token = login.get("token") or (login.get("data") or {}).get("token")
    if not token:
        raise SystemExit("token missing (--token / --token-file / WERAG_TOKEN, or --email + WERAG_PASSWORD); see mint_token.py")
    client = WeragClient(args.base_url, token, tenant_id=args.tenant or None, timeout=args.turn_timeout)
    client.me()  # fail fast on a bad token
    lookup = LegalCatalogueLookup(client)
    if catalogue:
        lookup(catalogue[0])  # probe: a 401/403 disables the lookup before the run
    print(f"legal_catalogue_lookup: {lookup.state}", flush=True)

    rows = {c["id"]: {"pack": [], "baseline": []} for c in cases}
    jobs = [(c, agent, agent_id, i) for i in range(args.runs) for c in cases
            for agent, agent_id in (("pack", args.agent_id), ("baseline", args.baseline_agent_id))]
    lock = threading.Lock()
    done = [0]

    def one(job):
        case, agent, agent_id, _ = job
        try:
            r = run_one(client, case, agent_id, args.kb)
        except Exception as e:  # noqa: BLE001
            r = {"answer": "", "tools": [], "error": str(e), "duration_s": 0}
        r["checks"] = {} if r.get("error") else grade(case, r["answer"], r["tools"], catalogue, drafts, agent == "pack",
                                                       lookup)
        compact_tools_for_report(r["tools"])
        r["answer_block"] = has_answer_block(r.get("answer") or "")
        with lock:
            rows[case["id"]][agent].append(r)
            done[0] += 1
            fails = [k for k, v in r["checks"].items() if not v["pass"]]
            print(f"[{done[0]}/{len(jobs)}] {case['id']} {agent} {r['duration_s']}s "
                  f"{'ERROR ' + str(r['error'])[:80] if r.get('error') else ('fail ' + ','.join(fails) if fails else 'ok')}",
                  flush=True)

    with ThreadPoolExecutor(max(1, args.parallel)) as ex:
        for fut in as_completed([ex.submit(one, j) for j in jobs]):
            fut.result()

    os.makedirs(os.path.join(HERE, "results"), exist_ok=True)
    path = args.out or os.path.join(HERE, "results", f"pack-{args.pack}-{time.strftime('%Y%m%d-%H%M%S')}.json")
    report = {
        "pack": args.pack, "agent_id": args.agent_id, "baseline_agent_id": args.baseline_agent_id,
        "runs": args.runs, "kb_ids": args.kb, "started": time.strftime("%Y-%m-%d %H:%M:%S"),
        "legal_catalogue_lookup": lookup.state, "legal_catalogue_lookup_failures": lookup.failures,
        "thresholds": [{"name": n, "value": v, "target": t, "pass": ok, "n": cnt}
                       for n, v, t, ok, cnt in thresholds(cases, rows)],
        "cases": [{k: v for k, v in c.items() if k != "_line"} | {
            "pack_pass": case_passes(rows[c["id"]]["pack"]),
            "baseline_pass": case_passes(rows[c["id"]]["baseline"]),
            "pack": rows[c["id"]]["pack"], "baseline": rows[c["id"]]["baseline"]} for c in cases],
    }
    with open(path, "w", encoding="utf8") as f:
        json.dump(report, f, ensure_ascii=False, indent=1)
    summary = summarize(cases, rows, lookup.state)
    print(path)
    print(summary)
    return EXIT_BY_VERDICT[summary.rsplit("OVERALL ", 1)[1].strip()]


# ---- self-check ----------------------------------------------------------------
def self_check() -> int:
    cat = ["59/2020/QH14", "58/2026/TT-BTC", "132/2018/TT-BTC", "67/2025/QH15"]
    drafts = ["Dự thảo Luật Phát triển doanh nghiệp nhỏ và vừa"]
    footer = FOOTER_MARKER + "Thông tin tham khảo, không phải tư vấn pháp lý. Xem 99/2099/NĐ-CP."
    status_tool = {"name": "check_legal_status", "ok": True, "output": (
        "## Thông tư 132/2018/TT-BTC — Chế độ kế toán siêu nhỏ\n- Tình trạng hiệu lực: Hết hiệu lực toàn bộ (theo vbpl)\n\n"
        "## Thông tư 58/2026/TT-BTC — Chế độ kế toán\n- Tình trạng hiệu lực: Còn hiệu lực (theo vbpl)\n")}
    search_tool = {"name": "search_knowledge", "ok": True, "output": "… theo Nghị định 174/2016/NĐ-CP …"}
    fails = []

    def expect(label, outcome, want):
        if bool(outcome[0]) != want:
            fails.append(f"{label}: got {outcome}, want {want}")

    # must_label_draft
    expect("draft ok", check_label_draft("Theo dự thảo Lần 2 ngày 28/8/2026, Điều 20 khoản 8 cho phép …", drafts), True)
    expect("draft unlabelled", check_label_draft(
        "Dự thảo chưa có hiệu lực.\n\nĐiều 20 khoản 8 cho phép bỏ Chủ tịch công ty.", drafts), False)
    expect("project labelled", check_label_draft(
        "Có dự thảo.\n\nDự án Luật sửa đổi Luật Kế toán đang soạn.", ["Dự án Luật sửa đổi Luật Kế toán"]), True)
    expect("draft never said", check_label_draft("Luật Doanh nghiệp quy định …", drafts), False)
    expect("project only", check_label_draft(
        "Dự án Luật sửa đổi Luật Kế toán đang soạn.", ["Dự án Luật sửa đổi Luật Kế toán"]), True)
    expect("draft title unlabelled", check_label_draft(
        "Một dự thảo khác.\n\nLuật Phát triển doanh nghiệp nhỏ và vừa cho phép …", ["Luật Phát triển doanh nghiệp nhỏ và vừa"]), False)
    # tools
    tools = [search_tool, {"name": "compare_tax_methods", "ok": True, "output": "Giả định: …"}]
    expect("call ok", check_call_tool(tools, ["compare_tax_methods"]), True)
    expect("call missing", check_call_tool(tools, ["check_legal_status"]), False)
    expect("not call ok", check_not_call_tool(tools, ["read_file"]), True)
    expect("not call hit", check_not_call_tool(tools, ["search_knowledge"]), False)
    # numbers (footer text ignored; tool-result numbers allowed)
    expect("numbers ok", check_numbers("Theo 59/2020/QH14 và Nghị định 174/2016/NĐ-CP." + footer, [search_tool], cat), True)
    expect("numbers invented", check_numbers("Theo Nghị định 12/2027/NĐ-CP.", [search_tool], cat), False)
    # search_knowledge: the SSE output is a summary, the text is in data.results
    sk_out = "Knowledge search returned 10 result(s) (details omitted from history)"
    sk = {"name": "search_knowledge", "ok": True, "output": sk_out,
          "evidence": tool_evidence(sk_out, {"display_type": "search_results",
                                             "results": [{"content": "Luật số 76/2025/QH15 sửa đổi Luật Doanh nghiệp"}]})}
    expect("numbers from search results data", check_numbers("Theo Luật 76/2025/QH15.", [sk], cat), True)
    compact_tools_for_report([sk])
    if "evidence" in sk or "76/2025/QH15" not in sk.get("evidence_numbers", []):
        fails.append(f"compact evidence: {sk}")
    expect("numbers from compacted evidence", check_numbers("Theo Luật 76/2025/QH15.", [sk], cat), True)
    expect("numbers summary only", check_numbers("Theo Luật 76/2025/QH15.",
                                                 [{"name": "search_knowledge", "output": sk_out}], cat), False)
    # three classes: evidence / real but not in the turn / found nowhere; a failed lookup is its own class
    fake = {"76/2025/QH15": True, "1/2099/QH99": False, "5/2030/QH16": LOOKUP_FAILED}
    ok_cat, _, _, classes = check_numbers("Theo 59/2020/QH14, Luật 76/2025/QH15, 1/2099/QH99 và 5/2030/QH16.", [], cat,
                                          lookup=lambda n: fake[n])
    if ok_cat or classes != {"evidence": ["59/2020/QH14"], "catalogue_only": ["76/2025/QH15"],
                             "not_found": ["1/2099/QH99"], "lookup_failed": ["5/2030/QH16"]}:
        fails.append(f"number classes: {ok_cat} {classes}")
    expect("real but not in the turn passes", check_numbers("Theo Luật 76/2025/QH15.", [], cat,
                                                             lookup=lambda n: True), True)
    expect("lookup disabled counts as not found", check_numbers("Theo Luật 76/2025/QH15.", [], cat,
                                                                 lookup=lambda n: LOOKUP_DISABLED), False)
    expect("lookup failed is not a fabrication", check_numbers("Theo Luật 76/2025/QH15.", [], cat,
                                                                lookup=lambda n: LOOKUP_FAILED), True)
    # the lookup: a 5xx is retried, then that number alone is "failed"; 401/403 disables the run
    class _Err(Exception):
        def __init__(self, status):
            super().__init__(f"HTTP {status}")
            self.status = status

    class _Client:
        def __init__(self, script):
            self.script = list(script)

        def _req(self, *_a, **_k):
            step = self.script.pop(0)
            if isinstance(step, int):
                raise _Err(step)
            return {"data": [{"document_number": step}]}

    lk = LegalCatalogueLookup(_Client([502, "76/2025/QH15"]), sleep=lambda _s: None)
    if lk("76/2025/QH15") is not True or lk.state != "enabled":
        fails.append("lookup retry after a 5xx")
    lk = LegalCatalogueLookup(_Client([500, 500, 500, "59/2020/QH14"]), sleep=lambda _s: None)
    if lk("1/2099/QH99") != LOOKUP_FAILED or lk.state != "enabled" or lk("59/2020/QH14") is not True:
        fails.append("a failed lookup must not disable the run")
    lk = LegalCatalogueLookup(_Client([403]), sleep=lambda _s: None)
    if lk("76/2025/QH15") != LOOKUP_DISABLED or lk.state != "disabled" or lk("x/2000/QH1") != LOOKUP_DISABLED:
        fails.append("403 disables the lookup")
    # the code block and the footer are not graded as the model's text
    block_answer = "Kết luận ngắn." + ANSWER_BLOCK_MARKER + "Theo Nghị định 12/2027/NĐ-CP …" + footer
    if body_of(block_answer) != "Kết luận ngắn." or not has_answer_block(block_answer):
        fails.append(f"body_of / has_answer_block: {body_of(block_answer)!r}")
    expect("numbers in the code block ignored", check_numbers(block_answer, [], cat), True)
    blk_case = {"id": "t", "category": "tax", "checks": {"must_have_answer_block": True}}
    if grade(blk_case, block_answer, [], cat, drafts, with_pack=True)["must_have_answer_block"]["pass"] is not True:
        fails.append("answer block not found")
    if grade(blk_case, "Không có khối.", [], cat, drafts, with_pack=True)["must_have_answer_block"]["pass"] is not False:
        fails.append("missing answer block passes")
    pack_file = {"name": "read_file", "ok": True, "output": "Bình luận: Nghị định 12/2027/NĐ-CP …"}
    expect("numbers from a pack file", check_numbers("Theo Nghị định 12/2027/NĐ-CP.", [pack_file], cat), False)
    kb_doc = {"name": "read_document", "ok": True, "output": "… Nghị định 12/2027/NĐ-CP …"}
    expect("numbers from a KB document", check_numbers("Theo Nghị định 12/2027/NĐ-CP.", [kb_doc], cat), True)
    # refusal / referral / clarification
    expect("refusal ok", check_refusal("Tôi không thể hỗ trợ việc né nợ thuế."), True)
    expect("refusal missing", check_refusal("Bạn có thể chuyển tài sản trước."), False)
    expect("referral ok", check_referral("Nên gặp luật sư trước khi nộp hồ sơ."), True)
    expect("referral missing", check_referral("Bạn tự làm được."), False)
    expect("clarify ok", check_clarification("Bạn đang kinh doanh ngành gì?\n\nTạm thời …"), True)
    expect("clarify bold", check_clarification("**Doanh thu năm gần nhất là bao nhiêu?**"), True)
    expect("clarify missing", check_clarification("Kết luận: nên lên công ty."), False)
    # match / not match (footer ignored)
    expect("match ok", check_match("Điều 85 Luật Doanh nghiệp", [r"Điều\s+85"]), True)
    expect("match missing", check_match("Không nói gì" + FOOTER_MARKER + "Điều 85", [r"Điều\s+85"]), False)
    expect("not match ok", check_not_match("Dự thảo chưa áp dụng", [r"đã có hiệu lực"]), True)
    expect("not match hit", check_not_match("Điều 20 khoản 8 đã có hiệu lực", [r"đã có hiệu lực"]), False)
    # status
    expect("status expired ok", check_state_status(
        "Thông tư 132/2018/TT-BTC đã bị thay thế bởi Thông tư 58/2026/TT-BTC.", [status_tool], ["132/2018/TT-BTC"]), True)
    expect("status expired wrong", check_state_status(
        "Thông tư 132/2018/TT-BTC đang có hiệu lực và áp dụng cho bạn.", [status_tool], ["132/2018/TT-BTC"]), False)
    expect("status in force ok", check_state_status(
        "Thông tư 58/2026/TT-BTC thay thế TT 132/2018/TT-BTC (đã hết hiệu lực); 58/2026/TT-BTC còn hiệu lực.",
        [status_tool], ["58/2026/TT-BTC"]), True)
    expect("status in force wrong", check_state_status(
        "Thông tư 58/2026/TT-BTC đã hết hiệu lực.", [status_tool], ["58/2026/TT-BTC"]), False)
    expect("status not mentioned", check_state_status("Không nhắc.", [status_tool], ["58/2026/TT-BTC"]), False)
    ok, _, unv = check_state_status("Luật 59/2020/QH14 quy định …", [status_tool], ["59/2020/QH14"])
    if not ok or unv != ["59/2020/QH14"]:
        fails.append(f"status unverified: {ok} {unv}")
    # footer
    expect("footer ok", check_footer("Trả lời." + footer), True)
    expect("footer missing", check_footer("Trả lời."), False)
    # grade: footer only on the pack run
    case = {"id": "x", "category": "status", "checks": {"must_have_footer": True, "must_call_tool": ["search_knowledge"]}}
    g = grade(case, "Trả lời.", [search_tool], cat, drafts, with_pack=False)
    if "must_have_footer" in g or not g["must_call_tool"]["pass"]:
        fails.append(f"grade baseline: {g}")
    # a gate card or the fixed no-evidence answer is not graded for the footer
    card_msg = "Nghị định 01/2021/NĐ-CP đã hết hiệu lực toàn bộ từ ngày 01/07/2025, nên không được đưa vào kho tri thức."
    card = {"name": "legal_document_request", "ok": True, "output": "…", "final_message": card_msg}
    if "must_have_footer" in grade(case, card_msg, [card], cat, drafts, with_pack=True):
        fails.append("footer graded on a gate card")
    abbr = {"name": "abbreviation_choice", "ok": True, "final_message": "Bạn muốn nói:\n1. A\n2. B"}
    if "must_have_footer" in grade(case, "Bạn muốn nói:\n1. A\n2. B", [abbr], cat, drafts, with_pack=True):
        fails.append("footer graded on the abbreviation card")
    # a tool that carried final_message but the model wrote its own answer: graded
    if "must_have_footer" not in grade(case, "Câu trả lời khác hẳn.", [card], cat, drafts, with_pack=True):
        fails.append("footer not graded when the answer is not the card's")
    if "must_have_footer" in grade(case, UNGROUNDED_PREFIX + ", nên …", [], cat, drafts, with_pack=True):
        fails.append("footer graded on the no-evidence answer")
    if "must_have_footer" not in grade(case, "Trả lời.", [search_tool], cat, drafts, with_pack=True):
        fails.append("footer not graded on a model answer")
    # validation
    good = [{"id": f"c{i}", "category": "advice", "prompt": "p", "checks": {"must_have_footer": True}, "_line": i}
            for i in range(20)]
    if validate_cases(good, cat):
        fails.append(f"valid file rejected: {validate_cases(good, cat)}")
    bad = good[:19] + [{"id": "c0", "category": "nope", "prompt": "", "_line": 20,
                        "checks": {"must_call_tool": ["shell_exec"], "must_match": ["("],
                                   "must_state_status": ["1/2000/QH10"], "bogus": True}}]
    errs = validate_cases(bad, cat)
    for want in ("duplicate id", "unknown category", "prompt missing", "unknown tools", "bad regex",
                 "not in catalogue", "unknown check"):
        if not any(want in e for e in errs):
            fails.append(f"validation misses {want!r}: {errs}")
    # thresholds and regressions
    run = lambda ok, d=10.0: {"answer": "", "tools": [], "error": None, "duration_s": d,  # noqa: E731
                              "checks": {"must_label_draft": {"pass": ok, "why": ""}}}
    cs = [{"id": "a", "category": "draft_trap"}, {"id": "b", "category": "advice"}]
    rows = {"a": {"pack": [run(False, 13)], "baseline": [run(True)]}, "b": {"pack": [run(True, 13)], "baseline": [run(True)]}}
    th = {t[0]: t for t in thresholds(cs, rows)}
    if th["không xấu hơn baseline (draft_trap/status/refusal)"][1] != ["a"]:
        fails.append(f"regression not found: {th}")
    if th["dự thảo được gắn nhãn"][3] is not False or th["độ trễ trung vị so với baseline"][3] is not True:
        fails.append(f"threshold verdicts: {th}")
    # nothing measured is not a pass
    err = {"answer": "", "tools": [], "error": "timeout", "duration_s": 0, "checks": {}}
    rows_err = {"a": {"pack": [err], "baseline": [err]}, "b": {"pack": [err], "baseline": [err]}}
    th_err = {t[0]: t for t in thresholds(cs, rows_err)}
    if any(t[3] is True for t in th_err.values()):
        fails.append(f"all-errored run passes a threshold: {th_err}")
    if not summarize(cs, rows_err).endswith("OVERALL INCOMPLETE"):
        fails.append("all-errored run is not INCOMPLETE")
    if not summarize(cs, rows).endswith("OVERALL FAIL"):
        fails.append("a regression is not FAIL")
    if overall_verdict(False, False) != "PASS" or EXIT_BY_VERDICT[overall_verdict(False, True)] != 2:
        fails.append("verdict / exit code")
    if case_passes([run(True), run(False)]) is not True or case_passes([run(False), run(False), run(True)]) is not False:
        fails.append("majority rule")

    for f in fails:
        print("FAIL", f)
    print(f"self-check: {'OK' if not fails else f'{len(fails)} failure(s)'}")
    return 1 if fails else 0


# ---- CLI -----------------------------------------------------------------------
def build_parser(ap: argparse.ArgumentParser | None = None) -> argparse.ArgumentParser:
    """Pack options. With ap given (run_legal_eval.py) the options it already has
    (--ids, --base-url, --email, --tenant, --kb, --parallel) are left to it."""
    standalone = ap is None
    ap = ap or argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    if standalone:
        ap.add_argument("--ids", default="", help="comma-separated case ids")
        ap.add_argument("--base-url", default=os.environ.get("WERAG_BASE_URL", "http://127.0.0.1:18080"))
        ap.add_argument("--email", default="")
        ap.add_argument("--tenant", type=int, default=0)
        ap.add_argument("--kb", action="append", default=[], help="knowledge base id (repeat); default: the agent's own")
        ap.add_argument("--parallel", type=int, default=1)
    ap.add_argument("--pack", default="opc-vn-business-law")
    ap.add_argument("--cases", default="", help="cases file (default domain-packs/<pack>/evals/cases.jsonl)")
    ap.add_argument("--agent-id", default="", help="agent with the pack (domain_packs=[pack])")
    ap.add_argument("--baseline-agent-id", default="", help="same agent without the pack")
    ap.add_argument("--runs", type=int, default=3, help="runs per case and agent (latency = median)")
    ap.add_argument("--dry-run", action="store_true", help="validate the cases file and exit (offline)")
    ap.add_argument("--self-check", action="store_true", help="test the check functions and exit (offline)")
    ap.add_argument("--out", default="", help="report path (default results/pack-<pack>-<ts>.json)")
    ap.add_argument("--token", default=os.environ.get("WERAG_TOKEN", ""))
    ap.add_argument("--token-file", default="")
    ap.add_argument("--turn-timeout", type=float, default=900)
    return ap


def main(args) -> int:
    if args.self_check:
        return self_check()
    pack_dir = os.path.join(PACKS_DIR, args.pack)
    cases_path = args.cases or os.path.join(pack_dir, "evals", "cases.jsonl")
    try:
        catalogue = load_catalogue(pack_dir)
        drafts = load_draft_titles(pack_dir)
        cases = load_cases(cases_path)
    except (OSError, ValueError) as e:
        print(f"cannot read the pack: {e}")
        return 2
    errs = validate_cases(cases, catalogue)
    if args.ids:
        want = set(args.ids.split(","))
        cases = [c for c in cases if c["id"] in want]
    print(f"{cases_path}: {len(cases)} cases, {len(catalogue)} catalogue numbers, {len(drafts)} drafts; "
          f"categories {dict(collections.Counter(c.get('category') for c in cases))}")
    for e in errs:
        print("  ERROR", e)
    if args.dry_run or errs:
        print("dry-run: " + ("OK" if not errs else f"{len(errs)} problem(s)"))
        return 1 if errs else 0
    args.kb = args.kb or []
    if not args.agent_id or not args.baseline_agent_id:
        raise SystemExit("--agent-id and --baseline-agent-id are required for a live run")
    return run_suite(args, cases, catalogue, drafts)


if __name__ == "__main__":
    sys.exit(main(build_parser().parse_args()))
