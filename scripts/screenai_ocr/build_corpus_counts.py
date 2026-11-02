"""Count syllables and syllable bigrams in a Vietnamese text corpus (vbpl HTML).

make_dataset.py uses the counts to settle words where Screen AI and SenOCR
read a scanned page differently ("trổng lúa" vs "trồng lúa").

    python build_corpus_counts.py /home/orca/data/vbpl/html --out data/vbpl_counts.pkl.gz
"""

import argparse
import collections
import glob
import gzip
import html
import os
import pickle
import re
import unicodedata
from multiprocessing import Pool

_TAG = re.compile(r"<(script|style)\b.*?</\1>|<[^>]+>", re.S | re.I)
_TOKEN = re.compile(r"\w+")


def tokens(text: str) -> list[str]:
    return _TOKEN.findall(unicodedata.normalize("NFC", text).lower())


def count_files(paths: list[str]) -> tuple[collections.Counter, collections.Counter]:
    uni, bi = collections.Counter(), collections.Counter()
    for p in paths:
        with open(p, encoding="utf-8", errors="replace") as f:
            text = html.unescape(_TAG.sub(" ", f.read()))
        # Bigrams do not cross line/sentence punctuation.
        for seg in re.split(r"[\n.;:!?]+", text):
            t = tokens(seg)
            uni.update(t)
            bi.update(zip(t, t[1:]))
    return uni, bi


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("src", help="directory of .html/.txt files")
    ap.add_argument("--out", default="data/vbpl_counts.pkl.gz")
    ap.add_argument("--min-bigram", type=int, default=2)
    ap.add_argument("--procs", type=int, default=8)
    args = ap.parse_args()

    files = sorted(glob.glob(os.path.join(args.src, "*.htm*")) + glob.glob(os.path.join(args.src, "*.txt")))
    batches = [files[i : i + 200] for i in range(0, len(files), 200)]
    uni, bi = collections.Counter(), collections.Counter()
    with Pool(args.procs) as pool:
        for n, (u, b) in enumerate(pool.imap_unordered(count_files, batches), 1):
            uni.update(u)
            bi.update(b)
            print(f"{n}/{len(batches)} batches", flush=True)
    bi = {k: v for k, v in bi.items() if v >= args.min_bigram}
    os.makedirs(os.path.dirname(os.path.abspath(args.out)), exist_ok=True)
    with gzip.open(args.out, "wb") as f:
        pickle.dump({"uni": dict(uni), "bi": bi, "files": len(files)}, f, protocol=pickle.HIGHEST_PROTOCOL)
    print(f"{len(files)} files, {len(uni)} syllables, {len(bi)} bigrams -> {args.out}")


if __name__ == "__main__":
    main()
