"""Convert a thuvienphapluat.vn (TVPL) document page to "WeRAG legal HTML v1".

The format is docs/legal-document-feed-spec.md §9.2: the prov-* classes of
vbpl for the structure, an id on every Điều / khoản / điểm, and on each
provision another document changes a data-amend attribute holding a JSON
array of marks.

What TVPL gives (checked on live pages, 2026-11-03):

* the body is in div.content1, one <p> per paragraph, tables as <table>;
* Điều / khoản / điểm carry anchors <a name="dieu_4">, <a name="khoan_5_4">
  (khoản 5 of Điều 4), <a name="diem_a_5_4">; not every one has an anchor,
  so the text ("Điều 5.", "2.", "a)") is the fallback;
* a provision another document changed has atmm=".lqhlTootip-<bookmark>"
  on its anchor; the note behind it (GetLqhlTip, loaded by tvpl_fetch) is
    <div id="bmTitle" class="clsBookmark1 bmTitle" tt="<bookmark>-1" crid="<TVPL id of B>">
      Khoản này được sửa đổi bởi Điểm a Khoản 1 Điều 1 Luật Doanh nghiệp
      sửa đổi 2025 có hiệu lực từ ngày 01/07/2025</div>
    <div id="bmContent" class="ct-<bookmark>-1">new wording …
      <a href="…/Luat-…-so-76-2025-QH15-659899.aspx?anchor=diem_a_1_1">Xem nội dung VB</a></div>
    <div id="bmContentOld" class="ctOld-<bookmark>-1">old wording</div>
  repeated once per amending document;
* inserted guidance (<huongdan>, "Bổ sung") is TVPL's own addition: dropped,
  the new wording is always read from B (§9.2).

The note text is kept verbatim in "note" for the SuperAdmin to check.
"""

import html as htmlmod
import json
import re
import unicodedata
import urllib.parse

from bs4 import BeautifulSoup, NavigableString, Tag

FORMAT = "werag-legal-html/1"
SOURCE = "tvpl"
ID_MAX = 64

# --- the amendment note -------------------------------------------------

# Longest phrases first: "sửa đổi, bổ sung" before "sửa đổi".
_OPS = [
    (r"sửa\s*đổi\s*,?\s*bổ\s*sung", "sua_doi_bo_sung"),
    (r"bổ\s*sung", "bo_sung"),
    (r"sửa\s*đổi", "sua_doi"),
    (r"thay\s*thế", "thay_the"),
    (r"bãi\s*bỏ|hết\s*hiệu\s*lực", "bai_bo"),
    (r"tạm\s*ngưng|tạm\s*ngừng|ngưng\s*hiệu\s*lực|ngừng\s*hiệu\s*lực", "tam_ngung"),
    (r"đình\s*chỉ", "dinh_chi"),
    (r"đính\s*chính", "dinh_chinh"),
    (r"giải\s*thích", "giai_thich"),
    (r"hướng\s*dẫn|quy\s*định\s*chi\s*tiết", "huong_dan"),
]
_OP_PHRASES = "|".join(p for p, _ in _OPS)
# "Khoản này được sửa đổi bởi …", "Điều này hết hiệu lực bởi …" (no được/bị)
_OP_RE = re.compile(r"(?:(?:được|bị)\s+(" + _OP_PHRASES + r")|(" + _OP_PHRASES + r")\s+bởi\b)", re.I)
# A note without one of those phrases ("… thực hiện theo quy định tại Điều
# 10 Nghị định 151/2025/NĐ-CP", "Ủy ban nhân dân cấp xã có trách nhiệm …
# theo …") still names B: kept as huong_dan with op_inferred, i.e. stored
# and shown for review, never used by the current-wording resolver.
_B_EXPIRY_RE = re.compile(r"\(\s*VB\s+hết\s+hiệu\s+lực\s*:?[^)]*\)", re.I)
_ABOUT_OTHER_RE = re.compile(r"\s*Nội\s+dung\b", re.I)
_EXPIRES_RE = re.compile(r"\bhết\s+hiệu\s+lực\b", re.I)
_REF_RE = re.compile(r"\b(?:theo(?:\s+quy\s+định)?(?:\s+tại)?|tại)\b", re.I)

# A số hiệu written in the note: 80/2021/NĐ-CP, 152/QĐ-UBND, 43/2024/QH15.
NUMBER_RE = re.compile(r"\b(\d{1,5}(?:/\d{4})?/[A-ZĐ][A-ZĐa-z0-9]*(?:-[A-ZĐ][A-ZĐa-z0-9]*)*)")
_DATE_RE = re.compile(r"(\d{1,2})/(\d{1,2})/(\d{4})")
_EFF_RE = re.compile(r"hiệu\s*lực\s*(?:thi\s*hành\s*)?(?:kể\s*)?từ\s*ngày\s*(\d{1,2}/\d{1,2}/\d{4})", re.I)
_EFF_TO_RE = re.compile(r"(?:đến|tới)\s*(?:hết\s*)?ngày\s*(\d{1,2}/\d{1,2}/\d{4})", re.I)

# TVPL slugs drop the diacritics of a số hiệu: ND-CP is NĐ-CP. Tokens that
# are an abbreviation with Đ in it, restored for display (the server's
# catalogue key folds Đ to D, so either form resolves).
_SLUG_D = {"ND": "NĐ", "QD": "QĐ", "HD": "HĐ", "HDND": "HĐND", "HDTP": "HĐTP", "UBTVQH": "UBTVQH",
           "TTLT": "TTLT", "NQLT": "NQLT", "DCT": "ĐCT", "HDBCQG": "HĐBCQG", "HDQT": "HĐQT", "DHD": "ĐHĐ"}


def iso_date(dmy):
    m = _DATE_RE.fullmatch(dmy.strip()) if dmy else None
    if not m:
        return ""
    d, mo, y = (int(x) for x in m.groups())
    if not (1 <= d <= 31 and 1 <= mo <= 12):
        return ""
    return f"{y:04d}-{mo:02d}-{d:02d}"


def number_from_slug(url):
    """The số hiệu in a TVPL document URL, or "".

    …/Luat-Doanh-nghiep-sua-doi-2025-so-76-2025-QH15-659899.aspx → 76/2025/QH15
    …/Nghi-dinh-70-2025-ND-CP-sua-doi-Nghi-dinh-123-2020-ND-CP-…-577816.aspx → 70/2025/NĐ-CP
    …/Quyet-dinh-152-QD-UBND-2024-phe-duyet-…-603456.aspx → 152/QĐ-UBND
    The first number of the slug is the document's own (titles name the
    amended document after it).
    """
    path = urllib.parse.urlsplit(url).path
    slug = path.rsplit("/", 1)[-1]
    slug = re.sub(r"-\d+\.aspx$", "", slug)
    toks = slug.split("-")
    for i, t in enumerate(toks):
        if not t.isdigit():
            continue
        j = i + 1
        year = ""
        if j < len(toks) and re.fullmatch(r"(19|20)\d\d", toks[j]) and j + 1 < len(toks) and _is_code(toks[j + 1]):
            year = toks[j]
            j += 1
        codes = []
        while j < len(toks) and _is_code(toks[j]):
            codes.append(_SLUG_D.get(toks[j], toks[j]))
            j += 1
        if codes:
            return "/".join([t] + ([year] if year else []) + ["-".join(codes)])
    return ""


def _is_code(tok):
    # an issuer/type code: QH15, BTC, ND, TTg, UBTVQH14 — not a slug word
    # (Nghi, Luat) or a bare number
    return bool(re.fullmatch(r"[A-Z]{2,}[a-z]?[0-9]*", tok))


_ANCHOR_RE = re.compile(r"^(?:diem_([a-zđ]{1,2})_(\d+)_(\d+)|khoan_(\d+)_(\d+)|dieu_(\d+))$")


def path_from_anchor(anchor):
    """TVPL anchor → provision path: diem_a_1_1 → dieu:1/khoan:1/diem:a."""
    m = _ANCHOR_RE.match((anchor or "").strip().lower())
    if not m:
        return ""
    if m.group(1):
        # TVPL spells điểm đ "dd" in anchors (diem_dd_7_1); the server's key is "đ"
        x = "đ" if m.group(1) == "dd" else m.group(1)
        return f"dieu:{m.group(3)}/khoan:{m.group(2)}/diem:{x}"
    if m.group(4):
        return f"dieu:{m.group(5)}/khoan:{m.group(4)}"
    return f"dieu:{m.group(6)}"


_TEXT_PATH_RE = re.compile(
    r"(?:Điểm\s+([a-zđ]{1,2})\s+)?(?:Khoản\s+(\d+)\s+)?Điều\s+(\d+[a-zđ]?)\b", re.I)


def path_from_text(text):
    """"Điểm a Khoản 1 Điều 1 …" → dieu:1/khoan:1/diem:a (first one only)."""
    # the part after "bởi": "Khoản này được sửa đổi bởi Khoản 3 Điều 1 …"
    tail = re.split(r"\bbởi\b", text, maxsplit=1)
    m = _TEXT_PATH_RE.search(tail[-1])
    if not m:
        return ""
    p = f"dieu:{m.group(3).lower()}"
    if m.group(2):
        p += f"/khoan:{m.group(2)}"
    if m.group(1):
        p += f"/diem:{m.group(1).lower()}"
    return p


def number_key(num):
    """A số hiệu reduced like the server's CatalogNumberKey (Đ → D, upper)."""
    return re.sub(r"\s+", "", num).upper().replace("Đ", "D")


def merge_paths(anchor_path, text_path):
    """The position in B from the link anchor and from the note's words.

    TVPL often links the Điều while the note names the khoản ("bởi Khoản 3
    Điều 2", anchor=dieu_2): when one is a prefix of the other the longer
    one wins. When they disagree the anchor is kept and the clash reported.
    Returns (path, problem or "")."""
    if not anchor_path or not text_path or anchor_path == text_path:
        return anchor_path or text_path, ""
    if text_path.startswith(anchor_path + "/"):
        return text_path, ""
    if anchor_path.startswith(text_path + "/"):
        return anchor_path, ""
    return anchor_path, f"link points to {anchor_path} but the note says {text_path}; kept the link's"


def parse_tip(raw):
    """The marks of one GetLqhlTip answer. Returns (marks, problems)."""
    marks, problems = [], []
    if not raw or not raw.strip():
        return marks, ["empty note"]
    soup = BeautifulSoup(raw, "html.parser")
    titles = soup.select("div.bmTitle") or soup.find_all("div", id="bmTitle")
    if not titles:
        # the short form (GetLqhlTootip): <div class='clsBookmark1'>…</div>
        titles = soup.find_all("div", class_=re.compile(r"^clsBookmark"))
    for t in titles:
        note = " ".join(t.get_text(" ", strip=True).split())
        tt = t.get("tt", "")
        body = soup.find("div", class_="ct-" + tt) if tt else t.find_next_sibling("div", id="bmContent")
        link = body.find("a", href=True) if body else None
        href = link["href"] if link else ""
        mk = {"note": note}
        # TVPL appends B's own expiry ("(VB hết hiệu lực: 01/07/2026)"): not
        # part of what the note says about A
        note = _B_EXPIRY_RE.sub("", note).strip()
        m = _OP_RE.search(note)
        if m:
            phrase = m.group(1) or m.group(2)
            mk["op"] = next(op for pat, op in _OPS if re.fullmatch(pat, phrase, re.I))
            if _ABOUT_OTHER_RE.match(note) and NUMBER_RE.search(note[:m.start()]):
                # the subject is another document: "Nội dung hướng dẫn Điều
                # này tại Nghị định 103/2024/NĐ-CP nay được sửa đổi bởi …" —
                # the guiding decree changed, not A: informational only
                mk["op"], mk["op_inferred"] = "huong_dan", "1"
        elif _EXPIRES_RE.search(note):
            # "Điểm này hết hiệu lực từ ngày … theo Nghị định …"
            m = _EXPIRES_RE.search(note)
            refs = [r for r in _REF_RE.finditer(note) if r.start() >= m.start()]
            m = refs[-1] if refs else m
            mk["op"] = "bai_bo"
        else:
            # the last "theo/tại" before B's number, else the whole note
            refs = list(_REF_RE.finditer(note))
            m = refs[-1] if refs else re.match(r"", note)
            mk["op"], mk["op_inferred"] = "huong_dan", "1"
        # số hiệu and position of B: written after the operation, else the
        # B link's slug
        after = note[m.end():]
        num = NUMBER_RE.search(after)
        slug_num = number_from_slug(href)
        mk["doc_number"] = num.group(1) if num else slug_num
        if not mk["doc_number"]:
            what = "no operation and no amending document" if mk.get("op_inferred") else "no số hiệu for the amending document"
            problems.append(f"{what}: {note[:120]}")
            continue
        if num and slug_num and number_key(num.group(1)) != number_key(slug_num):
            problems.append(f"note names {num.group(1)} but links to {slug_num}; kept the note's: {note[:100]}")
        q = urllib.parse.parse_qs(urllib.parse.urlsplit(href).query)
        path, why = merge_paths(path_from_anchor((q.get("anchor") or [""])[0]), path_from_text(after))
        if why:
            problems.append(f"{why}: {note[:100]}")
        if path:
            mk["path"] = path
        e = _EFF_RE.search(note)
        if e and iso_date(e.group(1)):
            mk["eff_from"] = iso_date(e.group(1))
        if mk["op"] == "tam_ngung":
            e2 = _EFF_TO_RE.search(note)
            if e2 and iso_date(e2.group(1)):
                mk["eff_to"] = iso_date(e2.group(1))
        crid = t.get("crid", "").strip()
        if crid:
            mk["tvpl_doc_id"] = crid  # informational, like op_inferred; the server ignores unknown keys
        marks.append({k: mk[k] for k in ("op", "doc_number", "path", "eff_from", "eff_to", "note", "tvpl_doc_id", "op_inferred") if mk.get(k)})
    return marks, problems


# --- the document -------------------------------------------------------

def page_properties(soup):
    """The "Thuộc tính" rows of a TVPL page: số hiệu, loại, cơ quan, dates."""
    out = {}
    want = {"Số hiệu:": "doc_number", "Loại văn bản:": "doc_type", "Nơi ban hành:": "issuer",
            "Ngày ban hành:": "issue_date", "Ngày hiệu lực:": "eff_from", "Tình trạng:": "eff_status"}
    for td in soup.find_all(["td", "th"]):
        label = " ".join(td.get_text(" ", strip=True).split())
        key = want.get(label)
        if not key or key in out:
            continue
        val = td.find_next_sibling(["td", "th"])
        if val is None:
            continue
        v = " ".join(val.get_text(" ", strip=True).split())
        if key in ("issue_date", "eff_from"):
            v = iso_date(v)
        if v and v != "Đã biết":
            out[key] = v
    return out


# Kept in step with the server's reader (internal/vbpl/convert.go
# articleNumRe / clauseNumRe / itemNumRe / quoteOpener / quoteCloserRe /
# appendixRe): a paragraph is only labelled Điều/khoản/điểm when the server
# will read the same number from its text, else its id would get no path.
_ARTICLE_RE = re.compile(r"^(?:Điều|ĐIỀU|Ðiều|Điêu|Điền|Ðiêu)\s*(\d+[a-zđ]?)(?=$|[\s.:])")
_ARTICLE_STRICT_RE = re.compile(r"^(?:Điều|ĐIỀU|Ðiều|Điêu|Điền|Ðiêu)\s*\d+[a-zđ]?\s*[.:]")
_CLAUSE_RE = re.compile(r"^(\d{1,3})\s*[.)]\s+\S")
_ITEM_RE = re.compile(r"^([a-zđ]{1,2})\s*\)\s*\S")
_CHAPTER_RE = re.compile(r"^Chương\s+[IVXLCDM\d]+\b", re.I)
_PART_RE = re.compile(r"^Phần\s+(thứ\s+\S+|[IVXLCDM\d]+)\b", re.I)
_SECTION_RE = re.compile(r"^Mục\s+\d+\b", re.I)
_SUBSECTION_RE = re.compile(r"^Tiểu\s+mục\s+\d+\b", re.I)
_APPENDIX_RE = re.compile(r"^(?:PHỤ LỤC|Phụ lục|PHỤ BIỂU|Phụ biểu|DANH MỤC|BIỂU MẪU|MẪU SỐ|Mẫu số|MẪU\s)")
_QUOTE_CLOSER_RE = re.compile(r"[”\"]\s*[.;,:]?\s*[.;]?$")


def _quote_opener(t):
    """A paragraph that starts a quoted span: it opens with “ or " that is
    not closed before the end (“Hóa đơn” là … is a quoted term, not a span)."""
    if not t or t[0] not in "“\"":
        return False
    want = "”" if t[0] == "“" else "\""
    j = t.find(want, 1)
    if j < 0:
        return True
    return t[j + 1:].strip(" .;,:\"”") == ""


def _article_number(text, anchors):
    """The Điều number the server will read, or "" when this is no Điều."""
    m = _ARTICLE_RE.match(text)
    if not m:
        return ""
    # "Điều 5 Luật này …" in a sentence is not a heading: need "Điều 5." /
    # "Điều 5:" or TVPL's own dieu_5 anchor
    if _ARTICLE_STRICT_RE.match(text) or f"dieu_{m.group(1).lower()}" in anchors:
        return m.group(1).lower()
    return ""


_LOWER = {"article": ("clause", "item"), "clause": ("item",), "item": ()}
_SUBJECT_LEVEL = {"điều": "article", "khoản": "clause", "điểm": "item"}


def _subject(note):
    """What a TVPL note is about: "Khoản này được …" → "khoản"."""
    m = re.match(r"\s*(Điều|Khoản|Điểm|Cụm\s+từ|Đoạn|Nội\s+dung)\s+này\b", note, re.I)
    return m.group(1).lower() if m else ""


def _text(el):
    return " ".join(el.get_text(" ", strip=True).replace("\xa0", " ").split())


def _clean(content):
    """Drop TVPL's own additions inside the body."""
    for t in content.find_all(["script", "style", "noscript", "huongdan", "iframe", "input", "button"]):
        t.decompose()
    for t in content.select('[id^="note_"], .dvNoteDieuKhoan, #dvNoteDieuKhoan'):
        t.decompose()


def _blocks(content):
    """Paragraphs and top-level tables of the body, in order."""
    out = []

    def walk(node):
        for c in node.children:
            if not isinstance(c, Tag):
                continue
            if c.name == "table":
                out.append(c)
            elif c.name in ("p", "h1", "h2", "h3", "h4", "h5", "h6"):
                out.append(c)
            elif c.name in ("div", "section", "article", "span", "center", "font", "blockquote"):
                walk(c)
    walk(content)
    return out


def _anchors(p):
    return [a.get("name", "").lower() for a in p.find_all("a") if a.get("name")]


def _bookmarks(p):
    out = []
    for el in [p] + p.find_all(True):
        m = re.search(r"lqhlTootip-(\d+)", el.get("atmm", "") or "")
        if m and m.group(1) not in out:
            out.append(m.group(1))
    return out


class _Path:
    def __init__(self):
        self.dieu = self.khoan = self.diem = None

    def article(self, n):
        self.dieu, self.khoan, self.diem = n, None, None

    def clause(self, n):
        self.khoan, self.diem = n, None

    def item(self, x):
        self.diem = x

    def value(self, level):
        if self.dieu is None:
            return ""
        p = f"dieu:{self.dieu}"
        if level in ("clause", "item") and self.khoan is not None:
            p += f"/khoan:{self.khoan}"
        if level == "item" and self.diem is not None:
            p += f"/diem:{self.diem}"
        return p


def convert(page_html, tips, source_url, source_doc_id=None):
    """(v1 HTML, metadata, report) for one TVPL document page."""
    soup = BeautifulSoup(page_html, "html.parser")
    content = soup.select_one("div.content1")
    if content is None:
        raise ValueError("page has no div.content1")
    props = page_properties(soup)
    law_id = source_doc_id or ""
    if not law_id:
        m = re.search(r"var\s+cLawID\s*=\s*'(\d+)'", page_html)
        law_id = m.group(1) if m else ""
    if not law_id:
        raise ValueError("no TVPL document id (cLawID / URL)")
    _clean(content)

    report = {"articles": 0, "clauses": 0, "items": 0, "marked": 0, "marks": 0, "by_op": {},
              "problems": [], "bookmarks_without_note": 0, "empty_notes": 0,
              "marks_on_parent": 0, "unplaced_marks": 0}
    out = []          # rendered strings, or [attrs, inner, marks] for a paragraph
    last_prov = None  # the entry of the Điều/khoản/điểm the body is inside
    last_at = {}      # level → entry of the current Điều / khoản / điểm
    path = _Path()
    seen_ids = {}
    in_quote = False
    in_appendix = False

    blocks = _blocks(content)
    texts = [_text(b) if b.name != "table" else "" for b in blocks]
    # the appendix can only start after the last Điều (as on the server)
    last_article = max((i for i, (b, t) in enumerate(zip(blocks, texts))
                        if t and _article_number(t, _anchors(b)) and not _quote_opener(t)), default=-1)

    def note_marks(el, where):
        marks = []
        for bm in _bookmarks(el):
            raw = tips.get(bm)
            if raw is None:
                report["bookmarks_without_note"] += 1
                continue
            if not raw.strip():
                report["empty_notes"] += 1  # TVPL answers some bookmarks with nothing
                continue
            ms, probs = parse_tip(raw)
            for pr in probs:
                report["problems"].append({"element": where, "bookmark": bm, "reason": pr})
            marks.extend(ms)
        return marks

    for i, el in enumerate(blocks):
        if el.name == "table":
            marks = note_marks(el, "table")
            if marks:
                report["unplaced_marks"] += len(marks)
                report["problems"].append({"element": "table", "reason": "mark inside a table"})
            out.append(str(_plain_table(el)))
            continue
        text = texts[i]
        if not text:
            continue
        anchors = _anchors(el)
        cls, level = "prov-content", None
        quoted = False

        if in_quote and not text.startswith(("“", "\"")) and _article_number(text, anchors):
            in_quote = False  # a real Điều ends a quote left open (as the server does)
        if not in_quote and _quote_opener(text):
            in_quote = True
        if in_quote:
            # quoted wording (an amending text quoting another): content
            quoted = True
            if _QUOTE_CLOSER_RE.search(text):
                in_quote = False
        elif not in_appendix and i > last_article >= 0 and _APPENDIX_RE.match(text):
            in_appendix = True
            out.append('<div class="doc-region-appendix">')
            last_prov, last_at = None, {}
        elif not in_appendix:
            a_path = next((path_from_anchor(a) for a in anchors if path_from_anchor(a)), "")
            art = _article_number(text, anchors)
            if _PART_RE.match(text):
                cls = "prov-part"
            elif _CHAPTER_RE.match(text) or any(a.startswith("chuong_") for a in anchors):
                cls = "prov-chapter"
            elif _SUBSECTION_RE.match(text):
                cls = "prov-subsection"
            elif _SECTION_RE.match(text) or any(a.startswith("muc_") for a in anchors):
                cls = "prov-section"
            elif art:
                path.article(art)
                cls, level = "prov-article", "article"
            elif path.dieu is not None and _CLAUSE_RE.match(text):
                path.clause(_CLAUSE_RE.match(text).group(1))
                cls, level = "prov-clause", "clause"
            elif path.dieu is not None and _ITEM_RE.match(text):
                path.item(_ITEM_RE.match(text).group(1).lower())
                cls, level = "prov-item", "item"
            elif a_path:
                report["problems"].append({"element": text[:80],
                                           "reason": f"TVPL anchor {a_path} but no number in the text; kept as content"})
            if cls in ("prov-part", "prov-chapter", "prov-section", "prov-subsection"):
                last_prov, last_at = None, {}  # a heading: what follows is not the last Điều's

        attrs = {"class": cls}
        entry = [attrs, _inline(el), []]
        if level:
            report[{"article": "articles", "clause": "clauses", "item": "items"}[level]] += 1
            eid = f"tvpl-{law_id}-{path.value(level)}"
            if eid in seen_ids:
                seen_ids[eid] += 1
                eid = f"{eid}~{seen_ids[eid]}"
            else:
                seen_ids[eid] = 1
            if len(eid) <= ID_MAX:
                attrs["id"] = eid
                last_prov = entry
                last_at[level] = entry
                for lower in _LOWER[level]:
                    last_at.pop(lower, None)
            else:
                report["problems"].append({"element": eid, "reason": f"id longer than {ID_MAX}"})

        marks = note_marks(el, attrs.get("id", text[:60]))
        if marks:
            if "id" in attrs:
                entry[2].extend(marks)
            elif cls == "prov-content" and last_prov is not None and not quoted and not in_appendix:
                # an unnumbered paragraph of a Điều/khoản: the change is to
                # the provision the note names ("Khoản này …"), else to the
                # nearest one
                for mk in marks:
                    lv = _SUBJECT_LEVEL.get(_subject(mk.get("note", "")))
                    (last_at.get(lv) or last_prov)[2].append(mk)
                report["marks_on_parent"] += len(marks)
            else:
                report["unplaced_marks"] += len(marks)
                report["problems"].append({"element": text[:80],
                                           "reason": "mark on a heading, a quote, the appendix or a paragraph outside any Điều/khoản/điểm"})
        out.append(entry)

    if in_appendix:
        out.append("</div>")

    lines = []
    for e in out:
        if isinstance(e, str):
            lines.append(e)
            continue
        attrs, inner, marks = e
        if marks:
            attrs["data-amend"] = json.dumps(marks, ensure_ascii=False)
            report["marked"] += 1
            report["marks"] += len(marks)
            for mk in marks:
                op = mk["op"] + ("?" if mk.get("op_inferred") else "")
                report["by_op"][op] = report["by_op"].get(op, 0) + 1
        a = "".join(f' {k}="{htmlmod.escape(v, quote=True)}"' for k, v in attrs.items())
        lines.append(f"<p{a}>{inner}</p>")

    page_title = page_title_of(soup)
    title = page_title or props.get("doc_number", "")
    doc = ("<!DOCTYPE html>\n<html lang=\"vi\"><head><meta charset=\"utf-8\">"
           f"<title>{htmlmod.escape(title)}</title></head><body>\n" + "\n".join(lines) + "\n</body></html>\n")
    meta = {"source": SOURCE, "source_url": source_url, "source_doc_id": law_id, "format": FORMAT}
    for k in META_KEYS:
        if props.get(k):
            meta[k] = props[k]
    # the server needs the full name to create a catalogue row for a
    # document vbpl.vn does not list yet (and to match it when it does)
    if page_title:
        meta["title"] = page_title
    return doc, meta, report


# The page properties sent as metadata (spec §9.2).
META_KEYS = ("doc_number", "doc_type", "issuer", "issue_date", "eff_from", "eff_status")

# " - Thư Viện Pháp Luật" and the like after the document's name in <title>.
_SITE_SUFFIX_RE = re.compile(r"\s*[-–|]\s*th[ưu]\s*vi[ệe]n\s*ph[áa]p\s*lu[ậa]t.*$", re.I)


def page_title_of(soup):
    """The document's name from the page <title>, the site's suffix cut."""
    raw = soup.title.get_text(strip=True) if soup.title else ""
    return _SITE_SUFFIX_RE.sub("", " ".join(raw.split())).strip()


def _inline(el):
    """The paragraph text, line breaks kept as <br>, every tag dropped."""
    parts = []
    for c in el.descendants:
        if isinstance(c, NavigableString):
            if c.parent is not None and c.parent.name in ("script", "style"):
                continue
            parts.append(htmlmod.escape(str(c).replace("\xa0", " ")))
        elif isinstance(c, Tag) and c.name == "br":
            parts.append("<br>")
    s = "".join(parts)
    s = re.sub(r"[ \t\r\n]+", " ", s).strip()
    return unicodedata.normalize("NFC", s)


def _plain_table(tbl):
    """The table with only rows, cells, spans and text."""
    soup = BeautifulSoup("", "html.parser")
    t = soup.new_tag("table")
    for tr in tbl.find_all("tr"):
        if tr.find_parent("table") is not tbl:
            continue
        r = soup.new_tag("tr")
        for td in tr.find_all(["td", "th"], recursive=False):
            c = soup.new_tag(td.name)
            for k in ("colspan", "rowspan"):
                if td.get(k):
                    c[k] = td[k]
            c.string = _text(td)
            r.append(c)
        t.append(r)
    return t
