#!/usr/bin/env python3
"""Measure component-labeling accuracy against gold labels.

For every ``X.docx`` with a sibling ``X.labels.json`` (gold, same shape as a
labeling reply) under the given directories, label the document with each
method and report:

- unit accuracy (all units, and *structural* units = everything except
  nội dung — the parts NĐ30 sizes/positions depend on),
- per-component precision / recall / F1 and the top confusions,
- document-type accuracy and share of documents with every structural unit
  right,
- downstream impact: share of rule checks whose verdict (pass/fail/warn/skip)
  equals the verdict obtained with the gold labels — the number that tells
  whether mislabeling changes the thể-thức report.

    python3 evaluation/run_eval.py evaluation/samples/synth
    python3 evaluation/run_eval.py samples/ --methods heuristic,llm --json out.json

LLM settings come from DOCFORMAT_LLM_* (see docformat/llm.py).
"""

from __future__ import annotations

import argparse
import collections
import glob
import json
import os
import sys
import time
from concurrent.futures import ThreadPoolExecutor

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))

from docformat import labeling  # noqa: E402
from docformat.checker import check_document  # noqa: E402
from docformat.labeling import _heuristic_label, build_units  # noqa: E402
from docformat.layout import inspect_docx  # noqa: E402
from docformat.segment import segment  # noqa: E402


def load_corpus(dirs: list, include_drafts: bool = False) -> list:
    out = []
    for d in dirs:
        for path in sorted(glob.glob(os.path.join(d, "**", "*.docx"),
                                     recursive=True)):
            gold_path = path[:-5] + ".labels.json"
            if not os.path.exists(gold_path):
                continue
            with open(gold_path, encoding="utf-8") as fh:
                gold = json.load(fh)
            if gold.get("draft") and not include_drafts:
                continue
            if isinstance(gold.get("labels"), list):  # reviewable list form
                gold["labels"] = {str(x["id"]): x.get("label") or x.get("component")
                                  for x in gold["labels"]}
            with open(path, "rb") as fh:
                out.append({"name": os.path.basename(path)[:-5],
                            "content": fh.read(), "gold": gold})
    return out


def unit_labels_of(seg, units: list) -> dict:
    return {u["id"]: _heuristic_label(seg, u["para"], u["zone"]) or "khac"
            for u in units}


def run_method(doc: dict, method: str, cfg=None) -> dict:
    layout = inspect_docx(doc["content"])
    heur = segment(layout)
    units = build_units(layout)
    t0 = time.time()
    info = {}
    if method == "heuristic":
        seg = heur
        labels_reply = None
    else:
        from docformat.llm import label_with_llm
        task, units_h, elided = labeling.prepare(layout, heur)
        try:
            labels_reply = label_with_llm(task, cfg)
            seg, diag = labeling.apply_labels(layout, labels_reply, heur,
                                              units_h, elided)
            info = {k: diag[k] for k in ("missing", "invalid") if diag.get(k)}
        except Exception as exc:  # report, count the doc as heuristic
            seg, labels_reply = heur, None
            info = {"error": str(exc)[:200]}
    elapsed = time.time() - t0
    pred = unit_labels_of(seg, units)

    # downstream: verdicts with predicted vs gold labels
    if method == "heuristic":
        rep = check_document(doc["content"], segmenter="heuristic")
    elif labels_reply is not None:
        rep = check_document(doc["content"], labels=labels_reply)
    else:
        rep = check_document(doc["content"], segmenter="heuristic")
    gold_rep = check_document(doc["content"], labels=doc["gold"])
    verdicts = {}
    gold_v = {c["id"]: c["status"] for c in gold_rep["checks"]
              if not c["id"].startswith("page.")}
    for c in rep["checks"]:
        if c["id"] in gold_v:
            verdicts[c["id"]] = (gold_v[c["id"]], c["status"])
    return {"pred": pred, "type": seg.detected_type, "elapsed": elapsed,
            "verdicts": verdicts, "info": info}


def score(corpus: list, results: list) -> dict:
    tot = ok = s_tot = s_ok = 0
    tp, fp, fn = (collections.Counter() for _ in range(3))
    confusion = collections.Counter()
    type_ok = docs_perfect = 0
    v_tot = v_ok = 0
    v_diff = collections.Counter()
    per_doc = []
    for doc, res in zip(corpus, results):
        gold = doc["gold"]["labels"]
        errs = []
        for uid, g in gold.items():
            p = res["pred"].get(uid, "khac")
            tot += 1
            ok += p == g
            if g != "noi_dung":
                s_tot += 1
                s_ok += p == g
            if p == g:
                tp[g] += 1
            else:
                fn[g] += 1
                fp[p] += 1
                confusion[(g, p)] += 1
                errs.append({"unit": uid, "gold": g, "pred": p})
        t_ok = res["type"] == doc["gold"]["document_type"]
        type_ok += t_ok
        structural_errs = [e for e in errs if e["gold"] != "noi_dung"
                           or e["pred"] != "noi_dung"]
        docs_perfect += not structural_errs
        for cid, (gv, pv) in res["verdicts"].items():
            v_tot += 1
            v_ok += gv == pv
            if gv != pv:
                v_diff[cid] += 1
        per_doc.append({"doc": doc["name"], "errors": errs,
                        "type": [doc["gold"]["document_type"], res["type"]],
                        "verdict_diffs": [c for c, (g, p) in res["verdicts"].items()
                                          if g != p],
                        "elapsed": round(res["elapsed"], 2), **res["info"]})
    comps = sorted(set(tp) | set(fn) | set(fp))
    prf = {}
    for c in comps:
        p = tp[c] / (tp[c] + fp[c]) if tp[c] + fp[c] else 0.0
        r = tp[c] / (tp[c] + fn[c]) if tp[c] + fn[c] else 0.0
        prf[c] = {"p": p, "r": r, "f1": 2 * p * r / (p + r) if p + r else 0.0,
                  "n": tp[c] + fn[c]}
    n = len(corpus)
    return {
        "unit_acc": ok / tot if tot else 0, "units": tot,
        "structural_acc": s_ok / s_tot if s_tot else 0, "structural_units": s_tot,
        "type_acc": type_ok / n if n else 0,
        "docs_perfect": docs_perfect / n if n else 0,
        "verdict_agreement": v_ok / v_tot if v_tot else 0,
        "verdict_diffs": v_diff.most_common(10),
        "per_component": prf,
        "confusions": [{"gold": g, "pred": p, "n": k}
                       for (g, p), k in confusion.most_common(15)],
        "time_total_s": sum(r["elapsed"] for r in results),
        "per_doc": per_doc,
    }


def print_report(name: str, s: dict, n_docs: int):
    print("\n=== %s (%d văn bản, %d unit) ===" % (name, n_docs, s["units"]))
    print("  Unit đúng:              %6.1f%%" % (100 * s["unit_acc"]))
    print("  Unit cấu trúc đúng:     %6.1f%%  (%d unit ngoài nội dung)"
          % (100 * s["structural_acc"], s["structural_units"]))
    print("  Loại văn bản đúng:      %6.1f%%" % (100 * s["type_acc"]))
    print("  Văn bản đúng hoàn toàn: %6.1f%%" % (100 * s["docs_perfect"]))
    print("  Kết quả chấm trùng gold:%6.1f%%" % (100 * s["verdict_agreement"]))
    print("  Thời gian:              %6.1fs thực tế, %.1fs/văn bản (cộng dồn)"
          % (s.get("wall_s", 0), s["time_total_s"] / max(n_docs, 1)))
    print("  %-22s %6s %6s %6s %5s" % ("thành phần", "P", "R", "F1", "n"))
    for c, m in sorted(s["per_component"].items(), key=lambda kv: kv[1]["f1"]):
        if m["n"]:
            print("  %-22s %6.2f %6.2f %6.2f %5d" % (c, m["p"], m["r"], m["f1"], m["n"]))
    if s["confusions"]:
        print("  Nhầm lẫn nhiều nhất (gold → dự đoán):")
        for c in s["confusions"][:8]:
            print("    %-20s → %-20s %d" % (c["gold"], c["pred"], c["n"]))
    if s["verdict_diffs"]:
        print("  Check bị lệch kết quả nhiều nhất:",
              ", ".join("%s(%d)" % kv for kv in s["verdict_diffs"][:6]))


def main(argv=None) -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("dirs", nargs="+")
    ap.add_argument("--methods", default="heuristic,llm")
    ap.add_argument("--workers", type=int, default=8)
    ap.add_argument("--json", help="write full report (per-doc errors) here")
    ap.add_argument("--include-drafts", action="store_true",
                    help="tính cả file nhãn còn \"draft\": true")
    args = ap.parse_args(argv)

    corpus = load_corpus(args.dirs, args.include_drafts)
    if not corpus:
        print("không tìm thấy cặp .docx + .labels.json", file=sys.stderr)
        return 2
    cfg = None
    methods = [m.strip() for m in args.methods.split(",") if m.strip()]
    if "llm" in methods:
        from docformat.llm import LLMConfig
        cfg = LLMConfig.from_env()
        if cfg is None:
            print("bỏ qua llm: chưa đặt DOCFORMAT_LLM_BASE_URL/MODEL",
                  file=sys.stderr)
            methods.remove("llm")
    report = {}
    for m in methods:
        workers = args.workers if m == "llm" else 1
        t0 = time.time()
        with ThreadPoolExecutor(max_workers=workers) as ex:
            results = list(ex.map(lambda d: run_method(d, m, cfg), corpus))
        s = score(corpus, results)
        s["wall_s"] = time.time() - t0
        print_report(m if m != "llm" else "llm (%s)" % cfg.model, s, len(corpus))
        report[m] = s
    if args.json:
        with open(args.json, "w", encoding="utf-8") as fh:
            json.dump(report, fh, ensure_ascii=False, indent=1)
        print("\nchi tiết: %s" % args.json)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
