"""Build a locked provision-level amendment test set from the reference sources.

Sources (all independent of our extractors):
  editorial  gold_edges.jsonl   vbpl editors' type="<code>:<source prov>" tags on A
  vbhn       vbhn_edges.jsonl   footnotes of văn bản hợp nhất
plus, for comparison only, the vbpl parser's chunk amends (parser_amends.jsonl).

Unit = one B chunk (passage). Labels = union of reference edges whose source
provision lies in that chunk, with the sources that agree. Chunks whose B
document appears in an earlier eval set are excluded so the set stays locked.

Usage: PGPASSWORD=… python3 build_testset.py <gold dir> "host=… dbname=… user=…" <out prefix> [--n 300] [--seed 20261020]
Writes <prefix>.jsonl (amend_eval.py input format) and <prefix>-labels.jsonl.
"""
import argparse, collections, json, os, random, subprocess, sys

HERE = os.path.dirname(os.path.abspath(__file__))
EVAL_DATA = os.path.join(HERE, "..", "data")
QUOTAS = {"sua_doi_bo_sung": 90, "bai_bo": 60, "thay_the": 60, "bo_sung": 50, "other": 40}


def jl(path):
    with open(path, encoding="utf-8") as f:
        return [json.loads(l) for l in f if l.strip()]


def psql(conn, sql):
    res = subprocess.run(["psql", conn, "-At", "-c", sql], capture_output=True, text=True)
    if res.returncode:
        sys.exit("psql: " + res.stderr[:500])
    out = res.stdout
    return [json.loads(l) for l in out.splitlines() if l.strip()]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("gold_dir")
    ap.add_argument("conn")
    ap.add_argument("prefix")
    ap.add_argument("--n", type=int, default=300)
    ap.add_argument("--seed", type=int, default=20261020)
    ap.add_argument("--keep", help="existing set .jsonl: keep its chunks, rebuild labels only")
    a = ap.parse_args()
    D = a.gold_dir

    cat = {}
    for l in open(os.path.join(D, "catalogue.tsv"), encoding="utf-8"):
        f = l.rstrip("\n").split("\t")
        if len(f) > 2:
            cat[f[0]] = f
    prov_chunk = {}
    for l in open(os.path.join(D, "prov_chunk.tsv"), encoding="utf-8"):
        doc, chunk, pid = l.rstrip("\n").split("\t")
        prov_chunk.setdefault(pid, (doc, chunk))
    by_path = {}
    for p in jl(os.path.join(D, "provisions.jsonl")):
        if p["path"]:
            by_path.setdefault((p["doc"], p["path"]), p["id"])

    labels = collections.defaultdict(dict)  # chunk -> key -> label

    def add(chunk, b_doc, op, a_doc, path, source, extra):
        k = (a_doc, path, op)
        lab = labels[chunk].setdefault(k, {"op": op, "a_doc": a_doc, "a_number": cat.get(a_doc, ["", ""])[1],
                                           "a_path": path, "b_doc": b_doc, "sources": [], "eff_from": None})
        if source not in lab["sources"]:
            lab["sources"].append(source)
        for kk, vv in extra.items():
            if vv and not lab.get(kk):
                lab[kk] = vv

    for e in jl(os.path.join(D, "gold_edges.jsonl")):
        hit = prov_chunk.get(e["source_prov"])
        if hit and e["target_path"]:
            add(hit[1], hit[0], e["op"], e["target_doc"], e["target_path"], "editorial",
                {"source_path": e["source_path"]})
    vb = os.path.join(D, "vbhn_edges.jsonl")
    for e in jl(vb) if os.path.exists(vb) else []:
        if not (e["source_doc"] and e["source_path"] and e["base_doc"] and e["target_path"] and e["op"]):
            continue
        pid = by_path.get((e["source_doc"], e["source_path"]))
        hit = prov_chunk.get(pid) if pid else None
        if hit:
            add(hit[1], hit[0], e["op"], e["base_doc"], e["target_path"], "vbhn",
                {"eff_from": e["eff_from"], "source_path": e["source_path"]})

    # Lock: drop B documents already used by earlier eval sets.
    used = set()
    for fn in os.listdir(EVAL_DATA):
        if fn.endswith(".jsonl") and "gold" not in fn:
            for r in jl(os.path.join(EVAL_DATA, fn)):
                if r.get("doc_number"):
                    used.add(r["doc_number"])
    chunks = []
    for ch, labs in labels.items():
        b = next(iter(labs.values()))["b_doc"]
        if cat.get(b, ["", ""])[1] in used:
            continue
        ops = collections.Counter(l["op"] for l in labs.values())
        main_op = ops.most_common(1)[0][0]
        chunks.append((ch, b, main_op if main_op in QUOTAS else "other"))

    if a.keep:
        kept = [json.loads(l) for l in open(a.keep, encoding="utf-8")]
        with open(a.prefix + "-labels.jsonl", "w", encoding="utf-8") as g:
            for r in kept:
                g.write(json.dumps({"id": r["id"], "b_doc": r["b_doc"], "b_number": r["doc_number"],
                                    "labels": list(labels.get(r["id"], {}).values())}, ensure_ascii=False) + "\n")
        src = collections.Counter("+".join(sorted(l["sources"])) for r in kept for l in labels.get(r["id"], {}).values())
        print(len(kept), "chunks kept; label sources:", dict(src))
        return

    rnd = random.Random(a.seed)
    rnd.shuffle(chunks)
    scale = a.n / sum(QUOTAS.values())
    want = {k: round(v * scale) for k, v in QUOTAS.items()}
    per_b = collections.Counter()
    picked = []
    for ch, b, st in chunks:
        if want[st] > 0 and per_b[b] < 2:
            picked.append((ch, st))
            want[st] -= 1
            per_b[b] += 1
    ids = ",".join("'" + c + "'" for c, _ in picked)
    rows = psql(a.conn, f"""
        select json_build_object('id', c.id, 'content', c.content, 'legal', c.metadata->'legal',
               'title', k.title, 'doc_number', k.metadata->>'doc_number', 'doc_type', k.metadata->>'doc_type',
               'b_doc', k.metadata->>'external_id')
        from chunks c join knowledges k on k.id = c.knowledge_id where c.id in ({ids})""")
    strat = dict(picked)
    with open(a.prefix + ".jsonl", "w", encoding="utf-8") as f, \
            open(a.prefix + "-labels.jsonl", "w", encoding="utf-8") as g:
        for r in rows:
            r["stratum"] = strat[r["id"]]
            f.write(json.dumps(r, ensure_ascii=False) + "\n")
            g.write(json.dumps({"id": r["id"], "b_doc": r["b_doc"], "b_number": r["doc_number"],
                                "labels": list(labels[r["id"]].values())}, ensure_ascii=False) + "\n")
    print(len(rows), "chunks;", collections.Counter(strat.values()), "; excluded B docs:", len(used))
    src = collections.Counter("+".join(sorted(l["sources"])) for r in rows for l in labels[r["id"]].values())
    print("label sources:", dict(src))


if __name__ == "__main__":
    sys.exit(main())
