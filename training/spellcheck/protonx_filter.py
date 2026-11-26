"""Turn protonx-legal-tc rewrites (protonx_mine.py shards) into spelling
candidates: one-to-one token replacements that only change diacritics or a
letter or two, on ordinary Vietnamese syllables.

Dropped: punctuation/space normalisation, codes, all-caps, proper nouns,
foreign words, accepted variants (hoà/hòa, lí/lý), pieces the model rewrote
heavily (> --max-per-piece replacements), paragraphs that occur in an eval file.

Output JSONL: {"src": piece, "findings": [{"wrong","correct","type"}], "p", "c", "kb_src"}
"""
from __future__ import annotations

import argparse
import collections
import difflib
import glob
import json
import re
import sys

from vi_text import accepted_variant, is_vietnamese_syllable, protected_token, strip_all_marks, vietnamese_ratio

TOK = re.compile(r"\S+")
EDGE = re.compile(r"^[\W_]+|[\W_]+$")


def key(s: str) -> str:
    return re.sub(r"\W+", "", s.lower())


def core_of(tok: str) -> tuple[str, str, str]:
    """leading punctuation, core, trailing punctuation"""
    m = re.match(r"^([\W_]*)(.*?)([\W_]*)$", tok, re.S)
    return m.group(1), m.group(2), m.group(3)


def edit_distance(a: str, b: str) -> int:
    if abs(len(a) - len(b)) > 2:
        return 3
    prev = list(range(len(b) + 1))
    for i, ca in enumerate(a, 1):
        cur = [i]
        for j, cb in enumerate(b, 1):
            cur.append(min(prev[j] + 1, cur[j - 1] + 1, prev[j - 1] + (ca != cb)))
        prev = cur
    return prev[-1]


def classify(w: str, c: str) -> str | None:
    """None = not a candidate."""
    if w == c or accepted_variant(w, c):
        return None
    if not (is_vietnamese_syllable(w) and is_vietnamese_syllable(c)):
        return None
    # keep the capitalisation pattern: the model must not re-case words
    if (w[0].isupper()) != (c[0].isupper()):
        return None
    if strip_all_marks(w).lower() == strip_all_marks(c).lower():
        return "diacritics"
    if edit_distance(w.lower(), c.lower()) <= 2 and edit_distance(strip_all_marks(w).lower(), strip_all_marks(c).lower()) <= 2:
        return "typo"
    return None


def findings_for(src: str, out: str, max_per_piece: int) -> list[dict] | None:
    ot = [(m.group(), m.start(), m.end()) for m in TOK.finditer(src)]
    ft = out.split()
    sm = difflib.SequenceMatcher(a=[t[0] for t in ot], b=ft, autojunk=False)
    ops = [o for o in sm.get_opcodes() if o[0] != "equal"]
    # a heavily rewritten piece is OCR garbage or a paraphrase, not a spelling fix
    if len(ops) > max_per_piece or any(o[0] in ("insert", "delete") and (o[2] - o[1]) + (o[4] - o[3]) > 1 for o in ops):
        return None
    # table fragments / data dictionaries: the model "fixes" random words there
    if len(ot) < 5 or vietnamese_ratio(src) < 0.8:
        return None
    finds = []
    for tag, i1, i2, j1, j2 in ops:
        if tag != "replace" or (i2 - i1) != (j2 - j1):
            continue  # token-count changes (split/merge) are not mined here
        for k in range(i2 - i1):
            wtok, start, _ = ot[i1 + k]
            ctok = ft[j1 + k]
            lw, w, tw = core_of(wtok)
            lc, c, tc = core_of(ctok)
            if not w or not c or (lw, tw) != (lc, tc):
                continue  # punctuation changed too: skip, keep the signal clean
            at_start = start == 0 or src[max(0, start - 2):start].strip() in ("", ".", ";", ":", ")", "-", "–", "+", "•")
            if protected_token(w, at_start) or protected_token(c, at_start):
                continue
            t = classify(w, c)
            # a word cut by the chunker ("ộ trung kiên" -> "bộ"): the model completes it, not a spelling fix
            if t == "typo" and (c.lower().endswith(w.lower()) or c.lower().startswith(w.lower()) or i1 + k == 0 or i1 + k == len(ot) - 1 or len(w) <= 2):
                t = None
            if t:
                finds.append({"wrong": w, "correct": c, "type": t})
    return finds


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("shard_glob")
    ap.add_argument("--out", required=True)
    ap.add_argument("--exclude-eval", action="append", default=[])
    ap.add_argument("--max-per-piece", type=int, default=4)
    a = ap.parse_args()
    banned = set()
    for p in a.exclude_eval:
        for l in open(p, encoding="utf-8"):
            r = json.loads(l)
            banned.update(key(t) for t in r.get("paragraphs", {}).values())
    n_pieces = n_heavy = n_kept = n_banned = 0
    types = collections.Counter()
    pairs = collections.Counter()
    seen = set()
    with open(a.out, "w", encoding="utf-8") as fo:
        for fn in sorted(glob.glob(a.shard_glob)):
            for l in open(fn, encoding="utf-8"):
                r = json.loads(l)
                n_pieces += 1
                k = key(r["src"])
                if k in banned:
                    n_banned += 1
                    continue
                if k in seen:
                    continue
                seen.add(k)
                finds = findings_for(r["src"], r["out"], a.max_per_piece)
                if finds is None:
                    n_heavy += 1
                    continue
                if not finds:
                    continue
                n_kept += 1
                for f in finds:
                    types[f["type"]] += 1
                    pairs[(f["wrong"].lower(), f["correct"].lower())] += 1
                fo.write(json.dumps({"src": r["src"], "findings": finds, "p": r["p"], "c": r["c"], "kb_src": r.get("kb_src", "")}, ensure_ascii=False) + "\n")
    print(f"changed pieces={n_pieces} eval-banned={n_banned} heavy-rewrite={n_heavy} candidate pieces={n_kept} findings={sum(types.values())} {dict(types)}", file=sys.stderr)
    print("top pairs:", ", ".join(f"{w}→{c} ×{n}" for (w, c), n in pairs.most_common(40)), file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
