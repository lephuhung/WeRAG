"""Generate short administrative chat questions from clean KB paragraphs with
the chat LLM (OpenAI-compatible). Output JSONL rows {"text": question, "src": "gen"}.

  python gen_queries.py kb_corrected.jsonl --out queries_gen.jsonl --n 2000
"""
from __future__ import annotations

import argparse, json, os, random, re, sys, time, urllib.request
from concurrent.futures import ThreadPoolExecutor

PROMPT = """Dưới đây là một đoạn văn bản hành chính/pháp luật. Hãy viết {k} câu hỏi NGẮN (5-18 từ) mà một cán bộ hoặc người dân có thể gõ vào ô chat để hỏi về nội dung liên quan. Yêu cầu:
- câu hỏi tự nhiên, đa dạng kiểu: hỏi số lượng, thời hạn, thủ tục, cơ quan, mức phí, điều kiện, "còn hiệu lực không", "quy định ở đâu";
- viết thường, không cần dấu chấm hỏi ở mọi câu, giống người gõ nhanh;
- một vài câu dùng viết tắt hành chính quen thuộc (UBND, HĐND, TTHC, BHXH, CCCD, GPLX, CNTT, ATTT, VBQPPL, NĐ-CP...) ở dạng viết hoa hoặc viết thường;
- không bịa tên riêng, không chép nguyên câu trong đoạn.
Trả lời bằng JSON: {{"questions": ["...", "..."]}}

Đoạn văn:
{par}"""


def ask(base, model, key, par, k, rng):
    body = {"model": model, "messages": [{"role": "user", "content": PROMPT.format(k=k, par=par)}],
            "temperature": 0.9, "max_tokens": 600, "chat_template_kwargs": {"enable_thinking": False}}
    req = urllib.request.Request(base.rstrip("/") + "/chat/completions", data=json.dumps(body).encode(),
                                 headers={"Content-Type": "application/json", "Authorization": "Bearer " + key})
    for attempt in range(3):
        try:
            raw = urllib.request.urlopen(req, timeout=120).read()
            txt = json.loads(raw)["choices"][0]["message"]["content"]
            m = re.search(r"\{.*\}", txt, re.S)
            qs = json.loads(m.group())["questions"]
            return [re.sub(r"\s+", " ", q).strip() for q in qs if isinstance(q, str) and 4 <= len(q.split()) <= 22]
        except Exception as e:  # noqa: BLE001
            time.sleep(2 * (attempt + 1))
    return []


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("kb")
    ap.add_argument("--out", required=True)
    ap.add_argument("--n", type=int, default=2000)
    ap.add_argument("--per", type=int, default=4)
    ap.add_argument("--workers", type=int, default=6)
    ap.add_argument("--base", default=os.environ.get("LLM_BASE_URL", "http://10.10.0.240:8000/v1"))
    ap.add_argument("--model", default=os.environ.get("LLM_MODEL_NAME", "Qwen/Qwen3.6-35B-A3B-FP8"))
    ap.add_argument("--api-key", default=os.environ.get("LLM_API_KEY", ""))
    ap.add_argument("--seed", type=int, default=0)
    a = ap.parse_args()
    rng = random.Random(a.seed)
    pars = [json.loads(l)["text"] for l in open(a.kb, encoding="utf-8") if l.strip()]
    pars = [p for p in pars if 25 <= len(p.split()) <= 160 and not p.isupper()]
    rng.shuffle(pars)
    need = a.n // a.per + 50
    seen, out, done = set(), [], 0
    with ThreadPoolExecutor(a.workers) as ex, open(a.out, "w", encoding="utf-8") as f:
        for qs in ex.map(lambda p: ask(a.base, a.model, a.api_key, p, a.per, rng), pars[:need]):
            done += 1
            for q in qs:
                key = q.lower()
                if key in seen:
                    continue
                seen.add(key)
                f.write(json.dumps({"text": q, "src": "gen"}, ensure_ascii=False) + "\n")
                out.append(q)
            if done % 25 == 0:
                f.flush()
                print(f"{done} paragraphs → {len(out)} questions", file=sys.stderr)
            if len(out) >= a.n:
                break
    print(f"total {len(out)}", file=sys.stderr)


if __name__ == "__main__":
    main()
