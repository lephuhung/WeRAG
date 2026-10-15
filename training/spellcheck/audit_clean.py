"""Audit "clean" paragraphs with two proofreaders before they become clean
training examples.

A paragraph used as a clean example teaches the model that every word in it
is right, so a residual error ("giảm sát", "tới thiểu") teaches it to ignore
that error. Two passes:

  run    send paragraphs (JSONL with "text") to one OpenAI-compatible model
         with the check_spelling prompt, 10 per request; writes
         {"i": row, "findings": [...]} per paragraph, resumable
  merge  combine the runs of two models:
           both silent                         -> clean
           both flag the same span, same fix   -> fixed (text corrected;
                                                  also a mined real error)
           anything else flagged               -> dropped (uncertain)
         Case-only and multi-word "missing word" findings are ignored: the
         prompt does not ask for them and the 35B model invents them.

Rows of llm_correct.py output ("orig" + "findings") are real pairs: a fixed
target keeps the pair only when the fixed span also occurs verbatim in
"orig", so the pair gains that finding; otherwise the pair is dropped.
"""
from __future__ import annotations

import argparse
import concurrent.futures as cf
import json
import sys
from pathlib import Path

from build_dataset import load_system_prompt, read_jsonl
from eval_model import ApiBackend, norm, parse_reply, validate
from vi_text import accepted_variant

PER_REQUEST = 10


def cmd_run(a) -> int:
    rows = read_jsonl(a.inp)
    out = Path(a.out)
    done = set()
    if out.exists():
        done = {json.loads(l)["i"] for l in open(out, encoding="utf-8") if l.strip()}
    todo = [i for i in range(len(rows)) if i not in done]
    if a.limit:
        todo = todo[: a.limit]
    system = load_system_prompt()
    api = ApiBackend(a.api, a.model_name, a.api_key, a.workers)
    batches = [todo[k : k + PER_REQUEST] for k in range(0, len(todo), PER_REQUEST)]

    def one(batch: list[int]) -> list[dict]:
        paras = {str(i): rows[i]["text"] for i in batch}
        user = "Đoạn cần kiểm tra:\n" + "\n".join(f"[{i}] {rows[i]['text']}" for i in batch) + "\n"
        raw = parse_reply(api.one([{"role": "system", "content": system}, {"role": "user", "content": user}]))
        if raw is None and len(batch) > 1:
            # the 35B model sometimes rambles inside "reason" until the JSON breaks; one paragraph at a time usually parses
            return [r for i in batch for r in one([i])]
        if raw is None:
            return [{"i": i, "bad": True} for i in batch]
        found = validate(raw, paras)
        return [{"i": i, "findings": [{"wrong": f["wrong"], "correct": f["correct"]} for f in found if f["paragraph"] == i]} for i in batch]

    n = bad = 0
    with open(out, "a", encoding="utf-8") as fo, cf.ThreadPoolExecutor(a.workers) as ex:
        for res in ex.map(one, batches):
            for r in res:
                if r.get("bad"):
                    bad += 1
                    continue  # retried on the next run
                fo.write(json.dumps(r, ensure_ascii=False) + "\n")
                n += 1
            fo.flush()
            if (n + bad) % 1000 < PER_REQUEST:
                print(f"{n + bad + len(done)}/{len(rows)} bad={bad}", file=sys.stderr, flush=True)
    print(f"written={n} bad_json_paragraphs={bad} (rerun to retry)", file=sys.stderr)
    return 0


def relevant(f: dict) -> bool:
    w, c = f["wrong"], f["correct"]
    if w.lower() == c.lower():
        return False  # capitalisation only
    if accepted_variant(w.lower(), c.lower()) or (w.lower(), c.lower()) in {("qui", "quy")}:
        return False  # accepted spellings (hoà/hòa, lí/lý, qui/quy): the prompt says not to report them
    if len(c.split()) > len(w.split()) and c.lower().startswith(w.lower()):
        return False  # "thi" -> "thi hành": truncated text, not a spelling error
    return True


def load_run(path: str) -> dict[int, list[dict]]:
    return {r["i"]: [f for f in r["findings"] if relevant(f)] for r in read_jsonl(path)}


def overlap(text: str, f: dict, g: dict) -> bool:
    i, j = text.find(f["wrong"]), text.find(g["wrong"])
    return i >= 0 and j >= 0 and i < j + len(g["wrong"]) and j < i + len(f["wrong"])


def cmd_merge(a) -> int:
    rows = read_jsonl(a.inp)
    ra, rb = load_run(a.run_a), load_run(a.run_b)
    stats = {"clean": 0, "fixed": 0, "dropped": 0, "unaudited": 0, "pairs_kept": 0, "pairs_dropped": 0}
    with open(a.out, "w", encoding="utf-8") as fo, open(a.mined, "w", encoding="utf-8") as fm:
        for i, row in enumerate(rows):
            if i not in ra or i not in rb:
                stats["unaudited"] += 1
                continue
            fa, fb = ra[i], rb[i]
            text = row["text"]
            agreed = []
            if fa or fb:
                ok = len(fa) == len(fb)
                for f in fa:
                    m = [g for g in fb if overlap(text, f, g) and norm(text.replace(f["wrong"], f["correct"], 1)) == norm(text.replace(g["wrong"], g["correct"], 1))]
                    if not m:
                        ok = False
                        break
                    agreed.append(f)
                if not ok:
                    stats["dropped"] += 1
                    continue
            fixed = text
            for f in agreed:
                fixed = fixed.replace(f["wrong"], f["correct"], 1)
            if agreed:
                stats["fixed"] += 1
                fm.write(json.dumps({"noisy": text, "text": fixed, "findings": [{**f, "reason": "lỗi chính tả"} for f in agreed], "src": row.get("src", "")}, ensure_ascii=False) + "\n")
            else:
                stats["clean"] += 1
            out = {**row, "text": fixed}
            if row.get("findings"):  # real pair: its target gained fixes
                extra = [{**f, "reason": "lỗi chính tả"} for f in agreed]
                if any(f["wrong"] not in row["orig"] for f in extra):
                    stats["pairs_dropped"] += 1
                    continue
                out["findings"] = row["findings"] + extra
                stats["pairs_kept"] += 1
            fo.write(json.dumps(out, ensure_ascii=False) + "\n")
    print(json.dumps(stats), file=sys.stderr)
    return 0


def main() -> int:
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest="cmd", required=True)
    r = sub.add_parser("run")
    r.add_argument("inp")
    r.add_argument("--out", required=True)
    r.add_argument("--api", required=True, help="base URL without /v1")
    r.add_argument("--model-name", required=True)
    r.add_argument("--api-key", default="")
    r.add_argument("--workers", type=int, default=4)
    r.add_argument("--limit", type=int, default=0)
    m = sub.add_parser("merge")
    m.add_argument("inp")
    m.add_argument("--run-a", required=True)
    m.add_argument("--run-b", required=True)
    m.add_argument("--out", required=True, help="audited rows (clean or fixed)")
    m.add_argument("--mined", required=True, help="real errors both models agree on")
    a = ap.parse_args()
    return cmd_run(a) if a.cmd == "run" else cmd_merge(a)


if __name__ == "__main__":
    sys.exit(main())
