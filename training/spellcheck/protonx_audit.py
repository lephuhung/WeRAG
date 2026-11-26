"""Audit protonx_filter.py candidates with a judge LLM (Qwen 35B) and emit
training pairs (build_dataset.py --pairs shape) for the findings it confirms.

Resumable: verdicts are appended to --verdicts as they arrive; rerunning skips
findings already judged.

  python protonx_audit.py cand.jsonl --api http://10.10.0.240:8000 --model-name Qwen/Qwen3.6-35B-A3B-AWQ \
      --verdicts verdicts.jsonl --pairs pairs.jsonl
"""
from __future__ import annotations

import argparse
import concurrent.futures as cf
import json
import re
import sys
import urllib.request

JUDGE_PROMPT = """Bạn là người soát chính tả văn bản hành chính, pháp luật tiếng Việt. Một công cụ tự động đã đánh dấu MỘT từ trong câu là lỗi chính tả và đề xuất cách sửa. Hãy đánh giá riêng đề xuất đó.

Trả lời "đúng" khi từ bị đánh dấu thật sự viết sai (gõ nhầm, sai hoặc thiếu dấu thành một từ không hợp nghĩa trong câu) VÀ cách sửa là từ đúng.
Trả lời "sai" khi từ bị đánh dấu vốn đã đúng và hợp nghĩa trong câu, hoặc khi cách sửa không phải từ cần dùng.
Trả lời "không rõ" khi câu bị cắt, lộn xộn (mảnh bảng biểu, danh mục) khiến không thể kết luận.
Không coi là lỗi: biến thể dấu (hoà/hòa, thuỷ/thủy), i/y (lí/lý), tên riêng, từ viết tắt, cách diễn đạt khác.

Trả lời DUY NHẤT một JSON: {"verdict": "đúng"} hoặc {"verdict": "sai"} hoặc {"verdict": "không rõ"}"""

VERDICT_RE = re.compile(r'"verdict"\s*:\s*"([^"]+)"')


def fkey(r: dict, f: dict) -> str:
    return f"{r['p']}:{r['c']}:{f['wrong']}>{f['correct']}"


def judge(api: str, model: str, api_key: str, sentence: str, wrong: str, correct: str) -> str:
    body = {
        "model": model,
        "temperature": 0.0,
        "max_tokens": 30,
        "chat_template_kwargs": {"enable_thinking": False},
        "messages": [
            {"role": "system", "content": JUDGE_PROMPT},
            {"role": "user", "content": f'Câu: "{sentence}"\nTừ bị đánh dấu: "{wrong}"\nĐề xuất sửa thành: "{correct}"'},
        ],
    }
    req = urllib.request.Request(api.rstrip("/") + "/v1/chat/completions", json.dumps(body).encode(), {"Content-Type": "application/json", "Authorization": f"Bearer {api_key or 'x'}"})
    try:
        txt = json.load(urllib.request.urlopen(req, timeout=120))["choices"][0]["message"]["content"]
    except Exception as e:  # noqa: BLE001
        return f"error:{type(e).__name__}"
    m = VERDICT_RE.search(txt)
    if not m:
        return "unparsed"
    v = m.group(1).strip().lower()
    return "yes" if v == "đúng" else "no" if v == "sai" else "unclear"


def apply(src: str, finds: list[dict]) -> str:
    out = src
    for f in finds:
        out = re.sub(r"(?<!\w)" + re.escape(f["wrong"]) + r"(?!\w)", f["correct"], out, count=1)
    return out


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("candidates")
    ap.add_argument("--api", required=True, help="base URL without /v1")
    ap.add_argument("--model-name", required=True)
    ap.add_argument("--api-key", default="")
    ap.add_argument("--workers", type=int, default=8)
    ap.add_argument("--limit", type=int, default=0)
    ap.add_argument("--verdicts", required=True, help="append-only JSONL of {key, verdict}")
    ap.add_argument("--pairs", required=True, help="confirmed findings as training pairs")
    a = ap.parse_args()
    rows = [json.loads(l) for l in open(a.candidates, encoding="utf-8")]
    if a.limit:
        rows = rows[: a.limit]
    done: dict[str, str] = {}
    try:
        for l in open(a.verdicts, encoding="utf-8"):
            d = json.loads(l)
            if not d["verdict"].startswith("error"):
                done[d["key"]] = d["verdict"]
    except FileNotFoundError:
        pass
    todo = [(r, f) for r in rows for f in r["findings"] if fkey(r, f) not in done]
    print(f"findings={sum(len(r['findings']) for r in rows)} judged={len(done)} todo={len(todo)}", file=sys.stderr)
    with open(a.verdicts, "a", encoding="utf-8") as fo, cf.ThreadPoolExecutor(a.workers) as ex:
        futs = {ex.submit(judge, a.api, a.model_name, a.api_key, r["src"], f["wrong"], f["correct"]): (r, f) for r, f in todo}
        n = 0
        for fut in cf.as_completed(futs):
            r, f = futs[fut]
            v = fut.result()
            done[fkey(r, f)] = v
            fo.write(json.dumps({"key": fkey(r, f), "verdict": v}, ensure_ascii=False) + "\n")
            n += 1
            if n % 500 == 0:
                fo.flush()
                print(f"  {n}/{len(todo)}", file=sys.stderr)
    counts: dict[str, int] = {}
    n_pairs = 0
    with open(a.pairs, "w", encoding="utf-8") as fo:
        for r in rows:
            keep = []
            for f in r["findings"]:
                v = done.get(fkey(r, f), "missing")
                counts[v] = counts.get(v, 0) + 1
                if v == "yes":
                    keep.append(f)
            if not keep:
                continue
            fixed = apply(r["src"], keep)
            if fixed == r["src"]:
                continue
            n_pairs += 1
            fo.write(json.dumps({
                "noisy": r["src"],
                "text": fixed,
                "findings": [{"wrong": f["wrong"], "correct": f["correct"], "reason": "sai dấu" if f["type"] == "diacritics" else "gõ nhầm"} for f in keep],
                "src": "vbpl_protonx",
            }, ensure_ascii=False) + "\n")
    print(f"verdicts={counts} pairs={n_pairs}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
