#!/usr/bin/env python3
"""Decide, per Party document, where its full text comes from and build it.

Input: one or more ``classified.jsonl`` (dcs_classify.py). The same document
is often listed on both portals: records are merged by số hiệu + ngày ban
hành, attachments deduplicated by their bytes.

Every document gets one **case** (``plan.jsonl``, one line per document):

=================  =========================================================
``text``           every part has a text layer: Word (.docx/.doc) or a
                   born-digital PDF; legacy TCVN3/VNI fonts converted. The
                   v1 HTML is built here → push mode ``full``.
``ocr``            at least one needed part is a scan (no text, or a
                   scanner's OCR layer, which is never trusted) → the PDFs
                   go to an OCR draft a SuperAdmin reviews.
``catalogue``      no usable file (none listed, link broken, archive that
                   cannot be opened…) → catalogue row only.
``skip``           not a Party document of this collection (a State số
                   hiệu listed by mistake) or no metadata to identify it.
=================  =========================================================

Parts: the main document first, then its phụ lục / đề cương / biểu mẫu in
the portal's order. A Word file and a signed scan of the same document are
one part (the Word text is used, the scan is the signed original).
"""
from __future__ import annotations

import argparse
import collections
import hashlib
import html as htmlmod
import json
import os
import re
import sys
import unicodedata
from datetime import datetime

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))  # siblings, also under python -I
import dcs_classify as clf  # noqa: E402
import dcs_text as txt  # noqa: E402

SOURCE = "dangcongsan"
FORMAT = "werag-legal-html/1"
ID_MAX = 64

# Number type symbol → catalogue type, when the symbol says it alone.
# QĐ is both Quyết định and Quy định for the Party: the portal's type decides.
SYMBOL_TYPE = {
    "NQ": "Nghị quyết", "CT": "Chỉ thị", "KL": "Kết luận", "QĐi": "Quy định", "QC": "Quy chế",
    "HD": "Hướng dẫn", "TB": "Thông báo", "KH": "Kế hoạch", "CTr": "Chương trình", "CV": "Công văn",
    "BC": "Báo cáo", "TTr": "Tờ trình", "ĐA": "Đề án", "CĐ": "Công điện",
}
QD_TYPES = {"Quyết định", "Quy định"}

PARTY_NUMBER_RE = re.compile(r"^(\d{1,5}[a-zA-Z]?)-(\w+)/(\w[\w\-]*?)(?:/nb)?$")
PARTY_TYPES = "NQ|QĐ|QĐi|CT|KL|QC|HD|TB|KH|CTr|CV|BC|TTr|ĐA|CĐ"
PARTY_BODY = r"[\wĐđ]*TW|TU|HU|QU|ĐU|ĐUK|BCĐ|TGV|CQTTBCĐ|BTGDV"
# "76/KL/TW", "36/NQ-TW", "08/CT-TW": a Party number with the wrong separators
LOOSE_PARTY_RE = re.compile(rf"^(\d{{1,5}}[a-zA-Z]?)\s*[/-]\s*({PARTY_TYPES})\s*[/-]\s*({PARTY_BODY})$")
# the portal's address names the document: "…-so-245-qd-tw-ngay-24-01-2025-…"
SLUG_RE = re.compile(r"-so-(\d{1,5}[a-z]?)-([a-z]{2,4})-?([a-z]{2,8})-ngay-(\d{1,2})-?(\d{1,2})-?(\d{4})")
SLUG_TYPES = {"nq": "NQ", "qd": "QĐ", "qdi": "QĐi", "ct": "CT", "kl": "KL", "qc": "QC", "hd": "HD", "tb": "TB",
              "kh": "KH", "ctr": "CTr", "cv": "CV", "bc": "BC"}
SLUG_BODIES = {"tw": "TW", "vptw": "VPTW", "btctw": "BTCTW", "btgtw": "BTGTW", "ubkttw": "UBKTTW", "bdvtw": "BDVTW",
               "bnctw": "BNCTW", "btgdvtw": "BTGDVTW"}


def canonical_number(s: str) -> str:
    n = clean_number(s)
    m = LOOSE_PARTY_RE.match(n)
    if m and not PARTY_NUMBER_RE.match(n):
        return f"{m.group(1)}-{m.group(2)}/{m.group(3)}"
    return n


def from_slug(url: str) -> tuple[str, str]:
    """Số hiệu and ngày the page address spells, "" when it does not."""
    path = url.rsplit("/", 1)[-1]
    m = SLUG_RE.search(path)
    if not m:
        # "…-ngay-10-8-2026-…" alone
        d = re.search(r"-ngay-(\d{1,2})-(\d{1,2})-(\d{4})-", path)
        return "", iso(f"{d.group(1)}/{d.group(2)}/{d.group(3)}") if d else ""
    typ, body = SLUG_TYPES.get(m.group(2)), SLUG_BODIES.get(m.group(3))
    number = f"{m.group(1)}-{typ}/{body}" if typ and body else ""
    return number, iso(f"{m.group(4)}/{m.group(5)}/{m.group(6)}")
APPENDIX_HEAD_RE = re.compile(r"^\W*(PHỤ\s*LỤC|PHỤ\s*BIỂU|ĐỀ\s*CƯƠNG|BIỂU\s*MẪU|DANH\s*MỤC|MẪU\b|Mẫu\b|\(?Kèm\s+theo)", re.I)
APPENDIX_NAME_RE = re.compile(r"(phu[_\- ]?luc|phuluc|_pl\d|\bpl\d|de[_\- ]?cuong|bieu|mau[_\- ]?\d|danh[_\- ]?muc|kem[_\- ]?theo)", re.I)
MAIN_HEAD_RE = re.compile(r"(BAN CHẤP HÀNH TRUNG ƯƠNG|ĐẢNG CỘNG SẢN VIỆT NAM|ĐẢNG BỘ|BAN BÍ THƯ|BỘ CHÍNH TRỊ)")

ARTICLE_RE = re.compile(r"^(?:Điều|ĐIỀU|Ðiều)\s*(\d+[a-zđ]?)\s*[.:]?(?=\s|$)")
CLAUSE_RE = re.compile(r"^(\d{1,3})\s*[.)]\s+\S")
ITEM_RE = re.compile(r"^([a-zđ]{1,2})\s*\)\s*\S")
CHAPTER_RE = re.compile(r"^Chương\s+[IVXLCDM\d]+\b", re.I)
PART_RE = re.compile(r"^Phần\s+(thứ\s+\S+|[IVXLCDM\d]+)\b", re.I)
SECTION_RE = re.compile(r"^Mục\s+\d+\b", re.I)


# ---------------------------------------------------------------------------
# metadata

def clean_number(s: str) -> str:
    s = unicodedata.normalize("NFC", s or "").replace("Ð", "Đ").replace("ð", "đ")
    return re.sub(r"\s+", "", s).strip(" .,;")


def number_key(s: str) -> str:
    return clean_number(s).upper().replace("Đ", "D")


def iso(dmy: str) -> str:
    m = re.match(r"^\s*(\d{1,2})/(\d{1,2})/(\d{4})\s*$", dmy or "")
    if not m:
        return ""
    try:
        return datetime(int(m.group(3)), int(m.group(2)), int(m.group(1))).strftime("%Y-%m-%d")
    except ValueError:
        return ""


def norm_type(t: str) -> str:
    t = re.sub(r"\s+", " ", t or "").strip()
    if not t:
        return ""
    t = t[0].upper() + t[1:].lower()
    return {"Thông báo kết luận": "Thông báo"}.get(t, t)


def doc_type_of(number: str, portal_type: str, head: str) -> tuple[str, str]:
    """The catalogue type and a note when the portal's type was overruled."""
    portal = norm_type(portal_type)
    m = PARTY_NUMBER_RE.match(number)
    sym = m.group(2) if m else ""
    if sym == "QĐ":
        if portal in QD_TYPES:
            return portal, ""
        # the heading of the text tells, when there is one
        h = head.upper()
        if "QUY ĐỊNH" in h[:400] and "QUYẾT ĐỊNH" not in h[:400]:
            return "Quy định", f"loại trên trang là {portal!r}, số hiệu QĐ và tiêu đề QUY ĐỊNH"
        return "Quyết định", f"loại trên trang là {portal!r}, số hiệu QĐ" if portal else ""
    by_sym = SYMBOL_TYPE.get(sym)
    if by_sym and portal and portal != by_sym:
        return by_sym, f"loại trên trang là {portal!r}, ký hiệu {sym} là {by_sym}"
    return by_sym or portal, ""


TEXT_NUMBER_RE = re.compile(r"Số\s*[:.]?\s*(\d{1,5}[a-zA-Z]?\s*-\s*[A-Za-zĐđÐ]+\s*/\s*[A-Za-zĐđÐ][\wĐđÐ\-]*)")
TEXT_TYPE_RE = re.compile(r"\b(NGHỊ QUYẾT|QUYẾT ĐỊNH|QUY ĐỊNH|QUY CHẾ|CHỈ THỊ|KẾT LUẬN|HƯỚNG DẪN|THÔNG BÁO|KẾ HOẠCH|"
                          r"CHƯƠNG TRÌNH|BÁO CÁO|TỜ TRÌNH|ĐỀ ÁN|CÔNG ĐIỆN)\b")
# the header's "Hà Nội, ngày 23 tháng 5 năm 2026", not a cited date ("Căn cứ … ngày …")
TEXT_DATE_RE = re.compile(r"(?:[A-ZĐ][a-zà-ỹ]+\s+)*[A-ZĐ][a-zà-ỹ]+,\s*ngày\s+(\d{1,2})\s+tháng\s+(\d{1,2})\s+năm\s+(\d{4})")


def identity_from_text(head: str) -> dict:
    """Số hiệu, loại and ngày ban hành the document's own header states."""
    out = {}
    h = head[:900]
    m = TEXT_NUMBER_RE.search(h)
    if m:
        out["number"] = clean_number(m.group(1))
    m = TEXT_TYPE_RE.search(h)
    if m:
        out["type"] = norm_type(m.group(1))
    elif re.search(r"\bV/v\b", h):
        out["type"] = "Công văn"
    m = TEXT_DATE_RE.search(h)
    if m:
        out["issued"] = iso(f"{m.group(1)}/{m.group(2)}/{m.group(3)}")
    return out


def lower_first(s: str) -> str:
    return s[:1].lower() + s[1:] if s else s


def full_title(doc_type: str, number: str, issuer: str, summary: str) -> str:
    """"Kết luận số 114-KL/TW của Bộ Chính trị về …" from the portal's
    trích yếu, which is either "Về …" or "Kết luận của Bộ Chính trị về …"."""
    s = re.sub(r"\s+", " ", summary or "").strip()
    head = f"{doc_type} số {number}"
    if s.lower().startswith(doc_type.lower()):
        rest = s[len(doc_type):].strip()
        rest = re.sub(r"^(số\s+)?" + re.escape(number) + r"\s*,?", "", rest, flags=re.I).strip()
        return f"{head} {rest}".strip()
    if issuer and not re.match(r"^(của|về)\b", s, re.I):
        return f"{head} của {issuer} {lower_first(s)}".strip()
    if issuer and s.lower().startswith("về"):
        return f"{head} của {issuer} {lower_first(s)}"
    return f"{head} {lower_first(s)}".strip()


# ---------------------------------------------------------------------------
# merging records

def load(paths: list[str]) -> list[dict]:
    recs = []
    for p in paths:
        with open(p, encoding="utf-8") as f:
            recs.extend(json.loads(line) for line in f)
    return recs


def merge(recs: list[dict]) -> list[dict]:
    """One document per (số hiệu, ngày ban hành); tulieu first (wider
    metadata), attachments of both sites, the same bytes once."""
    groups: dict[tuple, list[dict]] = collections.OrderedDict()
    for r in sorted(recs, key=lambda r: r["site"] != "tulieu"):
        num = canonical_number(r["meta"].get("number") or r.get("list_number") or "")
        day = iso(r["meta"].get("issued") or r.get("list_issued") or "")
        if not PARTY_NUMBER_RE.match(num) or not day:
            # the listing has a date where the number goes, or no date: the
            # page address spells both
            snum, sday = from_slug(r["url"])
            repaired = []
            if snum and not PARTY_NUMBER_RE.match(num) and not re.match(r"^\d+/\d{4}/", num):
                repaired.append(f"số hiệu {num!r} → {snum} (theo địa chỉ trang)")
                num = snum
            if sday and not day:
                repaired.append(f"ngày ban hành → {sday} (theo địa chỉ trang)")
                day = sday
            if repaired:
                r["meta"]["number"] = num
                r["meta"]["issued"] = "/".join(reversed(day.split("-"))) if day else ""
                r["repaired"] = repaired
        elif num != r["meta"].get("number"):
            r["meta"]["number"] = num
        groups.setdefault((number_key(num), day), []).append(r)
    docs = []
    for (key, day), rs in groups.items():
        meta: dict = {}
        for r in rs:
            for k, v in r["meta"].items():
                if v and v != "---" and not meta.get(k):
                    meta[k] = v
        if not meta.get("number"):
            meta["number"] = rs[0].get("list_number", "")
        if not meta.get("issued"):
            meta["issued"] = rs[0].get("list_issued", "")
        if not meta.get("summary"):
            meta["summary"] = rs[0].get("list_title", "")
        atts, seen = [], set()
        for r in rs:
            for a in r["attachments"]:
                digest = ""
                if a.get("path") and os.path.exists(a["path"]):
                    with open(a["path"], "rb") as f:
                        digest = hashlib.sha1(f.read()).hexdigest()
                k = digest or a["url"]
                if k in seen:
                    continue
                seen.add(k)
                atts.append(a)
        docs.append({"key": key, "issued": day, "meta": meta, "records": rs, "attachments": atts,
                     "repaired": list(dict.fromkeys(x for r in rs for x in r.get("repaired", [])))})
    return docs


# ---------------------------------------------------------------------------
# parts

def part_role(a: dict, number: str) -> str:
    an = a.get("analysis") or {}
    head = (an.get("head") or "")[:250]
    if head and APPENDIX_HEAD_RE.search(head[:60]):
        return "appendix"
    if head and MAIN_HEAD_RE.search(head[:160]):
        return "main"
    if APPENDIX_NAME_RE.search(a.get("name", "")):
        return "appendix"
    return "main?"


def part_need(a: dict) -> str:
    """text | ocr | unusable for one attachment."""
    an = a.get("analysis") or {}
    cls = an.get("class") or ""
    if cls == "word_text":
        return "text" if an.get("encoding") in ("unicode", "tcvn3", "vni") else "unusable"
    if cls == "pdf_text":
        # a text PDF in a font we cannot map is read from its images
        return "text" if an.get("encoding") in ("unicode", "tcvn3", "vni") else "ocr"
    if cls.startswith("pdf_"):
        return "ocr" if ("scan" in cls or "ocr_layer" in cls) else "unusable"
    if cls == "word_pictures":
        return "unusable"  # pictures in Word: no PDF to OCR (counted, rare)
    return "unusable"


def same_text_doc(word: dict, pdf: dict, number: str) -> bool:
    """A Word file and a scan/PDF that are the same document: the Word text
    names the document's own number in its header."""
    head = (word.get("analysis") or {}).get("head") or ""
    return number_key(number)[:12] in number_key(head[:300]) or number_key(number) in number_key(head)


def plan_parts(doc: dict) -> list[dict]:
    number = clean_number(doc["meta"].get("number", ""))
    usable = []
    for a in doc["attachments"]:
        an = a.get("analysis") or {}
        if an.get("class") == "archive":
            for m in an.get("members", []):
                usable.append({"name": m.get("name", ""), "url": a["url"], "path": a.get("path"), "member": m.get("name"),
                               "analysis": m})
            continue
        usable.append({"name": a["name"], "url": a["url"], "path": a.get("path"), "analysis": an})
    parts = []
    for a in usable:
        a["need"] = part_need(a)
        a["role"] = part_role(a, number)
        parts.append(a)
    # Word + PDF of the same document: one part, read from the Word file
    words = [p for p in parts if p["need"] == "text" and (p["analysis"].get("type") in ("doc", "docx"))]
    for w in words:
        if w["role"] == "appendix":
            continue
        for p in parts:
            if p is w or p.get("twin_of") or p["analysis"].get("type") != "pdf" or p["role"] == "appendix":
                continue
            if same_text_doc(w, p, number) and p["need"] != "text" or (
                    same_text_doc(w, p, number) and p["analysis"].get("pages", 0) and p["role"] != "appendix"):
                p["twin_of"] = w["name"]
                w["signed_original"] = p["url"]
                break
    parts = [p for p in parts if not p.get("twin_of")]
    # main first
    parts.sort(key=lambda p: {"main": 0, "main?": 1, "appendix": 2}[p["role"]])
    return parts


def decide(doc: dict) -> tuple[str, str, list[dict]]:
    meta = doc["meta"]
    number = clean_number(meta.get("number", ""))
    if not number or not doc["issued"]:
        return "skip", "không có số hiệu hoặc ngày ban hành", []
    if not PARTY_NUMBER_RE.match(number):
        return "skip", f"số hiệu {number!r} không phải kiểu văn bản của Đảng (văn bản Nhà nước hoặc lỗi số hiệu)", []
    parts = plan_parts(doc)
    good = [p for p in parts if p["need"] != "unusable"]
    if not good:
        why = "không có tệp đính kèm" if not doc["attachments"] else "tệp đính kèm không dùng được: " + ", ".join(
            sorted({(p["analysis"].get("class") or "?") for p in parts} or {"tải lỗi"}))
        return "catalogue", why, parts
    if any(p["need"] == "ocr" for p in good):
        return "ocr", "có phần là bản scan: " + ", ".join(p["name"] for p in good if p["need"] == "ocr"), good
    return "text", "", good


# ---------------------------------------------------------------------------
# v1 HTML

class Path:
    def __init__(self):
        self.dieu = self.khoan = self.diem = None

    def value(self, level):
        p = f"dieu:{self.dieu}"
        if level in ("clause", "item") and self.khoan is not None:
            p += f"/khoan:{self.khoan}"
        if level == "item" and self.diem is not None:
            p += f"/diem:{self.diem}"
        return p


def _cell(s: str) -> str:
    return "<br>".join(htmlmod.escape(x) for x in s.split("\n"))


def blocks_html(blocks: list, prefix: str, structured: bool, seen: dict, report: dict) -> list[str]:
    out = []
    path = Path()
    for kind, val in blocks:
        if kind == "table":
            rows = [r for r in val if any(c.strip() for c in r)]
            if not rows:
                continue
            out.append("<table>" + "".join("<tr>" + "".join(f"<td>{_cell(c)}</td>" for c in r) + "</tr>" for r in rows)
                       + "</table>")
            continue
        text = re.sub(r"[ \t ]+", " ", val).strip()
        if not text:
            continue
        cls, level = "prov-content" if structured else "", None
        if structured:
            if PART_RE.match(text):
                cls = "prov-part"
            elif CHAPTER_RE.match(text):
                cls = "prov-chapter"
            elif SECTION_RE.match(text):
                cls = "prov-section"
            elif ARTICLE_RE.match(text):
                path.dieu, path.khoan, path.diem = ARTICLE_RE.match(text).group(1), None, None
                cls, level = "prov-article", "article"
            elif path.dieu is not None and CLAUSE_RE.match(text):
                path.khoan, path.diem = CLAUSE_RE.match(text).group(1), None
                cls, level = "prov-clause", "clause"
            elif path.dieu is not None and ITEM_RE.match(text):
                path.diem = ITEM_RE.match(text).group(1).lower()
                cls, level = "prov-item", "item"
        attrs = f' class="{cls}"' if cls else ""
        if level:
            eid = f"{prefix}-{path.value(level)}"
            if eid in seen:
                seen[eid] += 1
                eid = f"{eid}~{seen[eid]}"
            else:
                seen[eid] = 1
            if len(eid) <= ID_MAX:
                attrs += f' id="{htmlmod.escape(eid)}"'
                report[level + "s"] += 1
            else:
                report["problems"].append(f"id dài quá {ID_MAX}: {eid}")
        out.append(f"<p{attrs}>{htmlmod.escape(text)}</p>")
    return out


def count_articles(blocks: list) -> int:
    return sum(1 for k, v in blocks if k == "p" and ARTICLE_RE.match(re.sub(r"\s+", " ", v).strip()))


def build_html(title: str, prefix: str, main_blocks: list, appendices: list[tuple[str, list]]) -> tuple[str, dict]:
    report = collections.Counter()
    report["problems"] = []
    structured = count_articles(main_blocks) >= 2
    seen: dict = {}
    lines = blocks_html(main_blocks, prefix, structured, seen, report)
    for name, blocks in appendices:
        lines.append('<div class="doc-region-appendix">')
        lines.extend(blocks_html(blocks, prefix + "-pl", False, seen, report))
        lines.append("</div>")
    report["structured"] = structured
    doc = ("<!DOCTYPE html>\n<html lang=\"vi\"><head><meta charset=\"utf-8\">"
           f"<title>{htmlmod.escape(title)}</title></head><body>\n" + "\n".join(lines) + "\n</body></html>\n")
    return doc, dict(report)


def text_quality(blocks: list) -> dict:
    t = "\n".join(v if k == "p" else "\n".join(" ".join(r) for r in v) for k, v in blocks)
    return {"chars": len(t), "encoding": clf.encoding_of(t), "quality": clf.vi_quality(t)}


def read_part(p: dict) -> list:
    with open(p["path"], "rb") as f:
        data = f.read()
    if p.get("member"):
        data = archive_member(data, p["member"])
    kind = clf.sniff(data)
    blocks, scanned = txt.blocks_of(kind, data)
    if scanned:
        raise ValueError(f"trang scan {scanned} trong tệp được coi là có chữ")
    return blocks


def archive_member(data: bytes, name: str) -> bytes:
    import subprocess
    import tempfile
    with tempfile.TemporaryDirectory() as d:
        src = os.path.join(d, "a")
        with open(src, "wb") as f:
            f.write(data)
        subprocess.run(["7z", "x", "-y", "-o" + d + "/x", src], capture_output=True, timeout=120, check=True)
        for root, _, files in os.walk(d + "/x"):
            if name in files:
                with open(os.path.join(root, name), "rb") as f:
                    return f.read()
    raise FileNotFoundError(name)


# ---------------------------------------------------------------------------

def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("classified", nargs="+")
    ap.add_argument("--out-dir", required=True)
    a = ap.parse_args(argv)
    os.makedirs(os.path.join(a.out_dir, "html"), exist_ok=True)
    docs = merge(load(a.classified))
    cases = collections.Counter()
    notes = collections.Counter()
    with open(os.path.join(a.out_dir, "plan.jsonl"), "w", encoding="utf-8") as fout:
        for doc in docs:
            meta = doc["meta"]
            rec0 = doc["records"][0]
            number = clean_number(meta.get("number", ""))
            case, why, parts = decide(doc)
            head = ""
            for p in parts:
                head = (p["analysis"].get("head") or "")
                if head:
                    break
            dtype, type_note = doc_type_of(number, meta.get("doc_type", ""), head)
            issuer = re.sub(r"\s+", " ", meta.get("issuer", "")).strip()
            title = full_title(dtype, number, issuer, meta.get("summary", "")) if dtype else ""
            md = {"source": SOURCE, "source_url": rec0["url"], "source_doc_id": rec0["source_doc_id"], "format": FORMAT,
                  "doc_number": number, "doc_type": dtype, "issuer": issuer, "issue_date": doc["issued"],
                  "title": title}
            eff = iso(meta.get("effective", ""))
            if eff:
                md["eff_from"] = eff
            for k in ("signer", "field"):
                if meta.get(k):
                    md[k] = re.sub(r"\s+", " ", meta[k]).strip()
            entry = {"case": case, "why": why, "metadata": md, "type_note": type_note, "repaired": doc.get("repaired", []),
                     "urls": [r["url"] for r in doc["records"]],
                     "parts": [{"name": p["name"], "url": p["url"], "path": p.get("path"), "member": p.get("member"),
                                "role": p["role"], "need": p["need"], "class": p["analysis"].get("class"),
                                "pages": p["analysis"].get("pages"), "signed_original": p.get("signed_original")}
                               for p in parts]}
            if case == "text":
                try:
                    blocks = [read_part(p) for p in parts]
                    main_blocks, apps = blocks[0], [(p["name"], b) for p, b in zip(parts[1:], blocks[1:])]
                    q = text_quality(main_blocks)
                    entry["quality"] = q
                    # the document's own header is the authority over the portal's listing
                    own = identity_from_text(" ".join(v if k == "p" else " ".join(" ".join(r) for r in v)
                                                      for k, v in main_blocks[:40]))
                    conflicts = []
                    if own.get("number") and number_key(own["number"]) != number_key(number):
                        conflicts.append(f"số hiệu trên trang {number}, trong văn bản {own['number']}")
                        md["doc_number"] = own["number"]
                    if own.get("type") and own["type"] != md["doc_type"] and own["type"] in set(SYMBOL_TYPE.values()) | QD_TYPES:
                        conflicts.append(f"loại {md['doc_type']} → {own['type']} theo tiêu đề văn bản")
                        md["doc_type"] = own["type"]
                    if own.get("issued") and own["issued"] != md["issue_date"]:
                        conflicts.append(f"ngày ban hành trên trang {md['issue_date']}, trong văn bản {own['issued']}")
                        md["issue_date"] = own["issued"]
                    if conflicts:
                        entry["identity_conflicts"] = conflicts
                        md["title"] = full_title(md["doc_type"], md["doc_number"], issuer, meta.get("summary", ""))
                        title = md["title"]
                    if q["encoding"] != "unicode" or q["chars"] < 200:
                        # a converter that did not give Vietnamese: read the images instead
                        entry["case"], entry["why"] = ("ocr", f"chữ đọc ra không phải tiếng Việt ({q['encoding']}, {q['chars']} ký tự)") \
                            if any(p["analysis"].get("type") == "pdf" for p in parts) else ("catalogue", f"chữ đọc ra không dùng được ({q['encoding']})")
                    else:
                        prefix = f"{SOURCE}-{md['source_doc_id']}"
                        html, report = build_html(title, prefix, main_blocks, apps)
                        name = re.sub(r"[^\w.\-]+", "_", md["source_doc_id"]) + ".html"
                        with open(os.path.join(a.out_dir, "html", name), "w", encoding="utf-8") as f:
                            f.write(html)
                        entry["html"] = os.path.join("html", name)
                        entry["report"] = report
                except Exception as e:  # noqa: BLE001
                    has_pdf = any(p["analysis"].get("type") == "pdf" for p in parts)
                    entry["case"] = "ocr" if has_pdf else "catalogue"
                    entry["why"] = f"đọc tệp lỗi: {e}"[:300]
            cases[entry["case"]] += 1
            if type_note:
                notes["type_overruled"] += 1
            fout.write(json.dumps(entry, ensure_ascii=False) + "\n")
    print(f"{len(docs)} documents: " + ", ".join(f"{k} {v}" for k, v in cases.most_common()), file=sys.stderr)
    if notes:
        print(dict(notes), file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
