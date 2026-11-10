#!/usr/bin/env python3
"""Fetch the Party documents ("Văn bản của Đảng") of the two dangcongsan portals.

Both portals run the same CMS, so one reader serves both:

* tulieuvankien.dangcongsan.vn/he-thong-van-ban/van-ban-cua-dang  (1998 →, 30 rows a page)
* dangcongsan.org.vn/van-ban-cua-dang                              (12/2024 →, 12 rows a page)

Steps, each cached under --cache-dir so a run resumes where it stopped:

1. listing pages ``?pageNo=N`` until a page has no row;
2. the detail page of every row: the metadata block (Trích yếu, Số hiệu,
   Loại văn bản, Lĩnh vực, Ngày ban hành/hiệu lực, Cơ quan BH, Người ký)
   and the attachments;
3. the attachments (``/upload/...``), unless --no-files; a record's
   ``attachments[].path`` is the cached file.

Output: ``<out>/records.jsonl``, one line per detail page. Requests are ≥
--delay seconds apart (one at a time).
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import asdict, dataclass, field

from bs4 import BeautifulSoup

SITES = {
    "tulieu": ("https://tulieuvankien.dangcongsan.vn", "/he-thong-van-ban/van-ban-cua-dang"),
    "org": ("https://dangcongsan.org.vn", "/van-ban-cua-dang"),
}
UA = "Mozilla/5.0 (X11; Linux x86_64) WeRAG-legal-catalogue/1.0"
MAX_FILE = 60 << 20
META_FIELDS = {
    "Trích yếu": "summary",
    "Số hiệu": "number",
    "Loại văn bản": "doc_type",
    "Lĩnh vực": "field",
    "Ngày ban hành": "issued",
    "Ngày hiệu lực": "effective",
    "Cơ quan BH": "issuer",
    "Người ký": "signer",
}


@dataclass
class Attachment:
    url: str
    name: str
    path: str = ""  # local file, "" when not downloaded
    size: int = 0
    error: str = ""


@dataclass
class Record:
    site: str
    url: str
    source_doc_id: str
    heading: str = ""
    list_title: str = ""
    list_number: str = ""
    list_issued: str = ""
    meta: dict = field(default_factory=dict)
    attachments: list = field(default_factory=list)
    body_text: str = ""  # text of an article body on the detail page, if any
    error: str = ""


class Fetcher:
    def __init__(self, cache: str, delay: float, refresh_lists: bool):
        self.cache, self.delay, self.refresh_lists = cache, delay, refresh_lists
        self._last = 0.0

    def _wait(self):
        gap = time.monotonic() - self._last
        if gap < self.delay:
            time.sleep(self.delay - gap)
        self._last = time.monotonic()

    def path_for(self, url: str, kind: str) -> str:
        key = hashlib.sha1(url.encode()).hexdigest()
        return os.path.join(self.cache, kind, key[:2], key)

    def get(self, url: str, kind: str, refresh: bool = False, limit: int = MAX_FILE) -> bytes:
        path = self.path_for(url, kind)
        if not refresh and os.path.exists(path):
            with open(path, "rb") as f:
                return f.read()
        data = b""
        for attempt in range(4):
            self._wait()
            try:
                req = urllib.request.Request(quote_url(url), headers={"User-Agent": UA})
                with urllib.request.urlopen(req, timeout=90) as r:
                    data = r.read(limit + 1)
                break
            except urllib.error.HTTPError as e:
                if e.code in (404, 410):
                    raise
                err = e
            except (urllib.error.URLError, TimeoutError, ConnectionError) as e:
                err = e
            time.sleep(5 * (attempt + 1))
        else:
            raise RuntimeError(f"{url}: {err}")
        if len(data) > limit:
            raise RuntimeError(f"{url}: larger than {limit >> 20} MB")
        os.makedirs(os.path.dirname(path), exist_ok=True)
        tmp = path + ".part"
        with open(tmp, "wb") as f:
            f.write(data)
        os.replace(tmp, path)
        return data


def clean(s: str) -> str:
    return re.sub(r"\s+", " ", s or "").strip()


def parse_listing(html: str) -> list[dict]:
    s = BeautifulSoup(html, "lxml")
    table = s.select_one(".list-legal-document-table table")
    if table is None or table.find("tbody") is None:
        return []
    rows = []
    for tr in table.find("tbody").find_all("tr", recursive=False):
        tds = tr.find_all("td", recursive=False)
        if len(tds) < 3:
            continue
        a = tds[1].find("a", href=True)
        if a is None:
            continue
        rows.append({
            "number": clean(tds[0].get_text(" ")),
            "href": a["href"],
            "title": clean(a.get_text(" ")),
            "issued": clean(tds[2].get_text()),
        })
    return rows


def parse_detail(html: str) -> tuple[str, dict, list[tuple[str, str]], str]:
    """heading, metadata, attachments (href, label), article body text."""
    s = BeautifulSoup(html, "lxml")
    box = s.select_one(".cont-document")
    meta: dict[str, str] = {}
    files: list[tuple[str, str]] = []
    heading = ""
    body = ""
    if box is not None:
        for row in box.select(".layout-info"):
            left = row.select_one(".col-left")
            right = row.select_one(".flex-1")
            if left is None or right is None:
                continue
            label = clean(left.get_text()).rstrip(":").strip()
            if label == "File đính kèm":
                for a in right.find_all("a", href=True):
                    files.append((a["href"], clean(a.get("title") or a.get_text())))
                continue
            key = META_FIELDS.get(label)
            if key:
                meta[key] = clean(right.get_text(" "))
        h = box.find_previous(["h1", "h2"])
        if h is not None:
            heading = clean(h.get_text(" "))
        # a few pages carry the text itself after the metadata block
        for cand in box.find_all_next("div", class_=re.compile(r"(article-content|content-detail|detail-content|news-content)")):
            t = clean(cand.get_text(" "))
            if len(t) > len(body):
                body = t
    return heading, meta, files, body


def source_doc_id(site: str, path: str) -> str:
    """Stable id of a detail page: the CMS id at the end of the slug when
    there is one (tulieu "...-192"), else a hash of the path."""
    path = urllib.parse.urlsplit(path).path
    # old pages end in the CMS id ("…-192"); new ones in ".html", where a
    # trailing number is part of the title ("…-nam-2026.html"), not an id
    m = re.search(r"-(\d+)$", path)
    if m and site == "tulieu":
        return f"tulieu-{m.group(1)}"
    return f"{site}-" + hashlib.sha1(path.encode()).hexdigest()[:16]


def quote_url(url: str) -> str:
    """Percent-encode a link's path and query as a browser would: file names
    carry Vietnamese letters ("QĐ_157.pdf")."""
    parts = urllib.parse.urlsplit(url)
    path = urllib.parse.quote(urllib.parse.unquote(parts.path), safe="/%")
    query = urllib.parse.quote(urllib.parse.unquote(parts.query), safe="=&%")
    return urllib.parse.urlunsplit((parts.scheme, parts.netloc, path, query, ""))


def file_name(url: str) -> str:
    name = urllib.parse.unquote(urllib.parse.urlsplit(url).path.rsplit("/", 1)[-1])
    return re.sub(r"[^\w.\-]+", "_", name)[-150:] or "file"


def crawl(site: str, fx: Fetcher, with_files: bool, max_pages: int, log) -> list[Record]:
    base, path = SITES[site]
    out: list[Record] = []
    seen: set[str] = set()
    page = 1
    while page <= max_pages:
        html = fx.get(f"{base}{path}?pageNo={page}", f"list-{site}", refresh=fx.refresh_lists).decode("utf-8", "replace")
        rows = parse_listing(html)
        if not rows:
            break
        log(f"{site} page {page}: {len(rows)} rows")
        for row in rows:
            href = urllib.parse.urljoin(base + "/", row["href"])
            clean_url = href.split("?")[0]
            if clean_url in seen:
                continue
            seen.add(clean_url)
            rec = Record(site=site, url=clean_url, source_doc_id=source_doc_id(site, clean_url),
                         list_title=row["title"], list_number=row["number"], list_issued=row["issued"])
            try:
                detail = fx.get(href, f"detail-{site}").decode("utf-8", "replace")
                rec.heading, rec.meta, links, rec.body_text = parse_detail(detail)
            except Exception as e:  # noqa: BLE001 - one bad page must not stop the crawl
                rec.error = f"detail: {e}"
                links = []
            for href2, _label in links:
                furl = urllib.parse.urljoin(base + "/", href2)
                att = Attachment(url=furl, name=file_name(furl))
                if with_files:
                    try:
                        att.size = len(fx.get(furl, "files"))
                        att.path = fx.path_for(furl, "files")
                    except Exception as e:  # noqa: BLE001
                        att.error = str(e)[:300]
                rec.attachments.append(asdict(att))
            out.append(rec)
        page += 1
    return out


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--site", choices=[*SITES, "all"], default="all")
    ap.add_argument("--cache-dir", required=True)
    ap.add_argument("--out-dir", required=True)
    ap.add_argument("--delay", type=float, default=1.0, help="seconds between requests (default 1)")
    ap.add_argument("--max-pages", type=int, default=500)
    ap.add_argument("--no-files", action="store_true", help="metadata only")
    ap.add_argument("--refresh-lists", action="store_true", help="reload listing pages (new documents)")
    a = ap.parse_args(argv)
    os.makedirs(a.out_dir, exist_ok=True)
    fx = Fetcher(a.cache_dir, a.delay, a.refresh_lists)
    log = lambda m: print(m, file=sys.stderr, flush=True)  # noqa: E731
    sites = list(SITES) if a.site == "all" else [a.site]
    n = 0
    tmp = os.path.join(a.out_dir, "records.jsonl.part")
    with open(tmp, "w", encoding="utf-8") as f:
        for site in sites:
            for rec in crawl(site, fx, not a.no_files, a.max_pages, log):
                f.write(json.dumps(asdict(rec), ensure_ascii=False) + "\n")
                n += 1
    os.replace(tmp, os.path.join(a.out_dir, "records.jsonl"))
    log(f"{n} records → {a.out_dir}/records.jsonl")
    return 0


if __name__ == "__main__":
    sys.exit(main())
