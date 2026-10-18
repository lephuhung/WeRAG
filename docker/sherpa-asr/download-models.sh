#!/bin/sh
# Downloads the models the sherpa-asr service loads into ./models (or $1).
#   - zipformer-vi-30M int8: Vietnamese speech recognition (sherpa-onnx release)
#   - silero_vad.onnx: voice activity detection
#   - vibert-capu int8 ONNX: punctuation + capitalisation (CC-BY-SA-4.0,
#     dragonSwing/vibert-capu, ONNX export welcomyou/vibert-capu-onnx)
set -eu
DIR=${1:-$(dirname "$0")/models}
mkdir -p "$DIR/vibert-capu/vocabulary"
cd "$DIR"

ASR=https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models
ZIP=sherpa-onnx-zipformer-vi-30M-int8-2026-02-09
[ -d "$ZIP" ] || curl -fsSL "$ASR/$ZIP.tar.bz2" | tar xj
[ -f silero_vad.onnx ] || curl -fsSLo silero_vad.onnx "$ASR/silero_vad.onnx"

HF=https://huggingface.co/welcomyou/vibert-capu-onnx/resolve/main
for f in vibert-capu.int8.onnx vocab.txt config.json vocabulary/labels.txt; do
  [ -f "vibert-capu/$f" ] || curl -fsSLo "vibert-capu/$f" "$HF/$f"
done
echo "models ready in $DIR"
