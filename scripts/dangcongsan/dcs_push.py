#!/usr/bin/env python3
"""Push the Party documents planned by dcs_build.py to WeRAG.

Per ``plan.jsonl`` case:

* ``text``      → ``POST /system/legal-documents/push`` (mode full) with the
                  built v1 HTML: catalogue row + full text in the KB
                  "Văn bản của Đảng" (unreviewed feed row, review link pending);
* ``ocr``, ``catalogue`` → ``push`` mode catalogue with request_full_text:
                  the catalogue row and a request in the legal request queue
                  (source dangcongsan_no_text), which the TVPL extension works
                  through: TVPL's typed text, no OCR. With ``--ocr`` a scan
                  goes to ``POST /system/legal-documents/push-scan`` instead
                  (PDF parts merged in order → an OCR draft the SuperAdmin
                  reviews at /platform/admin/legal-drafts/{id}): the fallback
                  for documents the extension marked "không có trên TVPL";
* ``skip``      → nothing.

A document already sent for OCR is not queued (its draft would race the
TVPL text).

One request at a time. A scan of ≤ 20 pages is OCRed while the request
waits, so the queue paces itself; after a longer one the script sleeps
--sec-per-page × pages. ``<out>/push-results.jsonl`` records every answer
(with the run it came from); a document already pushed with success is
skipped on the next run.

A document the server keeps failing on (HTTP 500 and the like, or a
request that times out) in GIVE_UP_RUNS runs in a row is given up: the
next run records it as refused (``gave_up``, with the reason) instead of
trying again, so one poison document cannot fail every run (and keep the
SuperAdmin alert on) for ever. Like a 400/422, it is tried again once its
metadata changes. Failures that say nothing about the document (the
backend unreachable, a wrong WERAG_API, 429, 502–504) do not count, nor
does a run in which every push of three or more failed.

Authentication: WERAG_API_KEY (X-API-Key of a key created in platform
organization 10000 with ingest) or WERAG_TOKEN (SuperAdmin JWT).
WERAG_API defaults to http://localhost:18080/api/v1.
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
import urllib.request
import uuid

import pymupdf

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))  # siblings, also under python -I
import dcs_build  # noqa: E402

SYNC_OCR_PAGES = 20


def multipart(fields: dict, file_field: str | None, filename: str, content: bytes | None, ctype: str) -> tuple[bytes, str]:
    boundary = uuid.uuid4().hex
    parts = []
    for k, v in fields.items():
        parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{k}"\r\n\r\n{v}\r\n'.encode())
    if file_field and content is not None:
        safe = re.sub(r'["\r\n]', "_", filename)
        parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{file_field}"; '
                     f'filename="{safe}"\r\nContent-Type: {ctype}\r\n\r\n'.encode())
        parts.append(content)
        parts.append(b"\r\n")
    parts.append(f"--{boundary}--\r\n".encode())
    return b"".join(parts), f"multipart/form-data; boundary={boundary}"


def post(api: str, path: str, headers: dict, fields: dict, filename: str = "", content: bytes | None = None,
         ctype: str = "application/octet-stream", timeout: int = 1800) -> tuple[int, dict]:
    body, mp = multipart(fields, "file" if content is not None else None, filename, content, ctype)
    req = urllib.request.Request(api.rstrip("/") + path, data=body, method="POST", headers={**headers, "Content-Type": mp})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, json.loads(r.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", "replace")
        try:
            return e.code, json.loads(raw)
        except ValueError:
            return e.code, {"raw": raw[:500]}
    except (urllib.error.URLError, TimeoutError, ConnectionError) as e:
        return 0, {"error": {"message": f"transport: {e}"}}


def merged_pdf(parts: list[dict]) -> tuple[bytes, int, list[str]]:
    """The PDF parts in order as one PDF; the parts left out (no PDF)."""
    out = pymupdf.open()
    left = []
    for p in parts:
        if not p.get("path") or not os.path.exists(p["path"]):
            left.append(p["name"])
            continue
        with open(p["path"], "rb") as f:
            data = f.read()
        if p.get("member"):
            data = dcs_build.archive_member(data, p["member"])
        if not data.lstrip()[:5].startswith(b"%PDF"):
            left.append(p["name"])
            continue
        src = pymupdf.open(stream=data, filetype="pdf")
        out.insert_pdf(src)
    pages = out.page_count
    return (out.tobytes(garbage=3, deflate=True) if pages else b""), pages, left


ACTION_OF_CASE = {"text": "full", "ocr": "scan", "catalogue": "catalogue"}


# Answers that settle a document for good: pushed, already held, or refused
# for what was sent (400/422, 413 too large; retried once the metadata
# changes). 401/403 stop the run; 429/5xx and transport errors are retried
# next run.
FINAL_REFUSALS = {400, 413, 422}

# A document failing in this many runs in a row is given up (see above).
GIVE_UP_RUNS = 5
# Failures that say nothing about the document: not counted toward giving up.
NOT_THE_DOCUMENT = {0, 404, 405, 429, 502, 503, 504}


def meta_hash(md: dict) -> str:
    return hashlib.sha1(json.dumps(md, sort_keys=True, ensure_ascii=False).encode()).hexdigest()[:12]


def load_done(path: str) -> set[tuple[str, str, str]]:
    """(source_doc_id, action, meta hash or "*") settled; older lines without
    an action name it by their case, and count whatever the metadata."""
    done = set()
    if os.path.exists(path):
        with open(path, encoding="utf-8") as f:
            for line in f:
                r = json.loads(line)
                act = r.get("action") or ACTION_OF_CASE.get(r.get("case"), "")
                if r.get("ok"):
                    done.add((r["source_doc_id"], act, "*"))
                elif (r.get("status") in FINAL_REFUSALS or r.get("gave_up")) and r.get("meta_hash"):
                    done.add((r["source_doc_id"], act, r["meta_hash"]))
    return done


def _document_failure(r: dict) -> bool:
    """A failed answer that is about the document (counts toward giving up)."""
    if r.get("ok") or r.get("gave_up") or r.get("status") in FINAL_REFUSALS:
        return False
    status = r.get("status")
    if status == 0:
        msg = json.dumps(r.get("answer") or {}, ensure_ascii=False)
        return "timed out" in msg  # the server hung on it; DNS / refused say nothing
    return isinstance(status, int) and status not in NOT_THE_DOCUMENT


def failure_streaks(path: str) -> dict[tuple[str, str], tuple[int, str]]:
    """(source_doc_id, action) → (runs in a row the server failed on the
    document, the last such answer), from push-results.jsonl in order. A
    success, a refusal or a give-up ends the streak; a run in which every
    push of three or more failed is ignored (an outage, not the document);
    lines without a run (older) count one run each."""
    if not os.path.exists(path):
        return {}
    rows = []
    with open(path, encoding="utf-8") as f:
        for i, line in enumerate(f):
            try:
                r = json.loads(line)
            except ValueError:
                continue
            r.setdefault("run", f"line-{i}")
            rows.append(r)
    attempts: dict[str, list[bool]] = {}
    for r in rows:
        attempts.setdefault(r["run"], []).append(bool(r.get("ok")) or r.get("status") in FINAL_REFUSALS)
    outage = {run for run, oks in attempts.items() if len(oks) >= 3 and not any(oks)}
    streaks: dict[tuple[str, str], tuple[int, str]] = {}
    for r in rows:
        if r["run"] in outage:
            continue
        key = (r.get("source_doc_id", ""), r.get("action") or ACTION_OF_CASE.get(r.get("case"), ""))
        if _document_failure(r):
            n, _ = streaks.get(key, (0, ""))
            streaks[key] = (n + 1, f"HTTP {r.get('status')}")
        elif r.get("ok") or r.get("gave_up") or r.get("status") in FINAL_REFUSALS:
            streaks.pop(key, None)
    return streaks


def is_done(done: set, sid: str, act: str, mh: str) -> bool:
    return (sid, act, "*") in done or (sid, act, mh) in done


def action_for(case: str, ocr: bool) -> str:
    """full (text pushed), scan (OCR draft), queue (catalogue row + a request
    in the legal request queue for the TVPL extension)."""
    if case == "text":
        return "full"
    if case == "ocr" and ocr:
        return "scan"
    return "queue"


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("plan_dir", help="dcs_build.py --out-dir")
    ap.add_argument("--case", action="append", choices=["text", "ocr", "catalogue"],
                    help="only these cases (repeatable; default all)")
    ap.add_argument("--limit", type=int, default=0, help="stop after N pushes")
    ap.add_argument("--only", action="append", default=[], help="only this số hiệu or source_doc_id (repeatable)")
    ap.add_argument("--sec-per-page", type=float, default=6.0, help="pause after a background OCR (default 6 s/page)")
    ap.add_argument("--ocr", action="store_true",
                    help="push scans for OCR drafts (push-scan) instead of queueing them for the TVPL extension")
    ap.add_argument("--dry-run", action="store_true", help="print what would be pushed")
    a = ap.parse_args(argv)

    api = os.environ.get("WERAG_API", "http://localhost:18080/api/v1")
    headers = {}
    if os.environ.get("WERAG_API_KEY"):
        headers["X-API-Key"] = os.environ["WERAG_API_KEY"]
    elif os.environ.get("WERAG_TOKEN"):
        headers["Authorization"] = "Bearer " + os.environ["WERAG_TOKEN"]
    elif not a.dry_run:
        print("WERAG_API_KEY or WERAG_TOKEN is required", file=sys.stderr)
        return 2
    cases = set(a.case or ["text", "ocr", "catalogue"])
    results_path = os.path.join(a.plan_dir, "push-results.jsonl")
    done = load_done(results_path)
    streaks = failure_streaks(results_path)
    run_id = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())
    n = 0
    server_errors = 0
    with open(os.path.join(a.plan_dir, "plan.jsonl"), encoding="utf-8") as fin, \
            open(results_path, "a", encoding="utf-8") as fres:
        for line in fin:
            e = json.loads(line)
            md = e["metadata"]
            sid = md["source_doc_id"]
            act = action_for(e["case"], a.ocr)
            mh = meta_hash(md)
            if e["case"] not in cases or is_done(done, sid, act, mh):
                continue
            if act == "queue" and (sid, "scan", "*") in done:
                continue  # an OCR draft waits for review: TVPL text must not race it
            if a.only and md["doc_number"] not in a.only and sid not in a.only:
                continue
            if a.limit and n >= a.limit:
                break
            res = {"source_doc_id": sid, "doc_number": md["doc_number"], "case": e["case"], "action": act, "meta_hash": mh,
                   "run": run_id}
            fails, last = streaks.get((sid, act), (0, ""))
            if fails >= GIVE_UP_RUNS and not a.dry_run:
                res.update(ok=False, gave_up=True, reason=f"máy chủ lỗi {fails} lần chạy liên tiếp ({last}): bỏ qua tới khi metadata đổi")
                fres.write(json.dumps(res, ensure_ascii=False) + "\n")
                fres.flush()
                print(f"GAVE_UP {act:<9} {md['doc_number']:<20} {res['reason']}", flush=True)
                continue
            meta_json = json.dumps(md, ensure_ascii=False)
            if e["case"] == "text":
                with open(os.path.join(a.plan_dir, e["html"]), "rb") as f:
                    html = f.read()
                if a.dry_run:
                    print(f"text  {md['doc_number']:<20} {len(html):>8} B  {md['title'][:70]}")
                    continue
                code, body = post(api, "/system/legal-documents/push", headers, {"metadata": meta_json, "mode": "full"},
                                  os.path.basename(e["html"]), html, "text/html; charset=utf-8")
            elif act == "scan":
                pdf, pages, left = merged_pdf([p for p in e["parts"]])
                res["pages"], res["left_out"] = pages, left
                if not pdf:
                    res.update(ok=False, status=0, error="no PDF part")
                    fres.write(json.dumps(res, ensure_ascii=False) + "\n")
                    continue
                if a.dry_run:
                    print(f"ocr   {md['doc_number']:<20} {pages:>4} pages {len(pdf) >> 10:>6} KB  left out {left}")
                    continue
                name = re.sub(r"[^\w.\-]+", "_", md["doc_number"]) + ".pdf"
                code, body = post(api, "/system/legal-documents/push-scan", headers, {"metadata": meta_json}, name, pdf,
                                  "application/pdf")
            else:
                if a.dry_run:
                    print(f"queue {md['doc_number']:<20} {e['case']:<9} {e['why'][:70]}")
                    continue
                meta_json = json.dumps({**md, "request_full_text": "true"}, ensure_ascii=False)
                code, body = post(api, "/system/legal-documents/push", headers, {"metadata": meta_json, "mode": "catalogue"})
            data = body.get("data") or {}
            res.update(status=code, ok=200 <= code < 300 or code == 409, answer=data or body)
            if code == 409:
                res["note"] = "đã có toàn văn"
            fres.write(json.dumps(res, ensure_ascii=False) + "\n")
            fres.flush()
            n += 1
            draft = data.get("draft") or {}
            print(f"{code} {act:<9} {md['doc_number']:<20} "
                  f"{data.get('ingest') or draft.get('status') or ('queued' if data.get('full_text_requested') else '')} {(body.get('error') or {}).get('message', '') if isinstance(body.get('error'), dict) else ''}",
                  flush=True)
            if code in (401, 403):
                print(f"STOP: the API refused the credentials (HTTP {code}): {json.dumps(body, ensure_ascii=False)[:300]}",
                      file=sys.stderr)
                return 3
            if not (200 <= code < 300 or code == 409 or code in FINAL_REFUSALS):
                server_errors += 1  # 404/405/413 (wrong WERAG_API?), 429, 5xx, transport
            if act == "scan" and code == 200 and res.get("pages", 0) > SYNC_OCR_PAGES and not data.get("existing"):
                time.sleep(a.sec_per_page * res["pages"])
    if server_errors:
        print(f"{server_errors} pushes failed (HTTP 404/429/5xx, transport…): retried next run", file=sys.stderr)
        return 4
    return 0


if __name__ == "__main__":
    sys.exit(main())
