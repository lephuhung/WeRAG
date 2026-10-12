#!/usr/bin/env python3
"""Upload crawled vbpl.vn documents into a WeRAG knowledge base.

Reads the crawl directory of crawl_vbpl.py (state.sqlite + html/<id>.html)
and uploads each page's original HTML as a file. The backend recognises the
vbpl.vn provision markup (internal/vbpl) and chunks one Điều per chunk with
Điều/Khoản/Điểm metadata and provision ids, so the HTML is uploaded as is.

Every upload carries metadata used elsewhere:
  external_id   the vbpl.vn id = legal_documents.id (legal review links the
                knowledge to its catalogue row from it)
  source        "vbpl"
  doc_number, doc_type, issue_date, eff_from, eff_status, vbpl_url
  vbpl_appendix_files  JSON list of the attached phụ lục files

Uploaded ids are remembered per knowledge base in <crawl-dir>/ingest.sqlite,
with the HTML hash, so a rerun only uploads new or changed pages (a changed
page is uploaded again; delete the stale knowledge by hand for now).

Usage:
  WERAG_API_KEY=sk-... python3 -I scripts/legal_catalog/ingest_vbpl.py \\
      --api http://localhost:18080/api/v1 --kb <kb-id> \\
      [--crawl-dir /home/orca/data/vbpl] [--ids ids.txt] [--doc-type "Luật"] \\
      [--eff-status "Còn hiệu lực"] [--limit N] [--dry-run]

Authentication: WERAG_API_KEY (X-API-Key) or WERAG_TOKEN (Bearer).
"""

import argparse
import datetime as dt
import hashlib
import json
import os
import re
import sqlite3
import sys
import time
import urllib.error
import urllib.request
import uuid

VBPL_DOC_URL = "https://vbpl.vn/van-ban/chi-tiet/{}"
# Pages below this size are the crawler's empty placeholders.
MIN_HTML_BYTES = 200


def name_of(v):
    return v.get("name") if isinstance(v, dict) else v


def file_name(detail):
    num = (detail.get("docNum") or "").strip()
    title = " ".join((detail.get("title") or "").split())
    name = f"{num} - {title}" if num and num not in title else (title or num or detail["id"])
    name = name.replace("/", "-")  # số hiệu 112/2025/QH15 → 112-2025-QH15
    name = re.sub(r'[\\:*?"<>|]+', " ", name)
    name = " ".join(name.split())[:180]
    return name + ".html"


def metadata(detail):
    meta = {
        "external_id": str(detail["id"]),
        "source": "vbpl",
        "doc_number": detail.get("docNum") or "",
        "doc_type": name_of(detail.get("docType")) or "",
        "issue_date": (detail.get("issueDate") or "")[:10],
        "eff_from": (detail.get("effFrom") or "")[:10],
        "eff_status": name_of(detail.get("effStatus")) or "",
        "vbpl_url": VBPL_DOC_URL.format(detail["id"]),
    }
    meta = {k: v for k, v in meta.items() if v}
    # Phụ lục published only as files: chunks citing them link the file
    # (upload metadata values are strings, so the list is JSON-encoded).
    files = [
        {"title": " ".join((r.get("fileTitle") or "").split()), "file_name": r["fileName"]}
        for r in detail.get("documentRelatedList") or []
        if r.get("relatedType") == "APPENDIX" and r.get("fileName")
    ]
    if files:
        meta["vbpl_appendix_files"] = json.dumps(files, ensure_ascii=False)
    return meta


def multipart(fields, file_field, filename, content):
    boundary = uuid.uuid4().hex
    parts = []
    for k, v in fields.items():
        parts.append(
            f'--{boundary}\r\nContent-Disposition: form-data; name="{k}"\r\n\r\n{v}\r\n'.encode()
        )
    parts.append(
        (
            f'--{boundary}\r\nContent-Disposition: form-data; name="{file_field}"; '
            f'filename="{filename}"\r\nContent-Type: text/html\r\n\r\n'
        ).encode()
        + content
        + b"\r\n"
    )
    parts.append(f"--{boundary}--\r\n".encode())
    return b"".join(parts), f"multipart/form-data; boundary={boundary}"


def upload(api, kb, headers, name, meta, content):
    body, ctype = multipart(
        {"fileName": name, "metadata": json.dumps(meta, ensure_ascii=False)},
        "file",
        name,
        content,
    )
    req = urllib.request.Request(
        f"{api.rstrip('/')}/knowledge-bases/{kb}/knowledge/file",
        data=body,
        method="POST",
        headers={**headers, "Content-Type": ctype},
    )
    with urllib.request.urlopen(req, timeout=300) as resp:
        return json.loads(resp.read().decode() or "{}")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--api", required=True, help="WeRAG API base, e.g. http://localhost:18080/api/v1")
    ap.add_argument("--kb", required=True, help="target knowledge base id")
    ap.add_argument("--crawl-dir", default="/home/orca/data/vbpl")
    ap.add_argument("--ids", help="file with one vbpl id per line (default: every crawled page)")
    ap.add_argument("--doc-type", action="append", help="keep only this docType name (repeatable)")
    ap.add_argument("--eff-status", action="append", help="keep only this effStatus name (repeatable)")
    ap.add_argument("--limit", type=int, default=0)
    ap.add_argument("--sleep", type=float, default=0.5, help="seconds between uploads")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    headers = {}
    if os.environ.get("WERAG_API_KEY"):
        headers["X-API-Key"] = os.environ["WERAG_API_KEY"]
    elif os.environ.get("WERAG_TOKEN"):
        headers["Authorization"] = "Bearer " + os.environ["WERAG_TOKEN"]
    elif not args.dry_run:
        sys.exit("set WERAG_API_KEY or WERAG_TOKEN")

    crawl = sqlite3.connect(f"file:{os.path.join(args.crawl_dir, 'state.sqlite')}?mode=ro", uri=True)
    state = sqlite3.connect(os.path.join(args.crawl_dir, "ingest.sqlite"))
    state.execute(
        """CREATE TABLE IF NOT EXISTS uploads (
            kb_id TEXT NOT NULL, vbpl_id TEXT NOT NULL, knowledge_id TEXT,
            html_sha256 TEXT NOT NULL, uploaded_at TEXT NOT NULL,
            PRIMARY KEY (kb_id, vbpl_id))"""
    )
    done = {
        r[0]: r[1]
        for r in state.execute("SELECT vbpl_id, html_sha256 FROM uploads WHERE kb_id = ?", (args.kb,))
    }

    wanted = None
    if args.ids:
        with open(args.ids, encoding="utf-8") as f:
            wanted = {line.strip() for line in f if line.strip()}

    uploaded = skipped = failed = 0
    for doc_id, raw in crawl.execute("SELECT id, detail FROM docs WHERE detail IS NOT NULL ORDER BY id"):
        if wanted is not None and doc_id not in wanted:
            continue
        detail = json.loads(raw)
        if args.doc_type and name_of(detail.get("docType")) not in args.doc_type:
            continue
        if args.eff_status and name_of(detail.get("effStatus")) not in args.eff_status:
            continue
        path = os.path.join(args.crawl_dir, "html", f"{doc_id}.html")
        if not os.path.exists(path) or os.path.getsize(path) < MIN_HTML_BYTES:
            continue
        with open(path, "rb") as f:
            content = f.read()
        sha = hashlib.sha256(content).hexdigest()
        if done.get(doc_id) == sha:
            skipped += 1
            continue
        name, meta = file_name(detail), metadata(detail)
        if args.dry_run:
            print(f"would upload {doc_id}: {name} {meta}")
        else:
            try:
                resp = upload(args.api, args.kb, headers, name, meta, content)
            except urllib.error.HTTPError as e:
                failed += 1
                print(f"{doc_id}: HTTP {e.code} {e.read()[:300]!r}", file=sys.stderr)
                continue
            except (urllib.error.URLError, TimeoutError) as e:
                failed += 1
                print(f"{doc_id}: {e}", file=sys.stderr)
                continue
            kid = (resp.get("data") or {}).get("id")
            state.execute(
                "INSERT OR REPLACE INTO uploads VALUES (?, ?, ?, ?, ?)",
                (args.kb, doc_id, kid, sha, dt.datetime.now(dt.timezone.utc).isoformat()),
            )
            state.commit()
            print(f"uploaded {doc_id} → {kid}: {name}")
            time.sleep(args.sleep)
        uploaded += 1
        if args.limit and uploaded >= args.limit:
            break
    print(f"done: uploaded={uploaded} unchanged={skipped} failed={failed}")


if __name__ == "__main__":
    main()
