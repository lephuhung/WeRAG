"""Turn the reviewed spelling labels of the label page ("Nhãn chính tả và
thể thức") into training pairs for build_dataset.py --pairs.

Input: the JSONL export of the label page, one DocumentLabel per line (the
last line {"_end": true, ...} is skipped). Fields used: kind, paragraph,
span_start/span_end (code points, end exclusive), flagged_text,
suggestion, verdict, source, review_verdict, review_correction,
user_correction, created_at.

Which label says what (format rows are skipped):

  review_verdict error      flagged_text -> review_correction (a finding)
  review_verdict not_error  the span is no mistake (negative)
  review_verdict skip       dropped
  not reviewed              dropped, unless --include-unreviewed:
    verdict accepted        flagged_text -> suggestion (the user typed it)
    verdict fixed           flagged_text -> user_correction
    verdict not_error       negative when source is user; verifier rows
                            (a model's verdict, weaker) only with
                            --include-verifier

Labels on the same paragraph are merged into one row
{"text": <paragraph with every finding's span replaced>, "noisy":
<paragraph>, "findings": [{"wrong", "correct"}], "src": "label:<verdicts>",
"protect": [<spans said to be no mistake>]}. A paragraph with negatives
only becomes {"text": p, "noisy": p, "findings": [], "src":
"label:not_error", "protect": [...]}.

Caveat: a negative says "this span is not an error", not "the paragraph is
clean", but build_dataset.py puts every row without findings in its clean
pool (and may noise it) and ignores "protect". Audit the negative rows
before training, and pass --protect-out so their words can go to
build_dataset.py --protect-words (the noiser then leaves them alone).

Export: on the label page filter Trạng thái duyệt = Đã duyệt, then Xuất
JSONL (or use the API: GET /api/v1/document-labels/export?kind=spelling&
review=reviewed). Then:

  python3 -I export_label_feedback.py labels.jsonl --out label_pairs.jsonl \\
      --holdout 50 --holdout-out label_eval.jsonl --protect-out label_protect.txt
  python3 build_dataset.py ... --pairs label_pairs.jsonl --protect-words label_protect.txt
"""
from __future__ import annotations

import argparse
import json
import sys
from collections import Counter

MAX_CORRECTION = 255


def locate(paragraph: str, flagged: str, start, end) -> tuple[int, int] | None:
    """The flagged span: span_start/span_end (code points; Python str
    indexes are code points too) when they hold flagged_text, else the
    first occurrence of flagged_text; None when neither works."""
    if not flagged or not paragraph:
        return None
    if isinstance(start, int) and isinstance(end, int) and 0 <= start < end <= len(paragraph):
        if paragraph[start:end] == flagged:
            return start, end
    at = paragraph.find(flagged)
    return (at, at + len(flagged)) if at >= 0 else None


def judge(label: dict, include_unreviewed: bool, include_verifier: bool) -> tuple[str, str] | None:
    """('error', correct) | ('not_error', '') | None (unusable) and the
    verdict name for src."""
    review = (label.get("review_verdict") or "").strip()
    flagged = label.get("flagged_text") or ""
    if review == "error":
        correct = (label.get("review_correction") or "").strip()
        return ("error", correct) if correct and correct != flagged else None
    if review == "not_error":
        return ("not_error", "")
    if review:  # skip, or anything newer
        return None
    if not include_unreviewed:
        return None
    verdict = label.get("verdict") or ""
    if verdict == "accepted":
        correct = (label.get("suggestion") or "").strip()
    elif verdict == "fixed":
        correct = (label.get("user_correction") or "").strip()
    elif verdict == "not_error":
        if label.get("source") == "verifier" and not include_verifier:
            return None
        return ("not_error", "")
    else:
        return None
    return ("error", correct) if correct and correct != flagged else None


def src_name(label: dict, kind: str) -> str:
    """error (reviewed), accepted, fixed, not_error or verifier."""
    if (label.get("review_verdict") or "").strip():
        return kind
    if kind == "not_error" and label.get("source") == "verifier":
        return "verifier"
    return label.get("verdict") or kind


def build_rows(labels: list[dict], include_unreviewed: bool = False, include_verifier: bool = False) -> list[dict]:
    """Rows for build_dataset.py --pairs, oldest paragraph first (by the
    newest label on it); each row also carries "_at" (dropped on write)."""
    groups: dict[str, dict] = {}
    # newest first: the latest label of a span decides it
    labels = sorted(labels, key=lambda l: l.get("created_at") or "", reverse=True)
    for label in labels:
        if label.get("_end") or label.get("kind") != "spelling":
            continue
        paragraph = label.get("paragraph") or ""
        flagged = label.get("flagged_text") or ""
        j = judge(label, include_unreviewed, include_verifier)
        if j is None:
            continue
        span = locate(paragraph, flagged, label.get("span_start"), label.get("span_end"))
        if span is None:
            continue
        kind, correct = j
        if kind == "error" and ("\n" in correct or len(correct) > MAX_CORRECTION):
            continue
        g = groups.setdefault(paragraph, {"spans": {}, "at": ""})
        g["at"] = max(g["at"], label.get("created_at") or "")
        g["spans"].setdefault(span, (kind, flagged, correct, src_name(label, kind)))
    rows: list[dict] = []
    seen: set[str] = set()
    for paragraph, g in groups.items():
        text, findings, srcs, last = paragraph, [], set(), None
        errors = [(sp, d) for sp, d in g["spans"].items() if d[0] == "error"]
        # replace right to left so earlier offsets stay valid; overlapping spans are dropped
        for (s, e), (_, wrong, correct, src) in sorted(errors, reverse=True):
            if last is not None and e > last:
                continue
            text = text[:s] + correct + text[e:]
            findings.insert(0, {"wrong": wrong, "correct": correct})
            srcs.add(src)
            last = s
        protect: list[str] = []
        for kind, flagged, _, _ in g["spans"].values():
            if kind == "not_error" and flagged not in protect and not any(f["wrong"] == flagged for f in findings):
                protect.append(flagged)
        if not findings and not protect:
            continue
        key = json.dumps([paragraph, findings], ensure_ascii=False)
        if key in seen:
            continue
        seen.add(key)
        srcs = sorted(srcs) if findings else ["not_error"]
        row = {"text": text, "noisy": paragraph, "findings": findings, "src": "label:" + "+".join(srcs)}
        if protect:
            row["protect"] = protect
        row["_at"] = g["at"]
        rows.append(row)
    rows.sort(key=lambda r: r["_at"])
    return rows


def read_labels(path: str) -> list[dict]:
    out = []
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if line:
                out.append(json.loads(line))
    return out


def write_rows(path: str, rows: list[dict]) -> None:
    with open(path, "w", encoding="utf-8") as f:
        for r in rows:
            f.write(json.dumps({k: v for k, v in r.items() if k != "_at"}, ensure_ascii=False) + "\n")


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("labels", help="JSONL export of the label page")
    ap.add_argument("--out", required=True, help="pairs for build_dataset.py --pairs")
    ap.add_argument("--include-unreviewed", action="store_true",
                    help="also use unreviewed user labels (accepted, fixed, not_error)")
    ap.add_argument("--include-verifier", action="store_true",
                    help="with --include-unreviewed: verifier model rejections become negatives too")
    ap.add_argument("--holdout", type=int, default=0, help="the newest N rows go to --holdout-out (evaluation)")
    ap.add_argument("--holdout-out", help="file for the --holdout rows (default: <out>.holdout.jsonl)")
    ap.add_argument("--protect-out", help="write the negatives' words (lower case) for build_dataset.py --protect-words")
    a = ap.parse_args(argv)
    rows = build_rows(read_labels(a.labels), a.include_unreviewed, a.include_verifier)
    held: list[dict] = []
    if a.holdout > 0:
        held, rows = rows[-a.holdout:], rows[:-a.holdout]
        write_rows(a.holdout_out or a.out.removesuffix(".jsonl") + ".holdout.jsonl", held)
    write_rows(a.out, rows)
    if a.protect_out:
        words = sorted({w.lower() for r in rows for p in r.get("protect", []) for w in p.split() if w.strip()})
        with open(a.protect_out, "w", encoding="utf-8") as f:
            f.writelines(w + "\n" for w in words)
    counts = Counter(r["src"] for r in rows + held)
    for src, n in sorted(counts.items()):
        print(f"{src}\t{n}", file=sys.stderr)
    print(f"rows={len(rows)} holdout={len(held)} with_findings={sum(bool(r['findings']) for r in rows + held)}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
