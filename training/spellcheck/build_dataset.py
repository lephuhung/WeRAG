"""Assemble SFT samples in the exact shape the check_spelling tool uses.

Each sample is a chat: the tool's system prompt (read from
config/prompt_templates/spellcheck_review.yaml so it never drifts), a user
message "Đoạn cần kiểm tra:\n[i] text\n..." and the assistant JSON
{"findings":[{"paragraph":i,"wrong":...,"correct":...,"reason":...}]}.

Paragraph sources (each JSONL row has "text" = clean paragraph):
  --kb      llm_correct.py output: rows with findings are real (orig, findings)
            pairs, rows without are clean paragraphs for the noiser
  --pairs   sanng_filter.py output: rows with "noisy" + "findings"
  --clean   any JSONL of clean paragraphs ("text"), noised on the fly

Mixing is by paragraph slot with --mix weights; a share of slots stays
clean and a share of batches is entirely clean so the model learns
{"findings":[]}.
"""
from __future__ import annotations

import argparse
import json
import random
import re
import sys
from pathlib import Path

import yaml

from noiser import PROTECTED_WORDS, inject, set_op_weights, set_telex_kind_weights, set_vocab
from vi_text import split_sentences, syllable_count, vietnamese_ratio

# Full form → short forms used in chat queries. The short form replaces the
# phrase in some query samples and is never flagged, so the model learns that
# an abbreviation is not a typo.
ABBREVIATIONS = {
    "ủy ban nhân dân": ["UBND", "ubnd"],
    "hội đồng nhân dân": ["HĐND", "hđnd"],
    "thủ tục hành chính": ["TTHC", "tthc"],
    "bảo hiểm xã hội": ["BHXH", "bhxh"],
    "bảo hiểm y tế": ["BHYT", "bhyt"],
    "căn cước công dân": ["CCCD", "cccd"],
    "giấy phép lái xe": ["GPLX", "gplx"],
    "công nghệ thông tin": ["CNTT", "cntt"],
    "an toàn thông tin": ["ATTT", "attt"],
    "văn bản quy phạm pháp luật": ["VBQPPL", "vbqppl"],
    "bí mật nhà nước": ["BMNN", "bmnn"],
    "nghiên cứu khoa học": ["NCKH", "nckh"],
    "kinh tế xã hội": ["KTXH", "ktxh"],
    "kinh tế - xã hội": ["KTXH", "ktxh"],
    "cán bộ công chức": ["CBCC", "cbcc"],
    "cán bộ, công chức": ["CBCC", "cbcc"],
    "tài nguyên và môi trường": ["TNMT", "tnmt"],
    "giáo dục và đào tạo": ["GDĐT", "gdđt", "GD&ĐT"],
    "nghị định": ["NĐ", "nđ"],
    "thông tư": ["TT", "tt"],
    "quyết định": ["QĐ", "qđ"],
    "cơ sở dữ liệu": ["CSDL", "csdl"],
    "phòng cháy chữa cháy": ["PCCC", "pccc"],
    "chính phủ": ["CP"],
    "thành phố": ["TP", "tp"],
}


def abbreviate_query(q: str, rng: random.Random) -> str:
    """Replace one or two full forms with a short form (both cases)."""
    low = q.lower()
    hits = [(full, shorts) for full, shorts in ABBREVIATIONS.items() if full in low]
    rng.shuffle(hits)
    for full, shorts in hits[:2]:
        i = low.find(full)
        q = q[:i] + rng.choice(shorts) + q[i + len(full) :]
        low = q.lower()
    return q

ROOT = Path(__file__).resolve().parents[2]
PROMPT_YAML = ROOT / "config" / "prompt_templates" / "spellcheck_review.yaml"


def load_system_prompt() -> str:
    doc = yaml.safe_load(PROMPT_YAML.read_text(encoding="utf-8"))
    for t in doc["templates"]:
        if t.get("id") == "spellcheck_review":
            return t["content"].strip()
    raise SystemExit("spellcheck_review template not found")


def read_jsonl(path: str) -> list[dict]:
    return [json.loads(l) for l in open(path, encoding="utf-8") if l.strip()]


def group_sentences(sentences: list[str], rng: random.Random) -> list[str]:
    """Group consecutive sentences into paragraphs of 1..3 sentences."""
    out, i = [], 0
    while i < len(sentences):
        k = rng.choices([1, 2, 3], weights=[45, 35, 20])[0]
        par = " ".join(sentences[i : i + k])
        i += k
        if 8 <= syllable_count(par) <= 90 and vietnamese_ratio(par) >= 0.9:
            out.append(par)
    return out


class Pools:
    def __init__(self, a, rng: random.Random):
        self.rng = rng
        self.kb_clean: list[str] = []
        self.kb_pairs: list[dict] = []
        self.pairs: list[dict] = []
        self.clean: list[str] = []
        self.queries: list[str] = []
        for p in a.queries or []:
            self.queries.extend(r["text"] for r in read_jsonl(p) if r.get("text"))
        if a.kb:
            rows = read_jsonl(a.kb)
            if a.kb_holdout:
                unchanged = [r for r in rows if not r["findings"]]
                changed = [r for r in rows if r["findings"]]
                hold_u = set(id(r) for r in unchanged[-a.kb_holdout :])
                hold_c = set(id(r) for r in changed[-a.kb_holdout // 2 :])
                rows = [r for r in rows if id(r) not in hold_u and id(r) not in hold_c]
            for r in rows:
                if r["findings"]:
                    self.kb_pairs.append({"noisy": r["orig"], "findings": r["findings"]})
                self.kb_clean.append(r["text"])
        for p in a.pairs or []:
            self.pairs.extend(r for r in read_jsonl(p) if r.get("findings"))
            self.clean.extend(r["text"] for r in read_jsonl(p) if not r.get("findings"))
        for p in a.clean or []:
            for r in read_jsonl(p):
                self.clean.append(r["text"])
        self.kb_max_uses = a.kb_max_uses
        self.uses: dict = {}
        print(
            f"pools: kb_clean={len(self.kb_clean)} kb_pairs={len(self.kb_pairs)} pairs={len(self.pairs)} clean={len(self.clean)} queries={len(self.queries)}",
            file=sys.stderr,
        )

    def _take(self, key, cap: int) -> bool:
        """Count one use of key; past --kb-max-uses the item leaves its pool.
        Without a cap, 312 real pairs repeated ~30x each and one OCR error
        ("Chuyển viên" in 50 job-title rows) appeared 1,717 times in sft_v3."""
        if not cap:
            return True
        n = self.uses.get(key, 0)
        if n >= cap:
            return False
        self.uses[key] = n + 1
        return True

    def _draw_kb_pair(self):
        while self.kb_pairs:
            k = self.rng.randrange(len(self.kb_pairs))
            r = self.kb_pairs[k]
            errs = [("err", f["wrong"].lower(), f["correct"].lower()) for f in r["findings"]]
            if self._take(("pair", r["noisy"]), self.kb_max_uses) and all(self._take(e, self.kb_max_uses * 10) for e in errs):
                return r
            self.kb_pairs.pop(k)
        return None

    def _draw_kb_clean(self):
        while self.kb_clean:
            k = self.rng.randrange(len(self.kb_clean))
            if self._take(("clean", self.kb_clean[k]), self.kb_max_uses):
                return self.kb_clean[k]
            self.kb_clean.pop(k)
        return None

    def draw(self, source: str, want_clean: bool) -> tuple[str, list[dict]]:
        rng = self.rng
        if source == "kb":
            if not want_clean and self.kb_pairs and rng.random() < 0.35:
                r = self._draw_kb_pair()
                if r:
                    return r["noisy"], r["findings"]
            text = self._draw_kb_clean()
            if text is None:  # KB pool used up: fall back to the clean pool
                text = rng.choice(self.clean)
        elif source == "pairs":
            if want_clean:
                text = rng.choice(self.clean) if self.clean else rng.choice(self.pairs)["text"]
                return text, []
            r = rng.choice(self.pairs)
            return r["noisy"], r["findings"]
        else:
            text = rng.choice(self.clean)
        if want_clean:
            return text, []
        noisy, findings = inject(text, rng)
        if not findings:
            return text, []
        return noisy, findings


def build_query_sample(pools: Pools, a, rng: random.Random, system: str) -> dict:
    """A batch of 1..6 short chat questions; errors are keyboard-heavy, some
    questions carry abbreviations (never flagged), some batches are clean."""
    n = rng.choices([1, 2, 3, 4, 5, 6], weights=[25, 20, 20, 15, 10, 10])[0]
    start = rng.randint(0, 400)
    all_clean = rng.random() < a.clean_batch_rate
    lines, findings = [], []
    for k in range(n):
        q = rng.choice(pools.queries)
        if rng.random() < a.query_abbr_rate:
            q = abbreviate_query(q, rng)
        want_clean = all_clean or rng.random() < a.clean_slot_rate
        fs: list[dict] = []
        if not want_clean:
            noisy, fs = inject(q, rng, n_errors=rng.choices([1, 1, 2], weights=[70, 0, 30])[0])
            if fs:
                q = noisy
        idx = start + k
        lines.append(f"[{idx}] {q}")
        for f in fs:
            findings.append({"paragraph": idx, "wrong": f["wrong"], "correct": f["correct"], "reason": f.get("reason", "lỗi chính tả")})
    user = "Đoạn cần kiểm tra:\n" + "\n".join(lines) + "\n"
    answer = json.dumps({"findings": findings}, ensure_ascii=False, separators=(",", ":"))
    return {"messages": [{"role": "system", "content": system}, {"role": "user", "content": user}, {"role": "assistant", "content": answer}]}


def build_sample(pools: Pools, a, rng: random.Random, system: str) -> dict:
    if pools.queries and rng.random() < a.query_rate:
        return build_query_sample(pools, a, rng, system)
    n = rng.choices([rng.randint(2, 6), rng.randint(7, 12), rng.randint(13, 25)], weights=[30, 45, 25])[0]
    start = rng.randint(0, 400)
    all_clean = rng.random() < a.clean_batch_rate
    sources = ["kb", "pairs", "clean"]
    weights = [a.mix[0] if (pools.kb_clean or pools.kb_pairs) else 0, a.mix[1] if (pools.pairs or pools.clean) else 0, a.mix[2] if pools.clean else 0]
    lines, findings = [], []
    for k in range(n):
        src = rng.choices(sources, weights=weights)[0]
        want_clean = all_clean or rng.random() < a.clean_slot_rate
        text, fs = pools.draw(src, want_clean)
        idx = start + k
        lines.append(f"[{idx}] {text}")
        for f in fs:
            findings.append({"paragraph": idx, "wrong": f["wrong"], "correct": f["correct"], "reason": f.get("reason", "lỗi chính tả")})
    user = "Đoạn cần kiểm tra:\n" + "\n".join(lines) + "\n"
    answer = json.dumps({"findings": findings}, ensure_ascii=False, separators=(",", ":"))
    return {"messages": [{"role": "system", "content": system}, {"role": "user", "content": user}, {"role": "assistant", "content": answer}]}


def sentences_from_parquet(path: str, column: str, limit: int, rng: random.Random) -> list[str]:
    import pyarrow.parquet as pq

    t = pq.read_table(path, columns=[column])
    vals = [v for v in t.column(column).to_pylist() if v]
    rng.shuffle(vals)
    out = []
    seen = set()
    for v in vals:
        v = re.sub(r"\s+", " ", v).strip()
        key = v.lower()
        if key in seen or len(v) < 20:
            continue
        seen.add(key)
        out.append(v)
        if len(out) >= limit:
            break
    return out


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--kb")
    ap.add_argument("--kb-holdout", type=int, default=200, help="last N unchanged and N/2 changed KB rows are reserved for eval")
    ap.add_argument("--kb-max-uses", type=int, default=0, help="cap on draws of one KB paragraph or pair (0 = no cap); one real error (wrong, correct) is capped at 10x this")
    ap.add_argument("--pairs", action="append")
    ap.add_argument("--clean", action="append")
    ap.add_argument("--queries", action="append", help="JSONL of short chat questions (\"text\")")
    ap.add_argument("--query-rate", type=float, default=0.0, help="share of samples that are query batches")
    ap.add_argument("--query-abbr-rate", type=float, default=0.35, help="share of query lines that get an abbreviation substituted")
    ap.add_argument("--telex-weight", type=float, help="noiser weight for telex_vni (default 3)")
    ap.add_argument("--typo-weight", type=float, help="noiser weight for typo (default 12)")
    ap.add_argument("--raw-extra-weight", type=float, help="telex_vni weight of raw keys with a doubled key, cascc (default 15)")
    ap.add_argument("--tone-mid-rate", type=float, help="share of raw Telex syllables with the tone key right after the vowel, casc (default 0.3)")
    ap.add_argument("--protect-words", help="file with one lower-case word per line the noiser must not corrupt")
    ap.add_argument("--clean-parquet", action="append", default=[], help="path:column, clean sentences to group into paragraphs")
    ap.add_argument("--clean-parquet-limit", type=int, default=60000)
    ap.add_argument("--n", type=int, default=20000)
    ap.add_argument("--val", type=int, default=400)
    ap.add_argument("--mix", type=float, nargs=3, default=[0.25, 0.45, 0.30], help="paragraph-slot weights: kb, pairs, clean")
    ap.add_argument("--clean-slot-rate", type=float, default=0.40)
    ap.add_argument("--clean-batch-rate", type=float, default=0.12)
    ap.add_argument("--seed", type=int, default=0)
    ap.add_argument("--out", required=True)
    a = ap.parse_args()
    rng = random.Random(a.seed)
    system = load_system_prompt()
    overrides = {}
    if a.telex_weight is not None:
        overrides["telex_vni"] = a.telex_weight
    if a.typo_weight is not None:
        overrides["typo"] = a.typo_weight
    if overrides:
        set_op_weights(**overrides)
    if a.raw_extra_weight is not None:
        set_telex_kind_weights(raw_extra=a.raw_extra_weight)
    if a.tone_mid_rate is not None:
        set_telex_kind_weights(tone_mid_rate=a.tone_mid_rate)
    for shorts in ABBREVIATIONS.values():
        PROTECTED_WORDS.update(x.lower() for x in shorts)
    if a.protect_words:
        PROTECTED_WORDS.update(w.strip().lower() for w in open(a.protect_words, encoding="utf-8") if w.strip())
    pools = Pools(a, rng)
    for spec in a.clean_parquet:
        path, col = spec.rsplit(":", 1)
        sents = sentences_from_parquet(path, col, a.clean_parquet_limit, rng)
        pools.clean.extend(group_sentences(sents, rng))
    print(f"clean paragraphs total={len(pools.clean)}", file=sys.stderr)
    counts: dict[str, int] = {}
    for text in pools.kb_clean + pools.clean + [r["text"] for r in pools.pairs]:
        for tok in text.split():
            core = re.sub(r"^\W+|\W+$", "", tok).lower()
            if core.isalpha():
                counts[core] = counts.get(core, 0) + 1
    set_vocab(w for w, c in counts.items() if c >= 3)
    print(f"syllable vocab={sum(1 for c in counts.values() if c >= 3)}", file=sys.stderr)
    out = Path(a.out)
    out.mkdir(parents=True, exist_ok=True)
    stats = {"samples": 0, "paragraphs": 0, "findings": 0, "clean_paragraphs": 0, "empty_answers": 0}
    with open(out / "train.jsonl", "w", encoding="utf-8") as ftr, open(out / "val.jsonl", "w", encoding="utf-8") as fva:
        for i in range(a.n + a.val):
            s = build_sample(pools, a, rng, system)
            ans = json.loads(s["messages"][2]["content"])["findings"]
            n_par = s["messages"][1]["content"].count("\n[")
            stats["samples"] += 1
            stats["paragraphs"] += n_par
            stats["findings"] += len(ans)
            stats["clean_paragraphs"] += n_par - len({f["paragraph"] for f in ans})
            stats["empty_answers"] += not ans
            (fva if i < a.val else ftr).write(json.dumps(s, ensure_ascii=False) + "\n")
    print(json.dumps(stats), file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
