"""Turn nrl-ai/vn-spell-correction-train rows into sparse paragraph pairs.

The source (CC BY-SA 4.0, Wikipedia + news, {"input": noisy, "target":
clean}) is noised at ~15% of words, mostly stripped diacritics and OCR-style
letter swaps. Real text has 1-3 errors per paragraph, so instead of using a
row as is, we align it word by word and rebuild the noisy side from the
clean target with only 1..max_errors of its word errors put back, chosen
with per-type weights that favour the errors the model misses (real-word
tone/vowel errors, hỏi/ngã, consonants) over stripped diacritics.

Rows are dropped when the target changes a number, a capitalised word
(proper nouns: the source "fixes" Phủ Day → Phủ Dầy) or a code, or when the
alignment is not one word to one word. Output matches sanng_filter.py:
  {"text": clean, "noisy": noisy, "findings": [{wrong, correct, reason}], "src": "nrl"}
"""
from __future__ import annotations

import argparse
import difflib
import json
import random
import re
import sys

from vi_text import accepted_variant, base_letters, is_vietnamese_syllable, protected_token, split_token, strip_all_marks, strip_tone, tone_of

DIGIT = re.compile(r"\d")

# (type, weight when choosing which errors to keep, reason)
TYPES = {
    "tone_wrong": (6.0, "sai dấu thanh"),
    "hoi_nga": (6.0, "sai dấu hỏi/ngã"),
    "vowel_mark": (5.0, "sai dấu"),
    "d_dd": (3.0, "sai phụ âm đầu"),
    "consonant": (4.0, "sai phụ âm"),
    "tone_missing": (1.0, "thiếu dấu thanh"),
    "no_marks": (0.4, "thiếu dấu"),
    "typo": (1.0, "gõ nhầm"),
}


def kind(w: str, c: str) -> str:
    wl, cl = w.lower(), c.lower()
    if strip_tone(wl) == strip_tone(cl):
        tw, tc = tone_of(wl), tone_of(cl)
        if tw == "ngang":
            return "tone_missing"
        return "hoi_nga" if {tw, tc} == {"hoi", "nga"} else "tone_wrong"
    if strip_all_marks(wl) == strip_all_marks(cl):
        if wl.isascii():
            return "no_marks"
        if wl.replace("d", "đ", 1) == cl:
            return "d_dd"
        return "vowel_mark"
    if is_vietnamese_syllable(wl) and base_letters(wl)[1:] == base_letters(cl)[1:] or base_letters(wl)[:-1] == base_letters(cl)[:-1]:
        return "consonant"
    return "typo"


def convert(row: dict, rng: random.Random, a) -> dict | None:
    nt, ct = row["input"].split(), row["target"].split()
    if not (a.min_tokens <= len(ct) <= a.max_tokens):
        return None
    sm = difflib.SequenceMatcher(a=nt, b=ct, autojunk=False)
    cands = []
    for op, i1, i2, j1, j2 in sm.get_opcodes():
        if op == "equal":
            continue
        if op != "replace" or i2 - i1 != j2 - j1:
            return None  # split/merge/insert: not a one-to-one alignment
        for i, j in zip(range(i1, i2), range(j1, j2)):
            w_lead, w, w_trail = split_token(nt[i])
            c_lead, c, c_trail = split_token(ct[j])
            if DIGIT.search(nt[i] + ct[j]):
                return None  # the target must never change a number
            if (w_lead, w_trail) != (c_lead, c_trail) or not w or not c:
                continue  # punctuation edits: leave them clean
            if w.lower() == c.lower() or accepted_variant(w.lower(), c.lower()):
                continue
            if protected_token(c, j == 0 or ct[j - 1].endswith((".", "!", "?"))) or c[0].isupper() and j > 0:
                return None  # names and codes: the source rewrites them
            if not is_vietnamese_syllable(c.lower()):
                return None
            cands.append((j, nt[i], kind(w, c)))
    if not cands:
        return None
    k = min(len(cands), rng.choices(range(1, a.max_errors + 1), weights=[50, 30, 20][: a.max_errors])[0])
    chosen = []
    pool = list(cands)
    for _ in range(k):
        wts = [TYPES[t][0] for _, _, t in pool]
        pick = rng.choices(range(len(pool)), weights=wts)[0]
        chosen.append(pool.pop(pick))
    noisy_tokens = list(ct)
    for j, w, _ in chosen:
        noisy_tokens[j] = w
    noisy = " ".join(noisy_tokens)
    findings = []
    for j, w, t in sorted(chosen):
        wrong, correct = w, ct[j]
        if noisy.count(wrong) != 1:
            if j == 0:
                return None
            wrong, correct = noisy_tokens[j - 1] + " " + w, noisy_tokens[j - 1] + " " + ct[j]
            if noisy.count(wrong) != 1:
                return None
        findings.append({"wrong": wrong.strip(",.;:!?()\"'"), "correct": correct.strip(",.;:!?()\"'"), "reason": TYPES[t][1]})
    if any(not f["wrong"] or f["wrong"] == f["correct"] for f in findings):
        return None
    return {"text": row["target"], "noisy": noisy, "findings": findings, "src": "nrl", "types": [t for _, _, t in chosen]}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("inp")
    ap.add_argument("--out", required=True)
    ap.add_argument("--limit", type=int, default=100000)
    ap.add_argument("--max-errors", type=int, default=3)
    ap.add_argument("--min-tokens", type=int, default=8)
    ap.add_argument("--max-tokens", type=int, default=80)
    ap.add_argument("--clean-rate", type=float, default=0.0, help="share of kept rows also written as clean targets")
    ap.add_argument("--skip", type=int, default=0, help="skip the first N rows (keep them for a held-out probe)")
    ap.add_argument("--seed", type=int, default=0)
    a = ap.parse_args()
    rng = random.Random(a.seed)
    n_in = n_out = 0
    seen = {}
    with open(a.out, "w", encoding="utf-8") as fo:
        for line in open(a.inp, encoding="utf-8"):
            n_in += 1
            if n_in <= a.skip:
                continue
            rec = convert(json.loads(line), rng, a)
            if rec is None:
                continue
            for t in rec.pop("types"):
                seen[t] = seen.get(t, 0) + 1
            fo.write(json.dumps(rec, ensure_ascii=False) + "\n")
            n_out += 1
            if a.clean_rate and rng.random() < a.clean_rate:
                fo.write(json.dumps({"text": rec["text"], "noisy": rec["text"], "findings": [], "src": "nrl"}, ensure_ascii=False) + "\n")
            if n_out >= a.limit:
                break
    total = sum(seen.values()) or 1
    print(f"rows_in={n_in} rows_out={n_out} types=" + json.dumps({t: round(n / total, 3) for t, n in sorted(seen.items(), key=lambda x: -x[1])}), file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
