"""Score a model on the eval set from build_eval.py.

Two backends:
  --api BASE --model-name NAME     any OpenAI-compatible endpoint (vLLM)
  --model PATH [--adapter PATH]    local transformers model, optional LoRA

Metrics (per part and overall):
  detect P/R/F1    predicted finding overlaps a gold span in that paragraph
  correct P/R/F1   ... and the correction matches (punctuation-insensitive)
  clean_fp_rate    share of clean paragraphs that received any finding
  bad_json         share of requests whose reply was not the JSON contract
Replies are validated the way the Go tool does: paragraph in batch, "wrong"
occurs in that paragraph, "correct" differs.
"""
from __future__ import annotations

import argparse
import concurrent.futures as cf
import json
import re
import sys
import time
import urllib.request

FENCE = re.compile(r"^```(?:json)?\s*|\s*```$")


def parse_reply(text: str) -> list[dict] | None:
    t = FENCE.sub("", text.strip())

    def attempt(s: str):
        try:
            obj = json.loads(s)
        except Exception:
            return None
        if isinstance(obj, dict) and "findings" in obj:
            return obj["findings"] or []
        if isinstance(obj, list):
            return obj
        return None

    out = attempt(t)
    if out is not None:
        return out
    for o, c in (("{", "}"), ("[", "]")):
        s, e = t.find(o), t.rfind(c)
        if s >= 0 and e > s:
            out = attempt(t[s : e + 1])
            if out is not None:
                return out
    return None


def norm(s: str) -> str:
    return re.sub(r"[\s\.,;:!?\"'“”‘’()]+", "", s).lower()


def spans(paragraph: str, wrong: str) -> list[tuple[int, int]]:
    out, start = [], 0
    while True:
        i = paragraph.find(wrong, start)
        if i < 0:
            return out
        out.append((i, i + len(wrong)))
        start = i + 1


def validate(raw: list, paragraphs: dict) -> list[dict]:
    good = []
    for f in raw or []:
        if not isinstance(f, dict):
            continue
        try:
            idx = int(str(f.get("paragraph", "")).strip("[] \"'"))
        except ValueError:
            continue
        p = paragraphs.get(str(idx))
        wrong, correct = str(f.get("wrong", "")).strip(), str(f.get("correct", "")).strip()
        if p is None or not wrong or norm(wrong) == norm(correct) or wrong not in p or len(wrong) > 200:
            continue
        good.append({"paragraph": idx, "wrong": wrong, "correct": correct})
    return good


def score(pred: list[dict], gold: list[dict], paragraphs: dict, clean: list[int]) -> dict:
    gold_spans = []
    for g in gold:
        p = paragraphs[str(g["paragraph"])]
        gs = spans(p, g["wrong"])
        gold_spans.append((g["paragraph"], gs[0] if gs else (-1, -1), norm(g["correct"]), norm(g["wrong"])))
    used = set()
    tp_d = tp_c = 0
    for f in pred:
        p = paragraphs[str(f["paragraph"])]
        fs = spans(p, f["wrong"])[0]
        hit = None
        for k, (gi, (a, b), gc, gw) in enumerate(gold_spans):
            if k in used or gi != f["paragraph"]:
                continue
            if fs[0] < b and a < fs[1]:
                hit = k
                break
        if hit is None:
            continue
        used.add(hit)
        tp_d += 1
        gi, _, gc, gw = gold_spans[hit]
        # the prediction may cover a wider span; compare the corrected paragraph text instead
        fixed_pred = norm(p.replace(f["wrong"], f["correct"], 1))
        g = gold[hit]
        fixed_gold = norm(p.replace(g["wrong"], g["correct"], 1))
        if fixed_pred == fixed_gold:
            tp_c += 1
    clean_hit = len({f["paragraph"] for f in pred if f["paragraph"] in set(clean)})
    return {"tp_d": tp_d, "tp_c": tp_c, "n_pred": len(pred), "n_gold": len(gold), "clean": len(clean), "clean_hit": clean_hit}


def prf(tp: int, n_pred: int, n_gold: int) -> tuple[float, float, float]:
    p = tp / n_pred if n_pred else 0.0
    r = tp / n_gold if n_gold else 0.0
    f = 2 * p * r / (p + r) if p + r else 0.0
    return round(p, 3), round(r, 3), round(f, 3)


class ApiBackend:
    def __init__(self, base: str, model: str, api_key: str, workers: int):
        self.base, self.model, self.key, self.workers = base.rstrip("/"), model, api_key, workers

    def one(self, messages: list[dict]) -> str:
        body = {"model": self.model, "messages": messages, "temperature": 0.01, "max_tokens": 4096, "chat_template_kwargs": {"enable_thinking": False}}
        headers = {"Content-Type": "application/json"}
        if self.key:
            headers["Authorization"] = "Bearer " + self.key
        req = urllib.request.Request(self.base + "/v1/chat/completions", data=json.dumps(body).encode(), headers=headers)
        for attempt in range(3):
            try:
                with urllib.request.urlopen(req, timeout=300) as r:
                    return json.load(r)["choices"][0]["message"]["content"]
            except Exception as e:  # noqa: BLE001
                err = e
                time.sleep(2 * (attempt + 1))
        return "ERROR: " + str(err)

    def run(self, reqs: list[dict]) -> list[str]:
        with cf.ThreadPoolExecutor(self.workers) as ex:
            return list(ex.map(lambda r: self.one(r["messages"]), reqs))


class LocalBackend:
    def __init__(self, path: str, adapter: str | None, batch: int, max_new: int):
        import torch
        from transformers import AutoModelForCausalLM, AutoTokenizer

        self.tok = AutoTokenizer.from_pretrained(path)
        self.tok.padding_side = "left"
        try:
            self.model = AutoModelForCausalLM.from_pretrained(path, dtype=torch.bfloat16).to("cuda")
        except Exception as e:  # multimodal checkpoints load through the image-text class
            print(f"AutoModelForCausalLM failed ({type(e).__name__}); using AutoModelForImageTextToText", file=sys.stderr)
            from transformers import AutoModelForImageTextToText

            self.model = AutoModelForImageTextToText.from_pretrained(path, dtype=torch.bfloat16).to("cuda")
        if adapter:
            from peft import PeftModel

            self.model = PeftModel.from_pretrained(self.model, adapter)
            self.model = self.model.merge_and_unload()
        self.model.eval()
        self.batch, self.max_new = batch, max_new

    def run(self, reqs: list[dict]) -> list[str]:
        import torch

        prompts = [self.tok.apply_chat_template(r["messages"], tokenize=False, add_generation_prompt=True, enable_thinking=False) for r in reqs]
        order = sorted(range(len(prompts)), key=lambda i: len(prompts[i]))
        out = [""] * len(prompts)
        for s in range(0, len(order), self.batch):
            ids = order[s : s + self.batch]
            enc = self.tok([prompts[i] for i in ids], return_tensors="pt", padding=True).to("cuda")
            with torch.no_grad():
                gen = self.model.generate(**enc, max_new_tokens=self.max_new, do_sample=False, pad_token_id=self.tok.pad_token_id)
            for j, i in enumerate(ids):
                out[i] = self.tok.decode(gen[j, enc["input_ids"].shape[1] :], skip_special_tokens=True).strip()
        return out


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("eval_jsonl")
    ap.add_argument("--api")
    ap.add_argument("--model-name", default="")
    ap.add_argument("--api-key", default="")
    ap.add_argument("--workers", type=int, default=8)
    ap.add_argument("--model")
    ap.add_argument("--adapter")
    ap.add_argument("--batch", type=int, default=8)
    ap.add_argument("--max-new", type=int, default=1024)
    ap.add_argument("--limit", type=int, default=0)
    ap.add_argument("--dump", help="write replies and per-request scores here")
    a = ap.parse_args()
    reqs = [json.loads(l) for l in open(a.eval_jsonl, encoding="utf-8")]
    if a.limit:
        reqs = reqs[: a.limit]
    backend = ApiBackend(a.api, a.model_name, a.api_key, a.workers) if a.api else LocalBackend(a.model, a.adapter, a.batch, a.max_new)
    t0 = time.time()
    replies = backend.run(reqs)
    dt = time.time() - t0
    agg: dict[str, dict] = {}
    dump = []
    for r, text in zip(reqs, replies):
        raw = parse_reply(text)
        pred = validate(raw, r["paragraphs"]) if raw is not None else []
        sc = score(pred, r["gold"], r["paragraphs"], r["clean"])
        sc["bad_json"] = int(raw is None)
        sc["n"] = 1
        for part in (r["part"], "ALL"):
            d = agg.setdefault(part, {})
            for k, v in sc.items():
                d[k] = d.get(k, 0) + v
        dump.append({"part": r["part"], "reply": text, "pred": pred, "gold": r["gold"], **sc})
    name = a.model_name or (a.adapter or a.model)
    print(f"model={name} requests={len(reqs)} sec={dt:.0f}")
    print(f"{'part':9s} {'req':>4s} {'detP':>6s} {'detR':>6s} {'detF1':>6s} {'corP':>6s} {'corR':>6s} {'corF1':>6s} {'cleanFP':>8s} {'badJSON':>8s}")
    for part in sorted(agg, key=lambda x: (x == "ALL", x)):
        d = agg[part]
        dp, dr, df = prf(d["tp_d"], d["n_pred"], d["n_gold"])
        cp, cr, cfv = prf(d["tp_c"], d["n_pred"], d["n_gold"])
        fp = f"{d['clean_hit'] / d['clean']:.3f}" if d["clean"] else "-"
        print(f"{part:9s} {d['n']:4d} {dp:6.3f} {dr:6.3f} {df:6.3f} {cp:6.3f} {cr:6.3f} {cfv:6.3f} {fp:>8s} {d['bad_json'] / d['n']:8.3f}")
    if a.dump:
        with open(a.dump, "w", encoding="utf-8") as fo:
            for d in dump:
                fo.write(json.dumps(d, ensure_ascii=False) + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
