#!/usr/bin/env python3
"""Evaluate wiki search on the VBPL knowledge bases.

For every query in queries.json the expected pages are resolved from the
database (by document number, by a title fragment, or by slug), then the
query is run through:
  - "new": the live API (full-text search, migration 000135), and
  - "old": the pre-full-text regex query, replayed directly in SQL.
Reports hit@1, hit@5 and MRR per group and overall, plus every miss.

Usage:
  python3 run.py --token TOKEN [--api http://localhost:18080] \
      [--psql "psql -h 172.20.0.3 -U postgres -d WeKnora"] [--limit 5]
"""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import time
import urllib.parse
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))


def psql_rows(cmd: str, sql: str, **vars_) -> list[list[str]]:
    # -c does not expand :'var' placeholders; feed the SQL through stdin.
    args = cmd.split() + ["-A", "-t", "-q", "-f", "-"]
    for k, v in vars_.items():
        args += ["-v", f"{k}={v}"]
    p = subprocess.run(args, input=sql, capture_output=True, text=True)
    if p.returncode != 0:
        raise SystemExit(f"psql failed: {p.stderr.strip()}")
    return [line.split("|") for line in p.stdout.splitlines() if line]


def resolve_expected(cmd: str, kb: str, item: dict) -> list[str]:
    if item.get("expect_empty"):
        return []
    if "slug" in item:
        rows = psql_rows(cmd, "select slug from wiki_pages where knowledge_base_id=:'kb' and slug=:'s' and deleted_at is null", kb=kb, s=item["slug"])
        return [r[0] for r in rows]
    if "numbers" in item:
        out = []
        for n in item["numbers"]:
            rows = psql_rows(cmd, "select slug from wiki_pages where knowledge_base_id=:'kb' and page_metadata->>'document_number'=:'n' and deleted_at is null", kb=kb, n=n)
            out += [r[0] for r in rows]
        return out
    if "title_like" in item:
        rows = psql_rows(cmd, "select slug from wiki_pages where knowledge_base_id=:'kb' and page_type='summary' and title ilike '%' || :'t' || '%' and deleted_at is null", kb=kb, t=item["title_like"])
        return [r[0] for r in rows]
    return []


def search_new(api: str, token: str, kb: str, q: str, limit: int) -> tuple[list[str], float]:
    url = f"{api}/api/v1/knowledgebase/{kb}/wiki/search?" + urllib.parse.urlencode({"q": q, "limit": limit})
    req = urllib.request.Request(url, headers={"Authorization": f"Bearer {token}"})
    t0 = time.time()
    with urllib.request.urlopen(req, timeout=60) as r:
        d = json.load(r)
    dt = time.time() - t0
    r = d.get("data", d)
    items = r if isinstance(r, list) else r.get("items", r.get("pages", []))
    return [p.get("slug") for p in items], dt


OLD_SQL = """
select slug from wiki_pages
where knowledge_base_id=:'kb' and status <> 'archived' and deleted_at is null
  and (title ~* :'q' or content ~* :'q' or summary ~* :'q' or slug ~* :'q')
order by (case when title ~* :'q' then 4 when slug ~* :'q' then 3 when summary ~* :'q' then 2 when content ~* :'q' then 1 else 0 end) desc,
         updated_at desc
limit :lim
"""


def search_old(cmd: str, kb: str, q: str, limit: int) -> tuple[list[str], float]:
    t0 = time.time()
    rows = psql_rows(cmd, OLD_SQL, kb=kb, q=q, lim=str(limit))
    return [r[0] for r in rows], time.time() - t0


def score(results: list[str], expected: list[str], expect_empty: bool) -> tuple[float, float, float]:
    if expect_empty:
        ok = 1.0 if not results else 0.0
        return ok, ok, ok
    for i, s in enumerate(results):
        if s in expected:
            return (1.0 if i == 0 else 0.0), 1.0, 1.0 / (i + 1)
    return 0.0, 0.0, 0.0


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--token", required=True)
    ap.add_argument("--api", default="http://localhost:18080")
    ap.add_argument("--psql", default="psql -h 172.20.0.3 -U postgres -d WeKnora")
    ap.add_argument("--limit", type=int, default=5)
    ap.add_argument("--out", default=os.path.join(HERE, "results.json"))
    args = ap.parse_args()

    spec = json.load(open(os.path.join(HERE, "queries.json"), encoding="utf-8"))
    kbs = spec["kbs"]
    rows = []
    for item in spec["queries"]:
        kb = kbs[item["kb"]]
        expected = resolve_expected(args.psql, kb, item)
        empty = bool(item.get("expect_empty"))
        if not expected and not empty:
            rows.append({**item, "skipped": "expected page not in KB"})
            continue
        new, t_new = search_new(args.api, args.token, kb, item["q"], args.limit)
        old, t_old = search_old(args.psql, kb, item["q"], args.limit)
        rows.append({
            **item, "expected": expected,
            "new": new, "old": old, "t_new": round(t_new, 3), "t_old": round(t_old, 3),
            "score_new": score(new, expected, empty), "score_old": score(old, expected, empty),
        })
    json.dump(rows, open(args.out, "w", encoding="utf-8"), ensure_ascii=False, indent=1)

    # ---- report
    def agg(sel):
        n = len(sel)
        if n == 0:
            return "–"
        h1 = sum(r["score_new"][0] for r in sel) / n
        h5 = sum(r["score_new"][1] for r in sel) / n
        mrr = sum(r["score_new"][2] for r in sel) / n
        o1 = sum(r["score_old"][0] for r in sel) / n
        o5 = sum(r["score_old"][1] for r in sel) / n
        omrr = sum(r["score_old"][2] for r in sel) / n
        return f"{n:>2} | mới hit@1 {h1:.2f} hit@5 {h5:.2f} MRR {mrr:.2f} | cũ hit@1 {o1:.2f} hit@5 {o5:.2f} MRR {omrr:.2f}"

    scored = [r for r in rows if "skipped" not in r]
    groups = []
    for r in scored:
        if r["group"] not in groups:
            groups.append(r["group"])
    print("Nhóm | n | kết quả")
    for g in groups:
        print(f"{g:<22} | {agg([r for r in scored if r['group'] == g])}")
    print(f"{'TỔNG':<22} | {agg(scored)}")
    lat_new = sum(r["t_new"] for r in scored) / max(len(scored), 1)
    lat_old = sum(r["t_old"] for r in scored) / max(len(scored), 1)
    print(f"Độ trễ trung bình: mới {lat_new*1000:.0f} ms (API), cũ {lat_old*1000:.0f} ms (SQL trực tiếp)")
    skipped = [r for r in rows if "skipped" in r]
    if skipped:
        print("\nBỏ qua (không có trang mong đợi trong KB):")
        for r in skipped:
            print(f"  [{r['kb']}] {r['q']}")
    print("\nCâu tìm kiếm mới chưa trúng top-5:")
    for r in scored:
        if r["score_new"][1] < 1:
            print(f"  [{r['kb']}] {r['q']!r} → mong đợi {r['expected'][:3]} | mới: {r['new'][:3]} | cũ: {r['old'][:3]}")
    print("\nCâu tìm kiếm mới kém hơn cũ:")
    for r in scored:
        if r["score_new"][2] < r["score_old"][2]:
            print(f"  [{r['kb']}] {r['q']!r} mới MRR {r['score_new'][2]:.2f} < cũ {r['score_old'][2]:.2f}")


if __name__ == "__main__":
    main()
