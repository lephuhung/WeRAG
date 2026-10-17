"""Social-media short forms (teencode) such as ko, dc, vs, ntn.

They are not abbreviations of a name (UBND, bhxh stay unflagged): the
checker reports them with the full word, reason "viết tắt không chuẩn".
Sanng1112 only has ~15 of them (vs, dc/đc, ng, nhìu, ko/khg/k/kh, qá/wa,
j, nx); this lexicon adds the common chat ones.

substitute() replaces full forms in a clean (or already noised) line and
returns findings in the check_spelling shape; findings already present in
the line keep their "wrong" text intact and unique.
"""
from __future__ import annotations

import random
import re

REASON = "viết tắt không chuẩn"

# full form -> [(short form, weight)]
LEXICON: dict[str, list[tuple[str, float]]] = {
    "không": [("ko", 40), ("k", 15), ("kh", 8), ("khg", 4), ("hok", 6), ("khum", 3), ("k0", 2)],
    "được": [("dc", 25), ("đc", 25)],
    "với": [("vs", 15), ("w", 3)],
    "người": [("ng", 10)],
    "nhiều": [("nhìu", 6)],
    "quá": [("qá", 3), ("wá", 3), ("wa", 2)],
    "gì": [("j", 6)],
    "rồi": [("r", 8), ("rùi", 3)],
    "sao": [("s", 3)],
    "nữa": [("nx", 3)],
    "cũng": [("cx", 6), ("cg", 2)],
    "biết": [("bít", 4), ("bik", 3)],
    "mình": [("mk", 4), ("mik", 4)],
    "như thế nào": [("ntn", 6)],
    "bây giờ": [("bh", 3), ("bgio", 2)],
    "vậy": [("z", 3), ("zậy", 2)],
    "trước": [("trc", 3)],
    "chưa": [("chx", 2)],
    "thôi": [("thui", 2)],
    "luôn": [("lun", 2)],
    "vẫn": [("vx", 1)],
    "mọi người": [("mn", 4), ("mng", 2)],
    "hôm nay": [("hnay", 2)],
    "thời gian": [("tg", 2)],
    "trả lời": [("tl", 2)],
    "nhưng mà": [("nma", 2)],
    "không biết": [("kb", 2)],
    "em": [("e", 3)],
    "anh": [("a", 2)],
}
# How often a full form is shortened when a line has several candidates
# (không/được dominate chat; default 1).
FULL_WEIGHT = {"không": 6, "được": 5, "với": 3, "người": 2, "như thế nào": 2, "biết": 1.5, "rồi": 1.5, "gì": 1}

# Single letters (other than k) read as list markers or legal points (điểm a,
# điểm e) in documents: only used in chat questions, never capitalised.
QUERY_ONLY = {"e", "a", "w", "j", "r", "s", "z"}
# Compounds where the word is not the one the short form stands for.
NEXT_BLOCK = {
    "không": {"gian", "khí", "quân", "phận", "lưu", "ảnh"},
    "quá": {"trình", "khứ", "hạn", "tải", "cảnh", "độ", "mức"},
    "thôi": {"việc", "học", "chức", "nôi"},
    "sao": {"chép", "y", "kê", "lục", "lưu", "hỏa", "biển", "vàng", "đỏ"},
    "người": {"ta"},
    "anh": {"hùng", "ninh", "chị", "em"},
    "em": {"bé"},
    "trước": {"bạ"},
}
# "s" for "sao" only when the question ends with it: "làm s", "vậy s?"
FINAL_ONLY = {"s", "z"}

_FULL = sorted(LEXICON, key=len, reverse=True)
_RX = re.compile(r"(?<![\wÀ-ỹ])(" + "|".join(re.escape(f) for f in _FULL) + r")(?![\wÀ-ỹ])", re.IGNORECASE)
_WORD = re.compile(r"[\wÀ-ỹ]+")


def short_forms() -> set[str]:
    return {s for v in LEXICON.values() for s, _ in v}


def _candidates(text: str, query: bool) -> list[tuple[int, int, str]]:
    out = []
    for m in _RX.finditer(text):
        full = m.group(1)
        key = full.lower()
        nxt = _WORD.match(text, m.end() + 1) if m.end() < len(text) and text[m.end()] == " " else None
        if nxt and nxt.group(0).lower() in NEXT_BLOCK.get(key, ()):
            continue
        out.append((m.start(), m.end(), full))
    return out


def _pick_short(full: str, at_end: bool, query: bool, rng: random.Random) -> str | None:
    opts = [(s, w) for s, w in LEXICON[full.lower()]
            if (query or s not in QUERY_ONLY) and (at_end or s not in FINAL_ONLY)
            and not (s in QUERY_ONLY and full[0].isupper())]
    if not opts:
        return None
    s = rng.choices([o[0] for o in opts], weights=[o[1] for o in opts])[0]
    if full[0].isupper():
        s = s[0].upper() + s[1:]
    return s


def _unique_wrong(text: str, start: int, end: int) -> tuple[int, int] | None:
    """Widen [start, end) by whole words until it occurs once in text."""
    lo, hi = start, end
    for _ in range(3):
        if text.count(text[lo:hi]) == 1:
            return lo, hi
        # extend one word to the left, else to the right
        left = text.rfind(" ", 0, max(0, lo - 1))
        if lo > 0:
            lo = left + 1 if left >= 0 else 0
        else:
            right = text.find(" ", hi + 1)
            hi = right if right >= 0 else len(text)
    return (lo, hi) if text.count(text[lo:hi]) == 1 else None


def substitute(text: str, findings: list[dict], rng: random.Random, n: int, query: bool = True) -> tuple[str, list[dict]]:
    """Replace up to n full forms in text with short forms. findings are the
    errors already in text; occurrences overlapping them are skipped and a
    replacement is undone if it breaks one. Returns (text, new findings)."""
    taken = []
    for f in findings:
        i = text.find(f["wrong"])
        if i >= 0:
            taken.append((i, i + len(f["wrong"])))
    cands = [c for c in _candidates(text, query) if not any(c[0] < b and a < c[1] for a, b in taken)]
    # weighted draw without replacement: key = u ** (1 / weight)
    cands.sort(key=lambda c: rng.random() ** (1 / FULL_WEIGHT.get(c[2].lower(), 1)), reverse=True)
    chosen = sorted(cands[:n], reverse=True)  # right to left keeps earlier offsets valid
    new = []
    for start, end, full in chosen:
        at_end = text[end:].strip(" ?.!") == ""
        short = _pick_short(full, at_end, query, rng)
        if not short:
            continue
        cand = text[:start] + short + text[end:]
        if any(cand.count(f["wrong"]) != 1 for f in findings + new):
            continue
        span = _unique_wrong(cand, start, start + len(short))
        if not span:
            continue
        lo, hi = span
        wrong = cand[lo:hi]
        correct = wrong[: start - lo] + full + wrong[start - lo + len(short) :]
        text = cand
        new.append({"wrong": wrong, "correct": correct, "reason": REASON})
    # spans of earlier picks may have been widened over a later replacement
    new = [f for f in new if text.count(f["wrong"]) == 1]
    return text, new


if __name__ == "__main__":
    rng = random.Random(0)
    for q in ["Người nước ngoài có được mua nhà không?", "Thủ tục này mất bao lâu và cần giấy tờ gì",
              "Cho em hỏi cấp lại căn cước thì làm như thế nào", "Hồ sơ đã nộp rồi mà vẫn chưa được trả lời, vậy phải làm sao",
              "Quá trình xử lý không gian mạng"]:
        for _ in range(2):
            print(substitute(q, [], rng, rng.choice([1, 2, 3])))
