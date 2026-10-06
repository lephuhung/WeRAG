#!/usr/bin/env python3
"""Draft gold labels for real documents with the LLM, for human review.

For every ``X.docx`` without ``X.labels.json`` under the given directories,
label it with the configured LLM (DOCFORMAT_LLM_*) — or the heuristic with
``--heuristic`` — and write ``X.labels.json`` in the reviewable list form::

    {"document_type": "quyet_dinh", "draft": true,
     "labels": [{"id": "0", "label": "co_quan_chu_quan", "text": "UBND TỈNH ..."},
                ...]}

Review each file: fix ``label`` where wrong (component names: see
``docformat/labeling.py`` COMPONENTS), fix ``document_type``, then set
``"draft": false``.  ``run_eval.py`` skips drafts unless ``--include-drafts``.

    python3 evaluation/draft_labels.py evaluation/samples/real
"""

from __future__ import annotations

import argparse
import glob
import json
import os
import sys
from concurrent.futures import ThreadPoolExecutor

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))

from docformat import labeling  # noqa: E402
from docformat.labeling import _heuristic_label, build_units  # noqa: E402
from docformat.layout import inspect_docx  # noqa: E402
from docformat.segment import segment  # noqa: E402


def _dump_reviewable(out: dict) -> str:
    """JSON with one unit per line — easy to scan and edit by hand."""
    head = {k: v for k, v in out.items() if k != "labels"}
    lines = [json.dumps(u, ensure_ascii=False) for u in out["labels"]]
    body = json.dumps(head, ensure_ascii=False, indent=1)[:-2]
    return body + ',\n "labels": [\n  ' + ",\n  ".join(lines) + "\n ]\n}\n"


def draft(path: str, cfg) -> str:
    with open(path, "rb") as fh:
        layout = inspect_docx(fh.read())
    if layout.errors and not layout.paragraphs:
        return "%s: bỏ qua (%s)" % (path, "; ".join(layout.errors))
    heur = segment(layout)
    units = build_units(layout)
    seg, how = heur, "heuristic"
    if cfg is not None:
        from docformat.llm import LLMError, label_with_llm
        task, units_h, elided = labeling.prepare(layout, heur)
        try:
            reply = label_with_llm(task, cfg)
            seg, _ = labeling.apply_labels(layout, reply, heur, units_h, elided)
            how = "llm"
        except (LLMError, ValueError) as exc:
            how = "heuristic (LLM lỗi: %s)" % str(exc)[:80]
    out = {
        "document_type": seg.detected_type,
        "draft": True,
        "drafted_by": how,
        "labels": [{"id": u["id"],
                    "label": _heuristic_label(seg, u["para"], u["zone"]) or "khac",
                    "text": u["text"]} for u in units],
    }
    with open(path[:-5] + ".labels.json", "w", encoding="utf-8") as fh:
        fh.write(_dump_reviewable(out))
    return "%s: %d unit, %s" % (os.path.basename(path), len(units), how)


def main(argv=None) -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("dirs", nargs="+")
    ap.add_argument("--heuristic", action="store_true",
                    help="không gọi LLM, chỉ dùng heuristic")
    ap.add_argument("--overwrite", action="store_true")
    ap.add_argument("--workers", type=int, default=4)
    args = ap.parse_args(argv)
    cfg = None
    if not args.heuristic:
        from docformat.llm import LLMConfig
        cfg = LLMConfig.from_env()
        if cfg is None:
            print("chưa đặt DOCFORMAT_LLM_BASE_URL/MODEL — dùng --heuristic "
                  "hoặc cấu hình LLM", file=sys.stderr)
            return 2
    todo = []
    for d in args.dirs:
        for path in sorted(glob.glob(os.path.join(d, "**", "*.docx"),
                                     recursive=True)):
            if args.overwrite or not os.path.exists(path[:-5] + ".labels.json"):
                todo.append(path)
    with ThreadPoolExecutor(max_workers=args.workers) as ex:
        for line in ex.map(lambda p: draft(p, cfg), todo):
            print(line)
    print("%d file nháp — rà soát rồi đặt \"draft\": false" % len(todo))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
