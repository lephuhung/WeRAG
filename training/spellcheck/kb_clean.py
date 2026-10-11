"""Turn exported knowledge-base chunks into candidate paragraphs.

Input: JSONL with {"content": ...} per chunk (see README, kb export).
Output: JSONL {"text": paragraph, "src": "kb"} with page markers, bare
numbers, table fragments, all-caps headings and near-duplicates removed.
The paragraphs are NOT guaranteed clean (OCR errors survive); llm_correct.py
handles that.
"""
from __future__ import annotations

import argparse
import json
import re
import sys

from vi_text import is_all_caps, letter_ratio, syllable_count, vietnamese_ratio

PAGE_RE = re.compile(r"<!--\s*page\s*\d+\s*-->", re.I)
MULTISPACE = re.compile(r"[ \t ]+")
TABLE_ROW = re.compile(r"^\s*\d+\s+\d+\s")  # "323 61 Chuyên viên ..." numbered table rows
BULLET = re.compile(r"^\s*(?:[-–•]|\d+[.)]|[a-zđ]\)|[IVX]+\.)\s*")


def paragraphs(content: str):
    content = PAGE_RE.sub("\n", content)
    for raw in content.split("\n"):
        line = MULTISPACE.sub(" ", raw).strip()
        if not line:
            continue
        if TABLE_ROW.match(line):
            continue
        if syllable_count(line) < 6 or syllable_count(line) > 120:
            continue
        if letter_ratio(line) < 0.6 or vietnamese_ratio(line) < 0.9:
            continue
        if is_all_caps(line):
            continue
        if line.count("|") > 1 or line.count("\t") > 0:
            continue
        yield line


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("inp")
    ap.add_argument("out")
    a = ap.parse_args()
    seen = set()
    n_in = n_out = 0
    with open(a.out, "w", encoding="utf-8") as fo:
        for line in open(a.inp, encoding="utf-8"):
            n_in += 1
            rec = json.loads(line)
            for p in paragraphs(rec["content"]):
                key = re.sub(r"\W+", "", p.lower())
                if key in seen:
                    continue
                seen.add(key)
                fo.write(json.dumps({"text": p, "src": "kb"}, ensure_ascii=False) + "\n")
                n_out += 1
    print(f"chunks={n_in} paragraphs={n_out}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
