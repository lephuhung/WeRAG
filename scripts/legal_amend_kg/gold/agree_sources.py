"""Agreement between the two reference sources (editorial tags vs VBHN
footnotes) where both cover the same base document, plus how often a
footnote's effective date differs from the amending document's catalogue date.

Usage: python3 agree_sources.py <gold dir>
"""
import collections, json, os, sys

D = sys.argv[1]
cat = {}
for l in open(os.path.join(D, "catalogue.tsv"), encoding="utf-8"):
    f = l.rstrip("\n").split("\t")
    if len(f) > 5:
        cat[f[0]] = f
ed = [json.loads(l) for l in open(os.path.join(D, "gold_edges.jsonl"), encoding="utf-8")]
vb = [json.loads(l) for l in open(os.path.join(D, "vbhn_edges.jsonl"), encoding="utf-8")]
ed_by_target = collections.defaultdict(list)
for e in ed:
    if e["target_path"]:
        ed_by_target[e["target_doc"]].append(e)

c = collections.Counter()
eff = collections.Counter()
for v in vb:
    if v["source_doc"] and v["eff_from"] and v["source_doc"] in cat:
        ce = cat[v["source_doc"]][4]
        eff["same_as_catalogue" if ce == v["eff_from"] else "catalogue_missing" if not ce else "differs"] += 1
    if not (v["base_doc"] and v["target_path"] and v["source_doc"] and v["op"]):
        continue
    eds = ed_by_target.get(v["base_doc"])
    if not eds:
        c["base_not_tagged_by_editors"] += 1
        continue
    same_src = [e for e in eds if e["source_doc"] == v["source_doc"]]
    if not same_src:
        c["editors_tag_base_but_not_this_amender"] += 1
        continue
    exact = [e for e in same_src if e["target_path"] == v["target_path"]]
    if exact:
        sp = any(e["source_path"] == v["source_path"] for e in exact)
        c["agree_target" + ("+source_path" if sp else "")] += 1
    elif any(e["target_path"].startswith(v["target_path"] + "/") or v["target_path"].startswith(e["target_path"] + "/")
             for e in same_src):
        c["agree_branch"] += 1
    else:
        c["vbhn_only"] += 1
print("VBHN edge vs editorial:", dict(c))
print("footnote eff date vs catalogue effective_on of amender:", dict(eff))
