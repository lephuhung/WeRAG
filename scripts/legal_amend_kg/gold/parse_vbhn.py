"""Turn VBHN (văn bản hợp nhất) footnotes into provision-level edges.

Each footnote-ref [n] sits on a provision paragraph of the consolidated text;
footnote n says what happened ("Khoản này được bãi bỏ theo quy định tại điểm a
khoản 1 Điều 2 của Nghị định số <a href=/van-ban/chi-tiet/ID>…</a>, có hiệu lực
kể từ ngày …").

Usage: python3 parse_vbhn.py <vbhn dir> <catalogue.tsv> <out jsonl>
"""
import collections, glob, html, json, os, re, sys, unicodedata

sys.path.insert(0, os.path.dirname(__file__))
from build_gold import ART, CLA, ITM, text  # noqa: E402

P_RE = re.compile(r"<p\b([^>]*)>(.*?)</p>", re.S)
REF = re.compile(r'<sup class="footnote-ref">.*?\[(\d+)\].*?</sup>', re.S)
ITEM = re.compile(r'<li class="footnote-item"[^>]*\bid="fn-(\d+)"[^>]*>(.*?)</li>', re.S)
LINK = re.compile(r'href="/van-ban/chi-tiet/([^"#?]+)"')
OPS = [("sửa đổi, bổ sung", "sua_doi_bo_sung"), ("sửa đổi", "sua_doi_bo_sung"), ("bổ sung", "bo_sung"),
       ("bãi bỏ", "bai_bo"), ("thay thế", "thay_the"), ("đính chính", "dinh_chinh"),
       ("hết hiệu lực", "het_hieu_luc"), ("ngưng hiệu lực", "tam_ngung"), ("không áp dụng", "khong_ap_dung")]
SUBJ = re.compile(r"^(Điều|Khoản|Điểm|Cụm từ|Từ|Các cụm từ|Các từ|Mục|Chương|Phụ lục|Tên|Mẫu|Biểu)", re.I)
SRC = re.compile(r"theo quy định tại\s+(.*?)\s+(?:của|tại)\s+(?:Luật|Nghị định|Thông tư|Quyết định|Nghị quyết|Pháp lệnh|Bộ luật)", re.I)
EFF = re.compile(r"có hiệu lực (?:thi hành )?(?:kể )?từ ngày\s+(\d{1,2})\s+tháng\s+(\d{1,2})\s+năm\s+(\d{4})", re.I)
REFPATH = re.compile(r"(?:điểm\s+([a-zđ])\s+)?(?:khoản\s+(\d+[a-z]?)\s+)?Điều\s+(\d+[a-z]?)", re.I)


def ref_path(s):
    m = REFPATH.search(s or "")
    if not m:
        return None
    d, k, a = m.group(1), m.group(2), m.group(3)
    p = f"dieu:{a.lower()}"
    if k:
        p += f"/khoan:{k.lower()}"
    if d:
        p += f"/diem:{d.lower()}"
    return p


def main(vdir, cat_file, out):
    cat = {}
    for l in open(cat_file, encoding="utf-8"):
        f = l.rstrip("\n").split("\t")
        if len(f) > 2:
            cat[f[0]] = f
    # consolidates relations: VBHN -> documents it merges (from the catalogue DB dump)
    cons = collections.defaultdict(list)
    for l in open(os.path.join(vdir, "consolidates.tsv"), encoding="utf-8"):
        v, t = l.rstrip("\n").split("\t")
        cons[v].append(t)
    stats = collections.Counter()
    w = open(out, "w", encoding="utf-8")
    for f in glob.glob(os.path.join(vdir, "html", "*.html")):
        vid = os.path.basename(f)[:-5]
        raw = open(f, encoding="utf-8", errors="replace").read()
        if len(raw) < 200:
            stats["empty_html"] += 1
            continue
        notes = {n: (text(body), LINK.findall(body)) for n, body in ITEM.findall(raw)}
        if not notes:
            stats["no_footnotes"] += 1
            continue
        stats["vbhn_with_footnotes"] += 1
        # The base document is the earliest-issued one the VBHN merges; the
        # others are its amenders (footnotes may link the base too).
        bases = sorted((cat[t][3], t) for t in cons.get(vid, []) if t in cat and cat[t][3])
        base = bases[0][1] if bases else None
        if base is None:
            stats["base_ambiguous"] += 1
        art = cla = None
        for m in P_RE.finditer(raw):
            attrs, body = m.group(1), m.group(2)
            cls = (re.search(r'class="([^"]+)"', attrs) or [None, ""])[1]
            t = text(REF.sub("", body))
            lvl = cls.replace("prov-", "") if cls.startswith("prov-") else "text"
            path = None
            if lvl == "article":
                mm = ART.match(t); art = mm.group(1).lower() if mm else None; cla = None
                path = f"dieu:{art}" if art else None
            elif lvl == "clause":
                mm = CLA.match(t); cla = mm.group(1).lower() if mm else None
                path = f"dieu:{art}/khoan:{cla}" if art and cla else None
            elif lvl in ("item", "point"):
                mm = ITM.match(t)
                path = (f"dieu:{art}/khoan:{cla}/diem:{mm.group(1)}" if art and cla else f"dieu:{art}/diem:{mm.group(1)}") \
                    if art and mm else None
            for n in REF.findall(body):
                note, links = notes.get(n, ("", []))
                low = note.lower()
                op = next((o for k, o in OPS if f"được {k}" in low or f"bị {k}" in low), None)
                eff = EFF.search(note)
                sm = SRC.search(note)
                subj = SUBJ.match(note)
                rec = {
                    "vbhn": vid, "vbhn_number": cat.get(vid, ["", ""])[1], "base_doc": base,
                    "target_level": lvl, "target_path": path, "target_nearest": f"dieu:{art}" if art else None,
                    "subject": subj.group(1) if subj else None, "op": op,
                    "source_doc": links[0] if links else None,
                    "source_path": ref_path(sm.group(1)) if sm else None,
                    "eff_from": f"{eff.group(3)}-{int(eff.group(2)):02d}-{int(eff.group(1)):02d}" if eff else None,
                    "note": note[:400], "provision_text": t[:200],
                }
                stats["edges"] += 1
                stats["op_" + str(op)] += 1
                stats["with_path"] += path is not None
                stats["with_src_doc"] += bool(links)
                stats["with_src_path"] += rec["source_path"] is not None
                stats["with_eff"] += eff is not None
                w.write(json.dumps(rec, ensure_ascii=False) + "\n")
    print(json.dumps(stats, ensure_ascii=False, indent=1))


if __name__ == "__main__":
    main(*sys.argv[1:4])
