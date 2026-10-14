"""Labelled questions for the chat gate's typo / abbreviation decisions.

kinds:
  typo   one word noised with keyboard errors (Telex/VNI leftovers, doubled
         keys, neighbour keys); gold = the word → its correction
  abbr   a known short form (ubnd, bhxh…) or an unknown one (bmnn, vphc…)
         inserted; it must never be "corrected"
  name   a product or foreign name; it must never be "corrected"
  clean  the question as written; nothing should happen

  python build_set.py --queries round3/queries_real_holdout.jsonl \
    --queries round3/queries_gen_holdout.jsonl --out typo_gate_set.jsonl
"""
from __future__ import annotations

import argparse
import json
import random
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "training" / "spellcheck"))
from noiser import PROTECTED_WORDS, inject, set_op_weights  # noqa: E402
from build_dataset import ABBREVIATIONS  # noqa: E402

SYLLABLES = set((Path(__file__).resolve().parents[2] / "internal" / "vietnamese_legal" / "abbreviation" / "data" / "vi_syllables.txt").read_text(encoding="utf-8").split())

UNKNOWN_ABBR = ["bmnn", "vphc", "tncn", "ttcp", "anm", "đnctd", "qlnn", "cnvc", "ptdl", "khcn"]
NAMES = [("giá {} magic 9 pro", "hornor"), ("cài {} trên máy tính cơ quan", "zalo"), ("cấu hình {} 18 pro", "iphone"),
         ("mở {} để xem tài liệu", "onlyoffice"), ("gửi qua {} cho phòng", "email"), ("tra {} nội bộ", "wiki")]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--queries", action="append", required=True)
    ap.add_argument("--n-typo", type=int, default=100)
    ap.add_argument("--n-clean", type=int, default=40)
    ap.add_argument("--seed", type=int, default=5)
    ap.add_argument("--out", required=True)
    a = ap.parse_args()
    rng = random.Random(a.seed)
    set_op_weights(telex_vni=40, typo=30)
    for shorts in ABBREVIATIONS.values():
        PROTECTED_WORDS.update(x.lower() for x in shorts)
    qs = []
    for p in a.queries:
        qs += [json.loads(l)["text"] for l in open(p, encoding="utf-8") if l.strip()]
    qs = [q for q in qs if not re.search(r"[@/]", q)]
    rng.shuffle(qs)
    rows = []
    for q in qs:
        if len([r for r in rows if r["kind"] == "typo"]) >= a.n_typo:
            break
        noisy, fs = inject(q, rng, n_errors=1)
        if len(fs) == 1 and " " not in fs[0]["wrong"] and " " not in fs[0]["correct"]:
            # a typo that is itself a syllable (óc for có) is a real-word error:
            # the gate does not look at those
            oov = fs[0]["wrong"].lower() not in SYLLABLES
            rows.append({"kind": "typo", "query": noisy, "word": fs[0]["wrong"], "gold": fs[0]["correct"], "oov": oov})
    for q in qs[-a.n_clean:]:
        rows.append({"kind": "clean", "query": q, "word": "", "gold": ""})
    known = [s[-1] for s in ABBREVIATIONS.values() if s[-1].islower()]
    for i, w in enumerate(UNKNOWN_ABBR + rng.sample(known, 10)):
        q = rng.choice(["quy định về {} như thế nào", "{} gồm những gì", "thời hạn xử lý {} là bao lâu", "báo cáo {} năm nay"]).format(w)
        rows.append({"kind": "abbr", "query": q, "word": w, "gold": ""})
    for tpl, w in NAMES:
        rows.append({"kind": "name", "query": tpl.format(w), "word": w, "gold": ""})
    with open(a.out, "w", encoding="utf-8") as f:
        for r in rows:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")
    print({k: sum(r["kind"] == k for r in rows) for k in ("typo", "clean", "abbr", "name")}, file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
