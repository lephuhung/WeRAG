"""Correct OCR/spelling errors in in-domain paragraphs with a large chat
model, keeping only conservative edits.

For every paragraph the model returns a corrected paragraph. The syllable
diff is accepted only when each edit is a 1:1 syllable substitution that
changes tone/diacritics, a known consonant confusion, or a small typo, and is
not an accepted variant (hoà/hòa, lí/lý, capitalisation). Paragraphs with
any other edit are dropped (their cleanliness is unknown).

Output JSONL rows:
  {"text": corrected, "src": "kb", "orig": original, "findings": [...]}
`findings` is empty when the model left the paragraph unchanged; such rows
double as clean paragraphs and as real (noisy, clean) pairs otherwise.
"""
from __future__ import annotations

import argparse
import concurrent.futures as cf
import difflib
import json
import os
import re
import sys
import time
import urllib.request

from noiser import FINALS, INITIALS
from vi_text import accepted_variant, base_letters, is_vietnamese_syllable, split_token, strip_tone

SYSTEM = (
    "Bạn là người hiệu đính văn bản hành chính, pháp luật tiếng Việt được nhận dạng từ bản quét (OCR). "
    "Nhiệm vụ: sửa lỗi chính tả do OCR hoặc gõ nhầm: sai dấu thanh, sai dấu mũ/móc, sai hoặc thiếu âm tiết, lặp từ. "
    "KHÔNG thay đổi cách diễn đạt, từ ngữ, dấu câu, viết hoa, số liệu, ký hiệu văn bản hay thứ tự từ. "
    "KHÔNG sửa biến thể chấp nhận được (hoà/hòa, thuỷ/thủy, lí/lý, kĩ/kỹ). "
    "Nếu đoạn không có lỗi, trả lại nguyên văn. "
    "Đầu vào là các đoạn đánh số [n]. Trả lời bằng đúng các đoạn đó, mỗi đoạn một dòng, giữ nguyên số thứ tự [n], không giải thích."
)


def call(base: str, model: str, paras: list[str], api_key: str = "") -> list[str] | None:
    user = "\n".join(f"[{i}] {p}" for i, p in enumerate(paras))
    body = {
        "model": model,
        "messages": [{"role": "system", "content": SYSTEM}, {"role": "user", "content": user}],
        "temperature": 0,
        "max_tokens": 4096,
        "chat_template_kwargs": {"enable_thinking": False},
    }
    headers = {"Content-Type": "application/json"}
    if api_key:
        headers["Authorization"] = "Bearer " + api_key
    req = urllib.request.Request(base.rstrip("/") + "/v1/chat/completions", data=json.dumps(body).encode(), headers=headers)
    for attempt in range(3):
        try:
            with urllib.request.urlopen(req, timeout=180) as r:
                text = json.load(r)["choices"][0]["message"]["content"]
            break
        except Exception:
            time.sleep(2 * (attempt + 1))
    else:
        return None
    out = [None] * len(paras)
    for line in text.splitlines():
        m = re.match(r"^\s*\[(\d+)\]\s*(.*)$", line)
        if m and int(m.group(1)) < len(paras):
            out[int(m.group(1))] = m.group(2).strip()
    return out


def _confusable(a: str, b: str) -> bool:
    """Same syllable up to tone/diacritics, a listed consonant confusion, or
    an edit distance of at most 2 on the base letters."""
    la, lb = a.lower(), b.lower()
    if base_letters(la) == base_letters(lb):
        return True
    ba, bb = base_letters(la), base_letters(lb)
    for x, y in INITIALS:
        for p, q in ((x, y), (y, x)):
            if ba.startswith(p) and bb.startswith(q) and ba[len(p):] == bb[len(q):]:
                return True
    for x, y in FINALS:
        for p, q in ((x, y), (y, x)):
            if ba.endswith(p) and bb.endswith(q) and ba[: -len(p)] == bb[: -len(q)]:
                return True
    return difflib.SequenceMatcher(None, ba, bb).ratio() >= 0.75 and abs(len(ba) - len(bb)) <= 2


def conservative_diff(orig: str, fixed: str) -> list[dict] | None:
    """Findings for accepted edits, or None when any edit is not conservative."""
    a, b = orig.split(), fixed.split()
    findings = []
    for tag, i1, i2, j1, j2 in difflib.SequenceMatcher(None, a, b, autojunk=False).get_opcodes():
        if tag == "equal":
            continue
        if tag == "replace" and i2 - i1 == j2 - j1:
            for x, y in zip(a[i1:i2], b[j1:j2]):
                lx, cx, tx = split_token(x)
                ly, cy, ty = split_token(y)
                if lx != ly or tx != ty:
                    return None
                if not cx or not cy or accepted_variant(cx, cy):
                    return None
                if not is_vietnamese_syllable(cy) or not _confusable(cx, cy):
                    return None
                findings.append({"wrong": x, "correct": y})
            continue
        if tag == "delete" and i2 - i1 == 1 and i1 > 0 and a[i1 - 1] == a[i1]:
            findings.append({"wrong": a[i1 - 1] + " " + a[i1], "correct": a[i1], "reason": "lặp từ"})
            continue
        if tag == "replace" and i2 - i1 == 2 and j2 - j1 == 1 and "".join(a[i1:i2]) == b[j1]:
            findings.append({"wrong": " ".join(a[i1:i2]), "correct": b[j1], "reason": "dư dấu cách"})
            continue
        if tag == "replace" and i2 - i1 == 1 and j2 - j1 == 2 and a[i1] == "".join(b[j1:j2]):
            findings.append({"wrong": a[i1], "correct": " ".join(b[j1:j2]), "reason": "thiếu dấu cách"})
            continue
        return None
    for f in findings:
        if "reason" not in f:
            w, c = split_token(f["wrong"])[1], split_token(f["correct"])[1]
            if base_letters(w) == base_letters(c):
                f["reason"] = "thiếu dấu thanh" if strip_tone(w) == w and strip_tone(c) != c else "sai dấu"
            else:
                f["reason"] = "gõ nhầm hoặc sai âm tiết"
        if orig.count(f["wrong"]) != 1:
            return None  # ambiguous span; keep the data simple
    return findings


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("inp")
    ap.add_argument("out")
    ap.add_argument("--base", default=os.environ.get("LLM_BASE_URL", "http://10.10.0.240:8000"))
    ap.add_argument("--model", default=os.environ.get("LLM_MODEL_NAME", "Qwen/Qwen3.6-35B-A3B-FP8"))
    ap.add_argument("--api-key", default=os.environ.get("LLM_API_KEY", ""))
    ap.add_argument("--batch", type=int, default=8)
    ap.add_argument("--workers", type=int, default=8)
    a = ap.parse_args()
    rows = [json.loads(l) for l in open(a.inp, encoding="utf-8")]
    batches = [rows[i : i + a.batch] for i in range(0, len(rows), a.batch)]
    stats = {"unchanged": 0, "accepted": 0, "rejected": 0, "failed": 0, "findings": 0}
    t0 = time.time()
    with cf.ThreadPoolExecutor(a.workers) as ex, open(a.out, "w", encoding="utf-8") as fo:
        for batch, fixed in zip(batches, ex.map(lambda b: call(a.base, a.model, [r["text"] for r in b], a.api_key), batches)):
            for r, f in zip(batch, fixed or [None] * len(batch)):
                if f is None:
                    stats["failed"] += 1
                    continue
                if f == r["text"]:
                    stats["unchanged"] += 1
                    fo.write(json.dumps({"text": r["text"], "src": r.get("src", "kb"), "orig": r["text"], "findings": []}, ensure_ascii=False) + "\n")
                    continue
                findings = conservative_diff(r["text"], f)
                if findings is None:
                    stats["rejected"] += 1
                    continue
                stats["accepted"] += 1
                stats["findings"] += len(findings)
                fo.write(json.dumps({"text": f, "src": r.get("src", "kb"), "orig": r["text"], "findings": findings}, ensure_ascii=False) + "\n")
    print(json.dumps(stats | {"sec": round(time.time() - t0)}), file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
