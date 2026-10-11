# Spell-check model training (Qwen3-1.7B → `check_spelling`)

Pipeline to fine-tune a small chat model as a drop-in for the
`check_spelling` tool (`internal/agent/tools/check_spelling.go`). The model
is trained on the tool's own request shape — numbered paragraphs in, a
`{"findings":[...]}` JSON out — and is served by vLLM as a `generic`
provider model selected through `spellcheck_model_id`.

Everything runs with the system `torch`/`transformers` plus `peft`,
`accelerate`, `pyarrow` and `pyyaml`. No trl/datasets needed.

```bash
pip install --no-cache-dir --no-deps --target /path/to/pylib peft accelerate
pip install --no-cache-dir --target /path/to/pylib pyarrow
export PYTHONPATH=/path/to/pylib
```

## Data sources and how each is used

| Source | Role | Script |
|---|---|---|
| Knowledge-base chunks (Luật, Nghị định; OCR text) | in-domain paragraphs; OCR errors corrected by the 35B model, conservative edits only → real (noisy, clean) pairs + clean paragraphs | `kb_clean.py`, `llm_correct.py` |
| `Sanng1112/vietnamese-spelling-synthetic-1gb` (gated, CC-BY-SA) | token-aligned synthetic errors, filtered (≤3 errors, no errors on names/codes, typing-only rows downsampled) | `sanng_filter.py` |
| `coung21/vi-spelling-correction` `target`, `bmd1905/vi-error-correction-2.0` `output` | clean Wikipedia / news sentences, noised on the fly | `build_dataset.py --clean-parquet` |
| `nguyenthanhasia/vsec-vietnamese-spell-correction` | **evaluation only** (real human errors, research licence) | `build_eval.py` |

Noise injection (`noiser.py`) follows the VSEC error distribution: about
two thirds tone/diacritic mistakes, one seventh one-key typos, the rest
consonant confusions, doubled words, split/merged syllables and a space
before punctuation. hỏi/ngã is upweighted. Protected tokens (capitalised
mid-sentence, codes, numbers, all-caps, foreign words) are never touched,
and accepted variants (hoà/hòa, lí/lý) are never produced as errors, which
matches the "do NOT report" list in the tool prompt.

## Steps

```bash
S=/path/to/workdir   # data and models live outside the repo

# 1. In-domain paragraphs from the KB (export chunks as JSONL first)
psql ... -Atc "select json_build_object('content', content) from chunks where chunk_type='text' and deleted_at is null" > $S/kb_chunks.jsonl
python kb_clean.py $S/kb_chunks.jsonl $S/kb_paragraphs.jsonl
python llm_correct.py $S/kb_paragraphs.jsonl $S/kb_corrected.jsonl        # uses LLM_BASE_URL / LLM_MODEL_NAME

# 2. Synthetic pairs
python sanng_filter.py $S/validation_full.jsonl --out $S/sanng_filtered.jsonl --limit 200000

# 3. SFT samples in the tool's format (train.jsonl / val.jsonl)
python build_dataset.py --kb $S/kb_corrected.jsonl --pairs $S/sanng_filtered.jsonl \
  --clean-parquet $S/coung21_test.parquet:target --clean-parquet $S/bmd2_test.parquet:output \
  --n 20000 --out $S/sft

# 4. Evaluation set (VSEC + held-out KB paragraphs, never in training)
python build_eval.py --vsec $S/vsec.parquet --kb $S/kb_corrected.jsonl --out $S/eval.jsonl

# 5. Train (LoRA r=32 on all linear layers, prompt tokens masked)
python train_lora.py --model $S/models/Qwen3-1.7B --data $S/sft --out $S/runs/pilot --merge

# 6. Score (same metrics for any backend)
python eval_model.py $S/eval.jsonl --model $S/models/Qwen3-1.7B --adapter $S/runs/pilot/adapter
python eval_model.py $S/eval.jsonl --api http://10.10.0.240:8000 --model-name Qwen/Qwen3.6-35B-A3B-FP8
```

`eval_model.py` validates replies exactly like the Go tool (paragraph in
batch, `wrong` found verbatim, `correct` differs) and reports detection and
correction P/R/F1, the share of clean paragraphs that got a finding
(`cleanFP`) and the share of replies that were not the JSON contract.

## Serving

Serve `runs/<run>/merged` with vLLM (`--enable-prefix-caching`, thinking
off is sent by the backend as `chat_template_kwargs.enable_thinking=false`),
add a `generic` model entry pointing at it in `config/builtin_models.yaml`
and set `spellcheck_model_id` on the document assistant agent.
