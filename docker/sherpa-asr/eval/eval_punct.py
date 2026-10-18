"""Punctuation/case restoration on FLEURS vi dev references: vibert-capu vs Gemma E4B."""
import json, os, re, sys, time, unicodedata, urllib.request
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, ".."))
from capu import Capu

MODELS = os.environ.get("ASR_MODELS_DIR", os.path.join(HERE, "..", "models"))
FLEURS = os.environ.get("FLEURS_DIR", "fleurs")
GEMMA_URL = os.environ.get("GEMMA_URL", "http://10.10.0.240:8001/v1/chat/completions")

N = int(next((a for a in sys.argv[1:] if a.isdigit()), 300))
rows, seen = [], set()
for line in open(f"{FLEURS}/dev.tsv", encoding="utf-8"):
    c = line.rstrip("\n").split("\t")
    if c[0] not in seen:
        seen.add(c[0]); rows.append(unicodedata.normalize("NFC", c[2]))
rows = rows[:N]

def marks(text):
    """words (lower) and {(word index, mark)} and set of capitalised indices"""
    toks = re.findall(r"[^\s,.:?!;\"“”()]+|[,.:?!;]", text)
    words, punct, caps = [], set(), set()
    for t in toks:
        if t in ",.:?!;":
            if words: punct.add((len(words) - 1, "." if t in ".!;" else t))
        else:
            if t[:1].isupper(): caps.add(len(words))
            words.append(t.lower())
    return words, punct, caps

def gemma(text):
    body = {"model": "gemma-4-e4b-it", "temperature": 0, "max_tokens": 400,
            "chat_template_kwargs": {"enable_thinking": False},
            "messages": [{"role": "user", "content":
              "Thêm dấu câu (, . : ?) và viết hoa đúng chính tả cho đoạn văn bản tiếng Việt do máy nhận dạng giọng nói tạo ra. "
              "Không thêm, bỏ hay sửa bất kỳ từ nào. Chỉ trả về đoạn văn đã sửa.\n\n" + text}]}
    req = urllib.request.Request(GEMMA_URL, json.dumps(body).encode(),
                                 {"Content-Type": "application/json"})
    return json.load(urllib.request.urlopen(req, timeout=60))["choices"][0]["message"]["content"].strip()

def score(name, fn):
    tp = {",": [0, 0, 0], ".": [0, 0, 0], "cap": [0, 0, 0]}  # tp, fp, fn
    changed = 0; t0 = time.time(); ex = []
    for ref in rows:
        rw, rp, rc = marks(ref)
        out = fn(" ".join(rw))
        hw, hp, hc = marks(out)
        if hw != rw:
            changed += 1
            continue
        for k in (",", "."):
            r = {p for p in rp if p[1] == k}; h = {p for p in hp if p[1] == k}
            tp[k][0] += len(r & h); tp[k][1] += len(h - r); tp[k][2] += len(r - h)
        tp["cap"][0] += len(rc & hc); tp["cap"][1] += len(hc - rc); tp["cap"][2] += len(rc - hc)
        if len(ex) < 4: ex.append((ref, out))
    el = time.time() - t0
    f = lambda a: (2 * a[0] / (2 * a[0] + a[1] + a[2])) if a[0] else 0.0
    pr = lambda a: f"P {a[0]/max(1,a[0]+a[1]):.2f} R {a[0]/max(1,a[0]+a[2]):.2f} F1 {f(a):.2f}"
    print(f"\n== {name}: {len(rows)} sentences, {el/len(rows)*1000:.0f} ms/sentence, words changed in {changed}")
    for k in (",", ".", "cap"): print(f"  {k:4s} {pr(tp[k])}")
    for r, o in ex: print("   REF:", r, "\n   OUT:", o)

c = Capu(f"{MODELS}/vibert-capu")
score("vibert-capu int8", c.restore)
if "--gemma" in sys.argv:
    score("gemma-4-e4b", gemma)
