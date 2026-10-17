"""Real-word confusion sets for the noiser (round 4).

v3.2 misses mostly errors that leave a real word behind (bổ→bố, tự→sự,
đạt→đặt, chính→chín): the random noiser rarely produces them, because most
of its edits give a non-word. A confusion set maps a frequent syllable to
the frequent syllables one edit away (another tone, another vowel mark,
d/đ, a swapped initial or final, one neighbouring key), weighted by how
often the confusable occurs, so the injected errors look like what people
actually leave in documents. Real errors mined from the audit
(mined.jsonl, KB pairs) are added with a boost.

  conf = build(counts, min_freq=30)
  conf["sự"] -> [("tự", 2.4, "typo"), ("sứ", 0.9, "tone_wrong"), ...]
"""
from __future__ import annotations

import json
import math
from collections import defaultdict

from vi_text import (
    MODIFIERS,
    TONES,
    accepted_variant,
    is_vietnamese_syllable,
    nfc,
    nfd,
    set_tone,
    strip_tone,
    tone_of,
)

# Weight of each edit class, after VSEC's real-error shares among single-word
# real-word errors (wrong tone 23%, vowel marks 18%, keys 14%, consonants).
CLASS_WEIGHT = {
    "tone_wrong": 24,
    "hoi_nga": 10,
    "tone_missing": 6,
    "vowel_mod": 18,
    "dd": 4,
    "initial": 8,
    "final": 6,
    "typo": 14,
}
REASON = {
    "tone_wrong": "sai dấu thanh",
    "hoi_nga": "sai dấu hỏi/ngã",
    "tone_missing": "thiếu dấu thanh",
    "vowel_mod": "sai nguyên âm",
    "dd": "sai nguyên âm",
    "initial": "sai phụ âm đầu",
    "final": "sai âm cuối",
    "typo": "gõ nhầm",
    "mined": "lỗi chính tả",
}
INITIALS = [("s", "x"), ("ch", "tr"), ("d", "gi"), ("d", "r"), ("gi", "r"), ("l", "n"), ("ng", "ngh"), ("g", "gh"),
            ("c", "k"), ("v", "d"), ("t", "s"), ("h", "k"), ("b", "v")]
FINALS = [("n", "ng"), ("t", "c"), ("nh", "n"), ("ch", "t"), ("m", "n"), ("i", "y"), ("u", "o")]
VOWEL_GROUPS = [("a", "ă", "â"), ("o", "ô", "ơ"), ("u", "ư"), ("e", "ê")]
QWERTY = {
    "q": "wa", "w": "qes", "e": "wrd", "r": "etf", "t": "ryg", "y": "tuh", "u": "yij", "i": "uok", "o": "ipl", "p": "o",
    "a": "qsz", "s": "awedxz", "d": "serfcx", "f": "drtgv", "g": "ftyhb", "h": "gyujn", "j": "huikm", "k": "jiol", "l": "kop",
    "z": "asx", "x": "zsdc", "c": "xdfv", "v": "cfgb", "b": "vghn", "n": "bhjm", "m": "njk",
}


def _tone_neighbours(s: str):
    t = tone_of(s)
    for nt in ("ngang", "huyen", "sac", "hoi", "nga", "nang"):
        if nt == t:
            continue
        n = set_tone(s, nt)
        if nt == "ngang":
            cls = "tone_missing"
        elif {t, nt} == {"hoi", "nga"}:
            cls = "hoi_nga"
        else:
            cls = "tone_wrong"
        yield n, cls


def _vowel_neighbours(s: str):
    """Every other vowel mark on one vowel (tone kept): đạt→đặt, sơ→sư."""
    d = nfd(s)
    i = 0
    while i < len(d):
        low = d[i].lower()
        grp = next((g for g in VOWEL_GROUPS if g[0] == low), None)
        if grp is None:
            i += 1
            continue
        j = i + 1
        mods = tone = ""
        while j < len(d) and (d[j] in MODIFIERS or d[j] in TONES):
            if d[j] in TONES:
                tone += d[j]
            else:
                mods += d[j]
            j += 1
        cur = nfc(low + mods)
        for v in grp:
            if v != cur:
                yield nfc(d[:i] + nfd(v) + tone + d[j:]), "vowel_mod"
        i = j
    if "đ" in s:
        yield s.replace("đ", "d", 1), "dd"
    elif s.startswith("d"):
        yield "đ" + s[1:], "dd"


def _affix_neighbours(s: str):
    for a, b in INITIALS:
        for x, y in ((a, b), (b, a)):
            if s.startswith(x) and len(s) > len(x) and not (x == "g" and s[1] in "hi") and not (x == "n" and s[1] in "gh") \
                    and not (x == "c" and s[1] == "h") and not (x == "t" and s[1] in "hr"):
                yield y + s[len(x):], "initial"
    for a, b in FINALS:
        for x, y in ((a, b), (b, a)):
            if s.endswith(x) and len(s) > len(x) + 1 and not (x == "n" and s.endswith(("ng", "nh"))) \
                    and not (x == "t" and s.endswith("ch")) and not (x == "c" and s.endswith("ch")):
                yield s[: -len(x)] + y, "final"


def _key_neighbours(s: str):
    """One base letter replaced by a neighbouring key, marks kept, landing
    on a real word (cho→chi, bản→bạn is a tone edit, not this)."""
    d = nfd(s)
    for i, ch in enumerate(d):
        if ch in QWERTY:
            for k in QWERTY[ch]:
                yield nfc(d[:i] + k + d[i + 1 :]), "typo"


def build(counts: dict[str, int], min_freq: int = 30) -> dict[str, list[tuple[str, float, str]]]:
    """counts: lower-case syllable -> corpus frequency."""
    freq = {w: c for w, c in counts.items() if c >= min_freq and is_vietnamese_syllable(w)}
    out: dict[str, list[tuple[str, float, str]]] = {}
    for s in freq:
        seen: dict[str, tuple[float, str]] = {}
        for gen in (_tone_neighbours, _vowel_neighbours, _affix_neighbours, _key_neighbours):
            for n, cls in gen(s):
                if n == s or n not in freq or accepted_variant(n, s) or n in seen:
                    continue
                seen[n] = (CLASS_WEIGHT[cls] * math.sqrt(freq[n]), cls)
        if seen:
            out[s] = [(n, w, cls) for n, (w, cls) in seen.items()]
    return out


def add_mined(conf: dict, paths: list[str], boost: float = 200.0) -> int:
    """Single-word real errors (correct -> wrong) from pair files."""
    n = 0
    for p in paths:
        for line in open(p, encoding="utf-8"):
            r = json.loads(line)
            for f in r.get("findings", []):
                w, c = f["wrong"].strip().lower(), f["correct"].strip().lower()
                if " " in w or " " in c or w == c or not is_vietnamese_syllable(w) or not is_vietnamese_syllable(c):
                    continue
                lst = conf.setdefault(c, [])
                if not any(x[0] == w for x in lst):
                    lst.append((w, boost, "mined"))
                    n += 1
    return n


def stats(conf: dict) -> dict[str, int]:
    c: dict[str, int] = defaultdict(int)
    for lst in conf.values():
        for _, _, cls in lst:
            c[cls] += 1
    return dict(c)
