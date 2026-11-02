"""Score extractors on the locked set against the reference labels
(editorial vbpl tags + VBHN footnotes).

The reference is incomplete (editors do not tag every provision; inserts of a
new unit have nowhere to be tagged), so "unconfirmed" predictions are not
counted as wrong: they go to a review file for hand adjudication.

Usage: python3 score_locked.py <set.jsonl> <labels.jsonl> <catalogue.tsv> <run.jsonl> [--review out.jsonl]
"""
import argparse, collections, json, re, unicodedata

NUM = re.compile(r"(\d+[\w.]*/\d{4}/[A-Za-zĐđÐ0-9\-–]+(?:\s*-\s*[A-Za-zĐđÐ0-9]+)*)")
# Titles from the knowledge base write "17-2023-NĐ-CP" for "17/2023/NĐ-CP".
DASHED = re.compile(r"\b(\d+)-(\d{4})-([A-ZĐÐ][A-Za-zĐđÐ0-9\-]*)")
NAME_TYPES = ("Luật", "Bộ luật", "Pháp lệnh", "Hiến pháp", "Nghị quyết", "Nghị định", "Thông tư")
# Reference op -> extractor ops that express the same act. vbpl code 10/12 is
# also used for phrase replacement, so both accept thay_cum_tu.
COMPAT = {
    "sua_doi_bo_sung": {"sua_doi", "bo_sung", "thay_cum_tu", "bai_bo_cum_tu"},
    "thay_the": {"sua_doi", "thay_cum_tu", "thay_the_van_ban"},
    "bai_bo": {"bai_bo", "bai_bo_cum_tu", "bai_bo_van_ban"},
    "bo_sung": {"bo_sung", "sua_doi"},
    "dinh_chinh": {"dinh_chinh", "sua_doi", "thay_cum_tu"},
    "tam_ngung": {"tam_ngung", "dinh_chi"},
    "dinh_chi": {"dinh_chi", "tam_ngung"},
    "giai_thich": set(),
}


def key(n):
    n = unicodedata.normalize("NFC", n).upper().replace(" ", "")
    for a, b in (("Đ", "D"), ("Ð", "D"), ("–", "-")):
        n = n.replace(a, b)
    return n


def path(d, k="", p=""):
    d, k, p = (str(x or "").strip().lower() for x in (d, k, p))
    if not d:
        return None
    s = f"dieu:{d}"
    if k:
        s += f"/khoan:{k}"
    if p:
        s += f"/diem:{p}"
    return s


def branch(a, b):
    return a == b or a.startswith(b + "/") or b.startswith(a + "/")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("set")
    ap.add_argument("labels")
    ap.add_argument("catalogue")
    ap.add_argument("run")
    ap.add_argument("--review")
    a = ap.parse_args()

    by_key = collections.defaultdict(set)
    titles = []
    for l in open(a.catalogue, encoding="utf-8"):
        f = l.rstrip("\n").split("\t")
        if len(f) > 2:
            by_key[key(f[2] or f[1])].add(f[0])
        if len(f) > 8:
            titles.append((unicodedata.normalize("NFC", f[8]).lower(), f[3], f[0]))
    issued = {t[2]: t[1] for t in titles}
    labels = {r["id"]: r for r in map(json.loads, open(a.labels, encoding="utf-8"))}
    rows = {r["id"]: r for r in map(json.loads, open(a.set, encoding="utf-8"))}
    run = {r["id"]: r for r in map(json.loads, open(a.run, encoding="utf-8"))}

    def resolve(doc, before=""):
        """Number first; a bare name ("Luật Khiếu nại") resolves to the newest
        same-name document issued before B — a stand-in for the Go
        ResolveLegalDocumentName date-aware lookup."""
        doc = DASHED.sub(lambda m: f"{m.group(1)}/{m.group(2)}/{m.group(3)}", doc or "")
        m = NUM.search(doc)
        if m:
            return by_key.get(key(m.group(1)), set())
        name = unicodedata.normalize("NFC", doc).strip().lower()
        if not name.startswith(tuple(t.lower() for t in NAME_TYPES)) or len(name) < 8:
            return set()
        cands = [(iss, i) for t, iss, i in titles
                 if (t == name or t.startswith(name + " số") or t.startswith(name + " ngày")) and iss and (not before or iss < before)]
        return {max(cands)[1]} if cands else set()

    systems = {"qwen": {}, "parser": {}}
    errors = 0
    for cid, r in rows.items():
        out = (run.get(cid) or {}).get("gemma") or {}
        if "error" in out or cid not in run:
            errors += 1
        preds = []
        for o in out.get("ops") or []:
            ids = resolve(o.get("target_doc"), issued.get(r.get("b_doc"), ""))
            for t in o.get("targets") or []:
                p = path(t.get("dieu"), t.get("khoan"), t.get("diem"))
                if p:
                    preds.append({"op": o.get("op"), "ids": ids, "doc": o.get("target_doc"), "path": p})
        systems["qwen"][cid] = preds
        preds = []
        for am in (r.get("legal") or {}).get("amends") or []:
            p = path(am.get("article"), am.get("khoan"), am.get("diem"))
            if p:
                preds.append({"op": "thay_cum_tu" if am.get("phrase") else None, "ids": resolve(am.get("document"), issued.get(r.get("b_doc"), "")),
                              "doc": am.get("document"), "path": p})
        systems["parser"][cid] = preds

    review = []
    print(f"set {len(rows)} chunks, labels {sum(len(l['labels']) for l in labels.values())}, qwen errors {errors}")
    for name, preds_by in systems.items():
        rec = collections.defaultdict(collections.Counter)
        prec = collections.Counter()
        for cid, lab in labels.items():
            preds = preds_by.get(cid, [])
            for g in lab["labels"]:
                hit = [p for p in preds if g["a_doc"] in p["ids"]]
                exact = [p for p in hit if p["path"] == g["a_path"]]
                br = [p for p in hit if branch(p["path"], g["a_path"])]
                ok_op = lambda ps: any(p["op"] is None or p["op"] in COMPAT.get(g["op"], ()) for p in ps)
                c = "exact+op" if exact and ok_op(exact) else "exact" if exact else \
                    "branch" if br else "doc_only" if hit else "missed"
                rec[g["op"]][c] += 1
                rec["ALL"][c] += 1
            for p in preds:
                same_doc = [g for g in lab["labels"] if g["a_doc"] in p["ids"]]
                if not p["ids"]:
                    c = "target_doc_unresolved"
                elif any(g["a_path"] == p["path"] for g in same_doc):
                    c = "confirmed_exact"
                elif any(branch(g["a_path"], p["path"]) for g in same_doc):
                    c = "confirmed_branch"
                else:
                    c = "unconfirmed"
                prec[c] += 1
                if c in ("unconfirmed", "target_doc_unresolved"):
                    review.append({"system": name, "id": cid, "b": lab["b_number"], "pred": {**p, "ids": sorted(p["ids"])},
                                   "class": c, "ref": [(g["a_number"], g["a_path"], g["op"]) for g in lab["labels"]][:15]})
        print(f"\n== {name}")
        print("  recall vs reference (by reference op):")
        for op, c in sorted(rec.items(), key=lambda x: -sum(x[1].values())):
            n = sum(c.values())
            print(f"    {op:16} n={n:4}  exact+op {c['exact+op'] / n:.2f}  exact {(c['exact+op'] + c['exact']) / n:.2f}"
                  f"  branch {(c['exact+op'] + c['exact'] + c['branch']) / n:.2f}  missed {c['missed'] / n:.2f}")
        n = sum(prec.values()) or 1
        print("  predictions:", dict(prec), " confirmed (exact+branch) share %.2f" %
              ((prec["confirmed_exact"] + prec["confirmed_branch"]) / n))
    if a.review:
        with open(a.review, "w", encoding="utf-8") as f:
            for x in review:
                f.write(json.dumps(x, ensure_ascii=False) + "\n")


if __name__ == "__main__":
    main()
