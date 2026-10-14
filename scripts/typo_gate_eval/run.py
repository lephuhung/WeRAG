"""Run typo_gate_set.jsonl through the live chat gate and score it.

For every question the gate's abbreviation_resolution event says what it
did: read words on its own (auto_corrections), asked (unknown_terms), or
nothing. Scored per kind:

  wrong_auto   a word read as something else than gold (must be 0), or any
               automatic reading of an abbreviation / name / clean word
  asked        the share of questions that got a question back
  auto_ok      typo questions read right without asking
  gate_s       seconds until the gate's event (p50 / p95)

  python run.py typo_gate_set.jsonl --token-file ../doc_assistant_eval/token.txt --tenant 10003
"""
from __future__ import annotations

import argparse
import json
import statistics
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "doc_assistant_eval"))
from werag_client import WeragClient  # noqa: E402


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("set")
    ap.add_argument("--token-file", required=True)
    ap.add_argument("--tenant", type=int)
    ap.add_argument("--base-url", default="http://127.0.0.1:18080")
    ap.add_argument("--agent", default="builtin-quick-answer")
    ap.add_argument("--per-session", type=int, default=20)
    ap.add_argument("--out", default="typo_gate_results.jsonl")
    a = ap.parse_args()
    client = WeragClient(a.base_url, open(a.token_file).read().strip(), a.tenant)
    rows = [json.loads(l) for l in open(a.set, encoding="utf-8") if l.strip()]
    results, session = [], None
    for i, row in enumerate(rows):
        if i % a.per_session == 0:
            session = client.create_session("typo gate eval")
        seen = {}
        t0 = time.time()

        def on_event(ev, seen=seen, t0=t0):
            if (ev.get("response_type") or ev.get("type")) == "abbreviation_resolution" and "state" not in seen:
                seen["state"] = (ev.get("data") or {}).get("abbreviation") or {}
                seen["t"] = time.time() - t0

        turn = client.chat(session, row["query"], agent_id=a.agent, on_event=on_event)
        st = seen.get("state", {})
        auto = {c["wrong"].lower(): c["correct"] for c in st.get("auto_corrections") or []}
        unknown = [u.lower() for u in st.get("unknown_terms") or []]
        res = {**row, "auto": auto, "unknown": unknown, "status": st.get("status"), "gate_s": seen.get("t"), "error": turn.error}
        w = row["word"].lower()
        if row["kind"] == "typo":
            got = auto.get(w)
            res["auto_ok"] = got is not None and got.lower() == row["gold"].lower()
            res["wrong_auto"] = got is not None and not res["auto_ok"]
        else:
            res["auto_ok"] = False
            res["wrong_auto"] = bool(auto)
        res["asked"] = bool(unknown)
        results.append(res)
        print(f"{i+1}/{len(rows)} {row['kind']:5} {row['query'][:60]!r} auto={auto} asked={unknown}", file=sys.stderr)
    with open(a.out, "w", encoding="utf-8") as f:
        for r in results:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")
    print("\nkind       n  asked  auto_ok  wrong_auto")
    groups = {
        "typo(oov)": [r for r in results if r["kind"] == "typo" and r.get("oov")],
        "typo(word)": [r for r in results if r["kind"] == "typo" and not r.get("oov")],
        "abbr": [r for r in results if r["kind"] == "abbr"],
        "name": [r for r in results if r["kind"] == "name"],
        "clean": [r for r in results if r["kind"] == "clean"],
    }
    for kind, rs in groups.items():
        if rs:
            print(f"{kind:10} {len(rs):3}  {sum(r['asked'] for r in rs)/len(rs):5.0%}  {sum(r['auto_ok'] for r in rs):7}  {sum(r['wrong_auto'] for r in rs):10}")
    times = sorted(r["gate_s"] for r in results if r["gate_s"] is not None)
    if times:
        print(f"gate seconds p50={statistics.median(times):.2f} p95={times[int(0.95*(len(times)-1))]:.2f}")
    errors = [r for r in results if r["error"]]
    if errors:
        print(f"{len(errors)} turns failed, e.g. {errors[0]['error'][:200]}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
