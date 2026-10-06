#!/usr/bin/env python3
"""Build doc-format-check.zip — the tenant-skill bundle installable through
WeKnora's sandbox-config skill upload.

Bundle layout:
    SKILL.md            (at zip root — the skill manifest)
    docformat/          (the pure-stdlib checker package)

The agent runs it with:
    PYTHONPATH="$WEKNORA_SKILL_DIR" python3 -m docformat check <file.docx>

Usage: python3 scripts/build_skill_bundle.py [out.zip]
"""

import os
import sys
import zipfile

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)                     # docformat/ project root
PKG = os.path.join(ROOT, "docformat")
SKILL = os.path.join(ROOT, "skill", "doc-format-check", "SKILL.md")

SKIP_DIRS = {"__pycache__", ".pytest_cache"}
SKIP_FILES = {"mcp_server.py"}  # needs the 'mcp' package — not for sandboxes


def main() -> int:
    out = sys.argv[1] if len(sys.argv) > 1 else os.path.join(
        ROOT, "dist", "doc-format-check.zip")
    os.makedirs(os.path.dirname(out), exist_ok=True)
    n = 0
    with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as zf:
        zf.write(SKILL, "SKILL.md")
        n += 1
        for dirpath, dirnames, filenames in os.walk(PKG):
            dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS]
            for fn in filenames:
                if fn in SKIP_FILES or fn.endswith((".pyc", ".pyo")):
                    continue
                src = os.path.join(dirpath, fn)
                arc = os.path.relpath(src, ROOT)
                zf.write(src, arc)
                n += 1
    print("wrote %s (%d files)" % (out, n))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
