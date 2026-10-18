"""WER/RTF of the Vietnamese sherpa-onnx models on FLEURS vi_vn dev.

FLEURS data: data/vi_vn/dev.tsv and data/vi_vn/audio/dev.tar.gz from the
google/fleurs dataset, unpacked so that $FLEURS_DIR/dev/*.wav exist.
Models missing from $ASR_MODELS_DIR are skipped.
"""
import os, re, sys, time, unicodedata
import numpy as np, soundfile as sf, sherpa_onnx as so

M = os.environ.get("ASR_MODELS_DIR", os.path.join(os.path.dirname(__file__), "..", "models"))
N = int(sys.argv[1]) if len(sys.argv) > 1 else 100
D = os.environ.get("FLEURS_DIR", "fleurs")

def zip_(d, e, dec, j):
    return lambda: so.OfflineRecognizer.from_transducer(
        encoder=f"{M}/{d}/{e}", decoder=f"{M}/{d}/{dec}", joiner=f"{M}/{d}/{j}",
        tokens=f"{M}/{d}/tokens.txt", num_threads=4)

MODELS = {
    "zipformer-vi-int8 (2025-04)": zip_("sherpa-onnx-zipformer-vi-int8-2025-04-20",
        "encoder-epoch-12-avg-8.int8.onnx", "decoder-epoch-12-avg-8.onnx", "joiner-epoch-12-avg-8.int8.onnx"),
    "zipformer-vi-30M-int8 (2026-02)": zip_("sherpa-onnx-zipformer-vi-30M-int8-2026-02-09",
        "encoder.int8.onnx", "decoder.onnx", "joiner.int8.onnx"),
    "moonshine-base-vi": lambda: so.OfflineRecognizer.from_moonshine_v2(
        encoder=f"{M}/sherpa-onnx-moonshine-base-vi-quantized-2026-02-27/encoder_model.ort",
        decoder=f"{M}/sherpa-onnx-moonshine-base-vi-quantized-2026-02-27/decoder_model_merged.ort",
        tokens=f"{M}/sherpa-onnx-moonshine-base-vi-quantized-2026-02-27/tokens.txt", num_threads=4),
}

def norm(t):
    t = unicodedata.normalize("NFC", t.lower())
    return re.sub(r"[^\w\s]", " ", t).split()

def wer(r, h):
    d = list(range(len(h) + 1))
    for i, rw in enumerate(r, 1):
        p, d[0] = d[0], i
        for j, hw in enumerate(h, 1):
            p, d[j] = d[j], min(d[j] + 1, d[j - 1] + 1, p + (rw != hw))
    return d[len(h)]

rows = []
seen = set()
for line in open(f"{D}/dev.tsv", encoding="utf-8"):
    c = line.rstrip("\n").split("\t")
    if c[0] in seen:  # one recording per sentence: more speakers' text variety
        continue
    seen.add(c[0]); rows.append((c[1], c[3]))
rows = rows[:N]
audio = []
for f, ref in rows:
    x, sr = sf.read(f"{D}/dev/{f}", dtype="float32")
    audio.append((x, sr, ref))
total_sec = sum(len(x) / sr for x, sr, _ in audio)

for name, mk in MODELS.items():
    try:
        rec = mk()
    except Exception as e:  # model not downloaded
        print(f"{name}: skipped ({e})")
        continue
    errs = words = 0; t0 = time.time(); samples = []
    for x, sr, ref in audio:
        s = rec.create_stream(); s.accept_waveform(sr, x); rec.decode_stream(s)
        h = s.result.text
        r = norm(ref); errs += wer(r, norm(h)); words += len(r)
        if len(samples) < 3: samples.append((ref, h))
    el = time.time() - t0
    print(f"{name:34s} WER {errs/words:6.1%}  RTF {el/total_sec:.3f}  ({len(audio)} clips, {total_sec/60:.1f} min)")
    for r, h in samples: print("   REF:", r, "\n   HYP:", h)
