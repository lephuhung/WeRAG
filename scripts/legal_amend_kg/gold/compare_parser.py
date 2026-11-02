"""Compare the vbpl parser's chunk amends (metadata.legal.amends) with the
editorial gold edges. Match on (B doc id, A doc id, Điều/khoản/điểm path).

Usage: python3 compare_parser.py <gold dir>
"""
import collections, json, os, re, sys, unicodedata

D = sys.argv[1]
NUM = re.compile(r"(\d+[\w.]*/\d{4}/[A-Za-zĐđÐ0-9\-–]+(?:\s*-\s*[A-Za-zĐđÐ0-9]+)*)")


def key(n):
    n = unicodedata.normalize("NFC", n).upper().replace(" ", "")
    for a, b in (("Đ", "D"), ("Ð", "D"), ("–", "-")):
        n = n.replace(a, b)
    return n


by_key = collections.defaultdict(set)
for l in open(os.path.join(D, "catalogue.tsv"), encoding="utf-8"):
    f = l.rstrip("\n").split("\t")
    if len(f) < 3:
        continue  # continuation line of a multi-line title
    by_key[key(f[2] or f[1])].add(f[0])


def path(a, k=None, d=None):
    p = f"dieu:{a.lower()}"
    if k:
        p += f"/khoan:{k.lower()}"
    if d:
        p += f"/diem:{d.lower()}"
    return p


gold = [json.loads(l) for l in open(os.path.join(D, "gold_edges.jsonl"), encoding="utf-8")]
gold = [g for g in gold if g["source_doc"] and g["target_path"]]
gkeys = collections.defaultdict(list)
gpairs = set()
for g in gold:
    gkeys[(g["source_doc"], g["target_doc"], g["target_path"])].append(g)
    gpairs.add((g["source_doc"], g["target_doc"]))

parser, unresolved, ambiguous = [], 0, 0
pdocs = set()
for l in open(os.path.join(D, "parser_amends.jsonl"), encoding="utf-8"):
    r = json.loads(l)
    pdocs.add(r["b_doc"])
    for a in r["amends"] or []:
        if not a.get("article"):
            continue
        m = NUM.search(a.get("document") or "")
        ids = by_key.get(key(m.group(1))) if m else None
        if not ids:
            unresolved += 1
            continue
        ambiguous += len(ids) > 1
        p = path(a["article"], a.get("khoan"), a.get("diem"))
        parser.append({"b": r["b_doc"], "a_ids": ids, "path": p, "phrase": bool(a.get("phrase"))})


def anc(p, q):
    return p == q or q.startswith(p + "/") or p.startswith(q + "/")


gold_by_pair = collections.defaultdict(list)
for g in gold:
    gold_by_pair[(g["source_doc"], g["target_doc"])].append(g)


def classify(b, a_ids, p):
    pairs = [(b, a) for a in a_ids if (b, a) in gpairs]
    if not pairs:
        return "pair_not_in_gold"
    if any((b, a, p) in gkeys for _, a in pairs):
        return "exact"
    if any(anc(p, g["target_path"]) for pr in pairs for g in gold_by_pair[pr]):
        return "same_branch"
    return "wrong_provision"


prec = collections.Counter()
prec_phrase = collections.Counter()
pset = set()
for e in parser:
    c = classify(e["b"], e["a_ids"], e["path"])
    prec[c] += 1
    if e["phrase"]:
        prec_phrase[c] += 1
    for a in e["a_ids"]:
        pset.add((e["b"], a, e["path"]))

# Recall: gold edges whose B doc was ingested (has parser output somewhere).
rec = collections.defaultdict(collections.Counter)
for g in gold:
    if g["source_doc"] not in pdocs:
        rec[g["op"]]["b_not_ingested_or_no_amends"] += 1
        continue
    k = (g["source_doc"], g["target_doc"], g["target_path"])
    if k in pset:
        rec[g["op"]]["exact"] += 1
    elif any(b == k[0] and a == k[1] and anc(p, k[2]) for (b, a, p) in pset):
        rec[g["op"]]["same_branch"] += 1
    else:
        rec[g["op"]]["missed"] += 1

print("parser amends with article:", len(parser), "unresolved target doc:", unresolved, "ambiguous number:", ambiguous)
print("PRECISION (parser edge vs gold, only B-A pairs the editors tagged):", dict(prec))
print("  phrase subset:", dict(prec_phrase))
print("RECALL by gold op:")
for op, c in sorted(rec.items(), key=lambda x: -sum(x[1].values())):
    print(" ", op, dict(c))
