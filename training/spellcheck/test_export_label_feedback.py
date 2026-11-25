"""python3 -m unittest test_export_label_feedback (from training/spellcheck)."""
from __future__ import annotations

import json
import os
import tempfile
import unittest

from export_label_feedback import build_rows, main

P = "Ủy ban ban hành quyết đinh số 5 về quyết đinh khác."


def read(path: str) -> str:
    with open(path, encoding="utf-8") as f:
        return f.read()


def label(**kw) -> dict:
    base = {
        "kind": "spelling", "paragraph": P, "span_start": None, "span_end": None,
        "flagged_text": "đinh", "suggestion": "định", "verdict": "not_error", "source": "user",
        "review_verdict": "", "review_correction": "", "created_at": "2026-11-20T10:00:00+07:00",
    }
    base.update(kw)
    return base


class BuildRows(unittest.TestCase):
    def test_reviewed_error_uses_span(self):
        # the second "đinh": code points 41..45
        start = P.index("đinh", P.index("đinh") + 1)
        rows = build_rows([label(review_verdict="error", review_correction="định", span_start=start, span_end=start + 4)])
        self.assertEqual(len(rows), 1)
        r = rows[0]
        self.assertEqual(r["noisy"], P)
        self.assertEqual(r["text"], "Ủy ban ban hành quyết đinh số 5 về quyết định khác.")
        self.assertEqual(r["findings"], [{"wrong": "đinh", "correct": "định"}])
        self.assertEqual(r["src"], "label:error")

    def test_reviewed_not_error_and_skip(self):
        rows = build_rows([label(review_verdict="not_error"), label(paragraph="Khác đinh.", review_verdict="skip")])
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]["findings"], [])
        self.assertEqual(rows[0]["text"], P)
        self.assertEqual(rows[0]["protect"], ["đinh"])
        self.assertEqual(rows[0]["src"], "label:not_error")

    def test_unreviewed_needs_flag(self):
        labels = [
            label(verdict="accepted"),
            label(paragraph="Ban hành quyết đinh.", verdict="fixed", user_correction="định", suggestion="đỉnh"),
            label(paragraph="Mô hình nói đinh.", verdict="not_error", source="verifier"),
        ]
        self.assertEqual(build_rows(labels), [])
        rows = build_rows(labels, include_unreviewed=True)
        self.assertEqual(sorted(r["src"] for r in rows), ["label:accepted", "label:fixed"])
        fixed = next(r for r in rows if r["src"] == "label:fixed")
        self.assertEqual(fixed["findings"], [{"wrong": "đinh", "correct": "định"}])
        self.assertEqual(len(build_rows(labels, include_unreviewed=True, include_verifier=True)), 3)

    def test_review_wins_over_user_verdict(self):
        rows = build_rows([label(verdict="accepted", review_verdict="not_error")], include_unreviewed=True)
        self.assertEqual(rows[0]["findings"], [])

    def test_skips_format_unlocatable_and_dedupes(self):
        rows = build_rows([
            label(kind="format", review_verdict="error", review_correction="x"),
            label(flagged_text="không có", review_verdict="error", review_correction="x"),
            label(review_verdict="error", review_correction="đinh"),  # correction == wrong
            {"_end": True, "count": 3},
        ])
        self.assertEqual(rows, [])
        same = label(review_verdict="error", review_correction="định")
        self.assertEqual(len(build_rows([same, dict(same, created_at="2026-11-21T10:00:00+07:00")])), 1)

    def test_two_findings_merged_in_one_paragraph(self):
        rows = build_rows([
            label(flagged_text="Ủy", review_verdict="error", review_correction="Uỷ", span_start=0, span_end=2),
            label(review_verdict="error", review_correction="định"),
        ])
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]["text"], "Uỷ ban ban hành quyết định số 5 về quyết đinh khác.")
        self.assertEqual([f["wrong"] for f in rows[0]["findings"]], ["Ủy", "đinh"])


class Main(unittest.TestCase):
    def test_holdout_and_protect(self):
        with tempfile.TemporaryDirectory() as d:
            src = os.path.join(d, "labels.jsonl")
            with open(src, "w", encoding="utf-8") as f:
                for i in range(3):
                    f.write(json.dumps(label(paragraph=f"Câu {i} có đinh.", review_verdict="error", review_correction="định",
                                             created_at=f"2026-11-2{i}T10:00:00+07:00"), ensure_ascii=False) + "\n")
                f.write(json.dumps(label(paragraph="Tên riêng Đinh Lăng.", flagged_text="Đinh Lăng", review_verdict="not_error"),
                                   ensure_ascii=False) + "\n")
                f.write('{"_end": true}\n')
            out, held, prot = (os.path.join(d, n) for n in ("pairs.jsonl", "eval.jsonl", "protect.txt"))
            self.assertEqual(main([src, "--out", out, "--holdout", "1", "--holdout-out", held, "--protect-out", prot]), 0)
            rows = [json.loads(l) for l in read(out).splitlines()]
            ev = [json.loads(l) for l in read(held).splitlines()]
            self.assertEqual(len(rows), 3)
            self.assertEqual([r["noisy"] for r in ev], ["Câu 2 có đinh."])
            self.assertNotIn("_at", rows[0])
            self.assertEqual(read(prot).split(), ["lăng", "đinh"])


if __name__ == "__main__":
    unittest.main()
