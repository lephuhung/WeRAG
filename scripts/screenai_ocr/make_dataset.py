"""Build SenOCR-Vi fine-tuning samples (page image + "OCR:" answer) with the
Screen AI server as labeller.

Pages are rendered exactly like docreader feeds SenOCR (pypdfium2, 200 dpi,
long edge ≤ 2000 px, JPEG 85), so training images match inference images.

Two kinds of page:

* scanned (text layer ≤ 50 chars): label = Screen AI. With --senocr-url and
  --counts, words the two readers disagree on are settled by corpus context
  (see ``arbitrate``); the page is ``auto`` when SenOCR saw no words Screen AI
  missed (--max-missing), else ``review``. Without SenOCR the fallback gate is
  every word's Screen AI confidence ≥ --min-word-conf (strict: few pages pass).
* born-digital (--born-digital): the PDF's text layer is ground truth. Screen AI
  gives the SenOCR-style layout, its words are replaced by the aligned text-
  layer words, and the image is degraded (--degrade variants) to look scanned.
  Status ``exact`` when alignment left ≤ --max-unaligned of the words, else
  ``review``.

    python make_dataset.py --senocr-url http://127.0.0.1:8001/v1 \\
        --counts data/vbpl_counts.pkl.gz --out data/scan1 /home/orca/data/vbpl/original_pdf/*.pdf
    python make_dataset.py --born-digital --degrade 2 --senocr-url http://127.0.0.1:8001/v1 \\
        --out data/bd1 some_digital.pdf

Output: <out>/images/*.jpg and <out>/dataset.jsonl, one record per image:
    {"id", "image", "messages": [{"role":"user","content":"<image>OCR:"},
     {"role":"assistant","content": label}], "meta": {...}}
Re-running with the same --out skips ids already written.
"""

from __future__ import annotations

import argparse
import base64
import collections
import gzip
import io
import json
import os
import pickle
import random
import re
import sys
import threading
import unicodedata
import zlib
from concurrent.futures import ThreadPoolExecutor

import numpy as np
import pypdfium2 as pdfium
import requests
from PIL import Image, ImageFilter
from rapidfuzz.distance import Levenshtein

RENDER_DPI = 200
RENDER_MAX_EDGE = 2000
JPEG_QUALITY = 85
SCANNED_MAX_CHARS = 50
IMAGE_EXTS = (".jpg", ".jpeg", ".png", ".bmp", ".tif", ".tiff", ".webp")

_write_lock = threading.Lock()


def render_page(page) -> Image.Image:
    w, h = page.get_size()
    scale = min(RENDER_DPI / 72, RENDER_MAX_EDGE / max(w, h))
    return page.render(scale=scale).to_pil().convert("RGB")


def jpeg_bytes(img: Image.Image, quality: int = JPEG_QUALITY) -> bytes:
    buf = io.BytesIO()
    img.save(buf, "JPEG", quality=quality)
    return buf.getvalue()


def degrade(img: Image.Image, seed: int) -> Image.Image:
    """Photocopy/scan look: grey, small skew, resolution loss, noise, blur."""
    r = random.Random(seed)
    g = img.convert("L").rotate(r.uniform(-1.0, 1.0), resample=Image.BICUBIC, fillcolor=255)
    f = r.uniform(0.5, 0.8)
    g = g.resize((int(g.width * f), int(g.height * f)), Image.BILINEAR).resize(g.size, Image.BILINEAR)
    a = np.asarray(g).astype(np.float32)
    a = a * r.uniform(0.85, 1.0) + r.uniform(0, 25)  # contrast / paper tone
    a += np.random.default_rng(seed).normal(0, r.uniform(4, 14), a.shape)
    g = Image.fromarray(np.clip(a, 0, 255).astype(np.uint8)).filter(ImageFilter.GaussianBlur(r.uniform(0.3, 0.8)))
    return g.convert("RGB")


def call_ocr(base_url: str, model: str, jpeg: bytes, api_key: str = "", extra: dict | None = None) -> dict:
    payload = {
        "model": model,
        "messages": [
            {
                "role": "user",
                "content": [
                    {"type": "image_url", "image_url": {"url": "data:image/jpeg;base64," + base64.b64encode(jpeg).decode()}},
                    {"type": "text", "text": "OCR:"},
                ],
            }
        ],
        "temperature": 0.0,
        "max_tokens": 4096,
        **(extra or {}),
    }
    headers = {"Authorization": f"Bearer {api_key}"} if api_key else {}
    r = requests.post(base_url.rstrip("/") + "/chat/completions", json=payload, headers=headers, timeout=300)
    r.raise_for_status()
    return r.json()


def norm_tokens(text: str) -> list[str]:
    return re.findall(r"\w+", unicodedata.normalize("NFC", text).lower())


def _strip_marks(s: str) -> str:
    s = unicodedata.normalize("NFD", s.replace("đ", "d").replace("Đ", "D"))
    return "".join(c for c in s if unicodedata.category(c) != "Mn").lower()


def correct_with_text_layer(label: str, truth: str) -> tuple[str, int, int]:
    """Replace Screen AI words with the aligned text-layer words.

    Keeps the label's line structure (SenOCR layout); only 1:1 substitutions
    that look like misreads of the same word are applied. Returns
    (corrected label, words changed, words left unaligned).
    """
    lines = label.split("\n")
    pos = [(li, m.start(), m.end()) for li, ln in enumerate(lines) for m in re.finditer(r"\S+", ln)]
    hyp = [lines[li][a:b] for li, a, b in pos]
    ref = re.findall(r"\S+", unicodedata.normalize("NFC", truth))
    fixes: dict[int, str] = {}
    unaligned = 0
    for op in Levenshtein.opcodes(hyp, ref):
        if op.tag == "equal":
            continue
        n_src, n_dst = op.src_end - op.src_start, op.dest_end - op.dest_start
        if op.tag == "replace" and n_src == n_dst:
            for k in range(n_src):
                h, t = hyp[op.src_start + k], ref[op.dest_start + k]
                if Levenshtein.normalized_similarity(_strip_marks(h), _strip_marks(t)) >= 0.5:
                    fixes[op.src_start + k] = t
                else:
                    unaligned += 1
        else:
            unaligned += max(n_src, n_dst)
    # Apply right-to-left so offsets stay valid within a line.
    for idx in sorted(fixes, reverse=True):
        li, a, b = pos[idx]
        lines[li] = lines[li][:a] + fixes[idx] + lines[li][b:]
    return "\n".join(lines), len(fixes), unaligned


def load_counts(path: str) -> tuple[dict, dict]:
    if not path:
        return {}, {}
    with gzip.open(path, "rb") as f:
        d = pickle.load(f)
    return d["uni"], d["bi"]


def _with_case(src: str, new: str) -> str:
    if src.isupper():
        return new.upper()
    if src[:1].isupper():
        return new[:1].upper() + new[1:]
    return new


def _is_loop(toks: list[str]) -> bool:
    return len(toks) >= 8 and len(set(toks)) / len(toks) < 0.3


def cut_loop(toks: list[str], max_n: int = 20, min_repeats: int = 4) -> tuple[list[str], bool]:
    """Cut a token list where an n-gram starts repeating back to back
    (SenOCR's degenerate tail: "đánh đánh đánh …", "thợ chịu hại của …")."""
    for start in range(len(toks)):
        for n in range(1, max_n + 1):
            gram = toks[start : start + n]
            if len(gram) < n:
                break
            reps = 1
            while toks[start + reps * n : start + (reps + 1) * n] == gram:
                reps += 1
            if reps >= min_repeats:
                return toks[:start], True
    return toks, False


def arbitrate(label: str, sen_text: str, uni: dict, bi: dict, ratio: float = 5.0):
    """Settle words where Screen AI (label) and SenOCR disagree.

    * 1:1 substitutions that are two readings of one word (similar once the
      diacritics are stripped) are scored on each side, P(w | prev) and
      P(next | w), from the corpus counts. SenOCR's reading replaces the label
      only when both sides prefer it and together ≥ ``ratio`` times; the label
      is confirmed the other way round; mixed evidence is a ``conflict``.
      Numbers are never settled this way.
    * Words both readers have but in a different place (reading order of
      headers and tables) cancel out — order, not text, differs.
    * Words only Screen AI has stay: it is a detector + recogniser and does not
      invent text (SenOCR drops table numbers, for instance).
    * Words only SenOCR has are ``missing`` — Screen AI may have skipped faint
      text. SenOCR's repetition loop is cut off first.

    On the 105/2025/QH15 eval (32 degraded pages, text layer as truth) the
    label was right in 93 % of the mixed-evidence ``conflicts`` and 41 of 43
    fixes were right, so conflicts keep the label and only ``missing`` words
    should block a page.

    Returns (label, info) — info: fixed, kept, moved, conflicts,
    conflict_idx (label word indexes), missing, senocr_loop.
    """
    spans = list(re.finditer(r"\w+", label))
    hyp = [unicodedata.normalize("NFC", m.group()).lower() for m in spans]
    sen, looped = cut_loop(norm_tokens(sen_text))
    fixed, kept, conflicts = [], [], []
    conflict_idx: list[int] = []
    edits: dict[int, str] = {}
    only_label: collections.Counter = collections.Counter()
    only_sen: collections.Counter = collections.Counter()

    total = sum(uni.values()) or 1

    def p_next(w: str, nxt: str) -> float:
        # Dirichlet-smoothed bigram, backing off to the unigram of ``nxt``.
        return (bi.get((w, nxt), 0) + DIRICHLET * uni.get(nxt, 0) / total) / (uni.get(w, 0) + DIRICHLET)

    # Pass 1: 1:1 pairs that are two readings of one word. Their alternative
    # readings also serve as context for their neighbours (a neighbour may be
    # misread too: "trưng câu" vs "trung cầu").
    pairs: list[tuple[int, str, str]] = []
    alts: dict[int, set] = {}
    for op in Levenshtein.opcodes(hyp, sen):
        if op.tag == "equal":
            continue
        a_toks, b_toks = hyp[op.src_start : op.src_end], sen[op.dest_start : op.dest_end]
        if op.tag != "replace" or len(a_toks) != len(b_toks):
            only_label.update(a_toks)
            if not _is_loop(b_toks):
                only_sen.update(b_toks)
            continue
        for k, (a, b) in enumerate(zip(a_toks, b_toks)):
            if a.isdigit() or b.isdigit() or (
                # Not two readings of one word: a misalignment, not a misread.
                Levenshtein.normalized_similarity(_strip_marks(a), _strip_marks(b)) < 0.6
            ):
                only_label[a] += 1
                only_sen[b] += 1
                continue
            pairs.append((op.src_start + k, a, b))
            alts[op.src_start + k] = {a, b}

    def sides(i: int, w: str) -> tuple[float, float]:
        """(P(w | prev), P(next | w)), best over the neighbours' readings."""
        prevs = alts.get(i - 1, {hyp[i - 1]}) if i > 0 else {None}
        nxts = alts.get(i + 1, {hyp[i + 1]}) if i + 1 < len(hyp) else {None}
        left = max(p_next(p, w) if p else uni.get(w, 0) / total for p in prevs)
        right = max(p_next(w, n) if n else 1.0 for n in nxts)
        return left, right

    # Screen AI misreads ~15x fewer words than SenOCR, so the label stands
    # unless BOTH sides of the context prefer SenOCR's reading.
    for i, a, b in pairs:
        (la, ra), (lb, rb) = sides(i, a), sides(i, b)
        if not (la and ra) and not (lb and rb):
            conflicts.append(f"{a}|{b}")
            conflict_idx.append(i)
            continue
        rl = lb / la if la else float("inf")
        rr = rb / ra if ra else float("inf")
        if rl >= 1 and rr >= 1 and rl * rr >= ratio:
            edits[i] = b
            fixed.append(f"{a}→{b}")
        elif rl <= 1 and rr <= 1 and rl * rr <= 1 / ratio:
            kept.append(f"{a}≠{b}")
        else:
            conflicts.append(f"{a}|{b}")
            conflict_idx.append(i)
    moved = sum((only_label & only_sen).values())
    missing = list((only_sen - only_label).elements())
    for i in sorted(edits, reverse=True):
        m = spans[i]
        label = label[: m.start()] + _with_case(m.group(), edits[i]) + label[m.end() :]
    return label, {
        "fixed": fixed,
        "kept": kept,
        "moved": moved,
        "conflicts": conflicts,
        "conflict_idx": conflict_idx,
        "missing": missing,
        "senocr_loop": looped,
    }


DIRICHLET = 100.0

_pdfium_lock = threading.Lock()  # pdfium is not thread-safe (docreader does the same)


def load_page(path: str, page_no: int) -> Image.Image:
    if not path.lower().endswith(".pdf"):
        return Image.open(path).convert("RGB")
    with _pdfium_lock:
        pdf = pdfium.PdfDocument(path)
        try:
            return render_page(pdf[page_no - 1])
        finally:
            pdf.close()


def iter_pages(paths: list[str]):
    for path in paths:
        name = os.path.splitext(os.path.basename(path))[0]
        if path.lower().endswith(".pdf"):
            pdf = pdfium.PdfDocument(path)
            for i in range(len(pdf)):
                page = pdf[i]
                tp = page.get_textpage()
                text = tp.get_text_range()
                tp.close()
                page.close()
                yield name, i + 1, path, text
            pdf.close()
        elif path.lower().endswith(IMAGE_EXTS):
            yield name, 1, path, ""


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("inputs", nargs="+", help="PDF or image files")
    ap.add_argument("--out", required=True)
    ap.add_argument("--server", default="http://127.0.0.1:8004/v1", help="Screen AI OpenAI-compatible base URL")
    ap.add_argument("--server-key", default=os.environ.get("SCREENAI_API_KEY", ""))
    ap.add_argument("--senocr-url", default="", help="optional SenOCR base URL to record its answer")
    ap.add_argument("--senocr-model", default="senocr-vi")
    ap.add_argument("--born-digital", action="store_true", help="use pages WITH a text layer (ground truth) instead of scanned pages")
    ap.add_argument("--degrade", type=int, default=1, help="degraded variants per born-digital page (0 = clean render only)")
    ap.add_argument("--keep-clean", action="store_true", help="born-digital: also keep the clean render")
    ap.add_argument("--min-word-conf", type=float, default=0.95)
    ap.add_argument("--counts", default="", help="corpus counts from build_corpus_counts.py (settles Screen AI/SenOCR disagreements)")
    ap.add_argument("--max-missing", type=int, default=0, help="scanned + --senocr-url: auto only if SenOCR saw ≤ this many words Screen AI did not")
    ap.add_argument("--max-conflicts", type=int, default=-1, help="also cap words left on mixed evidence (-1 = no cap; the label is right ~93%% there)")
    ap.add_argument("--max-unaligned", type=float, default=0.02)
    ap.add_argument("--max-pages", type=int, default=0, help="stop after this many source pages (0 = all)")
    ap.add_argument("--workers", type=int, default=4)
    args = ap.parse_args()
    uni, bi = load_counts(args.counts)

    img_dir = os.path.join(args.out, "images")
    os.makedirs(img_dir, exist_ok=True)
    out_path = os.path.join(args.out, "dataset.jsonl")
    done = set()
    if os.path.exists(out_path):
        with open(out_path) as f:
            done = {json.loads(l)["id"] for l in f if l.strip()}

    jobs = []
    for name, page_no, path, text in iter_pages(args.inputs):
        has_text = len(text.strip()) > SCANNED_MAX_CHARS
        if args.born_digital != has_text:
            continue
        base_id = f"{name}_p{page_no}"
        if args.born_digital:
            tags = ["clean"] if args.keep_clean or args.degrade == 0 else []
            tags += [f"deg{k}" for k in range(args.degrade)]
        else:
            tags = ["scan"]
        for tag in tags:
            sid = f"{base_id}_{tag}"
            if sid not in done:
                jobs.append((sid, path, page_no, tag, text))
        if args.max_pages and len({(j[1], j[2]) for j in jobs}) >= args.max_pages:
            break

    stats = {"written": 0, "failed": 0}

    def run(job):
        sid, path, page_no, tag, text = job
        img = load_page(path, page_no)
        if tag.startswith("deg"):
            img = degrade(img, zlib.crc32(sid.encode()))
        jpeg = jpeg_bytes(img)
        resp = call_ocr(args.server, "screenai-ocr", jpeg, args.server_key, {"screenai_return_lines": True})
        label = resp["choices"][0]["message"]["content"]
        words = [w for ln in resp.get("screenai", {}).get("lines", []) for w in ln.get("words", [])]
        meta = {
            "source": path,
            "page": page_no,
            "variant": tag,
            "kind": "born_digital" if args.born_digital else "scan",
            "n_words": len(words),
            "min_word_conf": round(min((w["confidence"] for w in words), default=0.0), 4),
        }
        if args.born_digital:
            label, changed, unaligned = correct_with_text_layer(label, text)
            meta["words_corrected"] = changed
            meta["words_unaligned"] = unaligned
            ok = unaligned <= args.max_unaligned * max(1, len(words))
            meta["status"] = "exact" if ok else "review"
        else:
            low = [w for w in words if w["confidence"] < args.min_word_conf]
            meta["low_conf_words"] = [f'{w["text"]} ({w["confidence"]:.2f})' for w in low][:50]
            meta["status"] = "review" if low else "auto"
        if args.senocr_url:
            sen = call_ocr(args.senocr_url, args.senocr_model, jpeg, extra={"repetition_penalty": 1.1})
            sen_text = sen["choices"][0]["message"]["content"]
            if not args.born_digital:
                # SenOCR is the second reader: settle disagreements, the
                # confidence gate above is only a fallback without it.
                label, arb = arbitrate(label, sen_text, uni, bi)
                meta["arbitration"] = {
                    "fixed": arb["fixed"],
                    "kept": len(arb["kept"]),
                    "moved": arb["moved"],
                    "senocr_loop": arb["senocr_loop"],
                    "conflicts": arb["conflicts"][:50],
                    "missing": arb["missing"][:50],
                    "n_missing": len(arb["missing"]),
                }
                ok = len(arb["missing"]) <= args.max_missing
                if args.max_conflicts >= 0:
                    ok = ok and len(arb["conflicts"]) <= args.max_conflicts
                meta["status"] = "auto" if ok else "review"
            ref, hyp = norm_tokens(label), norm_tokens(sen_text)
            meta["senocr"] = {
                "text": sen_text,
                "wer_vs_label": round(Levenshtein.distance(ref, hyp) / max(1, len(ref)), 4),
                "substitutions": [
                    f"{ref[o.src_pos]}→{hyp[o.dest_pos]}" for o in Levenshtein.editops(ref, hyp) if o.tag == "replace"
                ][:50],
            }
        if not label.strip():
            meta["status"] = "empty"

        img_rel = os.path.join("images", sid + ".jpg")
        with open(os.path.join(args.out, img_rel), "wb") as f:
            f.write(jpeg)
        rec = {
            "id": sid,
            "image": img_rel,
            "messages": [
                {"role": "user", "content": "<image>OCR:"},
                {"role": "assistant", "content": label},
            ],
            "meta": meta,
        }
        with _write_lock:
            with open(out_path, "a") as f:
                f.write(json.dumps(rec, ensure_ascii=False) + "\n")
            stats["written"] += 1
            print(sid, meta["status"], f'minconf={meta["min_word_conf"]}', meta.get("senocr", {}).get("wer_vs_label", ""), flush=True)

    with ThreadPoolExecutor(max_workers=max(1, args.workers)) as pool:
        for job, fut in [(j, pool.submit(run, j)) for j in jobs]:
            try:
                fut.result()
            except Exception as e:  # keep going; re-run resumes
                stats["failed"] += 1
                print(f"{job[0]} FAILED: {e}", file=sys.stderr, flush=True)

    counts: dict[str, int] = {}
    with open(out_path) as f:
        for l in f:
            s = json.loads(l)["meta"]["status"]
            counts[s] = counts.get(s, 0) + 1
    print(json.dumps({"this_run": stats, "dataset_status": counts}, ensure_ascii=False))


if __name__ == "__main__":
    main()
