"""Download Chrome's Screen AI component (Linux x64) into ./component/<version>/.

The component is fetched from Google's component updater the same way Chrome
does it; nothing is vendored in the repo. Also builds the one-symbol stub
(threadlogger::EnableThreadedLogging) that Chrome's host process normally
provides — without it dlopen fails with an undefined symbol.

    python fetch_component.py            # latest version
    python fetch_component.py --force    # re-download even if present
    python fetch_component.py --if-missing --no-stub   # container entrypoint
"""

import argparse
import hashlib
import io
import json
import os
import shutil
import struct
import subprocess
import sys
import zipfile

import requests

APP_ID = "mfhmdacoffpmifoibamicehhklffanao"  # Chrome Screen AI component
UPDATE_URL = "https://update.googleapis.com/service/update2/json"
CHROME_VERSION = "141.0.7390.54"  # any recent stable works; only gates eligibility
HERE = os.path.dirname(os.path.abspath(__file__))
STUB_SRC = "namespace threadlogger { void EnableThreadedLogging(int) {} }\n"


def query_update() -> tuple[str, str, str]:
    body = {
        "request": {
            "@os": "linux",
            "@updater": "chrome",
            "acceptformat": "crx3",
            "app": [{"appid": APP_ID, "version": "0.0.0.0", "updatecheck": {}}],
            "arch": "x64",
            "nacl_arch": "x86-64",
            "os": {"arch": "x86_64", "platform": "Linux"},
            "prodversion": CHROME_VERSION,
            "protocol": "3.1",
            "updaterversion": CHROME_VERSION,
        }
    }
    r = requests.post(UPDATE_URL, json=body, timeout=30)
    r.raise_for_status()
    text = r.text
    if text.startswith(")]}'"):
        text = text[4:]
    app = json.loads(text)["response"]["app"][0]
    uc = app["updatecheck"]
    if uc.get("status") != "ok":
        raise SystemExit(f"update server: {uc}")
    pkg = uc["manifest"]["packages"]["package"][0]
    base = next(u["codebase"] for u in uc["urls"]["url"] if u["codebase"].startswith("https://"))
    return uc["manifest"]["version"], base + pkg["name"], pkg["hash_sha256"]


def build_stub(dest: str) -> None:
    src = os.path.join(dest, "threadlogger_stub.cc")
    with open(src, "w") as f:
        f.write(STUB_SRC)
    cxx = shutil.which("g++") or shutil.which("c++") or shutil.which("clang++")
    if not cxx:
        raise SystemExit("need a C++ compiler (g++) to build the threadlogger stub")
    subprocess.run(
        [cxx, "-shared", "-fPIC", "-o", os.path.join(dest, "libthreadlogger_stub.so"), src],
        check=True,
    )


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--dest", default=os.environ.get("SCREENAI_COMPONENT_DIR", os.path.join(HERE, "component")))
    ap.add_argument("--force", action="store_true")
    ap.add_argument("--if-missing", action="store_true", help="do nothing when latest/ already holds the library")
    ap.add_argument("--no-stub", action="store_true", help="skip building the stub (the image ships one)")
    args = ap.parse_args()

    latest = os.path.join(args.dest, "latest")
    if args.if_missing and os.path.exists(os.path.join(latest, "libchromescreenai.so")):
        print(f"Screen AI present: {os.path.realpath(latest)}")
        return

    version, url, sha = query_update()
    dest = os.path.join(args.dest, version)
    if os.path.exists(os.path.join(dest, "libchromescreenai.so")) and not args.force:
        print(f"Screen AI {version} already in {dest}")
    else:
        print(f"downloading Screen AI {version} …")
        r = requests.get(url, timeout=600)
        r.raise_for_status()
        data = r.content
        if hashlib.sha256(data).hexdigest() != sha:
            raise SystemExit("sha256 mismatch — refusing to extract")
        if data[:4] != b"Cr24":
            raise SystemExit("not a CRX file")
        header_len = struct.unpack("<I", data[8:12])[0]
        os.makedirs(dest, exist_ok=True)
        zipfile.ZipFile(io.BytesIO(data[12 + header_len :])).extractall(dest)
    if not args.no_stub:
        build_stub(dest)
    if os.path.islink(latest) or os.path.exists(latest):
        os.remove(latest)
    os.symlink(version, latest)
    print(f"ready: {os.path.join(latest, 'libchromescreenai.so')}")


if __name__ == "__main__":
    sys.exit(main())
