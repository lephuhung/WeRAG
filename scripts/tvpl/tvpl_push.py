#!/usr/bin/env python3
"""Look a legal document up on thuvienphapluat.vn (TVPL) and push its
amendment marks to WeRAG (docs/legal-document-feed-spec.md §9.5).

For each document (by số hiệu or TVPL URL) the script

  1. finds the document on TVPL (search by số hiệu, the result whose own
     số hiệu matches),
  2. loads the page and the amendment note of every highlighted provision
     with Lightpanda (tvpl_fetch.py; one page at a time, ≥ 5 s apart; stops
     at a Cloudflare challenge or a login wall),
  3. converts it to "WeRAG legal HTML v1" (tvpl_convert.py) and writes
     <out-dir>/<số hiệu>.html + .json (metadata and report),
  4. POSTs it to /system/legal-documents/push (unless --dry-run). The
     server resolves A and every B in the catalogue and puts each link in
     the SuperAdmin review queue; nothing is used before it is approved.

Some documents show their amendment notes to signed-in members only; then
sign in with TVPL_USER / TVPL_PASS (read from the environment, never
written anywhere). The session cookies are kept in <cache-dir> (0600).

Usage:
  WERAG_API_KEY=sk-... [TVPL_USER=… TVPL_PASS=…] \\
  python3 -I scripts/tvpl/tvpl_push.py --api http://localhost:18080/api/v1 \\
      --lightpanda /path/to/lightpanda --cache-dir ~/data/tvpl-cache \\
      --out-dir ~/data/tvpl-out [--mode links|auto|full] [--dry-run] \\
      [--legal-document-id 148555] 123/2020/NĐ-CP 'https://thuvienphapluat.vn/van-ban/…-470561.aspx' …
  … --list numbers.txt          (one số hiệu or URL per line)

Authentication: WERAG_API_KEY (X-API-Key of a key created in platform
tenant 10000 with the ingest capability) or WERAG_TOKEN (a SuperAdmin JWT).
Needs: Python 3.10+, beautifulsoup4, the Lightpanda binary.
"""

import argparse
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
import uuid

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import tvpl_convert as conv  # noqa: E402
import tvpl_fetch as tf  # noqa: E402
from bs4 import BeautifulSoup  # noqa: E402


def search_results(page_html):
    """(url, title) of each result of a TVPL search page."""
    soup = BeautifulSoup(page_html, "html.parser")
    out = []
    for a in soup.select("p.nqTitle a[href]"):
        href = a["href"].split("?")[0]
        if "/van-ban/" in href and href not in [u for u, _ in out]:
            out.append((href, a.get_text(" ", strip=True)))
    return out


def pick_result(results, number):
    """The result whose own số hiệu is `number` (first number of its slug)."""
    key = conv.number_key(number)
    exact = [(u, t) for u, t in results if conv.number_key(conv.number_from_slug(u)) == key]
    if exact:
        return exact[0][0], ""
    return "", f"no TVPL search result has số hiệu {number} (saw: " + \
        "; ".join(conv.number_from_slug(u) or u[-50:] for u, _ in results[:5]) + ")"


def multipart(fields, file_field, filename, content):
    boundary = uuid.uuid4().hex
    parts = []
    for k, v in fields.items():
        parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{k}"\r\n\r\n{v}\r\n'.encode())
    parts.append(
        f'--{boundary}\r\nContent-Disposition: form-data; name="{file_field}"; '
        f'filename="{filename}"\r\nContent-Type: text/html; charset=utf-8\r\n\r\n'.encode())
    parts.append(content)
    parts.append(b"\r\n")
    parts.append(f"--{boundary}--\r\n".encode())
    return b"".join(parts), f"multipart/form-data; boundary={boundary}"


def push(api, headers, filename, html_bytes, meta, mode):
    body, ctype = multipart({"metadata": json.dumps(meta, ensure_ascii=False), "mode": mode},
                            "file", filename, html_bytes)
    req = urllib.request.Request(api.rstrip("/") + "/system/legal-documents/push", data=body, method="POST",
                                 headers={**headers, "Content-Type": ctype})
    try:
        with urllib.request.urlopen(req, timeout=300) as r:
            return r.status, json.loads(r.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", "replace")
        try:
            return e.code, json.loads(raw)
        except ValueError:
            return e.code, {"raw": raw[:500]}


def safe_name(s):
    return re.sub(r"[^\w.-]+", "_", s.replace("/", "-"))[:120]


def process(item, args, fetcher, headers, log):
    """One document; returns a summary dict."""
    if item.startswith("http"):
        url, number = item.split("?")[0], ""
        host = urllib.parse.urlsplit(url).hostname or ""
        if host not in ("thuvienphapluat.vn", "www.thuvienphapluat.vn") or "/van-ban/" not in url:
            return {"item": item, "status": "error", "reason": "not a thuvienphapluat.vn document URL"}
    else:
        number = item
        url, why = pick_result(search_results(fetcher.search(number, refresh=args.refresh)), number)
        if not url:
            return {"item": item, "status": "not_found", "reason": why}
    final, page, tips = fetcher.document(url, refresh=args.refresh)
    doc, meta, report = conv.convert(page, tips, final, source_doc_id=tf.doc_id_of_url(final) or None)
    if number and meta.get("doc_number") and conv.number_key(meta["doc_number"]) != conv.number_key(number):
        return {"item": item, "status": "mismatch", "url": final,
                "reason": f"TVPL page is {meta['doc_number']}, asked for {number}"}
    if args.legal_document_id:
        meta["legal_document_id"] = args.legal_document_id
    base = safe_name(meta.get("doc_number") or tf.doc_id_of_url(final) or "tvpl")
    os.makedirs(args.out_dir, exist_ok=True)
    html_path = os.path.join(args.out_dir, base + ".html")
    with open(html_path, "w", encoding="utf-8") as f:
        f.write(doc)
    summary = {"item": item, "url": final, "doc_number": meta.get("doc_number"), "file": html_path,
               "report": {k: v for k, v in report.items() if k != "problems"},
               "problems": report["problems"]}
    if report["bookmarks_without_note"]:
        summary["status"] = "incomplete"  # never push part of a document's marks
    elif not report["marks"]:
        summary["status"] = "no_marks"
    elif args.dry_run:
        summary["status"] = "dry_run"
    else:
        code, res = push(args.api, headers, base + ".html", doc.encode("utf-8"), meta, args.mode)
        summary["status"] = "pushed" if code == 200 else f"push_http_{code}"
        summary["push"] = res.get("data", res)
    with open(os.path.join(args.out_dir, base + ".json"), "w", encoding="utf-8") as f:
        json.dump({"metadata": meta, **summary}, f, ensure_ascii=False, indent=1)
    r = summary["report"]
    log(f"  {meta.get('doc_number')}: {r['articles']} Điều, {r['marks']} marks on {r['marked']} provisions "
        f"{r['by_op']}, {len(report['problems'])} problems → {summary['status']}")
    if "push" in summary:
        p = summary["push"]
        log(f"    server: mode={p.get('mode')} links={p.get('links')} pending={p.get('pending')} "
            f"kept={p.get('kept_decisions')} problems={len(p.get('problems') or [])}"
            if isinstance(p, dict) and "links" in p else f"    server: {json.dumps(p, ensure_ascii=False)[:400]}")
    return summary


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("items", nargs="*", help="số hiệu or TVPL document URL")
    ap.add_argument("--list", help="file with one số hiệu or URL per line")
    ap.add_argument("--api", default=os.environ.get("WERAG_API", "http://localhost:18080/api/v1"))
    ap.add_argument("--mode", default="links", choices=["links", "auto", "full"],
                    help="push mode (default links: only the review queue, never a KB)")
    ap.add_argument("--legal-document-id", help="catalogue id of A (only with a single document)")
    ap.add_argument("--lightpanda", help="Lightpanda binary (or LIGHTPANDA_BIN)")
    ap.add_argument("--cache-dir", default=os.environ.get("TVPL_CACHE_DIR") or os.path.expanduser("~/data/tvpl-cache"))
    ap.add_argument("--out-dir", default=os.environ.get("TVPL_OUT_DIR") or os.path.expanduser("~/data/tvpl-out"))
    ap.add_argument("--interval", type=float, default=tf.MIN_INTERVAL, help="seconds between page loads (≥ 5)")
    ap.add_argument("--refresh", action="store_true", help="load document pages again, ignoring the cache")
    ap.add_argument("--login", action="store_true", help="sign in first with TVPL_USER / TVPL_PASS")
    ap.add_argument("--dry-run", action="store_true", help="convert and write files, do not push")
    args = ap.parse_args()

    args.list_items = []
    if args.list:
        with open(args.list, encoding="utf-8") as f:
            args.list_items = [ln.strip() for ln in f if ln.strip() and not ln.startswith("#")]
    items = list(args.items) + args.list_items
    if not items:
        ap.error("give at least one số hiệu / URL or --list")
    if args.legal_document_id and len(items) != 1:
        ap.error("--legal-document-id needs exactly one document")
    os.umask(0o077)  # session cookies and output stay private

    headers = {}
    if not args.dry_run:
        if os.environ.get("WERAG_API_KEY"):
            headers["X-API-Key"] = os.environ["WERAG_API_KEY"]
        elif os.environ.get("WERAG_TOKEN"):
            headers["Authorization"] = "Bearer " + os.environ["WERAG_TOKEN"]
        else:
            ap.error("set WERAG_API_KEY or WERAG_TOKEN (or use --dry-run)")

    log = lambda s: print(s, flush=True)  # noqa: E731
    try:
        fetcher = tf.Fetcher(args.cache_dir, lightpanda=args.lightpanda, interval=args.interval, log=log)
    except tf.TVPLError as e:
        log(f"STOP: {e}")
        return 1
    if args.login:
        try:
            fetcher.login(os.environ.get("TVPL_USER", ""), os.environ.get("TVPL_PASS", ""))
        except tf.TVPLError as e:
            log(f"STOP: {e}")
            return 1
        except tf.TVPLStop as e:
            log(f"STOP: {e}")
            return 1

    results = []
    for it in items:
        log(f"{it}")
        try:
            results.append(process(it, args, fetcher, headers, log))
        except tf.TVPLStop as e:
            log(f"STOP: {e}")
            results.append({"item": it, "status": "stopped", "reason": str(e)})
            break
        except tf.TVPLIncomplete as e:
            log(f"  incomplete: {e}")
            results.append({"item": it, "status": "incomplete", "reason": str(e)})
        except (tf.TVPLError, ValueError) as e:
            log(f"  error: {e}")
            results.append({"item": it, "status": "error", "reason": str(e)})
    counts = {}
    for r in results:
        counts[r["status"]] = counts.get(r["status"], 0) + 1
    log(f"done: {counts}")
    return 0 if all(r["status"] in ("pushed", "dry_run", "no_marks") for r in results) else 1


if __name__ == "__main__":
    sys.exit(main())
