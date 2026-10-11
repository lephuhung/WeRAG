"""Vietnamese syllable utilities shared by the spelling data pipeline.

Everything here is pure stdlib. Syllables are handled in NFD so tone marks
(U+0300 huyền, U+0301 sắc, U+0303 ngã, U+0309 hỏi, U+0323 nặng) and vowel
modifiers (U+0302 circumflex, U+0306 breve, U+031B horn) can be swapped
independently of the base letters.
"""
from __future__ import annotations

import re
import unicodedata

TONES = {"̀": "huyen", "́": "sac", "̃": "nga", "̉": "hoi", "̣": "nang"}
TONE_BY_NAME = {v: k for k, v in TONES.items()}
MODIFIERS = {"̂", "̆", "̛"}
VOWELS = set("aeiouy")

# Letters that belong to Vietnamese words (lowercase, NFC).
VI_LETTERS = set("abcdeghiklmnopqrstuvxy" "àáảãạăằắẳẵặâầấẩẫậèéẻẽẹêềếểễệìíỉĩịòóỏõọôồốổỗộơờớởỡợùúủũụưừứửữựỳýỷỹỵđ")
FOREIGN_LETTERS = set("fjwz")

WORD_RE = re.compile(r"[\wÀ-ɏẠ-ỿ]+", re.UNICODE)
# A token with its surrounding punctuation split off: (lead, core, trail).
TOKEN_RE = re.compile(r"^([^\w]*)(.*?)([^\w]*)$", re.UNICODE | re.DOTALL)


def nfd(s: str) -> str:
    return unicodedata.normalize("NFD", s)


def nfc(s: str) -> str:
    return unicodedata.normalize("NFC", s)


def split_token(tok: str) -> tuple[str, str, str]:
    m = TOKEN_RE.match(tok)
    if not m:
        return "", tok, ""
    return m.group(1), m.group(2), m.group(3)


def tone_of(syl: str) -> str:
    """Tone name of a syllable, 'ngang' when unmarked."""
    for ch in nfd(syl):
        if ch in TONES:
            return TONES[ch]
    return "ngang"


def strip_tone(syl: str) -> str:
    return nfc("".join(ch for ch in nfd(syl) if ch not in TONES))


def strip_all_marks(syl: str) -> str:
    """Remove tone marks and vowel modifiers; đ becomes d."""
    out = "".join(ch for ch in nfd(syl) if ch not in TONES and ch not in MODIFIERS)
    return nfc(out).replace("đ", "d").replace("Đ", "D")


def base_letters(syl: str) -> str:
    return strip_all_marks(syl).lower()


def _vowel_positions(d: str) -> list[int]:
    """Indices (in the NFD string) of base vowels, skipping the 'u' of 'qu'
    and the 'i' of 'gi' initials."""
    pos = []
    low = d.lower()
    for i, ch in enumerate(low):
        if ch in VOWELS:
            if ch == "u" and i == 1 and low[0] == "q":
                continue
            if ch == "i" and i == 1 and low[0] == "g" and any(c in VOWELS for c in low[2:]):
                continue
            pos.append(i)
    return pos


def set_tone(syl: str, tone: str) -> str:
    """Return the syllable carrying `tone` (name or '' / 'ngang' for none).

    The mark is kept where the syllable already had one. For an unmarked
    syllable the standard placement is used: the modified vowel when there is
    one, else the last vowel when a final consonant follows, else the
    penultimate vowel of a vowel cluster.
    """
    d = nfd(syl)
    mark = TONE_BY_NAME.get(tone, "")
    had = [i for i, ch in enumerate(d) if ch in TONES]
    if had:
        # base letter the old mark belonged to
        b = had[0] - 1
        while b > 0 and unicodedata.combining(d[b]):
            b -= 1
        chars = [ch for ch in d if ch not in TONES]
        if mark:
            j = b + 1
            while j < len(chars) and chars[j] in MODIFIERS:
                j += 1
            chars.insert(j, mark)
        return nfc("".join(chars))
    if not mark:
        return nfc(d)
    vp = _vowel_positions(d)
    if not vp:
        return nfc(d)

    def after_mods(i: int) -> int:
        j = i + 1
        while j < len(d) and d[j] in MODIFIERS:
            j += 1
        return j

    modified = [i for i in vp if after_mods(i) > i + 1]
    if modified:
        target = modified[-1]
    elif len(vp) == 1:
        target = vp[0]
    else:
        last = vp[-1]
        ends_with_consonant = after_mods(last) < len(d)
        target = last if ends_with_consonant else vp[-2]
    j = after_mods(target)
    return nfc(d[:j] + mark + d[j:])


def is_vietnamese_syllable(core: str) -> bool:
    low = core.lower()
    if not low or any(ch not in VI_LETTERS for ch in low):
        return False
    return any(ch in VOWELS or ch in "àáảãạăằắẳẵặâầấẩẫậèéẻẽẹêềếểễệìíỉĩịòóỏõọôồốổỗộơờớởỡợùúủũụưừứửữựỳýỷỹỵ" for ch in low)


def is_all_caps(text: str) -> bool:
    letters = [c for c in text if c.isalpha()]
    return len(letters) >= 3 and all(c.isupper() for c in letters)


def looks_like_code(core: str) -> bool:
    """Document numbers, codes, abbreviations: digits, slashes, dashes, dots
    inside, or all capitals (UBND, TP.HCM)."""
    if any(c.isdigit() for c in core):
        return True
    if any(c in "/-._@" for c in core):
        return True
    if len(core) >= 2 and core.isupper():
        return True
    return False


def protected_token(core: str, at_sentence_start: bool) -> bool:
    """Tokens the noiser must never corrupt and the model must never flag:
    codes, all-caps, foreign words, proper nouns (capitalised mid-sentence)."""
    if not core or looks_like_code(core):
        return True
    if any(ch in FOREIGN_LETTERS for ch in core.lower()):
        return True
    if not is_vietnamese_syllable(core):
        return True
    if core[0].isupper() and not at_sentence_start:
        return True
    return False


# --- accepted variants (never a spelling error for the tool) ---------------

_YI_PAIRS = [("i", "y")]


def same_up_to_yi(a: str, b: str) -> bool:
    """lí/lý, kĩ/kỹ, mĩ/mỹ, quí/quý: identical except a final i/y carrying the
    same tone."""
    da, db = nfd(a.lower()), nfd(b.lower())
    if len(da) != len(db):
        return False
    diff = [(x, y) for x, y in zip(da, db) if x != y]
    return len(diff) == 1 and {diff[0][0], diff[0][1]} == {"i", "y"}


def same_up_to_tone_position(a: str, b: str) -> bool:
    """hoà/hòa, thuỷ/thủy: same letters, same tone, mark on another vowel."""
    if a.lower() == b.lower():
        return False
    return strip_tone(a).lower() == strip_tone(b).lower() and tone_of(a) == tone_of(b) and tone_of(a) != "ngang"


def accepted_variant(a: str, b: str) -> bool:
    return same_up_to_yi(a, b) or same_up_to_tone_position(a, b) or a.lower() == b.lower()


# --- text segmentation -----------------------------------------------------

_SENT_SPLIT = re.compile(r"(?<=[.!?;:])\s+(?=[A-ZÀ-Ỹ0-9“\"(])")


def split_sentences(par: str) -> list[str]:
    parts = _SENT_SPLIT.split(par.strip())
    return [p.strip() for p in parts if p.strip()]


def letter_ratio(text: str) -> float:
    if not text:
        return 0.0
    letters = sum(1 for c in text if c.isalpha())
    return letters / len(text)


def syllable_count(text: str) -> int:
    return len(text.split())


def vietnamese_ratio(text: str) -> float:
    toks = [split_token(t)[1] for t in text.split()]
    toks = [t for t in toks if t]
    if not toks:
        return 0.0
    return sum(1 for t in toks if is_vietnamese_syllable(t) or looks_like_code(t)) / len(toks)
