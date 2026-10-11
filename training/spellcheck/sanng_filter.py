"""Filter and re-weight Sanng1112/vietnamese-spelling-synthetic-1gb rows.

The dataset is token-aligned (noisy_tokens, clean_tokens, detection_labels,
error_types). Rows are converted into paragraph records with findings:
  {"text": clean, "noisy": noisy, "findings": [{wrong, correct, reason}], "src": "sanng"}

Filters:
  - 0..max_errors errors per row; rows with zero errors are kept at --clean-rate;
  - no error on a protected token (capitalised, digits, codes, foreign letters);
  - rows whose errors are all keyboard/telex/vni/transpose/delete are kept with
    probability --typing-keep (they dominate the source, real text is mostly
    tone/diacritic errors);
  - abbreviation (teencode) errors are dropped: they do not occur in
    administrative documents.

Input may be a parquet file (pyarrow), the original JSONL, or "-" for JSONL
on stdin, e.g. streamed with curl so the 6 GB train file never hits disk:

  curl -sL -H "Authorization: Bearer $HF_TOKEN" \
    https://huggingface.co/datasets/Sanng1112/vietnamese-spelling-synthetic-1gb/resolve/main/train_full.jsonl \
    | python sanng_filter.py - --out sanng_train_filtered.jsonl --limit 150000
"""
from __future__ import annotations

import argparse
import json
import random
import sys

from vi_text import accepted_variant, protected_token, split_token

REASONS = {
    "keyboard": "gõ nhầm",
    "telex": "lỗi gõ Telex/VNI",
    "vni": "lỗi gõ Telex/VNI",
    "delete": "gõ nhầm",
    "transpose": "gõ nhầm",
    "drop_diacritic": "thiếu dấu",
    "drop_tone": "thiếu dấu thanh",
    "consonant": "sai phụ âm",
    "swap_tone": "sai dấu hỏi/ngã",
}
TYPING = {"keyboard", "telex", "vni", "delete", "transpose"}


def detok(tokens: list[str]) -> str:
    out = " ".join(tokens)
    for p in ",.;:!?)":
        out = out.replace(" " + p, p)
    out = out.replace("( ", "(")
    return out


def iter_rows(path: str):
    if path.endswith(".parquet"):
        import pyarrow.parquet as pq

        pf = pq.ParquetFile(path)
        cols = ["noisy_tokens", "clean_tokens", "detection_labels", "error_types"]
        for batch in pf.iter_batches(batch_size=4096, columns=cols):
            for row in batch.to_pylist():
                yield row
    elif path == "-":
        for line in sys.stdin:
            if line.strip():
                try:
                    yield json.loads(line)
                except json.JSONDecodeError:
                    continue  # a truncated last line when streaming a byte range
    else:
        for line in open(path, encoding="utf-8"):
            yield json.loads(line)


def convert(row: dict, rng: random.Random, a) -> dict | None:
    nt, ct, lab, et = row["noisy_tokens"], row["clean_tokens"], row["detection_labels"], row["error_types"]
    if len(nt) != len(ct) or len(ct) < a.min_tokens or len(ct) > a.max_tokens:
        return None
    errs = [i for i, l in enumerate(lab) if l]
    if len(errs) > a.max_errors:
        return None
    if not errs:
        return None if rng.random() > a.clean_rate else {"text": detok(ct), "noisy": detok(ct), "findings": [], "src": "sanng"}
    types = {et[i] for i in errs}
    if "abbreviation" in types:
        return None
    if types <= TYPING and rng.random() > a.typing_keep:
        return None
    findings = []
    for i in errs:
        lead, core, trail = split_token(ct[i])
        if protected_token(core, i == 0 or (i > 0 and ct[i - 1].endswith((".", "!", "?")))):
            return None
        if nt[i] == ct[i] or accepted_variant(split_token(nt[i])[1], core):
            return None
        findings.append({"i": i, "wrong": nt[i], "correct": ct[i], "reason": REASONS.get(et[i], "lỗi chính tả")})
    noisy = detok(nt)
    out = []
    for f in findings:
        wrong = f["wrong"]
        if noisy.count(wrong) != 1:
            i = f["i"]
            lo = max(0, i - 1)
            wrong = " ".join(nt[lo : i + 1])
            if noisy.count(wrong) != 1:
                return None
            f["correct"] = " ".join(nt[lo:i] + [f["correct"]])
        out.append({"wrong": wrong, "correct": f["correct"], "reason": f["reason"]})
    return {"text": detok(ct), "noisy": noisy, "findings": out, "src": "sanng"}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("inp", nargs="+")
    ap.add_argument("--out", required=True)
    ap.add_argument("--limit", type=int, default=150000)
    ap.add_argument("--max-errors", type=int, default=3)
    ap.add_argument("--min-tokens", type=int, default=8)
    ap.add_argument("--max-tokens", type=int, default=80)
    ap.add_argument("--clean-rate", type=float, default=0.6)
    ap.add_argument("--typing-keep", type=float, default=0.45)
    ap.add_argument("--seed", type=int, default=0)
    ap.add_argument("--skip-rows", type=int, default=0, help="skip the first N raw rows (continue a previous streaming pass)")
    a = ap.parse_args()
    rng = random.Random(a.seed)
    n_in = n_out = 0
    with open(a.out, "w", encoding="utf-8") as fo:
        for path in a.inp:
            for row in iter_rows(path):
                n_in += 1
                if n_in <= a.skip_rows:
                    continue
                rec = convert(row, rng, a)
                if rec is None:
                    continue
                fo.write(json.dumps(rec, ensure_ascii=False) + "\n")
                n_out += 1
                if n_out >= a.limit:
                    break
            if n_out >= a.limit:
                break
    print(f"rows_in={n_in} rows_out={n_out}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
