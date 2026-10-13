"""Evaluation part "query": short chat questions with keyboard-heavy errors,
plus clean questions carrying abbreviations (ubnd, bhxh...). Same row shape
as build_eval.py so eval_model.py scores it; append to eval.jsonl or score
on its own.

  python build_query_eval.py --queries held_out.jsonl --out eval_query.jsonl
"""
from __future__ import annotations

import argparse
import json
import random
import sys

from build_dataset import ABBREVIATIONS, abbreviate_query, load_system_prompt, read_jsonl
from build_eval import make_request
from noiser import PROTECTED_WORDS, inject, set_op_weights


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--queries", action="append", required=True)
    ap.add_argument("--n-noisy", type=int, default=200)
    ap.add_argument("--n-clean", type=int, default=100)
    ap.add_argument("--per-request", type=int, default=5)
    ap.add_argument("--telex-weight", type=float, default=40)
    ap.add_argument("--typo-weight", type=float, default=30)
    ap.add_argument("--seed", type=int, default=7)
    ap.add_argument("--out", required=True)
    a = ap.parse_args()
    rng = random.Random(a.seed)
    set_op_weights(telex_vni=a.telex_weight, typo=a.typo_weight)
    for shorts in ABBREVIATIONS.values():
        PROTECTED_WORDS.update(x.lower() for x in shorts)
    system = load_system_prompt()
    qs = []
    for p in a.queries:
        qs.extend(r["text"] for r in read_jsonl(p) if r.get("text"))
    rng.shuffle(qs)
    paras: list[tuple[str, list[dict]]] = []
    noisy_n = clean_n = 0
    for q in qs:
        if noisy_n >= a.n_noisy and clean_n >= a.n_clean:
            break
        if rng.random() < 0.5:
            q = abbreviate_query(q, rng)
        if noisy_n < a.n_noisy:
            noisy, fs = inject(q, rng, n_errors=1 if rng.random() < 0.75 else 2)
            if fs:
                paras.append((noisy, fs))
                noisy_n += 1
                continue
        if clean_n < a.n_clean:
            paras.append((q, []))
            clean_n += 1
    rng.shuffle(paras)
    with open(a.out, "w", encoding="utf-8") as f:
        for i in range(0, len(paras), a.per_request):
            chunk = paras[i : i + a.per_request]
            clean_idx = {k for k, (_, fs) in enumerate(chunk) if not fs}
            f.write(json.dumps(make_request(system, chunk, rng.randint(1, 300), "query", clean_idx), ensure_ascii=False) + "\n")
    print(f"noisy={noisy_n} clean={clean_n} requests={(len(paras) + a.per_request - 1) // a.per_request}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
