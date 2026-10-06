"""CLI: python -m docformat {inspect|check|types} FILE.docx [--type ...]"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from .checker import SEGMENTERS, check_document, inspect_document, labeling_task_for
from .rules import available_types


def _render_text(report: dict) -> str:
    out = []
    dt = report.get("document_type", {})
    out.append("Văn bản: %s" % report.get("source", ""))
    out.append("Loại văn bản: phát hiện=%s, dùng bộ luật=%s (%s)" % (
        dt.get("detected"), dt.get("used"), dt.get("rule_set")))
    sg = report.get("segmentation", {})
    out.append("Gán thành phần: %s%s%s" % (
        sg.get("method"),
        " (%s)" % sg["model"] if sg.get("model") else "",
        " — %s" % sg["error"] if sg.get("error") else ""))
    if sg.get("disagreements"):
        out.append("  %d dòng LLM gán khác heuristic" % len(sg["disagreements"]))
    s = report.get("summary", {})
    out.append("Kết quả: %d đạt / %d sai / %d cảnh báo / %d bỏ qua" % (
        s.get("pass", 0), s.get("fail", 0), s.get("warn", 0), s.get("skip", 0)))
    for c in report.get("checks", []):
        mark = {"pass": "[ĐẠT]", "fail": "[SAI]",
                "warn": "[CẢNHCÁO]", "skip": "[BỎQUA]"}.get(c["status"], "[?]")
        out.append("  %s %s — %s" % (mark, c["id"], c.get("desc", "")))
        for ev in c.get("evidence", [])[:3]:
            if isinstance(ev, dict) and "text" in ev:
                out.append("       ↳ đoạn %s: %r (thực tế: %s)" % (
                    ev.get("para"), ev.get("text"), ev.get("actual")))
            elif isinstance(ev, dict):
                out.append("       ↳ %s" % ev)
        if c["status"] in ("fail", "warn") and c.get("note"):
            out.append("       ↳ %s" % c["note"])
    return "\n".join(out)


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(
        prog="docformat",
        description="Kiểm tra thể thức văn bản hành chính (.docx) theo NĐ30/2020")
    sub = ap.add_subparsers(dest="cmd", required=True)

    p_ins = sub.add_parser("inspect", help="Bóc tách layout + thành phần văn bản")
    p_ins.add_argument("file", help="đường dẫn .docx")
    p_ins.add_argument("--runs", action="store_true",
                       help="kèm chi tiết từng run (font/cỡ/đậm)")

    p_chk = sub.add_parser("check", help="Chấm thể thức theo loại văn bản")
    p_chk.add_argument("file", help="đường dẫn .docx")
    p_chk.add_argument("--type", default="auto",
                       help="cong_van|quyet_dinh|... (mặc định: tự phát hiện)")
    p_chk.add_argument("--format", choices=["json", "text"], default="text")
    for sp in (p_ins, p_chk):
        sp.add_argument("--segmenter", choices=SEGMENTERS,
                        default="heuristic" if sp is p_ins else "auto",
                        help="cách gán thành phần: auto (LLM nếu đã cấu hình "
                             "DOCFORMAT_LLM_*), heuristic, llm, labels")
        sp.add_argument("--labels", metavar="FILE",
                        help="JSON nhãn thành phần (trả lời cho label-task); "
                             "'-' = đọc từ stdin")

    p_task = sub.add_parser(
        "label-task",
        help="Xuất đề bài gán nhãn thành phần cho LLM/agent (JSON)")
    p_task.add_argument("file", help="đường dẫn .docx")

    sub.add_parser("types", help="Liệt kê loại văn bản có bộ luật")

    args = ap.parse_args(argv)
    if args.cmd == "types":
        print(json.dumps({"document_types": available_types()},
                         ensure_ascii=False, indent=2))
        return 0

    path = Path(args.file)
    if not path.is_file():
        print("không đọc được file: %s" % path, file=sys.stderr)
        return 2
    content = path.read_bytes()

    if args.cmd == "label-task":
        print(json.dumps(labeling_task_for(content), ensure_ascii=False,
                         indent=1))
        return 0

    labels = None
    if args.labels:
        raw = sys.stdin.read() if args.labels == "-" else \
            Path(args.labels).read_text(encoding="utf-8")
        labels = json.loads(raw)

    if args.cmd == "inspect":
        print(json.dumps(inspect_document(content, include_runs=args.runs,
                                          segmenter=args.segmenter,
                                          labels=labels),
                         ensure_ascii=False, indent=2))
        return 0

    report = check_document(content, document_type=args.type,
                            source_name=path.name, segmenter=args.segmenter,
                            labels=labels)
    if not report.get("ok"):
        print(json.dumps(report, ensure_ascii=False, indent=2))
        return 3
    if args.format == "json":
        print(json.dumps(report, ensure_ascii=False, indent=2))
    else:
        print(_render_text(report))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
