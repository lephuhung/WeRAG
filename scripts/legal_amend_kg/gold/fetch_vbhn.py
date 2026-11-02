import json, os, sys
sys.path.insert(0, "/home/WeRAG/scripts/legal_catalog")
import crawl_vbpl as cv
ids_file, out = sys.argv[1], sys.argv[2]
os.makedirs(os.path.join(out, "html"), exist_ok=True)
c = cv.Client(1.0, 4)
meta = open(os.path.join(out, "detail.jsonl"), "a", encoding="utf-8")
for line in open(ids_file, encoding="utf-8"):
    doc_id = line.split("\t")[0].strip()
    if not doc_id:
        continue
    path = os.path.join(out, "html", doc_id + ".html")
    if os.path.exists(path):
        continue
    try:
        p = json.loads(c.fetch(cv.DETAIL_URL.format(doc_id), headers={"Accept": "application/json"}))
    except Exception as e:
        print("ERR", doc_id, e, flush=True); continue
    d = p.get("data") or {}
    html = (d.get("documentContent") or {}).get("content") or ""
    open(path, "w", encoding="utf-8").write(html)
    d.pop("documentContent", None); d.pop("documentContentEn", None)
    meta.write(json.dumps({"id": doc_id, "detail": d}, ensure_ascii=False) + "\n")
    print(doc_id, len(html), flush=True)
