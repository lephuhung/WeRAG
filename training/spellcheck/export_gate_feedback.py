"""Turn the chat gate's answered "unclear word" questions into training
pairs for build_dataset.py --pairs.

The gate asks about a word it cannot read (cascc: abbreviation / you
mistyped "các" / keep / "did you mean <rewritten question>" / other) and
stores every answer in abbreviation_typo_feedback. Here each question with
a usable answer becomes one row {"text", "noisy", "findings", "src"}:

  typo         the word → the user's correction
  rewrite      every word that differs between the question and the
               rewrite the user picked (same word count only)
  keep         the word is right as typed: no finding for it (a negative
               example, e.g. a product name)
  abbreviation the word is an abbreviation: no finding for it

"other" answers carry no label and are skipped. The latest answer per
question wins.

  psql "$DSN" -Atc "select row_to_json(f) from abbreviation_typo_feedback f order by id" > feedback.jsonl
  python export_gate_feedback.py feedback.jsonl --out gate_pairs.jsonl
"""
from __future__ import annotations

import argparse
import json
import re
import sys

WORD = re.compile(r"[\w]+", re.UNICODE)


def words(s: str) -> list[str]:
    return WORD.findall(s)


def apply_word(text: str, wrong: str, correct: str) -> str:
    return re.sub(rf"(?<!\w){re.escape(wrong)}(?!\w)", correct, text, flags=re.IGNORECASE)


def to_pair(rows: list[dict]) -> dict | None:
    """rows: the answers about one question (same request), oldest first."""
    noisy = rows[0]["original_query"]
    clean, findings = noisy, []
    for r in rows:
        choice, word, answer = r["choice"], r["word"], (r.get("answer") or "").strip()
        if choice == "typo" and answer and answer.lower() != word.lower():
            # "full form (SHORT)" renders of a dictionary short form keep the short form only
            m = re.fullmatch(r".+\(([^()]+)\)", answer)
            correct = m.group(1) if m else answer
            clean = apply_word(clean, word, correct)
            findings.append({"wrong": word, "correct": correct, "reason": "lỗi gõ (người dùng xác nhận)"})
        elif choice == "rewrite" and answer:
            a, b = words(noisy), words(answer)
            if len(a) != len(b):
                continue
            for w, c in zip(a, b):
                if w != c and not any(f["wrong"] == w for f in findings):
                    findings.append({"wrong": w, "correct": c, "reason": "lỗi gõ (người dùng chọn câu sửa)"})
            clean = answer
    if not findings and not any(r["choice"] in ("keep", "abbreviation") for r in rows):
        return None
    return {"text": clean, "noisy": noisy, "findings": findings, "src": "gate_feedback"}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("feedback", help="JSONL rows of abbreviation_typo_feedback")
    ap.add_argument("--out", required=True)
    a = ap.parse_args()
    by_request: dict[str, list[dict]] = {}
    for line in open(a.feedback, encoding="utf-8"):
        if line.strip():
            r = json.loads(line)
            by_request.setdefault(r.get("request_id") or str(r["id"]), []).append(r)
    n = labelled = 0
    with open(a.out, "w", encoding="utf-8") as f:
        for rows in by_request.values():
            latest: dict[str, dict] = {}
            for r in rows:
                latest[r["word"].lower()] = r
            pair = to_pair(list(latest.values()))
            if pair:
                f.write(json.dumps(pair, ensure_ascii=False) + "\n")
                n += 1
                labelled += bool(pair["findings"])
    print(f"questions={len(by_request)} pairs={n} with_findings={labelled}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
