"""Build the Vietnamese syllable list the chat gate uses to spot words that
are not Vietnamese (typos such as cascc, abbreviations such as ubnd) and to
check Telex/VNI decodings: syllables seen at least --min times in clean
corpora that also have a Vietnamese syllable shape.

  python gen_syllables.py --kb kb_corrected.jsonl --pairs sanng_train_filtered.jsonl \
    --parquet coung21_test.parquet:target --parquet bmd2_test.parquet:output \
    --out ../../internal/vietnamese_legal/abbreviation/data/vi_syllables.txt
"""
from __future__ import annotations

import argparse
import collections
import json
import re
import sys
import unicodedata

TONES = "̣̀́̃̉"
ONSET = r"(?:ngh|ng|nh|ch|gh|gi|kh|ph|qu|th|tr|b|c|d|đ|g|h|k|l|m|n|p|r|s|t|v|x)?"
NUCLEUS = (r"(?:uyê|uya|ươ|uô|iê|yê|ưa|ua|ia|oa|oă|oe|uâ|uê|uơ|uy|"
           r"a|ă|â|e|ê|i|o|ô|ơ|u|ư|y)")
CODA = r"(?:ch|ng|nh|c|m|n|p|t|i|y|o|u)?"
SHAPE = re.compile(rf"^{ONSET}{NUCLEUS}{CODA}$")


def toneless(s: str) -> str:
    return unicodedata.normalize("NFC", "".join(c for c in unicodedata.normalize("NFD", s) if c not in TONES))


def words(text: str):
    for w in re.findall(r"[^\W\d_]+", unicodedata.normalize("NFC", text)):
        yield w.lower()


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--kb", action="append", default=[])
    ap.add_argument("--pairs", action="append", default=[])
    ap.add_argument("--parquet", action="append", default=[], help="path:column")
    ap.add_argument("--min", type=int, default=5)
    ap.add_argument("--out", required=True)
    a = ap.parse_args()
    counts: collections.Counter[str] = collections.Counter()
    for p in a.kb + a.pairs:
        for line in open(p, encoding="utf-8"):
            r = json.loads(line)
            counts.update(words(r.get("text", "")))
    for spec in a.parquet:
        import pyarrow.parquet as pq
        path, col = spec.rsplit(":", 1)
        for t in pq.read_table(path, columns=[col]).column(col).to_pylist():
            counts.update(words(t or ""))
    keep = sorted(w for w, c in counts.items() if c >= a.min and SHAPE.match(toneless(w)))
    with open(a.out, "w", encoding="utf-8") as f:
        f.write("\n".join(keep) + "\n")
    print(f"words={len(counts)} syllables={len(keep)}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
