"""Sample paragraphs (kb_clean.py output) for the clean pool, leaving out
any paragraph that occurs in an eval file or in the KB pairs, so a vbpl copy
of an eval paragraph can never leak into training.
"""
from __future__ import annotations

import argparse
import json
import random
import re
import sys

from build_dataset import read_jsonl


def key(s: str) -> str:
    return re.sub(r"\W+", "", s.lower())


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("inp")
    ap.add_argument("--exclude-eval", action="append", default=[], help="eval JSONL (build_eval.py shape, \"paragraphs\")")
    ap.add_argument("--exclude-kb", action="append", default=[], help="llm_correct.py output (text and orig)")
    ap.add_argument("--n", type=int, default=30000)
    ap.add_argument("--seed", type=int, default=4)
    ap.add_argument("--out", required=True)
    a = ap.parse_args()
    banned = set()
    for p in a.exclude_eval:
        for r in read_jsonl(p):
            banned.update(key(t) for t in r.get("paragraphs", {}).values())
    for p in a.exclude_kb:
        for r in read_jsonl(p):
            banned.update(key(r[f]) for f in ("text", "orig") if r.get(f))
    rows = read_jsonl(a.inp)
    keep = [r for r in rows if key(r["text"]) not in banned]
    random.Random(a.seed).shuffle(keep)
    keep = keep[: a.n]
    with open(a.out, "w", encoding="utf-8") as fo:
        for r in keep:
            fo.write(json.dumps(r, ensure_ascii=False) + "\n")
    print(f"in={len(rows)} excluded={len(rows) - len([r for r in rows if key(r['text']) not in banned])} out={len(keep)}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
