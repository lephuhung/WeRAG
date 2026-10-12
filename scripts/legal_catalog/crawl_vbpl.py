#!/usr/bin/env python3
"""Crawl vbpl.vn (central documents) into the WeRAG legal catalogue format.

Keeps the catalogue fresh without waiting for a new Hugging Face snapshot of
th1nhng0/vietnamese-legal-documents. Sources, all public (checked 2026-10-12):

  list       Next.js server action on https://vbpl.vn/van-ban/trung-uong
             ({total, items[]}, 500 items per page)
  detail     GET https://vbpl-bientap-gateway.moj.gov.vn/api/qtdc/public/doc/{id}
             (metadata + full text HTML)
  relations  the "diagram" server action: documentNamesByType = this
             document acts on others, documentNamesBySource = others act on it.
             The detail's "references" field the HF crawler read is now null.

Document ids are the ones of the HF dataset and of legal_documents, so the
output imports over the existing catalogue.

Every run lists the whole central catalogue, then fetches detail, full text
and relations only for documents that are new or whose list row changed
(status, dates, updatedDate). State lives in <out-dir>/state.sqlite and is
committed per document, so an interrupted run resumes where it stopped.
After crawling, the full set is exported:

  documents.jsonl  one row per kept document, HF metadata field names
                   (read by cmd/legal-catalog-import), then every other
                   central document with its list metadata only
                   ("metadata_only": true; translations left out)
  relations.jsonl  raw portal relations {doc_id, other_doc_id, relationship},
                   doc_id acting on other_doc_id with the portal label
  html/<id>.html   full text as published on vbpl.vn, except for documents
                   "Hết hiệu lực toàn bộ" (metadata only)

Kept documents follow scripts/legal_catalog/prepare_vbpl.py: central
issuers, the NĐ30 registry types, issued on or after --min-year.

Usage:
  python3 -I scripts/legal_catalog/crawl_vbpl.py <out-dir> [--limit N]
  go run ./cmd/legal-catalog-import -documents <out-dir>/documents.jsonl \\
      -relations <out-dir>/relations.jsonl -snapshot YYYY-MM-DD
"""

import argparse
import datetime as dt
import json
import os
import re
import sqlite3
import sys
import time
import urllib.error
import urllib.request

SITE = "https://vbpl.vn"
LIST_PAGE = SITE + "/van-ban/trung-uong"
DETAIL_URL = "https://vbpl-bientap-gateway.moj.gov.vn/api/qtdc/public/doc/{}"
# The portal's WAF answers 403 to non-browser user agents.
USER_AGENT = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"
# 403/429 mean "too fast": back off for long. Some documents answer 500 every
# time, so server errors get only a few short retries before being skipped.
THROTTLE_STATUS = {403, 429}
SERVER_ERROR_STATUS = {500, 502, 503, 504}
SERVER_ERROR_RETRIES = 2

# Fully expired documents keep metadata only (decided 2026-10-12): the
# catalogue must still answer "this document is no longer in force", but its
# text must not be served as current law. Partial expiry, suspension and
# "Không còn phù hợp" keep the full text.
EXPIRED_STATUS = "Hết hiệu lực toàn bộ"

# Relation codes of the portal front-end (enum in its JS bundle).
RELATION_LABELS = {
    1: "Bãi bỏ", 2: "Bản dịch", 3: "Căn cứ", 4: "Dẫn chiếu",
    5: "Đình chỉ thi hành", 6: "Đính chính", 7: "Hợp nhất",
    8: "Hướng dẫn áp dụng", 9: "Quy định chi tiết, hướng dẫn thi hành",
    10: "Sửa đổi, bổ sung", 11: "Tạm ngưng hiệu lực", 12: "Thay thế",
    13: "Bổ sung", 14: "Giải thích", 15: "Công bố", 16: "Sửa đổi",
    17: "Gia hạn hiệu lực", 18: "Tiếp tục còn hiệu lực",
}

# Same selection as prepare_vbpl.py (decided 2026-10-09).
ND30_TYPES = {
    "Nghị quyết", "Quyết định", "Chỉ thị", "Quy chế", "Quy định", "Thông cáo",
    "Thông báo", "Hướng dẫn", "Chương trình", "Kế hoạch", "Phương án", "Đề án",
    "Dự án", "Báo cáo", "Biên bản", "Tờ trình", "Hợp đồng", "Công văn",
    "Công điện", "Bản ghi nhớ", "Bản thỏa thuận", "Giấy ủy quyền", "Giấy mời",
    "Giấy giới thiệu", "Giấy nghỉ phép", "Phiếu gửi", "Phiếu chuyển", "Phiếu báo",
    "Thư công",
}
QPPL_TYPES = {
    "Hiến pháp", "Bộ luật", "Luật", "Pháp lệnh", "Nghị định", "Thông tư",
    "Thông tư liên tịch",
}
# English translations duplicate documents the list already holds.
STUB_SKIP_TYPES = {"Bản dịch văn bản"}
LOCAL_ISSUER = re.compile(
    r"UBND|HĐND|Ủy ban nhân dân|Uỷ ban nhân dân|Hội đồng nhân dân|tỉnh|Tỉnh|thành phố|Thành phố|TP\.")

SCHEMA = """
CREATE TABLE IF NOT EXISTS docs (
    id         TEXT PRIMARY KEY,
    list_sig   TEXT NOT NULL,
    list_item  TEXT NOT NULL,
    kept       INTEGER NOT NULL,
    fetched_sig TEXT,
    detail     TEXT,
    diagram    TEXT,
    fetched_at TEXT,
    error      TEXT
);
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT);
"""


class Client:
    """Sequential HTTP client with a fixed pause and back-off on throttling."""

    def __init__(self, delay, retries):
        self.delay = delay
        self.retries = retries
        self.last = 0.0
        self.requests = 0

    def fetch(self, url, data=None, headers=None):
        hdrs = {"User-Agent": USER_AGENT, "Referer": SITE + "/"}
        hdrs.update(headers or {})
        for attempt in range(self.retries + 1):
            wait = self.last + self.delay - time.monotonic()
            if wait > 0:
                time.sleep(wait)
            self.last = time.monotonic()
            self.requests += 1
            req = urllib.request.Request(url, data=data, headers=hdrs, method="POST" if data else "GET")
            try:
                with urllib.request.urlopen(req, timeout=120) as resp:
                    return resp.read().decode("utf-8", errors="replace")
            except urllib.error.HTTPError as e:
                if e.code in SERVER_ERROR_STATUS and attempt < SERVER_ERROR_RETRIES:
                    reason, pause = f"HTTP {e.code}", 5 * (attempt + 1)
                elif e.code in THROTTLE_STATUS and attempt < self.retries:
                    reason, pause = f"HTTP {e.code}", min(30 * 2 ** attempt, 600)
                else:
                    raise
            except (urllib.error.URLError, TimeoutError, ConnectionError) as e:
                if attempt == self.retries:
                    raise
                reason, pause = str(e), min(30 * 2 ** attempt, 600)
            log(f"  {reason} on {url[:90]}, retry in {pause}s")
            time.sleep(pause)
        raise RuntimeError("unreachable")

    def action(self, action_id, args):
        body = json.dumps(args, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        return self.fetch(LIST_PAGE, data=body, headers={
            "Next-Action": action_id,
            "Accept": "text/x-component",
            "Content-Type": "text/plain;charset=UTF-8",
            "Origin": SITE,
        })


def log(msg):
    print(f"{dt.datetime.now():%H:%M:%S} {msg}", flush=True)


def rsc_object(text, key):
    """Return the first JSON object holding key in a React Server Components
    response. Large rows are length-prefixed, so objects are not line-aligned."""
    dec = json.JSONDecoder()
    for m in re.finditer(r"[0-9a-f]+:(?=\{)", text):
        try:
            obj, _ = dec.raw_decode(text, m.end())
        except json.JSONDecodeError:
            continue
        if isinstance(obj, dict) and key in obj:
            return obj
    return None


def discover_actions(client):
    """Find the list and diagram server-action ids in the site's JS bundle.

    The ids change with every portal deployment. They are resolved by code
    structure — the react-query hooks with queryKey ["documents","all"] and
    ["documents","diagram"] call an export of the documents service module,
    whose exports are server references — so no unknown action is ever called
    (the bundle also holds actions that write, e.g. feedback)."""
    html = client.fetch(LIST_PAGE)
    chunks = [client.fetch(SITE + p) for p in sorted(set(re.findall(r'/_next/static/chunks/[^"\']+\.js', html)))]
    found = {}
    for js in chunks:
        for name, key in (("list", "all"), ("diagram", "diagram")):
            m = re.search(r'queryKey:\["documents","%s",\w+\],queryFn:.{0,200}?\(0,(\w+)\.(\w+)\)' % key, js)
            if name in found or not m:
                continue
            var, export = m.groups()
            # The import nearest before the hook belongs to the same module.
            imports = re.findall(r"\b%s=\w+\((\d+)\)" % re.escape(var), js[:m.start()])
            if not imports:
                continue
            for other in chunks:
                start = other.find("%s:function(e,t,n)" % imports[-1])
                if start < 0:
                    continue
                body = other[start:start + 4000]
                local = re.search(r"\b%s:function\(\)\{return (\w+)\}" % re.escape(export), body)
                ref = local and re.search(
                    r'\b%s=\(0,\w+\.\$\)\("([0-9a-f]{40,42})"\)' % re.escape(local.group(1)), body)
                if ref:
                    found[name] = ref.group(1)
                    break
    if len(found) == 2:
        return found["list"], found["diagram"]
    raise SystemExit(f"could not find the server actions in the vbpl.vn bundle (found {found}); "
                     "pass --list-action / --diagram-action")


def vn_date(value):
    if not value:
        return ""
    try:
        return dt.datetime.fromisoformat(value.replace("Z", "+00:00")).strftime("%d/%m/%Y")
    except ValueError:
        return value


def join_unique(values):
    return ", ".join(dict.fromkeys(v.strip() for v in values if v and v.strip()))


def list_signature(item):
    """The list-row fields whose change means the document must be refetched."""
    return json.dumps([
        item.get("docNum"), item.get("title"), (item.get("effStatus") or {}).get("name"),
        item.get("issueDate"), item.get("effFrom"), item.get("effTo"), item.get("updatedDate"),
    ], ensure_ascii=False)


def is_kept(item, min_year, all_central):
    if all_central:
        return True
    if (item.get("docType") or {}).get("name") not in ND30_TYPES | QPPL_TYPES:
        return False
    if LOCAL_ISSUER.search(item.get("agencyName") or "") or item.get("administrativeUnitIds"):
        return False
    return issued_since(item, min_year, unknown=True)


def issued_since(doc, min_year, unknown):
    """A few list rows carry no issueDate while the detail has it, so the list
    keeps them (unknown=True) and the export decides on the detail."""
    try:
        return int((doc.get("issueDate") or "")[:4]) >= min_year
    except ValueError:
        return unknown


def crawl_list(client, db, action, args):
    page, size, seen = 1, args.page_size, 0
    while True:
        text = client.action(action, [{
            "pageNumber": page, "pageSize": size, "sortBy": "issueDate",
            "sortDirection": "desc", "agencyLevel": "TRUNG_UONG",
        }])
        res = rsc_object(text, "items")
        if res is None:
            raise SystemExit(f"list page {page}: unexpected response (action id changed?): {text[:200]!r}")
        items = res.get("items") or []
        with db:
            for item in items:
                if not item.get("id"):
                    continue
                db.execute(
                    "INSERT INTO docs (id, list_sig, list_item, kept) VALUES (?,?,?,?) "
                    "ON CONFLICT(id) DO UPDATE SET list_sig=excluded.list_sig, "
                    "list_item=excluded.list_item, kept=excluded.kept",
                    (str(item["id"]), list_signature(item), json.dumps(item, ensure_ascii=False),
                     int(is_kept(item, args.min_year, args.all_central))))
        seen += len(items)
        total = int(res.get("total") or 0)
        if page == 1 or page % 20 == 0:
            log(f"list page {page}: {seen}/{total}")
        if not items or seen >= total:
            log(f"listed {seen} of {total} central documents")
            return
        page += 1


def crawl_documents(client, db, action, args):
    where = "kept=1" if args.refetch_all else \
        "kept=1 AND (fetched_sig IS NULL OR fetched_sig <> list_sig)"
    todo = [r[0] for r in db.execute(f"SELECT id FROM docs WHERE {where} ORDER BY id")]
    if args.limit:
        todo = todo[:args.limit]
    log(f"{len(todo)} documents to fetch")
    html_dir = os.path.join(args.out_dir, "html")
    os.makedirs(html_dir, exist_ok=True)
    started = time.monotonic()
    for n, doc_id in enumerate(todo, 1):
        sig = db.execute("SELECT list_sig FROM docs WHERE id=?", (doc_id,)).fetchone()[0]
        try:
            payload = json.loads(client.fetch(DETAIL_URL.format(doc_id), headers={"Accept": "application/json"}))
            detail = payload.get("data") if payload.get("success") is True else None
            if not isinstance(detail, dict):
                raise ValueError(f"detail: {payload.get('message') or payload.get('messageCode')}")
            diagram = rsc_object(client.action(action, [doc_id]), "documentNamesByType")
            if diagram is None:
                raise ValueError("diagram: unexpected response")
        except (urllib.error.URLError, ValueError, TimeoutError, ConnectionError) as e:
            with db:
                db.execute("UPDATE docs SET error=? WHERE id=?", (str(e)[:500], doc_id))
            log(f"  {doc_id}: {e}")
            continue
        content = (detail.get("documentContent") or {}).get("content") or ""
        if (detail.get("effStatus") or {}).get("name", "").strip() == EXPIRED_STATUS:
            content = ""
        path = os.path.join(html_dir, f"{doc_id}.html")
        if content:
            with open(path + ".tmp", "w", encoding="utf-8") as f:
                f.write(content)
            os.replace(path + ".tmp", path)
        elif os.path.exists(path):
            os.remove(path)
        # The full text lives in html/; keep the rest of the payload.
        detail.pop("documentContent", None)
        detail.pop("documentContentEn", None)
        detail["_has_html"] = bool(content)
        with db:
            db.execute(
                "UPDATE docs SET detail=?, diagram=?, fetched_sig=?, fetched_at=?, error=NULL WHERE id=?",
                (json.dumps(detail, ensure_ascii=False), json.dumps(diagram, ensure_ascii=False),
                 sig, dt.datetime.now(dt.timezone.utc).isoformat(timespec="seconds"), doc_id))
        if n % 100 == 0 or n == len(todo):
            rate = n / max(time.monotonic() - started, 1e-9)
            log(f"fetched {n}/{len(todo)} ({rate:.2f} doc/s, ~{(len(todo) - n) / max(rate, 1e-9) / 3600:.1f} h left)")


def document_row(item, detail):
    issues = detail.get("documentIssues") or []
    return {
        "id": str(detail.get("id") or item["id"]),
        "title": detail.get("title") or item.get("title") or "",
        "so_ky_hieu": detail.get("docNum") or item.get("docNum") or "",
        "ngay_ban_hanh": vn_date(detail.get("issueDate")),
        "loai_van_ban": (detail.get("docType") or {}).get("name") or "",
        "ngay_co_hieu_luc": vn_date(detail.get("effFrom")),
        "ngay_het_hieu_luc": vn_date(detail.get("effTo")),
        "ngay_dang_cong_bao": vn_date(detail.get("publicDate")),
        "nganh": join_unique(m.get("name") for m in detail.get("documentMajors") or []),
        "linh_vuc": join_unique(f.get("name") for f in detail.get("documentFields") or []),
        "co_quan_ban_hanh": join_unique(i.get("agencyName") for i in issues) or detail.get("agencyName") or "",
        "chuc_danh": join_unique(i.get("jobTitleName") for i in issues),
        "nguoi_ky": join_unique(i.get("personName") for i in issues),
        "pham_vi": "Trung ương" if detail.get("isLw") is not False else "Địa phương",
        "tinh_trang_hieu_luc": (detail.get("effStatus") or {}).get("name") or "",
        "url": f"{SITE}/van-ban/chi-tiet/{item['id']}",
        "has_html": bool(detail.get("_has_html")),
        "updated_date": detail.get("updatedDate") or item.get("updatedDate") or "",
    }


def list_row(item):
    """A document known from the catalogue list only: no signer, field or
    full text."""
    return {
        "id": str(item["id"]),
        "title": item.get("title") or "",
        "so_ky_hieu": item.get("docNum") or "",
        "ngay_ban_hanh": vn_date(item.get("issueDate")),
        "loai_van_ban": (item.get("docType") or {}).get("name") or "",
        "ngay_co_hieu_luc": vn_date(item.get("effFrom")),
        "ngay_het_hieu_luc": vn_date(item.get("effTo")),
        "ngay_dang_cong_bao": vn_date(item.get("publicDate")),
        "co_quan_ban_hanh": item.get("agencyName") or "",
        "pham_vi": "Trung ương",
        "tinh_trang_hieu_luc": (item.get("effStatus") or {}).get("name") or "",
        "url": f"{SITE}/van-ban/chi-tiet/{item['id']}",
        "has_html": False,
        "metadata_only": True,
        "updated_date": item.get("updatedDate") or "",
    }


def relation_rows(doc_id, diagram):
    """documentNamesByType: doc_id acts on the listed documents;
    documentNamesBySource: the listed documents act on doc_id."""
    for side, outgoing in (("documentNamesByType", True), ("documentNamesBySource", False)):
        for code, docs in (diagram.get(side) or {}).items():
            try:
                label = RELATION_LABELS.get(int(code), f"Loại quan hệ {code}")
            except ValueError:
                label = f"Loại quan hệ {code}"
            for other in docs or []:
                other_id = str(other.get("id") or "")
                if not other_id or other_id == doc_id:
                    continue
                if outgoing:
                    yield doc_id, other_id, label
                else:
                    yield other_id, doc_id, label


def export(db, args):
    out_dir = args.out_dir
    docs = rels = 0
    seen, exported = set(), set()
    with open(os.path.join(out_dir, "documents.jsonl.tmp"), "w", encoding="utf-8") as fd, \
            open(os.path.join(out_dir, "relations.jsonl.tmp"), "w", encoding="utf-8") as fr:
        for doc_id, item, detail, diagram in db.execute(
                "SELECT id, list_item, detail, diagram FROM docs WHERE kept=1 AND detail IS NOT NULL ORDER BY id"):
            detail = json.loads(detail)
            if not args.all_central and not issued_since(detail, args.min_year, unknown=False):
                continue
            fd.write(json.dumps(document_row(json.loads(item), detail), ensure_ascii=False) + "\n")
            exported.add(doc_id)
            docs += 1
            for row in relation_rows(doc_id, json.loads(diagram)):
                if row in seen:
                    continue
                seen.add(row)
                fr.write(json.dumps({"doc_id": row[0], "other_doc_id": row[1], "relationship": row[2]},
                                    ensure_ascii=False) + "\n")
                rels += 1
        # Every other central document goes in with its list metadata only,
        # so the catalogue still knows it (an old law someone asks about,
        # the target of a "căn cứ" or "hướng dẫn" edge). Translations are
        # copies of documents already listed.
        stubs = 0
        if not args.no_stubs:
            for doc_id, item in db.execute("SELECT id, list_item FROM docs ORDER BY id"):
                item = json.loads(item)
                if doc_id in exported or (item.get("docType") or {}).get("name") in STUB_SKIP_TYPES:
                    continue
                fd.write(json.dumps(list_row(item), ensure_ascii=False) + "\n")
                stubs += 1
    for name in ("documents.jsonl", "relations.jsonl"):
        os.replace(os.path.join(out_dir, name + ".tmp"), os.path.join(out_dir, name))
    log(f"exported {docs} documents, {stubs} metadata-only documents and {rels} relations to {out_dir}")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("out_dir")
    ap.add_argument("--min-year", type=int, default=2010)
    ap.add_argument("--all-central", action="store_true", help="keep every central document, no type/year filter")
    ap.add_argument("--delay", type=float, default=0.5, help="seconds between requests (default 0.5)")
    ap.add_argument("--retries", type=int, default=6)
    ap.add_argument("--page-size", type=int, default=500)
    ap.add_argument("--limit", type=int, default=0, help="fetch at most N documents this run")
    ap.add_argument("--refetch-all", action="store_true", help="refetch every kept document, changed or not")
    ap.add_argument("--skip-list", action="store_true", help="reuse the stored list, only fetch and export")
    ap.add_argument("--export-only", action="store_true")
    ap.add_argument("--no-stubs", action="store_true",
                    help="export only the kept documents, not the list metadata of the other central ones")
    ap.add_argument("--list-action", help="server action id of the catalogue search")
    ap.add_argument("--diagram-action", help="server action id of the relation diagram")
    args = ap.parse_args()

    os.makedirs(args.out_dir, exist_ok=True)
    db = sqlite3.connect(os.path.join(args.out_dir, "state.sqlite"))
    db.executescript(SCHEMA)
    if not args.export_only:
        client = Client(args.delay, args.retries)
        list_action, diagram_action = args.list_action, args.diagram_action
        if not (list_action and diagram_action):
            found = discover_actions(client)
            list_action = list_action or found[0]
            diagram_action = diagram_action or found[1]
        log(f"actions: list {list_action}, diagram {diagram_action}")
        if not args.skip_list:
            crawl_list(client, db, list_action, args)
            # Filters may have changed since the rows were stored.
            with db:
                for doc_id, item in db.execute("SELECT id, list_item FROM docs").fetchall():
                    db.execute("UPDATE docs SET kept=? WHERE id=?",
                               (int(is_kept(json.loads(item), args.min_year, args.all_central)), doc_id))
        crawl_documents(client, db, diagram_action, args)
        log(f"{client.requests} requests")
    export(db, args)
    kept, done, failed = db.execute(
        "SELECT SUM(kept), SUM(kept AND fetched_sig = list_sig), SUM(kept AND error IS NOT NULL) FROM docs").fetchone()
    log(f"kept {kept or 0}, up to date {done or 0}, last fetch failed {failed or 0}")


if __name__ == "__main__":
    sys.exit(main())
