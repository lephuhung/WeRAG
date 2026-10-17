"""Inject realistic Vietnamese spelling errors into clean paragraphs.

The error mix follows the human-made errors of VSEC (9,341 sentences):
roughly two thirds are tone or diacritic mistakes, one seventh single-key
typos, the rest consonant confusions, split/merge and doubled words. The
hỏi/ngã swap is upweighted because it is the dominant regional error in
central Vietnam.

Each injected error is recorded as a finding in the shape the check_spelling
tool expects: {"wrong": ..., "correct": ..., "reason": ...}, where "wrong"
is a verbatim substring of the noisy paragraph.
"""
from __future__ import annotations

import random
import re
from contextlib import contextmanager

from vi_text import (
    MODIFIERS,
    TONES,
    accepted_variant,
    base_letters,
    is_vietnamese_syllable,
    nfc,
    nfd,
    protected_token,
    set_tone,
    split_token,
    tone_of,
)

# (operation, weight, reason shown to the user)
OPS = [
    ("tone_wrong", 24, "sai dấu thanh"),
    ("tone_missing", 14, "thiếu dấu thanh"),
    ("hoi_nga", 10, "sai dấu hỏi/ngã"),
    ("vowel_mod", 14, "sai nguyên âm"),
    ("typo", 12, "gõ nhầm"),
    ("telex_vni", 3, "lỗi gõ Telex/VNI"),
    ("initial", 5, "sai phụ âm đầu"),
    ("final", 5, "sai âm cuối"),
    ("double", 4, "lặp từ"),
    ("split_merge", 6, "thiếu hoặc dư dấu cách"),
    ("punct_space", 3, "dấu cách trước dấu câu"),
    # real-word swap from a confusion set (confusion.py), off unless set
    ("confusion", 0, "lỗi chính tả"),
]
_OP_NAMES = [o[0] for o in OPS]
_OP_WEIGHTS = [o[1] for o in OPS]

# Words the noiser must never corrupt even though they look like syllables
# (lower-case abbreviations such as ubnd, hđnd in chat queries).
PROTECTED_WORDS: set[str] = set()


def set_op_weights(**overrides: float) -> None:
    """Override operation weights by name, e.g. set_op_weights(telex_vni=30, typo=25)."""
    for name, w in overrides.items():
        if name not in _OP_NAMES:
            raise KeyError(name)
        _OP_WEIGHTS[_OP_NAMES.index(name)] = w
REASON = {o[0]: o[2] for o in OPS}


@contextmanager
def op_weights(**overrides: float):
    """Temporarily override operation weights (query lines are keyboard-heavy,
    document paragraphs are not)."""
    saved = list(_OP_WEIGHTS)
    set_op_weights(**overrides)
    try:
        yield
    finally:
        _OP_WEIGHTS[:] = saved


# syllable -> [(confusable, weight, class)], see confusion.py
CONFUSIONS: dict[str, list[tuple[str, float, str]]] = {}


def set_confusions(conf: dict) -> None:
    CONFUSIONS.clear()
    CONFUSIONS.update(conf)


def _confusion(core: str, rng: random.Random) -> tuple[str, str] | None:
    from confusion import REASON as CONF_REASON

    opts = CONFUSIONS.get(core.lower())
    if not opts:
        return None
    new, _, cls = rng.choices(opts, weights=[o[1] for o in opts])[0]
    if core[0].isupper():
        new = new[0].upper() + new[1:]
    return new, CONF_REASON[cls]

# Known syllables (lowercase). When set, syllable-level corruptions prefer
# outputs that are real syllables (real-word errors such as sự→tự, the kind
# a spell checker finds hardest), like the human errors in VSEC.
VOCAB: set[str] = set()
REAL_WORD_PREFERENCE = 0.7


def set_vocab(syllables) -> None:
    VOCAB.clear()
    VOCAB.update(s.lower() for s in syllables)

QWERTY = {
    "q": "wa", "w": "qes", "e": "wrd", "r": "etf", "t": "ryg", "y": "tuh", "u": "yij", "i": "uok", "o": "ipl", "p": "o",
    "a": "qsz", "s": "awedxz", "d": "serfcx", "f": "drtgv", "g": "ftyhb", "h": "gyujn", "j": "huikm", "k": "jiol", "l": "kop",
    "z": "asx", "x": "zsdc", "c": "xdfv", "v": "cfgb", "b": "vghn", "n": "bhjm", "m": "njk",
}
INITIALS = [("s", "x"), ("ch", "tr"), ("d", "gi"), ("d", "r"), ("gi", "r"), ("l", "n"), ("ng", "ngh"), ("g", "gh"), ("c", "k")]
FINALS = [("n", "ng"), ("t", "c"), ("nh", "n"), ("i", "y"), ("ch", "t")]
VOWEL_GROUPS = [("a", "ă", "â"), ("o", "ô", "ơ"), ("u", "ư"), ("e", "ê")]
TELEX = {"̀": "f", "́": "s", "̃": "x", "̉": "r", "̣": "j"}
VNI = {"̀": "2", "́": "1", "̃": "4", "̉": "3", "̣": "5"}


def _tone_ops(core: str, op: str, rng: random.Random) -> str | None:
    t = tone_of(core)
    if op == "tone_missing":
        return set_tone(core, "ngang") if t != "ngang" else None
    if op == "hoi_nga":
        if t == "hoi":
            return set_tone(core, "nga")
        if t == "nga":
            return set_tone(core, "hoi")
        return None
    # tone_wrong: any other tone, including adding one to a toneless syllable
    choices = [x for x in ("huyen", "sac", "nga", "hoi", "nang") if x != t]
    return set_tone(core, rng.choice(choices))


def _vowel_mod(core: str, rng: random.Random) -> str | None:
    d = nfd(core)
    # positions of base vowels with their modifier (if any)
    cands = []
    for i, ch in enumerate(d):
        low = ch.lower()
        for grp in VOWEL_GROUPS:
            if low in grp or (low == grp[0]):
                cands.append((i, grp))
                break
    if "d" in core.lower() or "đ" in core.lower():
        cands.append((-1, ("d", "đ")))
    if not cands:
        return None
    i, grp = rng.choice(cands)
    if i == -1:
        if "đ" in core:
            return core.replace("đ", "d", 1)
        if "Đ" in core:
            return core.replace("Đ", "D", 1)
        if "d" in core:
            return core.replace("d", "đ", 1)
        return core.replace("D", "Đ", 1)
    # current letter = base + modifiers following it; tone marks (which sort
    # before some modifiers in NFD) are kept aside and re-attached
    j = i + 1
    mods = ""
    tone = ""
    while j < len(d) and (d[j] in MODIFIERS or d[j] in TONES):
        if d[j] in TONES:
            tone += d[j]
        else:
            mods += d[j]
        j += 1
    cur = nfc(d[i] + mods)
    others = [g for g in grp if g != cur.lower()]
    if not others:
        return None
    new = rng.choice(others)
    if d[i].isupper():
        new = new.upper()
    return nfc(d[:i] + nfd(new) + tone + d[j:])


def _typo(core: str, rng: random.Random) -> str | None:
    if len(core) < 2:
        return None
    kind = rng.choice(["adjacent", "delete", "transpose", "insert"])
    chars = list(core)
    i = rng.randrange(len(chars))
    if kind == "adjacent":
        b = base_letters(chars[i])
        if b not in QWERTY:
            return None
        rep = rng.choice(QWERTY[b])
        chars[i] = rep.upper() if chars[i].isupper() else rep
    elif kind == "delete":
        if len(chars) < 3:
            return None
        del chars[i]
    elif kind == "transpose":
        if i == len(chars) - 1:
            i -= 1
        chars[i], chars[i + 1] = chars[i + 1], chars[i]
    else:
        chars.insert(i, chars[i])
    return "".join(chars)


# How a telex_vni error is typed: tone key at the end, the whole syllable as
# raw keys, raw keys with one key pressed twice (cascc), modifiers raw only.
TELEX_KINDS = ["tone_key", "raw", "raw_extra", "mod_only"]
TELEX_KIND_WEIGHTS = [30, 40, 15, 15]
# Share of raw Telex syllables with the tone key typed straight after the
# vowel (casc) rather than at the end (cacs).
TONE_MID_RATE = 0.3


def set_telex_kind_weights(tone_mid_rate: float | None = None, **overrides: float) -> None:
    """Override telex_vni kind weights by name, e.g. set_telex_kind_weights(raw_extra=40),
    and optionally the share of tone keys typed mid-syllable."""
    global TONE_MID_RATE
    for name, w in overrides.items():
        TELEX_KIND_WEIGHTS[TELEX_KINDS.index(name)] = w
    if tone_mid_rate is not None:
        TONE_MID_RATE = tone_mid_rate


MOD_KEYS_TELEX = {"\u0302": {"a": "a", "e": "e", "o": "o"}, "\u0306": {"a": "w"}, "\u031b": {"o": "w", "u": "w"}}
MOD_KEYS_VNI = {"\u0302": "6", "\u0306": "8", "\u031b": "7"}


def raw_keystrokes(core: str, vni: bool, rng: random.Random) -> str | None:
    """The syllable as typed with the input method off: mootj / cacs / la6p5 /
    ddang. Tone key goes at the end (common) or right after the vowel."""
    d = nfd(core)
    out, tone, has_mark = [], "", False
    for ch in d:
        if ch in TONES:
            tone = ch
            has_mark = True
        elif ch in MODIFIERS:
            has_mark = True
            base = out[-1].lower() if out else ""
            if vni:
                out.append(MOD_KEYS_VNI[ch])
            else:
                out.append(MOD_KEYS_TELEX[ch].get(base, "w"))
        elif ch in "đĐ":
            has_mark = True
            out.append("dd" if ch == "đ" else "DD")
        else:
            out.append(ch)
    if not has_mark:
        return None
    s = "".join(out)
    if tone:
        key = (VNI if vni else TELEX)[tone]
        if not vni and rng.random() < TONE_MID_RATE:
            # tone key typed straight after the vowel: casc, nhaanj
            m = re.search(r"[aeiouyAEIOUY][aeiouywAEIOUYW]*", s)
            if m:
                s = s[: m.end()] + key + s[m.end() :]
            else:
                s += key
        else:
            s += key
    return s


def _telex_vni(core: str, rng: random.Random) -> str | None:
    d = nfd(core)
    marks = [i for i, ch in enumerate(d) if ch in TONES]
    vni = rng.random() < 0.35
    kind = rng.choices(TELEX_KINDS, weights=TELEX_KIND_WEIGHTS)[0]
    if kind == "tone_key":
        if not marks:
            return None
        table = VNI if vni else TELEX
        i = marks[0]
        rest = d[:i] + d[i + 1 :]
        # the mark key is typed at the end of the syllable
        return nfc(rest) + table[d[i]]
    raw = raw_keystrokes(core, vni, rng)
    if not raw:
        return None
    if kind == "raw_extra":
        # a key pressed twice or an extra key after the tone: cascc, cacss
        j = rng.randrange(max(1, len(raw) - 2), len(raw))
        return raw[: j + 1] + raw[j] + raw[j + 1 :]
    if kind == "mod_only":
        # modifiers typed raw but the tone applied: vieen → viên with tone kept
        if not marks:
            return raw
        tone = d[marks[0]]
        stripped = nfc("".join(ch for ch in d if ch not in TONES))
        r = raw_keystrokes(stripped, vni, rng)
        return r if r and r != core else raw
    return raw


def _swap_affix(core: str, pairs, rng: random.Random, at_end: bool) -> str | None:
    low = core.lower()
    opts = []
    for a, b in pairs:
        for x, y in ((a, b), (b, a)):
            if at_end:
                stem = low[: -len(x)] if low.endswith(x) and len(low) > len(x) else None
                if stem is not None and any(ch in "aeiouyàáảãạăằắẳẵặâầấẩẫậèéẻẽẹêềếểễệìíỉĩịòóỏõọôồốổỗộơờớởỡợùúủũụưừứửữựỳýỷỹỵ" for ch in stem):
                    opts.append(stem + y)
            else:
                if low.startswith(x) and len(low) > len(x) and low[len(x)] not in "h":
                    opts.append(y + low[len(x) :])
    if not opts:
        return None
    new = rng.choice(opts)
    if core[0].isupper():
        new = new[0].upper() + new[1:]
    return new


def corrupt_syllable(core: str, op: str, rng: random.Random) -> str | None:
    if op in ("tone_wrong", "tone_missing", "hoi_nga"):
        return _tone_ops(core, op, rng)
    if op == "vowel_mod":
        return _vowel_mod(core, rng)
    if op == "typo":
        return _typo(core, rng)
    if op == "telex_vni":
        return _telex_vni(core, rng)
    if op == "initial":
        return _swap_affix(core, INITIALS, rng, at_end=False)
    if op == "final":
        return _swap_affix(core, FINALS, rng, at_end=True)
    return None


def _sentence_starts(tokens: list[str]) -> set[int]:
    starts = {0}
    for i, t in enumerate(tokens[:-1]):
        if t.endswith((".", "!", "?", ":", ";")):
            starts.add(i + 1)
    return starts


def _unique_span(tokens: list[str], i: int) -> str:
    """Shortest token span around i that occurs once in the paragraph, so the
    'wrong' text locates unambiguously."""
    text = " ".join(tokens)
    for lo, hi in ((i, i), (i - 1, i), (i, i + 1), (i - 1, i + 1), (i - 2, i + 1)):
        if lo < 0 or hi >= len(tokens):
            continue
        span = " ".join(tokens[lo : hi + 1])
        if text.count(span) == 1:
            return span
    return " ".join(tokens[max(0, i - 2) : i + 2])


def inject(paragraph: str, rng: random.Random, n_errors: int | None = None, max_errors: int = 3) -> tuple[str, list[dict]]:
    """Return (noisy paragraph, findings). n_errors=None draws 1..max_errors
    with a bias to one error, like real documents."""
    tokens = paragraph.split()
    if not tokens:
        return paragraph, []
    starts = _sentence_starts(tokens)
    if n_errors is None:
        n_errors = rng.choices([1, 2, 3], weights=[62, 28, 10])[0]
    n_errors = min(n_errors, max_errors)
    candidates = [
        i for i, t in enumerate(tokens)
        if not protected_token(split_token(t)[1], i in starts) and split_token(t)[1].lower() not in PROTECTED_WORDS
    ]
    rng.shuffle(candidates)
    # Pick the error positions first, then apply them from the end of the
    # paragraph backwards so inserts/merges never shift a pending index.
    candidates = sorted(candidates[: n_errors * 3], reverse=True)
    used: set[int] = set()
    findings: list[dict] = []
    for i in candidates:
        if len(findings) >= n_errors:
            break
        if i in used or (i - 1) in used or (i + 1) in used:
            continue
        lead, core, trail = split_token(tokens[i])
        for _ in range(4):
            op = rng.choices(_OP_NAMES, weights=_OP_WEIGHTS)[0]
            if op == "double":
                tokens.insert(i + 1, tokens[i])
                used.update({i, i + 1})
                for f in findings:
                    f["i"] += 1
                findings.append({"i": i, "wrong": tokens[i] + " " + tokens[i + 1], "correct": tokens[i], "reason": REASON[op]})
                break
            if op == "split_merge":
                if i + 1 < len(tokens) and (i + 1) not in used and rng.random() < 0.6:
                    nlead, ncore, ntrail = split_token(tokens[i + 1])
                    if not ncore or protected_token(ncore, False) or trail:
                        continue
                    merged = tokens[i] + tokens[i + 1]
                    correct = tokens[i] + " " + tokens[i + 1]
                    del tokens[i + 1]
                    tokens[i] = merged
                    used.add(i)
                    for f in findings:
                        f["i"] -= 1
                    findings.append({"i": i, "wrong": merged, "correct": correct, "reason": REASON[op]})
                    break
                if len(core) >= 4:
                    k = rng.randrange(2, len(core) - 1)
                    broken = core[:k] + " " + core[k:]
                    tokens[i] = lead + broken + trail
                    used.add(i)
                    findings.append({"i": i, "wrong": broken, "correct": core, "reason": REASON[op]})
                    break
                continue
            if op == "punct_space":
                if trail and trail[0] in ",.;:" and len(trail) == 1:
                    tokens[i] = lead + core
                    tokens.insert(i + 1, trail)
                    used.update({i, i + 1})
                    for f in findings:
                        f["i"] += 1
                    findings.append({"i": i, "wrong": core + " " + trail, "correct": core + trail, "reason": REASON[op]})
                    break
                continue
            reason = REASON[op]
            if op == "confusion":
                got = _confusion(core, rng)
                new, reason = got if got else (None, reason)
            else:
                new = corrupt_syllable(core, op, rng)
            if not new or new == core or accepted_variant(new, core):
                continue
            if VOCAB and op in ("tone_wrong", "hoi_nga", "vowel_mod", "initial", "final") and new.lower() not in VOCAB and rng.random() < REAL_WORD_PREFERENCE:
                continue
            tokens[i] = lead + new + trail
            used.add(i)
            findings.append({"i": i, "wrong": new, "correct": core, "reason": reason})
            break
    noisy = " ".join(tokens)
    out = []
    for f in sorted(findings, key=lambda x: x["i"]):
        wrong = f["wrong"]
        if noisy.count(wrong) != 1:
            # widen to a unique span and apply the same widening to "correct"
            span = _unique_span(tokens, f["i"])
            if wrong in span:
                f["correct"] = span.replace(wrong, f["correct"], 1)
                wrong = span
        if wrong not in noisy or wrong == f["correct"]:
            continue
        out.append({"wrong": wrong, "correct": f["correct"], "reason": f["reason"]})
    return noisy, out


if __name__ == "__main__":
    import sys

    rng = random.Random(int(sys.argv[1]) if len(sys.argv) > 1 else 0)
    demo = [
        "Ủy ban nhân dân tỉnh đề nghị các sở, ban, ngành khẩn trương triển khai thực hiện Nghị định số 30/2020/NĐ-CP về công tác văn thư.",
        "Căn cứ Luật Tổ chức chính quyền địa phương ngày 19 tháng 6 năm 2015, Sở Nội vụ báo cáo kết quả kiểm tra việc chấp hành kỷ luật hành chính.",
        "Các đơn vị gửi báo cáo về Văn phòng trước ngày 15 hằng tháng để tổng hợp, theo dõi và đôn đốc thực hiện.",
    ]
    for p in demo:
        for _ in range(3):
            noisy, findings = inject(p, rng)
            print(noisy)
            print("   ", findings)
