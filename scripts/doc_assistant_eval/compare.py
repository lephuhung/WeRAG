#!/usr/bin/env python3
"""Saved baselines of the compliance suite, and a run compared with one.

results/ is not committed; a baseline is the compact, committed record of a
run (scenario and check statuses, no answers or events):

  # save a run as a baseline (baselines/<name>.json)
  python3 compare.py save results/<run>/<ts>/results.json --name 2026-10-18 \
      --note "onlyoffice, judge Qwen3.6-35B-A3B-FP8"

  # compare a new run with a baseline (exit 1 when a scenario got worse)
  python3 compare.py diff results/<run>/<ts>/results.json baselines/2026-10-18.json

Checks are matched by (scenario, step, label); a label that embeds a number
of the run (a share, a count) is compared by its text before " — ".
"""
from __future__ import annotations

import argparse
import json
import os
import sys
from typing import Any, Dict, List

HERE = os.path.dirname(os.path.abspath(__file__))
RANK = {"PASS": 0, "SKIP": 0, "WARN": 1, "FAIL": 2}


def load_run(path: str) -> List[Dict[str, Any]]:
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def compact(run: List[Dict[str, Any]]) -> Dict[str, Any]:
    out: Dict[str, Any] = {}
    for sc in run:
        steps = []
        for i, st in enumerate(sc.get("steps") or []):
            steps.append({
                "step": i + 1,
                "query": (st.get("query") or "")[:120],
                "status": st.get("status"),
                "checks": [{"label": c.get("label", ""), "status": c.get("status")} for c in st.get("checks") or []],
            })
        out[sc["id"]] = {
            "group": sc.get("group"), "title": sc.get("title"), "status": sc.get("status"),
            "setup_error": sc.get("setup_error"), "steps": steps,
        }
    return out


def summary(scenarios: Dict[str, Any]) -> Dict[str, int]:
    s = {"PASS": 0, "WARN": 0, "FAIL": 0}
    for v in scenarios.values():
        s[v["status"]] = s.get(v["status"], 0) + 1
    return s


def cmd_save(args: argparse.Namespace) -> int:
    scenarios = compact(load_run(args.results))
    doc = {"name": args.name, "note": args.note, "source": os.path.relpath(args.results, HERE),
           "summary": summary(scenarios), "scenarios": scenarios}
    os.makedirs(os.path.join(HERE, "baselines"), exist_ok=True)
    path = os.path.join(HERE, "baselines", args.name + ".json")
    with open(path, "w", encoding="utf-8") as f:
        json.dump(doc, f, ensure_ascii=False, indent=1)
    print(f"saved {path}: {doc['summary']}")
    return 0


def check_key(label: str) -> str:
    return label.split(" — ")[0].strip()


def cmd_diff(args: argparse.Namespace) -> int:
    new = compact(load_run(args.results))
    with open(args.baseline, encoding="utf-8") as f:
        base = json.load(f)
    old = base["scenarios"]
    print(f"baseline {base['name']} {base['summary']}  →  this run {summary(new)}")
    worse = False
    not_run = [sid for sid in sorted(old) if sid not in new]
    for sid in sorted(set(old) | set(new)):
        o, n = old.get(sid), new.get(sid)
        if o is None:
            print(f"  + new scenario {sid}: {n['status']}")
            continue
        if n is None:
            continue
        if o["status"] != n["status"]:
            arrow = "WORSE" if RANK[n["status"]] > RANK[o["status"]] else "better"
            worse = worse or arrow == "WORSE"
            print(f"  {arrow:6} {sid}: {o['status']} → {n['status']}")
        ochecks = {(s["step"], check_key(c["label"])): c["status"] for s in o["steps"] for c in s["checks"]}
        for s in n["steps"]:
            for c in s["checks"]:
                k = (s["step"], check_key(c["label"]))
                was = ochecks.get(k)
                if was is not None and was != c["status"] and RANK.get(c["status"], 0) != RANK.get(was, 0):
                    print(f"         step {s['step']}: {k[1]}: {was} → {c['status']}")
    if not_run:
        print(f"  (not in this run: {len(not_run)} scenarios — {', '.join(not_run[:8])}{' …' if len(not_run) > 8 else ''})")
    return 1 if worse else 0


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("save", help="save a run as a baseline")
    s.add_argument("results")
    s.add_argument("--name", required=True)
    s.add_argument("--note", default="")
    d = sub.add_parser("diff", help="compare a run with a baseline")
    d.add_argument("results")
    d.add_argument("baseline")
    args = ap.parse_args()
    return cmd_save(args) if args.cmd == "save" else cmd_diff(args)


if __name__ == "__main__":
    sys.exit(main())
