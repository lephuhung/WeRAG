# sherpa-asr — Vietnamese speech-to-text

CPU-only speech service behind the composer's microphone button
(`frontend-next/lib/speech/dictation.ts`). Compose service `sherpa-asr`,
profile `asr`.

```bash
docker/sherpa-asr/download-models.sh          # once, ~140 MB into docker/sherpa-asr/models
docker compose --profile asr up -d sherpa-asr # or COMPOSE_PROFILES=asr in .env
```

## Models

| Model | Role | Source / licence |
|---|---|---|
| `sherpa-onnx-zipformer-vi-30M-int8-2026-02-09` | speech recognition | k2-fsa/sherpa-onnx release (hynt/Zipformer-30M-RNNT-6000h) |
| `silero_vad.onnx` | cuts speech at pauses | k2-fsa/sherpa-onnx release |
| `vibert-capu/vibert-capu.int8.onnx` | punctuation + capitalisation | dragonSwing/vibert-capu, ONNX by welcomyou/vibert-capu-onnx, CC-BY-SA-4.0 |

Measured on FLEURS vi_vn dev (2026-10-18, `eval/`): recognition WER 9.7 %
(100 clips, RTF 0.012 on 4 threads); punctuation on 149 sentences comma F1
0.66, full stop 0.86, capitals 0.85 (precision 0.95), ~25 ms per sentence.
Gemma E4B scored a little higher on punctuation but took ~420 ms and changed
words in 16/149 sentences. Eight concurrent live sessions add ~0.15 s.

Mid-sentence capitals and colons are applied only at model probability ≥ 0.9
(`capu.SURE`): below that the tagger title-cases short phrases
("Nghị Định Ba Mươi Về …"). Numbers stay spelled out ("ba mươi").

## API

**`WS /ws`** (browser, through the frontend's `/asr/` proxy; the page must be
HTTPS or localhost for the microphone):

1. client → `{"token": "<access token>", "tenant_id": "…", "context": "<end of the draft>"}`;
   the token is checked against `WERAG_BACKEND_URL/api/v1/auth/me`.
2. server → `{"type": "ready"}`; client then streams 16 kHz mono Int16 LE PCM frames.
3. server → `partial` while speaking, `final` after a pause; both carry
   `transcript`, the whole session punctuated (re-punctuated as it grows, with
   `context` as read-only left context).
4. client → `{"type": "stop"}`; server decodes the rest, then `done`.
   Errors: `{"type": "error", "code": "unauthorized" | "unavailable" | "too_long"}`.

**`POST /v1/audio/transcriptions`** — OpenAI-compatible (`file`,
`response_format` = `json` | `text` | `verbose_json`), `Authorization: Bearer
$ASR_API_KEY`; disabled while `ASR_API_KEY` is empty. Any format ffmpeg reads,
up to 100 MB.

## Evaluation

`eval/eval_fleurs.py` (WER) and `eval/eval_punct.py` (punctuation; `--gemma`
adds the Gemma comparison) read FLEURS from `$FLEURS_DIR` (`dev.tsv` + `dev/*.wav`
from google/fleurs `data/vi_vn`). They need `sherpa-onnx onnxruntime tokenizers
soundfile numpy` installed locally.
