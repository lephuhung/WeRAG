#!/usr/bin/env python3
"""Run tracking of dcs-sync through the backend (legal_sync_runs, worker
dcs_sync): the container has no database credentials, so sync.sh records
its run over HTTP with the same API key as the pushes.

    run_track.py start STEP [--summary JSON]       → prints the run id
    run_track.py step ID STEP [--summary-file F]
    run_track.py heartbeat ID
    run_track.py finish ID ok|failed [--error-file F] [--summary-file F]
    run_track.py summarize fetch|build|upload LOG   → {"<step>": {counts}} on stdout

POST {WERAG_API}/system/legal-sync/runs[/ID/step|heartbeat|finish], header
X-API-Key: WERAG_API_KEY (an API key of organization 10000 with ingest) or
Authorization: Bearer WERAG_TOKEN. Each call is bounded by
DCS_RUN_TRACK_TIMEOUT seconds (default 20). Exit 0 on success, 1 otherwise
(the reason on stderr); sync.sh never fails on it.

The error sent with "finish failed" is the tail of the failed step's
output: the API key (and token) masked, control characters but newline
and tab and ANSI colour codes dropped, the last 8000 bytes on a character
boundary (the backend keeps 4000).
"""
from __future__ import annotations

import json
import os
import re
import sys
import urllib.error
import urllib.request

WORKER = "dcs_sync"
ERROR_TAIL = 8000

_CONTROL = re.compile(r"\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b[@-_]|[\x00-\x08\x0b-\x1f\x7f]")


def sanitize(text: str) -> str:
    for name in ("WERAG_API_KEY", "WERAG_TOKEN"):
        secret = os.environ.get(name, "")
        if len(secret) >= 8:
            text = text.replace(secret, "***")
    text = text.replace("\r\n", "\n").replace("\r", "\n")
    text = _CONTROL.sub("", text).strip()
    raw = text.encode("utf-8")
    if len(raw) > ERROR_TAIL:
        text = "…" + raw[-ERROR_TAIL:].decode("utf-8", "ignore")
    return text


def call(path: str, body: dict | None = None) -> dict:
    api = os.environ.get("WERAG_API", "http://localhost:18080/api/v1").rstrip("/")
    headers = {"Content-Type": "application/json"}
    if os.environ.get("WERAG_API_KEY"):
        headers["X-API-Key"] = os.environ["WERAG_API_KEY"]
    elif os.environ.get("WERAG_TOKEN"):
        headers["Authorization"] = "Bearer " + os.environ["WERAG_TOKEN"]
    else:
        raise RuntimeError("WERAG_API_KEY or WERAG_TOKEN is required")
    timeout = float(os.environ.get("DCS_RUN_TRACK_TIMEOUT") or 20)
    data = json.dumps(body or {}, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(api + "/system/legal-sync/runs" + path, data=data, method="POST", headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return json.loads(r.read().decode("utf-8") or "{}")
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", "replace")[:300]
        raise RuntimeError(f"HTTP {e.code}: {sanitize(raw)}") from None
    except (urllib.error.URLError, TimeoutError, ConnectionError, OSError) as e:
        raise RuntimeError(f"transport: {e}") from None


def read_json_file(path: str | None) -> dict:
    if not path:
        return {}
    try:
        with open(path, encoding="utf-8") as f:
            obj = json.load(f)
    except (OSError, ValueError):
        return {}  # a step that failed early wrote no summary
    return obj if isinstance(obj, dict) else {}


def read_text_file(path: str | None) -> str:
    if not path:
        return ""
    try:
        with open(path, "rb") as f:
            return f.read().decode("utf-8", "replace")
    except OSError:
        return ""


# --- counts of the scripts' own output -----------------------------------

_FETCH = re.compile(r"^(\d+) records \((\d+) loaded now\)", re.M)
_BUILD = re.compile(r"^(\d+) documents: (.*)$", re.M)
_PUSH = re.compile(r"^(\d{1,3}) (full|scan|queue)\s", re.M)
_PUSH_FAILED = re.compile(r"^(\d+) pushes failed", re.M)


def summarize(kind: str, text: str) -> dict:
    if kind == "fetch":
        out = {"records": 0, "loaded": 0}
        for m in _FETCH.finditer(text):
            out["records"] += int(m.group(1))
            out["loaded"] += int(m.group(2))
        return out
    if kind == "build":
        m = None
        for m in _BUILD.finditer(text):
            pass
        if not m:
            return {}
        out = {"planned": int(m.group(1))}
        for part in m.group(2).split(","):
            name, _, n = part.strip().rpartition(" ")
            if name and n.isdigit():
                out[name] = int(n)
        return out
    if kind == "upload":
        out = {"pushed": 0, "queued": 0, "existing": 0, "refused": 0, "failed": 0}
        for m in _PUSH.finditer(text):
            code, act = int(m.group(1)), m.group(2)
            if 200 <= code < 300:
                out["queued" if act == "queue" else "pushed"] += 1
            elif code == 409:
                out["existing"] += 1
            elif code in (400, 422):
                out["refused"] += 1
            else:
                out["failed"] += 1
        if "STOP: the API refused the credentials" in text:
            out["credentials_refused"] = True
        return out
    raise SystemExit(f"unknown summary kind {kind!r}")


def main(argv: list[str]) -> int:
    def opt(name: str) -> str | None:
        if name in argv:
            i = argv.index(name)
            if i + 1 < len(argv):
                return argv[i + 1]
        return None

    if not argv:
        print(__doc__, file=sys.stderr)
        return 2
    cmd, args = argv[0], argv[1:]
    try:
        if cmd == "summarize":
            counts = summarize(args[0], read_text_file(args[1]))
            print(json.dumps({args[0]: counts} if counts else {}, ensure_ascii=False))
            return 0
        if cmd == "start":
            summary = json.loads(opt("--summary") or "{}")
            res = call("", {"worker": WORKER, "step": args[0], "summary": summary})
            run_id = (res.get("data") or {}).get("id")
            if not isinstance(run_id, int) or run_id <= 0:
                raise RuntimeError(f"no run id in the answer: {str(res)[:200]}")
            print(run_id)
            return 0
        run_id = int(args[0])
        if cmd == "step":
            call(f"/{run_id}/step", {"step": args[1], "summary": read_json_file(opt("--summary-file"))})
        elif cmd == "heartbeat":
            call(f"/{run_id}/heartbeat")
        elif cmd == "finish":
            status = args[1]
            body = {"status": status, "summary": read_json_file(opt("--summary-file"))}
            if status == "failed":
                body["error"] = sanitize(read_text_file(opt("--error-file")))
            call(f"/{run_id}/finish", body)
        else:
            print(f"unknown command {cmd!r}", file=sys.stderr)
            return 2
    except (RuntimeError, ValueError, IndexError) as e:
        print(f"run tracking {cmd}: {e}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
