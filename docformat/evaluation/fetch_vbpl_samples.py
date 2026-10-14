#!/usr/bin/env python3
"""Download the real vbpl.vn documents listed in real_vbpl_manifest.json.

The files are public documents from vbpl.vn; samples/ is git-ignored, so only
the manifest (doc id, file name, sha256, expectations) is committed. The
gateway serves an attachment at

    https://vbpl-bientap-gateway.moj.gov.vn/api/qtdc/public/doc/minio/
        buckets/vbpl/<doc_id>/<file_name>/download

with the file name percent-encoded — in Unicode NFD for most files, as
listed (NFC) for others, so both are tried. The WAF
rejects requests without a browser User-Agent and rate-limits fast clients,
so calls are spaced by --delay seconds.

    python3 evaluation/fetch_vbpl_samples.py [--out evaluation/samples/real_vbpl]

Then run the Go test:

    DOCFORMAT_REAL_DIR=docformat/evaluation/samples/real_vbpl \\
        go test -run TestRealDocuments -v ./internal/docformat/
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import sys
import time
import unicodedata
import urllib.parse
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
BASE = "https://vbpl-bientap-gateway.moj.gov.vn/api/qtdc/public/doc/minio/buckets/vbpl"
UA = ("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) "
      "Chrome/124.0 Safari/537.36")


def file_urls(doc_id: str, file_name: str) -> list:
    out = []
    for form in ("NFD", "NFC"):
        name = urllib.parse.quote(unicodedata.normalize(form, file_name))
        url = "%s/%s/%s/download" % (BASE, urllib.parse.quote(doc_id), name)
        if url not in out:
            out.append(url)
    return out


def fetch(doc_id: str, file_name: str, delay: float) -> bytes:
    err = None
    for i, url in enumerate(file_urls(doc_id, file_name)):
        if i:
            time.sleep(delay)
        req = urllib.request.Request(url, headers={"User-Agent": UA})
        try:
            with urllib.request.urlopen(req, timeout=60) as resp:
                return resp.read()
        except Exception as exc:  # noqa: BLE001 — try the other form
            err = exc
    raise err


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--manifest", default=os.path.join(HERE, "real_vbpl_manifest.json"))
    ap.add_argument("--out", default=os.path.join(HERE, "samples", "real_vbpl"))
    ap.add_argument("--delay", type=float, default=1.2, help="seconds between calls (>= 1)")
    args = ap.parse_args()
    with open(args.manifest, encoding="utf-8") as fh:
        files = json.load(fh)["files"]
    os.makedirs(args.out, exist_ok=True)
    bad = 0
    for i, e in enumerate(files):
        path = os.path.join(args.out, e["file"])
        if os.path.exists(path):
            with open(path, "rb") as fh:
                if sha256(fh.read()) == e["sha256"]:
                    print("ok      %s" % e["file"])
                    continue
        try:
            data = fetch(e["doc_id"], e["file_name"], max(args.delay, 1.0))
        except Exception as exc:  # noqa: BLE001 — report and go on
            print("FAILED  %s (%s %s): %s" % (e["file"], e["doc_num"], e["doc_id"], exc))
            bad += 1
        else:
            if sha256(data) != e["sha256"]:
                print("CHANGED %s: sha256 differs from the manifest" % e["file"])
                bad += 1
            with open(path, "wb") as fh:
                fh.write(data)
            print("fetched %s" % e["file"])
        if i < len(files) - 1:
            time.sleep(max(args.delay, 1.0))
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
