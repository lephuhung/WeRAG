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
MD_HEADING = re.compile(r"^\s*(?:#{1,6}|>)\s+")
MD_EMPHASIS = re.compile(r"\*\*|__")
BULLET = re.compile(r"^\s*(?:[-–•]|\d+[.)]|[a-zđ]\)|[IVX]+\.)\s*")


def paragraphs(content: str, strip_md: bool = False):
    content = PAGE_RE.sub("\n", content)
    for raw in content.split("\n"):
        if strip_md:  # vbpl chunks are Markdown ("#### Điều 33.", "**bold**"); user documents are not
            raw = MD_EMPHASIS.sub("", MD_HEADING.sub("", raw))
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
    ap.add_argument("--strip-md", action="store_true", help="drop Markdown headings and bold markers")
    ap.add_argument("--src", default="kb", help="value of the src field")
    a = ap.parse_args()
    seen = set()
    n_in = n_out = 0
    with open(a.out, "w", encoding="utf-8") as fo:
        for line in open(a.inp, encoding="utf-8"):
            n_in += 1
            rec = json.loads(line)
            for p in paragraphs(rec["content"], a.strip_md):
                key = re.sub(r"\W+", "", p.lower())
                if key in seen:
                    continue
                seen.add(key)
                fo.write(json.dumps({"text": p, "src": a.src}, ensure_ascii=False) + "\n")
                n_out += 1
    print(f"chunks={n_in} paragraphs={n_out}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
