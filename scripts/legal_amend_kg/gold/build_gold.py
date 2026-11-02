"""Build a gold set of provision-level legal relations from the editorial
`type="<code>:<source provision id>"` attributes vbpl.vn puts on the
provisions of the TARGET document.

Usage: python3 build_gold.py <crawl dir> <out dir>
Writes provisions.jsonl (id -> doc, level, path, text) and gold_edges.jsonl.
"""
import glob, html, json, os, re, sys, unicodedata

CODES = {"1": "bai_bo", "5": "dinh_chi", "6": "dinh_chinh", "10": "sua_doi_bo_sung",
         "11": "tam_ngung", "12": "thay_the", "13": "bo_sung", "14": "giai_thich"}
P_RE = re.compile(r"<p\b([^>]*)>(.*?)</p>", re.S)
ATTR = lambda a, k: (re.search(r'\b%s="([^"]*)"' % k, a) or [None, None])[1]
ART = re.compile(r"^Điều\s+(\d+[a-zđ]?)\b", re.I)
CLA = re.compile(r"^(\d+[a-zđ]?)\s*[.)]")
ITM = re.compile(r"^([a-zđ](?:\d)?)\s*\)")


def text(s):
    return unicodedata.normalize("NFC", re.sub(r"\s+", " ", html.unescape(re.sub(r"<[^>]+>", " ", s)))).strip()


def parse(doc_id, raw):
    """Yield provisions with a label path; tags carry (code, source id)."""
    art = cla = None
    for m in P_RE.finditer(raw):
        attrs, body = m.group(1), m.group(2)
        cls = ATTR(attrs, "class") or ""
        if not cls.startswith("prov-") or cls == "prov-content":
            continue
        pid, t = ATTR(attrs, "id"), text(body)
        level = cls[5:]
        if level == "article":
            mm = ART.match(t); art = mm.group(1).lower() if mm else None; cla = None
            path = f"dieu:{art}" if art else None
        elif level == "clause":
            mm = CLA.match(t); cla = mm.group(1).lower() if mm else None
            path = f"dieu:{art}/khoan:{cla}" if art and cla else None
        elif level in ("item", "point"):
            mm = ITM.match(t)
            path = f"dieu:{art}/khoan:{cla}/diem:{mm.group(1)}" if art and cla and mm else \
                   (f"dieu:{art}/diem:{mm.group(1)}" if art and mm else None)
        else:
            path = None  # chapter/section/part: not a provision target we key
        # type = editors' relation tags; new-types = further amenders of the
        # same provision (usually later ones), same "<code>:<source id>" form.
        tags = []
        for attr_name in ("type", "new-types"):
            ty = ATTR(attrs, attr_name)
            for part in (ty or "").split(","):
                if ":" in part:
                    c, s = part.split(":", 1)
                    tags.append((c.strip(), s.strip(), attr_name))
        yield {"id": pid, "doc": doc_id, "level": level, "path": path, "text": t[:600], "tags": tags}


def main(crawl, out):
    os.makedirs(out, exist_ok=True)
    provs = {}
    for f in glob.glob(os.path.join(crawl, "html", "*.html")):
        doc = os.path.basename(f)[:-5]
        for p in parse(doc, open(f, encoding="utf-8", errors="replace").read()):
            if p["id"]:
                provs[p["id"]] = p
    with open(os.path.join(out, "provisions.jsonl"), "w", encoding="utf-8") as w:
        for p in provs.values():
            w.write(json.dumps({k: p[k] for k in ("id", "doc", "level", "path", "text")}, ensure_ascii=False) + "\n")
    n = 0
    with open(os.path.join(out, "gold_edges.jsonl"), "w", encoding="utf-8") as w:
        for p in provs.values():
            for code, src, attr_name in p["tags"]:
                s = provs.get(src)
                w.write(json.dumps({
                    "op": CODES.get(code, "code_" + code), "code": code, "attr": attr_name,
                    "target_doc": p["doc"], "target_prov": p["id"], "target_level": p["level"],
                    "target_path": p["path"], "target_text": p["text"][:300],
                    "source_prov": src, "source_doc": s["doc"] if s else None,
                    "source_path": s["path"] if s else None, "source_text": s["text"] if s else None,
                }, ensure_ascii=False) + "\n")
                n += 1
    print(f"provisions {len(provs)}, gold edges {n}")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
