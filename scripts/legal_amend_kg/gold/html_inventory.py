"""Inventory of tags, classes and attributes in crawled vbpl HTML.

Usage: python3 html_inventory.py <html dir> [<html dir> ...]
"""
import collections, glob, html.parser, os, re, sys


class Inv(html.parser.HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.tags = collections.Counter(); self.cls = collections.Counter(); self.attrs = collections.Counter()
        self.vals = collections.defaultdict(collections.Counter); self.seen = set()

    def handle_starttag(self, tag, attrs):
        self.tags[tag] += 1; self.seen.add("tag:" + tag)
        for k, v in attrs:
            v = v or ""
            self.attrs[f"{tag}@{k}"] += 1; self.seen.add(f"attr:{tag}@{k}")
            if k == "class":
                for c in v.split():
                    self.cls[f"{tag}.{c}"] += 1; self.seen.add(f"class:{tag}.{c}")
            elif k in ("style", "id"):
                shape = "uuid" if re.fullmatch(r"[0-9a-f-]{36}", v) else "num" if v.isdigit() else re.sub(r"\d+", "#", v)[:40]
                if k == "style":
                    for decl in v.split(";"):
                        if ":" in decl:
                            self.vals[f"{tag}@style"][decl.split(":")[0].strip().lower()] += 1
                else:
                    self.vals[f"{tag}@id"][shape] += 1
            else:
                shape = re.sub(r"[0-9a-f]{8}-[0-9a-f-]{27}", "<uuid>", v)
                shape = re.sub(r"\d+", "#", shape)[:60]
                self.vals[f"{tag}@{k}"][shape] += 1


for d in sys.argv[1:]:
    inv = Inv(); docs = collections.Counter(); n = empty = 0
    for f in glob.glob(os.path.join(d, "*.html")):
        t = open(f, encoding="utf-8", errors="replace").read()
        n += 1
        if len(t) < 200:
            empty += 1; continue
        inv.seen = set(); inv.feed(t); inv.close() if False else None
        for s in inv.seen:
            docs[s] += 1
    print(f"\n######## {d}: {n} files, {empty} empty")
    print("\n# tags (count, docs)")
    for k, v in inv.tags.most_common():
        print(f"  {k:12} {v:9} {docs['tag:' + k]:6}")
    print("\n# classes (count, docs)")
    for k, v in inv.cls.most_common():
        print(f"  {k:40} {v:9} {docs['class:' + k]:6}")
    print("\n# attributes (count, docs) + top value shapes")
    for k, v in inv.attrs.most_common():
        if k.endswith("@class"):
            continue
        top = ", ".join(f"{s!r}:{c}" for s, c in inv.vals[k].most_common(6))
        print(f"  {k:24} {v:9} {docs['attr:' + k]:6}  {top}")
