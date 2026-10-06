#!/usr/bin/env python3
"""Export Python reference results as parity fixtures for the Go port
(internal/docformat).

For every document — the synthetic corpus plus the hand-built test fixtures —
writes ``<name>.docx`` and ``<name>.json`` holding what the Python
implementation produces:

- ``units``: the labeling units (id, para, text, zone, ..., hint),
- ``heuristic``: detected type, components and every check result with the
  heuristic segmenter,
- ``labels`` / ``labeled``: when gold labels exist, the same report produced
  from those labels (exercises apply_labels + rules on clean components).

    python3 evaluation/export_parity.py ../internal/docformat/testdata/parity
"""

from __future__ import annotations

import glob
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
sys.path.insert(0, ROOT)
sys.path.insert(0, os.path.join(ROOT, "tests"))

from docformat.checker import check_document  # noqa: E402
from docformat.labeling import build_units  # noqa: E402
from docformat.layout import inspect_docx  # noqa: E402
from docformat.segment import segment  # noqa: E402


def report_view(rep: dict) -> dict:
    return {
        "detected": rep["document_type"]["detected"],
        "used": rep["document_type"]["used"],
        "rule_set": rep["document_type"]["rule_set"],
        "components": rep["components"],
        "checks": rep["checks"],
        "summary": rep["summary"],
    }


def export(name: str, content: bytes, gold: dict, out_dir: str):
    layout = inspect_docx(content)
    heur = segment(layout)
    out = {
        "units": build_units(layout, heur),
        "heuristic": report_view(check_document(content, segmenter="heuristic")),
    }
    if gold is not None:
        out["labels"] = gold
        out["labeled"] = report_view(check_document(content, labels=gold))
    with open(os.path.join(out_dir, name + ".docx"), "wb") as fh:
        fh.write(content)
    with open(os.path.join(out_dir, name + ".json"), "w", encoding="utf-8") as fh:
        json.dump(out, fh, ensure_ascii=False, separators=(",", ":"))


def fixture_docs():
    """Documents built by the Python unit tests."""
    import test_check as TC
    import test_labeling as TL
    import test_segment_real as TR

    docs = {
        "fx_good_cong_van": TC.good_cong_van(),
        "fx_good_cong_van_arial": TC.good_cong_van(font="Arial"),
        "fx_flat_cong_van": TC.flat_cong_van(),
        "fx_flat_centered": TL.flat_centered_doc(11),
    }
    captured = {}
    orig = TR.comps_of

    def cap(content):
        captured["last"] = content
        return orig(content)
    TR.comps_of = cap
    try:
        TR.TestSoAgencyQuyetDinh().setUp()
        docs["fx_qd_so_y_te"] = captured["last"]
        TR.TestBaoCaoTwoHeaderTables().setUp()
        docs["fx_bao_cao_two_tables"] = captured["last"]
        for style in ("tabs", "indent"):
            TR.TestCongVanTabSignature().build(style)
            docs["fx_cong_van_" + style] = captured["last"]
    finally:
        TR.comps_of = orig
    import test_ky_thay as TK
    kt_cases = {
        "ok": (["KT. TRƯỞNG PHÒNG", "PHÓ TRƯỞNG PHÒNG"],
               ["- Như trên;", "- Đ/c Trưởng phòng (để b/c);", "- Lưu: VT."]),
        "missing_kt": (["TRƯỞNG PHÒNG", "PHÓ TRƯỞNG PHÒNG"],
                       ["- Như trên;", "- Lưu: PA05 (Đ4)."]),
        "mismatch": (["KT. GIÁM ĐỐC", "PHÓ TRƯỞNG PHÒNG"], ["- Giám đốc (để b/c);"]),
        "tm_chain": (["TM. ỦY BAN NHÂN DÂN", "KT. CHỦ TỊCH", "PHÓ CHỦ TỊCH"],
                     ["- CT UBND tỉnh (để báo cáo);", "- Lưu: VT."]),
        "unmarked": (["KT. TRƯỞNG PHÒNG", "PHÓ TRƯỞNG PHÒNG"],
                     ["- Trưởng phòng;", "- Lưu: VT."]),
        "deputy_listed": (["KT. TRƯỞNG PHÒNG", "PHÓ TRƯỞNG PHÒNG"],
                          ["- Phó Trưởng phòng (để b/c);", "- Lưu: VT."]),
    }
    for name, (sig, nn) in kt_cases.items():
        docs["fx_ky_thay_" + name] = TK.signed_doc(sig, nn)
    return docs


def main(argv=None) -> int:
    argv = argv if argv is not None else sys.argv[1:]
    out_dir = argv[0] if argv else os.path.join(
        ROOT, "..", "internal", "docformat", "testdata", "parity")
    synth = argv[1] if len(argv) > 1 else os.path.join(HERE, "samples", "synth")
    os.makedirs(out_dir, exist_ok=True)
    n = 0
    for path in sorted(glob.glob(os.path.join(synth, "*.docx"))):
        gold_path = path[:-5] + ".labels.json"
        gold = None
        if os.path.exists(gold_path):
            with open(gold_path, encoding="utf-8") as fh:
                g = json.load(fh)
            gold = {"document_type": g["document_type"], "labels": g["labels"]}
        with open(path, "rb") as fh:
            export("synth_" + os.path.basename(path)[:-5], fh.read(), gold, out_dir)
        n += 1
    for name, content in fixture_docs().items():
        export(name, content, None, out_dir)
        n += 1
    print("wrote %d parity fixtures to %s" % (n, os.path.abspath(out_dir)))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
