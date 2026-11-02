"""Fetch vbpl 'provision-target' details (source provisions with their own
effFrom) for every target provision in gold_edges.jsonl. Resumable (sqlite).

Usage: python3 fetch_prov_targets.py <gold dir> [--delay 0.7] [--action ID]
"""
import argparse, json, os, sqlite3, sys, time
sys.path.insert(0, "/home/WeRAG/scripts/legal_catalog")
import crawl_vbpl as cv

ap = argparse.ArgumentParser()
ap.add_argument("gold_dir")
ap.add_argument("--delay", type=float, default=0.7)
ap.add_argument("--action", default="2836b8008e17a31b85409db6fcd8515b307b565d")
a = ap.parse_args()
db = sqlite3.connect(os.path.join(a.gold_dir, "prov_targets.sqlite"))
db.execute("CREATE TABLE IF NOT EXISTS t (doc TEXT, prov TEXT, body TEXT, error TEXT, at TEXT, PRIMARY KEY (doc, prov))")
todo = []
seen = set()
for line in open(os.path.join(a.gold_dir, "gold_edges.jsonl"), encoding="utf-8"):
    e = json.loads(line)
    k = (e["target_doc"], e["target_prov"])
    if k not in seen:
        seen.add(k); todo.append(k)
done = {(r[0], r[1]) for r in db.execute("SELECT doc, prov FROM t WHERE body IS NOT NULL")}
todo = [k for k in todo if k not in done]
print(f"{len(todo)} to fetch ({len(done)} done)", flush=True)
c = cv.Client(a.delay, 3)
fails = 0
for n, (doc, prov) in enumerate(todo, 1):
    body = err = None
    try:
        raw = c.action(a.action, [doc, prov], page=f"https://vbpl.vn/van-ban/chi-tiet/{doc}")
        # The result row "1:{...}" may follow a text chunk on the same line.
        i = raw.find('1:{"id"')
        if i < 0:
            raise ValueError("no result row")
        body = raw[i + 2:raw.find("\n", i) if raw.find("\n", i) > 0 else len(raw)]
        json.loads(body)
        fails = 0
    except Exception as ex:  # keep going; record the error
        err = str(ex)[:300]; fails += 1
    with db:
        db.execute("INSERT OR REPLACE INTO t VALUES (?,?,?,?,datetime('now'))", (doc, prov, body, err))
    if fails >= 20:
        print("20 failures in a row, stopping", flush=True); break
    if n % 200 == 0:
        print(f"{n}/{len(todo)}", flush=True)
print("done", flush=True)
