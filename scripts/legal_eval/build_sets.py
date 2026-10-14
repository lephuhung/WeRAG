#!/usr/bin/env python3
"""Build the two legal eval sets from the VLSP 2025 datasets (ura-hcmut, HF).

  nli_grounding.jsonl  — (article, question, label) pairs from vlsp2025-nli-non-reasoning,
                         stratified: Có / Không-hard (question names the same document) /
                         Không-easy (question names no document).
  expired_trap.jsonl   — vlsp2025-qa-reasoning questions whose reference answer cites a
                         document our catalogue marks "Hết hiệu lực toàn bộ" and that an
                         in-force document in the VBPL knowledge bases repeals/replaces.

Inputs (TSV exports, see README.md): catalogue, repeal/replace relations, KB doc numbers.
Needs pyarrow. The VLSP data is CC BY-NC-SA 4.0: only eval samples are kept here.
"""
from __future__ import annotations

import argparse
import collections
import json
import random
import re

import pyarrow.parquet as pq

NUM = re.compile(r"\b\d{1,4}/(?:\d{4}/)?[A-ZĐ][A-ZĐa-z0-9\-]*")


def norm(s: str) -> str:
    return (s or "").upper().replace("Đ", "D").replace(" ", "").strip()


def texts(path: str):
    return pq.read_table(path, columns=["text"]).column("text").to_pylist()


def build_nli(path: str, n_per: int, rng: random.Random):
    strata = collections.defaultdict(list)
    for x in texts(path):
        doc = re.search(r"### Legal Document\n(.*?)\n### Question", x, re.S)
        q = re.search(r"### Question\n(.*?)<\|im_end\|>", x, re.S)
        if not doc or not q:
            continue
        doc, q = doc.group(1).strip(), q.group(1).strip()
        label = x.split("<|im_start|>assistant")[-1].replace("<think>", "").replace("</think>", "")
        label = "Có" if label.replace("<|im_end|>", "").strip().startswith("Có") else "Không"
        words = len(doc.split())
        if words < 40 or words > 1500:  # keep the attachment a realistic article
            continue
        qn, hd = set(NUM.findall(q)), set(NUM.findall(doc.split("\n")[0]))
        if label == "Có":
            stratum = "yes"
        elif qn & hd:
            stratum = "no_same_doc"
        elif not qn:
            stratum = "no_unnamed"
        else:
            continue
        strata[stratum].append({"stratum": stratum, "label": label, "question": q, "article": doc})
    out = []
    for k in ("yes", "no_same_doc", "no_unnamed"):
        pool = strata[k]
        rng.shuffle(pool)
        out += pool[:n_per[k]]
    for i, r in enumerate(out):
        r["id"] = f"nli-{i:03d}"
    return out


def build_trap(path: str, catalog: str, rels: str, kbdocs: str, n_per: int, rng: random.Random):
    by_num = collections.defaultdict(list)
    by_id = {}
    for line in open(catalog, encoding="utf8"):
        p = line.rstrip("\n").split("\t")
        row = {"id": p[0], "number": p[1], "status": p[2], "issuer": p[3], "issued_on": p[4]}
        by_num[norm(p[1])].append(row)
        by_id[p[0]] = row
    in_kb = {norm(l.split("\t")[1]) for l in open(kbdocs, encoding="utf8") if l.count("\t") >= 2}
    succ = collections.defaultdict(set)  # expired id -> in-KB in-force successor numbers
    for line in open(rels):
        a, b, _ = line.rstrip("\n").split("\t")
        s = by_id.get(a)
        if s and s["status"] in ("Còn hiệu lực", "Hết hiệu lực một phần") and norm(s["number"]) in in_kb:
            succ[b].add(s["number"])
    strata = collections.defaultdict(list)
    for x in texts(path):
        q = re.search(r"### Question\n(.*?)<\|im_end\|>", x, re.S)
        if not q:
            continue
        q = q.group(1).strip()
        ans = x.split("</think>")[-1].replace("<|im_end|>", "").strip()
        expired = []
        for s in sorted(set(NUM.findall(ans))):
            rows = by_num.get(norm(s), [])
            if len(rows) == 1 and rows[0]["status"] == "Hết hiệu lực toàn bộ" and succ.get(rows[0]["id"]):
                expired.append({"number": rows[0]["number"], "successors": sorted(succ[rows[0]["id"]])})
        if not expired:
            continue
        named = any(norm(e["number"]) in {norm(n) for n in NUM.findall(q)} for e in expired)
        stratum = "names_expired" if named else "unnamed"
        strata[stratum].append({"stratum": stratum, "question": q, "reference_answer": ans, "expired": expired})
    out = []
    for k in ("names_expired", "unnamed"):
        pool = strata[k]
        rng.shuffle(pool)
        out += pool[:n_per]
    for i, r in enumerate(out):
        r["id"] = f"trap-{i:03d}"
    return out, {k: len(v) for k, v in strata.items()}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--vlsp", required=True, help="dir with <dataset>/train-00000-of-00001.parquet")
    ap.add_argument("--catalog", required=True, help="TSV id, number, status, issuer, issued_on, doc_type")
    ap.add_argument("--rels", required=True, help="TSV from_id, to_id, relation (repeals/replaces)")
    ap.add_argument("--kbdocs", required=True, help="TSV kb, doc_number, eff_status")
    ap.add_argument("--out", default="data")
    ap.add_argument("--seed", type=int, default=2025)
    args = ap.parse_args()
    rng = random.Random(args.seed)
    nli = build_nli(f"{args.vlsp}/nli-non-reasoning/train-00000-of-00001.parquet",
                    {"yes": 150, "no_same_doc": 100, "no_unnamed": 50}, rng)
    trap, pools = build_trap(f"{args.vlsp}/qa-reasoning/train-00000-of-00001.parquet",
                             args.catalog, args.rels, args.kbdocs, 100, rng)
    for name, rows in (("nli_grounding", nli), ("expired_trap", trap)):
        with open(f"{args.out}/{name}.jsonl", "w", encoding="utf8") as f:
            for r in rows:
                f.write(json.dumps(r, ensure_ascii=False) + "\n")
    print("nli", collections.Counter(r["stratum"] for r in nli))
    print("trap", collections.Counter(r["stratum"] for r in trap), "pools", pools)


if __name__ == "__main__":
    main()
