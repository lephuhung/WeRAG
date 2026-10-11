"""Build the evaluation set in the tool's request shape.

Three parts, all written to one JSONL (field "part" tells them apart):
  vsec      real human errors (nguyenthanhasia/vsec-vietnamese-spell-correction)
            batched 10 sentences per request; gold findings from
            correction_pairs minus accepted variants
  kb_real   held-out KB paragraphs whose OCR errors the 35B model fixed
            (llm_correct.py rows with findings, last N/2 of the file)
  kb_clean  held-out KB paragraphs the 35B model left unchanged (last N)

Each row: {"part", "messages": [system, user], "paragraphs": {idx: text},
           "gold": [{paragraph, wrong, correct}], "clean": [idx...]}
"""
from __future__ import annotations

import argparse
import json
import random
import sys

from build_dataset import load_system_prompt, read_jsonl
from vi_text import accepted_variant, split_token


def vsec_rows(path: str, limit: int, rng: random.Random) -> list[dict]:
    import pyarrow.parquet as pq

    t = pq.read_table(path, columns=["text", "corrected_text", "correction_pairs"]).to_pylist()
    rng.shuffle(t)
    out = []
    for r in t:
        text = r["text"].strip()
        gold = []
        ok = True
        for p in r["correction_pairs"]:
            w, c = p["error"], p["correction"]
            if accepted_variant(split_token(w)[1], split_token(c)[1]):
                continue
            if text.count(w) != 1:
                ok = False
                break
            gold.append({"wrong": w, "correct": c})
        if not ok or not gold:
            continue
        out.append({"text": text, "gold": gold})
        if len(out) >= limit:
            break
    return out


def make_request(system: str, paras: list[tuple[str, list[dict]]], start: int, part: str, clean_idx: set[int]) -> dict:
    lines, gold, pmap = [], [], {}
    for k, (text, fs) in enumerate(paras):
        idx = start + k
        pmap[str(idx)] = text
        lines.append(f"[{idx}] {text}")
        for f in fs:
            gold.append({"paragraph": idx, "wrong": f["wrong"], "correct": f["correct"]})
    user = "Đoạn cần kiểm tra:\n" + "\n".join(lines) + "\n"
    return {
        "part": part,
        "messages": [{"role": "system", "content": system}, {"role": "user", "content": user}],
        "paragraphs": pmap,
        "gold": gold,
        "clean": sorted(start + k for k in clean_idx),
    }


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--vsec", required=True, help="VSEC parquet file")
    ap.add_argument("--vsec-sentences", type=int, default=600)
    ap.add_argument("--kb", required=True, help="llm_correct.py output")
    ap.add_argument("--kb-holdout", type=int, default=200)
    ap.add_argument("--per-request", type=int, default=10)
    ap.add_argument("--seed", type=int, default=0)
    ap.add_argument("--out", required=True)
    a = ap.parse_args()
    rng = random.Random(a.seed)
    system = load_system_prompt()
    reqs = []

    vs = vsec_rows(a.vsec, a.vsec_sentences, rng)
    for i in range(0, len(vs), a.per_request):
        chunk = vs[i : i + a.per_request]
        reqs.append(make_request(system, [(r["text"], r["gold"]) for r in chunk], rng.randint(0, 400), "vsec", set()))

    rows = read_jsonl(a.kb)
    unchanged = [r for r in rows if not r["findings"]][-a.kb_holdout :]
    changed = [r for r in rows if r["findings"]][-a.kb_holdout // 2 :]
    for i in range(0, len(changed), a.per_request):
        chunk = changed[i : i + a.per_request]
        reqs.append(make_request(system, [(r["orig"], r["findings"]) for r in chunk], rng.randint(0, 400), "kb_real", set()))
    for i in range(0, len(unchanged), a.per_request):
        chunk = unchanged[i : i + a.per_request]
        reqs.append(make_request(system, [(r["text"], []) for r in chunk], rng.randint(0, 400), "kb_clean", set(range(len(chunk)))))

    with open(a.out, "w", encoding="utf-8") as fo:
        for r in reqs:
            fo.write(json.dumps(r, ensure_ascii=False) + "\n")
    parts = {}
    for r in reqs:
        parts[r["part"]] = parts.get(r["part"], 0) + 1
    print(f"requests={len(reqs)} by part={parts} vsec_sentences={len(vs)} kb_real={len(changed)} kb_clean={len(unchanged)}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
